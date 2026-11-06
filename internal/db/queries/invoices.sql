-- name: CreateInvoice :one
INSERT INTO invoices (organization_id, customer_id, number, issue_date, due_date)
VALUES (?, ?, ?, ?, ?) RETURNING *;

-- name: ListInvoices :many
SELECT * FROM invoices WHERE organization_id = ? ORDER BY issue_date DESC, id DESC;
