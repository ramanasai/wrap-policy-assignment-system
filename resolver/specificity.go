package resolver

// Specificity computes the automatic specificity of a predicate from its AST.
//
// Rationale (docs/ARCHITECTURE.md §Automatic specificity): each conjunct on a
// low-cardinality attribute narrows the employee population. We approximate
// per-clause selectivity by operator class — exact equality narrows most,
// exclusions (ne / not_in) match most of the population and narrow least.
//
//	3 — eq        (exact match on one attribute value)
//	2 — in, gte, gt, lte, lt  (set membership / range bounds)
//	1 — ne, not_in            (exclusion: matches almost everyone)
//
// The score is a pure function of the AST: deterministic, cached-per-version
// at the repository layer, never admin-declared. Explicit admin priority
// exists precisely as the escape hatch when automatic ranking surprises.
func Specificity(p Predicate) int {
	score := 0
	for _, c := range p.Clauses {
		score += clauseWeight(c.Op)
	}
	return score
}

func clauseWeight(op ClauseOp) int {
	switch op {
	case OpEq:
		return 3
	case OpIn, OpGTE, OpGT, OpLTE, OpLT:
		return 2
	case OpNe, OpNotIn:
		return 1
	default:
		return 0
	}
}
