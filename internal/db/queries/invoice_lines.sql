-- CreateInvoiceLine adds a line only to an unposted invoice in the given organization.
-- name: CreateInvoiceLine :one
INSERT INTO invoice_lines (invoice_id, description, quantity, unit_price_cents)
SELECT invoices.id, sqlc.arg(description), sqlc.arg(quantity), sqlc.arg(unit_price_cents)
FROM invoices
WHERE invoices.id = sqlc.arg(invoice_id)
  AND invoices.organization_id = sqlc.arg(organization_id)
  AND invoices.posted_transaction_id IS NULL
RETURNING *;

-- ListInvoiceLines returns one organization's lines for one invoice.
-- name: ListInvoiceLines :many
SELECT l.* FROM invoice_lines l
JOIN invoices i ON i.id = l.invoice_id
WHERE i.organization_id = sqlc.arg(organization_id) AND i.id = sqlc.arg(invoice_id)
ORDER BY l.id;

-- InvoiceTotal calculates the total in cents, including zero for an empty invoice.
-- name: InvoiceTotal :one
SELECT CAST(COALESCE(SUM(l.quantity * l.unit_price_cents), 0) AS INTEGER) AS total_cents
FROM invoices i
LEFT JOIN invoice_lines l ON l.invoice_id = i.id
WHERE i.organization_id = sqlc.arg(organization_id) AND i.id = sqlc.arg(invoice_id)
GROUP BY i.id;
