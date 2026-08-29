package repo

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	db "github.com/ramanasai/wrap-policy-assignment-system/gen/db"
)

// EmitEvent writes a transactional outbox row. Callers invoke this INSIDE
// the same transaction as their input change (or via WithTx variants in the
// workers) — that is what makes the outbox transactional (ARCHITECTURE §4).
func (s *Store) EmitEvent(ctx context.Context, eventType, companyID string, payload map[string]any, idempotencyKey string) (db.Outbox, error) {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return db.Outbox{}, fmt.Errorf("repo: emit event: payload: %w", err)
	}
	row, err := s.Q.InsertOutboxEvent(ctx, db.InsertOutboxEventParams{
		EventType:      eventType,
		CompanyID:      companyID,
		Payload:        payloadJSON,
		IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		return db.Outbox{}, fmt.Errorf("repo: emit event %s: %w", idempotencyKey, err)
	}
	return row, nil
}

// ClaimOutboxBatch claims the next unprocessed batch with FOR UPDATE SKIP
// LOCKED — concurrent workers never collide, and a killed worker loses
// nothing (the rows stay unclaimed by it and are re-claimable).
func (s *Store) ClaimOutboxBatch(ctx context.Context, batchSize int32) ([]db.Outbox, error) {
	rows, err := s.Q.ClaimOutboxBatch(ctx, batchSize)
	if err != nil {
		return nil, fmt.Errorf("repo: claim outbox batch: %w", err)
	}
	return rows, nil
}

// MarkProcessed / DeadLetter complete the claim lifecycle.
func (s *Store) MarkProcessed(ctx context.Context, id int64) error {
	return s.Q.MarkOutboxProcessed(ctx, id)
}

func (s *Store) DeadLetter(ctx context.Context, id int64) error {
	return s.Q.DeadLetterOutbox(ctx, id)
}

// UnprocessedCount is the queue-depth metric (observability + sweeper sizing).
func (s *Store) UnprocessedCount(ctx context.Context) (int64, error) {
	return s.Q.CountUnprocessedOutbox(ctx)
}

var _ = time.Now // reserved for backoff scheduling in the reconciler
