# Prototype Plan

Build order for the reference implementation. Guiding rule from the council verdict
(`DECISIONS.md` Q16): **40% code / 30% doc / 20% UX / 10% tradeoffs**, with the
decision-trace demo as the single highest-signal artifact.

## Phase 0 — Scaffold ✅ docs

- [x] `README.md`, `DECISIONS.md`, `TECH_STACK.md`, `AGENTS.md`, `CHANGELOG.md`
- [x] `docs/`: `ARCHITECTURE.md`, `DATA_MODEL.md`, `API.md`, `UX_FLOWS.md`,
      `SCALE_NOTES.md`, `TRADEOFFS.md`, `PROTOTYPE_PLAN.md`
- [ ] `git init`, module scaffold (`go.mod`), `db/sqlc.yaml`, Makefile

## Phase 1 — Schema + seed (db/)

- [x] Migration `0001_init.up.sql` + `0001_init.down.sql`: `policy_category`,
      `policy`, `policy_version`, `employee`, `fact_event`, `attribute_definition`,
      `assignment_rule`, `rule_version`, `assignment`, `decision_trace`,
      `shadowed_match`, `outbox`, `attribute_index` (GiST exclusion constraints
      pending — see validation notes below), demo-category seed rows
- [ ] sqlc queries: `facts.sql` (as-of), `rules.sql` (effective versions), `traces.sql`,
      `outbox.sql`, `index.sql`
- [ ] Seed script: 1 company, ~1,000 employees, 5 categories, ~15 rules — including the
      canonical demo cases:
      - CA 2yr+ tenure vacation rule (specificity conflict)
      - Manual override on one pay schedule (priority conflict)
      - Contractor shift-policy rule (attribute conjuncts)
      - Engineering app-access rules (additive/many)
      - A future-dated rule ("effective Jan 1")

## Phase 2 — Pure resolver (resolver/)

- [ ] `ast.go` — rule AST types + JSON (un)marshaling + validation
- [ ] Predicate evaluation against `Facts` (typed attribute map; derived `tenure_days`)
- [ ] `specificity.go` — automatic specificity ranking from AST structure (cached per version)
- [ ] `conflicts.go` — total order: manual > priority > specificity > (created_at, id);
      shadowing for `single`, additive for `many`
- [ ] `trace.go` — decision trace construction (per-rule matched/why-lost + snapshots)
- [ ] Tests:
      - `determinism_test.go` — **rapid**: same input → byte-identical output
      - `cardinality_test.go` — exactly-one-or-zero for `single`; union for `many`
      - `shadow_test.go` — winner deletion resurrects loser
      - table tests for the seed demo cases

## Phase 3 — Repository + API (internal/)

- [ ] `repo/` — sqlc rows ↔ resolver types; the only conversion boundary
- [ ] `api/` (chi):
      - `POST /employees` · `POST /employees/{id}/facts` (with valid-time support)
      - `POST /rules` · `PUT /rules/{id}/versions` (effective-dated versions)
      - `GET /employees/{id}/assignments?as_of=DATE`
      - `GET /employees/{id}/explain?category=X&as_of=DATE` → stored trace
      - `POST /rules/preview` (dry-run diff: who gains/loses)
      - `POST /employees/preview` (hypothetical hire → readiness checklist)
- [ ] Outbox writer in the same tx as every fact/rule change
- [ ] `docker-compose.yml` (Postgres + app)

## Phase 4 — Reconciliation + scheduler (internal/)

- [ ] LISTEN/NOTIFY bridge → reconciler claims outbox (`FOR UPDATE SKIP LOCKED`)
- [ ] Inverted-index affected-set computation (entering/leaving diff)
- [ ] Materialized projection update + shadowed-match promotion on rule deletion
- [ ] Scheduler worker: future-dated fact/rule transitions fire on their effective date
- [ ] Sweeper: expected-vs-actual periodic reconciliation (drift backstop)
- [ ] Tests: `reconciliation_test.go` — attribute change → correct diff; event loss →
      sweeper converges

## Phase 5 — The demo (the graded artifact)

- [ ] `make demo`: seeds → runs a scripted narrative:
      1. Onboard Priya → show readiness checklist output
      2. Save the CA tenure rule → show the 17-employee diff
      3. Change Priya's location CA→NY → show gain/lose diff + projection update
      4. Cross the tenure gate → show shadowing flip with both traces
      5. Delete the winning rule → loser resurrects
      6. **`explain`**: "why does Priya have X as of March 3?" → human-readable trace
      7. Backdated correction → replay shows corrected history, original trace intact
- [ ] `demo/` script + captured output committed to the repo
- [ ] README quickstart: `docker compose up && make demo`

## Phase 6 — Submission polish

- [ ] Final pass over all docs; cross-link every claim to code or trace output
- [ ] `AGENTS.md` accuracy check (commands actually work)
- [ ] Email-ready summary: doc map + demo highlights + tradeoffs pointer
