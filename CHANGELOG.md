# Changelog

All notable changes to this project are documented here. Format based on
[Keep a Changelog](https://keepachangelog.com/); entries are grouped by session/date,
newest first.

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
