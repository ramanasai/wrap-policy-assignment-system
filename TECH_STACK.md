# Tech Stack

Chosen for: **correctness first, then explainability, then delight**. Every choice optimizes
for a take-home that a senior engineer can read in 30 minutes and trust.

## Summary table

| Layer | Choice | Why (one line) |
|---|---|---|
| Language | **Go 1.27** | Typed, single static binary; the resolver is a pure function package — determinism in the type system, trivial deployment |
| API framework | **chi (or Gin) + net/http** | Thin, idiomatic routing shell around the resolver; no magic |
| Database | **PostgreSQL 16** | Bitemporal ranges (`tstzrange`/`daterange`), exclusion constraints, JSONB — one DB does everything |
| Data access | **sqlc + pgx/v5** | Hand-written SQL compiled to fully-typed Go — the bitemporal range queries live in `.sql` files, reviewed as SQL, not buried in ORM chains |
| Schema migrations | **golang-migrate** (or goose) | Versioned, reviewable SQL migrations; pairs with the policy-versioning story |
| Rule AST / JSON | **Plain Go structs + `encoding/json` (+ `jsonschema` gen)** | One canonical AST type shared by API, resolver, and (future) form builder |
| Tests | **testify + rapid (property-based)** | Rapid for determinism invariants (same inputs → same outputs, exactly one `single` winner) |
| Task queue / events | **Postgres `LISTEN/NOTIFY` + outbox table** (v1) | No extra infra; the outbox IS the audit trail. NATS JetStream swap-in later |
| Scheduler | **Postgres-backed job table + worker goroutine** | Future-dated changes are just rows with `effective_date` in the future |
| Caching (read path) | **In-process LRU keyed on policy-snapshot version** (`hashicorp/golang-lru`) | Cache invalidation = version bump; no Redis needed for the demo |
| Logging / metrics | **zerolog (structured JSON) + Prometheus** | zero-allocation JSON logging; loggers injected via `internal/logging`, never imported ad hoc |
| Frontend (walkthrough) | **Figma mockups + static HTML** | UX criterion asks for flows, not a production SPA |
| Diagrams | **Mermaid in-repo** | Diagrams live beside the code, diffable in PRs |

## Infrastructure choices, expanded

### PostgreSQL — the load-bearing decision

The entire temporal model maps cleanly onto Postgres primitives:

- **Effective-dated records** → `daterange` columns + GiST **exclusion constraints**
  (e.g. one pay schedule per employee: `EXCLUDE USING gist (employee_id WITH =,
  valid_range WITH &&) WHERE (category_id = 'pay_schedule')`) make double-booking a
  one-per-category assignment *impossible at the DB level*, not just at the app level.
- **Bitemporality** → two interval columns: `valid_range` (business truth) and
  `recorded_at`/superseded-by columns (processing time). Postgres range operators
  (`@>`, `&&`, `lower_inc`) express "facts as of date D" as plain indexed SQL.
- **Policy versioning** → immutable `policy_version` rows; rules reference versions, giving
  copy-on-write policy snapshots (the NGAC pattern) without a second datastore.
- **Outbox + LISTEN/NOTIFY** → transactional event publication for the push side, with the
  outbox table doubling as the audit log. A dedicated broker (NATS JetStream) is the right
  swap at scale — documented in `TRADEOFFS.md`, not built here.

**Rejected:** MongoDB (no range types, bitemporal queries become app-level code),
Datomic-style DBs (correct fit for immutability, but opaque to graders and heavy to run),
Temporal/workflow engines (wrong abstraction — this is a decision system, not orchestration).

### Go + sqlc — the load-bearing code decision

- **sqlc means the bitemporal queries are SQL first.** "Facts as of date D with
  post-correction visibility" is a `DISTINCT ON ... WHERE valid_range @> $1` query that lives
  in `db/queries/facts.sql`, gets type-checked, and compiles to a typed Go method. The
  hardest parts of this system are the range/supersession queries — sqlc forces them to be
  reviewed as SQL instead of leaking into application code.
- **pgx/v5** natively maps Postgres `daterange`/`tstzrange` → Go `pgtype.Range[T]`, so the
  two-clock model round-trips without adapters.
- **The resolver package has zero dependencies** — no DB types, no HTTP types. It consumes
  plain Go structs (`Facts`, `RuleVersion`, `CategoryConfig`) and returns
  `ResolutionResult`. sqlc row types convert to resolver types at the repository boundary;
  this keeps the pure core testable in microseconds.
- **Single binary, `goroutine`s for workers.** The reconciler, scheduler, and API deploy as
  one static binary (or three processes from one binary). LISTEN/NOTIFY wakes the reconciler;
  `FOR UPDATE SKIP LOCKED` claims outbox batches — the classic Postgres-as-queue pattern
  without Redis/Rabbit.
