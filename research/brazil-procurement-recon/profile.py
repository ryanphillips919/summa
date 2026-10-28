#!/usr/bin/env python3
"""Load official CSV members as text, then measure explicit-key join coverage.

Usage: python3 research/brazil-procurement-recon/profile.py
Writes ignored SQLite and JSON evidence under .data/brazil-procurement-recon/.
"""
import csv
import io
import json
import sqlite3
import sys
import zipfile
from collections import Counter
from datetime import datetime, timezone
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
DATA = ROOT / ".data" / "brazil-procurement-recon"
RAW = DATA / "raw"
DB = DATA / "profile.sqlite"
OUT = DATA / "profile.json"
SUFFIXES = {
    "Empenho": "empenho",
    "ItemEmpenho": "item",
    "ItemEmpenhoHistorico": "historico",
    "Liquidacao": "liquidacao",
    "Liquidacao_EmpenhosImpactados": "liq_bridge",
    "Pagamento": "pagamento",
    "Pagamento_EmpenhosImpactados": "pag_bridge",
    "Pagamento_ListaFaturas": "faturas",
}
KEYS = {
    "empenho": ["Código Empenho", "Id Empenho"],
    "item": ["Código Empenho", "Id Empenho", "Sequencial"],
    "historico": ["Código Empenho", "Id Empenho", "Sequencial"],
    "liquidacao": ["Código Liquidação"],
    "liq_bridge": ["Código Liquidação", "Código Empenho"],
    "pagamento": ["Código Pagamento"],
    "pag_bridge": ["Código Pagamento", "Código Empenho"],
    "faturas": ["Código Pagamento", "Código Lista", "Código Sequência"],
}


def q(name):
    return '"' + name.replace('"', '""') + '"'


def load():
    DB.unlink(missing_ok=True)
    db = sqlite3.connect(DB)
    db.execute("PRAGMA journal_mode=OFF")
    db.execute("PRAGMA synchronous=OFF")
    db.execute("PRAGMA temp_store=MEMORY")
    metadata = {"created_utc": datetime.now(timezone.utc).isoformat(), "archives": [], "tables": {}}
    for path in sorted(RAW.glob("*_Despesas.zip")):
        day = path.name[:8]
        print("loading", day, flush=True)
        with zipfile.ZipFile(path) as z:
            members = {n.split("_Despesas_", 1)[-1].removesuffix(".csv"): n for n in z.namelist() if "_Despesas_" in n and n.endswith(".csv")}
            metadata["archives"].append({"day": day, "path": str(path.relative_to(ROOT))})
            for suffix, table in SUFFIXES.items():
                member = members.get(suffix)
                if member is None:
                    raise ValueError(f"Missing {suffix} in {path}")
                with z.open(member) as binary:
                    stream = io.TextIOWrapper(binary, encoding="latin-1", newline="")
                    reader = csv.DictReader(stream, delimiter=";", quotechar='"')
                    fields = reader.fieldnames
                    if not fields or any(x is None for x in fields):
                        raise ValueError(f"Bad header {member}: {fields}")
                    info = metadata["tables"].setdefault(table, {"members": [], "headers": fields, "rows": 0, "sample": []})
                    if fields != info["headers"]:
                        raise ValueError(f"Header drift in {member}")
                    if len(info["members"]) == 0:
                        db.execute("CREATE TABLE " + q(table) + " (day TEXT, " + ", ".join(q(c) + " TEXT" for c in fields) + ")")
                    cols = ["day"] + fields
                    insert = "INSERT INTO " + q(table) + " VALUES (" + ",".join(["?"] * len(cols)) + ")"
                    batch = []
                    count = 0
                    for row in reader:
                        if None in row or any(k not in row for k in fields):
                            raise ValueError(f"Malformed row in {member} at {count}")
                        if len(info["sample"]) < 2:
                            info["sample"].append({k: row[k] for k in KEYS[table]})
                        batch.append([day] + [row[k] for k in fields])
                        count += 1
                        if len(batch) >= 2000:
                            db.executemany(insert, batch)
                            batch.clear()
                    if batch:
                        db.executemany(insert, batch)
                    info["rows"] += count
                    info["members"].append({"day": day, "member": member, "uncompressed_bytes": z.getinfo(member).file_size, "rows": count})
        db.commit()
    for table, keys in KEYS.items():
        for key in set(keys):
            db.execute("CREATE INDEX " + q("ix_" + table + "_" + key.replace(" ", "_")) + " ON " + q(table) + "(" + q(key) + ")")
    db.commit()
    return db, metadata


