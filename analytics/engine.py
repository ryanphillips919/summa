#!/usr/bin/env python3
"""Small, deterministic cash-flow analytics engine for Summa.

Reads a JSON request from stdin and writes a JSON result to stdout.  Keep this
boundary stable: Go owns authorization and database access; this program only
receives already-authorized ledger-derived cash movements.
"""

from __future__ import annotations

import json
import math
import re
import statistics
import sys
from collections import defaultdict
from datetime import date, datetime, timedelta
from typing import Any


def parse_date(value: str) -> date:
    """Accept the ISO date or timestamp that Summa's API returns."""
    try:
        return date.fromisoformat(value[:10])
    except (TypeError, ValueError) as exc:
        raise ValueError("date must be an ISO-8601 date or timestamp") from exc


def median_absolute_deviation(values: list[float], center: float) -> float:
    return statistics.median(abs(value - center) for value in values)


def validate_request(payload: dict[str, Any]) -> tuple[list[dict[str, Any]], int]:
    entries = payload.get("cash_movements")
    if not isinstance(entries, list) or not entries:
        raise ValueError("cash_movements must be a non-empty array")
    horizon = payload.get("forecast_days", 14)
    if not isinstance(horizon, int) or not 1 <= horizon <= 90:
        raise ValueError("forecast_days must be an integer between 1 and 90")

    normalized = []
    for index, entry in enumerate(entries):
        if not isinstance(entry, dict):
            raise ValueError(f"cash_movements[{index}] must be an object")
        movement_date = parse_date(entry.get("date"))
        amount = entry.get("amount")
        if not isinstance(amount, (int, float)) or isinstance(amount, bool):
            raise ValueError(f"cash_movements[{index}].amount must be a number")
        normalized.append({
            "date": movement_date,
            "amount": float(amount),
            "id": entry.get("id"),
            "description": entry.get("description", ""),
            "vendor": entry.get("vendor", ""),
        })
    return normalized, horizon


def rolling_mean_prediction(values: list[float]) -> float:
    return statistics.fmean(values[-7:])


def trend_seasonal_fit(values: list[float], first_weekday: int) -> tuple[float, float, list[float], float]:
    """Fit a linear trend plus a weekday residual using plain Python."""
    window = values[-min(56, len(values)):]
    x_values = list(range(len(window)))
    x_mean, y_mean = statistics.fmean(x_values), statistics.fmean(window)
    denominator = sum((x - x_mean) ** 2 for x in x_values)
    slope = sum((x - x_mean) * (y - y_mean) for x, y in zip(x_values, window)) / denominator
    intercept = y_mean - slope * x_mean
    residuals = [y - (intercept + slope * x) for x, y in zip(x_values, window)]
    weekday_adjustment = [
        statistics.fmean([
            residual for index, residual in enumerate(residuals)
            if (first_weekday + len(values) - len(window) + index) % 7 == weekday
        ])
        for weekday in range(7)
    ]
    rmse = math.sqrt(statistics.fmean(value * value for value in residuals))
    return intercept, slope, weekday_adjustment, rmse


def backtest_mae(values: list[float], model: str, first_weekday: int) -> float | None:
    """One-day rolling-origin validation on the newest observed days."""
    errors = []
    for index in range(max(14, len(values) - 14), len(values)):
        training = values[:index]
        if model == "rolling_mean":
            predicted = rolling_mean_prediction(training)
        elif len(training) >= 28:
            intercept, slope, seasonal, _ = trend_seasonal_fit(training, first_weekday)
            predicted = intercept + slope * min(56, len(training)) + seasonal[(first_weekday + index) % 7]
        else:
            return None
        errors.append(abs(values[index] - predicted))
    return statistics.fmean(errors) if errors else None


def canonical_text(value: str) -> str:
    return re.sub(r"\s+", " ", value.strip().casefold())


