-- name: CreateCustomer :one
INSERT INTO customers (organization_id, name, email) VALUES (?, ?, ?) RETURNING *;

-- name: ListCustomers :many
SELECT * FROM customers WHERE organization_id = ? ORDER BY name, id;

-- name: GetCustomer :one
SELECT * FROM customers
WHERE id = sqlc.arg(customer_id) AND organization_id = sqlc.arg(organization_id);
