#!/usr/bin/env python3
"""Render an auditable source inventory from ignored download/probe manifests."""
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
DATA = ROOT / ".data" / "brazil-procurement-recon"
DEST = Path(__file__).with_name("sources.md")
profile = json.loads((DATA / "profile.json").read_text(encoding="utf-8"))
manifest = {x["day"]: x for x in map(json.loads, (DATA / "raw" / "manifest.jsonl").read_text(encoding="utf-8").splitlines())}
probe = json.loads((DATA / "api_probe.json").read_text(encoding="utf-8"))
detail = json.loads((DATA / "api_detail.json").read_text(encoding="utf-8"))
lines = [
    "# Official sources and acquisition record",
    "",
    "## Primary sources",
    "",
    "- [CGU Portal da Transparência daily expenditure downloads](https://portaldatransparencia.gov.br/download-de-dados/despesas/). The page enumerated dates through 2026-10-23 at inspection.",
    "- [Empenho dictionary](https://portaldatransparencia.gov.br/dicionario-de-dados/empenhos), [Liquidação dictionary](https://portaldatransparencia.gov.br/dicionario-de-dados/liquidacao), and [Pagamento dictionary](https://portaldatransparencia.gov.br/dicionario-de-dados/pagamentos).",
    "- [Contratos.gov.br Swagger UI](https://contratos.comprasnet.gov.br/api/docs) and [OpenAPI JSON](https://contratos.comprasnet.gov.br/docs/api-docs.json).",
    "",
    "## Daily archives",
    "",
    "All times are UTC. Each exact request URL redirected to the shown official download host. Raw ZIPs, JSON responses, SQLite, and JSON profiles are under ignored `.data/brazil-procurement-recon/`.",
    "",
    "| Day | Exact request URL | Final ZIP URL | Retrieved UTC | Bytes | SHA-256 |",
    "|---|---|---|---|---:|---|",
]
for day in sorted(manifest):
    m = manifest[day]
    lines.append(f"| {day} | [{m['source_url']}]({m['source_url']}) | [{m['final_url']}]({m['final_url']}) | {m['retrieved_utc']} | {m['bytes']:,} | `{m['sha256']}` |")
lines += [
    "",
    "## Actual CSV member names and row counts",
    "",
    "Members are named `YYYYMMDD_Despesas_<suffix>.csv`. The official page says `ListaFatura` (singular), but the archive actually contains `Pagamento_ListaFaturas` (plural). The ZIP also contains other payment lists that were outside this study.",
    "",
    "| Day | Empenho | ItemEmpenho | ItemEmpenhoHistorico | Liquidacao | Liquidacao_EmpenhosImpactados | Pagamento | Pagamento_EmpenhosImpactados | Pagamento_ListaFaturas |",
    "|---|---:|---:|---:|---:|---:|---:|---:|---:|",
]
order = ["empenho", "item", "historico", "liquidacao", "liq_bridge", "pagamento", "pag_bridge", "faturas"]
for day in sorted(manifest):
    vals = [next(m["rows"] for m in profile["tables"][t]["members"] if m["day"] == day) for t in order]
    lines.append("| " + day + " | " + " | ".join(f"{n:,}" for n in vals) + " |")
lines += ["", "## Actual CSV headers", "", "Transcribed by the loader from the real Latin-1, semicolon-delimited ZIP members. All columns are loaded as text so leading zeroes survive.", ""]
for table in order:
    info = profile["tables"][table]
    lines += [f"### {table} ({info['rows']:,} rows; {len(info['headers'])} columns)", "", "```text", "; ".join(info["headers"]), "```", ""]
