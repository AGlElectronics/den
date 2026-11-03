-- Den MVP initial schema.
--
-- Scope: organizations, users + external_identities, memberships, modules,
-- pairing tokens & module credentials, branding profiles & assets, and a
-- flat audit log. Nothing module-specific (inventory, events, HR) belongs
-- here — see docs/adr/0005-no-shared-postgres.md.

-- +goose Up
-- +goose StatementBegin
CREATE EXTENSION IF NOT EXISTS pgcrypto;
-- +goose StatementEnd

-- ----------------------------------------------------------------------------
-- ORGANIZATIONS
-- ----------------------------------------------------------------------------

CREATE TABLE organizations (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug          TEXT NOT NULL UNIQUE,
    display_name  TEXT NOT NULL,
    legal_name    TEXT NOT NULL DEFAULT '',
    website_url   TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ----------------------------------------------------------------------------
-- USERS & IDENTITIES
-- ----------------------------------------------------------------------------

CREATE TABLE users (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email         TEXT,
    display_name  TEXT NOT NULL,
    is_disabled   BOOLEAN NOT NULL DEFAULT FALSE,
    last_login_at TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX users_email_lower_idx ON users (lower(email));

-- (issuer, subject) together are unique; neither alone is. Dev auth uses
-- issuer='dev:' and subject=<dev user name>.
CREATE TABLE external_identities (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    issuer      TEXT NOT NULL,
    subject     TEXT NOT NULL,
    email       TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (issuer, subject)
);

CREATE INDEX external_identities_user_idx ON external_identities(user_id);

-- ----------------------------------------------------------------------------
-- ORGANIZATION MEMBERSHIP
-- ----------------------------------------------------------------------------

CREATE TABLE organization_memberships (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role            TEXT NOT NULL CHECK (role IN ('owner','admin','member')),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (organization_id, user_id)
);

CREATE INDEX org_memberships_user_idx ON organization_memberships(user_id);

-- OIDC group -> org role bindings. Multiple issuers may define the same
-- group name independently, so (organization_id, issuer, group, role) is
-- what's unique.
CREATE TABLE role_assignments (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    issuer          TEXT NOT NULL,
    group_name      TEXT NOT NULL,
    role            TEXT NOT NULL CHECK (role IN ('owner','admin','member')),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (organization_id, issuer, group_name, role)
);

CREATE INDEX role_assignments_org_idx ON role_assignments(organization_id);

-- ----------------------------------------------------------------------------
-- MODULES REGISTRY
-- ----------------------------------------------------------------------------

CREATE TABLE modules (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    slug            TEXT NOT NULL,
    display_name    TEXT NOT NULL,
    base_url        TEXT NOT NULL DEFAULT '',
    environment     TEXT NOT NULL DEFAULT 'production',
    enabled         BOOLEAN NOT NULL DEFAULT TRUE,
    status          TEXT NOT NULL DEFAULT 'unpaired'
                     CHECK (status IN ('unpaired','paired','degraded','disabled')),
    manifest        JSONB NOT NULL DEFAULT '{}'::jsonb,
    last_seen_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (organization_id, slug, environment)
);

CREATE INDEX modules_org_idx ON modules(organization_id);

-- ----------------------------------------------------------------------------
-- PAIRING TOKENS + MODULE CREDENTIALS
-- ----------------------------------------------------------------------------

-- One-shot pairing tokens. Only the SHA-256 hash of the raw token is
-- stored. Consumed and expired rows are kept for audit.
CREATE TABLE pairing_tokens (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    module_id   UUID NOT NULL REFERENCES modules(id) ON DELETE CASCADE,
    token_hash  TEXT NOT NULL UNIQUE,
    expires_at  TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    created_by  UUID REFERENCES users(id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX pairing_tokens_module_idx ON pairing_tokens(module_id);

-- Long-lived bearer credentials for mated modules talking back to Den.
CREATE TABLE module_credentials (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    module_id       UUID NOT NULL REFERENCES modules(id) ON DELETE CASCADE,
    credential_hash TEXT NOT NULL UNIQUE,
    label           TEXT NOT NULL DEFAULT '',
    last_used_at    TIMESTAMPTZ,
    revoked_at      TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX module_credentials_module_idx ON module_credentials(module_id);

-- ----------------------------------------------------------------------------
-- BRANDING PROFILES + ASSETS
-- ----------------------------------------------------------------------------

CREATE TABLE branding_profiles (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    revision        INTEGER NOT NULL,
    theme           JSONB NOT NULL DEFAULT '{}'::jsonb,
    typography      JSONB NOT NULL DEFAULT '{}'::jsonb,
    copy            JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_by      UUID REFERENCES users(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (organization_id, revision)
);

CREATE INDEX branding_profiles_org_rev_idx ON branding_profiles(organization_id, revision DESC);

CREATE TABLE branding_assets (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    role            TEXT NOT NULL,
    storage_bucket  TEXT NOT NULL,
    storage_key     TEXT NOT NULL,
    content_type    TEXT NOT NULL,
    size_bytes      BIGINT NOT NULL CHECK (size_bytes >= 0),
    uploaded_by     UUID REFERENCES users(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (storage_bucket, storage_key)
);

CREATE INDEX branding_assets_org_idx ON branding_assets(organization_id);
CREATE INDEX branding_assets_role_idx ON branding_assets(organization_id, role);

-- ----------------------------------------------------------------------------
-- AUDIT EVENTS
-- ----------------------------------------------------------------------------

CREATE TABLE audit_events (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID REFERENCES organizations(id) ON DELETE SET NULL,
    actor_user_id   UUID REFERENCES users(id)         ON DELETE SET NULL,
    action          TEXT NOT NULL,
    entity_type     TEXT NOT NULL,
    entity_id       UUID NOT NULL,
    before_json     JSONB,
    after_json      JSONB,
    note            TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX audit_events_entity_idx  ON audit_events(entity_type, entity_id);
CREATE INDEX audit_events_org_idx     ON audit_events(organization_id, created_at DESC);
CREATE INDEX audit_events_created_idx ON audit_events(created_at DESC);

-- ----------------------------------------------------------------------------
-- SEED
-- ----------------------------------------------------------------------------

-- One default organization so a fresh dev install isn't empty.
INSERT INTO organizations (id, slug, display_name)
VALUES ('00000000-0000-0000-0000-00000000d001', 'default', 'Default Organization');

-- Dev user + identity under the synthetic 'dev:' issuer.
INSERT INTO users (id, email, display_name)
VALUES ('00000000-0000-0000-0000-00000000d0a1', 'dev-admin@den.local', 'Dev Admin');

INSERT INTO external_identities (user_id, issuer, subject, email)
VALUES ('00000000-0000-0000-0000-00000000d0a1', 'dev:', 'dev-admin', 'dev-admin@den.local');

INSERT INTO organization_memberships (organization_id, user_id, role)
VALUES ('00000000-0000-0000-0000-00000000d001',
        '00000000-0000-0000-0000-00000000d0a1',
        'owner');

-- Starter branding profile so GET /branding/profile returns something.
INSERT INTO branding_profiles (organization_id, revision, theme, typography, copy, created_by)
VALUES (
    '00000000-0000-0000-0000-00000000d001',
    1,
    '{
        "primary":        "#e44763",
        "primary_hover":  "#c93a53",
        "accent":         "#f7d1d9",
        "background":     "#ffffff",
        "foreground":     "#241e20",
        "muted":          "#f7f4f5",
        "border":         "#e7dcdf"
    }'::jsonb,
    '{"heading_font": "Montserrat", "body_font": "Inter"}'::jsonb,
    '{"tagline": "Welcome to Den.", "legal_footer": ""}'::jsonb,
    '00000000-0000-0000-0000-00000000d0a1'
);

-- +goose Down
DROP TABLE IF EXISTS audit_events;
DROP TABLE IF EXISTS branding_assets;
DROP TABLE IF EXISTS branding_profiles;
DROP TABLE IF EXISTS module_credentials;
DROP TABLE IF EXISTS pairing_tokens;
DROP TABLE IF EXISTS modules;
DROP TABLE IF EXISTS role_assignments;
DROP TABLE IF EXISTS organization_memberships;
DROP TABLE IF EXISTS external_identities;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS organizations;
