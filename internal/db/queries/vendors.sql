-- name: CreateVendor :one
INSERT INTO vendors (organization_id, name, email) VALUES (?, ?, ?) RETURNING *;

-- name: ListVendors :many
SELECT * FROM vendors WHERE organization_id = ? ORDER BY name, id;

-- name: UpdateVendor :one
UPDATE vendors SET name = sqlc.arg(name), email = sqlc.arg(email)
WHERE id = sqlc.arg(vendor_id) AND organization_id = sqlc.arg(organization_id)
RETURNING *;
