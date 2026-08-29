package repo

import (
	"context"
	"fmt"

	db "github.com/ramanasai/wrap-policy-assignment-system/gen/db"
	"github.com/ramanasai/wrap-policy-assignment-system/internal/utils"
	"github.com/ramanasai/wrap-policy-assignment-system/resolver"
)

// SegmentAttr is the derived attribute injected into facts at resolve time,
// carrying the employee's current segment (Supergroup) memberships.
const SegmentAttr = "segments"

// ListSegments returns all segments (worker rebuild + seed).
func (s *Store) ListSegments(ctx context.Context) ([]db.Segment, error) {
	return s.Q.ListSegments(ctx)
}

// CreateSegment registers a named, reusable predicate.
func (s *Store) CreateSegment(ctx context.Context, id, companyID, name string, predicateJSON []byte) error {
	_, err := s.Q.InsertSegment(ctx, db.InsertSegmentParams{
		ID:        id,
		CompanyID: companyID,
		Name:      name,
		Predicate: predicateJSON,
	})
	if err != nil {
		return fmt.Errorf("repo: create segment %s: %w", id, err)
	}
	return nil
}

// RecomputeSegmentMembers evaluates a segment's predicate over the population
// and returns the NEW member set. Membership is derived state — never
// hand-edited. The caller (reconciler) resets the table and diffs for
// change propagation.
func (s *Store) RecomputeSegmentMembers(ctx context.Context, seg db.Segment) ([]string, error) {
	pred, err := resolver.ParsePredicate(seg.Predicate)
	if err != nil {
		return nil, fmt.Errorf("repo: segment %s predicate invalid: %w", seg.ID, err)
	}

	today := utils.TodayUTC()
	employeeIDs, err := s.ListEmployeeIDs(ctx)
	if err != nil {
		return nil, err
	}
	// Same engine as rules; at 100k scale this narrows via the inverted
	// index (SCALE_NOTES §1) — today's population scan is the honest v1.
	var members []string
	for _, empID := range employeeIDs {
		facts, err := s.FactsAt(ctx, empID, today)
		if err != nil {
			return nil, err
		}
		if ok, _ := pred.Matches(facts); ok {
			members = append(members, empID)
		}
	}
	return members, nil
}

// SetSegmentMembers writes an exact membership set (reset + insert).
func (s *Store) SetSegmentMembers(ctx context.Context, segmentID string, members []string) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("repo: set segment members: begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck — no-op after commit

	qtx := s.Q.WithTx(tx)
	if err := qtx.ResetSegmentMembership(ctx, segmentID); err != nil {
		return fmt.Errorf("repo: set segment members: reset: %w", err)
	}
	for _, empID := range members {
		if err := qtx.InsertSegmentMember(ctx, db.InsertSegmentMemberParams{
			SegmentID:  segmentID,
			EmployeeID: empID,
		}); err != nil {
			return fmt.Errorf("repo: set segment members: insert %s: %w", empID, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("repo: set segment members: commit: %w", err)
	}
	return nil
}

// GetEmployeeSegments returns the employee's current segment memberships.
func (s *Store) GetEmployeeSegments(ctx context.Context, employeeID string) ([]string, error) {
	rows, err := s.Q.GetEmployeeSegments(ctx, employeeID)
	if err != nil {
		return nil, fmt.Errorf("repo: employee segments: %w", err)
	}
	return rows, nil
}

// injectSegments merges the employee's segment memberships into the fact
// snapshot as the derived "segments" list attribute (resolver stays pure —
// segments arrive as ordinary facts). Called by FactsAt.
func (s *Store) injectSegments(ctx context.Context, facts *resolver.Facts) error {
	rows, err := s.Q.GetEmployeeSegments(ctx, facts.EmployeeID)
	if err != nil {
		return fmt.Errorf("repo: inject segments for %s: %w", facts.EmployeeID, err)
	}
	list := make([]any, len(rows))
	for i, id := range rows {
		list[i] = id
	}
	facts.Attributes[SegmentAttr] = list
	return nil
}
