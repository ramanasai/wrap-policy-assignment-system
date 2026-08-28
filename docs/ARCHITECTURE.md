# Architecture

## System overview

```mermaid
flowchart LR
    subgraph Sources["Sources of truth (immutable, effective-dated)"]
        FACTS["Employee fact events<br/>(hires, moves, tenure gates)"]
        RULES["Rules + policy versions<br/>(effective-dated, prioritized)"]
        CATS["Policy categories<br/>(cardinality + resolution strategy)"]
    end

    subgraph Core["Resolution core (PURE, no I/O)"]
        RES["Resolver<br/>resolve(rules@D, facts@D, category)"]
    end

    subgraph Derived["Derived artifacts"]
        ASSIGN["Effective assignments<br/>(query-time or materialized)"]
        TRACE["Decision traces<br/>(immutable, written at decision time)"]
        CHECK["Readiness checklist<br/>(derived, never stored)"]
        DIFF["Preview diffs<br/>(rule save / attribute change)"]
    end

    subgraph Push["Push layer (actions only)"]
        OUTBOX["Transactional outbox"]
        RECON["Reconciler worker<br/>(inverted-index affected sets)"]
        PROJ["Materialized projection"]
        FAN["Event fan-out<br/>(provisioning, notifications)"]
    end

    AdminUI["Admin UI<br/>(form-driven rule builder)"] -- "compiles to JSON AST" --> RULES
    FACTS --> RES
    RULES --> RES
    CATS --> RES
    RES --> ASSIGN
    RES --> TRACE
    RES --> CHECK
    RES --> DIFF
    FACTS -- "change event" --> OUTBOX
    RULES -- "change event" --> OUTBOX
    OUTBOX --> RECON
    RECON --> PROJ
    PROJ --> FAN
    SWEEPER["Scheduled sweeper<br/>(MidPoint-style backstop)"] --> RECON
```

## Components

### 1. Bitemporal event store (Postgres)

Everything that changes over time is an **append-only event with two clocks**:

