#!/usr/bin/env python3
"""Rank managing units by observed explicit links and invoice-list density.

Usage: python3 research/brazil-procurement-recon/select_cohort.py
Default eligibility: >=50 commitments with both bridge types, >=20 invoice-list
rows, >=50 distinct commitment favored IDs. It ranks eligible units by the
number of commitments with both bridge types.
"""
import argparse
import json
import sqlite3
from collections import Counter, defaultdict
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
DATA = ROOT / ".data" / "brazil-procurement-recon"


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--min-both", type=int, default=50)
    parser.add_argument("--min-invoice-rows", type=int, default=20)
    parser.add_argument("--min-favored-ids", type=int, default=50)
    args = parser.parse_args()
    db = sqlite3.connect(DATA / "profile.sqlite")
    liq = {row[0] for row in db.execute('SELECT DISTINCT "Código Empenho" FROM liq_bridge')}
    payments = defaultdict(set)
    for code, doc in db.execute('SELECT "Código Empenho","Código Pagamento" FROM pag_bridge'):
        payments[code].add(doc)
    invoice_rows = Counter(row[0] for row in db.execute('SELECT "Código Pagamento" FROM faturas'))
    units = defaultdict(lambda: {"codes": set(), "both": set(), "payments": set(), "favored": set(), "name": ""})
    for code, ug, name, favored in db.execute('SELECT "Código Empenho","Código Unidade Gestora","Unidade Gestora","Código Favorecido" FROM empenho'):
        unit = units[ug]
        unit["name"] = name
        unit["codes"].add(code)
        unit["payments"].update(payments.get(code, ()))
        if favored:
            unit["favored"].add(favored)
        if code in liq and code in payments:
            unit["both"].add(code)
    ranking = []
    for ug, unit in units.items():
        rec = {
            "ug": ug,
            "name": unit["name"],
            "commitments": len(unit["codes"]),
            "commitments_with_both": len(unit["both"]),
            "payment_documents": len(unit["payments"]),
            "invoice_list_rows": sum(invoice_rows[p] for p in unit["payments"]),
            "distinct_favored_ids": len(unit["favored"]),
        }
        rec["eligible"] = rec["commitments_with_both"] >= args.min_both and rec["invoice_list_rows"] >= args.min_invoice_rows and rec["distinct_favored_ids"] >= args.min_favored_ids
        ranking.append(rec)
    ranking.sort(key=lambda r: (r["eligible"], r["commitments_with_both"], r["invoice_list_rows"], r["distinct_favored_ids"]), reverse=True)
    result = {"thresholds": vars(args), "eligible_count": sum(r["eligible"] for r in ranking), "selected": next((r for r in ranking if r["eligible"]), None), "top_eligible": [r for r in ranking if r["eligible"]][:20]}
    (DATA / "selection.json").write_text(json.dumps(result, ensure_ascii=False, indent=2), encoding="utf-8")
    print(json.dumps(result, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
