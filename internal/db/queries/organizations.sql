-- Queries for organizations and users.

-- CreateOrganization adds a company.
-- name: CreateOrganization :one
INSERT INTO organizations (name) VALUES (?) RETURNING *;

-- name: GetOrganization :one
SELECT * FROM organizations WHERE id = ?;

-- CreateUser adds a person. Emails must be unique.
-- name: CreateUser :one
INSERT INTO users (email, name) VALUES (?, ?) RETURNING *;
