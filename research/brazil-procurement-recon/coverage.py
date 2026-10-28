#!/usr/bin/env python3
"""Measure fixed-row bridge coverage as commitment partition coverage expands.

Usage: python3 research/brazil-procurement-recon/coverage.py --fixed-from 20261021
"""
import argparse
import json
import sqlite3
from collections import Counter
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
DATA = ROOT / ".data" / "brazil-procurement-recon"


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--fixed-from", default="20261021", help="YYYYMMDD")
    args = parser.parse_args()
    db = sqlite3.connect(DATA / "profile.sqlite")
    out = {"fixed_from": args.fixed_from, "bridges": {}}
    for table in ("liq_bridge", "pag_bridge"):
        base = db.execute(f"SELECT count(*) FROM {table} WHERE day>=?", (args.fixed_from,)).fetchone()[0]
        fixed = db.execute(
            f'SELECT count(*) FROM {table} b WHERE b.day>=? AND EXISTS (SELECT 1 FROM empenho e WHERE e."Código Empenho"=b."Código Empenho" AND e.day>=?)',
            (args.fixed_from, args.fixed_from),
        ).fetchone()[0]
        expanded = db.execute(
            f'SELECT count(*) FROM {table} b WHERE b.day>=? AND EXISTS (SELECT 1 FROM empenho e WHERE e."Código Empenho"=b."Código Empenho")',
            (args.fixed_from,),
        ).fetchone()[0]
        unmatched_years = Counter(row[0][11:15] for row in db.execute(
            f'SELECT b."Código Empenho" FROM {table} b WHERE NOT EXISTS (SELECT 1 FROM empenho e WHERE e."Código Empenho"=b."Código Empenho")'
        ))
        out["bridges"][table] = {"fixed_bridge_rows": base, "matched_fixed_days": fixed, "matched_all_downloaded_days": expanded, "unmatched_commitment_years_all_days": dict(unmatched_years)}
    (DATA / "coverage.json").write_text(json.dumps(out, ensure_ascii=False, indent=2), encoding="utf-8")
    print(json.dumps(out, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
