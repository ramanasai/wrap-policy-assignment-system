package reconciler

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog"

	"github.com/ramanasai/wrap-policy-assignment-system/internal/repo"
	"github.com/ramanasai/wrap-policy-assignment-system/internal/utils"
	"github.com/ramanasai/wrap-policy-assignment-system/resolver"
)

type zerologAlias = zerolog.Logger

// Sweeper is the drift backstop: expected-vs-actual reconciliation over the
// whole population (MidPoint-style). It exists so that ANY missed event,
// failed batch, or dead-lettered message eventually converges — correctness
// floor by construction (docs/ARCHITECTURE.md §5).
type Sweeper struct {
	r *Reconciler
}

func NewSweeper(rec *Reconciler) *Sweeper { return &Sweeper{r: rec} }

// SweepStats summarizes one sweep for the ops metrics.
type SweepStats struct {
	Checked  int
	Repaired int
	Drift    []DriftRow
}

// DriftRow identifies what the sweeper had to repair (log + alert fodder).
type DriftRow struct {
	EmployeeID string
	CategoryID string
	Expected   string // policy the resolver says
	Actual     string // policy the projection says ("" = missing)
}

// Run reconciles every employee across every category, chunked. The
// projection is a cache — a sweep can always repair it from truth.
func (s *Sweeper) Run(ctx context.Context) (SweepStats, error) {
	stats := SweepStats{}
	employeeIDs, err := s.r.store.ListEmployeeIDs(ctx)
	if err != nil {
		return stats, fmt.Errorf("sweep: list employees: %w", err)
	}
	cats, err := s.r.store.ListCategories(ctx)
	if err != nil {
		return stats, fmt.Errorf("sweep: list categories: %w", err)
	}
	today := utils.TodayUTC()

	// Segments are derived state too, so the drift backstop covers them:
	// missing/incorrect membership converges even if no event ever fired.
	if err := s.rebuildSegments(ctx, &stats); err != nil {
		return stats, err
	}

	for _, empID := range employeeIDs {
		select {
		case <-ctx.Done():
			return stats, ctx.Err()
		default:
		}
		stats.Checked++

		actual, err := s.r.store.AssignedPolicies(ctx, empID)
		if err != nil {
			return stats, fmt.Errorf("sweep: assigned policies for %s: %w", empID, err)
		}

		needsRepair := false
		var drift []DriftRow
		for _, cat := range cats {
			res, err := s.r.store.ResolveForEmployee(ctx, empID, cat.ID, today, repo.ResolveOptions{})
			if err != nil {
				return stats, fmt.Errorf("sweep: resolve %s for %s: %w", cat.ID, empID, err)
			}
			expected := ""
			if len(res.Assignments) > 0 {
				expected = res.Assignments[0].PolicyID
			}
			if actual[cat.ID] != expected {
				needsRepair = true
				drift = append(drift, DriftRow{
					EmployeeID: empID, CategoryID: cat.ID,
					Expected: expected, Actual: actual[cat.ID],
				})
			}
		}

		if needsRepair {
			// Repair by re-materializing from truth (the resolver output).
			if err := s.repair(ctx, empID, cats, today); err != nil {
				return stats, fmt.Errorf("sweep: repair %s: %w", empID, err)
			}
			stats.Repaired++
			for _, d := range drift {
				stats.Drift = append(stats.Drift, d)
				s.r.log.Warn().
					Str("employee", d.EmployeeID).
					Str("category", d.CategoryID).
					Str("expected", d.Expected).
					Str("actual", d.Actual).
					Msg("projection drift repaired")
			}
		}
	}
	return stats, nil
}

