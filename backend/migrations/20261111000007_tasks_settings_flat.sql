-- +goose Up
-- Tasks keys are flat under tasks.* (tasks.trash_days, tasks.due_soon_days, …).
-- A build that saved the older nested names (tasks.retention.*, tasks.reminders.due_soon_days)
-- is converted. A key already set at the top level wins.
UPDATE module_settings
   SET data = jsonb_set(
         data,
         '{tasks}',
         COALESCE((data -> 'tasks') -> 'retention', '{}'::jsonb)
         || CASE
              WHEN jsonb_typeof((data -> 'tasks') -> 'reminders') = 'object'
                   AND ((data -> 'tasks') -> 'reminders') ? 'due_soon_days'
              THEN jsonb_build_object('due_soon_days', ((data -> 'tasks') -> 'reminders') -> 'due_soon_days')
              ELSE '{}'::jsonb
            END
         || ((data -> 'tasks') - ARRAY['retention', 'reminders'])
       )
 WHERE (data -> 'tasks') ?| ARRAY['retention', 'reminders'];

-- +goose Down
-- The nested names are not restored.
SELECT 1;
