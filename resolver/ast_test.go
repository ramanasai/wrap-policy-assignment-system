package resolver

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// ParsePredicate — structural validation
// ---------------------------------------------------------------------------

func TestParsePredicate_Valid(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		wantCls int
	}{
		{
			name:    "single eq clause",
			json:    `{"op":"and","clauses":[{"attr":"location","op":"eq","value":"US-CA"}]}`,
			wantCls: 1,
		},
		{
			name: "multiple clauses (the canonical CA tenure example)",
			json: `{"op":"and","clauses":[
				{"attr":"employment_type","op":"eq","value":"contractor"},
				{"attr":"location","op":"in","value":["US-CA"]},
				{"attr":"tenure_days","op":"gte","value":730}]}`,
			wantCls: 3,
		},
		{
			name:    "bool literal",
			json:    `{"op":"and","clauses":[{"attr":"is_manager","op":"eq","value":true}]}`,
			wantCls: 1,
		},
		{
			name:    "not_in with array",
			json:    `{"op":"and","clauses":[{"attr":"department","op":"not_in","value":["Sales"]}]}`,
			wantCls: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := ParsePredicate([]byte(tt.json))
			if err != nil {
				t.Fatalf("ParsePredicate() error = %v, want nil", err)
			}
			if len(p.Clauses) != tt.wantCls {
				t.Fatalf("got %d clauses, want %d", len(p.Clauses), tt.wantCls)
			}
			if p.Op != OpAnd {
				t.Fatalf("op = %q, want %q", p.Op, OpAnd)
			}
		})
	}
}

