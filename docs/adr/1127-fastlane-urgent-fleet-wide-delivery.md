# ADR 1127 — Fastlane: one command from merged commit to every goatnet

**Status:** Accepted (2026-08-13, ops) · **Design:** [fastlane.md](../design/fastlane.md) · **Track:** `fastlane`

**Amended 2026-09-09:** the script now performs steps 14 and 15 (deploy every goatnet, verify each host's deploy ledger) through `deploy-component.sh`, and lands its own PR through `pr-land.sh`; for its first month it printed those steps instead.

**Amended 2026-11-02:** decision 6 no longer ships a whole set. A graduation-set member is refused, because fastlane moves one component and the `--set` flag only claimed otherwise; the flag is removed from both avenues. Its emergency route is `graduate.py hotfix`, or graduating the set. The irreversibility refusal now reads the declared `rollback:` in `fleet-pins.yaml`: `id-admin-console`, `portal`, `netbird-mgmt`, `spire`, `grant-keyd` and `kratos-selfservice-ui` declare a rollback that is not a pin revert, so fastlane refuses them.

## Context

Getting a change onto a goatnet costs fifteen steps and two full CI cycles,
whatever the change is. Measured on 2026-08-13, shipping **one line of HTML** —
a missing `<script>` tag (#2511) — required:

| | |
|---|---|
| 1–2 | PR, 42 CI checks, merge |
| 3–4 | wait for the multi-arch image, resolve its list digest, verify it under both GHCR orgs |
| 5–8 | edit `fleet-pins.yaml`, run `generate-pins.py`, re-sign the zagros intent, re-sign the zagros-prod intent |
| 9–11 | `graduate.py open`, abandon the superseded record with a note, `graduate.py check` |
| 12–13 | a **second** PR, 42 more checks, merge |
| 14–15 | run the deploy playbook, verify live |

Three properties make this worse than the step count suggests.

**The ceremony is flat.** A one-line HTML fix and a schema migration pay exactly
the same fifteen steps. Cost is uncorrelated with risk, so the tax lands hardest
on the small, obvious, low-risk fixes — which is where it most distorts
behaviour, because the rational response to a 40-minute pipeline is to batch
unrelated changes together, which is itself a risk.

**The pin PR races main.** The pin bump regenerates every goatnet's
`intent.json` (they embed the fleet-pins source hash), so *any* other session
merging first conflicts it. Resolving means re-merge, re-generate, re-sign, and
a **fresh 42-check cycle** — which can lose the race again. This happened during
the very session that motivated this ADR.

**Some of the gating was measurably pointless.** F-327: two multi-arch image
workflows triggered on `ops/consistency/intent/**`, a generated path that
`.dockerignore` excludes from their build context outright. ~140 needless builds
a month, each one gating the pin PR that caused it.

There is already an expedited path — `graduate.py hotfix` — but it is
*advisory*: it writes a record and then **prints** the fourteen remaining steps
for a human to perform. It reduces the thinking, not the work.

## Decision

1. **Fastlane is an actuating path, not another advisory one.**
   `ops/scripts/fastlane.sh <component> --ref <tag|digest> --reason … --ack …`
   performs steps 3–15: digest resolution, pin edit, regeneration, both
   re-signings, the record, the PR, and the deploy to **every** goatnet. The
   operator supplies intent; the tool supplies mechanics.

2. **It moves `fleet:`, not a deviation.** Fastlane's semantics are "this goes
   everywhere, now", so the value of record moves and per-goatnet deviations for
   that component are cleared. A fastlane leaves the fleet *converged*, not
   skewed — the opposite of a ladder candidate.

3. **It reuses the `hotfix` record class rather than inventing one.** A fastlane
   is an expedited change that owes a backfill, which is exactly what `hotfix`
   already means. `graduate.py status` therefore renders fastlane debt as
   `hotfix-backfill-owed` with **no change to graduate.py**, and `check` already
   refuses to let it be forgotten.

4. **Fastlane defers ceremony; it never deletes correctness.** The floor, kept
   in full:

   | Kept | Why |
   |---|---|
   | Image built from a merged commit by CI | never a local build to a live goatnet |
   | Digest-pinned, multi-arch index, verified under both GHCR orgs | F-245 |
   | Deploy-history ledger written on every host | `rollback.sh` reads it; F-323 is drift a ledger cannot see |
   | Rollback ref recorded in the pin comment | rollback must not require archaeology |
   | ADR 1084 §D3 single-homed admin guard | prevents a second admin surface |
   | Graduation-set atomicity (ADR 1125) | "these move together" is a constraint, not ceremony |

   What it skips: the ladder ordering, the soak window, the second PR, and
   hand-authored record keeping.

5. **The debt is written as data, by the tool.** Every fastlane emits a record
   naming what was skipped, the previous ref, and the operator ack. Nothing
   relies on anyone remembering. The evidence for this rule is in the tree: of
   17 abandoned release records, **8 carry no machine-readable reason at all**
   because the convention asked humans to supply one.

6. **A graduation-set member fastlanes the whole set.** Fastlane calls
   `graduate.py`'s existing `open_set_for()` — no second membership lookup. A
   partial failure reports as failed, naming which members landed, because a
   half-shipped set is precisely what the set exists to prevent.

7. **Fastlane still goes through a PR, auto-merged.** It does not push to main.
   The PR is opened and set to auto-merge, so required checks still run and the
   human wait disappears; combined with the F-327 trigger fix, that is where most
   of the latency actually was. Branch protection stays the backstop.

## Consequences

- A fastlane change reaches every goatnet with no soak. That is the point, and
  it is why `--ack` is mandatory and recorded.
- Prod can receive a change that preprod has never run for more than minutes.
  The ladder still exists and is still the default; fastlane is the exception
  the operator invokes deliberately, and it is visible as `hotfix-backfill-owed`
  until someone graduates the ref normally.
- Fastlane is **not** for schema migrations, identity/ACL changes, or anything
  whose rollback is not a pin revert. The script refuses components whose
  rollback is flagged irreversible in the ledger.
- The backfill is real and owed. Fastlane makes the debt loud, not absent.

## Alternatives considered

**Make the ladder faster instead.** Worth doing regardless (F-327 is exactly
that) but it does not address the flat-cost property: the ladder's soak gate is
a *time* gate, and no amount of CI tuning makes a 1-day soak fast.

**Let fastlane push straight to main.** Removes the last wait, and was rejected:
branch protection is the one mechanism that survives a bug in fastlane itself.

**A new record class `fastlane`.** Rejected — it would need `graduate.py`
grading, status rendering, and CI-gate changes to say what `hotfix` already
says. A marker field inside a `hotfix` record carries the distinction at zero
cost to the tooling.
