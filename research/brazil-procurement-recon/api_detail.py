#!/usr/bin/env python3
"""Inspect selected contract details and compare explicit invoice/commitment IDs.

Usage: python3 research/brazil-procurement-recon/api_detail.py 974259 979199 854161 735931
"""
import argparse
import hashlib
import json
import sqlite3
from datetime import datetime, timezone
from pathlib import Path
import requests

ROOT = Path(__file__).resolve().parents[2]
DATA = ROOT / ".data" / "brazil-procurement-recon"
OUT = DATA / "api"
BASE = "https://contratos.comprasnet.gov.br"


def fetch(path):
    response = requests.get(BASE + path, timeout=(20, 45), headers={"Accept": "application/json"})
    body = response.content
    file = OUT / (hashlib.sha256(path.encode()).hexdigest()[:16] + ".json")
    file.write_bytes(body)
    try:
        parsed = response.json()
    except ValueError:
        parsed = None
    return parsed, {
        "path": path, "url": response.url,
        "retrieved_utc": datetime.now(timezone.utc).isoformat(),
        "status": response.status_code, "bytes": len(body),
        "sha256": hashlib.sha256(body).hexdigest(),
        "body_path": str(file.relative_to(ROOT)),
        "response_type": type(parsed).__name__,
        "response_rows": len(parsed) if isinstance(parsed, list) else None,
    }


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("contract_ids", type=int, nargs="+")
    args = parser.parse_args()
    OUT.mkdir(parents=True, exist_ok=True)
    db = sqlite3.connect(DATA / "profile.sqlite")
    all_e = {r[0] for r in db.execute('SELECT "Código Empenho" FROM empenho')}
    liq = {r[0] for r in db.execute('SELECT DISTINCT "Código Empenho" FROM liq_bridge')}
    pag = {r[0] for r in db.execute('SELECT DISTINCT "Código Empenho" FROM pag_bridge')}
    results = []
    for cid in args.contract_ids:
        endpoints = {
            "contract": f"/api/contrato/id/{cid}",
            "items": f"/api/contrato/{cid}/itens",
            "commitments": f"/api/contrato/{cid}/empenhos",
            "invoices": f"/api/contrato/{cid}/faturas",
            "orders": f"/api/v1/execucoes/contratos/{cid}",
            "appropriations": f"/api/v1/contrato/apropriacao/consultar/{cid}",
        }
        data = {}
        records = {}
        for name, path in endpoints.items():
            data[name], records[name] = fetch(path)
        id_to_code = {
            x["id"]: str(x.get("unidade_gestora", "")) + str(x.get("gestao", "")) + str(x.get("numero", ""))
            for x in data["commitments"] if isinstance(data["commitments"], list)
        }
        invoice_links = []
        for invoice in data["invoices"] if isinstance(data["invoices"], list) else []:
            for ref in invoice.get("dados_empenho", []):
                code = id_to_code.get(ref.get("id_empenho"))
                invoice_links.append({
                    "invoice_id": invoice.get("id"),
                    "invoice_number": invoice.get("numero"),
                    "api_commitment_id": ref.get("id_empenho"),
                    "full_code": code,
                    "in_cgu_commitment_sample": code in all_e if code else False,
                    "has_cgu_liquidation_bridge": code in liq if code else False,
                    "has_cgu_payment_bridge": code in pag if code else False,
                })
        results.append({
            "contract_id": cid, "requests": records,
            "api_commitment_rows": len(id_to_code),
            "invoice_rows": len(data["invoices"]) if isinstance(data["invoices"], list) else None,
            "invoice_commitment_refs": invoice_links,
            "contract_item_ids": [x.get("id") for x in data["items"]] if isinstance(data["items"], list) else [],
            "invoice_item_ref_count": sum(len(x.get("dados_item_faturado", [])) for x in data["invoices"]) if isinstance(data["invoices"], list) else None,
            "invoice_item_resolved_count": sum(ref.get("id_item_contrato") in {item.get("id") for item in data["items"]} for invoice in data["invoices"] for ref in invoice.get("dados_item_faturado", [])) if isinstance(data["invoices"], list) and isinstance(data["items"], list) else None,
        })
    bank = []
    for path in ["/api/v1/ordembancaria/consultar/1", "/api/v1/ordembancaria/consultar/instrumento_cobranca/1"]:
        _, rec = fetch(path)
        bank.append(rec)
    summary = {"run_utc": datetime.now(timezone.utc).isoformat(), "contracts": results, "bank_order_probes": bank}
    (DATA / "api_detail.json").write_text(json.dumps(summary, ensure_ascii=False, indent=2), encoding="utf-8")
    print(json.dumps(summary, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
