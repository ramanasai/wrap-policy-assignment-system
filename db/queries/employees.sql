-- employees.sql — population queries (preview diffs, reconciliation fan-out)

-- name: ListEmployeeIDs :many
SELECT id FROM employee ORDER BY id;

-- name: CountEmployees :one
SELECT count(*) FROM employee;

-- name: InsertAttributeDefinition :one
INSERT INTO attribute_definition (key, value_type, allowed_ops, enum_values, description)
VALUES (
    sqlc.arg('key'), sqlc.arg('value_type')::text, sqlc.arg('allowed_ops')::text[],
    sqlc.arg('enum_values'), sqlc.arg('description')
)
RETURNING *;

-- name: ListAttributeDefinitions :many
SELECT * FROM attribute_definition ORDER BY key;