def edge(db, left, right, key):
    l, r, k = q(left), q(right), q(key)
    def scalar(sql):
        return db.execute(sql).fetchone()[0]
    out = {"left": left, "right": right, "key": key}
    for side, table in (("left", l), ("right", r)):
        out[side + "_rows"] = scalar(f"SELECT count(*) FROM {table}")
        out[side + "_distinct_nonnull"] = scalar(f"SELECT count(DISTINCT {k}) FROM {table} WHERE {k} IS NOT NULL AND trim({k}) <> ''")
        out[side + "_null_or_blank"] = scalar(f"SELECT count(*) FROM {table} WHERE {k} IS NULL OR trim({k}) = ''")
    out["left_matched_rows"] = scalar(f"SELECT count(*) FROM {l} a WHERE a.{k} IS NOT NULL AND trim(a.{k}) <> '' AND EXISTS (SELECT 1 FROM {r} b WHERE b.{k}=a.{k})")
    out["right_matched_rows"] = scalar(f"SELECT count(*) FROM {r} b WHERE b.{k} IS NOT NULL AND trim(b.{k}) <> '' AND EXISTS (SELECT 1 FROM {l} a WHERE a.{k}=b.{k})")
    out["matched_distinct_keys"] = scalar(f"SELECT count(DISTINCT a.{k}) FROM {l} a WHERE EXISTS (SELECT 1 FROM {r} b WHERE b.{k}=a.{k})")
    for side, table, other in (("left", l, r), ("right", r, l)):
        out[side + "_unmatched_examples"] = [x[0] for x in db.execute(f"SELECT a.{k} FROM {table} a WHERE a.{k} IS NOT NULL AND trim(a.{k})<>'' AND NOT EXISTS (SELECT 1 FROM {other} b WHERE b.{k}=a.{k}) LIMIT 3")]
    out["matched_examples"] = [x[0] for x in db.execute(f"SELECT DISTINCT a.{k} FROM {l} a WHERE EXISTS (SELECT 1 FROM {r} b WHERE b.{k}=a.{k}) LIMIT 3")]
    out["cardinality"] = {}
    for side, table in (("left", l), ("right", r)):
        out["cardinality"][side] = dict(db.execute(f"SELECT CASE WHEN n=1 THEN 'one' ELSE 'many' END, count(*) FROM (SELECT {k},count(*) n FROM {table} WHERE {k} IS NOT NULL AND trim({k})<>'' GROUP BY {k}) GROUP BY 1"))
    return out


def main():
    DATA.mkdir(parents=True, exist_ok=True)
    db, evidence = load()
    edges = [
        ("empenho", "item", "Código Empenho"),
        ("item", "historico", "Código Empenho"),
        ("item", "historico", "Id Empenho"),
        ("liquidacao", "liq_bridge", "Código Liquidação"),
        ("liq_bridge", "empenho", "Código Empenho"),
        ("pagamento", "pag_bridge", "Código Pagamento"),
        ("pag_bridge", "empenho", "Código Empenho"),
        ("pagamento", "faturas", "Código Pagamento"),
    ]
    evidence["edges"] = [edge(db, *args) for args in edges]
    OUT.write_text(json.dumps(evidence, ensure_ascii=False, indent=2), encoding="utf-8")
    print("wrote", OUT)


if __name__ == "__main__":
    main()
