-- +goose Up
-- The initial migration seeded a "Dev Admin" with the owner role under the
-- dev: issuer. Owners are never demoted by the group sync, so the seed would
-- stay an owner in every installation. Dev mode provisions its users on
-- first request, so nothing needs the seed.
UPDATE branding_profiles SET created_by = NULL WHERE created_by = '00000000-0000-0000-0000-00000000d0a1';
UPDATE branding_assets   SET uploaded_by = NULL WHERE uploaded_by = '00000000-0000-0000-0000-00000000d0a1';
UPDATE pairing_tokens    SET created_by = NULL WHERE created_by = '00000000-0000-0000-0000-00000000d0a1';
DELETE FROM users WHERE id = '00000000-0000-0000-0000-00000000d0a1';

-- +goose Down
INSERT INTO users (id, email, display_name)
VALUES ('00000000-0000-0000-0000-00000000d0a1', 'dev-admin@den.local', 'Dev Admin')
ON CONFLICT (id) DO NOTHING;
