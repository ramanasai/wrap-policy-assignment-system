-- segments.sql — Supergroup (segment) machinery: derived membership.
-- Membership is REBUILDABLE: truncate + recompute from predicates at any time.

-- name: InsertSegment :one
INSERT INTO segment (id, company_id, name, predicate)
VALUES (sqlc.arg('id'), sqlc.arg('company_id'), sqlc.arg('name'), sqlc.arg('predicate'))
RETURNING *;

-- name: ListSegments :many
SELECT * FROM segment ORDER BY id;

-- name: GetSegment :one
SELECT * FROM segment WHERE id = sqlc.arg('id');

-- name: ResetSegmentMembership :exec
-- Remove all members BEFORE rebuilding (segment predicate changed).
DELETE FROM segment_membership WHERE segment_id = sqlc.arg('segment_id');

-- name: InsertSegmentMember :exec
INSERT INTO segment_membership (segment_id, employee_id)
VALUES (sqlc.arg('segment_id'), sqlc.arg('employee_id'))
ON CONFLICT DO NOTHING;

-- name: GetSegmentMembers :many
-- The POST-rebuild affected set (what changed since the last build is the
-- reconciler's job — it diffs before/after membership).
SELECT employee_id FROM segment_membership WHERE segment_id = sqlc.arg('segment_id') ORDER BY employee_id;

-- name: GetEmployeeSegments :many
SELECT segment_id FROM segment_membership WHERE employee_id = sqlc.arg('employee_id') ORDER BY segment_id;

-- name: ListSegmentIDs :many
SELECT id FROM segment ORDER BY id;