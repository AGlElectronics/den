-- name: GetLatestBrandingProfile :one
SELECT * FROM branding_profiles
 WHERE organization_id = $1
 ORDER BY revision DESC
 LIMIT 1;

-- name: InsertBrandingProfile :one
INSERT INTO branding_profiles (organization_id, revision, theme, typography, copy, created_by)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ListBrandingAssets :many
SELECT * FROM branding_assets
 WHERE organization_id = $1
 ORDER BY role, created_at DESC;

-- name: GetBrandingAsset :one
SELECT * FROM branding_assets WHERE id = $1;

-- name: InsertBrandingAsset :one
INSERT INTO branding_assets (organization_id, role, storage_bucket, storage_key, content_type, size_bytes, uploaded_by)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: DeleteBrandingAsset :exec
DELETE FROM branding_assets WHERE id = $1;
