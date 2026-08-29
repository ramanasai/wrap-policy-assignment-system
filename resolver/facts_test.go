package resolver

import (
	"strings"
	"testing"
	"time"
)

func date(s string) time.Time {
	t, _ := time.Parse(dateLayout, s)
	return t
}

func TestDeriveAttributes_ComputesTenure(t *testing.T) {
	// Boundary-verified: asOf minus exactly 730 days → tenure 730.
	asOf := date("2026-03-03")
	hire := asOf.AddDate(0, 0, -730).Format(dateLayout)

	out, err := DeriveAttributes(map[string]any{"hire_date": hire}, asOf)
	if err != nil {
		t.Fatalf("DeriveAttributes() = %v", err)
	}
	if got := out[AttrTenureDay]; got != 730 {
		t.Errorf("tenure_days = %v, want 730", got)
	}
}

func TestDeriveAttributes_ZeroDayBoundary(t *testing.T) {
	// Same-day: tenure 0 (must exist and be an int, not float).
	out, err := DeriveAttributes(map[string]any{"hire_date": "2026-03-03"}, date("2026-03-03"))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got, ok := out[AttrTenureDay].(int); !ok || got != 0 {
		t.Errorf("tenure on hire day = %#v, want int 0", out[AttrTenureDay])
	}
}

func TestDeriveAttributes_OneDay(t *testing.T) {
	out, err := DeriveAttributes(map[string]any{"hire_date": "2026-03-02"}, date("2026-03-03"))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got := out[AttrTenureDay]; got != 1 {
		t.Errorf("tenure = %v, want 1", got)
	}
}

func TestDeriveAttributes_FutureHireClampedToZero(t *testing.T) {
	// Future hires have tenure 0, not negative — negative tenure would make
	// "gte 0" rules behave nonsensically for pre-start employees.
	out, err := DeriveAttributes(map[string]any{"hire_date": "2026-12-01"}, date("2026-03-03"))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got := out[AttrTenureDay]; got != 0 {
		t.Errorf("tenure = %v, want 0 (clamped)", got)
	}
}

func TestDeriveAttributes_ExplicitTenureWins(t *testing.T) {
	// Explicit beats derived — mirrors manual-override philosophy.
	out, err := DeriveAttributes(
		map[string]any{"hire_date": "2024-01-19", "tenure_days": 42},
		date("2026-03-03"),
	)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got := out[AttrTenureDay]; got != 42 {
		t.Errorf("tenure = %v, want explicit 42", got)
	}
}

func TestDeriveAttributes_NoHireDate_NoOp(t *testing.T) {
	attrs := map[string]any{"location": "US-CA"}
	out, err := DeriveAttributes(attrs, date("2026-03-03"))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if _, has := out[AttrTenureDay]; has {
		t.Error("tenure derived without hire_date")
	}
	if len(out) != 1 {
		t.Errorf("unexpected attrs injected: %v", out)
	}
}

func TestDeriveAttributes_InvalidHireDateErrors(t *testing.T) {
	// Garbage hire_date + absent tenure = loud error, not silent skip —
	// a resolver run against a broken snapshot must not silently misclassify.
	_, err := DeriveAttributes(map[string]any{"hire_date": "01/19/2024"}, date("2026-03-03"))
	if err == nil || !strings.Contains(err.Error(), "derive tenure_days") {
		t.Fatalf("err = %v, want derivation error", err)
	}
}

func TestDeriveAttributes_InvalidHireDateWithExplicitTenureIsFine(t *testing.T) {
	// Explicit tenure wins BEFORE we ever look at hire_date — no error.
	out, err := DeriveAttributes(
		map[string]any{"hire_date": "garbage", "tenure_days": 10},
		date("2026-03-03"),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := out[AttrTenureDay]; got != 10 {
		t.Errorf("tenure = %v, want 10", got)
	}
}

func TestDeriveAttributes_NonStringHireDateErrors(t *testing.T) {
	_, err := DeriveAttributes(map[string]any{"hire_date": 20240119}, date("2026-03-03"))
	if err == nil || !strings.Contains(err.Error(), "expected string") {
		t.Fatalf("err = %v, want type error", err)
	}
}

func TestDeriveAttributes_InputNotMutated(t *testing.T) {
	// Snapshot semantics: the caller's facts map must be untouched.
	attrs := map[string]any{"hire_date": "2024-01-19"}
	if _, err := DeriveAttributes(attrs, date("2026-03-03")); err != nil {
		t.Fatalf("err = %v", err)
	}
	if _, has := attrs[AttrTenureDay]; has {
		t.Fatal("DeriveAttributes mutated its input map")
	}
	if len(attrs) != 1 {
		t.Fatalf("input map grew: %v", attrs)
	}
}
