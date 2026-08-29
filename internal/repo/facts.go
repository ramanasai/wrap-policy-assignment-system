package repo

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/ramanasai/wrap-policy-assignment-system/gen/db"
	"github.com/ramanasai/wrap-policy-assignment-system/resolver"
)

// ---------------------------------------------------------------------------
// pgtype helpers — the ONLY place Postgres range/date types are constructed.
// Valid time is date-granular; [from, ∞) is the standard open-ended interval.
// ---------------------------------------------------------------------------

func pgDate(s string) (pgtype.Date, error) {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return pgtype.Date{}, fmt.Errorf("repo: invalid date %q: %w", s, err)
	}
	return pgtype.Date{Time: t, Valid: true}, nil
}

// pgRangeFrom returns [from, ∞) — end-exclusive convention, unbounded upper.
func pgRangeFrom(from string) (pgtype.Range[pgtype.Date], error) {
	lower, err := pgDate(from)
	if err != nil {
		return pgtype.Range[pgtype.Date]{}, err
	}
	return pgtype.Range[pgtype.Date]{
		Lower:     lower,
		LowerType: pgtype.Inclusive,
		UpperType: pgtype.Unbounded,
		Valid:     true,
	}, nil
}

// ---------------------------------------------------------------------------
// Facts
// ---------------------------------------------------------------------------

// FactsAt returns the employee's attribute snapshot at a business date —
// the post-correction view (latest recorded wins among overlapping ranges).
func (s *Store) FactsAt(ctx context.Context, employeeID, date string) (resolver.Facts, error) {
	asOf, err := pgDate(date)
	if err != nil {
		return resolver.Facts{}, err
	}
	rows, err := s.Q.GetEmployeeFactsAsOf(ctx, db.GetEmployeeFactsAsOfParams{
		EmployeeID: employeeID,
		AsOf:       asOf,
	})
	if err != nil {
		return resolver.Facts{}, fmt.Errorf("repo: facts as-of %s for %s: %w", date, employeeID, err)
	}
	attrs := make(map[string]any, len(rows))
	for _, r := range rows {
		var v any
		if err := json.Unmarshal(r.Value, &v); err != nil {
			return resolver.Facts{}, fmt.Errorf("repo: fact %s for %s: decode value: %w", r.AttributeKey, employeeID, err)
		}
		attrs[r.AttributeKey] = v
	}
	return resolver.Facts{
		EmployeeID: employeeID,
		AsOf:       date,
		Attributes: attrs,
	}, nil
}

// AddFact appends a fact event starting at validFrom and closes the previous
// open interval for that attribute in one transaction (bitemporal per Q2:
// values are append-only; interval bounds close — no gaps, no overlaps).
// Returns the new fact event ID.
func (s *Store) AddFact(ctx context.Context, employeeID, attributeKey string, value any, validFrom, trigger string) (int64, error) {
	valueJSON, err := json.Marshal(value)
	if err != nil {
		return 0, fmt.Errorf("repo: add fact: encode value: %w", err)
	}
	validRange, err := pgRangeFrom(validFrom)
	if err != nil {
		return 0, err
	}
	newStart, err := pgDate(validFrom)
	if err != nil {
		return 0, err
	}

	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("repo: add fact: begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck — no-op after commit

	qtx := s.Q.WithTx(tx)
	// Close the previous open fact BEFORE inserting, so the new open-ended
	// range can never be caught by the closure predicate.
	if _, err := qtx.CloseOpenFactRange(ctx, db.CloseOpenFactRangeParams{
		EmployeeID:   employeeID,
		AttributeKey: attributeKey,
		NewStart:     newStart,
	}); err != nil {
		return 0, fmt.Errorf("repo: add fact: close previous: %w", err)
	}
	fact, err := qtx.InsertFactEvent(ctx, db.InsertFactEventParams{
		EmployeeID:   employeeID,
		AttributeKey: attributeKey,
		Value:        valueJSON,
		ValidRange:   validRange,
		Trigger:      trigger,
	})
	if err != nil {
		return 0, fmt.Errorf("repo: add fact: insert: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("repo: add fact: commit: %w", err)
	}
	return fact.ID, nil
}

// AddEmployee registers an employee.
func (s *Store) AddEmployee(ctx context.Context, id, companyID, hiredOn string) error {
	hired, err := pgDate(hiredOn)
	if err != nil {
		return err
	}
	if _, err := s.Q.UpsertEmployee(ctx, db.UpsertEmployeeParams{
		ID:        id,
		CompanyID: companyID,
		HiredOn:   hired,
	}); err != nil {
		return fmt.Errorf("repo: add employee %s: %w", id, err)
	}
	return nil
}

// CountEmployees returns the population size (seed idempotency check).
func (s *Store) CountEmployees(ctx context.Context) (int64, error) {
	return s.Q.CountEmployees(ctx)
}

// ListEmployeeIDs returns the full population (preview/reconciliation scale).
func (s *Store) ListEmployeeIDs(ctx context.Context) ([]string, error) {
	rows, err := s.Q.ListEmployeeIDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("repo: list employees: %w", err)
	}
	return rows, nil
}
