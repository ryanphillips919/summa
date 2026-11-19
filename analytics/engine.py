#!/usr/bin/env python3
"""Cash-flow forecasting and anomaly detection for Summa."""

from __future__ import annotations

import json
import math
import re
import statistics
import sys
from collections import defaultdict
from datetime import date, timedelta
from typing import Any

from statsmodels.tsa.holtwinters import ExponentialSmoothing

MONTHLY_SEASONAL_PERIODS = 30
MIN_HOLT_WINTERS_OBSERVATIONS = MONTHLY_SEASONAL_PERIODS * 2
ROLLING_WINDOW_DAYS = 30
MIN_CATEGORY_HISTORY = 4
OUTLIER_THRESHOLD = 3.5


def parse_date(value: Any) -> date:
    try:
        return date.fromisoformat(value[:10])
    except (TypeError, ValueError) as exc:
        raise ValueError("date must be an ISO-8601 date or timestamp") from exc


def canonical_text(value: Any) -> str:
    if not isinstance(value, str):
        return ""
    return re.sub(r"\s+", " ", value.strip().casefold())


def median_absolute_deviation(values: list[float], center: float) -> float:
    return statistics.median(abs(value - center) for value in values)


def category_for(movement: dict[str, Any]) -> str:
    return (
        canonical_text(movement["category"])
        or canonical_text(movement["vendor"])
        or "uncategorized"
    )


def descriptor_for(movement: dict[str, Any]) -> str:
    return (
        canonical_text(movement["vendor"])
        or canonical_text(movement["description"])
        or category_for(movement)
    )


def validate_request(payload: dict[str, Any]) -> tuple[list[dict[str, Any]], int]:
    entries = payload.get("cash_movements")
    if not isinstance(entries, list) or not entries:
        raise ValueError("cash_movements must be a non-empty array")

    horizon = payload.get("forecast_days", 30)
    if not isinstance(horizon, int) or not 1 <= horizon <= 90:
        raise ValueError("forecast_days must be an integer between 1 and 90")

    normalized = []
    for index, entry in enumerate(entries):
        if not isinstance(entry, dict):
            raise ValueError(f"cash_movements[{index}] must be an object")

        amount = entry.get("amount")
        if not isinstance(amount, (int, float)) or isinstance(amount, bool):
            raise ValueError(f"cash_movements[{index}].amount must be a number")

        normalized.append(
            {
                "id": str(entry.get("id", "")),
                "date": parse_date(entry.get("date")),
                "amount": float(amount),
                "description": str(entry.get("description", "")),
                "vendor": str(entry.get("vendor", "")),
                "category": str(entry.get("category", "")),
            }
        )

    return normalized, horizon


def rolling_mean_prediction(values: list[float]) -> float:
    return statistics.fmean(values[-ROLLING_WINDOW_DAYS:])


def rolling_mean_forecast(values: list[float], horizon: int) -> tuple[list[float], float]:
    recent = values[-ROLLING_WINDOW_DAYS:]
    prediction = statistics.fmean(recent)
    rmse = statistics.stdev(recent) if len(recent) > 1 else 0.0
    return [prediction] * horizon, rmse


def holt_winters_forecast(
    values: list[float],
    horizon: int,
) -> tuple[list[float], float] | None:
    """Forecast daily cash flow using an approximately monthly 30-day cycle."""
    if len(values) < MIN_HOLT_WINTERS_OBSERVATIONS:
        return None

    try:
        model = ExponentialSmoothing(
            values,
            trend="add",
            seasonal="add",
            seasonal_periods=MONTHLY_SEASONAL_PERIODS,
            initialization_method="estimated",
        ).fit(optimized=True)

        forecast_values = list(model.forecast(horizon))
        residuals = [
            actual - fitted
            for actual, fitted in zip(values, model.fittedvalues)
        ]
        rmse = math.sqrt(statistics.fmean(residual * residual for residual in residuals))
        return forecast_values, rmse
    except (ValueError, ArithmeticError, OverflowError):
        return None


def backtest_mae(values: list[float], model_type: str) -> float | None:
    """Evaluate one-day predictions on the newest observed dates."""
    if model_type == "holt_winters":
        start = max(MIN_HOLT_WINTERS_OBSERVATIONS, len(values) - 14)
    elif model_type == "rolling_mean":
        start = max(ROLLING_WINDOW_DAYS, len(values) - 14)
    else:
        return None

    errors = []
    for index in range(start, len(values)):
        training = values[:index]

        if model_type == "rolling_mean":
            prediction = rolling_mean_prediction(training)
        else:
            forecast = holt_winters_forecast(training, 1)
            if forecast is None:
                continue
            prediction = forecast[0][0]

        errors.append(abs(values[index] - prediction))

    return statistics.fmean(errors) if errors else None


def is_recurring(movement: dict[str, Any], category_movements: list[dict[str, Any]]) -> bool:
    """Recognize a repeated, similarly sized payment before calling it unusual."""
    descriptor = descriptor_for(movement)
    matching = [
        candidate
        for candidate in category_movements
        if descriptor_for(candidate) == descriptor
    ]

    if len(matching) < 3:
        return False

    amounts = [abs(candidate["amount"]) for candidate in matching]
    typical_amount = statistics.median(amounts)
    if typical_amount == 0:
        return False

    if any(
        abs(abs(candidate["amount"]) - typical_amount) / typical_amount > 0.15
        for candidate in matching
    ):
        return False

    dates = sorted(candidate["date"] for candidate in matching)
    intervals = [
        (later - earlier).days
        for earlier, later in zip(dates, dates[1:])
    ]
    if not intervals:
        return False

    typical_interval = statistics.median(intervals)
    return all(
        abs(interval - typical_interval) <= max(3, typical_interval * 0.25)
        for interval in intervals
    )


