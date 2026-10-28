#!/usr/bin/env python3
"""Select and describe a coherent UG slice from measured full-key joins.

Usage: python3 research/brazil-procurement-recon/cohort.py --ug 114623
"""
import argparse
import json
import sqlite3
from collections import Counter, defaultdict
from datetime import datetime, timezone
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
DATA = ROOT / ".data" / "brazil-procurement-recon"
DB = DATA / "profile.sqlite"


def rows(db, table, cols):
    expr = ",".join('"' + c + '"' for c in cols)
    return db.execute("SELECT " + expr + " FROM " + table)


def composite_edge(db, left, right, cols):
    lc = Counter(tuple(row) for row in rows(db, left, cols))
    rc = Counter(tuple(row) for row in rows(db, right, cols))
    valid = lambda k: all(v is not None and str(v).strip() for v in k)
    out = {"left": left, "right": right, "keys": cols}
    for side, counter, other in [("left", lc, rc), ("right", rc, lc)]:
        out[side + "_rows"] = sum(counter.values())
        out[side + "_distinct_keys"] = sum(valid(k) for k in counter)
        out[side + "_null_rows"] = sum(n for k, n in counter.items() if not valid(k))
        out[side + "_matched_rows"] = sum(n for k, n in counter.items() if valid(k) and k in other)
        out[side + "_cardinality"] = {
            "one": sum(n == 1 for k, n in counter.items() if valid(k)),
            "many": sum(n > 1 for k, n in counter.items() if valid(k)),
        }
        out[side + "_unmatched_examples"] = [list(k) for k in counter if valid(k) and k not in other][:3]
    out["matched_distinct_keys"] = sum(valid(k) and k in rc for k in lc)
    out["matched_examples"] = [list(k) for k in lc if valid(k) and k in rc][:3]
    return out


def unit_summary(db, ug):
    commitments = list(db.execute('SELECT "Código Empenho","Id Empenho","Código Unidade Gestora","Unidade Gestora","Código Gestão","Código Favorecido","Favorecido","Data Emissão",day FROM empenho WHERE "Código Unidade Gestora"=?', (ug,)))
    codes = {r[0] for r in commitments}
    ids = {r[1] for r in commitments}
    liq_by_doc = defaultdict(set)
    pag_by_doc = defaultdict(set)
    liq_by_e = defaultdict(set)
    pag_by_e = defaultdict(set)
    all_liq_by_doc = defaultdict(set)
    all_pag_by_doc = defaultdict(set)
    for doc, code in rows(db, "liq_bridge", ["Código Liquidação", "Código Empenho"]):
        all_liq_by_doc[doc].add(code)
        if code in codes:
            liq_by_doc[doc].add(code)
            liq_by_e[code].add(doc)
    for doc, code in rows(db, "pag_bridge", ["Código Pagamento", "Código Empenho"]):
        all_pag_by_doc[doc].add(code)
        if code in codes:
            pag_by_doc[doc].add(code)
            pag_by_e[code].add(doc)
    liq_docs = set(liq_by_doc)
    pag_docs = set(pag_by_doc)
    invoice_by_doc = defaultdict(list)
    for doc, lst, seq, value in rows(db, "faturas", ["Código Pagamento", "Código Lista", "Código Sequência", "Valor Lançamento em R$"]):
        if doc in pag_docs:
            invoice_by_doc[doc].append([lst, seq, value])
    item_by_e = defaultdict(set)
    for eid, seq in rows(db, "item", ["Id Empenho", "Sequencial"]):
        if eid in ids:
            item_by_e[eid].add(seq)
    hist_by_item = defaultdict(list)
    for eid, seq, op, date in rows(db, "historico", ["Id Empenho", "Sequencial", "Tipo Operação", "Data Operação"]):
        if eid in ids:
            hist_by_item[(eid, seq)].append([op, date])
    liq_info = {r[0]: r[1:] for r in db.execute('SELECT "Código Liquidação","Data Emissão","Código Unidade Gestora","Código Gestão" FROM liquidacao') if r[0] in liq_docs}
    pag_info = {r[0]: r[1:] for r in db.execute('SELECT "Código Pagamento","Data Emissão","Código Unidade Gestora","Código Gestão","Código Tipo Documento" FROM pagamento') if r[0] in pag_docs}
    multi_liq = [{"document": d, "commitment_count": len(all_liq_by_doc[d]), "cohort_commitments": sorted(cs)[:3]} for d, cs in liq_by_doc.items() if len(all_liq_by_doc[d]) > 1][:5]
    multi_pag = [{"document": d, "commitment_count": len(all_pag_by_doc[d]), "cohort_commitments": sorted(cs)[:3]} for d, cs in pag_by_doc.items() if len(all_pag_by_doc[d]) > 1][:5]
    changes = [{"id_empenho": eid, "sequencial": seq, "operations": ops[:5]} for (eid, seq), ops in hist_by_item.items() if len(ops) > 1][:5]
    multi_invoices = [{"payment": d, "entries": entries[:5], "entry_count": len(entries)} for d, entries in invoice_by_doc.items() if len(entries) > 1][:5]
    return {
        "ug": ug,
        "ug_name": commitments[0][3] if commitments else None,
        "management_codes": sorted({r[4] for r in commitments}),
        "archive_days": sorted({r[8] for r in commitments}),
        "commitment_emission_min": min((r[7] for r in commitments), default=None),
        "commitment_emission_max": max((r[7] for r in commitments), default=None),
        "commitments": len(codes),
        "suppliers": len({r[5] for r in commitments if r[5]}),
        "items": sum(len(v) for v in item_by_e.values()),
        "item_history_rows": sum(len(v) for v in hist_by_item.values()),
        "commitments_with_liquidation": len(liq_by_e),
        "commitments_with_payment": len(pag_by_e),
        "commitments_with_both": len(set(liq_by_e) & set(pag_by_e)),
        "liquidation_documents": len(liq_docs),
        "payment_documents": len(pag_docs),
        "liquidation_documents_present": len(liq_info),
        "payment_documents_present": len(pag_info),
        "invoice_list_rows": sum(len(v) for v in invoice_by_doc.values()),
        "invoice_list_codes": len({v[0] for a in invoice_by_doc.values() for v in a}),
        "payments_with_invoice_list": len(invoice_by_doc),
        "multi_commitment_liquidation_examples": multi_liq,
        "multi_commitment_payment_examples": multi_pag,
        "item_change_examples": changes,
        "multiple_invoice_entry_examples": multi_invoices,
        "linked_example": next(({"commitment": k, "liquidation": sorted(liq_by_e[k])[0], "payment": sorted(pag_by_e[k])[0]} for k in sorted(set(liq_by_e) & set(pag_by_e))), None),
        "supplier_examples": [{"code": r[5], "name": r[6]} for r in commitments[:3]],
    }


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--ug", default="114623")
    args = parser.parse_args()
    db = sqlite3.connect(DB)
    out = {
        "created_utc": datetime.now(timezone.utc).isoformat(),
        "composite_edges": [
            composite_edge(db, "item", "historico", ["Id Empenho", "Sequencial"]),
            composite_edge(db, "item", "historico", ["Código Empenho", "Sequencial"]),
        ],
        "cohort": unit_summary(db, args.ug),
    }
    (DATA / "cohort.json").write_text(json.dumps(out, ensure_ascii=False, indent=2), encoding="utf-8")
    print(json.dumps(out, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
