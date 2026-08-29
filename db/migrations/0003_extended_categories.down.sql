BEGIN;

UPDATE attribute_definition
SET enum_values = '["full_time","contractor","intern"]'::jsonb
WHERE key = 'employment_type';

DELETE FROM policy_category WHERE id IN ('work_schedule', 'shift_policy', 'holiday_calendar');

COMMIT;