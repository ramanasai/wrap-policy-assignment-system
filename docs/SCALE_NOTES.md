# Scale Notes (100k+ Users)

Distilled from a broad Go-at-scale research brief. **Only the items that touch this system's
actual components.** Everything else (WebSockets, search, ML serving, geo, payments) was
triaged out — see `AGENTS.md` invariants: this system is about resolution correctness,
explainability, and reconciliation.

## 1. The read/write path (§11, §25)

### Our model IS event sourcing + CQRS

| Research pattern | Our implementation |
|---|---|
| Event sourcing — state as append-only events | `fact_event`, `rule_version`, `policy_version` (bitemporal, append-only) |
| CQRS — separate write/read models | Resolver computes on demand (read); reconciler maintains projection (write side) |
| Snapshotting — avoid replaying all history | Projection table + snapshot-version-keyed LRU = standing snapshot; full replay only for audits |
| Event schema versioning | `payload JSONB` + version columns; additive-only evolution |

**Go 1.27 note:** `encoding/json/v2` is now the default — measure before/after on the trace
and AST (un)marshal hot paths; unmarshal is reported significantly faster.

### CDC alternative to the outbox (§11)

Our transactional outbox is the right v1 (simple, doubles as audit). At larger scale,
**Debezium-style WAL tailing** eliminates the double-write question entirely and offloads
fan-out from the transactional path. Tradeoff: more moving parts, eventual-visibility of
events. Decision rule: swap when outbox polling/NOTIFY fan-out exceeds ~5k events/sec sustained.

## 2. Connection & pool discipline (§2, §11)

- **Pool exhaustion is the real bottleneck before CPU.** Sizing heuristic for pgx:
  `pool_size ≈ (core_count × 2) + effective_spindles`, capped by Postgres `max_connections`
  budgeted across services (API, reconciler, scheduler each get an explicit slice).
- **Transaction vs session pooling:** reconciler batches must use transaction-mode pooling
  (pgbouncer) carefully — `LISTEN/NOTIFY` requires session affinity, so the notify bridge gets
  its own dedicated connection pool, separate from query pools.
- Use `pgx` `MaxConns`, `MinConns`, `MaxConnLifetime` explicitly; no defaults in prod.

## 3. Reconciler resilience (§3)

- **Bulkhead isolation:** per-category concurrency caps in the reconciler (a pathological
  `app_access` rule change with 90k affected employees can't starve `pay_schedule`
  reconciliation). Implemented as a semaphore per category, global limit as backstop.
- **Retry with jittered exponential backoff** on external pushes; **retry budget** so a
  flaky downstream can't consume the whole outbox worker pool. Dead-letter after N attempts
  (the `outbox.attempts` column); DLQ review, not silent drop.
- **Load shedding:** under overload, the reconciler sheds *projection freshness* (read path
  is always correct — it pulls), never *decision correctness*. This is the payoff of
  "pull for decisions": the thing we shed is a cache.
- **Graceful shutdown:** reconciler drains in-flight outbox batches and exits between
  chunks; `FOR UPDATE SKIP LOCKED` means a killed worker loses nothing — the next worker
  claims the rows.

## 4. Go runtime specifics (§1, §31)

- **`uber-go/automaxprocs`**: mandatory — cgroup CPU limits make `GOMAXPROCS` default wrong
  in containers; wrong GOMAXPROCS + goroutine-per-request = latency collapse under load.
- **Worker pools for the reconciler** (bounded, chunked), goroutine-per-request for the API.
- **`GOMEMLIMIT`** set explicitly to keep GC from ballooning inside bursty recompute chunks;
  watch GC pause metrics via `runtime/metrics` against the p99 SLO.
- **Go 1.27 audit items** (per release notes): stdlib goroutine-leak profile in `runtime/pprof`
  (replaces bespoke leak tooling for the scheduler/reconciler tests); timer channels always
  unbuffered (the `asynctimerchan` GODEBUG crutch is gone — audit any scheduler code relying
  on buffered timer channels); native stdlib UUID for idempotency keys (drops a third-party
  dep); `go fix` modernizers for keeping migrations of old patterns cheap.

## 5. Multi-tenancy & audit at scale (§17)

- **Isolation strategy:** shared schema + `company_id` scoping at the repository layer (schema
  already has it). DB-per-tenant only if a customer demands physical isolation — cost curve
  documented in `TRADEOFFS.md`.
- **Noisy neighbor:** per-company reconciler concurrency caps + per-company rate limits on the
  API; one giant customer's 90k-employee department flip can't starve others.
- **Audit logging:** our `decision_trace` + `outbox` are already async-off-the-decision-path
  (traces are written as part of decision materialization, but decisions are *readable*
  regardless of trace write latency). Immutable, queryable — the §17 pattern.

## 6. Webhook/push delivery to external systems (§22)

- **At-least-once + idempotency keys** (Stripe pattern) — already in the outbox schema.
- **HMAC signature verification** on the receiving side; we sign outbound events with a
  per-consumer secret, include `event_id` + `recorded_at` for replay protection.
- **Retry with backoff + DLQ** for consumers; contract stability via versioned event payload
  (`event_schema_version`).

## 7. Capacity math (§13)

- Little's Law for the read path: at 200 req/s resolution queries × 15ms p99 → ~3 concurrent
  resolutions; CPU-bound cost is dominated by rule evaluation — size API pods off rules ×
  employees, not request count.
- **Load test the reconciler** with k6 (or a Go harness) at the Phase-5 demo scale, plus a
  100k-employee synthetic benchmark to substantiate the affected-set recompute claim in
  `ARCHITECTURE.md`.
- Test taxonomy: **load** (expected peak), **stress** (find the cliff), **soak** (projection
  drift/sweeper correctness over hours), **spike** (mass tenure-gate day: 10k employees
  crossing thresholds at midnight).

## What we deliberately did NOT adopt from the brief

Full-text search, Bloom filters/HyperLogLog, Raft/consensus (Postgres handles it),
service mesh, multi-region active-active, CDNs, feature-flag platforms, WebSockets,
ML feature stores, payment ledgers, geospatial indexing. Each is a real pattern with no
surface in this problem — citing them would be decoration, not engineering.
