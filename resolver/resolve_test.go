package resolver

import (
	"encoding/json"
	"math/rand"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// fixtures
//
// Fixture predicates are compile-time literals parsed once at init; a failure
// there is test-code breakage and panics loudly. Rule construction is pure.
// ---------------------------------------------------------------------------

const catVacation = "time_off_vacation"

func mustPred(predicateJSON string) Predicate {
	pred, err := ParsePredicate([]byte(predicateJSON))
	if err != nil {
		panic("test fixture predicate broken: " + err.Error())
	}
	return pred
}

// The fixture predicate corpus.
var (
	predCA    = mustPred(`{"op":"and","clauses":[{"attr":"location","op":"eq","value":"US-CA"}]}`)
	predUS    = mustPred(`{"op":"and","clauses":[{"attr":"location","op":"in","value":["US-CA","US-NY","US-WA"]}]}`)
	predCAEng = mustPred(`{"op":"and","clauses":[{"attr":"location","op":"eq","value":"US-CA"},{"attr":"department","op":"eq","value":"Engineering"}]}`)
	predNY    = mustPred(`{"op":"and","clauses":[{"attr":"location","op":"eq","value":"US-NY"}]}`)
)

// rule builds an effective rule version for the given category.
func rule(id, categoryID string, createdAt time.Time, pred Predicate) RuleVersion {
	return RuleVersion{
		RuleID:          id,
		RuleVersionID:   id + ":v1",
		CategoryID:      categoryID,
		PolicyID:        "pol_" + id,
		PolicyVersionID: "pol_" + id + ":v1",
		Source:          SourceAuthored,
		CreatedAt:       createdAt,
		Predicate:       pred,
	}
}

func withManual(r RuleVersion) RuleVersion          { r.Source = SourceManual; return r }
func withPriority(r RuleVersion, p int) RuleVersion { r.Priority = p; return r }

func fixedTime(offset time.Duration) time.Time {
	return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Add(offset)
}

func singlePriorityCategory(id string) CategoryConfig {
	return CategoryConfig{ID: id, Cardinality: CardinalitySingle, ResolutionStrategy: StrategyPriorityRank}
}

func singleChoiceCategory(id string) CategoryConfig {
	return CategoryConfig{ID: id, Cardinality: CardinalitySingle, ResolutionStrategy: StrategyExplicitUserChoice}
}

func manyCategory(id string) CategoryConfig {
	return CategoryConfig{ID: id, Cardinality: CardinalityMany, ResolutionStrategy: StrategyAdditive}
}

func caFacts() Facts {
	return Facts{
		EmployeeID: "emp_1",
		Attributes: map[string]any{
			"location":        "US-CA",
			"department":      "Engineering",
			"employment_type": "full_time",
		},
	}
}

var defsStandard = map[string]AttributeDefinition{
	"location":        {Key: "location", ValueType: TypeString, AllowedOps: []ClauseOp{OpEq, OpNe, OpIn, OpNotIn}},
	"department":      {Key: "department", ValueType: TypeString, AllowedOps: []ClauseOp{OpEq, OpNe, OpIn}},
	"employment_type": {Key: "employment_type", ValueType: TypeEnum, AllowedOps: []ClauseOp{OpEq, OpNe, OpIn}, EnumValues: []string{"full_time", "contractor", "intern"}},
	"tenure_days":     {Key: "tenure_days", ValueType: TypeNumber, AllowedOps: []ClauseOp{OpGTE, OpGT, OpLTE, OpLT, OpEq}},
	"hire_date":       {Key: "hire_date", ValueType: TypeDate, AllowedOps: []ClauseOp{OpEq, OpGTE, OpLTE}},
}

// The canonical scenario: three vacation rules for a CA-based engineer.
// r_us is high-priority but broad; r_ca and r_ca_eng are narrower.
// Higher priority wins on the priority dimension, so r_us should WIN —
// this scenario exercises priority-dominance with two shadowed rules.
func shadowingScenario() Input {
	return Input{
		Category: singlePriorityCategory(catVacation),
		Date:     "2026-03-03",
		Facts:    caFacts(),
		Rules: []RuleVersion{
			withPriority(rule("r_us", catVacation, fixedTime(0), predUS), 5),
			withPriority(rule("r_ca", catVacation, fixedTime(1*time.Hour), predCA), 5),
			rule("r_ca_eng", catVacation, fixedTime(2*time.Hour), predCAEng),
		},
	}
}

func mustNoMatch(t *testing.T) Input {
	t.Helper()
	return Input{
		Category: singlePriorityCategory(catVacation),
		Date:     "2026-03-03",
		Facts:    caFacts(),
		Rules:    []RuleVersion{rule("r_ny", catVacation, fixedTime(0), predNY)},
	}
}

func choiceScenario(t *testing.T) Input {
	t.Helper()
	return Input{
		Category: singleChoiceCategory("manager"),
		Date:     "2026-03-03",
		Facts:    caFacts(),
		Rules: []RuleVersion{
			rule("m_a", "manager", fixedTime(0), predCA),
			rule("m_b", "manager", fixedTime(1*time.Hour), predCA),
		},
	}
}

// ---------------------------------------------------------------------------
// Core resolution scenarios
// ---------------------------------------------------------------------------

func TestResolve_NoMatch(t *testing.T) {
	res, err := Resolve(mustNoMatch(t))
	if err != nil {
		t.Fatalf("Resolve() = %v", err)
	}
	if res.Outcome != OutcomeNoMatch {
		t.Errorf("outcome = %q, want no_match", res.Outcome)
	}
	if len(res.Assignments) != 0 {
		t.Errorf("assignments = %v, want none", res.Assignments)
	}
	if len(res.Trace.Evaluated) != 1 || res.Trace.Evaluated[0].Matched {
		t.Errorf("trace should record the evaluated-but-unmatched rule: %+v", res.Trace.Evaluated)
	}
	if !strings.Contains(res.Trace.Evaluated[0].WhyNot, "clause failed") {
		t.Errorf("whyNot = %q", res.Trace.Evaluated[0].WhyNot)
	}
	if !strings.Contains(res.Trace.ShortAnswer, "No rule matched") {
		t.Errorf("short answer = %q", res.Trace.ShortAnswer)
	}
}

func TestResolve_SingleCleanAssignment(t *testing.T) {
	res, err := Resolve(Input{
		Category: singlePriorityCategory(catVacation),
		Date:     "2026-03-03",
		Facts:    caFacts(),
		Rules:    []RuleVersion{rule("r_ca", catVacation, fixedTime(0), predCA)},
	})
	if err != nil {
		t.Fatalf("Resolve() = %v", err)
	}
	if res.Outcome != OutcomeAssigned || len(res.Assignments) != 1 {
		t.Fatalf("outcome/assignments = %q/%v", res.Outcome, res.Assignments)
	}
	a := res.Assignments[0]
	if a.PolicyID != "pol_r_ca" || a.PolicyVersionID != "pol_r_ca:v1" || a.RuleID != "r_ca" {
		t.Errorf("assignment = %+v", a)
	}
	if len(res.Shadowed) != 0 || res.Options != nil {
		t.Errorf("clean assignment must have no shadowed matches or options: %v / %v", res.Shadowed, res.Options)
	}
}

func TestResolve_PriorityDominatesSpecificity(t *testing.T) {
	// r_us has priority 10 (broad), r_ca_eng priority 0 (narrow):
	// priority dimension wins first — r_us shadows the narrower rule.
	res, err := Resolve(Input{
		Category: singlePriorityCategory(catVacation),
		Date:     "2026-03-03",
		Facts:    caFacts(),
		Rules: []RuleVersion{
			withPriority(rule("r_us", catVacation, fixedTime(0), predUS), 10),
			rule("r_ca_eng", catVacation, fixedTime(2*time.Hour), predCAEng),
		},
	})
	if err != nil {
		t.Fatalf("Resolve() = %v", err)
	}
	if res.Outcome != OutcomeShadowed {
		t.Fatalf("outcome = %q, want shadowed", res.Outcome)
	}
	if res.Assignments[0].RuleID != "r_us" {
		t.Fatalf("winner = %s, want r_us (explicit priority dominates)", res.Assignments[0].RuleID)
	}
	if res.Shadowed[0].ByRuleID != "r_us" {
		t.Errorf("shadowed = %+v", res.Shadowed)
	}
}

func TestResolve_SpecificityTiebreak(t *testing.T) {
	// Equal priority: narrower CA+Engineering (spec 6) beats broad US (spec 2).
	specific := rule("r_ca_eng", catVacation, fixedTime(1*time.Hour), predCAEng)
	broad := rule("r_us", catVacation, fixedTime(0), predUS)

	res, err := Resolve(Input{
		Category: singlePriorityCategory(catVacation),
		Date:     "2026-03-03",
		Facts:    caFacts(),
		Rules:    []RuleVersion{broad, specific}, // deliberately unsorted input
	})
	if err != nil {
		t.Fatalf("Resolve() = %v", err)
	}
	if res.Outcome != OutcomeShadowed {
		t.Fatalf("outcome = %q, want shadowed", res.Outcome)
	}
	if len(res.Assignments) != 1 || res.Assignments[0].RuleID != "r_ca_eng" {
		t.Fatalf("winner = %+v, want r_ca_eng (narrower rule)", res.Assignments)
	}
	if len(res.Shadowed) != 1 || res.Shadowed[0].RuleID != "r_us" || res.Shadowed[0].ByRuleID != "r_ca_eng" {
		t.Fatalf("shadowed = %+v", res.Shadowed)
	}
	for _, e := range res.Trace.Evaluated {
		if e.RuleID == "r_us" {
			if !strings.Contains(e.WhyLost, "lost specificity tiebreak to r_ca_eng") {
				t.Errorf("r_us whyLost = %q", e.WhyLost)
			}
		}
	}
}

func TestResolve_ManualOverrideBeatsHigherPriority(t *testing.T) {
	// Invariant: manual wins dimension 1 — even against a higher-priority authored rule.
	authored := rule("r_high", catVacation, fixedTime(0), predCA)
	manual := withManual(rule("r_manual", catVacation, fixedTime(9*time.Hour), predCA))

	res, err := Resolve(Input{
		Category: singlePriorityCategory(catVacation),
		Date:     "2026-03-03",
		Facts:    caFacts(),
		Rules:    []RuleVersion{withPriority(authored, 1000), manual},
	})
	if err != nil {
		t.Fatalf("Resolve() = %v", err)
	}
	if res.Assignments[0].RuleID != "r_manual" {
		t.Fatalf("winner = %s, want manual override r_manual", res.Assignments[0].RuleID)
	}
	found := false
	for _, e := range res.Trace.Evaluated {
		if e.RuleID == "r_high" && strings.Contains(e.WhyLost, "lost to manual override") {
			found = true
		}
	}
	if !found {
		t.Error("trace must explain that the authored rule lost to the manual override")
	}
}

func TestResolve_ExplicitUserChoice_Conflict(t *testing.T) {
	// Manager-style category: two rules match → human decides, resolver ranks.
	res, err := Resolve(choiceScenario(t))
	if err != nil {
		t.Fatalf("Resolve() = %v", err)
	}
	if res.Outcome != OutcomeConflictNeedsDecision {
		t.Fatalf("outcome = %q, want conflict_needs_decision", res.Outcome)
	}
	if res.Assignments != nil {
		t.Errorf("resolver must not pick on the user's behalf: %v", res.Assignments)
	}
	if len(res.Options) != 2 {
		t.Fatalf("options = %+v, want 2 ranked options", res.Options)
	}
	// Equal everything → m_a wins rank 1 by id tiebreak.
	if res.Options[0].RuleID != "m_a" || res.Options[0].Rank != 1 || res.Options[1].Rank != 2 {
		t.Errorf("ranks = %+v", res.Options)
	}
}

func TestResolve_ExplicitUserChoice_SingleMatchResolvesCleanly(t *testing.T) {
	res, err := Resolve(Input{
		Category: singleChoiceCategory("manager"),
		Date:     "2026-03-03",
		Facts:    caFacts(),
		Rules:    []RuleVersion{rule("m_a", "manager", fixedTime(0), predCA)},
	})
	if err != nil {
		t.Fatalf("Resolve() = %v", err)
	}
	if res.Outcome != OutcomeAssigned || len(res.Assignments) != 1 {
		t.Fatalf("single match under choice strategy: %q/%v", res.Outcome, res.Assignments)
	}
}

func TestResolve_AdditiveStacksEverything(t *testing.T) {
	res, err := Resolve(Input{
		Category: manyCategory("app_access"),
		Date:     "2026-03-03",
		Facts:    caFacts(),
		Rules: []RuleVersion{
			rule("r_slack", "app_access", fixedTime(0), predCA),
			rule("r_figma", "app_access", fixedTime(0), predCA),
			rule("r_github", "app_access", fixedTime(0), predCA),
		},
	})
	if err != nil {
		t.Fatalf("Resolve() = %v", err)
	}
	if res.Outcome != OutcomeAssigned {
		t.Fatalf("outcome = %q, want assigned", res.Outcome)
	}
	if len(res.Assignments) != 3 {
		t.Fatalf("additive must stack all matches: %v", res.Assignments)
	}
	if len(res.Shadowed) != 0 || res.Options != nil {
		t.Errorf("additive has no shadowing/options: %v / %v", res.Shadowed, res.Options)
	}
	// Output order follows the deterministic total order (ids ascending here).
	if res.Assignments[0].RuleID != "r_figma" || res.Assignments[1].RuleID != "r_github" || res.Assignments[2].RuleID != "r_slack" {
		t.Errorf("deterministic additive order violated: %v", res.Assignments)
	}
}

func TestResolve_AdditiveDedupesSamePolicy(t *testing.T) {
	// Two additive rules pointing at the SAME policy (tenure-based AND
	// segment-based security training): the policy is materialized once;
	// the trace still covers both rules. Regression: this previously
	// produced duplicate policy rows that exploded the projection PK.
	tenure := rule("r_sec_tenure", "compliance_training", fixedTime(0),
		mustPred(`{"op":"and","clauses":[{"attr":"tenure_days","op":"gte","value":365}]}`))
	segment := rule("r_sec_segment", "compliance_training", fixedTime(1*time.Hour),
		mustPred(`{"op":"and","clauses":[{"attr":"segments","op":"contains","value":"field_ops"}]}`))
	// Both map to the same policy AND policy version (as in production, where
	// version ids come from the DB row, not the rule id).
	tenure.PolicyID = "pol_train_security"
	segment.PolicyID = "pol_train_security"
	tenure.PolicyVersionID = "pol_train_security:v1"
	segment.PolicyVersionID = "pol_train_security:v1"

	res, err := Resolve(Input{
		Category: manyCategory("compliance_training"),
		Date:     "2026-03-03",
		Facts: Facts{EmployeeID: "e", Attributes: map[string]any{
			"tenure_days": 500,
			"segments":    []any{"field_ops"},
		}},
		Rules: []RuleVersion{tenure, segment},
	})
	if err != nil {
		t.Fatalf("Resolve() = %v", err)
	}
	if len(res.Assignments) != 1 {
		t.Fatalf("additive dedup: got %d assignments, want 1: %+v", len(res.Assignments), res.Assignments)
	}
	if res.Assignments[0].PolicyID != "pol_train_security" {
		t.Fatalf("assignment = %+v", res.Assignments[0])
	}
	// Both rules evaluated (trace completeness preserved).
	matched := 0
	for _, e := range res.Trace.Evaluated {
		if e.Matched {
			matched++
		}
	}
	if matched != 2 {
		t.Fatalf("trace matched %d rules, want 2 (both evaluated)", matched)
	}
}

func TestResolve_RulesFromOtherCategoriesAreIgnored(t *testing.T) {
	other := rule("r_other", "app_access", fixedTime(0), predCA)
	mine := rule("r_mine", catVacation, fixedTime(0), predCA)

	res, err := Resolve(Input{
		Category: singlePriorityCategory(catVacation),
		Date:     "2026-03-03",
		Facts:    caFacts(),
		Rules:    []RuleVersion{other, mine},
	})
	if err != nil {
		t.Fatalf("Resolve() = %v", err)
	}
	if len(res.Assignments) != 1 || res.Assignments[0].RuleID != "r_mine" {
		t.Fatalf("cross-category leakage: %v", res.Assignments)
	}
	for _, id := range res.Trace.PolicySnapshot.RuleVersionIDs {
		if id == "r_other:v1" {
			t.Error("ignored rule leaked into the policy snapshot")
		}
	}
}

// ---------------------------------------------------------------------------
// Temporal integration — tenure derivation through the full pipeline
// ---------------------------------------------------------------------------

func TestResolve_TenureCrossing(t *testing.T) {
	// The canonical scenario: 2-year tenure gate for the enhanced vacation policy.
	asOf := "2026-03-03"
	hireAtCrossing := mustDate(asOf).AddDate(0, 0, -730).Format("2006-01-02") // exactly 730 days
	hireJustBefore := mustDate(asOf).AddDate(0, 0, -729).Format("2006-01-02") // 729 days

	ruleTenure := rule("r_tenure", catVacation, fixedTime(0),
		mustPred(`{"op":"and","clauses":[{"attr":"tenure_days","op":"gte","value":730}]}`))

	atCrossing, err := Resolve(Input{
		Category:    singlePriorityCategory(catVacation),
		Date:        asOf,
		Facts:       Facts{EmployeeID: "emp_1", Attributes: map[string]any{"hire_date": hireAtCrossing}},
		Rules:       []RuleVersion{ruleTenure},
		Definitions: defsStandard,
	})
	if err != nil {
		t.Fatalf("Resolve(at crossing) = %v", err)
	}
	if atCrossing.Outcome != OutcomeAssigned {
		t.Fatalf("tenure exactly 730 must match: %q (%s)", atCrossing.Outcome, atCrossing.Trace.ShortAnswer)
	}

	before, err := Resolve(Input{
		Category:    singlePriorityCategory(catVacation),
		Date:        asOf,
		Facts:       Facts{EmployeeID: "emp_1", Attributes: map[string]any{"hire_date": hireJustBefore}},
		Rules:       []RuleVersion{ruleTenure},
		Definitions: defsStandard,
	})
	if err != nil {
		t.Fatalf("Resolve(day before crossing) = %v", err)
	}
	if before.Outcome != OutcomeNoMatch {
		t.Fatalf("tenure 729 must not match a 730 gate: %q", before.Outcome)
	}
	if !strings.Contains(before.Trace.Evaluated[0].WhyNot, "clause failed: tenure_days gte 730") {
		t.Errorf("whyNot = %q", before.Trace.Evaluated[0].WhyNot)
	}
}

func mustDate(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

// ---------------------------------------------------------------------------
// Input validation
// ---------------------------------------------------------------------------

func TestResolve_ValidationErrors(t *testing.T) {
	good := rule("r1", catVacation, fixedTime(0), predCA)

	tests := []struct {
		name       string
		input      Input
		wantSubstr string
	}{
		{
			name: "invalid date format",
			input: Input{
				Category: singlePriorityCategory(catVacation), Date: "03/03/2026",
				Facts: caFacts(), Rules: []RuleVersion{good},
			},
			wantSubstr: "YYYY-MM-DD",
		},
		{
			name: "single + additive is a config error",
			input: Input{
				Category: CategoryConfig{ID: catVacation, Cardinality: CardinalitySingle, ResolutionStrategy: StrategyAdditive},
				Date:     "2026-03-03", Facts: caFacts(), Rules: []RuleVersion{good},
			},
			wantSubstr: "forbids strategy",
		},
		{
			name: "many + priority_rank is a config error",
			input: Input{
				Category: CategoryConfig{ID: "x", Cardinality: CardinalityMany, ResolutionStrategy: StrategyPriorityRank},
				Date:     "2026-03-03", Facts: caFacts(), Rules: []RuleVersion{good},
			},
			wantSubstr: "requires strategy",
		},
		{
			name: "empty category id",
			input: Input{
				Category: CategoryConfig{}, Date: "2026-03-03",
				Facts: caFacts(), Rules: []RuleVersion{good},
			},
			wantSubstr: "category id is required",
		},
		{
			name: "strict predicate validation surfaces bad operators with rule id",
			input: Input{
				Category:    singlePriorityCategory(catVacation),
				Date:        "2026-03-03",
				Facts:       caFacts(),
				Rules:       []RuleVersion{rule("r_bad", catVacation, fixedTime(0), mustPred(`{"op":"and","clauses":[{"attr":"location","op":"lte","value":"US-CA"}]}`))},
				Definitions: defsStandard,
			},
			wantSubstr: "r_bad",
		},
		{
			name: "invalid hire_date with tenure-referencing rules errors",
			input: Input{
				Category: singlePriorityCategory(catVacation),
				Date:     "2026-03-03",
				Facts:    Facts{EmployeeID: "e", Attributes: map[string]any{"hire_date": "garbage"}},
				Rules: []RuleVersion{rule("r_t", catVacation, fixedTime(0),
					mustPred(`{"op":"and","clauses":[{"attr":"tenure_days","op":"gte","value":1}]}`))},
			},
			wantSubstr: "derive tenure_days",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Resolve(tt.input)
			if err == nil {
				t.Fatalf("Resolve() error = nil, want containing %q", tt.wantSubstr)
			}
			if !strings.Contains(err.Error(), tt.wantSubstr) {
				t.Fatalf("error = %q, want containing %q", err.Error(), tt.wantSubstr)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Determinism invariants — the properties the whole design rests on
// ---------------------------------------------------------------------------

func TestResolve_SameInputByteIdenticalOutput(t *testing.T) {
	in := shadowingScenario()
	r1, err := Resolve(in)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	r2, err := Resolve(in)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	b1, _ := json.Marshal(r1)
	b2, _ := json.Marshal(r2)
	if string(b1) != string(b2) {
		t.Fatalf("determinism violated:\nrun1: %s\nrun2: %s", b1, b2)
	}
}

func TestResolve_PermutationInvariance(t *testing.T) {
	// Property: input ORDER of rules must never affect output.
	// Seeded shuffles, 200 permutations, byte-compare.
	base := shadowingScenario()
	want, err := Resolve(base)
	if err != nil {
		t.Fatalf("baseline: %v", err)
	}
	wantJSON, _ := json.Marshal(want)

	rng := rand.New(rand.NewSource(7))
	for iter := 0; iter < 200; iter++ {
		shuffled := append([]RuleVersion(nil), base.Rules...)
		rng.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
		got, err := Resolve(Input{Category: base.Category, Date: base.Date, Facts: base.Facts, Rules: shuffled})
		if err != nil {
			t.Fatalf("permutation %d: %v", iter, err)
		}
		gotJSON, _ := json.Marshal(got)
		if string(gotJSON) != string(wantJSON) {
			t.Fatalf("permutation %d diverged:\nwant: %s\ngot:  %s", iter, wantJSON, gotJSON)
		}
	}
}

func TestResolve_TraceSnapshotImmutableToCallerMutation(t *testing.T) {
	in := shadowingScenario()
	facts := in.Facts.Attributes
	res, err := Resolve(in)
	if err != nil {
		t.Fatalf("Resolve() = %v", err)
	}
	before, _ := json.Marshal(res.Trace.FactsSnapshot)

	// Caller mutates their facts AFTER resolving — the recorded snapshot
	// (what was decided at the time) must not move.
	facts["location"] = "US-NY"

	after, _ := json.Marshal(res.Trace.FactsSnapshot)
	if string(before) != string(after) {
		t.Fatalf("trace snapshot mutated by caller:\nbefore: %s\nafter:  %s", before, after)
	}
}

func TestResolve_TraceRanksContiguous(t *testing.T) {
	res, err := Resolve(shadowingScenario())
	if err != nil {
		t.Fatalf("Resolve() = %v", err)
	}
	// Trace order is RuleID-sorted, so ranks arrive out of order; verify the
	// SET of ranks over all matched rules is exactly {1..matched}.
	ranks := map[int]bool{}
	matched := 0
	for _, e := range res.Trace.Evaluated {
		if e.Matched {
			matched++
			ranks[e.Rank] = true
		} else if e.Rank != 0 {
			t.Errorf("unmatched rule carries a rank: %+v", e)
		}
	}
	if len(ranks) != matched {
		t.Fatalf("rank coverage mismatch: %v vs matched=%d", ranks, matched)
	}
	for r := 1; r <= matched; r++ {
		if !ranks[r] {
			t.Fatalf("rank %d missing from %v (trace: %+v)", r, ranks, res.Trace.Evaluated)
		}
	}
}

func TestResolve_TraceOutcomeMatchesResultOutcome(t *testing.T) {
	scenarios := []struct {
		name  string
		input Input
		want  string
	}{
		{"shadowed", shadowingScenario(), OutcomeShadowed},
		{"no match", mustNoMatch(t), OutcomeNoMatch},
		{"decision", choiceScenario(t), OutcomeConflictNeedsDecision},
	}
	for _, s := range scenarios {
		t.Run(s.name, func(t *testing.T) {
			res, err := Resolve(s.input)
			if err != nil {
				t.Fatalf("Resolve() = %v", err)
			}
			if res.Trace.Outcome != res.Outcome || res.Outcome != s.want {
				t.Fatalf("outcome=%q trace=%q want=%q", res.Outcome, res.Trace.Outcome, s.want)
			}
		})
	}
}
