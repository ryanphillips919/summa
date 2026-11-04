-- Queries for organizations and users.

-- CreateOrganization adds a company.
-- name: CreateOrganization :one
INSERT INTO organizations (name) VALUES (?) RETURNING *;

-- CreateUser adds a person. Emails must be unique.
-- name: CreateUser :one
INSERT INTO users (email, name) VALUES (?, ?) RETURNING *;
