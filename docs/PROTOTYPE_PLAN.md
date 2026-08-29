# Prototype Plan

Build order for the reference implementation. Guiding rule from the council verdict
(`DECISIONS.md` Q16): **40% code / 30% doc / 20% UX / 10% tradeoffs**, with the
decision-trace demo as the single highest-signal artifact.

## Phase 0 — Scaffold ✅ docs

- [x] `README.md`, `DECISIONS.md`, `TECH_STACK.md`, `AGENTS.md`, `CHANGELOG.md`, `.env.example`
- [x] `docs/`: `ARCHITECTURE.md`, `DATA_MODEL.md`, `API.md`, `UX_FLOWS.md`,
      `SCALE_NOTES.md`, `TRADEOFFS.md`, `PROTOTYPE_PLAN.md`
- [x] `git init`, module scaffold (`go.mod`), `sqlc.yaml`, Makefile, `.env.example`

## Phase 1 — Schema + seed (db/)

- [x] Migration `0001_init.up.sql` + `0001_init.down.sql`: `policy_category`,
      `policy`, `policy_version`, `employee`, `fact_event`, `attribute_definition`,
      `assignment_rule`, `rule_version`, `assignment`, `decision_trace`,
      `shadowed_match`, `outbox`, `attribute_index` (GiST exclusion constraints
      pending — see validation notes below), demo-category seed rows
- [x] sqlc queries: `facts.sql` (as-of), `rules.sql` (effective versions), `traces.sql`,
      `outbox.sql`, `index.sql`, `categories.sql`, `employees.sql` → `gen/db`
- [x] Seed script (`cmd/seed`): 1 company, 1,000 employees, 6 categories, 13 rules — including the
      canonical demo cases:
      - CA 2yr+ tenure vacation rule (specificity conflict)
      - Manual override on one pay schedule (priority conflict)
      - Contractor shift-policy rule (attribute conjuncts)
      - Engineering app-access rules (additive/many)
      - A future-dated rule ("effective Jan 1")

## Phase 2 — Pure resolver (resolver/)

- [x] `ast.go` + `value.go` — rule AST types, JSON parse/validate, total evaluation
- [x] Predicate evaluation against `Facts` (typed attribute map; derived `tenure_days`)
- [x] `specificity.go` — automatic specificity ranking from AST structure
- [x] `conflicts.go` — total order: manual > priority > specificity > (created_at, id);
      shadowing for `single`, additive for `many`
- [x] `trace.go` — decision trace construction (per-rule matched/why-lost + snapshots)
- [x] `facts.go` — attribute registry + derived attributes (tenure clamped ≥0, explicit wins)
- [x] `resolve.go` — full pipeline with strict strategy/cardinality validation
- [x] Tests (90.3% resolver coverage): byte-identical determinism, 200-permutation
      invariance, antisymmetry/transitivity sweeps, shadowing, snapshot immutability,
      tenure crossing, error taxonomy (std-lib + seeded-shuffle property tests)

## Phase 3 — Repository + API (internal/)

- [x] `repo/` — sqlc rows ↔ resolver types; the only conversion boundary
      (7 live-Postgres integration tests with SKIP pattern via `internal/testdb`)
- [x] `api/` (chi):
      - `POST /employees` · `POST /employees/{id}/facts` (with valid-time support)
      - `POST /rules` · `PUT /rules/{id}/versions` (effective-dated versions)
      - `GET /employees/{id}/assignments?as_of=DATE`
      - `GET /employees/{id}/explain?category=X&as_of=DATE` → stored trace
      - `POST /rules/preview` (dry-run diff: who gains/loses)
      - `POST /employees/preview` (hypothetical hire → readiness checklist)
- [x] Outbox writer in the same tx as fact changes (EmitEvent with idempotency keys)
- [x] Live-DB API integration tests (readiness, diff, explain honesty, preview, validation)
- [ ] `docker-compose.yml` (Postgres + app)

## Phase 4 — Reconciliation + scheduler (internal/)

- [x] LISTEN/NOTIFY bridge (trigger + dedicated connection) → reconciler claims
      outbox (`FOR UPDATE SKIP LOCKED`)
- [x] Affected-set recompute: per-employee fan-in (fact/employee changes) and
      per-category fan-out (rule changes)
- [x] Materialized projection update + shadowed-match replacement per resolution
- [x] Scheduler worker: future-dated fact/rule transitions fire on their
      effective date with dated idempotency keys (re-runs are no-ops)
- [x] Sweeper: expected-vs-actual drift backstop, chunked, repairs from truth
      via the same materialize+trace helper (decisions stay auditable)
- [x] Tests: `reconciliation_test.go` — outbox→projection, rule-change recompute,
      drift injection→sweep repair, scheduler idempotency (live Postgres)

## Phase 5 — The demo (the graded artifact)

- [x] `make demo`: seeds → runs a scripted narrative:
      1. Onboard Priya → show readiness checklist output
      2. Save the CA tenure rule → show the 17-employee diff
      3. Change Priya's location CA→NY → show gain/lose diff + projection update
      4. Cross the tenure gate → show shadowing flip with both traces
      5. Delete the winning rule → loser resurrects
      6. **`explain`**: "why does Priya have X as of March 3?" → human-readable trace
      7. Backdated correction → replay shows corrected history, original trace intact
- [x] `demo/` script + captured output committed to the repo
- [ ] README quickstart: `docker compose up && make demo`

## Phase 6 — Submission polish

- [ ] Final pass over all docs; cross-link every claim to code or trace output
- [ ] `AGENTS.md` accuracy check (commands actually work)
- [ ] Email-ready summary: doc map + demo highlights + tradeoffs pointer
