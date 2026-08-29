package resolver

import (
	"fmt"
	"sort"
)

// RankedRule pairs a rule with its computed specificity for the total order.
type RankedRule struct {
	Rule        RuleVersion
	Specificity int
}

// Compare orders two ranked rules by the deterministic total order from
// DECISIONS.md Q4, applied dimension by dimension:
//
//	1. manual override wins over everything
//	2. explicit priority, higher wins
//	3. automatic specificity, higher (more selective) wins
//	4. created_at, earlier rule wins (oldest-rule-stable convention)
//	5. rule ID, lexicographic — final, total tiebreak
//
// Returns -1 if a ranks BEFORE b (a wins), +1 if b ranks before a, 0 never:
// the final ID dimension makes the order total, so equal zero cannot occur
// for distinct rules — that is what makes resolution deterministic.
func Compare(a, b RankedRule) int {
	// 1. Manual overrides first.
	aMan, bMan := a.Rule.Source == SourceManual, b.Rule.Source == SourceManual
	if aMan != bMan {
		if aMan {
			return -1
		}
		return 1
	}
	// 2. Explicit priority, higher wins.
	if a.Rule.Priority != b.Rule.Priority {
		if a.Rule.Priority > b.Rule.Priority {
			return -1
		}
		return 1
	}
	// 3. Automatic specificity, higher wins.
	if a.Specificity != b.Specificity {
		if a.Specificity > b.Specificity {
			return -1
		}
		return 1
	}
	// 4. Earlier-created rule wins.
	if !a.Rule.CreatedAt.Equal(b.Rule.CreatedAt) {
		if a.Rule.CreatedAt.Before(b.Rule.CreatedAt) {
			return -1
		}
		return 1
	}
	// 5. Rule ID lexicographic — total.
	if a.Rule.RuleID != b.Rule.RuleID {
		if a.Rule.RuleID < b.Rule.RuleID {
			return -1
		}
		return 1
	}
	return 0
}

// SortMatched returns the matched rules in the deterministic total order.
// The input slice is never mutated; a fresh slice is returned.
func SortMatched(rules []RuleVersion) []RankedRule {
	ranked := make([]RankedRule, len(rules))
	for i, r := range rules {
		ranked[i] = RankedRule{Rule: r, Specificity: Specificity(r.Predicate)}
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		return Compare(ranked[i], ranked[j]) < 0
	})
	return ranked
}

// lossReason explains WHY loser lost to winner, naming the first dimension on
// which they differ — this is the raw material of the decision trace's
// "why_lost" field (docs/ARCHITECTURE.md, evaluation criteria: explainable).
func lossReason(loser, winner RankedRule) string {
	if loser.Rule.Source != winner.Rule.Source && winner.Rule.Source == SourceManual {
		return fmt.Sprintf("lost to manual override %s", winner.Rule.RuleID)
	}
	if loser.Rule.Priority != winner.Rule.Priority {
		return fmt.Sprintf("lost priority tiebreak to %s (%d < %d)",
			winner.Rule.RuleID, loser.Rule.Priority, winner.Rule.Priority)
	}
	if loser.Specificity != winner.Specificity {
		return fmt.Sprintf("lost specificity tiebreak to %s (%d < %d)",
			winner.Rule.RuleID, loser.Specificity, winner.Specificity)
	}
	if !loser.Rule.CreatedAt.Equal(winner.Rule.CreatedAt) {
		return fmt.Sprintf("lost recency tiebreak to %s (older rule wins)",
			winner.Rule.RuleID)
	}
	return fmt.Sprintf("lost id tiebreak to %s", winner.Rule.RuleID)
}
