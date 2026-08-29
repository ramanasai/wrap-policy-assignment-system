// Package reconciler is the event-driven worker that keeps the materialized
// projection aligned with the sources of truth (docs/ARCHITECTURE.md §4).
//
// Contract:
//   - Decision consistency is transactional (read path pulls, always correct).
//   - Enforcement convergence is eventual with a bounded, observed SLA:
//     this worker + the sweeper backstop ARE that SLA.
//   - The projection is a cache; every failure here is recoverable because
//     nothing authoritative is ever lost.
package reconciler

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	db "github.com/ramanasai/wrap-policy-assignment-system/gen/db"
	"github.com/rs/zerolog"

	"github.com/ramanasai/wrap-policy-assignment-system/internal/repo"
	"github.com/ramanasai/wrap-policy-assignment-system/internal/utils"
)

// Config bounds the worker (bulkhead per SCALE_NOTES §3 lives in the caller).
type Config struct {
	BatchSize     int32
	PollInterval  time.Duration // safety-net poll when NOTIFY is missed
	MaxAttempts   int32         // dead-letter threshold
	NotifyChannel string        // LISTEN channel (matches migration 0002)
}

// DefaultConfig mirrors .env.example defaults.
func DefaultConfig() Config {
	return Config{
		BatchSize:     500,
		PollInterval:  5 * time.Second,
		MaxAttempts:   5,
		NotifyChannel: "new_outbox",
	}
}

// Reconciler processes outbox events into the materialized projection.
type Reconciler struct {
	store *repo.Store
	log   zerolog.Logger
	cfg   Config
}

func New(store *repo.Store, log zerolog.Logger, cfg Config) *Reconciler {
	return &Reconciler{store: store, log: log, cfg: cfg}
}

// BatchSize and PollInterval expose the effective worker bounds for the
// startup log / dashboards.
func (r *Reconciler) BatchSize() int32            { return r.cfg.BatchSize }
func (r *Reconciler) PollInterval() time.Duration { return r.cfg.PollInterval }

// ProcessBatch claims and processes one batch. Returns counts; exported for
// tests and the sweeper.
func (r *Reconciler) ProcessBatch(ctx context.Context) (processed, deadLettered int, err error) {
	events, err := r.store.ClaimOutboxBatch(ctx, r.cfg.BatchSize)
	if err != nil {
		return 0, 0, fmt.Errorf("claim: %w", err)
	}
	for _, ev := range events {
		if perr := r.processEvent(ctx, ev.ID, ev.EventType, ev.Payload); perr != nil {
			if ev.Attempts >= r.cfg.MaxAttempts {
				if dlErr := r.store.DeadLetter(ctx, ev.ID); dlErr != nil {
					return processed, deadLettered, fmt.Errorf("dead-letter event %d: %w", ev.ID, dlErr)
				}
				deadLettered++
				r.log.Error().Err(perr).Int64("event_id", ev.ID).Msg("event dead-lettered after max attempts")
				continue
			}
			// Attempts were incremented at claim time; the row remains
			// eligible for retry on a later batch.
			r.log.Warn().Err(perr).Int64("event_id", ev.ID).Int("attempt", int(ev.Attempts)).Msg("event processing failed — will retry")
			continue
		}
		if err := r.store.MarkProcessed(ctx, ev.ID); err != nil {
			return processed, deadLettered, fmt.Errorf("mark processed event %d: %w", ev.ID, err)
		}
		processed++
	}
	return processed, deadLettered, nil
}

func (r *Reconciler) processEvent(ctx context.Context, id int64, eventType string, payload []byte) error {
	switch eventType {
	case "fact_changed", "employee_changed":
		var p factChangedPayload
		if err := json.Unmarshal(payload, &p); err != nil {
			return fmt.Errorf("decode payload: %w", err)
		}
		return r.ReconcileEmployee(ctx, p.EmployeeID, &id)
	case "rule_changed", "category_changed":
		var p ruleChangedPayload
		if err := json.Unmarshal(payload, &p); err != nil {
			return fmt.Errorf("decode payload: %w", err)
		}
		return r.ReconcileRuleChange(ctx, p.CategoryID, &id)
	case "segment_changed":
		var p segmentChangedPayload
		if err := json.Unmarshal(payload, &p); err != nil {
			return fmt.Errorf("decode payload: %w", err)
		}
		return r.ReconcileSegmentChange(ctx, p.SegmentID, &id)
	default:
		return fmt.Errorf("unknown event type %q", eventType)
	}
}

