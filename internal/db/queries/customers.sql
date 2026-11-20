-- name: CreateCustomer :one
INSERT INTO customers (organization_id, name, email) VALUES (?, ?, ?) RETURNING *;

-- name: ListCustomers :many
SELECT * FROM customers WHERE organization_id = ? ORDER BY name, id;

-- name: GetCustomer :one
SELECT * FROM customers WHERE organization_id = ? AND id = ?;

-- name: UpdateCustomer :one
UPDATE customers SET name = ?, email = ? WHERE organization_id = ? AND id = ? RETURNING *;
