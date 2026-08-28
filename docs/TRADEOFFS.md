# Tradeoffs & Limitations

What we chose **not** to do, why, and what it would take to change course. Honest
documentation of the gaps — flagged rather than hidden.

## Architectural tradeoffs

### 1. Bitemporal everything vs. effective-dating only

**Chose:** bitemporal (valid time + processing time) from day one.

| Pros | Cons |
|---|---|
| "As of date Z" audits are replayable, forever | ~2x schema complexity on temporal tables |
| Backdated corrections are first-class, not hacks | Every as-of query must handle supersession — easy to get subtly wrong |
| Future-dating comes free (same machinery) | Appends grow unboundedly (mitigated: retention policy) |

**Alternative rejected:** effective-dating only. Cheaper, but a backdated correction after
payroll ran would permanently corrupt the audit answer — the one thing the spec demands.

### 2. Derived assignments vs. materialized truth

**Chose:** assignments are derived; the materialized table is a rebuildable projection.

| Pros | Cons |
|---|---|
| Correct by construction; no drift between "truth" and "assignments" | Read latency on cold cache (mitigated: snapshot-version-keyed LRU) |
| Historical queries = pure recomputation | Heavy consumers (reporting) need the projection maintained |
| Truncating and rebuilding the table is safe | Requires discipline: nothing may treat `assignment` rows as authoritative |

**Alternative rejected:** materialized-only (write-time assignment). Faster reads, but every
optimization of the write path risks drift, and "why" questions need the trace anyway.

### 3. Pull-for-decisions / push-for-actions vs. push-everywhere

**Chose:** resolver computes on demand; push only feeds projections and external systems.

| Pros | Cons |
|---|---|
| Same-request consistency after HR edits — no staleness in decisions | Every decision re-evaluates rules (mitigated: version-keyed cache) |
| Policy edit invalidates caches via version bump — no invalidation logic | Projection lag between change and push (bounded, observed SLA) |
| Simpler failure story: decisions can't be "behind" | Two read paths to document (direct resolve vs projection) |

### 4. Deterministic total order vs. error-on-conflict

**Chose:** always resolve deterministically (manual > priority > specificity > created_at/id);
surface conflicts in UI rather than blocking.

| Pros | Cons |
|---|---|
| Resolution never deadlocks payroll/onboarding on a config mistake | A mis-prioritized rule resolves *silently* — mitigated by the conflicts tab + shadowed-match surfacing |
| Explainable: every loss has a recorded reason | Specificity ranking can surprise admins — mitigated by explicit priority override |

**Alternative rejected:** error-on-conflict (make admin resolve manually). Safer-feeling, but
a single overlapping rule would block a whole department's pay schedule — availability beats
purity, given the conflicts are visible.

### 5. Shadowing vs. not-applied for losing rules

**Chose:** persist shadowed matches; losers resurrect when the winner is deleted.

| Pros | Cons |
|---|---|
| Deleting a rule never silently drops coverage | More resolution-output volume (cap trace depth) |
| Matches admin intuition: "removing the exception restores the default" | Slightly complex reconciler logic |

### 6. Semantics as data (category columns) vs. per-policy code

**Chose:** cardinality + resolution strategy as declarative columns on `policy_category`.

| Pros | Cons |
|---|---|
| One generic resolver; new policy types are rows, not code | Less flexible than arbitrary per-category conflict logic |
| Every conflict is explainable from config alone | Categories frozen after use (`immutable_after_use`) — changing semantics needs a migration |

### 7. Go + sqlc vs. alternatives

**Chose:** Go 1.27, sqlc + pgx, chi. The bitemporal queries are the hard part; sqlc keeps
them as reviewed SQL with compile-checked Go bindings.

| Pros | Cons |
|---|---|
| Typed end-to-end: SQL → rows → resolver structs → API | More ceremony for CRUD than a dynamic ORM |
| Single static binary; workers + API in one artifact | Property-based testing less mature than Python's hypothesis (`rapid` is good, not equal) |
| `pgtype.Range` maps `daterange` natively | Slower iteration than Python for the UX mockups (kept those out of Go entirely) |

## Scope limitations (v1 prototype) — flagged, not hidden

| Limitation | Impact | Path to production |
|---|---|---|
| **No real scheduler daemon** — future-dated transitions processed by a simple worker loop | A stopped worker delays transitions | Replace with persistent job queue; semantics unchanged |
| **Postgres-as-queue** for outbox | Contention at very high event rates | Swap to NATS JetStream; outbox table stays as audit |
| **In-process LRU cache** | Multi-instance deployments recompute per node | Redis keyed on the same version tuple |
| **No UI built** — UX flows are mockups + API-backed flows | UX criterion rests on documentation | The form builder compiles to the same JSON AST the API already accepts |
| **Custom attributes supported by schema, seeded minimally** | Demo shows 6 standard attributes | Registry is live; adding attributes is data, not code |
| **Retention/aggregation of old traces not implemented** | Trace table grows | Documented policy: full traces for audit window, winner-only summaries beyond |
| **Reconciler affected-sets via inverted index, not tested at 100k scale** | Scale claims are architectural, not benchmarked | Seed 100k synthetic employees; benchmark chunked recompute |
| **No multi-company tenancy enforcement in resolver** | Demo is single-company | `company_id` scoping at the repository layer (already in schema) |

## Deliberate non-goals

- **Accrual/balance computation** (vacation day math) — payroll domain; we only decide
  *membership* in policies.
- **Approval workflows** (who signs off on a rule change) — documented as a Temporal use case.
- **External policy engines (OPA/Cedar)** — they don't model cardinality, effective dating,
  or shadowing; we'd fight the tool. Revisit only for app-access *authorization checks*
  (a different question than assignment).
- **Real-time streaming reconciliation** — batched chunked recompute meets the SLA; streaming
  adds failure modes without changing answers.

## What would change our mind

- If **retroactive audit** turns out to be a nice-to-have: drop the second clock, keep
  effective-dating — ~30% schema simplification.
- If companies routinely need **arbitrary conflict logic** per category: move from declarative
  strategy columns to a small expression-based resolution plugin — at the cost of
  explainability guarantees.
- If **read volume** dwarfs write volume by 1000:1 (e.g., payroll-day storms): promote the
  projection to the primary read path with the resolver as the correctness fallback.
