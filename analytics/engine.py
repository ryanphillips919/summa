#!/usr/bin/env python3
"""Cash-flow analytics engine for Summa.

Reads a JSON request from stdin and writes a JSON result to stdout.  Keeps this
boundary stable: Go owns authorization and database access; this program only
receives already-authorized ledger-derived cash movements.
"""

from __future__ import annotations

import argparse
import json
import math
import re
import sqlite3
import statistics
import sys
from collections import defaultdict
from datetime import date, timedelta
from pathlib import Path
from typing import Any

from statsmodels.tsa.holtwinters import ExponentialSmoothing


DEFAULT_OBSERVATION_DAYS = 120


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


def load_database_request(
    database_path: str,
    organization_id: int,
    forecast_days: int,
    observation_days: int,
) -> dict[str, Any] | None:
    """Load one organization's cash movements directly from SQLite."""
    database_uri = Path(database_path).resolve().as_uri() + "?mode=ro"

    with sqlite3.connect(database_uri, uri=True) as conn:
        latest_row = conn.execute(
            """
            SELECT MAX(t.date)
            FROM transactions AS t
            JOIN entries AS e ON e.transaction_id = t.id
            JOIN accounts AS cash ON cash.id = e.account_id
            WHERE t.organization_id = ?
              AND cash.organization_id = t.organization_id
              AND cash.name = 'Cash'
              AND cash.type = 'asset'
            """,
            (organization_id,),
        ).fetchone()

        if latest_row is None or latest_row[0] is None:
            return None

        latest = parse_date(latest_row[0])
        start_date = (latest - timedelta(days=observation_days - 1)).isoformat()

        opening_row = conn.execute(
            """
            SELECT COALESCE(SUM(
                CASE e.direction
                    WHEN 'debit' THEN e.amount
                    ELSE -e.amount
                END
            ), 0)
            FROM transactions AS t
            JOIN entries AS e ON e.transaction_id = t.id
            JOIN accounts AS cash ON cash.id = e.account_id
            WHERE t.organization_id = ?
              AND cash.organization_id = t.organization_id
              AND cash.name = 'Cash'
              AND cash.type = 'asset'
              AND t.date < ?
            """,
            (organization_id, start_date),
        ).fetchone()

        rows = conn.execute(
            """
            SELECT
                t.id,
                t.date,
                SUM(CASE e.direction
                    WHEN 'debit' THEN e.amount
                    ELSE -e.amount
                END) AS cash_cents,
                t.description,
                COALESCE((
                    SELECT GROUP_CONCAT(DISTINCT counterparty.name)
                    FROM entries AS other_entry
                    JOIN accounts AS counterparty
                        ON counterparty.id = other_entry.account_id
                    WHERE other_entry.transaction_id = t.id
                      AND counterparty.organization_id = t.organization_id
                      AND counterparty.name != 'Cash'
                ), '') AS category
            FROM transactions AS t
            JOIN entries AS e ON e.transaction_id = t.id
            JOIN accounts AS cash ON cash.id = e.account_id
            WHERE t.organization_id = ?
              AND cash.organization_id = t.organization_id
              AND cash.name = 'Cash'
              AND cash.type = 'asset'
              AND t.date >= ?
            GROUP BY t.id, t.date, t.description
            HAVING cash_cents != 0
            ORDER BY t.date, t.id
            """,
            (organization_id, start_date),
        ).fetchall()

    movements = [
        {
            "id": str(transaction_id),
            "date": movement_date,
            "amount": cash_cents / 100,
            "description": description,
            "category": category,
        }
        for transaction_id, movement_date, cash_cents, description, category in rows
    ]

    return {
        "forecast_days": forecast_days,
        "opening_cash_balance": opening_row[0] / 100,
        "cash_movements": movements,
    }


