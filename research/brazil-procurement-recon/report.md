# Brazil federal procurement and expenditure reconciliation

## Verdict

**A defensible transaction-level graph exists for a bounded federal slice, but a complete procure-to-payment chain does not.** Real CGU records link commitments to item and item-history rows, liquidation documents to the commitments they affect, payment documents to the commitments they affect, and payments to SIAFI invoice-list entries. Public Contratos.gov.br responses add explicit contract, contract-item, invoice, and commitment references. The missing public edge is **which payment settled which liquidation or which Contratos.gov.br invoice**. A shared commitment is insufficient to assert either relationship.

This conclusion uses ten official daily expenditure ZIPs dated **14–23 October 2026**, retrieved on 28 October 2026 UTC (27 October in Denver), plus a live 40-contract API probe and four detailed contracts. Raw responses, hashes, exact URLs, row counts, and all observed CSV headers are recorded in [sources.md](sources.md). The ignored local evidence is in `.data/brazil-procurement-recon/`. No synthetic rows enter the results.

## Strongest proven graph

```text
Contratos.gov.br contract id ── contract_id ──> contract item id
                 │                              ↑
                 ├── contract_id ──> invoice ──┘ dados_item_faturado.id_item_contrato
                 └── contract_id ──> API commitment id
                                      ↑
invoice ── dados_empenho.id_empenho ─┘
API commitment ── UG + gestão + numero ──> CGU Código Empenho
CGU Empenho ── Código Empenho ──> ItemEmpenho
ItemEmpenho ── (Código Empenho, Sequencial) ──> ItemEmpenhoHistorico
CGU Liquidação ── Código Liquidação ──> Liquidação_EmpenhosImpactados
CGU Pagamento ── Código Pagamento ──> Pagamento_EmpenhosImpactados
CGU Pagamento ── Código Pagamento ──> Pagamento_ListaFaturas
both impacted-commitment bridges ── Código Empenho ──> CGU Empenho
```

The graph intentionally has no direct liquidation-to-payment or contract-invoice-to-CGU-payment arrow. The CGU list file has `Código Lista` and `Código Sequência`, not the Contratos.gov.br invoice `id` or `numero`. Similar amounts, dates, supplier names, and descriptions were not used as edges. The CGU payment `Favorecido` can be a bank or intermediary rather than the commitment supplier.