def category_outliers(expenses: list[dict[str, Any]]) -> list[dict[str, Any]]:
    by_category: defaultdict[str, list[dict[str, Any]]] = defaultdict(list)
    for movement in expenses:
        by_category[category_for(movement)].append(movement)

    anomalies = []
    for category, movements in by_category.items():
        for movement in movements:
            history = [
                candidate
                for candidate in movements
                if candidate["id"] != movement["id"]
            ]
            if len(history) < MIN_CATEGORY_HISTORY:
                continue

            if is_recurring(movement, movements):
                continue

            amounts = [abs(candidate["amount"]) for candidate in history]
            center = statistics.median(amounts)
            mad = median_absolute_deviation(amounts, center)
            amount = abs(movement["amount"])

            if mad == 0:
                is_outlier = amount > center and amount - center >= max(1.0, center * 0.25)
                score = None
            else:
                score = 0.6745 * (amount - center) / mad
                is_outlier = abs(score) >= OUTLIER_THRESHOLD

            if not is_outlier:
                continue

            anomalies.append(
                {
                    "id": movement["id"],
                    "date": movement["date"].isoformat(),
                    "amount": round(movement["amount"], 2),
                    "description": movement["description"],
                    "category": category,
                    "modified_z_score": round(score, 2) if score is not None else None,
                    "reason": (
                        f"Expense differs substantially from prior {category} spending."
                    ),
                    "type": "category_amount_outlier",
                    "related_transaction_ids": [],
                }
            )

    return anomalies


def duplicate_candidates(expenses: list[dict[str, Any]]) -> list[dict[str, Any]]:
    groups: defaultdict[tuple[str, float, str], list[dict[str, Any]]] = defaultdict(list)

    for movement in expenses:
        groups[
            (
                movement["date"].isoformat(),
                round(abs(movement["amount"]), 2),
                descriptor_for(movement),
            )
        ].append(movement)

    anomalies = []
    for matches in groups.values():
        if len(matches) < 2:
            continue

        matching_ids = [movement["id"] for movement in matches]
        for movement in matches:
            anomalies.append(
                {
                    "id": movement["id"],
                    "date": movement["date"].isoformat(),
                    "amount": round(movement["amount"], 2),
                    "description": movement["description"],
                    "category": category_for(movement),
                    "modified_z_score": None,
                    "reason": (
                        "Potential duplicate: same date, amount, and payee or description."
                    ),
                    "type": "potential_duplicate",
                    "related_transaction_ids": [
                        transaction_id
                        for transaction_id in matching_ids
                        if transaction_id != movement["id"]
                    ],
                }
            )

    return anomalies


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

    values = [point["net_cash_flow"] for point in daily]
    baseline_mae = backtest_mae(values, "rolling_mean")
    holt_winters_mae = backtest_mae(values, "holt_winters")

    holt_winters = holt_winters_forecast(values, horizon)
    if holt_winters is None:
        forecast_values, rmse = rolling_mean_forecast(values, horizon)
        model_name = "30-day rolling mean (limited-history fallback)"
        selected_mae = baseline_mae
    else:
        forecast_values, rmse = holt_winters
        model_name = "Holt-Winters Exponential Smoothing (30-day seasonality)"
        selected_mae = holt_winters_mae

    forecast = []
    running_total = 0.0
    for offset, predicted in enumerate(forecast_values, start=1):
        forecast_day = last_day + timedelta(days=offset)
        running_total += predicted
        margin = 1.96 * rmse * math.sqrt(offset)

        forecast.append(
            {
                "date": forecast_day.isoformat(),
                "predicted_net_cash_flow": round(predicted, 2),
                "lower_95": round(predicted - margin, 2),
                "upper_95": round(predicted + margin, 2),
                "cumulative_predicted_cash_flow": round(running_total, 2),
            }
        )

    expenses = [movement for movement in movements if movement["amount"] < 0]
    anomalies = category_outliers(expenses) + duplicate_candidates(expenses)

    return {
        "methodology": {
            "forecast": (
                "30-day seasonal Holt-Winters is the primary model; "
                "a 30-day rolling mean is used only when history is insufficient. "
                "Both can be evaluated with rolling-origin MAE."
            ),
            "anomalies": (
                "Expense amounts are compared with historical spending in their "
                "category, recurring payments are excluded, and exact duplicate "
                "candidates are reported separately."
            ),
        },
        "summary": {
            "movement_count": len(movements),
            "observed_from": first_day.isoformat(),
            "observed_through": last_day.isoformat(),
            "daily_baseline": round(rolling_mean_prediction(values), 2),
            "anomaly_count": len(anomalies),
            "forecast_model": model_name,
            "baseline_validation_mae": (
                round(baseline_mae, 2) if baseline_mae is not None else None
            ),
            "selected_model_validation_mae": (
                round(selected_mae, 2) if selected_mae is not None else None
            ),
        },
        "daily_cash_flow": [
            {
                "date": point["date"].isoformat(),
                "net_cash_flow": point["net_cash_flow"],
            }
            for point in daily
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