def analyze_database(
    database_path: str,
    organization_id: int,
    forecast_days: int,
    observation_days: int,
) -> dict[str, Any]:
    request = load_database_request(
        database_path,
        organization_id,
        forecast_days,
        observation_days,
    )
    if request is None:
        return {"empty": True}

    result = analyze(request)
    result["cash_movements"] = request["cash_movements"]
    result["opening_cash_balance"] = request["opening_cash_balance"]
    return result


def rolling_mean_prediction(values: list[float]) -> float:
    return statistics.fmean(values[-7:])


def holt_winters_forecast(values: list[float], horizon: int) -> tuple[list[float], float]:
    """Fit Holt-Winters with an approximately monthly 30-day cycle."""
    try:
        if len(values) < 90:
            raise ValueError("Insufficient data for monthly seasonal Holt-Winters")
            
        model = ExponentialSmoothing(
            values,
            trend="add",
            seasonal="add",
            seasonal_periods=30,
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
    start_idx = max(90, len(values) - 14) if model_type == "holt_winters" else max(14, len(values) - 14)
    
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
    
    use_hw = len(values) >= 90
    model_name = "Holt-Winters (monthly seasonality)" if use_hw else "7-day rolling mean (limited history fallback)"

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

    # Group exact duplicate candidates first. A single finding represents a
    # suspected group, and those entries are excluded from amount outliers.
    expenses = [movement for movement in movements if movement["amount"] < 0]
    anomalies = []
    duplicates: defaultdict[tuple[str, float, str], list[dict[str, Any]]] = defaultdict(list)
    for movement in expenses:
        descriptor = canonical_text(
            movement["vendor"] or movement["category"] or movement["description"]
        )
        if descriptor:
            duplicates[(movement["date"].isoformat(), round(abs(movement["amount"]), 2), descriptor)].append(movement)
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

    # Compare expenses to their own category, not every other business cost.
    by_category: defaultdict[str, list[dict[str, Any]]] = defaultdict(list)
    for movement in expenses:
        category = canonical_text(movement["category"] or movement["vendor"] or "Uncategorized")
        by_category[category].append(movement)

    for category_expenses in by_category.values():
        if len(category_expenses) < 3:
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
            is_outlier = (score is not None and score >= 3.5) or (
                mad == 0 and amount >= center * 2 and amount - center >= 500.0
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
                "reason": f"{amount:,.2f} is unusually high for {category_label} (typical outflow: {center:,.2f}).",
                "type": "amount_outlier",
            })

    anomalies.sort(key=lambda item: (item["date"], item["type"]), reverse=True)

    return {
        "methodology": {
            "forecast": "monthly-seasonal Holt-Winters with a 7-day rolling-mean limited-history fallback; both MAEs are reported for evaluation",
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
        parser = argparse.ArgumentParser()
        parser.add_argument("--database")
        parser.add_argument("--organization-id", type=int)
        parser.add_argument("--forecast-days", type=int, default=30)
        parser.add_argument(
            "--observation-days",
            type=int,
            default=DEFAULT_OBSERVATION_DAYS,
        )
        args = parser.parse_args()

        if args.database:
            if args.organization_id is None:
                raise ValueError("--organization-id is required with --database")
            if not 1 <= args.forecast_days <= 90:
                raise ValueError("--forecast-days must be an integer between 1 and 90")
            if args.observation_days < 1:
                raise ValueError("--observation-days must be positive")
            result = analyze_database(
                args.database,
                args.organization_id,
                args.forecast_days,
                args.observation_days,
            )
        else:
            payload = json.load(sys.stdin)
            if not isinstance(payload, dict):
                raise ValueError("request body must be a JSON object")
            result = analyze(payload)

        json.dump(result, sys.stdout, separators=(",", ":"))
    except (OSError, sqlite3.Error, ValueError, json.JSONDecodeError) as exc:
        print(json.dumps({"error": str(exc)}), file=sys.stderr)
        sys.exit(2)


if __name__ == "__main__":
    main()
