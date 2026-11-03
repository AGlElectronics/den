-- name: InsertPairingToken :one
INSERT INTO pairing_tokens (module_id, token_hash, expires_at, created_by)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetPairingTokenByHash :one
SELECT * FROM pairing_tokens WHERE token_hash = $1;

-- name: ConsumePairingToken :exec
UPDATE pairing_tokens
   SET consumed_at = now()
 WHERE id = $1 AND consumed_at IS NULL;

-- name: InsertModuleCredential :one
INSERT INTO module_credentials (module_id, credential_hash, label)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetModuleCredentialByHash :one
SELECT * FROM module_credentials
 WHERE credential_hash = $1 AND revoked_at IS NULL;

-- name: TouchModuleCredential :exec
UPDATE module_credentials SET last_used_at = now() WHERE id = $1;

-- name: RevokeModuleCredential :exec
UPDATE module_credentials SET revoked_at = now() WHERE id = $1;
