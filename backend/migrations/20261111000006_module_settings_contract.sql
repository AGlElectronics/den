-- +goose Up
-- Module settings follow the 1.0 contract (docs: concepts/module-settings):
-- the Finance seller lives under finance.seller.{legal_name,address,vat_id},
-- Stash's Lost & Found under stash.lost_found with the modes kept_after_con
-- and deleted_after_closing, and retention holds day counts only.
UPDATE module_settings
   SET data = (data - 'finance') || jsonb_build_object('finance', jsonb_build_object('seller', jsonb_strip_nulls(jsonb_build_object(
         'legal_name', data->'finance'->'seller_name',
         'address',    data->'finance'->'seller_address',
         'vat_id',     data->'finance'->'vat_number'))))
 WHERE data ? 'finance' AND NOT (data->'finance' ? 'seller');

UPDATE module_settings
   SET data = jsonb_set(data, '{stash}', ((data->'stash') - 'lost_and_found') || jsonb_build_object('lost_found', jsonb_strip_nulls(jsonb_build_object(
         'mode', CASE data->'stash'->'lost_and_found'->>'mode'
                   WHEN 'days_after_con' THEN 'kept_after_con'
                   WHEN 'hours_after_closing' THEN 'deleted_after_closing'
                 END,
         'days',  data->'stash'->'lost_and_found'->'days',
         'hours', data->'stash'->'lost_and_found'->'hours'))))
 WHERE data->'stash' ? 'lost_and_found';

UPDATE module_settings
   SET data = jsonb_set(data, '{retention}', (data->'retention') - 'pending_legal_review' - 'backup_note')
 WHERE data ? 'retention';

-- +goose Down
-- The old key names are not restored; settings saved after the upgrade
-- keep the contract's names.
SELECT 1;
