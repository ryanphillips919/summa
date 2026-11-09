-- name: CreateCustomer :one
INSERT INTO customers (organization_id, name, email) VALUES (?, ?, ?) RETURNING *;

-- name: ListCustomers :many
SELECT * FROM customers WHERE organization_id = ? ORDER BY name, id;
