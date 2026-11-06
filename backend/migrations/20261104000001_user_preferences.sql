-- +goose Up
-- Per-person UI preferences shared by every Con Services app. Keyed by the
-- IdP identity rather than users.id so a person who has only ever opened a
-- module (never Den) still gets one.
CREATE TABLE user_preferences (
    issuer     TEXT NOT NULL,
    subject    TEXT NOT NULL,
    theme      TEXT NOT NULL DEFAULT 'system' CHECK (theme IN ('system', 'light', 'dark')),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (issuer, subject)
);

-- +goose Down
DROP TABLE user_preferences;
