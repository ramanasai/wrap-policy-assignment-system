# Policy Assignment System — Design Decisions (Council-Sourced)

Answers to the 16 scoping questions, synthesized from a 3-advisor senior-engineer panel
(oracle, reviewer, researcher) backed by web research on Rippling, SAP SuccessFactors,
Payroll Engine, NetIQ Identity Manager, MidPoint, NIST NGAC, and OPA.

## A. Temporality & Conflict Resolution

### Q1. Tenure policies — retroactive or effective-at-crossing?
**Decision: Bitemporal, with resolution as a pure function.**
- Employee attributes & rules stored as **immutable, effective-dated events** (valid time) + a
  system-time/processing-time column ("second clock").
- Tenure thresholds are **scheduled future-dated events** at the crossing date.
- Resolution = `f(rules@valid_date, attributes@valid_date)` → assignments. Never hand-edit
  assignments; they are derived state, always recomputable.
- Retroactive corrections are new events with `valid_time < processing_time`; "as of date Z"
  is answered by re-running resolution, not by patching stored rows.
- Tradeoff: bitemporality doubles schema complexity, but buys free auditability + replayable
  reconciliation. (Sources: SAP SuccessFactors effective dating, Payroll Engine "second clock")

### Q2. End-semantics when an employee leaves a rule's scope
**Decision: End-exclusive continuous coverage** — old assignment ends the day the new state
begins, `[start, end)` intervals, no gaps, no overlaps. **No grandfathering by default**;
grandfathering is an explicit opt-in flag on a rule ("lock-in existing members").

### Q3. Intraday resolution?
**Decision: Date granularity for valid time; full timestamps for system time only.**
Every policy domain operates on dates. If a tenure crossing lands mid-pay-period, payroll's
own period logic decides which pay run is affected — not the resolver. Keep the resolver
interval-agnostic internally so hourly shift policies can be added later.

### Q4. Tie-breaking hierarchy (exclusive assignments)
**Decision: Deterministic total order, applied in sequence:**
1. **Manual override** always wins
2. **Explicit priority** — admin-set integer on the rule (default equal)
3. **Automatic specificity** — computed from the predicate AST (each additional conjunct on a
   low-cardinality attribute narrows the population); cached, not admin-declared
4. **Stable final tiebreak** on `(rule created_at, rule id)`

Every resolution records the full ordered trace of why each loser lost.
Specificity is automatic (computable, no admin drift) with explicit priority as the escape hatch.

### Q5. Stackable vs exclusive; shadowed vs not-applied
**Decision: Cardinality class is a first-class property of the assignment TYPE:**
- `additive / many-to-many` (trainings, apps): union everything, no conflict concept
- `single-value / one-to-one` (manager, pay schedule): resolver must produce exactly one
- optional `bounded` (one time-off policy per subcategory)

For single-value types, the losing match is **SHADOWED, not discarded**: persist shadowed-match
records so deleting the winning rule deterministically resurrects the loser on next recompute.
Audit log records "rule R applied; rules R2, R3 matched and were shadowed by priority X."

### Q16. Submission format weighting
**Decision: 40% working code** (Postgres schema + runnable resolution engine + tests),
**30% design doc** with architecture diagram, **20% UX walkthrough** (rule builder, new-hire
policy preview, "you changed department → 4 assignments change" diff view), **10% tradeoffs**.
Highest-signal artifact: a demo of "why does X have Y as of date Z?" returning a
human-readable decision trace.

## B. Reconciliation, Architecture & Scale

### Q7. Scale target
**Decision: Design core for ~10k employees, degrade gracefully to 100k.**
Naive per-employee recompute is fine up to ~1k. Beyond that, an **inverted index**
(attribute-value → employee-id sets, Rippling "Supergroup" style) means a rule/attribute
change touches only the affected diff (entering/leaving employees), not the whole population.
Batched chunked recompute jobs (MidPoint-style). Demo at 1–10k with seeded synthetic data;
document the 100k path. Single-tenant-per-company keeps working sets small.

### Q8. Demo policy set (5 types spanning every axis)
1. **Manager assignment** — exclusive, org-chart-derived
2. **Pay schedule** — exclusive, attribute/tenure-based
3. **App access** — additive/multi, RBAC group-based
4. **Compliance training** — additive/multi, recurring with deadlines
5. **Benefits plan enrollment** — exclusive, manual+rule hybrid (drop if time-pressed)

Every type reuses one resolution engine; variation lives in per-type cardinality/source config.

### Q10. Consistency semantics
**Decision: Split the question.**
- **Resolution/decision consistency: transactional.** Attribute change committed → immediately
  visible to resolution in the same request (read-your-writes). Materialized projection updated
  in the same DB transaction if kept.
- **Enforcement (provisioning to Okta/SCIM/external): eventual**, at-least-once with
  idempotency/event store + reconciliation sweeper backstop, bounded observed SLA (seconds).
