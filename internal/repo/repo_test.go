package repo

import (
	"context"
	"encoding/json"
	"testing"

	db "github.com/ramanasai/wrap-policy-assignment-system/gen/db"
	"github.com/ramanasai/wrap-policy-assignment-system/internal/config"
	"github.com/ramanasai/wrap-policy-assignment-system/internal/testdb"
	"github.com/ramanasai/wrap-policy-assignment-system/resolver"
)

func testStore(t *testing.T) *Store {
	store, err := New(context.Background(), config.Config{DatabaseURL: testdb.URL(t, "pas_repo_test")})
	if err != nil {
		t.Fatalf("repo.New: %v", err)
	}
	t.Cleanup(store.Close)
	return store
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestStore_LoadsAttributeDefinitions(t *testing.T) {
	s := testStore(t)
	defs := s.Definitions()
	if len(defs) == 0 {
		t.Fatal("attribute registry empty — migrations didn't seed")
	}
	loc, ok := defs["location"]
	if !ok || loc.ValueType != resolver.TypeString {
		t.Fatalf("location def = %+v, want TypeString", loc)
	}
}

func TestFactsAt_BitemporalRoundTrip(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	if err := s.AddEmployee(ctx, "emp_bt", "co_test", "2024-01-19"); err != nil {
		t.Fatalf("AddEmployee: %v", err)
	}

	// Hire-time location, then a backdated relocation valid Mar 3.
	if _, err := s.AddFact(ctx, "emp_bt", "location", "US-NY", "2024-01-19", "hr_edit"); err != nil {
		t.Fatalf("AddFact NY: %v", err)
	}
	if _, err := s.AddFact(ctx, "emp_bt", "location", "US-CA", "2026-03-03", "hr_edit"); err != nil {
		t.Fatalf("AddFact CA: %v", err)
	}

	// Before the relocation: NY.
	before, err := s.FactsAt(ctx, "emp_bt", "2026-03-02")
	if err != nil {
		t.Fatalf("FactsAt(before): %v", err)
	}
	if before.Attributes["location"] != "US-NY" {
		t.Errorf("before = %v, want US-NY", before.Attributes["location"])
	}

	// After: CA — and the NY row must be superseded, not deleted.
	after, err := s.FactsAt(ctx, "emp_bt", "2026-03-10")
	if err != nil {
		t.Fatalf("FactsAt(after): %v", err)
	}
	if after.Attributes["location"] != "US-CA" {
		t.Errorf("after = %v, want US-CA", after.Attributes["location"])
	}

	// Interval closure: the NY fact must now end at 2026-03-03 (exclusive),
	// and NO row is superseded — a change closes intervals, corrections supersede.
	var supersededCount int
	if err := s.Pool.QueryRow(ctx,
		"SELECT count(*) FROM fact_event WHERE employee_id='emp_bt' AND superseded_by IS NOT NULL",
	).Scan(&supersededCount); err != nil || supersededCount != 0 {
		t.Fatalf("superseded rows = %d (err=%v), want 0 (changes close intervals, not supersede)", supersededCount, err)
	}
	var upperInf bool
	if err := s.Pool.QueryRow(ctx,
		"SELECT upper_inf(valid_range) FROM fact_event WHERE employee_id='emp_bt' AND value='\"US-NY\"'::jsonb",
	).Scan(&upperInf); err != nil || upperInf {
		t.Fatalf("old NY fact must have a closed interval after the change (upperInf=%v, err=%v)", upperInf, err)
	}
}

func TestCreateRule_EffectiveWindowHonored(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	// Seed the category/policy minimally.
	seedVacationCategory(t, s)

	pred := mustPredJSON(`{"op":"and","clauses":[{"attr":"location","op":"eq","value":"US-CA"}]}`)
	if err := s.CreateRule(ctx, "r_window", "co_test", "vacation", "pol_vac",
		resolver.SourceAuthored, 0, 3, pred, "2026-06-01"); err != nil {
		t.Fatalf("CreateRule: %v", err)
	}

	// Before validity: zero effective rules.
	before, err := s.EffectiveRules(ctx, "vacation", "2026-05-31")
	if err != nil {
		t.Fatalf("EffectiveRules(before): %v", err)
	}
	if len(before) != 0 {
		t.Fatalf("before validity got %d rules, want 0", len(before))
	}

	// After validity: exactly one, correctly converted.
	after, err := s.EffectiveRules(ctx, "vacation", "2026-06-15")
	if err != nil {
		t.Fatalf("EffectiveRules(after): %v", err)
	}
	if len(after) != 1 {
		t.Fatalf("after validity got %d rules, want 1", len(after))
	}
	r := after[0]
	if r.RuleID != "r_window" || r.RuleVersionID != "r_window:v1" ||
		r.Source != resolver.SourceAuthored || r.Priority != 0 {
		t.Errorf("converted rule = %+v", r)
	}
	if r.Predicate.Clauses[0].Attr != "location" || r.Predicate.Clauses[0].Op != resolver.OpEq {
		t.Errorf("predicate round-trip broken: %+v", r.Predicate)
	}
}

func TestResolveForEmployee_EndToEnd(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	seedVacationCategory(t, s)

	// CA engineer: matches both the broad US rule (priority 5) and the
	// narrower CA+Eng rule (priority 5) → specificity decides, loser shadowed.
	if err := s.AddEmployee(ctx, "emp_e2e", "co_test", "2024-01-19"); err != nil {
		t.Fatalf("AddEmployee: %v", err)
	}
	for k, v := range map[string]any{
		"location": "US-CA", "department": "Engineering", "employment_type": "full_time",
	} {
		if _, err := s.AddFact(ctx, "emp_e2e", k, v, "2024-01-19", "hr_edit"); err != nil {
			t.Fatalf("AddFact %s: %v", k, err)
		}
	}
	broad := mustPredJSON(`{"op":"and","clauses":[{"attr":"location","op":"in","value":["US-CA","US-NY"]}]}`)
	narrow := mustPredJSON(`{"op":"and","clauses":[{"attr":"location","op":"eq","value":"US-CA"},{"attr":"department","op":"eq","value":"Engineering"}]}`)
	if err := s.CreateRule(ctx, "r_e2e_broad", "co_test", "vacation", "pol_vac", resolver.SourceAuthored, 5, 2, broad, "2024-06-01"); err != nil {
		t.Fatalf("CreateRule broad: %v", err)
	}
	if err := s.CreateRule(ctx, "r_e2e_narrow", "co_test", "vacation", "pol_vac", resolver.SourceAuthored, 5, 6, narrow, "2024-06-01"); err != nil {
		t.Fatalf("CreateRule narrow: %v", err)
	}

	res, err := s.ResolveForEmployee(ctx, "emp_e2e", "vacation", "2026-03-03", ResolveOptions{})
	if err != nil {
		t.Fatalf("ResolveForEmployee: %v", err)
	}
	if res.Outcome != resolver.OutcomeShadowed {
		t.Fatalf("outcome = %q, want shadowed (full stack)", res.Outcome)
	}
	if len(res.Assignments) != 1 || res.Assignments[0].RuleID != "r_e2e_narrow" {
		t.Fatalf("winner = %+v, want narrow rule (specificity tiebreak)", res.Assignments)
	}
	if len(res.Shadowed) != 1 || res.Shadowed[0].RuleID != "r_e2e_broad" {
		t.Fatalf("shadowed = %+v", res.Shadowed)
	}
	if len(res.Trace.Evaluated) < 2 {
		t.Errorf("trace should cover both rules: %+v", res.Trace.Evaluated)
	}
}

func TestPersistTrace_RoundTrip(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	seedVacationCategory(t, s)
	if err := s.AddEmployee(ctx, "emp_trace", "co_test", "2024-01-19"); err != nil {
		t.Fatalf("AddEmployee: %v", err)
	}
	if _, err := s.AddFact(ctx, "emp_trace", "location", "US-CA", "2024-01-19", "hr_edit"); err != nil {
		t.Fatalf("AddFact: %v", err)
	}
	if err := s.CreateRule(ctx, "r_trace", "co_test", "vacation", "pol_vac", resolver.SourceAuthored, 0, 3,
		mustPredJSON(`{"op":"and","clauses":[{"attr":"location","op":"eq","value":"US-CA"}]}`), "2024-06-01"); err != nil {
		t.Fatalf("CreateRule: %v", err)
	}

	res, err := s.ResolveForEmployee(ctx, "emp_trace", "vacation", "2026-03-03", ResolveOptions{})
	if err != nil {
		t.Fatalf("ResolveForEmployee: %v", err)
	}
	if err := s.PersistTrace(ctx, "emp_trace", "vacation", "2026-03-03", "materialize", res); err != nil {
		t.Fatalf("PersistTrace: %v", err)
	}

	// Read back: same outcome, snapshot carries the location.
	_, outcome, factsSnap, _, evaluated, err := s.LatestTrace(ctx, "emp_trace", "vacation", "2026-03-03")
	if err != nil {
		t.Fatalf("LatestTrace: %v", err)
	}
	if outcome != res.Outcome {
		t.Errorf("stored outcome = %q, want %q", outcome, res.Outcome)
	}
	if !json.Valid([]byte(factsSnap)) || !json.Valid([]byte(evaluated)) {
		t.Errorf("stored snapshots not valid JSON")
	}
	if !contains(factsSnap, "US-CA") {
		t.Errorf("facts snapshot missing location: %s", factsSnap)
	}

	// Explaining a date with no trace → ErrNoTrace (never recompute).
	if _, _, _, _, _, err := s.LatestTrace(ctx, "emp_trace", "vacation", "2020-01-01"); err != ErrNoTrace {
		t.Fatalf("LatestTrace(no such date) = %v, want ErrNoTrace", err)
	}
}

func TestOutbox_ClaimLifecycle(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	// Idempotency: same key twice → second insert fails.
	if _, err := s.EmitEvent(ctx, "fact_changed", "co_test", map[string]any{"k": 1}, "idem-lifecycle-1"); err != nil {
		t.Fatalf("EmitEvent: %v", err)
	}
	if _, err := s.EmitEvent(ctx, "fact_changed", "co_test", map[string]any{"k": 1}, "idem-lifecycle-1"); err == nil {
		t.Fatal("duplicate idempotency key must be rejected by the unique constraint")
	}

	claimed, err := s.ClaimOutboxBatch(ctx, 500)
	if err != nil {
		t.Fatalf("ClaimOutboxBatch: %v", err)
	}
	if len(claimed) == 0 {
		t.Fatal("claimed 0 rows after emitting an event")
	}
	for _, row := range claimed {
		if err := s.MarkProcessed(ctx, row.ID); err != nil {
			t.Fatalf("MarkProcessed: %v", err)
		}
	}
	n, err := s.UnprocessedCount(ctx)
	if err != nil {
		t.Fatalf("UnprocessedCount: %v", err)
	}
	if n != 0 {
		t.Fatalf("unprocessed = %d, want 0 after marking processed", n)
	}

	// Dead-letter path: emit, dead-letter, confirm excluded from claims.
	if _, err := s.EmitEvent(ctx, "fact_changed", "co_test", map[string]any{"k": 2}, "idem-lifecycle-2"); err != nil {
		t.Fatalf("EmitEvent 2: %v", err)
	}
	claimed, _ = s.ClaimOutboxBatch(ctx, 500)
	for _, row := range claimed {
		if err := s.DeadLetter(ctx, row.ID); err != nil {
			t.Fatalf("DeadLetter: %v", err)
		}
	}
	n, _ = s.UnprocessedCount(ctx)
	if n != 0 {
		t.Fatalf("dead-lettered rows must not count as unprocessed: %d", n)
	}
}

func TestResolveForEmployee_NonexistentEmployeeIsNoMatch(t *testing.T) {
	s := testStore(t)
	seedVacationCategory(t, s)

	res, err := s.ResolveForEmployee(context.Background(), "emp_ghost", "vacation", "2026-03-03", ResolveOptions{})
	if err != nil {
		t.Fatalf("unknown employee must resolve to no_match (facts are just empty), got error: %v", err)
	}
	if res.Outcome != resolver.OutcomeNoMatch {
		t.Fatalf("outcome = %q, want no_match", res.Outcome)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func seedVacationCategory(t *testing.T, s *Store) {
	t.Helper()
	ctx := context.Background()
	// Category semantics (idempotent: already present → skip seeding).
	if _, err := s.Category(ctx, "vacation"); err == nil {
		return // already seeded
	}
	if _, err := s.Q.InsertCategory(ctx, db.InsertCategoryParams{
		ID:                 "vacation",
		DisplayName:        "Vacation (test)",
		Cardinality:        "single",
		ResolutionStrategy: "priority_rank",
		DefaultPriority:    0,
		Tiebreaker:         "priority_then_id",
	}); err != nil {
		t.Fatalf("seed category: %v", err)
	}
	if err := s.AddPolicy(ctx, "pol_vac", "vacation", "Test Vacation"); err != nil {
		t.Fatalf("seed policy: %v", err)
	}
	if err := s.AddPolicyVersion(ctx, "pol_vac:v1", "pol_vac", 1, "2024-01-01"); err != nil {
		t.Fatalf("seed policy version: %v", err)
	}
}

func mustPredJSON(jsonStr string) []byte {
	return []byte(jsonStr)
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
