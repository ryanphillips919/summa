-- name: CreateInvoice :one
INSERT INTO invoices (organization_id, customer_id, number, issue_date, due_date)
VALUES (?, ?, ?, ?, ?) RETURNING *;

-- name: ListInvoices :many
SELECT * FROM invoices WHERE organization_id = ? ORDER BY issue_date DESC, id DESC;

-- GetInvoice returns one invoice in an organization.
-- name: GetInvoice :one
SELECT * FROM invoices
WHERE id = sqlc.arg(invoice_id)
  AND organization_id = sqlc.arg(organization_id);

-- MarkInvoicePosted links an invoice to its ledger transaction exactly once.
-- name: MarkInvoicePosted :one
UPDATE invoices
SET posted_transaction_id = sqlc.arg(transaction_id)
WHERE id = sqlc.arg(invoice_id)
  AND organization_id = sqlc.arg(organization_id)
  AND posted_transaction_id IS NULL
RETURNING *;