The [CGU commitment dictionary](https://portaldatransparencia.gov.br/dicionario-de-dados/empenhos) defines the full SIAFI code as six UG digits, five gestão digits, and eleven document characters. All identifiers stayed as text; shortened document numbers were never used across units. The [liquidation](https://portaldatransparencia.gov.br/dicionario-de-dados/liquidacao) and [payment](https://portaldatransparencia.gov.br/dicionario-de-dados/pagamentos) dictionaries explicitly permit one document to affect multiple commitments.

## Measured CGU joins

The table reports row matches using an existence join, so a repeated bridge key does not multiply source rows. “Distinct” counts nonblank join keys on each side. Null or blank join-key rows were **zero on both sides for every edge**. Cardinality is the observed key-level relationship; exact one/many counts follow below.

| Left → right; key | Source rows L / R | Distinct keys L / R | Matched rows L / R | Match % L / R | Observed cardinality |
|---|---:|---:|---:|---:|---|
| Empenho → ItemEmpenho; `Código Empenho` | 96,796 / 176,745 | 96,796 / 96,900 | 96,742 / 176,578 | 99.94 / 99.91 | one to many |
| ItemEmpenho → ItemEmpenhoHistorico; `(Código Empenho, Sequencial)` | 176,745 / 445,646 | 176,745 / 176,532 | 176,519 / 445,631 | 99.87 / 99.997 | one to many |
| Liquidação → impacted commitments; `Código Liquidação` | 208,693 / 239,029 | 208,693 / 209,291 | 208,693 / 238,299 | 100 / 99.69 | one to many |
| Liquidation bridge → Empenho; `Código Empenho` | 239,029 / 96,796 | 110,531 / 96,796 | 71,870 / 25,102 | 30.07 / 25.93 | many to one |
| Pagamento → impacted commitments; `Código Pagamento` | 296,131 / 307,240 | 296,131 / 284,738 | 284,131 / 306,532 | 95.95 / 99.77 | one to many |
| Payment bridge → Empenho; `Código Empenho` | 307,240 / 96,796 | 123,304 / 96,796 | 66,291 / 19,468 | 21.58 / 20.11 | many to one |
| Pagamento → ListaFaturas; `Código Pagamento` | 296,131 / 18,475 | 296,131 / 15,186 | 15,186 / 18,475 | 5.13 / 100 | one to many |

Among matching-key candidates, 19,117 commitment keys have multiple item rows; 60,684 item keys have multiple history rows. The liquidation bridge repeats 18,628 document keys and 20,016 commitment keys. The payment bridge repeats 13,838 document keys and 44,226 commitment keys. The invoice list repeats 791 payment keys; one observed payment, `195016112012026OB000231`, has 200 list entries. These are row cardinalities, not proof of individual settlement allocations.

### Coverage and unmatched evidence

The portal states that an updated commitment appears in the partition of its **latest update**, with its old partition row removed. A payment on one day can therefore point to a commitment in an earlier daily ZIP. For the **same fixed 21–23 October bridge rows**, adding 14–20 October commitments raised liquidation-bridge row matches from **17,940/92,400 (19.42%)** to **35,279/92,400 (38.18%)**, and payment-bridge matches from **13,922/98,121 (14.19%)** to **25,459/98,121 (25.95%)**. The ten-day table above uses all ten days on both sides. Its remaining bridge-to-commitment misses are **coverage unknown**, not proven broken foreign keys: 156,980 unmatched liquidation-bridge rows and 225,911 unmatched payment-bridge rows reference a 2026 commitment code, and older years also occur. A full historical partition scan is needed before a genuine orphan rate can be stated.

Limited real examples and plausible explanations:

| Edge | Matched key | Unmatched key and side | Interpretation |
|---|---|---|---|
| Empenho → Item | `110001000012026NE000003` | `130141000012023NE800042` (Empenho); `113206115042018NE008681` (Item) | Small parent/item asymmetry; older records or publication timing may differ. |
| Item → history | `154215152782021NE000056`, sequence `1` | `154215152782016NE000542`, sequence `1` (Item); `250110000012021NE000035`, sequence `1` (History) | History is nearly complete, but some item rows have no history and some history rows have no item in this window. |
| Liquidação → bridge | `110001000012026NS020312` | `153052152262026NS017299` (bridge) | All sampled liquidation documents have bridge rows; 730 bridge rows lack their parent in the window. |
| Liquidation bridge → Empenho | `110001000012026NE000006` | `110001000012025NE001119` (bridge) | Commitment partition can predate the sample. |
| Pagamento → bridge | `110001000012026DF802435` | `110001000012026OB007847` (payment); `153052152262026OB007030` (bridge) | 12,000 payment documents lack a bridge row: 9,724 say `Extraorçamentário=SIM` and 2,276 say `NÃO`. The latter needs source-specific investigation. |
| Payment bridge → Empenho | `110001000012026NE000003` | `110001000012025NE001258` (bridge) | Older commitment partitions are outside this bounded sample. |
| Pagamento → ListaFaturas | `110001000012026OB007887` | `110001000012026DF802435` (payment) | Invoice lists are optional; all 18,475 list rows find a sampled payment. |

The history CSV has **445,646 rows but only 440,375 distinct full eight-field values**. Identical rows occur inside a single daily file and there is no event ID. Do not silently deduplicate them or interpret row count as a guaranteed operation count. The exact full `(Código Empenho, Sequencial)` item key is unique; `(Id Empenho, Sequencial)` is not unique in this sample (176,585 keys for 176,745 item rows).

## Best coherent slice: IBGE Paraná

The [cohort-ranking script](select_cohort.py) requires at least 50 commitments with both bridge types, 20 invoice-list rows, and 50 distinct commitment favored IDs, then ranks by linked commitments. Seven units qualify; **UG 114623, gestão 11301, SUPERINTENDENCIA ESTADUAL DO IBGE NO PARANA**, under IBGE, ranks first with 133 commitments linked to both stages. In the ten-day archive window, this unit has **181 distinct commitment codes**, **105 distinct commitment favored IDs**, **243 item rows**, **1,171 item-history rows**, **209 linked liquidation documents**, and **301 linked payment documents**. Of its commitments, **136** connect to a liquidation bridge, **138** to a payment bridge, and **133** to both. All 209 and 301 linked documents have parent rows in the downloaded window. There are **32 CGU invoice-list rows, 32 list codes, and 32 payments with a list**. The commitment rows in this slice were published on 14–16 and 19–23 October; their emission dates are 14–23 October. “Supplier” here means a distinct `Código Favorecido` on commitment rows and includes noncommercial favored entities.

Observed multi-commitment documents include liquidation `114623113012026NS007244` impacting commitments `114623113012026NE000467` and `114623113012026NE001161`, and liquidation `114623113012026NS007266` impacting `...NE000176` and `...NE000214`. Payment `114623113012026OB002925` and liquidation `114623113012026NS007511` each reference three commitment codes, including `114623113012026NE000215`. Another cohort commitment, `114623113012026NE000033` (`Id Empenho 565732699`), has item changes: sequence `1` includes `INCLUSAO` on 22 January 2026 and `REFORCO` entries on 23 February and 25 March. These are item changes present in the October updated partition.

The same payment `...OB002925` has CGU invoice-list code `114623113012026LF000494`, sequence `1`, and launch value R$ 16,499.01. The commitment and liquidation show COPEL DISTRIBUICAO S.A. as favored entity, while that payment’s favored entity is BANCO DO BRASIL SA. The official bridge, not favored-name equality, establishes the payment-to-commitment edge. This example still does **not** prove that `...OB002925` settled `...NS007511`. No sampled payment within UG 114623 has multiple CGU `ListaFaturas` rows; multiple contract invoices are present in the API sample below.

## Contratos.gov.br live sample

The [OpenAPI schema](https://contratos.comprasnet.gov.br/docs/api-docs.json) and real responses were inspected. The public `/api/contrato/ug/114623` request returned **193 active contract records**. The 40 most recent by `data_assinatura` yielded **57 API commitment rows** across `/api/contrato/{id}/empenhos`; **20 rows in 18 contracts** exactly matched a CGU `Código Empenho` in the ten-day sample after concatenating the API's string `unidade_gestora + gestao + numero`. For instance, contract **974259**, number **00025/2026**, has API commitment id **15279972** and `114623 + 11301 + 2026NE000804 = 114623113012026NE000804`. This exact CGU code has one sampled liquidation document and one sampled payment document through their bridge rows. No name or amount matching was used.

Four contracts with such matches were probed in detail:

| Contract id | Contract items | API commitments | API invoices | Invoice→commitment references | References matching CGU commitments | References with CGU liquidation/payment bridges |
|---:|---:|---:|---:|---:|---:|---:|
| 974259 | 1 | 1 | 2 | 2 | 2 | 2 / 2 |
| 979199 | 1 | 1 | 3 | 3 | 3 | 3 / 3 |
| 854161 | 1 | 2 | 6 | 9 | 9 | 9 / 9 |
| 735931 | 1 | 4 | 21 | 21 | 17 | 17 / 17 |
| **Total** | **4** | **8** | **32** | **35** | **31** | **31 / 31** |

All **32** `dados_item_faturado.id_item_contrato` references resolve to the four returned contract-item IDs. Invoice `dados_empenho.id_empenho` resolves to an API commitment ID for all 35 references. The 31 CGU matches represent 28 distinct invoices; the four other references concern older commitment codes outside the window. An observed multi-commitment invoice is contract 854161 invoice id **2421346** (`numero 1736952`), which references API commitment IDs **14488218** and **15208517**, resolving to full CGU codes `114623113012026NE000407` and `114623113012026NE000696`. All four endpoints—contract, item, commitment, invoice—returned HTTP 200 for each detailed contract. Invoice `numero` and `sfadrao_id` did not provide an explicit match to the CGU payment `ListaFaturas` identifiers in this sample.

The documented `/api/v1/execucoes/contratos/{id}` service/supply-order and `/api/v1/contrato/apropriacao/consultar/{id}` appropriation endpoints returned **HTTP 401**, `Authorization Token not found`, for each of the four IDs. The two documented bank-order lookups (`/api/v1/ordembancaria/consultar/1` and `/api/v1/ordembancaria/consultar/instrumento_cobranca/1`) and `/api/v1/contrato/ug/114623` also returned 401. The probe value `1` tests access only and is not claimed to be a valid appropriation or instrument ID. Without authenticated responses, no service-order, appropriation, or bank-order cardinality or cross-source settlement match can be measured. Exact request paths and response metadata appear in [sources.md](sources.md).

## Reproducible join logic

The loader retains every CSV field as `TEXT`, uses Latin-1 and `;`, and records the source day. The [acquisition](acquire.py), [profile](profile.py), [coverage](coverage.py), [cohort ranking](select_cohort.py), [cohort detail](cohort.py), [API probe](api_probe.py), [API detail](api_detail.py), and [source rendering](render_sources.py) scripts reproduce this report's measurements from current official responses. An existence join prevents one-to-many multiplication:

```sql
SELECT COUNT(*) AS matched_payment_bridge_rows
FROM pag_bridge AS b
WHERE EXISTS (
  SELECT 1 FROM empenho AS e
  WHERE e."Código Empenho" = b."Código Empenho"
);
```

The item-to-history edge requires **both** fields:

```sql
SELECT i."Código Empenho", i."Sequencial", h."Tipo Operação", h."Data Operação"
FROM item AS i
JOIN historico AS h
  ON h."Código Empenho" = i."Código Empenho"
 AND h."Sequencial" = i."Sequencial";
```

For the contract API, the exact cross-source key is constructed as `str(unidade_gestora) + str(gestao) + numero`; the API `dados_empenho.id_empenho` points to the contract commitment's `id`. Case differences in older API document strings were not silently normalized.

## Recommendation for Summa

Build an **evidence-labeled, managing-unit-scoped graph** with source keys and archive provenance on every edge. Support the proven commitment, item/history, liquidation-impact, payment-impact, contract-item, and contract-invoice relationships. Present liquidation and payment as separate branches from a commitment, and label CGU `ListaFaturas` as a payment list entry rather than a validated supplier invoice. Do not create a unified invoice-to-settlement timeline or company history from shared commitments, favored names, amounts, or dates.

A production-quality historical dataset needs automated full partition coverage for relevant commitment years, snapshot/update handling, and authenticated Contratos.gov.br access or an official invoice/payment bridge before settlement can be asserted. The measured ten-day slice is sufficient to validate the schema and a coherent unit-level graph; its low bridge-to-commitment rates measure **bounded archive coverage**, not system-wide integrity.
