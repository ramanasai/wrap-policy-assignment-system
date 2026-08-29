-- facts.sql — bitemporal employee fact queries
--
-- The as-of query is the heart of the "second clock" model: it returns the
-- attribute snapshot a resolver needs, honoring (a) the valid-time window and
-- (b) post-correction visibility (latest recorded_at wins among overlapping
-- valid ranges for the same attribute).

-- name: GetEmployeeFactsAsOf :many
SELECT DISTINCT ON (fe.attribute_key)
       fe.attribute_key,
       fe.value,
       fe.recorded_at
FROM fact_event fe
WHERE fe.employee_id = sqlc.arg('employee_id')
  AND fe.valid_range @> sqlc.arg('as_of')::date
  AND fe.superseded_by IS NULL
ORDER BY fe.attribute_key, fe.valid_range DESC, fe.recorded_at DESC;

-- name: GetEmployeeFactsAsOfAt :many
-- Post-correction view as of a PROCESSING time (replaying history as it was
-- known at moment T): superseded_by IS NULL OR superseded later than T.
SELECT DISTINCT ON (fe.attribute_key)
       fe.attribute_key,
       fe.value,
       fe.recorded_at
FROM fact_event fe
WHERE fe.employee_id = sqlc.arg('employee_id')
  AND fe.valid_range @> sqlc.arg('as_of')::date
  AND (fe.superseded_by IS NULL OR fe.recorded_at <= sqlc.arg('processing_time')::timestamptz)
ORDER BY fe.attribute_key, fe.valid_range DESC, fe.recorded_at DESC;

-- name: InsertFactEvent :one
INSERT INTO fact_event (
    employee_id, attribute_key, value, valid_range, trigger
) VALUES (
    sqlc.arg('employee_id'), sqlc.arg('attribute_key'), sqlc.arg('value'),
    sqlc.arg('valid_range')::daterange, sqlc.arg('trigger')::text
)
RETURNING *;

-- name: SupersedeFactEvents :execrows
-- Mark all current (non-superseded) facts for this employee+attribute as
-- superseded by the new event. Runs in the same tx as InsertFactEvent.
UPDATE fact_event
SET superseded_by = sqlc.arg('superseded_by')::bigint
WHERE employee_id = sqlc.arg('employee_id')
  AND attribute_key = sqlc.arg('attribute_key')
  AND superseded_by IS NULL;
