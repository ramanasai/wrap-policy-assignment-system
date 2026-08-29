-- rules.sql — assignment rules + effective-dated version selection

-- name: GetEffectiveRuleVersionsAsOf :many
-- Every rule version for the category that is valid at the given date, at
-- most one (the latest-recorded) per rule — the post-correction view that
-- mirrors GetEmployeeFactsAsOf semantics.
SELECT DISTINCT ON (rv.rule_id)
       r.id            AS rule_id,
       rv.id           AS rule_version_id,
       r.category_id,
       r.policy_id,
       r.source,
       r.priority,
       r.created_at,
       rv.predicate,
       rv.version      AS rule_version_number
FROM assignment_rule r
JOIN rule_version rv ON rv.rule_id = r.id
WHERE r.category_id = sqlc.arg('category_id')
  AND rv.valid_range @> sqlc.arg('as_of')::date
ORDER BY rv.rule_id, rv.recorded_at DESC;

-- name: GetRuleVersionsByIDs :many
SELECT rv.id, rv.rule_id, rv.version, rv.predicate, rv.valid_range, rv.recorded_at
FROM rule_version rv
WHERE rv.id = ANY(sqlc.arg('rule_version_ids')::text[]);

-- name: InsertAssignmentRule :one
INSERT INTO assignment_rule (
    id, company_id, category_id, policy_id, source, priority, specificity
) VALUES (
    sqlc.arg('id'), sqlc.arg('company_id'), sqlc.arg('category_id'),
    sqlc.arg('policy_id'), sqlc.arg('source')::text, sqlc.arg('priority'),
    sqlc.arg('specificity')
)
RETURNING *;

-- name: InsertRuleVersion :one
INSERT INTO rule_version (
    id, rule_id, version, predicate, valid_range
) VALUES (
    sqlc.arg('id'), sqlc.arg('rule_id'), sqlc.arg('version'),
    sqlc.arg('predicate'), sqlc.arg('valid_range')::daterange
)
RETURNING *;

-- name: DeleteRuleVersions :execrows
DELETE FROM rule_version WHERE rule_id = sqlc.arg('rule_id');

-- name: DeleteAssignmentRule :execrows
DELETE FROM assignment_rule WHERE id = sqlc.arg('id');
