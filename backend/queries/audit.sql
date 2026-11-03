-- name: InsertAuditEvent :one
INSERT INTO audit_events (
    organization_id, actor_user_id, action,
    entity_type, entity_id, before_json, after_json, note
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: ListAuditEventsForOrganization :many
SELECT * FROM audit_events
 WHERE organization_id = $1
 ORDER BY created_at DESC
 LIMIT $2;
