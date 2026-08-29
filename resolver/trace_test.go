package resolver

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// copyAttrs — snapshot semantics
// ---------------------------------------------------------------------------

func TestCopyAttrs_DeepCopiesSliceValues(t *testing.T) {
	src := map[string]any{
		"tags":     []any{"a", "b"},
		"location": "US-CA",
		"level":    5,
	}
	snap := copyAttrs(src)

	// Mutating the snapshot's slice must not affect the source...
	snap["tags"].([]any)[0] = "MUTATED"
	if src["tags"].([]any)[0] != "a" {
		t.Fatal("copyAttrs is shallow for slices — snapshot leaked mutation")
	}
	// ...and mutating the source must not affect the snapshot.
	src["tags"].([]any)[0] = "SOURCE-MUT"
	if snap["tags"].([]any)[0] != "MUTATED" {
		t.Fatal("snapshot aliased the source slice")
	}
	// Scalars are value-copied in Go maps anyway; assert presence.
	if snap["location"] != "US-CA" || snap["level"] != 5 {
		t.Fatalf("snapshot lost scalar values: %v", snap)
	}
}

func TestCopyAttrs_EmptyAndNil(t *testing.T) {
	if got := copyAttrs(nil); len(got) != 0 {
		t.Fatalf("copyAttrs(nil) = %v, want empty non-nil", got)
	}
	if got := copyAttrs(map[string]any{}); len(got) != 0 {
		t.Fatalf("copyAttrs(empty) = %v, want empty", got)
	}
}

// ---------------------------------------------------------------------------
// buildEvaluations — trace construction semantics
// ---------------------------------------------------------------------------

func evalRules(t *testing.T) ([]RuleVersion, map[string]matchResult, []RankedRule) {
	t.Helper()
	rules := []RuleVersion{
		mkRule("r1", SourceAuthored, 0, 0, eqPred()),  // will match, wins (id tiebreak)
		mkRule("r2", SourceAuthored, 0, 0, eqPred()),   // will match, shadowed
		mkRule("r3", SourceAuthored, 0, 0, nePred()),   // will match, shadowed (spec 1)
		mkRule("r4", SourceAuthored, 0, 0, mkPredByName(t, "missing")), // not matched
	}
	mr := map[string]matchResult{
		"r1": {matched: true},
		"r2": {matched: true},
		"r3": {matched: true},
		"r4": {matched: false, whyNot: "attribute \"nope\" not present in facts"},
	}
	ranked := SortMatched([]RuleVersion{rules[0], rules[1], rules[2]})
	return rules, mr, ranked
}

func mkPredByName(t *testing.T, name string) Predicate {
	t.Helper()
	switch name {
	case "missing":
		return Predicate{Op: OpAnd, Clauses: []Clause{
			{Attr: "nope", Op: OpEq, Value: 1},
		}}
	}
	return eqPred()
}

func TestBuildEvaluations_ShadowingTrace(t *testing.T) {
	rules, mr, ranked := evalRules(t)
	// Rules must be presented in RuleID-sorted order (the deterministic order).
	sortedRules := []RuleVersion{rules[0], rules[1], rules[2], rules[3]}

	ev := buildEvaluations(sortedRules, mr, ranked, StrategyPriorityRank, false)

	if len(ev) != 4 {
		t.Fatalf("got %d evaluations, want 4", len(ev))
	}
	byID := map[string]RuleEvaluation{}
	for _, e := range ev {
		byID[e.RuleID] = e
	}

	if e := byID["r1"]; e.Outcome != RuleOutcomeWinner || e.Rank != 1 {
		t.Errorf("r1 = %+v, want winner rank 1", e)
	}
	if e := byID["r2"]; e.Outcome != RuleOutcomeShadowed || e.Rank != 2 {
		t.Errorf("r2 = %+v, want shadowed rank 2", e)
	}
	if !strings.Contains(byID["r2"].WhyLost, "lost id tiebreak to r1") {
		t.Errorf("r2 whyLost = %q", byID["r2"].WhyLost)
	}
	if e := byID["r3"]; e.Outcome != RuleOutcomeShadowed {
		t.Errorf("r3 outcome = %q, want shadowed", e.Outcome)
	}
	if !strings.Contains(byID["r3"].WhyLost, "specificity") {
		t.Errorf("r3 whyLost should name specificity, got %q", byID["r3"].WhyLost)
	}
	if e := byID["r4"]; e.Matched || e.Outcome != RuleOutcomeNotMatched || e.WhyNot == "" {
		t.Errorf("r4 = %+v, want not_matched with whyNot", e)
	}
}