type segmentChangedPayload struct {
	SegmentID string `json:"segment_id"`
}

type factChangedPayload struct {
	EmployeeID   string `json:"employee_id"`
	AttributeKey string `json:"attribute_key"`
}

type ruleChangedPayload struct {
	CategoryID string `json:"category_id"`
}

// ReconcileSegmentChange rebuilds a segment's membership from its predicate
// and re-reconciles every employee whose membership CHANGED — group
// membership changes flow through the exact same machinery as fact changes.
func (r *Reconciler) ReconcileSegmentChange(ctx context.Context, segmentID string, triggerEventID *int64) error {
	segments, err := r.store.ListSegments(ctx)
	if err != nil {
		return fmt.Errorf("reconcile segment: list: %w", err)
	}
	seg := (*db.Segment)(nil)
	for i := range segments {
		if segments[i].ID == segmentID {
			seg = &segments[i]
		}
	}
	if seg == nil {
		return fmt.Errorf("reconcile segment: %s not found", segmentID)
	}

	newMembers, err := r.store.RecomputeSegmentMembers(ctx, *seg)
	if err != nil {
		return fmt.Errorf("reconcile segment: recompute: %w", err)
	}

	// Diff against current membership → the affected set (entering/leaving).
	current, err := r.store.Q.GetSegmentMembers(ctx, segmentID)
	if err != nil {
		return fmt.Errorf("reconcile segment: read current: %w", err)
	}
	had := map[string]bool{}
	for _, id := range current {
		had[id] = true
	}
	changed := map[string]bool{}
	for _, id := range newMembers {
		if !had[id] {
			changed[id] = true
		}
	}
	for _, id := range current {
		if !hadCurrent(newMembers, id) {
			changed[id] = true
		}
	}

	if err := r.store.SetSegmentMembers(ctx, segmentID, newMembers); err != nil {
		return fmt.Errorf("reconcile segment: persist membership: %w", err)
	}

	// Re-reconcile only the changed employees (affected-set, not population).
	for empID := range changed {
		if err := r.ReconcileEmployee(ctx, empID, triggerEventID); err != nil {
			return fmt.Errorf("reconcile segment %s: member %s: %w", segmentID, empID, err)
		}
	}
	r.log.Info().Str("segment", segmentID).Int("members", len(newMembers)).Int("changed", len(changed)).Msg("segment membership rebuilt")
	return nil
}

func hadCurrent(members []string, id string) bool {
	for _, m := range members {
		if m == id {
			return true
		}
	}
	return false
}

// ReconcileEmployee re-resolves every category for one employee and
// materializes results + traces (traces written at decision time — #6).
func (r *Reconciler) ReconcileEmployee(ctx context.Context, employeeID string, triggerEventID *int64) error {
	cats, err := r.store.ListCategories(ctx)
	if err != nil {
		return fmt.Errorf("list categories: %w", err)
	}
	today := utils.TodayUTC()
	for _, cat := range cats {
		if err := r.materializeAndTrace(ctx, employeeID, cat.ID, today, triggerEventID); err != nil {
			return err
		}
	}
	return nil
}

// materializeAndTrace is the ONE place decisions become projection + audit:
// invariant #6 — traces are written at decision time, never recomputed.
// The sweeper uses the same helper so sweep-repairs stay auditable too.
func (r *Reconciler) materializeAndTrace(ctx context.Context, employeeID, categoryID, today string, triggerEventID *int64) error {
	res, err := r.store.ResolveForEmployee(ctx, employeeID, categoryID, today, repo.ResolveOptions{})
	if err != nil {
		return fmt.Errorf("resolve %s for %s: %w", categoryID, employeeID, err)
	}
	if err := r.store.Materialize(ctx, employeeID, res, triggerEventID); err != nil {
		return fmt.Errorf("materialize %s for %s: %w", categoryID, employeeID, err)
	}
	if err := r.store.PersistTrace(ctx, employeeID, categoryID, today, "materialize", res); err != nil {
		return fmt.Errorf("persist trace for %s: %w", categoryID, err)
	}
	return nil
}

