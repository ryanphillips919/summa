#!/usr/bin/env python3
"""Small, deterministic cash-flow analytics engine for Summa.

Reads a JSON request from stdin and writes a JSON result to stdout.  Keep this
boundary stable: Go owns authorization and database access; this program only
receives already-authorized ledger-derived cash movements.
"""

from __future__ import annotations

import json
import math
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
        })
    return normalized, horizon


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

    # A transparent baseline suitable for an early product. Use only the most
    # recent 7 calendar days, including zero-movement days.
    recent = [entry["net_cash_flow"] for entry in daily[-7:]]
    baseline = statistics.fmean(recent)
    daily_stddev = statistics.stdev(recent) if len(recent) > 1 else 0.0
    forecast = []
    running_total = 0.0
    for offset in range(1, horizon + 1):
        # Error widens with the square root of the forecast horizon.
        margin = 1.96 * daily_stddev * math.sqrt(offset)
        predicted = baseline
        running_total += predicted
        forecast.append({
            "date": (last_day + timedelta(days=offset)).isoformat(),
            "predicted_net_cash_flow": round(predicted, 2),
            "lower_95": round(predicted - margin, 2),
            "upper_95": round(predicted + margin, 2),
            "cumulative_predicted_cash_flow": round(running_total, 2),
        })

    amounts = [movement["amount"] for movement in movements]
    center = statistics.median(amounts)
    mad = median_absolute_deviation(amounts, center)
    # The modified Z-score's 0.6745 normalizes MAD to a normal distribution.
    # If all values are identical, there is no amount anomaly to report.
    anomalies = []
    if mad > 0:
        for movement in movements:
            score = 0.6745 * (movement["amount"] - center) / mad
            if abs(score) >= 3.5:
                anomalies.append({
                    "id": movement["id"],
                    "date": movement["date"].isoformat(),
                    "amount": round(movement["amount"], 2),
                    "description": movement["description"],
                    "modified_z_score": round(score, 2),
                    "reason": "Amount is unusually large or small relative to typical cash movements.",
                })

    return {
        "methodology": {
            "forecast": "7-day rolling mean of daily net cash flow",
            "anomalies": "modified Z-score using median absolute deviation; threshold 3.5",
        },
        "summary": {
            "movement_count": len(movements),
            "observed_from": first_day.isoformat(),
            "observed_through": last_day.isoformat(),
            "daily_baseline": round(baseline, 2),
            "anomaly_count": len(anomalies),
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
