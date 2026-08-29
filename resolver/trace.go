package resolver

import (
	"fmt"
	"sort"
)

// Trace is the decision trace: the immutable audit artifact answering
// "why does employee X have assignment Y as of date Z?"
//
// It records every rule evaluated (matched or not, and why), the rank of the
// matched rules under the deterministic total order, and snapshots of the
// facts and policy configuration used — so the answer never changes even
// after inputs change (docs/ARCHITECTURE.md: traces are written at decision
// time and never recomputed).
type Trace struct {
	Outcome        string           `json:"outcome"`
	FactsSnapshot  map[string]any   `json:"facts_snapshot"`
	PolicySnapshot PolicySnapshot   `json:"policy_snapshot"`
	Evaluated      []RuleEvaluation `json:"evaluated"`
	ShortAnswer    string           `json:"short_answer"`
}

// PolicySnapshot captures the category semantics + the exact rule versions
// evaluated, keyed by version ID so historical replay can pin inputs.
type PolicySnapshot struct {
	CategoryID        string             `json:"category_id"`
	Cardinality       Cardinality        `json:"cardinality"`
	ResolutionStrategy ResolutionStrategy `json:"resolution_strategy"`
	RuleVersionIDs    []string           `json:"rule_version_ids"`
}

// RuleEvaluation is the per-rule line of the trace.
type RuleEvaluation struct {
	RuleID  string `json:"rule_id"`
	Matched bool   `json:"matched"`
	Outcome string `json:"outcome"` // winner | shadowed | blocked | needs_decision | not_matched
	Rank    int    `json:"rank,omitempty"`
	WhyNot  string `json:"why_not,omitempty"`
	WhyLost string `json:"why_lost,omitempty"`
}

// copyAttrs deep-copies the attribute map so the trace snapshot is immune to
// later mutation of the caller's facts (snapshot semantics).
func copyAttrs(attrs map[string]any) map[string]any {
	out := make(map[string]any, len(attrs))
	for k, v := range attrs {
		if s, ok := v.([]any); ok {
			cp := make([]any, len(s))
			copy(cp, s)
			out[k] = cp
			continue
		}
		out[k] = v
	}
	return out
}

// buildEvaluations assembles the per-rule trace lines in a deterministic
// order: rules sorted by RuleID. Ranks (among matched rules) follow the
// deterministic total order from SortMatched.
func buildEvaluations(rulesByID []RuleVersion, matchResults map[string]matchResult, ranked []RankedRule, strategy ResolutionStrategy, needsDecision bool) []RuleEvaluation {
	rankByRule := make(map[string]int, len(ranked))
	for i, rr := range ranked {
		rankByRule[rr.Rule.RuleID] = i + 1 // ranks are 1-based and contiguous
	}

	out := make([]RuleEvaluation, 0, len(rulesByID))
	for _, r := range rulesByID {
		mr := matchResults[r.RuleID]
		if !mr.matched {
			out = append(out, RuleEvaluation{
				RuleID:  r.RuleID,
				Matched: false,
				Outcome: RuleOutcomeNotMatched,
				WhyNot:  mr.whyNot,
			})
			continue
		}
		rank := rankByRule[r.RuleID]

		switch {
		case strategy == StrategyExplicitUserChoice && needsDecision:
			out = append(out, RuleEvaluation{
				RuleID:  r.RuleID,
				Matched: true,
				Outcome: RuleOutcomeNeedsDecision,
				Rank:    rank,
			})
		case rank == 1:
			out = append(out, RuleEvaluation{
				RuleID:  r.RuleID,
				Matched: true,
				Outcome: RuleOutcomeWinner,
				Rank:    rank,
			})
		case strategy == StrategyAdditive:
			// Additive categories have no conflict: every match stacks.
			out = append(out, RuleEvaluation{
				RuleID:  r.RuleID,
				Matched: true,
				Outcome: RuleOutcomeWinner,
				Rank:    rank,
			})
		default:
			winner := ranked[0]
			out = append(out, RuleEvaluation{
				RuleID:  r.RuleID,
				Matched: true,
				Outcome: RuleOutcomeShadowed,
				Rank:    rank,
				WhyLost: lossReason(RankedRule{Rule: r, Specificity: Specificity(r.Predicate)}, winner),
			})
		}
	}
	return out
}

// shortAnswer renders the headline explanation — the progressive-disclosure
// "short answer" layer of the explain inspector (docs/UX_FLOWS.md §5).
func shortAnswer(outcome string, ranked []RankedRule, strategy ResolutionStrategy, needsDecision bool) string {
	switch outcome {
	case OutcomeNoMatch:
		return fmt.Sprintf("No rule matched for this category as of this date (%d rules evaluated).", len(ranked))
	case OutcomeConflictNeedsDecision:
		return fmt.Sprintf("%d rules matched; strategy %q requires an explicit decision. Top pick under the standard tiebreak: %s.",
			len(ranked), strategy, ranked[0].Rule.RuleID)
	case OutcomeShadowed:
		return fmt.Sprintf("Rule %s matched and won; %d other matching rule(s) were shadowed by the deterministic tiebreak.",
			ranked[0].Rule.RuleID, len(ranked)-1)
	default: // assigned
		return fmt.Sprintf("Rule %s matched and was assigned cleanly.", ranked[0].Rule.RuleID)
	}
}

// sortRuleVersionIDs returns a sorted copy — used for the policy snapshot.
func sortRuleVersionIDs(rules []RuleVersion) []string {
	ids := make([]string, 0, len(rules))
	for _, r := range rules {
		ids = append(ids, r.RuleVersionID)
	}
	sort.Strings(ids)
	return ids
}
