# Email — ready to send

**To:** eng@warp.co
**Subject:** Policy Assignment System — Submission

---

Hi Warp team,

I've designed and built a Policy Assignment System as requested, and it's
ready to evaluate: **https://github.com/ramanasai/wrap-policy-assignment-system**

**What it is.** A generic substrate for the "rules over employee context →
assignments" pattern that runs across time off, pay schedules, app access,
compliance trainings, benefits, work schedules, shift policies, holiday
calendars, and manager reporting. Employee facts and rules are immutable,
effective-dated (bitemporal) events; resolution is a pure, deterministic
function with a fully traced tie-break (manual > priority > specificity >
creation order); losing matches are shadowed, not discarded, so deleting a
rule resurrects the default. Changes flow through a transactional outbox
into a reconciler worker with a sweeper drift backstop.

**How to run it (one command):**
```bash
docker compose up --build            # Postgres + API (:8080) + worker
docker compose run --rm demo         # the scripted narrative, all live data
```
A captured demo run is committed at `demo/output.txt`, and `SUBMISSION.md`
maps every evaluation criterion to where it's satisfied and evidenced.

**Highlights**
- Determinism & explainability property-tested (byte-identical outputs
  across 200 input permutations; every tie-break loss named in the trace)
- Auditable: "why does X have Y as of Z?" answered from immutable decision
  traces written at decision time (9,000+ in the demo company)
- Reconciliation proven live: relocation diffs, tenure crossings, segment
  membership changes, and a sweeper heals injected drift
- Honest tradeoffs documented (docs/TRADEOFFS.md), Go 1.27 + sqlc + Postgres

The doc set (`docs/`), schema, code, and demo all cross-check each other;
bugs found by live runs are recorded in CHANGELOG.md.

Happy to walk through any part of it.

Best regards,
sairamana

---
*Repository history note: this was developed as a private repo and made
public solely for this submission. No credentials are checked in; `.env` is
gitignored.*