func TestParsePredicate_Invalid(t *testing.T) {
	tests := []struct {
		name       string
		json       string
		wantSubstr string // expected error substring
	}{
		{name: "malformed JSON", json: `{not json`, wantSubstr: "parse predicate"},
		{name: "wrong group op", json: `{"op":"or","clauses":[{"attr":"a","op":"eq","value":1}]}`, wantSubstr: `group op must be "and"`},
		{name: "empty clauses", json: `{"op":"and","clauses":[]}`, wantSubstr: "at least one clause"},
		{name: "missing clauses", json: `{"op":"and"}`, wantSubstr: "at least one clause"},
		{name: "empty attr", json: `{"op":"and","clauses":[{"attr":"","op":"eq","value":1}]}`, wantSubstr: "attr is required"},
		{name: "unknown operator", json: `{"op":"and","clauses":[{"attr":"a","op":"regex","value":"x"}]}`, wantSubstr: "unknown operator"},
		{name: "nil value", json: `{"op":"and","clauses":[{"attr":"a","op":"eq"}]}`, wantSubstr: "value is required"},
		{name: "null value", json: `{"op":"and","clauses":[{"attr":"a","op":"eq","value":null}]}`, wantSubstr: "value is required"},
		{name: "in with scalar", json: `{"op":"and","clauses":[{"attr":"a","op":"in","value":"x"}]}`, wantSubstr: "requires an array value"},
		{name: "not_in with scalar", json: `{"op":"and","clauses":[{"attr":"a","op":"not_in","value":5}]}`, wantSubstr: "requires an array value"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParsePredicate([]byte(tt.json))
			if err == nil {
				t.Fatalf("ParsePredicate() error = nil, want containing %q", tt.wantSubstr)
			}
			if !strings.Contains(err.Error(), tt.wantSubstr) {
				t.Fatalf("error = %q, want containing %q", err.Error(), tt.wantSubstr)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Validate — operator legality and value typing against definitions
// ---------------------------------------------------------------------------

func testDefs() map[string]AttributeDefinition {
	return map[string]AttributeDefinition{
		"location":        {Key: "location", ValueType: TypeString, AllowedOps: []ClauseOp{OpEq, OpNe, OpIn, OpNotIn}},
		"department":      {Key: "department", ValueType: TypeString, AllowedOps: []ClauseOp{OpEq, OpIn}},
		"employment_type": {Key: "employment_type", ValueType: TypeEnum, AllowedOps: []ClauseOp{OpEq, OpNe, OpIn}, EnumValues: []string{"full_time", "contractor", "intern"}},
		"tenure_days":     {Key: "tenure_days", ValueType: TypeNumber, AllowedOps: []ClauseOp{OpGTE, OpGT, OpLTE, OpLT, OpEq, OpIn}},
		"hire_date":       {Key: "hire_date", ValueType: TypeDate, AllowedOps: []ClauseOp{OpEq, OpGTE, OpLTE}},
		"is_manager":      {Key: "is_manager", ValueType: TypeBool, AllowedOps: []ClauseOp{OpEq}},
	}
}

func TestPredicateValidate_Accepts(t *testing.T) {
	defs := testDefs()
	valid := []string{
		`{"op":"and","clauses":[{"attr":"location","op":"eq","value":"US-CA"}]}`,
		`{"op":"and","clauses":[{"attr":"tenure_days","op":"gte","value":730}]}`,
		`{"op":"and","clauses":[{"attr":"tenure_days","op":"eq","value":0}]}`,
		`{"op":"and","clauses":[{"attr":"hire_date","op":"gte","value":"2024-01-19"}]}`,
		`{"op":"and","clauses":[{"attr":"is_manager","op":"eq","value":false}]}`,
		`{"op":"and","clauses":[{"attr":"employment_type","op":"in","value":["full_time","contractor"]}]}`,
		// attribute absent from defs: shape-only validation applies
		`{"op":"and","clauses":[{"attr":"custom_field","op":"eq","value":42}]}`,
	}
	for i, json := range valid {
		p, err := ParsePredicate([]byte(json))
		if err != nil {
			t.Fatalf("case %d: parse failed: %v", i, err)
		}
		if err := p.Validate(defs); err != nil {
			t.Errorf("case %d: Validate() = %v, want nil", i, err)
		}
	}
}

func TestPredicateValidate_Rejects(t *testing.T) {
	defs := testDefs()
	tests := []struct {
		name       string
		json       string
		wantSubstr string
	}{
		{
			name:       "operator not in AllowedOps (lte on string)",
			json:       `{"op":"and","clauses":[{"attr":"location","op":"lte","value":"US-CA"}]}`,
			wantSubstr: "not allowed",
		},
		{
			name:       "number expected, string given",
			json:       `{"op":"and","clauses":[{"attr":"tenure_days","op":"gte","value":"many"}]}`,
			wantSubstr: "expected number",
		},
		{
			name:       "number array with string element",
			json:       `{"op":"and","clauses":[{"attr":"tenure_days","op":"in","value":[1,"two"]}]}`,
			wantSubstr: "not a number",
		},
		{
			name:       "bad date format",
			json:       `{"op":"and","clauses":[{"attr":"hire_date","op":"eq","value":"01/19/2024"}]}`,
			wantSubstr: "expected date",
		},
		{
			name:       "bool expected, string given",
			json:       `{"op":"and","clauses":[{"attr":"is_manager","op":"eq","value":"yes"}]}`,
			wantSubstr: "expected bool",
		},
		{
			name:       "enum value outside allowed set",
			json:       `{"op":"and","clauses":[{"attr":"employment_type","op":"eq","value":"volunteer"}]}`,
			wantSubstr: "not in enum",
		},
		{
			name:       "enum in-list with one bad value",
			json:       `{"op":"and","clauses":[{"attr":"employment_type","op":"in","value":["full_time","ghost"]}]}`,
			wantSubstr: "not in enum",
		},
		{
			name:       "string value given to date attribute",
			json:       `{"op":"and","clauses":[{"attr":"hire_date","op":"eq","value":20240119}]}`,
			wantSubstr: "expected date",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := ParsePredicate([]byte(tt.json))
			if err != nil {
				t.Fatalf("parse failed: %v", err)
			}
			err = p.Validate(defs)
			if err == nil {
				t.Fatalf("Validate() = nil, want containing %q", tt.wantSubstr)
			}
			if !strings.Contains(err.Error(), tt.wantSubstr) {
				t.Fatalf("error = %q, want containing %q", err.Error(), tt.wantSubstr)
			}
		})
	}
}

func TestPredicateValidate_NilDefsIsDefensiveNoOp(t *testing.T) {
	p, err := ParsePredicate([]byte(`{"op":"and","clauses":[{"attr":"tenure_days","op":"gte","value":"many"}]}`))
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if err := p.Validate(nil); err != nil {
		t.Fatalf("Validate(nil) = %v, want nil (defensive mode)", err)
	}
}

// ---------------------------------------------------------------------------
// Matches — evaluation semantics
// ---------------------------------------------------------------------------

func testFacts() Facts {
	return Facts{
		EmployeeID: "emp_1",
		AsOf:       "2026-03-03",
		Attributes: map[string]any{
			"location":        "US-CA",
			"department":      "Engineering",
			"employment_type": "contractor",
			"tenure_days":     743,
			"hire_date":       "2024-01-19",
			"is_manager":      true,
			"salary":          120000.0,
		},
	}
}

func TestPredicateMatches_ScalarOperators(t *testing.T) {
	facts := testFacts()
	tests := []struct {
		name string
		clsr string
		want bool
	}{
		{name: "eq true", clsr: `{"attr":"location","op":"eq","value":"US-CA"}`, want: true},
		{name: "eq false", clsr: `{"attr":"location","op":"eq","value":"US-NY"}`, want: false},
		{name: "ne true", clsr: `{"attr":"location","op":"ne","value":"US-NY"}`, want: true},
		{name: "ne false", clsr: `{"attr":"location","op":"ne","value":"US-CA"}`, want: false},
		{name: "gte at boundary", clsr: `{"attr":"tenure_days","op":"gte","value":743}`, want: true},
		{name: "gte below", clsr: `{"attr":"tenure_days","op":"gte","value":730}`, want: true},
		{name: "gte above", clsr: `{"attr":"tenure_days","op":"gte","value":744}`, want: false},
		{name: "gt at boundary is exclusive", clsr: `{"attr":"tenure_days","op":"gt","value":743}`, want: false},
		{name: "lte at boundary", clsr: `{"attr":"tenure_days","op":"lte","value":743}`, want: true},
		{name: "lt at boundary is exclusive", clsr: `{"attr":"tenure_days","op":"lt","value":743}`, want: false},
		{name: "int-normalized float eq", clsr: `{"attr":"salary","op":"eq","value":120000}`, want: true},
		{name: "bool true", clsr: `{"attr":"is_manager","op":"eq","value":true}`, want: true},
		{name: "bool false", clsr: `{"attr":"is_manager","op":"eq","value":false}`, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := ParsePredicate([]byte(`{"op":"and","clauses":[` + tt.clsr + `]}`))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			got, whyNot := p.Matches(facts)
			if got != tt.want {
				t.Fatalf("Matches() = %v (%s), want %v", got, whyNot, tt.want)
			}
		})
	}
}

func TestPredicateMatches_MembershipOperators(t *testing.T) {
	facts := testFacts()
	tests := []struct {
		name string
		clsr string
		want bool
	}{
		{name: "in hit", clsr: `{"attr":"location","op":"in","value":["US-NY","US-CA"]}`, want: true},
		{name: "in miss", clsr: `{"attr":"location","op":"in","value":["US-NY","US-WA"]}`, want: false},
		{name: "not_in miss (member)", clsr: `{"attr":"location","op":"not_in","value":["US-CA"]}`, want: false},
		{name: "not_in hit (non-member)", clsr: `{"attr":"location","op":"not_in","value":["US-NY"]}`, want: true},
		{name: "numeric membership", clsr: `{"attr":"tenure_days","op":"in","value":[100, 743, 900]}`, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := ParsePredicate([]byte(`{"op":"and","clauses":[` + tt.clsr + `]}`))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			got, whyNot := p.Matches(facts)
			if got != tt.want {
				t.Fatalf("Matches() = %v (%s), want %v", got, whyNot, tt.want)
			}
		})
	}
}

func TestPredicateMatches_ANDSemantics(t *testing.T) {
	facts := testFacts()

	p, _ := ParsePredicate([]byte(`{"op":"and","clauses":[
		{"attr":"location","op":"eq","value":"US-CA"},
		{"attr":"tenure_days","op":"gte","value":730}]}`))
	if got, _ := p.Matches(facts); !got {
		t.Error("all clauses true should match")
	}

	p, _ = ParsePredicate([]byte(`{"op":"and","clauses":[
		{"attr":"location","op":"eq","value":"US-CA"},
		{"attr":"tenure_days","op":"gte","value":9999}]}`))
	if got, _ := p.Matches(facts); got {
		t.Error("one false clause must fail the AND")
	}
}

func TestPredicateMatches_WhyNotReasons(t *testing.T) {
	facts := testFacts()
	tests := []struct {
		name       string
		json       string
		wantSubstr string
	}{
		{
			name:       "missing attribute",
			json:       `{"op":"and","clauses":[{"attr":"office_floor","op":"eq","value":3}]}`,
			wantSubstr: `not present in facts`,
		},
		{
			name:       "value mismatch reports got",
			json:       `{"op":"and","clauses":[{"attr":"location","op":"eq","value":"US-NY"}]}`,
			wantSubstr: "clause failed: location eq US-NY (got US-CA)",
		},
		{
			name:       "type mismatch is an evaluation error not silent false",
			json:       `{"op":"and","clauses":[{"attr":"location","op":"gte","value":5}]}`,
			wantSubstr: "type mismatch",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := ParsePredicate([]byte(tt.json))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			got, whyNot := p.Matches(facts)
			if got {
				t.Fatalf("Matches() = true, want false with reason containing %q", tt.wantSubstr)
			}
			if !strings.Contains(whyNot, tt.wantSubstr) {
				t.Fatalf("whyNot = %q, want containing %q", whyNot, tt.wantSubstr)
			}
		})
	}
}

func TestPredicateMatches_VacuousEmptyPredicate(t *testing.T) {
	// Programmatically constructed zero-clause predicate matches everything.
	// Documented behavior: the resolver's Parse never produces this; callers
	// who build predicates by hand get matches-all, not a panic.
	p := Predicate{Op: OpAnd}
	got, whyNot := p.Matches(testFacts())
	if !got || whyNot != "" {
		t.Fatalf("empty predicate: got (%v, %q), want (true, \"\")", got, whyNot)
	}
}

func TestPredicateMatches_Deterministic(t *testing.T) {
	facts := testFacts()
	p, _ := ParsePredicate([]byte(`{"op":"and","clauses":[
		{"attr":"location","op":"in","value":["US-CA","US-NY"]},
		{"attr":"tenure_days","op":"gte","value":730}]}`))
	wantOK, wantWhy := p.Matches(facts)
	for i := 0; i < 100; i++ {
		gotOK, gotWhy := p.Matches(facts)
		if gotOK != wantOK || gotWhy != wantWhy {
			t.Fatalf("iteration %d diverged: (%v,%q) vs (%v,%q)", i, gotOK, gotWhy, wantOK, wantWhy)
		}
	}
}
