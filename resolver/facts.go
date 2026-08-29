package resolver

import (
	"fmt"
	"time"
)

// ValueType enumerates the typed attribute kinds from attribute_definition.
// Types drive validation, coercion, and comparison semantics.
type ValueType string

const (
	TypeString ValueType = "string"
	TypeNumber ValueType = "number"
	TypeDate   ValueType = "date"
	TypeBool   ValueType = "bool"
	TypeEnum   ValueType = "enum"
)

// AttributeDefinition declares one attribute in the registry (attribute_definition
// table). The resolver is attribute-agnostic: registering a new attribute is
// data, not code. Definitions enable strict predicate validation.
type AttributeDefinition struct {
	Key         string     `json:"key"`
	ValueType   ValueType  `json:"value_type"`
	AllowedOps  []ClauseOp `json:"allowed_ops"`
	EnumValues  []string   `json:"enum_values,omitempty"`
	Description string     `json:"description,omitempty"`
}

// Facts is the attribute snapshot of one employee at one date.
// Attributes is a generic typed map keyed by attribute registry keys —
// this is what makes custom attributes a data change, not a code change.
type Facts struct {
	EmployeeID string         `json:"employee_id"`
	AsOf       string         `json:"as_of"` // "YYYY-MM-DD"
	Attributes map[string]any `json:"attributes"`
}

// Derived attribute keys injected at resolve time.
const (
	AttrHireDate  = "hire_date"
	AttrTenureDay = "tenure_days"
)

// DeriveAttributes returns a copy of attrs with derived attributes injected.
//
//   - tenure_days: days since hire_date, clamped at 0 (future hires have tenure 0).
//   - An explicit tenure_days in attrs always wins (explicit beats derived) —
//     mirrors how manual values outrank derived ones everywhere in this system.
//
// attrs is never mutated; the result is always a fresh map.
// An unparseable hire_date is an error ONLY when tenure_days is absent and
// some rule may need it — surfacing garbage loudly beats silently skipping.
func DeriveAttributes(attrs map[string]any, asOf time.Time) (map[string]any, error) {
	out := make(map[string]any, len(attrs)+1)
	for k, v := range attrs {
		out[k] = v
	}

	if _, explicit := out[AttrTenureDay]; explicit {
		return out, nil // explicit wins; nothing to derive
	}
	raw, present := out[AttrHireDate]
	if !present {
		return out, nil // nothing derivable
	}
	hireStr, ok := raw.(string)
	if !ok {
		return out, fmt.Errorf("derive %s: hire_date is %T, expected string", AttrTenureDay, raw)
	}
	hire, err := time.Parse(dateLayout, hireStr)
	if err != nil {
		return out, fmt.Errorf("derive %s: hire_date %q: %w", AttrTenureDay, hireStr, err)
	}
	days := int(asOf.Sub(hire).Hours() / 24)
	if days < 0 {
		days = 0 // future hires have tenure 0, not negative tenure
	}
	out[AttrTenureDay] = days
	return out, nil
}
