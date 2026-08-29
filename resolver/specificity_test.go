package resolver

import (
	"strings"
	"testing"
	"time"
)

// mkPred builds a predicate from clause JSON fragments.
func mkPred(t *testing.T, clauses ...string) Predicate {
	t.Helper()
	return Predicate{Op: OpAnd, Clauses: mustClauses(t, clauses...)}
}

func mustClauses(t *testing.T, fragments ...string) []Clause {
	t.Helper()
	raw := `{"op":"and","clauses":[` + strings.Join(fragments, ",") + `]}`
	p, err := ParsePredicate([]byte(raw))
	if err != nil {
		t.Fatalf("mkPred parse: %v", err)
	}
	return p.Clauses
}

func TestSpecificity_WeightsPerOperator(t *testing.T) {
	tests := []struct {
		name  string
		p     Predicate
		score int
	}{
		{"empty predicate scores 0", Predicate{Op: OpAnd}, 0},
		{"eq = 3", mkPred(t, `{"attr":"a","op":"eq","value":1}`), 3},
		{"ne = 1", mkPred(t, `{"attr":"a","op":"ne","value":1}`), 1},
		{"in = 2", mkPred(t, `{"attr":"a","op":"in","value":[1,2]}`), 2},
		{"not_in = 1", mkPred(t, `{"attr":"a","op":"not_in","value":[1]}`), 1},
		{"gte = 2", mkPred(t, `{"attr":"a","op":"gte","value":1}`), 2},
		{"gt = 2", mkPred(t, `{"attr":"a","op":"gt","value":1}`), 2},
		{"lte = 2", mkPred(t, `{"attr":"a","op":"lte","value":1}`), 2},
		{"lt = 2", mkPred(t, `{"attr":"a","op":"lt","value":1}`), 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Specificity(tt.p); got != tt.score {
				t.Errorf("Specificity = %d, want %d", got, tt.score)
			}
		})
	}
}

func TestSpecificity_CanonicalCATenureRule(t *testing.T) {
	// The seed rule: employment_type eq + location eq + tenure gte = 3+3+2 = 8.
	p := mkPred(t,
		`{"attr":"employment_type","op":"eq","value":"contractor"}`,
		`{"attr":"location","op":"eq","value":"US-CA"}`,
		`{"attr":"tenure_days","op":"gte","value":730}`,
	)
	if got := Specificity(p); got != 8 {
		t.Errorf("Specificity = %d, want 8", got)
	}
}

func TestSpecificity_MoreConjunctsNarrowsMore(t *testing.T) {
	one := mkPred(t, `{"attr":"location","op":"eq","value":"US-CA"}`)
	two := mkPred(t,
		`{"attr":"location","op":"eq","value":"US-CA"}`,
		`{"attr":"department","op":"eq","value":"Engineering"}`,
	)
	if Specificity(two) <= Specificity(one) {
		t.Errorf("two-conjunct (%d) must outrank one-conjunct (%d)", Specificity(two), Specificity(one))
	}
}

func TestSpecificity_EqOutranksExclusionAtSameCount(t *testing.T) {
	// 2 clauses: eq+eq (6) vs ne+ne (2). The exact-match rule is narrower.
	exact := mkPred(t,
		`{"attr":"location","op":"eq","value":"US-CA"}`,
		`{"attr":"department","op":"eq","value":"Engineering"}`,
	)
	excl := mkPred(t,
		`{"attr":"location","op":"ne","value":"US-CA"}`,
		`{"attr":"department","op":"ne","value":"Sales"}`,
	)
	if Specificity(exact) <= Specificity(excl) {
		t.Errorf("exact (%d) must outrank exclusion (%d)", Specificity(exact), Specificity(excl))
	}
}

func TestSpecificity_PureFunctionOfAST(t *testing.T) {
	p := mkPred(t,
		`{"attr":"location","op":"eq","value":"US-CA"}`,
		`{"attr":"tenure_days","op":"gte","value":730}`,
	)
	want := Specificity(p)
	for i := 0; i < 100; i++ {
		if got := Specificity(p); got != want {
			t.Fatalf("iteration %d: %d != %d", i, got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// tie-break dimension tests use hand-built rules (no predicate parsing noise)
// ---------------------------------------------------------------------------

// mkRule builds a rule with the given fields and an eq predicate (specificity 3)
// unless the caller supplies pred. Shared by all resolver test files.
func mkRule(id string, source Source, priority int, offset time.Duration, pred Predicate) RuleVersion {
	return RuleVersion{
		RuleID:     id,
		Source:     source,
		Priority:   priority,
		CreatedAt:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Add(offset),
		Predicate:  pred,
		CategoryID: "test",
	}
}

func eqPred() Predicate {
	return Predicate{Op: OpAnd, Clauses: []Clause{
		{Attr: "a", Op: OpEq, Value: 1},
	}}
}

func nePred() Predicate {
	return Predicate{Op: OpAnd, Clauses: []Clause{
		{Attr: "a", Op: OpNe, Value: 1},
	}}
}
