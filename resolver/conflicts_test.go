package resolver

import (
	"math/rand"
	"sort"
	"reflect"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Compare — one test per dimension of the total order
// (manual > priority > specificity > created_at > id)
// ---------------------------------------------------------------------------

// better reports whether a ranks BEFORE b (wins).
func better(a, b RankedRule) bool { return Compare(a, b) < 0 }

func TestCompare_ManualOverridesEverything(t *testing.T) {
	// Manual with WORSE priority, specificity, and a later creation date
	// must still outrank the authored rule (invariant: manual wins, dimension 1).
	manual := mkRule("m1", SourceManual, -100, 99*time.Hour, eqPred())  // specificity 3
	authored := mkRule("a1", SourceAuthored, 1000, 0, nePred()) // specificity 1
	if !better(RankedRule{manual, 3}, RankedRule{authored, 1}) {
		t.Fatal("manual must outrank authored regardless of other dimensions")
	}
	if better(RankedRule{authored, 1}, RankedRule{manual, 3}) {
		t.Fatal("antisymmetry violated for manual dimension")
	}
	// system source is NOT special — authored vs system resolve by later dims.
	sys := mkRule("s1", SourceSystem, 0, 0, eqPred())
	if better(RankedRule{sys, 3}, RankedRule{authored, 3}) == better(RankedRule{authored, 3}, RankedRule{sys, 3}) {
		// both false impossible; both true impossible — sanity only
		t.Fatal("compare produced symmetric result for distinct rules")
	}
}

func TestCompare_PriorityHigherWins(t *testing.T) {
	hi := mkRule("hi", SourceAuthored, 10, 0, eqPred())  // created later
	lo := mkRule("lo", SourceAuthored, 5, 1*time.Hour, eqPred())
	if !better(RankedRule{hi, 3}, RankedRule{lo, 3}) {
		t.Fatal("priority 10 must outrank priority 5")
	}
	// Priority beats creation order: earlier-created but lower-priority loses.
	if better(RankedRule{lo, 3}, RankedRule{hi, 3}) {
		t.Fatal("earlier creation must not beat higher priority")
	}
}

func TestCompare_SpecificityHigherWins(t *testing.T) {
	// Equal priority: the narrower (eq, spec 3) rule beats the broader (ne, spec 1).
	narrow := mkRule("n", SourceAuthored, 0, 1*time.Hour, eqPred())
	broad := mkRule("b", SourceAuthored, 0, 0, nePred())
	if !better(RankedRule{narrow, 3}, RankedRule{broad, 1}) {
		t.Fatal("specificity 3 must outrank specificity 1 at equal priority")
	}
	// Specificity beats creation order.
	if better(RankedRule{broad, 1}, RankedRule{narrow, 3}) {
		t.Fatal("earlier creation must not beat higher specificity")
	}
}

func TestCompare_OlderRuleWinsOnEqualEverything(t *testing.T) {
	old := mkRule("old", SourceAuthored, 0, 0, eqPred())
	new := mkRule("new", SourceAuthored, 0, 5*time.Hour, eqPred())
	if !better(RankedRule{old, 3}, RankedRule{new, 3}) {
		t.Fatal("earlier-created rule must win the recency tiebreak")
	}
}

func TestCompare_RuleIDFinalTiebreak(t *testing.T) {
	// Identical in every dimension except ID: lexicographic, ascending.
	a := mkRule("r1", SourceAuthored, 0, 0, eqPred())
	b := mkRule("r2", SourceAuthored, 0, 0, eqPred())
	if !better(RankedRule{a, 3}, RankedRule{b, 3}) {
		t.Fatal("r1 must rank before r2 on the id tiebreak")
	}
	if better(RankedRule{b, 3}, RankedRule{a, 3}) {
		t.Fatal("id tiebreak antisymmetry violated")
	}
}

func TestCompare_AntisymmetryAndTransitivity(t *testing.T) {
	// Sweep a matrix of rules across all dimensions; verify:
	//   - antisymmetry: Compare(a,b) < 0  ⇔  Compare(b,a) > 0
	//   - transitivity: sort is consistent (via full-order sort check)
	rules := []RankedRule{
		{mkRule("r1", SourceAuthored, 0, 0, eqPred()), 3},
		{mkRule("r2", SourceAuthored, 5, 0, eqPred()), 3},
		{mkRule("r3", SourceAuthored, 0, 0, nePred()), 1},
		{mkRule("r4", SourceManual, -50, 9*time.Hour, eqPred()), 3},
		{mkRule("r5", SourceAuthored, 5, 1*time.Hour, nePred()), 1},
		{mkRule("r6", SourceSystem, 0, 0, eqPred()), 3},
	}
	for i := range rules {
		for j := range rules {
			if i == j {
				if Compare(rules[i], rules[j]) != 0 {
					t.Fatalf("rule %s does not compare equal to itself", rules[i].Rule.RuleID)
				}
				continue
			}
			ab := Compare(rules[i], rules[j])
			ba := Compare(rules[j], rules[i])
			if (ab < 0) == (ba < 0) {
				t.Fatalf("antisymmetry violated: %s vs %s → (%d, %d)",
					rules[i].Rule.RuleID, rules[j].Rule.RuleID, ab, ba)
			}
			if ab == 0 {
				t.Fatalf("zero compare for distinct rules %s vs %s — order not total",
					rules[i].Rule.RuleID, rules[j].Rule.RuleID)
			}
		}
	}

	// Transitivity: sort by Compare, then every pair must agree with its
	// position (a total order has no inversions after sorting).
	sorted := append([]RankedRule(nil), rules...)
	sort.SliceStable(sorted, func(i, j int) bool { return Compare(sorted[i], sorted[j]) < 0 })
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if Compare(sorted[i], sorted[j]) > 0 {
				t.Fatalf("transitivity violated: %s must rank before %s",
					sorted[i].Rule.RuleID, sorted[j].Rule.RuleID)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// SortMatched
// ---------------------------------------------------------------------------

func TestSortMatched_TotalOrderOnMixedInput(t *testing.T) {
	input := []RuleVersion{
		mkRule("later-authored", SourceAuthored, 0, 5*time.Hour, eqPred()),
		mkRule("manual", SourceManual, -100, 9*time.Hour, eqPred()),
		mkRule("high-priority", SourceAuthored, 10, 0, eqPred()),
		mkRule("ne-rule", SourceAuthored, 0, 0, nePred()),
	}
	got := SortMatched(input)
	want := []string{"manual", "high-priority", "later-authored", "ne-rule"}
	for i, rr := range got {
		if rr.Rule.RuleID != want[i] {
			t.Fatalf("position %d = %s, want %s (full order: %v)",
				i, rr.Rule.RuleID, want[i], ids(got))
		}
	}
}

func TestSortMatched_DoesNotMutateInput(t *testing.T) {
	input := []RuleVersion{
		mkRule("z", SourceAuthored, 0, 0, eqPred()),
		mkRule("a", SourceAuthored, 0, 0, eqPred()),
	}
	original := []string{"z", "a"}
	SortMatched(input)
	for i, r := range input {
		if r.RuleID != original[i] {
			t.Fatalf("input mutated: got %v, want %v", ruleIDs(input), original)
		}
	}
}

func TestSortMatched_PermutationInvariance(t *testing.T) {
	// Property: ANY input ordering of the same rules produces the SAME
	// ranked output. Seeded shuffles over 200 permutations.
	rules := []RuleVersion{
		mkRule("r1", SourceAuthored, 3, 0, eqPred()),
		mkRule("r2", SourceAuthored, 3, 1*time.Hour, eqPred()),
		mkRule("r3", SourceAuthored, 3, 2*time.Hour, nePred()),
		mkRule("r4", SourceManual, -100, 3*time.Hour, nePred()),
		mkRule("r5", SourceSystem, 1, 4*time.Hour, eqPred()),
	}
	want := ids(SortMatched(rules))

	rng := rand.New(rand.NewSource(42))
	for iter := 0; iter < 200; iter++ {
		shuffled := append([]RuleVersion(nil), rules...)
		rng.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
		got := ids(SortMatched(shuffled))
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("permutation %d diverged: got %v, want %v", iter, got, want)
		}
	}
}

func TestSortMatched_EmptyInput(t *testing.T) {
	got := SortMatched(nil)
	if len(got) != 0 {
		t.Fatalf("SortMatched(nil) = %v, want empty", got)
	}
}

// ---------------------------------------------------------------------------
// lossReason — the why_lost strings that feed the decision trace
// ---------------------------------------------------------------------------

func TestLossReason_NamesDimensionAndWinner(t *testing.T) {
	// Each case pairs a loser with a winner that beats it on ONE dimension,
	// in chain order — matching how the resolver actually produces losers.
	tests := []struct {
		name       string
		winner     RankedRule
		loser      RankedRule
		wantSubstr string
	}{
		{
			name:       "lost to manual override",
			winner:     RankedRule{mkRule("winner", SourceManual, 0, 0, eqPred()), 3},
			loser:      RankedRule{mkRule("loser", SourceAuthored, 999, 0, eqPred()), 3},
			wantSubstr: "lost to manual override winner",
		},
		{
			name:       "lost priority names values",
			winner:     RankedRule{mkRule("winner", SourceAuthored, 2, 0, eqPred()), 3},
			loser:      RankedRule{mkRule("loser", SourceAuthored, 1, 0, eqPred()), 3},
			wantSubstr: "lost priority tiebreak to winner (1 < 2)",
		},
		{
			name:       "lost specificity names values",
			winner:     RankedRule{mkRule("winner", SourceAuthored, 0, 0, eqPred()), 3},
			loser:      RankedRule{mkRule("loser", SourceAuthored, 0, 0, nePred()), 1},
			wantSubstr: "lost specificity tiebreak to winner (1 < 3)",
		},
		{
			name:       "lost recency",
			winner:     RankedRule{mkRule("winner", SourceAuthored, 0, 1*time.Hour, eqPred()), 3},
			loser:      RankedRule{mkRule("loser", SourceAuthored, 0, 0, eqPred()), 3},
			wantSubstr: "lost recency tiebreak to winner",
		},
		{
			name:       "lost id is the last resort",
			winner:     RankedRule{mkRule("winner", SourceAuthored, 0, 0, eqPred()), 3},
			loser:      RankedRule{mkRule("aaa", SourceAuthored, 0, 0, eqPred()), 3},
			wantSubstr: "lost id tiebreak to winner",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := lossReason(tt.loser, tt.winner)
			if !contains(got, tt.wantSubstr) {
				t.Fatalf("lossReason = %q, want containing %q", got, tt.wantSubstr)
			}
		})
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func ids(ranked []RankedRule) []string {
	out := make([]string, len(ranked))
	for i, rr := range ranked {
		out[i] = rr.Rule.RuleID
	}
	return out
}

func ruleIDs(rules []RuleVersion) []string {
	out := make([]string, len(rules))
	for i, r := range rules {
		out[i] = r.RuleID
	}
	return out
}
