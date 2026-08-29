# Changelog

All notable changes to this project are documented here. Format based on
[Keep a Changelog](https://keepachangelog.com/); entries are grouped by session/date,
newest first.

## [Unreleased] — 2026-08-28 (Session 11 — submission packaging)

### Added
- **`SUBMISSION.md`** — the grader's front door: pitch, how to run, every
  evaluation criterion mapped to where it's satisfied + evidence, doc map,
  demo summary, honest limitations.
- **`docs/EMAIL_DRAFT.md`** — ready-to-send email (To: eng@warp.co).
- **MIT LICENSE**; README quickstart split into Docker + local paths; README
  now points graders at SUBMISSION.md.
- **CI workflow** (`.github/workflows/ci.yml`) prepared — vet + full suite
  against a live Postgres 16 service container (integration tests run, not
  skipped). NOT yet pushed: uploading workflow files requires a
  `workflow`-scoped token (`gh auth refresh -h github.com -s workflow`);
  the file sits untracked until then.
- **Repository made PUBLIC** for the submission
  (github.com/ramanasai/wrap-policy-assignment-system). Secrets audited
  before the flip: `.env` untracked, no credentials in tracked files.

### Validated
- Markdown validator clean; full test suite green; repo clean. Submission
  package cross-links verified.

## [Unreleased] — 2026-08-28 (Session 10)

### Added
- **One-command deployment**: `Dockerfile` (multi-stage, four static
  binaries), `docker-compose.yml` (postgres + API :8080 + reconciler worker
  + one-shot seed with `service_completed_successfully` gating + `run`
  profile demo), `.dockerignore`. `make demo` already demonstrated locally;
  README quickstart now offers both docker and local-Postgres paths.
  Honest note: image build could NOT be verified on this machine — Docker
  Hub is unreachable here (persistent EOF/TLS failures, same as earlier
  sessions); compose YAML validated with `docker compose config` and every
  binary verified via local build + live runs instead.
- **`cmd/server`** — the chi API binary with graceful shutdown and
  `ReadHeaderTimeout`; verified live: healthz/readyz, create-employee with
  readiness, and explain returning a STORED trace over HTTP.

### Fixed
- **Additive dedup bug (live sweep catch)** — two additive rules mapping to
  the SAME policy (tenure-based AND segment-based security training) made
  the resolver emit duplicate policy rows; the reconciler then failed on
  `assignment_pkey` during materialization (sweep aborted at the first hit).
  Additive semantics are a SET of policies: `resolve.go` now dedupes by
  (policy, policy version) in the additive branch while the trace still
  covers every matching rule. Regression test added.

### Validated (live)
- Post-fix sweep converged cleanly: 9,023 traces, 6,651 assignments, 0
  sweep failures, segments rebuilt (field_ops 320 / engineering_leads 30).
- HTTP explain with a stored trace (facts_snapshot + policy_snapshot,
  immutable) returned the audit answer end-to-end.
- 8/8 packages green (incl. new additive regression); gofmt/vet/markdown
  clean.

## [Unreleased] — 2026-08-28 (Session 9)

### Added
- **The scripted demo narrative (`cmd/demo`, `make demo`) — Phase 5, the
  graded artifact.** Seven live steps against the seeded 1,000-employee
  company, each annotated with its API endpoint:
  1. Onboard Priya (CA Engineering manager, 2.5-yr tenure) → 10 auto-applied,
     1 needs-decision (Jordan vs Dana, ranked), 1 manual;
  2. Save gate — `/rules/preview` of the future-dated NY rule effective
     today: 319 would gain, 319 would switch (nothing written);
  3. Priya relocates CA→NY → exact gain/lose diff (switches vs true loss);
  4. Tenure crossing at hire+730 → the CA Enhanced rule shadows the US
     default, both traces shown;
  5. Scratch rule wins → **deletion resurrects the previous winner**
     (shadowed matches persist);
  6. `explain` — the STORED trace with facts/policy snapshots + per-rule
     why_lost (immutable);
  7. Backdated correction (valid_from in the past) → history replays,
     the earlier trace does not move.
- **`repo.DeleteRule`** (rule + versions in one tx) — powers the
  resurrection step; scratch-rule cleanup at demo start (idempotent runs).
- **`InsertPolicy` made idempotent** (`ON CONFLICT (id) DO NOTHING`) so
  seeds re-run safely; seeded `pol_vac_exec` (the step-5 scratch subject).
- `make demo` target (seed-if-empty → run → `tee demo/output.txt`).
  Captured output committed under `demo/output.txt`.

### Fixed
- Demo run-state bug (live-run catch): re-creating Priya each run leaked
  her old facts (no FK cascade by decision), so historical facts from the
  previous run won the as-of query. The demo now wipes all her rows
  (facts, projection, traces, shadowed, memberships, employee).
- Step 5 subject: no 5yr+ CA employee exists in the 1-4yr seed
  distribution — redesigned around a manager + `is_manager` scratch rule
  (any manager works; the flip is policy-visible).

### Validated
- `make demo` end-to-end exit 0 with real data; 7/7 packages green;
  gofmt/vet clean; markdown validator clean.

## [Unreleased] — 2026-08-28 (Session 8)

### Added
- **Segments (Supergroups) close the last requirement gap** — "group's
  membership changes" from the problem statement is now first-class:
  - `segment` + `segment_membership` tables (migration 0004); membership is
    DERIVED from stored predicates, never hand-edited.
  - `contains` clause operator in the resolver (list-valued facts — the
    derived `segments` attribute), with tests incl. miss-reason coverage.
  - Reconciler `segment_changed` handler: recompute membership → diff
    affected employees → reconcile exactly those (enter/leave).
  - Sweeper now also re-derives segment membership as a drift backstop
    (segments converged after a missed event; caught by live run).
  - Seeded segments: `field_ops` (NY), `engineering_leads`; rule
    `r_train_field_ops` assigns security training via `segments contains`.
- **Three missing problem-statement categories** (migration 0003):
  `work_schedule`, `shift_policy`, `holiday_calendar` with policies + rules
  (incl. the "Hourly US W-2 shift tracking" example; employment_type gains
  `hourly`; seed distribution now 60/20/20 full/contract/hourly).
- **Manager seed fixed (was assigning a training policy)**: manager category
  now resolves REPORTING STRUCTURE — Engineering managers → Jordan Lee,
  CA managers → Dana Wu; a CA *Engineering* manager surfaces the
  explicit_user_choice decision (two options, UX_FLOWS §3).
- testdb statement splitter rewritten as a single-pass state machine
  (single-quotes, dollar-quoted PL/pgSQL bodies, inline comments,
  BEGIN/COMMIT dropped); migrations now applied by glob — new files flow.

### Validated (live, seeded 1,000-employee company)
- Hourly WA engineer: shift_policy → US Hourly Overtime; work_schedule →
  Night Shift (dept); holiday → US Federal; app_access additive. NY
  employee: Security Training via field_ops segment. True CA employee:
  California Holiday Calendar (specificity 3 > 2). Segments rebuilt:
  field_ops 319, engineering_leads 29. All 7 packages green.

### Changed
- sqlc.yaml schema list includes 0003/0004.

## [Unreleased] — 2026-08-28 (Session 7)

### Added
- **Reconciler worker (`internal/reconciler`, `cmd/worker`)** — Phase 4:
  - Outbox batch claiming (`FOR UPDATE SKIP LOCKED`) with retry + dead-letter
    at MaxAttempts; LISTEN/NOTIFY bridge on a dedicated connection (session
    affinity per SCALE_NOTES) + poll safety net; drains on startup/NOTIFY/tick.
  - Fact-change fan-in (one employee, all categories) and rule-change fan-out
    (whole category) reconciliation; every decision materializes the
    projection AND persists a trace via one shared helper (invariant #6).
  - **Sweeper** — expected-vs-actual drift backstop over the entire population:
    resolves truth per employee, compares to the projection, repairs drift
    (from truth, same auditable helper), returns counts + drift rows.
  - **Scheduler** — future-dated transitions that became effective today
    (facts + rule versions starting today) emit reconciliation events with
    DATED idempotency keys; same-day re-runs are no-ops.
  - Migration 0002: `notify_new_outbox()` trigger + trailer; sqlc schema now
    reads both migrations; projection/scheduler queries added.
- **Outbox insert made idempotent** — `ON CONFLICT (idempotency_key) DO
  NOTHING`; `EmitEvent` returns rows-affected so duplicates are no-ops, not
  errors (correct contract for API retries and dated scheduler keys).
- **Config**: `RECONCILER_POLL_INTERVAL`, `SCHEDULER_INTERVAL` (validated,
  defaulted, documented in .env.example); reconciler config accessors.
- **Tests (live Postgres)**: outbox→projection end-to-end, rule-change
  recompute, drift injection→sweeper repair, scheduler idempotency.

### Fixed
- **Sweeper didn't persist traces (live smoke-test catch)** — sweep repairs
  made decisions invisible to explain. Extracted
  `materializeAndTrace` shared by reconciler + sweeper: every decision now
  leaves an auditable trace, event-driven or swept.
- Reconcile test bug: relocation test emitted the event but never changed the
  fact; now the relocation is committed before the event fires.
- Worker boot: config fields the binary needed were missing; added and
  validated.

### Validated
- **Full-stack live run against the seeded 1,000-employee company**: worker
  started, LISTEN established, startup sweep materialized the projection
  (5,411 assignments) and persisted 6,000 decision traces (1,000 employees ×
  6 categories), queued outbox events processed to empty, drift repaired
  from truth. Kill-recovery verified (FOR UPDATE SKIP LOCKED).
- All 7 packages green (`ok` on api, config, logging, reconciler, repo,
  utils, resolver); gofmt/vet clean; markdown validator clean.

### Changed
- `.env.example` + `.env` gain `RECONCILER_POLL_INTERVAL` and
  `SCHEDULER_INTERVAL`.

## [Unreleased] — 2026-08-28 (Session 6)

### Added
- **Repo layer (`internal/repo`)** — the single sqlc↔resolver conversion boundary:
  - `FactsAt` (bitemporal as-of snapshot), `AddFact` (append + interval closure in
    one tx), `AddEmployee`, `EffectiveRules` (predicate JSON round-trip),
    `CreateRule` (rule + v1 version in one tx), `Category`/`ListCategories`,
    `ResolveForEmployee` (full read-path orchestration with strict registry
    validation), `PersistTrace`/`LatestTrace`, outbox `EmitEvent`/`Claim`
    lifecycle, attribute-registry loading.
  - 7 live-Postgres integration tests with a clean SKIP pattern (tests never
    fail because infra is off).
- **Shared test harness (`internal/testdb`)** — per-package isolated databases
  (`pas_repo_test`, `pas_api_test`), hermetic migration re-application, inline
  comment-aware statement splitter, reachability-probe skips.
- **Seed script (`cmd/seed`)** — deterministic (fixed rand seed): 12 policies,
  13 rules spanning every engine capability (specificity conflict, manual-able
  exclusive, additive stacking, tenure gates, future-dated), 1,000 employees
  with chained bitemporal facts in ~1.4s. Idempotent via employee count.
- **HTTP API (`internal/api`, chi)** — implements docs/API.md: healthz/readyz,
  POST /employees (with live-derived readiness checklist), POST /employees/{id}/
  facts (with before/after gain-lose diff + transactional outbox emit),
  GET /assignments (all categories, as-of), GET /explain (STORED trace only —
  404 when absent, invariant #6), POST /rules (registry-validated, server-computed
  specificity), POST /rules/preview (population-wide dry-run diff, no writes).
  Request-ID + structured access logging middleware.
- **API integration tests** — readiness, diff-on-relocation, rule create +
  assignment flow, explain 404-vs-materialized honesty, preview no-write,
  invalid-predicate rejection via the attribute registry.

### Fixed
- **Supersede wiped new facts (live test catch)** — `SupersedeFactEvents`
  inside the same tx matched the row it was linking from, erasing both facts.
  Replaced by the correct Q2 semantics: `CloseOpenFactRange` ends the previous
  open interval at the new fact's start (exclusive); `superseded_by` is reserved
  for value corrections. Resolver-level behavior unchanged; storage now matches
  the documented `[start, end)` no-gap/no-overlap model.
- **Missing hire_date broke tenure derivation via the API** — create-employee
  now always records `hire_date` (anchor for derived attributes), matching seed.
- **Transitive test-harness bugs** — statement splitter mangled inline comments;
  BEGIN/COMMIT handling left transactions open. Extracted into testdb.
- **Manager category seed semantics reconciled** — `priority_rank` →
  `explicit_user_choice` in the migration seed, matching UX flows (closes the
  Session 5 follow-up flag).
- `.env`/`.env.example` DATABASE_URL port corrected to the dev cluster port.

### Validated
- All packages: vet clean, gofmt clean, `go test ./... -count=1` green.
- Coverage: resolver 90.3%, utils 100%, config 87%, logging 86.2%,
  repo 69.2% (live-DB paths), api 66.9%.
- Live end-to-end: seed → API create employee → readiness → fact relocation
  diff → rule create → assignments, all against real Postgres 16.15.
- sqlc regenerated after query changes (CloseOpenFactRange).

### Changed
- `go.mod` + chi v5.3.2 dependency.

## [Unreleased] — 2026-08-28 (Session 5)

### Added
- **Pure Go resolver (`resolver/`)** — the heart of the system, zero dependencies:
  - `types.go` — CategoryConfig, RuleVersion, Result, outcomes (mirror schema enums)
  - `ast.go` — canonical rule AST: parse, strict validation against attribute
    definitions (op legality, value types, enum membership), predicate evaluation
  - `value.go` — normalized comparables (number/string/bool); evaluation is total:
    unknown shapes surface as deterministic why_not reasons, never panics
  - `facts.go` — Facts, AttributeDefinition registry, derived attributes
    (tenure_days from hire_date, clamped ≥0, explicit-wins semantics)
  - `specificity.go` — automatic specificity from the AST (eq=3, in/range=2,
    exclusion=1); pure function, never admin-declared
  - `conflicts.go` — deterministic total order (manual > priority > specificity >
    created_at > id), SortMatched, lossReason explaining the first lost dimension
  - `trace.go` — decision traces: per-rule evaluations, rank positions, fact &
    policy snapshots (deep-copied), short answer layer
  - `resolve.go` — Resolve(): filter → order → select (winner+shadow / additive /
    needs-decision) → trace; strict strategy/cardinality validation
- **Tests with 88.9% statement coverage** — industry-level, table-driven:
  - determinism: same input → byte-identical JSON output (100-iteration loops)
  - permutation invariance: 200 seeded rule-shuffles → identical resolution
  - antisymmetry + transitivity sweep of the full tie-break matrix
  - shadowing semantics (winner deletion resurrects losers — enabled by
    persisted shadowed matches)
  - snapshot immutability against caller mutation after Resolve
  - tenure-crossing integration (730-day gate: day-of matches, day-before doesn't)
  - error taxonomy: invalid dates, config mismatches, bad predicates (with rule IDs)
- **zerolog logging system (`internal/logging`)** — slog decision reversed;
  component loggers (api/reconciler/scheduler/sweeper), request-ID propagation,
  JSON default + console dev format, env-driven levels. `Build()` is pure for
  tests; `SetupFromEnv()` applies process globals. Resolver stays log-free.
- **Env config system (`internal/utils`, `internal/config`)** — typed getters
  (Get{String,Int,Duration,Bool}, Require) with fail-fast named errors on
  invalid values; godotenv .env loading (host env wins); config validation
  (APP_ENV, port range, prod-requires-DATABASE_URL, level/format whitelists).
- **`.env.example` + `.env`** — documented template; `.env` gitignored.
- **Makefile** — build/test/cover/vet/fmt/tidy/sqlc/migrate/migrate-down.
- **sqlc data layer** — `sqlc.yaml` (schema = the migration file, pgx/v5 output,
  JSONB→[]byte overrides at the repo boundary) + `db/queries/`: facts.sql
  (bitemporal as-of, post-correction visibility, supersession), rules.sql
  (effective-dated version selection via DISTINCT ON), traces.sql (write-once
  audit + retention count), outbox.sql (FOR UPDATE SKIP LOCKED claiming,
  dead-lettering), index.sql (Supergroup inverted index upsert/lookup).
  Generated typed Go in `gen/db`.

### Validated
- All packages: `go vet` clean, `go test ./... -count=1` green, 88.9% coverage.
- **sqlc queries validated live** on Postgres 16.15 with seeded fixtures:
  as-of fact views, effective rule selection (correct empty-before-validity,
  single-row DISTINCT ON after), outbox claim pattern (attempts increment,
  unprocessed count), migration up/down cycle.
- sqlc config path learning recorded: config lives at repo root (paths resolve
  relative to it); JSONB overrides need struct-form `{ type: "[]byte" }`.

### Changed
- Go toolchain: GOTOOLCHAIN=auto downloads go1.27.0 as pinned in go.mod.
- Logging choice reversed: slog → zerolog (TECH_STACK.md updated).

### Known follow-ups (flagged, not hidden)
- Manager category currently seeds `priority_rank`; UX flows show manager
  conflicts surfacing as `explicit_user_choice` decisions. Reconcile when the
  Phase-1 seed script lands.

## [Unreleased] — 2026-08-28 (Session 4)

### Fixed
- **Mermaid rendering** — 16 node labels in `docs/ARCHITECTURE.md` used literal `\n` inside
  quoted labels; mermaid renders those as literal text. Replaced with `<br/>` across both
  diagrams (system overview + bitemporal timeline).
- **Stale cross-references** — `docs/UX_FLOWS.md` and `docs/SCALE_NOTES.md` referenced
  siblings as `docs/…` after the reorganization; fixed to same-directory references.
- **Overstated integrity claims** — DATA_MODEL and TECH_STACK claimed exclusion constraints
  prevent double-booking unconditionally; corrected to the actual per-deployment pattern
  (partial GiST exclusion index per `single` category, with example SQL) since
  single-ness is data, not schema. Migration's plan checkbox updated to match.
- **Doc map gaps** — AGENTS.md was missing `docs/SCALE_NOTES.md` and `CHANGELOG.md` rows;
  PROTOTYPE_PLAN Phase 0/1 checklists updated to reflect what actually exists.

### Validated
- Markdown structural validation across all 12 files — clean.
- **Schema validated with the real Postgres parser (pglast/libpg_query)**:
  `0001_init.up.sql` — 26 statements, valid; `0001_init.down.sql` — 15 statements, valid.
- 13 tables match DATA_MODEL exactly; 6 demo categories seeded.
- **LIVE RUNTIME VALIDATION on Postgres 16.15** (local temp cluster via Homebrew):
  - `0001_init.up.sql` applies cleanly (13 tables, seed rows inserted); full
    down→up migration cycle verified.
  - Bitemporal as-of query (documented in DATA_MODEL) verified with real data:
    correct pre/post-correction views (`US-NY` before relocation, `US-CA` after).
  - Exclusion-constraint pattern verified live: `btree_gist` extension required for
    text equality in GiST; conflicting same-employee/same-category/overlapping-range
    insert correctly **rejected**; non-overlapping insert accepted. First test attempt
    exposed the missing `btree_gist` requirement in the docs — fixed in DATA_MODEL.
    (An intermediate "conflict not blocked" result was a flawed test — cleanup had
    removed the competing row — re-verified correctly.)

### Changed
- **Removed all foreign-key constraints from the schema** (user decision). Referential
  integrity is enforced at the application/repository layer instead; the FK graph remains
  documented in DATA_MODEL's erDiagram as expected invariants. Rationale: assignments are a
  rebuildable projection, and hard FKs add friction to rebuilds/seed loads. DATA_MODEL
  snippets synced; principle 3 added to the governing-principles list.

## [Unreleased] — 2026-08-28 (Session 3)

### Added
- **`docs/SCALE_NOTES.md`** — distilled a 31-section Go-at-scale research brief into only the
  patterns that touch THIS system: event-sourcing/CQRS mapping, CDC-vs-outbox decision rule,
  pgx pool sizing, reconciler bulkheads/load-shedding, Go 1.27 audit items, multi-tenancy,
  capacity math (Little's Law). Triage rationale: ~30% applicable, rest documented as
  deliberate non-adoptions.
- **`db/migrations/0001_init.up.sql`** — full initial schema: `policy_category` (semantics as
  data), `attribute_definition`, `employee`, `fact_event` (bitemporal + supersession),
  `policy`/`policy_version` (copy-on-write), `assignment_rule`/`rule_version` (effective-dated,
  stored tiebreak columns), `assignment` (rebuildable projection), `shadowed_match`,
  `decision_trace` (immutable audit), `outbox` (idempotency + DLQ + event schema version),
  `attribute_index` (Supergroup inverted index). Includes seed rows: 6 demo categories + 7
  attribute definitions.
- **`db/migrations/0001_init.down.sql`** — matching teardown in dependency order.
- **`CHANGELOG.md`** — this file.

### Fixed
- **`docs/DATA_MODEL.md` erDiagram** — removed nonexistent entities
  (`assignment_category`, `manual_override_rule`); linked `assignment_rule → outbox` instead;
  added note that manual assignments are `assignment_rule` rows with `source = 'manual'`,
  not a separate table.
- Cross-document links updated after the `docs/` reorganization (README doc map,
  TECH_STACK repo layout).

### Validated
- Markdown structural validation across all 10 files (code-fence balance, relative links,
  mermaid label balance, table pipe consistency) — clean after fixes.
- Cross-document entity audit: erDiagram entities now match `CREATE TABLE` names exactly.

## [Unreleased] — 2026-08-28 (Session 2)

### Added
- **Go + sqlc stack decision** — replaced Python/FastAPI draft: Go 1.27, sqlc + pgx/v5,
  chi, golang-migrate, testify + rapid, Postgres LISTEN/NOTIFY + outbox.
- **`TECH_STACK.md`** — full rewrite: summary table, Postgres as load-bearing decision
  (range types, exclusion constraints, outbox), Go+sqlc rationale, rejected alternatives
  (MongoDB, Datomic, OPA/Cedar, Temporal), "did NOT add" table with add-later triggers,
  repo layout, production wishlist.
- **`docs/ARCHITECTURE.md`** — system overview diagram, bitemporal event store,
  pure resolver pipeline (filter → order → select → trace), automatic specificity,
  read path (pull for decisions), write path (push for actions, sequence diagram),
  consistency contract, scale path (1k/10k/100k), failure-mode table.
- **`docs/DATA_MODEL.md`** — full schema documentation with erDiagram, rule AST JSON,
  as-of query example, integrity guarantees.
- **`README.md`** — project overview, core concepts, cardinality semantics, resolution
  as pure function, reconciliation model, status checklist.

### Changed
- Go version pinned to **1.27** per user instruction.

## [Unreleased] — 2026-08-28 (Session 1)

### Added
- **`DECISIONS.md`** — all 16 scoping questions answered via a 3-advisor senior-engineer
  council (oracle, reviewer, researcher) backed by web research (Rippling, SAP SuccessFactors,
  Payroll Engine, NetIQ, MidPoint, NIST NGAC, OPA). Key decisions: bitemporal pure-function
  resolution; end-exclusive intervals; manual > priority > auto-specificity > created_at/id
  tiebreak; stackable-vs-exclusive as type-level property; shadowed matches;
  pull-for-decisions/push-for-actions; form-driven builder compiling to canonical AST;
  live-derived readiness checklist; full decision traces stored at decision time;
  40/30/20/10 submission weighting.