def analyze(payload: dict[str, Any]) -> dict[str, Any]:
    movements, horizon = validate_request(payload)
    by_day: defaultdict[date, float] = defaultdict(float)
    for movement in movements:
        by_day[movement["date"]] += movement["amount"]

    first_day, last_day = min(by_day), max(by_day)
    daily = []
    cursor = first_day
    while cursor <= last_day:
        daily.append({"date": cursor, "net_cash_flow": round(by_day[cursor], 2)})
        cursor += timedelta(days=1)

    values = [entry["net_cash_flow"] for entry in daily]
    baseline = rolling_mean_prediction(values)
    baseline_mae = backtest_mae(values, "rolling_mean", first_day.weekday())
    trend_mae = backtest_mae(values, "trend_seasonal", first_day.weekday())
    use_trend = trend_mae is not None and (baseline_mae is None or trend_mae < baseline_mae)
    model_name = "linear trend with weekday seasonality" if use_trend else "7-day rolling mean"
    if use_trend:
        intercept, slope, weekday_adjustment, rmse = trend_seasonal_fit(values, first_day.weekday())
    else:
        recent = values[-7:]
        rmse = statistics.stdev(recent) if len(recent) > 1 else 0.0
    forecast = []
    running_total = 0.0
    for offset in range(1, horizon + 1):
        forecast_day = last_day + timedelta(days=offset)
        if use_trend:
            x = min(56, len(values)) + offset - 1
            predicted = intercept + slope * x + weekday_adjustment[forecast_day.weekday()]
            margin = 1.96 * rmse * math.sqrt(1 + offset / len(values))
        else:
            predicted = baseline
            margin = 1.96 * rmse * math.sqrt(offset)
        running_total += predicted
        forecast.append({
            "date": forecast_day.isoformat(),
            "predicted_net_cash_flow": round(predicted, 2),
            "lower_95": round(predicted - margin, 2),
            "upper_95": round(predicted + margin, 2),
            "cumulative_predicted_cash_flow": round(running_total, 2),
        })

    # Audit amount outliers only among outflows. Comparing payments to revenue
    # would make the score difficult to interpret.
    expenses = [movement for movement in movements if movement["amount"] < 0]
    amounts = [abs(movement["amount"]) for movement in expenses]
    center = statistics.median(amounts) if amounts else 0.0
    mad = median_absolute_deviation(amounts, center) if amounts else 0.0
    # The modified Z-score's 0.6745 normalizes MAD to a normal distribution.
    # If all values are identical, there is no amount anomaly to report.
    anomalies = []
    if mad > 0:
        for movement in expenses:
            score = 0.6745 * (abs(movement["amount"]) - center) / mad
            if abs(score) >= 3.5:
                anomalies.append({
                    "id": movement["id"],
                    "date": movement["date"].isoformat(),
                    "amount": round(movement["amount"], 2),
                    "description": movement["description"],
                    "modified_z_score": round(score, 2),
                    "reason": "Expense amount differs substantially from typical spending.",
                    "type": "amount_outlier",
                })

    duplicates: defaultdict[tuple[str, float, str], list[dict[str, Any]]] = defaultdict(list)
    for movement in expenses:
        descriptor = canonical_text(movement["vendor"] or movement["description"])
        if descriptor:
            duplicates[(movement["date"].isoformat(), round(abs(movement["amount"]), 2), descriptor)].append(movement)
    for matches in duplicates.values():
        if len(matches) > 1:
            for movement in matches:
                anomalies.append({
                    "id": movement["id"], "date": movement["date"].isoformat(),
                    "amount": round(movement["amount"], 2), "description": movement["description"],
                    "modified_z_score": None,
                    "reason": "Potential duplicate: same date, amount, and vendor or description.",
                    "type": "potential_duplicate",
                })

    return {
        "methodology": {
            "forecast": "rolling-mean baseline compared with linear trend plus weekday seasonality using rolling-origin MAE",
            "anomalies": "expense amount modified Z-score (MAD threshold 3.5) plus exact duplicate candidates",
        },
        "summary": {
            "movement_count": len(movements),
            "observed_from": first_day.isoformat(),
            "observed_through": last_day.isoformat(),
            "daily_baseline": round(baseline, 2),
            "anomaly_count": len(anomalies),
            "forecast_model": model_name,
            "baseline_validation_mae": round(baseline_mae, 2) if baseline_mae is not None else None,
            "selected_model_validation_mae": round((trend_mae if use_trend else baseline_mae), 2) if (trend_mae if use_trend else baseline_mae) is not None else None,
        },
        "daily_cash_flow": [
            {"date": entry["date"].isoformat(), "net_cash_flow": entry["net_cash_flow"]}
            for entry in daily
        ],
        "forecast": forecast,
        "anomalies": anomalies,
    }


def main() -> None:
    try:
        payload = json.load(sys.stdin)
        if not isinstance(payload, dict):
            raise ValueError("request body must be a JSON object")
        json.dump(analyze(payload), sys.stdout, separators=(",", ":"))
    except (ValueError, json.JSONDecodeError) as exc:
        print(json.dumps({"error": str(exc)}), file=sys.stderr)
        sys.exit(2)


if __name__ == "__main__":
    main()
