# API Contract

Thin chi-based HTTP layer over the resolver. Every mutating endpoint writes an outbox row in
the same transaction. The rule AST JSON here is the **same type** the form builder compiles to
and the resolver consumes.

## Conventions

- JSON errors: `{"error": {"code": "...", "message": "..."}}`
- All dates are `YYYY-MM-DD` (valid time); server timestamps are RFC 3339 (processing time).
- `as_of` defaults to today; `company_id` from auth context (single-company in v1).

## Employees & facts

### `POST /employees`

```jsonc
// Request
{ "name": "Priya Sharma", "hired_on": "2024-01-19",
  "facts": { "department": "Engineering", "location": "US-CA", "employment_type": "full_time" } }
// 201 → { "id": "emp_123", "readiness": { ...checklist, see below } }
```

### `POST /employees/{id}/facts`

Adds a fact event. Supports **future-dating** and **backdating** (both just `valid_from`).

```jsonc
{ "attribute_key": "location", "value": "US-NY",
  "valid_from": "2026-01-15",          // omit = today; past = backdated correction
  "trigger": "hr_edit" }
// 200 → { "diff": { "gained": [...], "lost": [...], "needs_decision": [...] } }
```

The `diff` is computed by the resolver (facts before vs after) — this is the
"you changed a department, 4 assignments change" payload from `docs/UX_FLOWS.md` §4.

### `GET /employees/{id}/facts?as_of=DATE`

As-of attribute snapshot (bitemporal query, supersession-aware).

## Rules

### `POST /rules`

```jsonc
{
  "category_id": "vacation",
  "policy_id": "pol_ca_enhanced",
  "priority": 0,                          // explicit; default 0 (Standard)
  "predicate": {                          // canonical AST
    "op": "and",
    "clauses": [
      { "attr": "location",    "op": "eq",  "value": "US-CA" },
      { "attr": "tenure_days", "op": "gte", "value": 730 }
    ]
  },
  "valid_from": "2026-01-01"              // omit = immediately
}
// 201 → { "id": "rule_42", "specificity_rank": 2,
//         "preview": { "matches_now": 23, "sample": ["emp_7", "emp_88"] } }
```

### `POST /rules/preview` — dry-run diff (the save gate)

Same payload as `POST /rules` (or a proposed edit + `rule_id`), but nothing is written.
Returns the exact before/after computed by the resolver:

```jsonc
{ "gained": [{ "employee_id": "emp_7", "name": "Alice Chen", "via": "rule_42" }],
  "lost":   [{ "employee_id": "emp_9", "name": "Sam Okafor",
               "reason": "lost priority tiebreak to rule_31" }],
  "unchanged_conflicts": [{ "employee_id": "emp_12", "category_id": "pay_schedule" }] }
```

### `PUT /rules/{id}/versions` — new effective-dated version (never edits history)

## Resolution & explanation

### `GET /employees/{id}/assignments?as_of=DATE`

All effective assignments grouped by category:

```jsonc
{ "as_of": "2026-03-03", "categories": {
    "vacation":    { "policy_id": "pol_ca_enhanced", "rule_id": "rule_42", "source": "authored" },
    "app_access":  [ { "policy_id": "pol_figma" }, { "policy_id": "pol_github" } ],
    "manager":     { "policy_id": "mgr_jordan", "rule_id": "rule_11" } } }
```

### `GET /employees/{id}/explain?category=X&as_of=DATE`

Returns the **stored** decision trace (immutable, written at decision time):

```jsonc
{ "outcome": "assigned",
  "short_answer": "Rule 'CA 2yr+ tenure' matched and won on specificity (2 clauses > 1).",
  "evaluated": [
    { "rule_id": "rule_42", "matched": true,  "rank": 1, "outcome": "winner" },
    { "rule_id": "rule_07", "matched": true,  "rank": 2, "outcome": "shadowed",
      "why_lost": "specificity: 1 clause < 2" },
    { "rule_id": "rule_15", "matched": false,
      "why_not": "employment_type 'full_time' != 'contractor'" } ],
  "facts_snapshot":  { "location": "US-CA", "tenure_days": 743 },
  "policy_snapshot": { "category": "vacation", "cardinality": "single" } }
```

### `POST /employees/preview` — hypothetical hire (readiness checklist)

Full attribute map in, derived checklist out — powers the onboarding flow:

```jsonc
// `contains` matches list-valued facts (segment memberships):
//   { "attr": "segments", "op": "contains", "value": "field_ops" }

// Request: attribute map only (employee doesn't exist yet)
// Response:
{ "auto_applied":    [ { "category_id": "vacation", "policy_id": "pol_ca_enhanced", "via": "rule_42" } ],
  "needs_decision":  [ { "category_id": "manager",
                         "options": [ { "policy_id": "mgr_jordan", "rule_id": "rule_11", "rank": 1 } ],
                         "blocked": ["rule_19"] } ],
  "manual_required": [ { "category_id": "benefits_401k", "reason": "no rule covers this category" } ] }
```

## Internal endpoints (workers)

- `POST /internal/reconcile` — force expected-vs-actual sweep (ops tooling)
- `GET /healthz`, `GET /readyz`

## Idempotency

All `POST`s accept `Idempotency-Key`; the outbox dedupes on it, so a retried HR edit can never
double-fire downstream provisioning.
