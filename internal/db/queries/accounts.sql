-- name: CreateAccount :one
INSERT INTO accounts (organization_id, code, name, type, parent_id) VALUES (?, ?, ?, ?, ?) RETURNING *;

-- name: GetAccount :one
SELECT * FROM accounts WHERE organization_id = ? AND id = ?;

-- name: ListAccounts :many
SELECT * FROM accounts WHERE organization_id = ? ORDER BY code;

-- name: AccountHasChildren :one
SELECT EXISTS (SELECT 1 FROM accounts WHERE organization_id = ? AND parent_id = ?);
