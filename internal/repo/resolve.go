package repo

import (
	"context"
	"encoding/json"
	"fmt"

	db "github.com/ramanasai/wrap-policy-assignment-system/gen/db"
	"github.com/ramanasai/wrap-policy-assignment-system/resolver"
)

// ResolveOptions controls what Resolve persists.
type ResolveOptions struct {
	// CompanyID stamps outbox events emitted by this resolution's inputs.
	CompanyID string
	// EmitEvents writes outbox rows for the inputs that triggered this
	// resolution (fact changes are emitted by AddFact instead — leave false
	// for pure read-path resolutions).
	EmitEvents bool
}

// ResolveForEmployee is the full read-path orchestration:
//
//	category config + facts@date + rules@date → resolver.Resolve
//
// Decision consistency is transactional (read-your-writes): everything is
// read from the same committed snapshot via the pool.
func (s *Store) ResolveForEmployee(ctx context.Context, employeeID, categoryID, date string, opts ResolveOptions) (resolver.Result, error) {
	cat, err := s.Category(ctx, categoryID)
	if err != nil {
		return resolver.Result{}, err
	}
	facts, err := s.FactsAt(ctx, employeeID, date)
	if err != nil {
		return resolver.Result{}, err
	}
	rules, err := s.EffectiveRules(ctx, categoryID, date)
	if err != nil {
		return resolver.Result{}, err
	}
	return resolver.Resolve(resolver.Input{
		Category:    cat,
		Date:        date,
		Facts:       facts,
		Rules:       rules,
		Definitions: s.Definitions(),
	})
}

// PersistTrace records the decision trace at decision time (invariant #6:
// explanations are written at decision time, never recomputed on read).
func (s *Store) PersistTrace(ctx context.Context, employeeID, categoryID, date, trigger string, res resolver.Result) error {
	factsSnap, err := json.Marshal(res.Trace.FactsSnapshot)
	if err != nil {
		return fmt.Errorf("repo: persist trace: facts snapshot: %w", err)
	}
	policySnap, err := json.Marshal(res.Trace.PolicySnapshot)
	if err != nil {
		return fmt.Errorf("repo: persist trace: policy snapshot: %w", err)
	}
	evaluated, err := json.Marshal(res.Trace.Evaluated)
	if err != nil {
		return fmt.Errorf("repo: persist trace: evaluated: %w", err)
	}
	asOf, err := pgDate(date)
	if err != nil {
		return err
	}
	if _, err := s.Q.InsertDecisionTrace(ctx, db.InsertDecisionTraceParams{
		EmployeeID:     employeeID,
		CategoryID:     categoryID,
		AsOfDate:       asOf,
		Trigger:        trigger,
		Outcome:        res.Outcome,
		FactsSnapshot:  factsSnap,
		PolicySnapshot: policySnap,
		Evaluated:      evaluated,
	}); err != nil {
		return fmt.Errorf("repo: persist trace: %w", err)
	}
	return nil
}

// LatestTrace returns the most recent stored trace for the triple — the
// explain inspector's source of truth (never recomputed).
func (s *Store) LatestTrace(ctx context.Context, employeeID, categoryID, date string) (trigger, outcome, factsSnapshot, policySnapshot, evaluated string, err error) {
	asOf, derr := pgDate(date)
	if derr != nil {
		return "", "", "", "", "", derr
	}
	rows, qerr := s.Q.GetDecisionTrace(ctx, db.GetDecisionTraceParams{
		EmployeeID: employeeID,
		CategoryID: categoryID,
		AsOfDate:   asOf,
		Limit:      1,
	})
	if qerr != nil {
		return "", "", "", "", "", fmt.Errorf("repo: latest trace: %w", qerr)
	}
	if len(rows) == 0 {
		return "", "", "", "", "", ErrNoTrace
	}
	r := rows[0]
	return r.Trigger, r.Outcome, string(r.FactsSnapshot), string(r.PolicySnapshot), string(r.Evaluated), nil
}

// ErrNoTrace means no decision trace was persisted for the query — callers
// surface this as 404 rather than recomputing (invariant #6).
var ErrNoTrace = fmt.Errorf("repo: no decision trace stored for this (employee, category, date)")
