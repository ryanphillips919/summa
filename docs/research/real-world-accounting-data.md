# Real-World Accounting Data Research

## Executive conclusion

No single public, inspectable corpus identified in this research contains a complete, explicitly linked procure-to-pay (P2P), order-to-cash (O2C), bank-settlement, and general-ledger history for one operating company. Summa should use **separate real reference datasets** for requirement discovery and a **deterministic synthetic company** for complete linked correctness tests. The sources below belong to different organizations and systems; their records must never be combined into a fictitious company history.

The current primary portfolio is UADY's accounting dossiers, Brazil's IBGE Paraná procurement/expenditure cohort, and Travis County's payment details. BPI Challenge 2019 and SAP SALT complement those three with process variation and sales master-data structure. This portfolio informs the ledger model; it is not a proposal to implement a full ERP.

## Evaluation standard

A relationship is **verified** only when published data supplies an explicit identifier, bridge row, documented join path, or a source document placing records in the same transaction packet. Similar names, dates, amounts, descriptions, or nearby records are hypotheses, not verified joins. Dossier-level continuity differs from normalized row-level allocation. An event named “clear invoice” is process evidence, not by itself a bank transaction or balanced journal.

For each source, distinguish what it publishes, what joins within one organization, and which lifecycle edge remains absent. Links below point to publisher or official dataset pages where found. This document compares published evidence and the supplied research inventory; it does **not** claim Summa downloaded or loaded every dataset.

## Primary and complementary sources

