-- index.sql — the inverted attribute index (Rippling "Supergroup" pattern).
-- Reconciliation cost is proportional to the affected diff, never company size.

-- name: UpsertAttributeIndex :exec
INSERT INTO attribute_index (company_id, attribute_key, value_hash, employee_ids, updated_at)
VALUES (
    sqlc.arg('company_id'), sqlc.arg('attribute_key'), sqlc.arg('value_hash'),
    sqlc.arg('employee_ids')::text[], now()
)
ON CONFLICT (company_id, attribute_key, value_hash)
DO UPDATE SET employee_ids = EXCLUDED.employee_ids, updated_at = now();

-- name: GetEmployeesByAttributeValue :many
SELECT employee_ids FROM attribute_index
WHERE company_id = sqlc.arg('company_id')
  AND attribute_key = sqlc.arg('attribute_key')
  AND value_hash = sqlc.arg('value_hash');

-- name: DeleteAttributeIndexEntries :exec
DELETE FROM attribute_index
WHERE company_id = sqlc.arg('company_id')
  AND attribute_key = sqlc.arg('attribute_key')
  AND value_hash = sqlc.arg('value_hash');

-- name: UpsertEmployee :one
INSERT INTO employee (id, company_id, hired_on)
VALUES (sqlc.arg('id'), sqlc.arg('company_id'), sqlc.arg('hired_on')::date)
RETURNING *;

-- name: GetEmployee :one
SELECT * FROM employee WHERE id = sqlc.arg('id');