- **valid time** — when the fact is true in the business world (`valid_range: daterange`),
  possibly in the future ("transfers to Engineering on the 15th") or backdated ("actually
  relocated to CA on Feb 1").
- **processing time** — when the system learned it (`recorded_at`), plus supersession pointers.

Employee attributes, rules, and policy definitions all live here. Nothing is `UPDATE`d in
place; a correction is a new event that supersedes the old one.

```mermaid
flowchart TD
    E1["Feb 1: Alice hired — dept: Sales, loc: NY<br/>(recorded Feb 1)"]
    E2["Mar 3: Alice relocates — loc: CA<br/>(valid Mar 3, recorded Mar 5 = backdated)"]
    E3["Mar 5: correction recorded<br/>valid-time < processing-time"]
    Q["Query: facts_at(Alice, Mar 3)?<br/>→ dept Sales, loc CA (post-correction view)"]
    E1 --> E2 --> E3 --> Q
```

### 2. Resolver (pure function)

The heart of the system. **No I/O, no framework imports — a pure Go package.** Signature:

```go
func Resolve(
    facts    Facts,           // snapshot of attribute map at date D
    rules    []RuleVersion,   // effective, non-superseded rule versions at D
    category CategoryConfig,  // cardinality, resolution strategy, tiebreakers
    date     time.Time,
) ResolutionResult
// .Assignments — winner(s)
// .Shadowed    — matched-but-superseded rules (for 'single' categories)
// .Trace       — every rule evaluated: matched? why lost? inputs used
```

Pipeline inside `resolve`:

1. **Filter** — evaluate each rule's predicate against `facts` → matched set.
2. **Order** — sort matched rules by the deterministic total order:
   `manual_override → explicit priority desc → automatic specificity desc → (created_at, id)`.
3. **Select** — for `single` categories take the head; the rest become `shadowed`.
   For `many` categories take all (additive).
4. **Trace** — record per-rule: matched/not, failing predicate clause, loss reason,
   fact snapshot hash, policy version hash.

**Determinism invariants** (property-tested): same inputs → identical output, always;
`single` categories yield exactly one assignment or zero (with the reason in the trace).

#### Automatic specificity

Computed from the predicate AST, cached per rule version. Each conjunct narrows by the
cardinality of the attribute it constrains (`location = CA` narrows more than
`employment_type = full_time`). Ranked deterministically; explicit priority exists as the
admin's override when the automatic ranking surprises them.

### 3. Read path: pull for decisions

| Query | How it's served |
|---|---|
| "Policies for employee E on date D" | Compute-on-read via resolver; LRU cache keyed on `(policy_version_max, facts_version(E), D)` |
| "Why does E have P as of D?" | Read the stored decision trace (never recomputed — inputs may have changed) |
| "Assignments for payroll run of March" | Compute-on-read over the batch of employees; results cached per snapshot version |
| "What will change if we hire Bob with attrs X?" | Same resolver, hypothetical facts → readiness checklist |

A policy or rule edit bumps the snapshot version → all derived caches invalidate naturally.
No bespoke invalidation logic.

### 4. Write path: push for actions

```mermaid
sequenceDiagram
    participant HR as HR Admin/API
    participant DB as Postgres
    participant W as Reconciler worker
    participant Ext as External systems

    HR->>DB: Commit attribute change (tx)
    DB->>DB: Write outbox row in SAME tx (idempotency key)
    DB-->>W: NOTIFY new_outbox
    W->>DB: Claim outbox rows (FOR UPDATE SKIP LOCKED)
    W->>W: Inverted index → affected employees + diff (entering/leaving)
    W->>DB: Update materialized projection (per affected diff)
    W->>Ext: Push events (at-least-once, idempotent)
    W->>DB: Mark processed / dead-letter
    Note over DB,Ext: Sweeper periodically recomputes stragglers<br/>(expected-vs-actual, MidPoint-style)
```

- **Transactional outbox**: the change and its event commit atomically — no dual-write bug.
- **Inverted index**: `attribute_value → employee_ids` (Rippling Supergroup pattern). A rule
  edit or attribute change touches only the diff — employees entering or leaving the affected
  predicate scopes — never a full-company recompute.
- **Sweeper backstop**: a scheduled expected-vs-actual reconciliation catches missed events,
  guaranteeing the projection converges even after failures.

### 5. Consistency contract

> **Decision consistency is transactional; enforcement convergence is eventual and measured.**

- Reading decisions after a committed change is always correct (read-your-writes on facts).
- Materialized projections and external pushes converge within a bounded, observed SLA
  (seconds), with the sweeper as the correctness floor.
- The materialized table is **always a projection, never the source of truth** — it can be
  dropped and rebuilt from events at any time.

### 6. Scale path

| Employees | Strategy |
|---|---|
| ≤ 1k | Naive per-employee recompute on change is fine |
| ~10k (demo target) | Inverted-index affected sets + batched chunked recompute |
| ~100k | Same architecture; throughput problem only — chunking, parallel workers, per-shard (per-company) isolation. Single-tenant-per-company keeps resolution domains small. |

## Failure modes & how the design answers them

| Failure | Answer |
|---|---|
| Winning rule deleted → coverage silently vanishes | Shadowed matches persist; loser resurrects on next recompute |
| Backdated correction lands after payroll ran | Bitemporal replay: resolution for affected dates recomputes; traces flag the retro window |
| Two rules both match a `single` category | Deterministic total order; loser shadowed; full trace explains |
| Event lost after DB commit | Sweeper expected-vs-actual recompute catches drift |
| Admin fat-fingers a broad rule | Preview diff before save shows exactly who gains/loses |
| "Why did this change?" after the fact | Immutable decision traces keyed by (employee, category, window, trigger) |
