# AGENTS.md

Guide for coding agents (and humans) working in this repo.

## What this is

A take-home system design for a **Policy Assignment System** (see `README.md` for the
problem). The doc set is the primary deliverable right now; a Go reference implementation
follows `docs/PROTOTYPE_PLAN.md`.

## Doc map

| File | Contents |
|---|---|
| `README.md` | Problem, core concepts, quickstart (when code exists) |
| `CHANGELOG.md` | All notable changes, newest first — **update on every change** |
| `DECISIONS.md` | 16 scoping questions answered + sourced (do not contradict; update it if a decision changes) |
| `TECH_STACK.md` | Go 1.27, sqlc + pgx, chi, Postgres — choices and rejections |
| `docs/ARCHITECTURE.md` | Components, resolution/reconciliation flows, consistency contract |
| `docs/DATA_MODEL.md` | Full Postgres schema (bitemporal, outbox, inverted index) |
| `docs/API.md` | HTTP contract; rule AST JSON shared by API/form-builder/resolver |
| `docs/UX_FLOWS.md` | Admin flows: builder, save-gate diff, readiness checklist, explain inspector |
| `docs/SCALE_NOTES.md` | Applicable 100k+ patterns: pooling, bulkheads, CDC-vs-outbox, Go 1.27 notes |
| `docs/TRADEOFFS.md` | Pros/cons of every major choice, scope limitations, non-goals |
| `docs/PROTOTYPE_PLAN.md` | Build order (Phases 0–6) with checkboxes |

## Governing invariants (violating these = bug)

1. **Assignments are derived state.** Never mutate `assignment` rows outside the reconciler;
   they are a rebuildable projection.
2. **The resolver is pure.** `resolver/` must have zero non-stdlib imports, no I/O, no
   DB/HTTP types. sqlc rows convert to resolver types only in `internal/repo/`.
3. **Bitemporal, append-only.** Corrections append events (`superseded_by`); nothing
   UPDATEs temporal business data in place.
4. **Determinism.** Same `(facts, rules, category, date)` → byte-identical
   `ResolutionResult`. Tiebreaks must read stored columns (`priority`, `created_at`, `id`),
   never map iteration order or time-of-day.
5. **Cardinality semantics are data** on `policy_category` — no per-policy resolver branches.
6. **Explanations are written at decision time.** Never recompute a trace on read.
7. **No database-level foreign keys** (user decision). Relationships are enforced in the
   repository layer; do not add `REFERENCES` clauses to migrations. The FK graph in
   `docs/DATA_MODEL.md`'s erDiagram documents the expected invariants only.

## Commands (kept accurate — stale commands are worse than no commands)

```bash
make test         # go test ./... (unit + property tests) via Go 1.27 toolchain
make cover        # tests + coverage summary
make vet          # go vet ./...
make sqlc         # regenerate gen/db from db/queries (requires sqlc in PATH)
make migrate      # apply migrations to local dev DB (postgres://localhost:55432/pas)
make migrate-down # tear schema down (dev only)
make build        # compile all packages
make tidy         # go mod tidy
```

Configuration: copy `.env.example` to `.env` (gitignored); `internal/config`
loads typed config with fail-fast validation on invalid values.

## Conventions

- Go: `gofmt`, `go vet`, small interfaces; log through `internal/logging` (zerolog) — never import zerolog directly in feature packages; no panics across package
  boundaries.
- SQL lives in `db/queries/*.sql` (sqlc); no query strings in Go code.
- Docs: keep pros/cons honest in `docs/TRADEOFFS.md`; if you add a limitation in code, add
  its row there in the same change.
- Mermaid diagrams live in the `.md` files they explain, diffable in PRs.

## Current status

Docs complete (Phases 0 partial). Next: scaffold Go module + migrations per
`docs/PROTOTYPE_PLAN.md` Phase 1.
