-- projection.sql — materialized assignment projection + sweeper comparisons.
-- The projection is a CACHE: rebuildable, never authoritative (ARCHITECTURE §5).

-- name: ReplaceAssignmentsForEmployeeCategory :exec
-- Delete the employee's current (open-ended) assignments for a category and
-- insert the freshly resolved winners. Historical closed intervals survive.
DELETE FROM assignment
WHERE employee_id = sqlc.arg('employee_id')
  AND category_id = sqlc.arg('category_id')
  AND upper_inf(valid_range);

-- name: InsertAssignment :exec
INSERT INTO assignment (
    employee_id, category_id, policy_id, policy_version_id, rule_id, valid_range, computed_at, trigger_event_id
) VALUES (
    sqlc.arg('employee_id'), sqlc.arg('category_id'), sqlc.arg('policy_id'),
    sqlc.arg('policy_version_id'), sqlc.arg('rule_id'),
    sqlc.arg('valid_range')::daterange, now(), sqlc.arg('trigger_event_id')
);

-- name: ReplaceShadowedMatches :exec
DELETE FROM shadowed_match
WHERE employee_id = sqlc.arg('employee_id')
  AND category_id = sqlc.arg('category_id');

-- name: InsertShadowedMatch :exec
INSERT INTO shadowed_match (employee_id, category_id, rule_id, by_rule_id, valid_range)
VALUES (
    sqlc.arg('employee_id'), sqlc.arg('category_id'), sqlc.arg('rule_id'),
    sqlc.arg('by_rule_id'), sqlc.arg('valid_range')::daterange
);

-- name: GetAssignedPolicies :many
-- Current (open-ended) assignments for the sweeper's expected-vs-actual check.
SELECT category_id, policy_id FROM assignment
WHERE employee_id = sqlc.arg('employee_id')
  AND upper_inf(valid_range);

-- ---------------------------------------------------------------------------
-- Scheduler queries — future-dated transitions that become effective today
-- ---------------------------------------------------------------------------

-- name: ListFactEventsStartingToday :many
SELECT fe.id, fe.employee_id, fe.attribute_key
FROM fact_event fe
WHERE lower(fe.valid_range) = sqlc.arg('today')::date
  AND fe.recorded_at < now()  -- future-dated when written; effective now
  AND fe.recorded_at < (now() - interval '1 minute');  -- not the write that just happened

-- name: ListRuleVersionsStartingToday :many
SELECT DISTINCT r.id AS rule_id, r.category_id
FROM assignment_rule r
JOIN rule_version rv ON rv.rule_id = r.id
WHERE lower(rv.valid_range) = sqlc.arg('today')::date
  AND rv.recorded_at < (now() - interval '1 minute');