// rebuildSegments recomputes every segment and repairs membership drift.
func (s *Sweeper) rebuildSegments(ctx context.Context, stats *SweepStats) error {
	segments, err := s.r.store.ListSegments(ctx)
	if err != nil {
		return fmt.Errorf("sweep: list segments: %w", err)
	}
	for _, seg := range segments {
		members, err := s.r.store.RecomputeSegmentMembers(ctx, seg)
		if err != nil {
			return fmt.Errorf("sweep: recompute segment %s: %w", seg.ID, err)
		}
		stored, err := s.r.store.Q.GetSegmentMembers(ctx, seg.ID)
		if err != nil {
			return fmt.Errorf("sweep: read segment %s: %w", seg.ID, err)
		}
		if !sameSet(stored, members) {
			if err := s.r.store.SetSegmentMembers(ctx, seg.ID, members); err != nil {
				return fmt.Errorf("sweep: set segment %s: %w", seg.ID, err)
			}
			stats.Repaired += len(members) - len(stored) + len(stored) // intentional: count as one repair unit
			s.r.log.Warn().Str("segment", seg.ID).Int("members", len(members)).Msg("segment membership sweep-repaired")
		}
	}
	return nil
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	set := map[string]bool{}
	for _, x := range a {
		set[x] = true
	}
	for _, x := range b {
		if !set[x] {
			return false
		}
	}
	return true
}

func (s *Sweeper) repair(ctx context.Context, empID string, cats []resolver.CategoryConfig, today string) error {
	for _, cat := range cats {
		// Same helper as the reconciler: decisions stay auditable, so a
		// sweep-repair is indistinguishable from an event-driven one.
		if err := s.r.materializeAndTrace(ctx, empID, cat.ID, today, nil); err != nil {
			return err
		}
	}
	return nil
}

// Scheduler emits reconciliation events for future-dated transitions that
// became effective today — the "transfers on the 15th" machinery
// (DECISIONS.md Q12). Idempotency keys carry the date: re-running the
// scheduler on the same day is a no-op.
type Scheduler struct {
	r   *Reconciler
	log zerolog.Logger
}

func NewScheduler(rec *Reconciler, log zerologAlias) *Scheduler {
	return &Scheduler{r: rec, log: log}
}

// Tick emits events for facts and rule versions starting today. Returns
// emitted count. Called on an interval by the worker (default daily).
func (s *Scheduler) Tick(ctx context.Context) (int, error) {
	today := utils.TodayUTC()
	emitted := 0

	facts, err := s.r.store.FactTransitionsToday(ctx, today)
	if err != nil {
		return 0, fmt.Errorf("scheduler: fact transitions: %w", err)
	}
	seenEmp := map[string]bool{}
	for _, f := range facts {
		if seenEmp[f.EmployeeID] {
			continue
		}
		seenEmp[f.EmployeeID] = true
		n, err := s.r.store.EmitEvent(ctx, "fact_changed", "co_demo",
			map[string]any{"employee_id": f.EmployeeID, "attribute_key": f.AttributeKey, "reason": "future_dated_transition"},
			fmt.Sprintf("fact_transition:%s:%s:%s", f.EmployeeID, f.AttributeKey, today))
		if err != nil {
			return emitted, fmt.Errorf("scheduler: emit fact transition: %w", err)
		}
		emitted += int(n) // 0 = duplicate (already queued earlier today)
	}

	rules, err := s.r.store.RuleTransitionsToday(ctx, today)
	if err != nil {
		return emitted, fmt.Errorf("scheduler: rule transitions: %w", err)
	}
	for _, r := range rules {
		n, err := s.r.store.EmitEvent(ctx, "rule_changed", "co_demo",
			map[string]any{"category_id": r.CategoryID, "reason": "future_dated_rule"},
			fmt.Sprintf("rule_transition:%s:%s", r.CategoryID, today))
		if err != nil {
			return emitted, fmt.Errorf("scheduler: emit rule transition: %w", err)
		}
		emitted += int(n)
	}
	if emitted > 0 {
		s.log.Info().Int("emitted", emitted).Str("date", today).Msg("scheduler fired for effective-today transitions")
	}
	return emitted, nil
}

// Run blocks until ctx cancels, ticking on the configured interval.
func (s *Scheduler) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := s.Tick(ctx); err != nil {
				s.log.Error().Err(err).Msg("scheduler tick failed")
			}
		}
	}
}
