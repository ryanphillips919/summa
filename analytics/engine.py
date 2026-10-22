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


def holt_winters_forecast(values: list[float], horizon: int) -> tuple[list[float], float]:
    """Fit Holt-Winters Exponential Smoothing with weekly seasonality."""
    try:
        # Require at least 28 days of data for stable seasonal decomposition
        if len(values) < 28:
            raise ValueError("Insufficient data for seasonal Holt-Winters")
            
        model = ExponentialSmoothing(
            values,
            trend="add",
            seasonal="add",
            seasonal_periods=7,
            initialization_method="estimated"
        ).fit(optimized=True)
        
        forecast_values = model.forecast(horizon)
        fitted_values = model.fittedvalues
        residuals = [actual - fitted for actual, fitted in zip(values, fitted_values)]
        rmse = math.sqrt(statistics.fmean(r * r for r in residuals))
        
        return list(forecast_values), rmse
    except Exception:
        # Fallback to rolling mean if optimization fails or data is too short
        recent = values[-7:]
        mean_val = statistics.fmean(recent)
        rmse = statistics.stdev(recent) if len(recent) > 1 else 0.0
        return [mean_val] * horizon, rmse


def backtest_mae(values: list[float], model_type: str) -> float | None:
    """One-day rolling-origin validation on the newest observed days."""
    errors = []
    start_idx = max(28, len(values) - 14) if model_type == "holt_winters" else max(14, len(values) - 14)
    
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
    
    # Run model competition between baseline and Holt-Winters
    baseline_mae = backtest_mae(values, "rolling_mean")
    hw_mae = backtest_mae(values, "holt_winters")
    
    use_hw = hw_mae is not None and (baseline_mae is None or hw_mae < baseline_mae)
    model_name = "Holt-Winters Exponential Smoothing (Additive)" if use_hw else "7-day rolling mean"

    # Generate forecast values and error metrics based on the winning model
    if use_hw:
        forecast_values, rmse = holt_winters_forecast(values, horizon)
    else:
        recent = values[-7:]
        forecast_values = [baseline] * horizon
        rmse = statistics.stdev(recent) if len(recent) > 1 else 0.0

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

    # Audit amount outliers only among outflows
    expenses = [movement for movement in movements if movement["amount"] < 0]
    amounts = [abs(movement["amount"]) for movement in expenses]
    center = statistics.median(amounts) if amounts else 0.0
    mad = median_absolute_deviation(amounts, center) if amounts else 0.0
    
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
                    "id": movement["id"], 
                    "date": movement["date"].isoformat(),
                    "amount": round(movement["amount"], 2), 
                    "description": movement["description"],
                    "modified_z_score": None,
                    "reason": "Potential duplicate: same date, amount, and vendor or description.",
                    "type": "potential_duplicate",
                })

    return {
        "methodology": {
            "forecast": "rolling-mean baseline compared with Holt-Winters Exponential Smoothing using rolling-origin MAE",
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
            "selected_model_validation_mae": round((hw_mae if use_hw else baseline_mae), 2) if (hw_mae if use_hw else baseline_mae) is not None else None,
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