| Source | Organization / data shape | Verified process coverage | Why it is valuable | Important gaps | Recommended Summa role |
|---|---|---|---|---|---|
| [UADY Escuela Preparatoria Uno, Mexico: acquisition dossier](https://transparencia.uady.mx/sitios/prepa1/documentos_publicos/2025/1ER%20TRIMESTRE%20%28ENERO-MARZO%29/DOCUMENTOS%20COMPROBATORIOS/ADQUISICIONES/IMPRESORA.pdf?Mobile=1&Source=%2Fsitios%2Fprepa1%2F_layouts%2F15%2Fmobile%2Fviewa.aspx%3FList%3D1d1fe7ab-a019-41c5-b8cf-7dd1c9fb1e3f%26View%3D36890a59-b9d8-4483-83df-d1ea3e808e12%26RootFolder%3D%252Fsitios%252Fprepa1%252Fdocumentos_publicos%252F2025%252F1ER%2BTRIMESTRE%2B%28ENERO-MARZO%29%252FDOCUMENTOS%2BCOMPROBATORIOS%252FADQUISICIONES%26wdFCCState%3D1) | One operating unit; public PDF transaction dossiers | Within a dossier: PO/purchase case → supplier invoice → receipt/asset acceptance → AP accrual → payment/bank confirmation → AP-clearing journal | Real source documents and balanced debit/credit entries show AP and bank-settlement semantics in one packet | PDF packets, limited scale; no O2C or normalized invoice-to-payment allocation table. Continuity is dossier-level. | Accounting document, AP, bank-settlement, and GL semantics |
| [Brazil federal expenditure](https://portaldatransparencia.gov.br/download-de-dados/despesas/) and [Contratos.gov.br API](https://contratos.comprasnet.gov.br/api/docs), scoped to IBGE Paraná | One federal managing-unit cohort within national public data | Contract ID → item/invoice/commitment IDs in the contract API; commitment code → item/history; liquidation and payment documents → impacted-commitment bridge rows in [official dictionaries](https://portaldatransparencia.gov.br/dicionario-de-dados/empenhos) | Large-scale real P2P, changes, many-to-many document relationships, and explicit source keys | A shared commitment does **not** prove which payment settled a particular liquidation or contract invoice; no conventional JE lines or O2C. Daily partitions can hide earlier commitments. | Primary procurement, acceptance, obligation, and history corpus |
| [Travis County VPR/VPD](https://tctransparency.traviscountytx.gov/Help/WhatIsVpr) and [payment details](https://tctransparency.traviscountytx.gov/vpd) | One county AP/payment system | Vendor → check/ACH → invoice and/or PO application → applied detail with fund, department, and expense category | Publisher describes repeated detail rows for a single payment and splits across funds/departments | Only paid invoices appear; no receipt/acceptance, invoice-line detail, accrual, or GL journal chain | Invoice-payment allocation and split/batched settlement benchmark |
| [BPI Challenge 2019](https://research.tue.nl/en/datasets/bpi-challenge-2019/) | Anonymized multinational SAP P2P event history, keyed by purchase-document item | Vendor/PO item → goods-receipt and invoice-receipt events → invoice-clearing events | Real process variants, invoice-before-receipt, blocks, rework, partial and repeated flows | Event log rather than a complete document graph; receipt-to-invoice allocation can be ambiguous; no O2C, bank evidence, or GL lines | P2P exception and process benchmark |
| [SAP SALT](https://huggingface.co/datasets/SAP/SALT/blob/main/README.md) | One anonymized enterprise sales dataset with linked customer, address, sales-document, and item tables | Customer/address → sales document → sales-item lines in the published linked-table design | Real sales/customer/master-data structure, useful counterweight to P2P-only sources | Stops before delivery, billing, cash receipt, AR, and GL. [Dataset files are gated](https://huggingface.co/datasets/SAP/SALT/tree/main); access terms must be met before testing rows. | O2C/master-data structural reference |

The Brazil graph needs careful labeling. The official liquidation and payment impacted-commitment bridges support **two branches from a commitment**, not a liquidation → payment edge. Contratos.gov.br invoice commitment IDs can resolve to API commitments, and unit + gestão + document number can form an exact CGU commitment code. Neither that common code nor a CGU payment-list row establishes that a specific contract invoice was settled by a specific payment.

## Other useful partial sources

| Source | Explicitly useful evidence | Missing link / limit |
|---|---|---|
| [NYCHA Checkbook](https://www.checkbooknyc.com/nycha_spending/transactions/vendor_inv/70145/datasource/checkbook_nycha/widget/inv_contract_id/po_num_inv/PO1103416/agg_type/PO/tcode/PO/newwindow) | Oracle-derived vendor, invoice, line/distribution, and check views distinguish invoice lines from accounting distributions. | No public receipt/acceptance or full conventional journal chain. |
| [Ukraine Prozorro](https://prozorro.gov.ua/en) + [E-data](https://edata.gov.ua/) | Supplier, item, contract, and Treasury-payment evidence across public procurement and finance systems. | Cross-system supplier/contract/payment similarity is not invoice-to-payment allocation without explicit keys. |
| [Cook County Check Register](https://datacatalog.cookcountyil.gov/d/4h27-5s9x) | County payment and invoice details can show check-to-invoice membership. | No verified PO/receipt/GL chain; per-invoice allocation amounts require field-level validation. |
| [Los Angeles Checkbook/procurement data](https://controllerdata.lacity.org/Purchasing/LA_PROCUREMENT/v5c4-aqci) | Payment, invoice number, PO, fund, and account can co-occur on published rows. | Co-occurrence is not a normalized document-header or balanced journal graph. |
| [NYC HPD open-market-order invoices](https://catalog.data.gov/dataset/invoices-for-open-market-order-omo-charges) | Work-order and contractor-invoice references for service purchasing. | Weak receipt/acceptance and GL linkage; payment references need record-level validation. |
| [Olist Brazilian e-commerce](https://www.kaggle.com/datasets/olistbr/brazilian-ecommerce) | Customer/order/product/seller/payment behavior in a marketplace. | Not ordinary customer invoice, AR, cash-receipt, and GL history. |
| [UCI Online Retail II](https://archive.ics.uci.edu/dataset/502/online+retail+ii) | Customer/product/invoice-line and cancellation behavior. | No order, fulfillment, payment allocation, AR, or GL. |
| [Romanian invoice-clearing corpus](https://data.mendeley.com/datasets/28rbmvwsm9/1) | Pseudonymous debtor/creditor/invoice obligations and working-capital network behavior. | Many firms rather than one operating company; no P2P/O2C source documents or actual settlement/GL chain. Check data-use terms before adoption. |
| [PKDD'99 Berka Financial](https://relational.fel.cvut.cz/dataset/Financial) | Linked bank accounts, customers, loans, and transactions stress relational ingestion. | Banking data, not an operating company's P2P/O2C bookkeeping. |
| [Transparent Utah](https://transparent.utah.gov/entities) | Vendor, account, organizational-dimension, and financial-transaction/correction behavior. | No verified invoice → PO → receipt workflow. |
| [BPI Challenge 2020](https://icpmconference.org/2020/bpi-challenge/) | Approval, rework, and reimbursement workflow events. | University travel reimbursement, not supplier-bill/receipt/GL data. |
| [Austin Finance Online](https://financeonline.austintexas.gov/afo/account_services/search/) | PO commodity lines and purchasing/payment references. | No verified invoice/receipt/GL lifecycle join. |
| Chicago FMPS contracts/payments | Contract/PO → voucher/payment references. | Invoice/receipt/posting layers missing from the research inventory. |
| Baton Rouge PO/contract data | Requisition, PO/contract header and line structure. | No clean invoice/payment connection. |
| Greensboro PO/invoice spend | PO/invoice amounts and timing. | Insufficient linked lifecycle history. |
| Finnish procurement spend | Invoice/bookkeeping-style procurement reporting. | Too few explicit relationship keys for workflow reconstruction. |
| Czech Ministry of Defence invoices | Supplier invoice, items, order/contract references, paid fields. | No verified payment-document ID, receipt, or GL link. |
| Other city/state checkbooks | Vendor, agency, category, and payment reporting. | Usually aggregated or disconnected from source documents. |

Hospital billing logs can illustrate status events, but underlying services, payments, and accounting links were not established. Enron/FERC/bankruptcy records and Panama Papers are fragmented or cross-company document corpora, not ordinary ERP history. SEC filings and market data are aggregate reporting references, not transaction-level lifecycle evidence.

## Near-misses and design references

| Reference | What it teaches | Why it is not an adoptable real corpus here |
|---|---|---|
| [ECE Group Services SAP P2P extraction study](https://www.alessandroberti.it/new_papers/2023_Berti_Interconnected.pdf) | Requisition, PO, receipt, invoice, FI/payment objects, many-to-many relations, and reversals in a mature SAP topology. | The production rows behind the study were not released as a reusable transaction corpus. |
| [University of Toledo Banner ODS metadata](https://reports.utoledo.edu/metadata/metadata92/INVOICE_ODS_target.html), including [receipt-item](https://reports.utoledo.edu/metadata/metadata92/RECEIVED_ITEM_ODS_target.html) and [PO](https://reports.utoledo.edu/metadata/metadata92/PURCHASE_ORDER_ODS_target.html) views | Schema vocabulary for PO/items, receiving, invoice/items/distributions, checks, and posting history. | Public schemas and training material, not an export of actual linked historical rows. |
| DataPaid | Claimed licensed AP/AR, approvals, payments, reconciliation, and ERP workflow data could be a later procurement option. | No inspectable sample schema, stable IDs, or rows established in this research; marketing claims are not verified dataset evidence. |
| Real SAP journal-entry anomaly studies | Show that GL-level enterprise data can exist and can test journal anomalies. | No released source-document graph joining their entries to the P2P lifecycle was established. |
| [SAP procure-to-pay documentation](https://help.sap.com/doc/50587a11a60b4aed8b2e934b79cf554a/2405/en-US/Procure_to_Pay_Services_EN.pdf), UiPath extraction specifications, and municipal ERP manuals | Explain mature object names, posting concepts, and possible joins. | Documentation and extraction recipes, not one company's published transaction history. |
| EY Academic Resource Center | Potential educational finance/ETL material. | Access-controlled and no complete, inspectable graph verified. |

## Explicitly synthetic or provenance-limited material

| Source | Useful role | Classification limit |
|---|---|---|
| Summa deterministic synthetic company | Complete O2C + P2P + GL ground truth, invariants, seeded exceptions, and performance tests. | All assumptions and transactions are authored by Summa. |
| [Charles River Accounting Dataset](https://charlesriver.accountinganalyticshub.com/docs/technical-guide) | Broad connected teaching schema and fixture ideas. | Generated fictional company, not observed history. |
| [Zenodo simulated OCEL P2P/O2C](https://zenodo.org/records/21977764) and [synthetic relational/OCEL examples](https://zenodo.org/records/22079246) | Object/event modeling and extraction tests. | Explicit simulations. |
| ERPsim datasets | ERP-shaped activity and classroom workflow examples. | Simulated organizations and transactions. |
| GitHub mirror labeled “real SAP O2C” | Possible adversarial process-log fixture after independent review. | Provenance is insufficient to cite as trusted real company history. |

## Recommendation for Summa's first ledger/demo scope

| Reference | Use |
|---|---|
| UADY | Accounting document, AP, bank-settlement, and GL semantics |
| Brazil / IBGE Paraná | Procurement, acceptance, obligation, and history complexity |
| Travis County | Invoice-payment allocation behavior |
| BPI Challenge 2019 | P2P exception and process behavior |
| SAP SALT | Customer, sales, and master-data structure |
| Deterministic synthetic company | Complete linked ground truth and controlled tests |

Keep the first ledger/demo centered on balanced, immutable posted entries, a chart of accounts, trial balance, reversal/correction behavior, and visible source/provenance references. Use a small deterministic fixture to exercise those invariants. When document workflows are added, represent invoices, receipts, payments, and journal entries as distinct objects, and allow one-to-many/many-to-many allocations where a source proves them. Preserve the distinction between a documented edge and an inferred association in both data and UI.

Do **not** expand the first demo into contract management, full purchasing, billing, AR/AP automation, bank reconciliation, or a universal cross-dataset ERP import. Those need separate verified source keys, workflow rules, and tests. In particular, do not infer Brazil invoice settlement from a shared commitment or treat SALT sales items as completed O2C cash cycles. The portfolio should challenge the model's assumptions without dictating premature product scope.
