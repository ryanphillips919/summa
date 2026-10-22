-- name: CreateOrganization :one
INSERT INTO organizations (name) VALUES (?) RETURNING *;

-- name: CreateUser :one
INSERT INTO users (email, name) VALUES (?, ?) RETURNING *;

-- name: AddMembership :exec
INSERT INTO memberships (organization_id, user_id, role) VALUES (?, ?, ?);

-- name: GetMembershipRole :one
SELECT role FROM memberships WHERE organization_id = ? AND user_id = ?;

-- name: FirstOrganization :one
SELECT * FROM organizations ORDER BY id LIMIT 1;

-- name: ListMembers :many
SELECT u.id, u.name, u.email, m.role
FROM memberships m
JOIN users u ON u.id = m.user_id
WHERE m.organization_id = ?
ORDER BY u.id;
