#!/usr/bin/env python3
"""Command-line entry point for Summa analytics."""

from __future__ import annotations

import argparse
import json
import sqlite3
import sys

from anomalies import find_anomalies
from cash_flow import forecast
from database import load_cash_snapshot


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--database", required=True)
    parser.add_argument("--organization-id", required=True, type=int)
    parser.add_argument("--forecast-days", default=30, type=int)
    parser.add_argument("--observation-days", default=120, type=int)
    args = parser.parse_args()

    try:
        if not 1 <= args.forecast_days <= 90:
            raise ValueError("--forecast-days must be an integer between 1 and 90")
        if args.observation_days < 1:
            raise ValueError("--observation-days must be positive")

        snapshot = load_cash_snapshot(
            args.database,
            args.organization_id,
            args.forecast_days,
            args.observation_days,
        )
        if snapshot is None:
            result = {"empty": True}
        else:
            movements = snapshot["cash_movements"]
            result = forecast(movements, snapshot["forecast_days"])
            result["anomalies"] = find_anomalies(movements)
            result["summary"]["anomaly_count"] = len(result["anomalies"])
            result["methodology"] = {
                "forecast": "monthly-seasonal Holt-Winters with a 7-day rolling-mean limited-history fallback",
                "anomalies": "category-aware expense review plus grouped exact duplicate candidates",
            }
            result["cash_movements"] = movements
            result["opening_cash_balance"] = snapshot["opening_cash_balance"]

        json.dump(result, sys.stdout, separators=(",", ":"))
    except (OSError, sqlite3.Error, ValueError) as exc:
        print(json.dumps({"error": str(exc)}), file=sys.stderr)
        sys.exit(2)


if __name__ == "__main__":
    main()
