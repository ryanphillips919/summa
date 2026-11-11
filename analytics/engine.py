#!/usr/bin/env python3
"""Small, deterministic cash-flow analytics engine for Summa.

Reads a JSON request from stdin and writes a JSON result to stdout.  Keeps this
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

from statsmodels.tsa.holtwinters import ExponentialSmoothing

MONTHLY_SEASONAL_PERIODS = 30
MIN_HOLT_WINTERS_DAYS = MONTHLY_SEASONAL_PERIODS * 3
OUTLIER_MIN_HISTORY = 3
OUTLIER_MIN_ABSOLUTE_DIFFERENCE = 500.0


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
            "category": entry.get("category", ""),
        })
    return normalized, horizon


def rolling_mean_prediction(values: list[float]) -> float:
    return statistics.fmean(values[-7:])


def holt_winters_forecast(values: list[float], horizon: int) -> tuple[list[float], float]:
    """Fit daily Holt-Winters with an approximate monthly seasonal cycle."""
    if len(values) < MIN_HOLT_WINTERS_DAYS:
        raise ValueError("Insufficient data for monthly seasonal Holt-Winters")

    model = ExponentialSmoothing(
        values,
        trend="add",
        seasonal="add",
        seasonal_periods=MONTHLY_SEASONAL_PERIODS,
        initialization_method="estimated",
    ).fit(optimized=True)

    forecast_values = model.forecast(horizon)
    fitted_values = model.fittedvalues
    residuals = [actual - fitted for actual, fitted in zip(values, fitted_values)]
    rmse = math.sqrt(statistics.fmean(residual * residual for residual in residuals))
    return list(forecast_values), rmse


def backtest_mae(values: list[float], model_type: str) -> float | None:
    """One-day rolling-origin validation on the newest observed days."""
    errors = []
    start_idx = (
        max(MIN_HOLT_WINTERS_DAYS, len(values) - 14)
        if model_type == "holt_winters"
        else max(14, len(values) - 14)
    )
    
    for index in range(start_idx, len(values)):
        training = values[:index]
        if model_type == "rolling_mean":
            predicted = rolling_mean_prediction(training)
        elif model_type == "holt_winters":
            try:
                hw_preds, _ = holt_winters_forecast(training, 1)
                predicted = hw_preds[0]
            except Exception:
                return None
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

    # Retain the rolling mean for validation and a limited-history fallback.
    # With enough history, forecast using monthly-seasonal Holt-Winters.
    baseline_mae = backtest_mae(values, "rolling_mean")
    hw_mae = backtest_mae(values, "holt_winters")

    try:
        forecast_values, rmse = holt_winters_forecast(values, horizon)
        model_name = "Holt-Winters (monthly seasonality)"
        selected_mae = hw_mae
    except Exception:
        recent = values[-7:]
        forecast_values = [baseline] * horizon
        rmse = statistics.stdev(recent) if len(recent) > 1 else 0.0
        model_name = "7-day rolling mean (limited history fallback)"
        selected_mae = baseline_mae

    forecast = []
    running_total = 0.0
    for offset, predicted in enumerate(forecast_values, start=1):
        forecast_day = last_day + timedelta(days=offset)
        running_total += predicted
        
        # 95% Confidence Interval margin widens progressively over the forecast horizon
        margin = 1.96 * rmse * math.sqrt(offset)
        
        forecast.append({
            "date": forecast_day.isoformat(),
            "predicted_net_cash_flow": round(predicted, 2),
            "lower_95": round(predicted - margin, 2),
            "upper_95": round(predicted + margin, 2),
            "cumulative_predicted_cash_flow": round(running_total, 2),
        })

    # Group duplicates first. A single finding represents the suspected pair;
    # duplicates are excluded from amount checks to avoid duplicate findings.
    expenses = [movement for movement in movements if movement["amount"] < 0]
    duplicates: defaultdict[tuple[str, float, str], list[dict[str, Any]]] = defaultdict(list)
    for movement in expenses:
        descriptor = canonical_text(
            movement["vendor"] or movement["category"] or movement["description"]
        )
        if descriptor:
            duplicates[(movement["date"].isoformat(), round(abs(movement["amount"]), 2), descriptor)].append(movement)

    anomalies = []
    duplicate_ids: set[str] = set()
    for matches in duplicates.values():
        if len(matches) > 1:
            first = matches[0]
            related_ids = [str(movement["id"]) for movement in matches if movement["id"] is not None]
            duplicate_ids.update(related_ids)
            anomalies.append({
                "id": first["id"],
                "related_ids": related_ids,
                "date": first["date"].isoformat(),
                "amount": round(first["amount"], 2),
                "description": first["description"],
                "category": first["category"],
                "modified_z_score": None,
                "reason": f"{len(matches)} matching outflows share the same date, amount, and category.",
                "type": "potential_duplicate",
            })

    # Compare expenses to their own account/category, not every other cost in
    # the business. This keeps routine payroll and supplier payments clean.
    by_category: defaultdict[str, list[dict[str, Any]]] = defaultdict(list)
    for movement in expenses:
        category = canonical_text(movement["category"] or movement["vendor"] or "Uncategorized")
        by_category[category].append(movement)

    for category_expenses in by_category.values():
        if len(category_expenses) < OUTLIER_MIN_HISTORY:
            continue
        amounts = [abs(movement["amount"]) for movement in category_expenses]
        center = statistics.median(amounts)
        mad = median_absolute_deviation(amounts, center)
        for movement in category_expenses:
            movement_id = str(movement["id"])
            amount = abs(movement["amount"])
            if movement_id in duplicate_ids or amount <= center:
                continue
            score = None if mad == 0 else 0.6745 * (amount - center) / mad
            is_outlier = (
                score is not None and score >= 3.5
            ) or (
                mad == 0
                and amount >= center * 2
                and amount - center >= OUTLIER_MIN_ABSOLUTE_DIFFERENCE
            )
            if not is_outlier:
                continue
            category_label = movement["category"] or "this category"
            anomalies.append({
                "id": movement["id"],
                "related_ids": [movement_id] if movement["id"] is not None else [],
                "date": movement["date"].isoformat(),
                "amount": round(movement["amount"], 2),
                "description": movement["description"],
                "category": movement["category"],
                "modified_z_score": round(score, 2) if score is not None else None,
                "reason": (
                    f"{abs(movement['amount']):,.2f} is unusually high for "
                    f"{category_label} (typical outflow: {center:,.2f})."
                ),
                "type": "amount_outlier",
            })

    anomalies.sort(key=lambda item: (item["date"], item["type"]), reverse=True)

    return {
        "methodology": {
            "forecast": "monthly-seasonal Holt-Winters; a 7-day rolling mean is retained only as a limited-history fallback and validation reference",
            "anomalies": "category-aware expense review: high outflows versus category history plus grouped exact duplicate candidates",
        },
        "summary": {
            "movement_count": len(movements),
            "observed_from": first_day.isoformat(),
            "observed_through": last_day.isoformat(),
            "daily_baseline": round(baseline, 2),
            "anomaly_count": len(anomalies),
            "forecast_model": model_name,
            "baseline_validation_mae": round(baseline_mae, 2) if baseline_mae is not None else None,
            "holt_winters_validation_mae": round(hw_mae, 2) if hw_mae is not None else None,
            "selected_model_validation_mae": round(selected_mae, 2) if selected_mae is not None else None,
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