func TestBuildEvaluations_AdditiveHasNoShadowing(t *testing.T) {
	rules, mr, ranked := evalRules(t)
	// Additive: every matched rule is a winner (stacks).
	ev := buildEvaluations(rules[:3], mr, ranked, StrategyAdditive, false)
	for _, e := range ev {
		if e.Outcome != RuleOutcomeWinner {
			t.Errorf("%s outcome = %q, want winner (additive stacks)", e.RuleID, e.Outcome)
		}
		if e.WhyLost != "" {
			t.Errorf("%s has whyLost in additive mode: %q", e.RuleID, e.WhyLost)
		}
	}
}

func TestBuildEvaluations_ExplicitUserChoiceNeedsDecision(t *testing.T) {
	rules, mr, ranked := evalRules(t)
	ev := buildEvaluations(rules[:3], mr, ranked, StrategyExplicitUserChoice, true)
	for _, e := range ev {
		if e.Outcome != RuleOutcomeNeedsDecision {
			t.Errorf("%s outcome = %q, want needs_decision", e.RuleID, e.Outcome)
		}
		if e.Rank == 0 {
			t.Errorf("%s missing rank", e.RuleID)
		}
	}
	// Ranks must follow the deterministic order: r1 (priority 10) first.
	if ev[0].RuleID != "r1" || ev[0].Rank != 1 {
		t.Errorf("first option = %+v, want r1 rank 1", ev[0])
	}
}

func TestBuildEvaluations_RanksContiguous(t *testing.T) {
	rules, mr, ranked := evalRules(t)
	ev := buildEvaluations(rules, mr, ranked, StrategyPriorityRank, false)
	seen := map[int]bool{}
	for _, e := range ev {
		if e.Rank != 0 { // 0 = not ranked (not matched)
			if e.Rank > 3 || seen[e.Rank] {
				t.Fatalf("ranks not contiguous 1..3: %v", ranksOf(ev))
			}
			seen[e.Rank] = true
		}
	}
	if len(seen) != 3 {
		t.Fatalf("expected 3 ranked rules, got %v", ranksOf(ev))
	}
}

// ---------------------------------------------------------------------------
// shortAnswer — headline explanation layer
// ---------------------------------------------------------------------------

func TestShortAnswer_PerOutcome(t *testing.T) {
	ranked := SortMatched([]RuleVersion{
		mkRule("r1", SourceAuthored, 10, 0, eqPred()),
		mkRule("r2", SourceAuthored, 0, 0, eqPred()),
	})

	tests := []struct {
		name       string
		outcome    string
		ranked     []RankedRule
		strategy   ResolutionStrategy
		needDec    bool
		wantSubstr string
	}{
		{"assigned", OutcomeAssigned, ranked, StrategyPriorityRank, false, "r1 matched and was assigned cleanly"},
		{"shadowed", OutcomeShadowed, ranked, StrategyPriorityRank, false, "shadowed"},
		{"no match", OutcomeNoMatch, nil, StrategyPriorityRank, false, "No rule matched"},
		{"needs decision", OutcomeConflictNeedsDecision, ranked, StrategyExplicitUserChoice, true, "explicit decision"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shortAnswer(tt.outcome, tt.ranked, tt.strategy, tt.needDec)
			if !strings.Contains(got, tt.wantSubstr) {
				t.Fatalf("shortAnswer = %q, want containing %q", got, tt.wantSubstr)
			}
		})
	}
}

// ---------------------------------------------------------------------------

func ranksOf(ev []RuleEvaluation) []int {
	out := []int{}
	for _, e := range ev {
		if e.Rank != 0 {
			out = append(out, e.Rank)
		}
	}
	return out
}
