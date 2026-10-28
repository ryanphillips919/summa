#!/usr/bin/env python3
"""Reproducible bounded Contratos.gov.br API probe; raw JSON stays ignored.

Usage: python3 research/brazil-procurement-recon/api_probe.py --ug 114623 --limit 40
"""
import argparse
import concurrent.futures
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
    name = hashlib.sha256(path.encode()).hexdigest()[:16] + ".json"
    (OUT / name).write_bytes(body)
    rec = {
        "path": path,
        "url": response.url,
        "retrieved_utc": datetime.now(timezone.utc).isoformat(),
        "status": response.status_code,
        "bytes": len(body),
        "sha256": hashlib.sha256(body).hexdigest(),
        "body_path": str((OUT / name).relative_to(ROOT)),
    }
    try:
        rec["json"] = response.json()
    except ValueError:
        rec["body_excerpt"] = body[:200].decode("utf-8", errors="replace")
    return rec


def compact(rec):
    return {k: v for k, v in rec.items() if k != "json"}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--ug", default="114623")
    parser.add_argument("--limit", type=int, default=40)
    args = parser.parse_args()
    OUT.mkdir(parents=True, exist_ok=True)
    docs = fetch("/docs/api-docs.json")
    listing = fetch(f"/api/contrato/ug/{args.ug}")
    contracts = listing.get("json", [])
    if not isinstance(contracts, list):
        raise RuntimeError(f"Contract listing failed: {compact(listing)}")
    chosen = sorted(contracts, key=lambda c: c.get("data_assinatura") or "", reverse=True)[:args.limit]
    paths = [f"/api/contrato/{c['id']}/empenhos" for c in chosen]
    with concurrent.futures.ThreadPoolExecutor(max_workers=4) as pool:
        commitment_responses = list(pool.map(fetch, paths))
    db = sqlite3.connect(DATA / "profile.sqlite")
    cgu_codes = {r[0] for r in db.execute('SELECT "Código Empenho" FROM empenho WHERE "Código Unidade Gestora"=?', (args.ug,))}
    matches = []
    for contract, rec in zip(chosen, commitment_responses):
        response_rows = rec.get("json")
        if not isinstance(response_rows, list):
            continue
        for row in response_rows:
            full = str(row.get("unidade_gestora", "")) + str(row.get("gestao", "")) + str(row.get("numero", ""))
            if full in cgu_codes:
                matches.append({"contract_id": contract["id"], "contract_number": contract["numero"], "api_commitment_id": row.get("id"), "full_code": full})
    detail_ids = list(dict.fromkeys(m["contract_id"] for m in matches))[:3]
    for contract in chosen:
        if len(detail_ids) >= 3:
            break
        if contract["id"] not in detail_ids:
            detail_ids.append(contract["id"])
    details = []
    for contract_id in detail_ids:
        for suffix in [f"/api/contrato/id/{contract_id}", f"/api/contrato/{contract_id}/itens", f"/api/contrato/{contract_id}/faturas", f"/api/v1/execucoes/contratos/{contract_id}", f"/api/v1/contrato/apropriacao/consultar/{contract_id}"]:
            details.append(fetch(suffix))
    auth = [fetch(f"/api/v1/ordembancaria/consultar/1"), fetch(f"/api/v1/ordembancaria/consultar/instrumento_cobranca/1"), fetch(f"/api/v1/contrato/ug/{args.ug}")]
    summary = {
        "run_utc": datetime.now(timezone.utc).isoformat(),
        "ug": args.ug,
        "limit": args.limit,
        "docs": compact(docs),
        "listing": compact(listing),
        "listed_contracts": len(contracts),
        "sampled_contracts": [{"id": c["id"], "numero": c["numero"], "data_assinatura": c.get("data_assinatura")} for c in chosen],
        "commitment_responses": [compact(r) for r in commitment_responses],
        "api_commitment_rows": sum(len(r.get("json", [])) for r in commitment_responses if isinstance(r.get("json"), list)),
        "exact_cgu_matches": matches,
        "detail_responses": [compact(r) for r in details],
        "auth_responses": [compact(r) for r in auth],
    }
    (DATA / "api_probe.json").write_text(json.dumps(summary, ensure_ascii=False, indent=2), encoding="utf-8")
    print(json.dumps({k: v for k, v in summary.items() if k not in ("commitment_responses", "detail_responses", "auth_responses", "sampled_contracts")}, ensure_ascii=False))


if __name__ == "__main__":
    main()
