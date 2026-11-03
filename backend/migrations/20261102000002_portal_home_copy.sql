-- +goose Up
-- Friendlier portal identity for the everyone-facing Den home.
UPDATE organizations
   SET display_name = 'Con Services'
 WHERE slug = 'default' AND display_name = 'Default Organization';

UPDATE branding_profiles
   SET copy = copy || '{"convention_name":"Con Services","tagline":"Your home for inventory, events, and crew tools.","welcome":"Welcome back"}'::jsonb
 WHERE organization_id = '00000000-0000-0000-0000-00000000d001'
   AND revision = (
     SELECT MAX(revision) FROM branding_profiles
      WHERE organization_id = '00000000-0000-0000-0000-00000000d001'
   );

-- +goose Down
UPDATE organizations
   SET display_name = 'Default Organization'
 WHERE slug = 'default' AND display_name = 'Con Services';
