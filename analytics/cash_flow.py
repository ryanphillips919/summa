"""Cash-flow aggregation and forecasting."""

from __future__ import annotations

import math
import statistics
from collections import defaultdict
from datetime import date, timedelta
from typing import Any

from statsmodels.tsa.holtwinters import ExponentialSmoothing


def parse_date(value: str) -> date:
    try:
        return date.fromisoformat(value[:10])
    except (TypeError, ValueError) as exc:
        raise ValueError("date must be an ISO-8601 date or timestamp") from exc


def validation_mae(values: list[float], model: str) -> float | None:
    """Compare forecasting methods on the newest observed days."""
    start = max(90, len(values) - 14) if model == "holt_winters" else max(14, len(values) - 14)
    errors = []
    for index in range(start, len(values)):
        training = values[:index]
        if model == "rolling_mean":
            prediction = statistics.fmean(training[-7:])
        else:
            if len(training) < 90:
                continue
            try:
                prediction = ExponentialSmoothing(
                    training, trend="add", seasonal="add", seasonal_periods=30,
                    initialization_method="estimated",
                ).fit(optimized=True).forecast(1)[0]
            except (ArithmeticError, ValueError):
                continue
        errors.append(abs(values[index] - prediction))
    return statistics.fmean(errors) if errors else None


def forecast(movements: list[dict[str, Any]], horizon: int) -> dict[str, Any]:
    by_day: defaultdict[date, float] = defaultdict(float)
    for movement in movements:
        by_day[parse_date(movement["date"])] += float(movement["amount"])

    first_day, last_day = min(by_day), max(by_day)
    daily = []
    cursor = first_day
    while cursor <= last_day:
        daily.append({"date": cursor, "net_cash_flow": round(by_day[cursor], 2)})
        cursor += timedelta(days=1)

    values = [point["net_cash_flow"] for point in daily]
    recent = values[-7:]
    baseline = statistics.fmean(recent)
    forecast_values = [baseline] * horizon
    rmse = statistics.stdev(recent) if len(recent) > 1 else 0.0
    model_name = "7-day rolling mean (limited history fallback)"

    if len(values) >= 90:
        try:
            model = ExponentialSmoothing(
                values, trend="add", seasonal="add", seasonal_periods=30,
                initialization_method="estimated",
            ).fit(optimized=True)
            forecast_values = list(model.forecast(horizon))
            residuals = [actual - fitted for actual, fitted in zip(values, model.fittedvalues)]
            rmse = math.sqrt(statistics.fmean(value * value for value in residuals))
            model_name = "Holt-Winters (monthly seasonality)"
        except (ArithmeticError, ValueError):
            pass

    baseline_mae = validation_mae(values, "rolling_mean")
    holt_winters_mae = validation_mae(values, "holt_winters")

    projected = []
    running_total = 0.0
    for offset, predicted in enumerate(forecast_values, start=1):
        running_total += predicted
        margin = 1.96 * rmse * math.sqrt(offset)
        projected.append({
            "date": (last_day + timedelta(days=offset)).isoformat(),
            "predicted_net_cash_flow": round(predicted, 2),
            "lower_95": round(predicted - margin, 2),
            "upper_95": round(predicted + margin, 2),
            "cumulative_predicted_cash_flow": round(running_total, 2),
        })

    return {
        "daily_cash_flow": [
            {"date": point["date"].isoformat(), "net_cash_flow": point["net_cash_flow"]}
            for point in daily
        ],
        "forecast": projected,
        "summary": {
            "movement_count": len(movements),
            "observed_from": first_day.isoformat(),
            "observed_through": last_day.isoformat(),
            "daily_baseline": round(baseline, 2),
            "forecast_model": model_name,
            "baseline_validation_mae": round(baseline_mae, 2) if baseline_mae is not None else None,
            "selected_model_validation_mae": round(
                holt_winters_mae if model_name.startswith("Holt-Winters") else baseline_mae,
                2,
            ) if (holt_winters_mae if model_name.startswith("Holt-Winters") else baseline_mae) is not None else None,
        },
    }