lines += [
    "## Candidate keys and dictionary check",
    "",
    "- `Empenho.Código Empenho` (96,796/96,796 distinct) and `Empenho.Id Empenho` (96,796/96,796) are unique in this window. The dictionary specifies full `Código Empenho` as six UG digits + five gestão digits + eleven document characters; the shortened code is not globally unique.",
    "- `ItemEmpenho.(Código Empenho, Sequencial)` is unique (176,745/176,745). `ItemEmpenhoHistorico` has no event ID; even the full eight-field record has 440,375 distinct values among 445,646 rows. Repeated rows cannot safely be collapsed into one event.",
    "- `Liquidacao.Código Liquidação` (208,693/208,693) and `Pagamento.Código Pagamento` (296,131/296,131) are unique. The official dictionaries describe these full UG + gestão + document codes, and their names match the CSV headers.",
    "- Bridge row candidates `(document code, Código Empenho, Código Natureza Despesa Completa, Subitem)` are unique in each bridge (239,029/239,029 liquidation; 307,240/307,240 payment). `(Código Pagamento, Código Lista, Código Sequência)` is unique for the invoice list (18,475/18,475).",
    "- Actual `Empenho` header uses `Código SubFunção`/`SubFunção`; the dictionary uses `Código Subfunção`/`Subfunção` casing. Join code uses the exact CSV spellings. The three document dictionaries describe parent documents; the download page names the bridge and list files but does not provide equally detailed field dictionaries for them.",
    "",
    "## Reproduction",
    "",
    "From the repository root, with Python 3:",
    "",
    "```bash",
    "python3 -m pip install -r research/brazil-procurement-recon/requirements.txt",
    "python3 research/brazil-procurement-recon/acquire.py 20261014 20261015 20261016 20261017 20261018 20261019 20261020 20261021 20261022 20261023",
    "python3 research/brazil-procurement-recon/profile.py",
    "python3 research/brazil-procurement-recon/coverage.py --fixed-from 20261021",
    "python3 research/brazil-procurement-recon/select_cohort.py",
    "python3 research/brazil-procurement-recon/cohort.py --ug 114623",
    "python3 research/brazil-procurement-recon/api_probe.py --ug 114623 --limit 40",
    "python3 research/brazil-procurement-recon/api_detail.py 974259 979199 854161 735931",
    "python3 research/brazil-procurement-recon/render_sources.py",
    "```",
    "",
    "Downloads are mutable daily partitions, so a later retrieval may have different bytes and row counts. Compare SHA-256 values above before comparing metrics.",
    "",
    "## Contratos.gov.br exact request paths",
    "",
    f"OpenAPI JSON: `{probe['docs']['url']}` at {probe['docs']['retrieved_utc']}, {probe['docs']['bytes']:,} bytes, SHA-256 `{probe['docs']['sha256']}`.",
    f"Public unit list: `{probe['listing']['url']}` at {probe['listing']['retrieved_utc']}, HTTP {probe['listing']['status']}, {probe['listing']['bytes']:,} bytes, SHA-256 `{probe['listing']['sha256']}`.",
    "",
    "The bounded probe requested `/api/contrato/{id}/empenhos` for the 40 most recent contracts by `data_assinatura` in the unit-list response. The detailed sample requested the following for contract IDs 974259, 979199, 854161, and 735931:",
    "",
    "- `/api/contrato/id/{id}`",
    "- `/api/contrato/{id}/itens`",
    "- `/api/contrato/{id}/empenhos`",
    "- `/api/contrato/{id}/faturas`",
    "- `/api/v1/execucoes/contratos/{id}`",
    "- `/api/v1/contrato/apropriacao/consultar/{id}`",
    "- `/api/v1/ordembancaria/consultar/1` and `/api/v1/ordembancaria/consultar/instrumento_cobranca/1` (access checks; 1 is not claimed to be a valid business ID).",
    "- `/api/v1/contrato/ug/114623` (access check).",
    "",
    "The exact 40 commitment paths, per-request UTC times, HTTP statuses, sizes, hashes, and ignored raw JSON paths are in `.data/brazil-procurement-recon/api_probe.json`; corresponding detail metadata is in `.data/brazil-procurement-recon/api_detail.json`. The public detail endpoints returned HTTP 200; tested v1 order, appropriation, and bank-order endpoints returned HTTP 401 with `Authorization Token not found`.",
]
DEST.write_text("\n".join(lines) + "\n", encoding="utf-8")
print(DEST)