- Doc claim: *"decision consistency is transactional; enforcement convergence is eventual and measured."*

### Q11. Push vs pull
**Decision: Pull for decisions, push for actions.**
- **Primary read path: pull** — compute-on-read over effective-dated facts, cached keyed on
  (employee facts, date, policy-snapshot version). Policy edit bumps version → natural cache
  invalidation. Same-request consistency falls out; historical queries are pure recalculation.
- **Push/event-driven recompute** for materialized projections consumed by external systems:
  provisioning fan-out, notifications, dashboards. Periodic MidPoint-style scheduled recompute
  catches missed events.
- The materialized assignments table is **always a projection, never the source of truth**.

### Q12. Future-dated changes
**Decision: Must-support, first-class design constraint, built in v1.**
Every business record carries an effective start/end interval; resolution is parameterized by
date. A Jan-1 rule change is just a future interval — no code path difference. Scheduler enqueues
recompute/notify jobs at the effective date. Retrofitting effective-dating later means rewriting
the entire data model and history — build the two-clock model from the start.

## C. UX, Auditability & Data Model

### Q6. Declaring cardinality/conflict semantics
**Decision: First-class columns on a `policy_category` config table:**
- `cardinality ENUM('single','many')`
- `resolution_strategy ENUM('priority_rank','explicit_user_choice','additive')`
- `default_priority INT`, `tiebreaker ENUM(...)` — always a deterministic last-resort tiebreak

Enforcement lives in the resolver (one generic algorithm parameterized per category), not in
per-policy code. Category semantics immutable-once-in-use (or require migration + impact
preview) so historical decisions remain interpretable.

### Q9. Custom attributes/segments
**Decision: Fixed seeded attribute set for the demo (department, location, employment_type,
manager, level, hire_date), but the engine runs over a generic typed attribute map with an
`attribute_definition` registry table** — custom attributes then require zero engine changes.
Rules compile to predicates over `employee.attributes[key] → value`. Reusable segments
("field ops cohort") are named, materializable stored predicates (Supergroups), resolvable to
cached employee sets, composable into rules and previews.

### Q13. Rule authoring UX
**Decision: Form-driven condition builder** (attribute → operator → value, AND/OR grouping)
**compiling to the same canonical JSON rule AST the API accepts** — never a raw expression
language for this persona. Three-part harness (reuses the production resolver, no separate
preview logic to drift):
1. **Live match preview** — headcount + sample employee list
2. **Diff preview before saving** — who gains/loses the policy
3. **Per-employee "why matched / why not" inspector** with a date slider for what-ifs

### Q14. Onboarding UX
**Decision: Yes — live-derived policy readiness checklist, three fixed buckets:**
- **Auto-applied** (resolved deterministically)
- **Needs decision** (cardinality=single with competing rules — admin picks inline; the choice
  becomes an audit event)
- **Manual required** (no automated match)

Never store the checklist as mutable state — derive it from the resolver on every attribute/
policy change (stale checklists are the classic failure mode). Pair with "last computed as-of"
timestamp + change notifications. Also org-wide: "3 hires this week, 1 blocked."

### Q15. "Why does X have Y as of date Z?"
**Decision: Full decision trace (b), stored at decision time** — every evaluated rule with
matched/not-matched and why-lost (predicate failed on attribute K, lost tiebreaker, superseded
by later version), plus attribute snapshot and policy-definition snapshot version.
- Nearly free: a correct resolver evaluates all candidates anyway (it must, to detect conflicts).
- **Do not recompute explanations on demand** — answers silently change when inputs have since
  changed. Write traces as immutable events keyed by (employee, policy_category,
  effective_window, evaluation_trigger) with snapshot hashes.
- Render the winning-rule view as the default UI layer; expandable trace below.
- OPA decision logs are the industry precedent. Bound retention (full traces for audit window,
  aggregated winner-only summaries for older).

## Unifying Architecture (the one-paragraph story)

**One generic resolver; everything else is configuration or projection.**

```
Employee attributes ──┐
                      ├──> RESOLVER (pure function) ──> Effective assignments (per date)
Rules (versioned)  ───┤         │
Policy categories  ───┘         ├──> Decision traces (immutable, audit)
                                ├──> Readiness checklist (derived)
                                └──> Materialized projection ──> push events ──> provisioning/notify
```

- **Storage:** bitemporal, immutable, effective-dated events; versioned policy snapshots
- **Resolution:** pure `f(facts@date, rules@date) → assignments`, cardinality on the type,
  manual > priority > automatic specificity > created_at/id, shadowed matches persisted
- **Reconciliation:** MidPoint-style expected-vs-actual recompute, inverted-index affected-set
  recompute, idempotent push events, scheduled sweeper as backstop
- **Trust mechanism for admins:** live previews, diffs, and the decision-trace inspector