- Rejected Python for this submission: fine for the resolver, but sqlc + pgx + Go gives the
  whole pipeline (typed SQL → typed rows → typed resolver input → typed output) a single
  compile-checked spine, which is exactly the DX story we want to demo.

### The resolver stays pure

- The resolver is a **pure, dependency-free Go package** (`resolver/`) — no framework, no DB
  types, no I/O. The chi API and workers are thin shells around it. This is deliberate: the
  interesting artifact is the resolver, and it must be importable and testable in isolation
  (microsecond unit tests, no fixtures).
- Plain Go structs define the **canonical rule AST**, so the API contract, the form-builder
  compiler target, and the resolver input are literally the same types; a JSON Schema can be
  generated from them for the UI.
- `rapid` (Go's property-based testing, QuickCheck lineage) covers the determinism invariants
  that matter for a conflict-resolution engine.

### What we deliberately did NOT add (v1)

| Not added | Why | When to add |
|---|---|---|
| Redis | Cache keys are version tuples; in-process LRU suffices | Multi-instance read fan-out |
| NATS / Kafka | Postgres outbox + NOTIFY covers eventing at 10k employees | >50k employees or polyglot consumers |
| OPA / Cedar | External policy engines don't model cardinality or effective dating; we'd fight them | Never for this domain; maybe for app-access authz checks specifically |
| Kubernetes | Docker Compose is the entire deployment story | Production multi-service rollout |
| Separate projection store (ES) | Read models are small; Postgres materialized tables suffice | Reporting/analytics workloads |

## Repository layout

```
.
├── README.md
├── AGENTS.md                # repo guide: invariants, commands, conventions
├── SUBMISSION.md            # grader front door: criteria → evidence mapping
├── TECH_STACK.md            ← you are here
├── DECISIONS.md
├── LICENSE                  # MIT
├── system-map/              # interactive system map (Archify)
│   ├── architecture.json    #   typed IR (source of truth)
│   ├── index.html           #   self-contained interactive map
│   └── preview.png          #   rendered preview (embedded in README)
├── docs/
│   ├── ARCHITECTURE.md
│   ├── DATA_MODEL.md
│   ├── API.md               # HTTP contract + canonical rule AST
│   ├── UX_FLOWS.md
│   ├── EMAIL_DRAFT.md           # ready-to-send submission email
│   ├── SCALE_NOTES.md       # distilled 100k+ patterns that touch THIS system
│   ├── TRADEOFFS.md
│   └── PROTOTYPE_PLAN.md
├── cmd/
│   ├── server/              # chi API binary (:8080, graceful shutdown)
│   ├── worker/              # reconciler + sweeper + scheduler
│   ├── seed/                # deterministic demo company
│   └── demo/                # scripted narrative (make demo / docker run --rm demo)
├── sqlc.yaml                # sqlc config: migration schema → gen/db typed Go
├── db/
│   ├── migrations/          # golang-migrate-compatible SQL migrations
│   ├── queries/             # sqlc .sql files (facts, rules, traces, outbox, index)
│   └── seed/                # demo company: 1k employees, 9 categories
├── gen/                     # sqlc-generated typed Go (pgx/v5) — DO NOT hand-edit
│   └── db/
├── resolver/                # PURE — no I/O, no framework deps, NO logging
│   ├── ast.go               #   rule AST types + JSON parse/validate/matches
│   ├── value.go             #   normalized comparables + clause evaluation
│   ├── facts.go             #   Facts, attribute definitions, derived attrs (tenure)
│   ├── specificity.go       #   automatic specificity ranking from AST
│   ├── conflicts.go         #   tie-break total order + shadowing
│   ├── trace.go             #   decision-trace construction
│   └── resolve.go           #   Resolve(input) → Result (assignments+trace)
├── internal/
│   ├── config/              # typed env config (Load/MustLoad; fail-fast validation)
│   ├── logging/             # zerolog setup: component loggers, request IDs
│   ├── utils/               # env getters, .env loading, date helpers
│   ├── repo/                #   converts sqlc rows ↔ resolver types (the only boundary)
│   ├── api/                 #   chi routes: rules, employees, resolve, explain, preview
│   ├── events/              #   outbox writer + LISTEN/NOTIFY bridge
│   ├── reconciler/          #   event → inverted-index affected-set recompute
│   └── scheduler/           #   future-dated effective transitions
└── tests/
    ├── determinism_test.go  #   rapid: same input → same output
    ├── cardinality_test.go  #   exactly-one for 'single', additive for 'many'
    ├── temporal_test.go     #   as-of queries, retroactive corrections
    └── reconciliation_test.go
```

## Desired tools/services (production wishlist)

- **NATS JetStream** — durable event fan-out to provisioning/notification consumers
- **Temporal (Go SDK)** — long-running human-in-the-loop flows (e.g., multi-step offboarding)
- **OpenTelemetry + Grafana** — per-decision latency, reconciliation lag SLOs
- **Feature-flagged dual-write resolver** — canary new rule semantics against shadow traffic
