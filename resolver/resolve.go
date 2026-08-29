package resolver

import (
	"fmt"
	"sort"
	"time"
)

// Input is everything Resolve needs. It is pure data: the caller (repository
// layer) has already selected effective rule versions and fact snapshots.
type Input struct {
	Category CategoryConfig
	Date     string // "YYYY-MM-DD" — date-granular valid time
	Facts    Facts
	Rules    []RuleVersion
	// Definitions is optional. When non-nil, every rule predicate is strictly
	// validated against the registry and surfaced errors beat silent misfires.
	Definitions map[string]AttributeDefinition
}

// Resolve computes the effective assignments for one employee, one category,
// one date — deterministically and with a complete decision trace.
//
// Pipeline (docs/ARCHITECTURE.md):
//  1. filter   — evaluate each rule's predicate against facts
//  2. order    — sort matched rules by the deterministic total order
//  3. select   — per cardinality/strategy (winner+shadow / additive / decision)
//  4. trace    — record every rule, why each lost, inputs used
//
// Invariants (property-tested):
//   - same input → byte-identical output
//   - single-cardinality categories yield exactly one assignment or zero,
//     and zero always comes with a reason in the trace
func Resolve(in Input) (Result, error) {
	if err := validateCategory(in.Category); err != nil {
		return Result{}, err
	}
	asOf, err := time.Parse(dateLayout, in.Date)
	if err != nil {
		return Result{}, fmt.Errorf("resolve: date %q must be YYYY-MM-DD", in.Date)
	}

	// Derive attributes (tenure from hire_date) on a copy; explicit values win.
	derived, err := DeriveAttributes(in.Facts.Attributes, asOf)
	if err != nil {
		return Result{}, fmt.Errorf("resolve: %w", err)
	}
	facts := Facts{EmployeeID: in.Facts.EmployeeID, AsOf: in.Date, Attributes: derived}

	// Keep only rules for this category (defensive: misfiled rules are ignored,
	// never allowed to leak across categories).
	rules := make([]RuleVersion, 0, len(in.Rules))
	for _, r := range in.Rules {
		if r.CategoryID == in.Category.ID {
			rules = append(rules, r)
		}
	}

	// Deterministic evaluation order: sort all rules by Rule ID.
	sort.Slice(rules, func(i, j int) bool { return rules[i].RuleID < rules[j].RuleID })

	// Strict predicate validation when definitions are supplied.
	if in.Definitions != nil {
		for _, r := range rules {
			if err := r.Predicate.Validate(in.Definitions); err != nil {
				return Result{}, fmt.Errorf("resolve: rule %s: %w", r.RuleID, err)
			}
		}
	}

	// 1. Filter.
	matchResults := make(map[string]matchResult, len(rules))
	var matched []RuleVersion
	for _, r := range rules {
		ok, whyNot := r.Predicate.Matches(facts)
		matchResults[r.RuleID] = matchResult{matched: ok, whyNot: whyNot}
		if ok {
			matched = append(matched, r)
		}
	}

	// 2. Order.
	ranked := SortMatched(matched)

	res := Result{CategoryID: in.Category.ID, AsOfDate: in.Date}
	needsDecision := false

	// 3. Select — per cardinality/strategy.
	switch {
	case len(ranked) == 0:
		res.Outcome = OutcomeNoMatch

	case in.Category.ResolutionStrategy == StrategyAdditive:
		res.Outcome = OutcomeAssigned
		seen := map[string]bool{}
		for _, rr := range ranked {
			key := rr.Rule.PolicyID + "/" + rr.Rule.PolicyVersionID
			if seen[key] {
				continue // additive = a SET of policies: several rules may
				// map to the same policy (e.g. tenure-based AND segment-based
				// security training) — materialized once (trace covers both).
			}
			seen[key] = true
			res.Assignments = append(res.Assignments, toAssignment(rr.Rule))
		}

	case in.Category.ResolutionStrategy == StrategyExplicitUserChoice && len(ranked) > 1:
		needsDecision = true
		res.Outcome = OutcomeConflictNeedsDecision
		for i, rr := range ranked {
			res.Options = append(res.Options, DecisionOption{
				PolicyID:        rr.Rule.PolicyID,
				PolicyVersionID: rr.Rule.PolicyVersionID,
				RuleID:          rr.Rule.RuleID,
				Rank:            i + 1,
			})
		}

	case in.Category.ResolutionStrategy == StrategyPriorityRank && len(ranked) > 1:
		res.Outcome = OutcomeShadowed
		res.Assignments = append(res.Assignments, toAssignment(ranked[0].Rule))
		for _, rr := range ranked[1:] {
			res.Shadowed = append(res.Shadowed, ShadowedMatch{
				RuleID:   rr.Rule.RuleID,
				ByRuleID: ranked[0].Rule.RuleID,
			})
		}

	default: // exactly one match — any strategy resolves cleanly
		res.Outcome = OutcomeAssigned
		res.Assignments = append(res.Assignments, toAssignment(ranked[0].Rule))
	}

	// 4. Trace.
	res.Trace = Trace{
		Outcome:       res.Outcome,
		FactsSnapshot: copyAttrs(facts.Attributes),
		PolicySnapshot: PolicySnapshot{
			CategoryID:        in.Category.ID,
			Cardinality:       in.Category.Cardinality,
			ResolutionStrategy: in.Category.ResolutionStrategy,
			RuleVersionIDs:    sortRuleVersionIDs(rules),
		},
		Evaluated:   buildEvaluations(rules, matchResults, ranked, in.Category.ResolutionStrategy, needsDecision),
		ShortAnswer: shortAnswer(res.Outcome, ranked, in.Category.ResolutionStrategy, needsDecision),
	}
	return res, nil
}

// validateCategory enforces the declared strategy/cardinality combos:
// single categories resolve via priority_rank or explicit_user_choice;
// many categories are additive. Any other combo is a configuration error.
func validateCategory(c CategoryConfig) error {
	if c.ID == "" {
		return fmt.Errorf("resolve: category id is required")
	}
	switch c.Cardinality {
	case CardinalitySingle:
		switch c.ResolutionStrategy {
		case StrategyPriorityRank, StrategyExplicitUserChoice:
			return nil
		default:
			return fmt.Errorf("resolve: category %q: cardinality %q forbids strategy %q",
				c.ID, c.Cardinality, c.ResolutionStrategy)
		}
	case CardinalityMany:
		if c.ResolutionStrategy == StrategyAdditive {
			return nil
		}
		return fmt.Errorf("resolve: category %q: cardinality %q requires strategy %q",
			c.ID, c.Cardinality, StrategyAdditive)
	default:
		return fmt.Errorf("resolve: category %q: unknown cardinality %q", c.ID, c.Cardinality)
	}
}

func toAssignment(r RuleVersion) Assignment {
	return Assignment{
		PolicyID:        r.PolicyID,
		PolicyVersionID: r.PolicyVersionID,
		RuleID:          r.RuleID,
		Source:          r.Source,
	}
}

// matchResult is the per-rule outcome of the filter phase.
type matchResult struct {
	matched bool
	whyNot  string
}
