-- +goose Up
-- Roles come from IdP groups configured in the environment
-- (DEN_ADMIN_GROUPS, DEN_MEMBER_GROUPS, DEN_OPS_GROUPS); nothing ever read
-- this table. See docs/adr/0007-roles-from-idp-groups.md.
DROP TABLE IF EXISTS role_assignments;

-- The IdP groups from the person's last sign-in to Den. Den uses them to
-- decide which modules a person may use (portal tiles, and which people a
-- paired module may ask about).
ALTER TABLE users ADD COLUMN groups TEXT[] NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE users DROP COLUMN groups;

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
