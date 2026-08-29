-- outbox.sql — transactional outbox: claim, complete, dead-letter

-- name: InsertOutboxEvent :one
INSERT INTO outbox (
    event_type, company_id, payload, idempotency_key
) VALUES (
    sqlc.arg('event_type')::text, sqlc.arg('company_id'),
    sqlc.arg('payload'), sqlc.arg('idempotency_key')
)
RETURNING *;

-- name: ClaimOutboxBatch :many
-- FOR UPDATE SKIP LOCKED is the Postgres-as-queue pattern: concurrent
-- reconciler workers never grab the same rows, and a killed worker loses
-- nothing — the next worker claims them (docs/ARCHITECTURE.md §4).
UPDATE outbox o
SET attempts = o.attempts + 1
WHERE o.id IN (
    SELECT id FROM outbox
    WHERE processed_at IS NULL
      AND dead_lettered = FALSE
    ORDER BY id
    LIMIT sqlc.arg('batch_size')::int
    FOR UPDATE SKIP LOCKED
)
RETURNING o.*;

-- name: MarkOutboxProcessed :exec
UPDATE outbox SET processed_at = now()
WHERE id = sqlc.arg('id');

-- name: DeadLetterOutbox :exec
UPDATE outbox SET dead_lettered = TRUE
WHERE id = sqlc.arg('id');

-- name: CountUnprocessedOutbox :one
SELECT count(*) FROM outbox WHERE processed_at IS NULL AND NOT dead_lettered;
