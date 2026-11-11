-- +goose Up
-- internal_url: the address Den uses for server-to-server calls to the
-- module (role catalog, portal to-dos), for example its in-cluster
-- address. Empty means base_url.
ALTER TABLE modules ADD COLUMN internal_url TEXT NOT NULL DEFAULT '';

-- portal_visibility: who sees the module on the portal and may be asked
-- about by it. role_holders = people with one of the groups in the module's
-- role catalog; everyone = every member (for example Learn, which has no
-- learner role).
ALTER TABLE modules ADD COLUMN portal_visibility TEXT NOT NULL DEFAULT 'role_holders'
    CHECK (portal_visibility IN ('role_holders', 'everyone'));

-- database_name is only set when Den created the module's database; only
-- then may Den drop it.
COMMENT ON COLUMN modules.database_name IS 'Set only when Den created the database; only such databases can be dropped by Den.';

-- +goose Down
COMMENT ON COLUMN modules.database_name IS NULL;
ALTER TABLE modules DROP COLUMN portal_visibility;
ALTER TABLE modules DROP COLUMN internal_url;
