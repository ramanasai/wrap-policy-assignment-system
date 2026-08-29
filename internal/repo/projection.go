package repo

import (
	"context"
	"fmt"

	db "github.com/ramanasai/wrap-policy-assignment-system/gen/db"
	"github.com/ramanasai/wrap-policy-assignment-system/resolver"
)

// Materialize writes one resolution into the projection (assignment +
// shadowed matches) for an employee+category, inside one transaction.
//
// Semantics (docs/ARCHITECTURE.md): the projection is a CACHE — it may be
// truncated and rebuilt from events. Historical closed intervals survive;
// only the open-ended current window is replaced.
func (s *Store) Materialize(ctx context.Context, employeeID string, res resolver.Result, triggerEventID *int64) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("repo: materialize: begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck — no-op after commit

	qtx := s.Q.WithTx(tx)
	if err := qtx.ReplaceAssignmentsForEmployeeCategory(ctx, db.ReplaceAssignmentsForEmployeeCategoryParams{
		EmployeeID: employeeID,
		CategoryID: res.CategoryID,
	}); err != nil {
		return fmt.Errorf("repo: materialize: delete stale: %w", err)
	}
	if err := qtx.ReplaceShadowedMatches(ctx, db.ReplaceShadowedMatchesParams{
		EmployeeID: employeeID,
		CategoryID: res.CategoryID,
	}); err != nil {
		return fmt.Errorf("repo: materialize: replace shadowed: %w", err)
	}

	validRange, err := pgRangeFrom(res.AsOfDate)
	if err != nil {
		return err
	}
	for _, a := range res.Assignments {
		if err := qtx.InsertAssignment(ctx, db.InsertAssignmentParams{
			EmployeeID:       employeeID,
			CategoryID:       res.CategoryID,
			PolicyID:         a.PolicyID,
			PolicyVersionID:  a.PolicyVersionID,
			RuleID:           a.RuleID,
			ValidRange:       validRange,
			TriggerEventID:   triggerEventID,
		}); err != nil {
			return fmt.Errorf("repo: materialize: insert assignment: %w", err)
		}
	}
	for _, sm := range res.Shadowed {
		if err := qtx.InsertShadowedMatch(ctx, db.InsertShadowedMatchParams{
			EmployeeID: employeeID,
			CategoryID: res.CategoryID,
			RuleID:     sm.RuleID,
			ByRuleID:   sm.ByRuleID,
			ValidRange: validRange,
		}); err != nil {
			return fmt.Errorf("repo: materialize: insert shadowed: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("repo: materialize: commit: %w", err)
	}
	return nil
}

// AssignedPolicies returns the employee's current open-ended assignments
// (category → policy) — the "actual" side of the sweeper's expected-vs-actual.
func (s *Store) AssignedPolicies(ctx context.Context, employeeID string) (map[string]string, error) {
	rows, err := s.Q.GetAssignedPolicies(ctx, employeeID)
	if err != nil {
		return nil, fmt.Errorf("repo: assigned policies: %w", err)
	}
	out := make(map[string]string, len(rows))
	for _, r := range rows {
		out[r.CategoryID] = r.PolicyID
	}
	return out, nil
}

// FutureTransitions lists facts and rule versions that BECOME effective
// today (the scheduler's future-dated catch — docs/PROTOTYPE_PLAN Phase 4).
func (s *Store) FactTransitionsToday(ctx context.Context, today string) ([]db.ListFactEventsStartingTodayRow, error) {
	d, err := pgDate(today)
	if err != nil {
		return nil, err
	}
	return s.Q.ListFactEventsStartingToday(ctx, d)
}

func (s *Store) RuleTransitionsToday(ctx context.Context, today string) ([]db.ListRuleVersionsStartingTodayRow, error) {
	d, err := pgDate(today)
	if err != nil {
		return nil, err
	}
	return s.Q.ListRuleVersionsStartingToday(ctx, d)
}
