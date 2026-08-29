# Warp — Policy Assignment System

> 📬 **For graders:** start at [`SUBMISSION.md`](./SUBMISSION.md) — how to run,
> criteria mapping, and the demo.
>
> A take-home system design for the problem described in `DECISIONS.md`: a single, generic
substrate that decides **which policies apply to which employees on any given date** — for
time off, pay schedules, app access, compliance trainings, benefits, managers, and more.

## The one-paragraph story

> **One generic resolver; everything else is configuration or projection.** Employee facts and
> rules are stored as immutable, effective-dated (bitemporal) events. Resolution is a *pure
> function* of `(rules @ date, employee facts @ date)` — never hand-edited mutable state.
> Every policy category declares its cardinality and conflict semantics declaratively; the
> resolver produces deterministic, fully-traced decisions that answer *"why does employee X
> have assignment Y as of date Z?"* for any X, Y, and Z.

## Documentation map

**Root:**

| File | What it covers |
|---|---|
| [`README.md`](./README.md) | This overview |
| [`DECISIONS.md`](./DECISIONS.md) | The 16 scoping questions, answered and sourced (council record) |
| [`TECH_STACK.md`](./TECH_STACK.md) | Infrastructure choices, tools & services, pros/cons |
| [`AGENTS.md`](./AGENTS.md) | Repo guide for coding agents/humans: invariants, commands, conventions |

**`docs/`:**

| File | What it covers |
|---|---|
| [`docs/ARCHITECTURE.md`](./docs/ARCHITECTURE.md) | System design: components, resolution & reconciliation flows, diagrams |
| [`docs/DATA_MODEL.md`](./docs/DATA_MODEL.md) | Postgres schema: bitemporal events, policy categories, rules, traces |
| [`docs/API.md`](./docs/API.md) | HTTP contract; the canonical rule AST shared by API, form builder, resolver |
| [`docs/UX_FLOWS.md`](./docs/UX_FLOWS.md) | Admin experience: rule builder, onboarding checklist, change warnings, explain inspector |
| [`docs/SCALE_NOTES.md`](./docs/SCALE_NOTES.md) | Go-at-scale patterns that apply here: pooling, bulkheads, CDC-vs-outbox, Go 1.27 audit items |
| [`docs/TRADEOFFS.md`](./docs/TRADEOFFS.md) | What we chose NOT to do, known limitations, future work |
| [`docs/PROTOTYPE_PLAN.md`](./docs/PROTOTYPE_PLAN.md) | Build order for the reference implementation |

## Core concepts

```
Policy category   a kind of thing that can be assigned (pay schedule, app access…)
                  → declares cardinality ('single' | 'many') + resolution strategy
Policy            a concrete assignable object (e.g. "US Bi-Weekly Pay", "Figma")
Rule              a predicate over employee attributes → a policy
                  → effective-dated, prioritized, versioned
Assignment        the derived fact "employee E has policy P on date D"
                  → ALWAYS computed, never hand-written (manual overrides are rules too)
Decision trace    the immutable record of WHY: every rule evaluated, who won, why losers lost,
                  plus attribute & policy snapshots at decision time
```

## Cardinality semantics (declared per category)

- **`single`** — exactly one per employee (manager, pay schedule). Conflicts resolved by
  deterministic total order: *manual override > explicit priority > automatic specificity >
  (created_at, id)*. Losing matches are **shadowed**, not discarded — they resurrect if the
  winning rule is deleted.
- **`many`** — additive (app access, trainings). All matches stack; no conflict concept.

## Resolution is a pure function

```go
// resolver/resolve.go — pure, zero dependencies
func Resolve(
    facts    Facts,            // snapshot of employee attribute map at date D
    rules    []RuleVersion,    // effective, non-superseded rule versions at D
    category CategoryConfig,   // cardinality, resolution strategy, tiebreakers
    date     time.Time,
) ResolutionResult
// .Assignments — winner(s)
// .Shadowed    — matched-but-superseded rules (for 'single' categories)
// .Trace       — every rule evaluated: matched? why lost? inputs used
```

Because inputs are effective-dated and versioned, **any historical query is just a replay**:
"What paid schedules applied to Alice on March 3rd?" = re-run `resolve` with March 3rd inputs.
Nothing is patched; everything is recomputed.

## Reconciliation model

- **Decision consistency is transactional:** an HR attribute change is immediately visible to
  resolution (read-your-writes).
- **Enforcement convergence is eventual and measured:** pushes to external systems
  (provisioning, notifications) are idempotent, at-least-once, with a sweeper backstop.
- **Pull for decisions, push for actions:** the resolver answers queries on demand; an
  event-driven layer materializes projections for external consumers only.
- **Inverted attribute index** means a rule or attribute change recomputes only the affected
  diff (employees entering/leaving each rule's scope), not the whole company.

## Quickstart

```bash
# Option A — one command (Docker):
docker compose up --build          # postgres + API (:8080) + reconciler worker
docker compose run --rm demo       # the scripted narrative (grader artifact)

# Option B — local Postgres (recommended for development):
make migrate   # apply schema to local Postgres (port 55432)
make demo      # seed the demo company (1k employees) + run the scripted narrative
cat demo/output.txt   # the captured run
```

## Status

- [x] Scoping questions answered (`DECISIONS.md`)
- [x] Tech stack, architecture, data model, API contract, UX flows, tradeoffs documented
- [ ] Reference implementation (schema + resolver + tests) — see [`docs/PROTOTYPE_PLAN.md`](./docs/PROTOTYPE_PLAN.md)

See [`docs/PROTOTYPE_PLAN.md`](./docs/PROTOTYPE_PLAN.md) for the build order.
