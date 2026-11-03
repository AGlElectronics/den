-- name: GetOrganization :one
SELECT * FROM organizations WHERE id = $1;

-- name: GetOrganizationBySlug :one
SELECT * FROM organizations WHERE slug = $1;

-- name: ListOrganizations :many
SELECT * FROM organizations ORDER BY display_name;

-- name: InsertOrganization :one
INSERT INTO organizations (slug, display_name, legal_name, website_url)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: UpdateOrganization :one
UPDATE organizations
   SET display_name = $2,
       legal_name   = $3,
       website_url  = $4,
       updated_at   = now()
 WHERE id = $1
RETURNING *;
