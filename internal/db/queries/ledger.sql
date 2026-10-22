-- name: InsertTransaction :one
INSERT INTO transactions (organization_id, occurred_on, description, counterparty_id, reverses_id, posted_by)
VALUES (?, ?, ?, ?, ?, ?) RETURNING *;

-- name: InsertEntry :exec
INSERT INTO entries (organization_id, transaction_id, account_id, direction, amount, memo)
VALUES (?, ?, ?, ?, ?, ?);

-- TransactionTotals is the balance check run before committing a posting.
-- name: TransactionTotals :one
SELECT count(*) AS entry_count,
       CAST(COALESCE(SUM(CASE WHEN direction = 'debit'  THEN amount END), 0) AS INTEGER) AS debits,
       CAST(COALESCE(SUM(CASE WHEN direction = 'credit' THEN amount END), 0) AS INTEGER) AS credits
FROM entries WHERE transaction_id = ?;

-- TrialBalance returns per-account debits and credits as of a date. Go turns
-- them into balances using the account type (see docs/ledger-rules.md).
-- name: TrialBalance :many
SELECT a.id, a.code, a.name, a.type,
       CAST(SUM(CASE WHEN e.direction = 'debit'  THEN e.amount ELSE 0 END) AS INTEGER) AS debits,
       CAST(SUM(CASE WHEN e.direction = 'credit' THEN e.amount ELSE 0 END) AS INTEGER) AS credits
FROM accounts a
JOIN entries e      ON e.organization_id = a.organization_id AND e.account_id = a.id
JOIN transactions t ON t.id = e.transaction_id
WHERE a.organization_id = sqlc.arg(organization_id) AND t.occurred_on <= sqlc.arg(as_of)
GROUP BY a.id ORDER BY a.code;

-- name: GetTransaction :one
SELECT * FROM transactions WHERE organization_id = ? AND id = ?;

-- name: ListTransactionEntries :many
SELECT * FROM entries WHERE organization_id = ? AND transaction_id = ? ORDER BY id;

-- AccountTotals returns one account's debits and credits as of a date.
-- name: AccountTotals :one
SELECT CAST(COALESCE(SUM(CASE WHEN e.direction = 'debit'  THEN e.amount END), 0) AS INTEGER) AS debits,
       CAST(COALESCE(SUM(CASE WHEN e.direction = 'credit' THEN e.amount END), 0) AS INTEGER) AS credits
FROM entries e
JOIN transactions t ON t.id = e.transaction_id
WHERE e.organization_id = sqlc.arg(organization_id)
  AND e.account_id = sqlc.arg(account_id)
  AND t.occurred_on <= sqlc.arg(as_of);

-- ListRecentTransactions is the journal: newest first, with who posted each
-- transaction and the reversal that undid it, if any.
-- name: ListRecentTransactions :many
SELECT t.id, t.occurred_on, t.description, t.reverses_id, t.posted_at,
       u.name AS posted_by_name,
       r.id AS reversed_by
FROM transactions t
JOIN users u ON u.id = t.posted_by
LEFT JOIN transactions r ON r.organization_id = t.organization_id AND r.reverses_id = t.id
WHERE t.organization_id = sqlc.arg(organization_id)
ORDER BY t.occurred_on DESC, t.id DESC
LIMIT sqlc.arg(max_rows);

-- name: ListEntriesForTransactions :many
SELECT e.transaction_id, e.direction, e.amount, e.memo,
       a.code AS account_code, a.name AS account_name
FROM entries e
JOIN accounts a ON a.organization_id = e.organization_id AND a.id = e.account_id
WHERE e.organization_id = sqlc.arg(organization_id)
  AND e.transaction_id IN (sqlc.slice(transaction_ids))
ORDER BY e.transaction_id, e.id;
