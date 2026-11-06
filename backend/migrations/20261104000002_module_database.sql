-- +goose Up
-- The database the operator created for a module ("Add module" with a
-- database). Modules set up before Den managed databases leave it empty;
-- Den then assumes the database carries the module's slug.
ALTER TABLE modules ADD COLUMN database_name TEXT;

-- +goose Down
ALTER TABLE modules DROP COLUMN database_name;
