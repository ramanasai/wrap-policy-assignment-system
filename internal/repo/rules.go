package repo

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/ramanasai/wrap-policy-assignment-system/gen/db"
	"github.com/ramanasai/wrap-policy-assignment-system/resolver"
)

// EffectiveRules returns every effective rule version for a category at a
// date, converted to resolver.RuleVersion — the resolver's only view of rules.
func (s *Store) EffectiveRules(ctx context.Context, categoryID, date string) ([]resolver.RuleVersion, error) {
	asOf, err := pgDate(date)
	if err != nil {
		return nil, err
	}
	rows, err := s.Q.GetEffectiveRuleVersionsAsOf(ctx, db.GetEffectiveRuleVersionsAsOfParams{
		CategoryID: categoryID,
		AsOf:       asOf,
	})
	if err != nil {
		return nil, fmt.Errorf("repo: effective rules for %s @ %s: %w", categoryID, date, err)
	}

	out := make([]resolver.RuleVersion, 0, len(rows))
	for _, r := range rows {
		pred, err := resolver.ParsePredicate(r.Predicate)
		if err != nil {
			return nil, fmt.Errorf("repo: rule %s: stored predicate invalid: %w", r.RuleID, err)
		}
		createdAt, err := timestamptzOrNow(r.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("repo: rule %s: %w", r.RuleID, err)
		}
		out = append(out, resolver.RuleVersion{
			RuleID:        r.RuleID,
			RuleVersionID: r.RuleVersionID,
			CategoryID:    r.CategoryID,
			PolicyID:      r.PolicyID,
			Source:        resolver.Source(r.Source),
			Priority:      int(r.Priority),
			CreatedAt:     createdAt,
			Predicate:     pred,
		})
	}
	return out, nil
}

// CreateRule persists a rule + its v1 version in one transaction.
// predicateJSON must already be canonical AST JSON (validated by the API layer
// against the attribute registry before reaching here).
func (s *Store) CreateRule(ctx context.Context, id, companyID, categoryID, policyID string, source resolver.Source, priority int, specificity int, predicateJSON []byte, validFrom string) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("repo: create rule: begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	qtx := s.Q.WithTx(tx)
	if _, err := qtx.InsertAssignmentRule(ctx, db.InsertAssignmentRuleParams{
		ID:          id,
		CompanyID:   companyID,
		CategoryID:  categoryID,
		PolicyID:    policyID,
		Source:      string(source),
		Priority:    int32(priority),
		Specificity: int32Ptr(specificity),
	}); err != nil {
		return fmt.Errorf("repo: create rule %s: insert rule: %w", id, err)
	}
	validRange, err := pgRangeFrom(validFrom)
	if err != nil {
		return err
	}
	if _, err := qtx.InsertRuleVersion(ctx, db.InsertRuleVersionParams{
		ID:         id + ":v1",
		RuleID:     id,
		Version:    1,
		Predicate:  predicateJSON,
		ValidRange: validRange,
	}); err != nil {
		return fmt.Errorf("repo: create rule %s: insert version: %w", id, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("repo: create rule %s: commit: %w", id, err)
	}
	return nil
}

// DeleteRule removes a rule and its versions (cascade via repo semantics;
// no DB-level FK by decision). The reconciler's rule_changed fan-out runs
// after deletion so shadowed matches resurrect deterministically.
func (s *Store) DeleteRule(ctx context.Context, ruleID string) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("repo: delete rule: begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	qtx := s.Q.WithTx(tx)
	if _, err := qtx.DeleteRuleVersions(ctx, ruleID); err != nil {
		return fmt.Errorf("repo: delete rule %s: versions: %w", ruleID, err)
	}
	if _, err := qtx.DeleteAssignmentRule(ctx, ruleID); err != nil {
		return fmt.Errorf("repo: delete rule %s: %w", ruleID, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("repo: delete rule %s: commit: %w", ruleID, err)
	}
	return nil
}

// timestamptzOrNow converts a pgtype.Timestamptz; a NULL created_at (possible
// only via hand-seeded rows) falls back to now — deterministic per-invocation
// is not required here because it's a repair path, not a decision path.
func timestamptzOrNow(ts pgtype.Timestamptz) (time.Time, error) {
	if ts.Valid {
		return ts.Time, nil
	}
	return time.Now().UTC(), nil
}

func int32Ptr(v int) *int32 {
	out := int32(v)
	return &out
}
