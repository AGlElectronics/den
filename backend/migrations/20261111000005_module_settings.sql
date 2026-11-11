-- +goose Up
-- Settings Den hands to paired modules through the integration bundle
-- (retention, SMTP, Finance seller details, Stash Lost & Found, the Rally
-- submission site, identity links). One row per organisation. Secrets (the
-- SMTP password) live in their own column, which the admin API never returns.
CREATE TABLE module_settings (
    organization_id UUID PRIMARY KEY REFERENCES organizations(id) ON DELETE CASCADE,
    data            JSONB NOT NULL DEFAULT '{}'::jsonb,
    secrets         JSONB NOT NULL DEFAULT '{}'::jsonb,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by      UUID REFERENCES users(id) ON DELETE SET NULL
);

-- +goose Down
DROP TABLE module_settings;
