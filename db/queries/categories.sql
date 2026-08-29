-- categories.sql — policy category config + policies (used by resolver wiring and seed)

-- name: GetCategory :one
SELECT * FROM policy_category WHERE id = sqlc.arg('id');

-- name: ListCategories :many
SELECT * FROM policy_category ORDER BY id;

-- name: InsertCategory :one
INSERT INTO policy_category (
    id, display_name, cardinality, resolution_strategy, default_priority, tiebreaker
) VALUES (
    sqlc.arg('id'), sqlc.arg('display_name'), sqlc.arg('cardinality')::text,
    sqlc.arg('resolution_strategy')::text, sqlc.arg('default_priority'), sqlc.arg('tiebreaker')
)
RETURNING *;

-- name: InsertPolicy :execrows
-- Idempotent: seeds may be re-run (policy payloads are versioned otherwise).
INSERT INTO policy (id, category_id, name, payload)
VALUES (sqlc.arg('id'), sqlc.arg('category_id'), sqlc.arg('name'), sqlc.arg('payload'))
ON CONFLICT (id) DO NOTHING;

-- name: InsertPolicyVersion :one
INSERT INTO policy_version (id, policy_id, version, payload, valid_range)
VALUES (
    sqlc.arg('id'), sqlc.arg('policy_id'), sqlc.arg('version'),
    sqlc.arg('payload'), sqlc.arg('valid_range')::daterange
)
RETURNING *;

-- name: ListPolicies :many
SELECT * FROM policy ORDER BY id;