// ReconcileRuleChange recomputes every employee for the affected category —
// the affected-set path, batched (MidPoint-style recompute).
func (r *Reconciler) ReconcileRuleChange(ctx context.Context, categoryID string, triggerEventID *int64) error {
	employeeIDs, err := r.store.ListEmployeeIDs(ctx)
	if err != nil {
		return fmt.Errorf("list employees: %w", err)
	}
	today := utils.TodayUTC()
	for _, empID := range employeeIDs {
		res, err := r.store.ResolveForEmployee(ctx, empID, categoryID, today, repo.ResolveOptions{})
		if err != nil {
			return fmt.Errorf("resolve %s for %s: %w", categoryID, empID, err)
		}
		if err := r.store.Materialize(ctx, empID, res, triggerEventID); err != nil {
			return fmt.Errorf("materialize %s for %s: %w", categoryID, empID, err)
		}
	}
	return nil
}

// Run blocks until ctx is cancelled: LISTEN/NOTIFY wakeups + poll safety net.
// The LISTEN connection is dedicated (session affinity — SCALE_NOTES §2).
func (r *Reconciler) Run(ctx context.Context, notifyURL string) {
	notifyCh := make(chan struct{}, 1)
	go r.listenLoop(ctx, notifyURL, notifyCh)

	ticker := time.NewTicker(r.cfg.PollInterval)
	defer ticker.Stop()

	r.drain(ctx) // catch up before idling
	for {
		select {
		case <-ctx.Done():
			r.log.Info().Msg("reconciler shutting down")
			return
		case <-notifyCh:
			r.drain(ctx)
		case <-ticker.C:
			r.drain(ctx)
		}
	}
}

// drain processes batches until the queue is empty.
func (r *Reconciler) drain(ctx context.Context) {
	start := time.Now()
	total := 0
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		processed, deadLettered, err := r.ProcessBatch(ctx)
		if err != nil {
			r.log.Error().Err(err).Msg("batch failed — backing off")
			time.Sleep(time.Second)
			return
		}
		total += processed
		if processed == 0 && deadLettered == 0 {
			break // queue drained
		}
	}
	if total > 0 {
		r.log.Info().Int("events", total).Dur("elapsed", time.Since(start)).Msg("outbox drained")
	}
}

// listenLoop holds the dedicated LISTEN connection. A missed notification is
// harmless: the poll loop is the safety net.
func (r *Reconciler) listenLoop(ctx context.Context, notifyURL string, notifyCh chan<- struct{}) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if err := r.listenOnce(ctx, notifyURL, notifyCh); err != nil {
			r.log.Warn().Err(err).Msg("LISTEN connection lost — reconnecting")
			time.Sleep(2 * time.Second)
		}
	}
}

func (r *Reconciler) listenOnce(ctx context.Context, notifyURL string, notifyCh chan<- struct{}) error {
	conn, err := pgx.Connect(ctx, notifyURL)
	if err != nil {
		return fmt.Errorf("listen connect: %w", err)
	}
	defer conn.Close(context.Background())

	if _, err := conn.Exec(ctx, "LISTEN "+r.cfg.NotifyChannel); err != nil {
		return fmt.Errorf("LISTEN: %w", err)
	}
	r.log.Info().Str("channel", r.cfg.NotifyChannel).Msg("LISTEN established")
	for {
		n, err := conn.WaitForNotification(ctx)
		if err != nil {
			return err // connection lost — reconnect
		}
		r.log.Debug().Str("payload", n.Payload).Msg("NOTIFY received")
		select {
		case notifyCh <- struct{}{}:
		default: // already pending — coalesce
		}
	}
}
