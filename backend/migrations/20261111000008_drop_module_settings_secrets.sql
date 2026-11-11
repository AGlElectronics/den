-- +goose Up
-- The SMTP password is Den's secret (DEN_SMTP_PASSWORD or
-- DEN_SMTP_PASSWORD_FILE), not a database column. Any password stored
-- here is dropped; copy it into the secret before upgrading.
ALTER TABLE module_settings DROP COLUMN secrets;

-- +goose Down
-- The previous password cannot be restored.
ALTER TABLE module_settings ADD COLUMN secrets JSONB NOT NULL DEFAULT '{}'::jsonb;
