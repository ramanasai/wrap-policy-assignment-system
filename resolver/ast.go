package resolver

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// GroupOp is the combinator of a predicate. The canonical AST is an AND group
// (docs/DATA_MODEL.md); OR-style logic is expressed via saved segments, not
// nested groups — keeping the resolver total and every decision explainable.
type GroupOp string

const OpAnd GroupOp = "and"

// ClauseOp is a comparison operator. Weighted by selectivity for specificity:
// exact matches narrow more than exclusions.
type ClauseOp string

const (
	OpEq       ClauseOp = "eq"
	OpNe       ClauseOp = "ne"
	OpIn       ClauseOp = "in"
	OpNotIn    ClauseOp = "not_in"
	OpGTE      ClauseOp = "gte"
	OpGT       ClauseOp = "gt"
	OpLTE      ClauseOp = "lte"
	OpLT       ClauseOp = "lt"
	OpContains ClauseOp = "contains" // fact value is a LIST containing the scalar predicate value
)

var allClauseOps = map[ClauseOp]bool{
	OpEq: true, OpNe: true, OpIn: true, OpNotIn: true,
	OpGTE: true, OpGT: true, OpLTE: true, OpLT: true,
	OpContains: true,
}

// Predicate is the canonical rule AST: an AND group of clauses.
type Predicate struct {
	Op      GroupOp  `json:"op"`
	Clauses []Clause `json:"clauses"`
}

// Clause is a single attribute comparison.
type Clause struct {
	Attr  string      `json:"attr"`
	Op    ClauseOp    `json:"op"`
	Value interface{} `json:"value"`
}

// ParsePredicate parses and structurally validates canonical predicate JSON.
// Type-level validation (op legality per attribute, value types) happens in
// Validate once attribute definitions are available.
func ParsePredicate(data []byte) (Predicate, error) {
	var p Predicate
	if err := json.Unmarshal(data, &p); err != nil {
		return Predicate{}, fmt.Errorf("parse predicate: %w", err)
	}
	if p.Op != OpAnd {
		return Predicate{}, fmt.Errorf("group op must be %q, got %q", OpAnd, p.Op)
	}
	if len(p.Clauses) == 0 {
		return Predicate{}, fmt.Errorf("predicate must contain at least one clause")
	}
	for i, c := range p.Clauses {
		if c.Attr == "" {
			return Predicate{}, fmt.Errorf("clause %d: attr is required", i)
		}
		if !allClauseOps[c.Op] {
			return Predicate{}, fmt.Errorf("clause %d (%s): unknown operator %q", i, c.Attr, c.Op)
		}
		if c.Value == nil {
			return Predicate{}, fmt.Errorf("clause %d (%s): value is required", i, c.Attr)
		}
		if (c.Op == OpIn || c.Op == OpNotIn) && !isJSONSlice(c.Value) {
			return Predicate{}, fmt.Errorf("clause %d (%s): operator %q requires an array value", i, c.Attr, c.Op)
		}
	}
	return p, nil
}

// Validate checks the predicate against attribute definitions: operator
// legality per attribute type, value-type compatibility, and enum membership.
// defs == nil skips type checks (defensive mode for tests/ad-hoc evaluation).
func (p Predicate) Validate(defs map[string]AttributeDefinition) error {
	for i, c := range p.Clauses {
		def, ok := defs[c.Attr]
		if !ok {
			continue // unknown attributes are data concerns, not shape errors
		}
		if !containsOp(def.AllowedOps, c.Op) {
			return fmt.Errorf("clause %d (%s): operator %q not allowed for type %q", i, c.Attr, c.Op, def.ValueType)
		}
		if err := checkValue(c, def); err != nil {
			return fmt.Errorf("clause %d (%s): %w", i, c.Attr, err)
		}
	}
	return nil
}

// Matches evaluates the predicate against facts. Returns the outcome and a
// deterministic, human-readable why_not reason for the first failing clause.
// A predicate with zero clauses matches vacuously (matches-all).
func (p Predicate) Matches(facts Facts) (bool, string) {
	for _, c := range p.Clauses {
		raw, present := facts.Attributes[c.Attr]
		if !present {
			return false, fmt.Sprintf("attribute %q not present in facts", c.Attr)
		}
		ok, err := evalClause(c, raw)
		if err != nil {
			return false, fmt.Sprintf("clause %s %s %s: %v", c.Attr, c.Op, renderValue(c.Value), err)
		}
		if !ok {
			return false, fmt.Sprintf("clause failed: %s %s %s (got %s)", c.Attr, c.Op, renderValue(c.Value), renderValue(raw))
		}
	}
	return true, ""
}

func checkValue(c Clause, def AttributeDefinition) error {
	switch def.ValueType {
	case TypeNumber:
		if c.Op == OpIn || c.Op == OpNotIn {
			for _, v := range toSlice(c.Value) {
				if _, err := toNumber(v); err != nil {
					return fmt.Errorf("array element %v is not a number", v)
				}
			}
			return nil
		}
		if _, err := toNumber(c.Value); err != nil {
			return fmt.Errorf("expected number, got %T", c.Value)
		}
	case TypeDate:
		vals := []interface{}{c.Value}
		if c.Op == OpIn || c.Op == OpNotIn {
			vals = toSlice(c.Value)
		}
		for _, v := range vals {
			s, ok := v.(string)
			if !ok || !isValidDate(s) {
				return fmt.Errorf("expected date \"YYYY-MM-DD\", got %v", v)
			}
		}
	case TypeBool:
		if c.Op == OpIn || c.Op == OpNotIn {
			for _, v := range toSlice(c.Value) {
				if _, ok := v.(bool); !ok {
					return fmt.Errorf("array element %v is not a bool", v)
				}
			}
			return nil
		}
		if _, ok := c.Value.(bool); !ok {
			return fmt.Errorf("expected bool, got %T", c.Value)
		}
	case TypeString, TypeEnum:
		vals := []interface{}{c.Value}
		if c.Op == OpIn || c.Op == OpNotIn {
			vals = toSlice(c.Value)
		}
		for _, v := range vals {
			if _, ok := v.(string); !ok {
				return fmt.Errorf("expected string, got %T", v)
			}
		}
		if def.ValueType == TypeEnum && len(def.EnumValues) > 0 {
			allowed := map[string]bool{}
			for _, e := range def.EnumValues {
				allowed[e] = true
			}
			for _, v := range vals {
				if !allowed[v.(string)] {
					return fmt.Errorf("value %q not in enum %v", v, def.EnumValues)
				}
			}
		}
	}
	return nil
}

// renderValue produces a stable, human-readable rendering of a value for
// trace reasons. It must be deterministic: identical inputs render identically.
func renderValue(v interface{}) string {
	switch t := v.(type) {
	case nil:
		return "<nil>"
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(t), 'f', -1, 64)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case int32:
		return strconv.FormatInt(int64(t), 10)
	case []interface{}:
		parts := make([]string, len(t))
		for i, e := range t {
			parts[i] = renderValue(e)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	default:
		return fmt.Sprintf("%v", t)
	}
}
