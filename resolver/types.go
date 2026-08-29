// Package resolver implements the policy assignment resolution engine.
//
// It is a PURE package: no I/O, no database types, no framework imports.
// Resolution is a deterministic function of (category config, facts, rules, date)
// → (assignments, shadowed matches, decision trace). The same inputs always
// produce byte-identical outputs; ties are broken only on stored, stable fields.
package resolver

import (
	"time"
)

// Cardinality of a policy category: how many policies of this kind an employee may hold.
//
//   - Single: exactly one per employee (manager, pay schedule). Conflicts are resolved
//     by the deterministic total order; losing matches are SHADOWED, not discarded.
//   - Many: additive (app access, trainings). All matches stack; no conflict concept.
type Cardinality string

const (
	CardinalitySingle Cardinality = "single"
	CardinalityMany   Cardinality = "many"
)

// ResolutionStrategy declares how a category's conflicts behave.
//
//   - PriorityRank: resolver picks the head of the deterministic total order.
//   - ExplicitUserChoice: competing matches require a human decision (the resolver
//     reports ranked options; it never picks on the user's behalf).
//   - Additive: everything matches and stacks.
type ResolutionStrategy string

const (
	StrategyPriorityRank      ResolutionStrategy = "priority_rank"
	StrategyExplicitUserChoice ResolutionStrategy = "explicit_user_choice"
	StrategyAdditive          ResolutionStrategy = "additive"
)

// Source distinguishes authored rules from manual overrides and system rules.
// Manual overrides outrank everything (dimension 1 of the tie-break order).
type Source string

const (
	SourceAuthored Source = "authored"
	SourceManual   Source = "manual"
	SourceSystem   Source = "system"
)

// Outcome values reported on the result and in decision traces.
// These mirror decision_trace.outcome in the schema.
const (
	OutcomeAssigned              = "assigned"                // cleanly resolved
	OutcomeShadowed              = "shadowed"                // winner exists; losing matches were shadowed
	OutcomeNoMatch               = "no_match"                // nothing matched
	OutcomeConflictNeedsDecision = "conflict_needs_decision" // explicit choice required
)

// Per-rule trace outcomes.
const (
	RuleOutcomeWinner       = "winner"
	RuleOutcomeShadowed     = "shadowed"
	RuleOutcomeNotMatched   = "not_matched"
	RuleOutcomeNeedsDecision = "needs_decision"
)

// CategoryConfig carries the declarative semantics of a policy category.
// Mirrors the policy_category table (docs/DATA_MODEL.md).
type CategoryConfig struct {
	ID                 string             `json:"id"`
	Cardinality        Cardinality        `json:"cardinality"`
	ResolutionStrategy ResolutionStrategy `json:"resolution_strategy"`
	DefaultPriority    int                `json:"default_priority"`
	Tiebreaker         string             `json:"tiebreaker"`
}

// RuleVersion is one effective-dated version of an assignment rule.
// The resolver consumes already-effective versions; selecting versions for a
// date is the caller's (repository) concern.
type RuleVersion struct {
	RuleID          string    `json:"rule_id"`
	RuleVersionID   string    `json:"rule_version_id"`
	CategoryID      string    `json:"category_id"`
	PolicyID        string    `json:"policy_id"`
	PolicyVersionID string    `json:"policy_version_id"`
	Source          Source    `json:"source"`
	Priority        int       `json:"priority"` // explicit, admin-set
	CreatedAt       time.Time `json:"created_at"`
	Predicate       Predicate `json:"predicate"`
}

// Assignment is one resolved (derived) assignment. Validity ranges are the
// caller's concern — the resolver reasons per date, not per interval.
type Assignment struct {
	PolicyID        string `json:"policy_id"`
	PolicyVersionID string `json:"policy_version_id"`
	RuleID          string `json:"rule_id"`
	Source          Source `json:"source"`
}

// ShadowedMatch records a matched rule that lost to a winner. Losing matches
// must persist so that deleting the winning rule deterministically resurrects
// the loser on the next recompute.
type ShadowedMatch struct {
	RuleID   string `json:"rule_id"`
	ByRuleID string `json:"by_rule_id"`
}

// DecisionOption is a ranked choice surfaced when a category's strategy is
// explicit_user_choice and multiple rules matched. Rank 1 is the recommended
// pick under the same total order used for shadowing.
type DecisionOption struct {
	PolicyID        string `json:"policy_id"`
	PolicyVersionID string `json:"policy_version_id"`
	RuleID          string `json:"rule_id"`
	Rank            int    `json:"rank"`
}

// Result is the complete, self-explanatory output of one resolution.
type Result struct {
	CategoryID  string           `json:"category_id"`
	AsOfDate    string           `json:"as_of_date"`
	Outcome     string           `json:"outcome"`
	Assignments []Assignment     `json:"assignments,omitempty"`
	Shadowed    []ShadowedMatch  `json:"shadowed,omitempty"`
	Options     []DecisionOption `json:"options,omitempty"`
	Trace       Trace            `json:"trace"`
}
