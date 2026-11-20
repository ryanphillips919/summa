"""Category-aware expense review signals."""

from __future__ import annotations

import re
import statistics
from collections import defaultdict
from typing import Any


def canonical_text(value: str) -> str:
    return re.sub(r"\s+", " ", value.strip().casefold())


def find_anomalies(movements: list[dict[str, Any]]) -> list[dict[str, Any]]:
    expenses = [movement for movement in movements if movement["amount"] < 0]
    duplicates: defaultdict[tuple[str, float, str], list[dict[str, Any]]] = defaultdict(list)
    for movement in expenses:
        descriptor = canonical_text(movement.get("category") or movement.get("description", ""))
        duplicates[(movement["date"][:10], round(abs(movement["amount"]), 2), descriptor)].append(movement)

    findings = []
    duplicate_ids: set[str] = set()
    for matches in duplicates.values():
        if len(matches) < 2:
            continue
        first = matches[0]
        related_ids = [str(item["id"]) for item in matches]
        duplicate_ids.update(related_ids)
        findings.append({
            "id": first["id"], "related_ids": related_ids, "date": first["date"][:10],
            "amount": round(first["amount"], 2), "description": first["description"],
            "category": first.get("category", ""), "modified_z_score": None,
            "reason": f"{len(matches)} matching outflows share the same date, amount, and category.",
            "type": "potential_duplicate",
        })

    by_category: defaultdict[str, list[dict[str, Any]]] = defaultdict(list)
    for movement in expenses:
        by_category[canonical_text(movement.get("category") or "Uncategorized")].append(movement)
    for grouped in by_category.values():
        if len(grouped) < 3:
            continue
        amounts = [abs(item["amount"]) for item in grouped]
        center = statistics.median(amounts)
        mad = statistics.median(abs(amount - center) for amount in amounts)
        for movement in grouped:
            amount = abs(movement["amount"])
            score = None if mad == 0 else 0.6745 * (amount - center) / mad
            unusual = (score is not None and score >= 3.5) or (
                mad == 0 and amount >= center * 2 and amount - center >= 500
            )
            if str(movement["id"]) in duplicate_ids or amount <= center or not unusual:
                continue
            findings.append({
                "id": movement["id"], "related_ids": [str(movement["id"])],
                "date": movement["date"][:10], "amount": round(movement["amount"], 2),
                "description": movement["description"], "category": movement.get("category", ""),
                "modified_z_score": round(score, 2) if score is not None else None,
                "reason": f"{amount:,.2f} is unusually high for {movement.get('category') or 'this category'} (typical outflow: {center:,.2f}).",
                "type": "amount_outlier",
            })
    return sorted(findings, key=lambda item: (item["date"], item["type"]), reverse=True)
