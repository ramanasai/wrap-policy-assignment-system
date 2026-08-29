package resolver

import (
	"fmt"
	"strings"
	"time"
)

// Date layout used across the system: valid time is date-granular
// (docs/ARCHITECTURE.md — "date granularity for valid time").
const dateLayout = "2006-01-02"

// ---------------------------------------------------------------------------
// JSON helpers
// ---------------------------------------------------------------------------

func isJSONSlice(v interface{}) bool {
	_, ok := v.([]interface{})
	return ok
}

func toSlice(v interface{}) []interface{} {
	if s, ok := v.([]interface{}); ok {
		return s
	}
	return nil
}

func containsOp(ops []ClauseOp, op ClauseOp) bool {
	for _, o := range ops {
		if o == op {
			return true
		}
	}
	return false
}

func toNumber(v interface{}) (float64, error) {
	switch n := v.(type) {
	case float64:
		return n, nil
	case float32:
		return float64(n), nil
	case int:
		return float64(n), nil
	case int64:
		return float64(n), nil
	case int32:
		return float64(n), nil
	default:
		return 0, fmt.Errorf("not a number: %T", v)
	}
}

func isValidDate(s string) bool {
	_, err := time.Parse(dateLayout, s)
	return err == nil
}

// ---------------------------------------------------------------------------
// Normalized comparables
//
// Every fact value and predicate value is normalized to one of three kinds
// before comparison. This makes evaluation total: unknown shapes become
// kindErr and surface as a deterministic why_not reason, never a panic.
// ---------------------------------------------------------------------------

type comparableKind int

const (
	kindErr comparableKind = iota
	kindNumber
	kindString
	kindBool
)

type comparableValue struct {
	kind comparableKind
	num  float64
	str  string
	bl   bool
}

// normalizeComparable maps raw values into comparable space.
//
//   - numbers (any Go numeric shape) → kindNumber
//   - strings                        → kindString
//   - bools                          → kindBool
//
// Date strings are NOT auto-promoted by sniffing: date comparisons are driven
// by the attribute definition (TypeDate), never by guessing.
func normalizeComparable(v interface{}) comparableValue {
	switch t := v.(type) {
	case float64:
		return comparableValue{kind: kindNumber, num: t}
	case float32:
		return comparableValue{kind: kindNumber, num: float64(t)}
	case int:
		return comparableValue{kind: kindNumber, num: float64(t)}
	case int64:
		return comparableValue{kind: kindNumber, num: float64(t)}
	case int32:
		return comparableValue{kind: kindNumber, num: float64(t)}
	case string:
		return comparableValue{kind: kindString, str: t}
	case bool:
		return comparableValue{kind: kindBool, bl: t}
	default:
		return comparableValue{kind: kindErr}
	}
}

// compare returns <0, 0, >0. Only valid within the same kind; cross-kind
// comparisons return an error so callers record a deterministic why_not.
func (c comparableValue) compare(o comparableValue) (int, error) {
	if c.kind != o.kind {
		return 0, fmt.Errorf("type mismatch: cannot compare %s with %s", kindName(c.kind), kindName(o.kind))
	}
	switch c.kind {
	case kindNumber:
		switch {
		case c.num < o.num:
			return -1, nil
		case c.num > o.num:
			return 1, nil
		default:
			return 0, nil
		}
	case kindString:
		return strings.Compare(c.str, o.str), nil
	case kindBool:
		switch {
		case c.bl == o.bl:
			return 0, nil
		case !c.bl:
			return -1, nil
		default:
			return 1, nil
		}
	default:
		return 0, fmt.Errorf("uncomparable value")
	}
}

func kindName(k comparableKind) string {
	switch k {
	case kindNumber:
		return "number"
	case kindString:
		return "string"
	case kindBool:
		return "bool"
	default:
		return "unknown"
	}
}

// ---------------------------------------------------------------------------
// Clause evaluation
// ---------------------------------------------------------------------------

// evalClause evaluates one clause against a raw fact value. Errors are
// evaluation errors (type mismatches, bad values) — callers turn them into
// why_not reasons; they never panic and never silently pass.
func evalClause(c Clause, raw interface{}) (bool, error) {
	// Membership operators normalize both sides before comparing.
	if c.Op == OpIn || c.Op == OpNotIn {
		items := toSlice(c.Value)
		found := false
		for _, item := range items {
			eq, err := equalNormalized(raw, item)
			if err != nil {
				return false, err
			}
			if eq {
				found = true
				break
			}
		}
		if c.Op == OpIn {
			return found, nil
		}
		return !found, nil
	}

	// contains: the FACT value is a list; the predicate value is a scalar.
	if c.Op == OpContains {
		list, ok := raw.([]any)
		if !ok {
			return false, fmt.Errorf("fact value for contains is not a list: %v", raw)
		}
		for _, item := range list {
			eq, err := equalNormalized(item, c.Value)
			if err != nil {
				return false, err
			}
			if eq {
				return true, nil
			}
		}
		return false, nil
	}

	ln := normalizeComparable(raw)
	if ln.kind == kindErr {
		return false, fmt.Errorf("cannot normalize fact value %v", raw)
	}
	rn := normalizeComparable(c.Value)
	if rn.kind == kindErr {
		return false, fmt.Errorf("cannot normalize predicate value %v", c.Value)
	}
	cmp, err := ln.compare(rn)
	if err != nil {
		return false, err
	}

	switch c.Op {
	case OpEq:
		return cmp == 0, nil
	case OpNe:
		return cmp != 0, nil
	case OpGTE:
		return cmp >= 0, nil
	case OpGT:
		return cmp > 0, nil
	case OpLTE:
		return cmp <= 0, nil
	case OpLT:
		return cmp < 0, nil
	}
	return false, fmt.Errorf("unhandled operator %q", c.Op)
}

func equalNormalized(raw, item interface{}) (bool, error) {
	ln, rn := normalizeComparable(raw), normalizeComparable(item)
	if ln.kind == kindErr || rn.kind == kindErr {
		return false, fmt.Errorf("cannot compare %v with %v", raw, item)
	}
	cmp, err := ln.compare(rn)
	if err != nil {
		return false, err
	}
	return cmp == 0, nil
}

// Date note: comparisons on dates need no special casing. The validator
// (ast.go checkValue) enforces strict "YYYY-MM-DD", and fixed-width ISO dates
// compare chronologically under plain string ordering — so normalizeComparable's
// string path IS the date path, deterministically.
