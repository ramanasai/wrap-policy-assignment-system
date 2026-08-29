-- 0003_extended_categories.up.sql
-- Adds the policy categories the problem statement names but the initial
-- migration didn't seed: work schedules, shift policies, holiday calendars.
-- employment_type gains the "hourly" value (the "Hourly US W-2" example).

BEGIN;

INSERT INTO policy_category (id, display_name, cardinality, resolution_strategy, default_priority) VALUES
    ('work_schedule',      'Work Schedule',      'single', 'priority_rank',       35),
    ('shift_policy',       'Shift Policy',       'single', 'priority_rank',       25),
    ('holiday_calendar',   'Holiday Calendar',   'single', 'priority_rank',       30);

UPDATE attribute_definition
SET enum_values = '["full_time","contractor","intern","hourly"]'::jsonb
WHERE key = 'employment_type';

COMMIT;