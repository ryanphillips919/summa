-- Analytics result snapshots are written by Python and read by Go.

-- name: GetLatestAnalyticsRun :one
SELECT *
FROM analytics_runs
WHERE organization_id = ?
ORDER BY id DESC
LIMIT 1;

-- name: LatestTransactionID :one
SELECT CAST(COALESCE(MAX(id), 0) AS INTEGER) AS latest_transaction_id
FROM transactions
WHERE organization_id = ?;