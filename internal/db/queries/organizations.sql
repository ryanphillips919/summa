-- name: CreateOrganization :one
INSERT INTO organizations (name) VALUES (?) RETURNING *;

-- name: CreateUser :one
INSERT INTO users (email, name) VALUES (?, ?) RETURNING *;

-- name: AddMembership :exec
INSERT INTO memberships (organization_id, user_id, role) VALUES (?, ?, ?);

-- name: GetMembershipRole :one
SELECT role FROM memberships WHERE organization_id = ? AND user_id = ?;
