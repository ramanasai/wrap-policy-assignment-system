-- traces.sql — decision traces are write-once audit artifacts (never updated)

-- name: InsertDecisionTrace :one
INSERT INTO decision_trace (
    employee_id, category_id, as_of_date, trigger, outcome,
    facts_snapshot, policy_snapshot, evaluated
) VALUES (
    sqlc.arg('employee_id'), sqlc.arg('category_id'), sqlc.arg('as_of_date')::date,
    sqlc.arg('trigger')::text, sqlc.arg('outcome')::text,
    sqlc.arg('facts_snapshot'), sqlc.arg('policy_snapshot'), sqlc.arg('evaluated')
)
RETURNING *;

-- name: GetDecisionTrace :many
SELECT * FROM decision_trace
WHERE employee_id = sqlc.arg('employee_id')
  AND category_id = sqlc.arg('category_id')
  AND as_of_date = sqlc.arg('as_of_date')::date
ORDER BY created_at DESC
LIMIT sqlc.arg('limit')::int;

-- name: CountTracesBefore :one
-- Retention sweep support (TRADEOFFS.md: full traces for the audit window,
-- aggregated summaries beyond).
SELECT count(*) FROM decision_trace WHERE created_at < sqlc.arg('before')::timestamptz;
