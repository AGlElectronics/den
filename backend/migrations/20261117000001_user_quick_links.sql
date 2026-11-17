-- +goose Up
-- Personal Quick links for the Den portal home. Keyed by IdP identity like
-- user_preferences so the same person keeps them across devices.
CREATE TABLE user_quick_links (
    issuer     TEXT NOT NULL,
    subject    TEXT NOT NULL,
    links      JSONB NOT NULL DEFAULT '[]'::jsonb,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (issuer, subject)
);

-- +goose Down
DROP TABLE user_quick_links;
