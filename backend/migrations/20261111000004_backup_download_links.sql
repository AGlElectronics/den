-- +goose Up
-- One-time backup download links (Backups page → Download). Kept in the
-- database rather than in memory so any Den replica can serve a link.
-- Only the SHA-256 hash of the token is stored.
CREATE TABLE backup_download_links (
    token_hash  TEXT PRIMARY KEY,
    backup_key  TEXT NOT NULL,
    expires_at  TIMESTAMPTZ NOT NULL,
    used_at     TIMESTAMPTZ,
    created_by  UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX backup_download_links_expires_idx ON backup_download_links(expires_at);

-- +goose Down
DROP TABLE backup_download_links;
