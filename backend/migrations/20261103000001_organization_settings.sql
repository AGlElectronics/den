-- +goose Up
-- Organisation-wide settings (email domain, naming templates, …) that paired
-- modules read from Den instead of keeping their own copy.
CREATE TABLE organization_settings (
    organization_id UUID PRIMARY KEY REFERENCES organizations(id) ON DELETE CASCADE,
    data            JSONB NOT NULL DEFAULT '{}'::jsonb,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by      TEXT NOT NULL DEFAULT ''
);

-- +goose Down
DROP TABLE organization_settings;
