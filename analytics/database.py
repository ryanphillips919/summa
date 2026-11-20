"""SQLite access for the analytics engine."""

from __future__ import annotations

import sqlite3
from datetime import date, timedelta
from pathlib import Path
from typing import Any


def load_cash_snapshot(
    database_path: str,
    organization_id: int,
    forecast_days: int,
    observation_days: int,
) -> dict[str, Any] | None:
    """Return one organization's cash movements and opening balance."""
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
              AND cash.name = 'Cash' AND cash.type = 'asset'
            """,
            (organization_id,),
        ).fetchone()
        if latest_row is None or latest_row[0] is None:
            return None

        start_date = (
            date.fromisoformat(latest_row[0][:10])
            - timedelta(days=observation_days - 1)
        ).isoformat()

        opening_cents = conn.execute(
            """
            SELECT COALESCE(SUM(CASE e.direction
                WHEN 'debit' THEN e.amount ELSE -e.amount END), 0)
            FROM transactions AS t
            JOIN entries AS e ON e.transaction_id = t.id
            JOIN accounts AS cash ON cash.id = e.account_id
            WHERE t.organization_id = ?
              AND cash.organization_id = t.organization_id
              AND cash.name = 'Cash' AND cash.type = 'asset'
              AND t.date < ?
            """,
            (organization_id, start_date),
        ).fetchone()[0]

        rows = conn.execute(
            """
            SELECT t.id, t.date,
                SUM(CASE e.direction WHEN 'debit' THEN e.amount ELSE -e.amount END),
                t.description,
                COALESCE((
                    SELECT GROUP_CONCAT(DISTINCT counterparty.name)
                    FROM entries AS other_entry
                    JOIN accounts AS counterparty ON counterparty.id = other_entry.account_id
                    WHERE other_entry.transaction_id = t.id
                      AND counterparty.organization_id = t.organization_id
                      AND counterparty.name != 'Cash'
                ), '')
            FROM transactions AS t
            JOIN entries AS e ON e.transaction_id = t.id
            JOIN accounts AS cash ON cash.id = e.account_id
            WHERE t.organization_id = ?
              AND cash.organization_id = t.organization_id
              AND cash.name = 'Cash' AND cash.type = 'asset'
              AND t.date >= ?
            GROUP BY t.id, t.date, t.description
            HAVING SUM(CASE e.direction WHEN 'debit' THEN e.amount ELSE -e.amount END) != 0
            ORDER BY t.date, t.id
            """,
            (organization_id, start_date),
        ).fetchall()

    return {
        "forecast_days": forecast_days,
        "opening_cash_balance": opening_cents / 100,
        "cash_movements": [
            {
                "id": str(transaction_id),
                "date": movement_date,
                "amount": cash_cents / 100,
                "description": description,
                "category": category,
            }
            for transaction_id, movement_date, cash_cents, description, category in rows
        ],
    }
