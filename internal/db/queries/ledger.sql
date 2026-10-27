-- Queries for the ledger: accounts, transactions and entries. sqlc turns
-- each one into a Go function named after "-- name:". ":one" returns a
-- single row, ":many" returns a list, ":exec" returns nothing.

-- CreateAccount adds an account to an organization's chart of accounts.
-- name: CreateAccount :one
INSERT INTO accounts (organization_id, name, type) VALUES (?, ?, ?) RETURNING *;

-- ListAccounts is an organization's chart of accounts, A to Z.
-- name: ListAccounts :many
SELECT * FROM accounts WHERE organization_id = ? ORDER BY name;

-- CreateTransaction saves a transaction's header. Posting is one
-- CreateTransaction plus one CreateEntry per line, all inside one database
-- transaction so it all saves or none of it does (the Go code does that).
-- name: CreateTransaction :one
INSERT INTO transactions (organization_id, date, description) VALUES (?, ?, ?) RETURNING *;

-- CreateEntry saves one line of a transaction.
-- name: CreateEntry :exec
INSERT INTO entries (transaction_id, account_id, direction, amount) VALUES (?, ?, ?, ?);

-- ListTransactions is the journal, newest first.
-- name: ListTransactions :many
SELECT * FROM transactions WHERE organization_id = ? ORDER BY date DESC, id DESC;

-- ListEntries returns the lines of one transaction.
-- name: ListEntries :many
SELECT * FROM entries WHERE transaction_id = ? ORDER BY id;

-- AccountTotals adds up every account's debits and credits. Balances are
-- never stored; they're always worked out from the entries like this, and
-- the Go code turns the two totals into a balance using the account type.
-- name: AccountTotals :many
SELECT a.id, a.name, a.type,
       CAST(COALESCE(SUM(CASE WHEN e.direction = 'debit'  THEN e.amount END), 0) AS INTEGER) AS debits,
       CAST(COALESCE(SUM(CASE WHEN e.direction = 'credit' THEN e.amount END), 0) AS INTEGER) AS credits
FROM accounts a
LEFT JOIN entries e ON e.account_id = a.id
WHERE a.organization_id = ?
GROUP BY a.id
ORDER BY a.name;
