-- name: ListModulesForOrganization :many
SELECT * FROM modules WHERE organization_id = $1 ORDER BY display_name;

-- name: GetModule :one
SELECT * FROM modules WHERE id = $1;

-- name: InsertModule :one
INSERT INTO modules (organization_id, slug, display_name, base_url, environment)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: UpdateModule :one
UPDATE modules
   SET display_name = $2,
       base_url     = $3,
       environment  = $4,
       enabled      = $5,
       updated_at   = now()
 WHERE id = $1
RETURNING *;

-- name: SetModuleStatus :exec
UPDATE modules
   SET status       = $2,
       manifest     = COALESCE($3::jsonb, manifest),
       last_seen_at = now(),
       updated_at   = now()
 WHERE id = $1;

-- name: DeleteModule :exec
DELETE FROM modules WHERE id = $1;
