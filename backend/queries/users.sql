-- name: GetUser :one
SELECT * FROM users WHERE id = $1;

-- name: GetUserByExternalIdentity :one
SELECT u.*
  FROM users u
  JOIN external_identities ei ON ei.user_id = u.id
 WHERE ei.issuer = $1 AND ei.subject = $2;

-- name: InsertUser :one
INSERT INTO users (email, display_name)
VALUES ($1, $2)
RETURNING *;

-- name: TouchUserLogin :exec
UPDATE users SET last_login_at = now(), updated_at = now() WHERE id = $1;

-- name: InsertExternalIdentity :one
INSERT INTO external_identities (user_id, issuer, subject, email)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: InsertOrganizationMembership :one
INSERT INTO organization_memberships (organization_id, user_id, role)
VALUES ($1, $2, $3)
ON CONFLICT (organization_id, user_id)
    DO UPDATE SET role = EXCLUDED.role, updated_at = now()
RETURNING *;

-- name: ListUserMemberships :many
SELECT m.*
  FROM organization_memberships m
 WHERE m.user_id = $1;
