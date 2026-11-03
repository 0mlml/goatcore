# fastlane — one command from a merged commit to every goatnet

| | |
|---|---|
| Scope | The deliberate exception to the release ladder: getting a change onto **every** goatnet now, skipping the soak and the hand-authored record keeping, without skipping the things that make a deploy recoverable. |
| Decision | [ADR 1127](../adr/1127-fastlane-urgent-fleet-wide-delivery.md) |
| Related | [release-graduation.md](release-graduation.md) (the ladder this bypasses), [universal-deploy-rollback.md](universal-deploy-rollback.md) (the ledger it must not skip), environment-parity.md |
| Code | `ops/scripts/fastlane.sh`, `ops/release/graduate.py` (`abandon`, C8) |
| Skill | `.claude/skills/fastlane/SKILL.md` — carries the ladder-vs-fastlane DECISION and what each refusal means. It exists because CLAUDE.md is at its CI-enforced 30 KB cap with ~20 bytes free, so the skill list is the discoverability channel a session actually gets. |
| Status | Built and FIRST FIRED LIVE 2026-08-13 — portal to `sha-91ca1db` on zagros + zagros-prod in one command, operator-directed. |

## The problem, measured

Shipping **one line of HTML** on 2026-08-13 — a missing `<script>` tag that left
three nav dropdowns stacked on top of each other — took fifteen steps, three CI
cycles, and several hours of wall clock. Not because anything went wrong; that
is simply what the path costs.

The cost is **flat**: a one-line HTML fix and a schema migration pay the same
fifteen steps. Because it is uncorrelated with risk, the tax falls hardest on
small obvious fixes, and the rational response to a multi-hour pipeline is to
batch unrelated changes — which is itself a risk.

Two structural aggravations showed up in the same session:

- **The pin PR races main.** A pin bump regenerates every goatnet's
  `intent.json` (they embed the fleet-pins source hash), so any other session
  merging first conflicts it. Resolving costs a re-merge, a regeneration, two
  re-signings and a **fresh 42-check cycle** — which can lose the race again.
- **Some gating was measurably pointless.** F-327:
  two multi-arch image workflows triggered on a generated path their own
  `.dockerignore` excludes from the build context. ~140 needless builds a month,
  each gating the pin PR that caused it.

## What fastlane is

```
ops/scripts/fastlane.sh <component> --ref <sha-tag|full-ref> \
    --reason "why this cannot wait for the ladder" --ack "<operator>"
```

It performs the mechanical steps that `graduate.py hotfix` only *prints*:

| | |
|---|---|
| 1 | Resolve the component from `fleet-pins.yaml` — var, ledger name, deploy playbook, goatnet member groups |
| 2 | Refuse if the component is in an **open graduation set** (unless `--set` ships the whole set) |
| 3 | Resolve the tag to a digest, and **verify it is identical under both GHCR orgs** |
| 4 | Move `fleet:` and clear that component's deviations |
| 5 | Regenerate pins + every goatnet intent |
| 6 | Re-sign each intent with its own goatnet's CA key |
| 7 | Write a `hotfix`-class record marking it a fastlane |
| 8 | **Abandon superseded records**, with machine-readable reasons |
| 9 | Re-run the gates, land the pin PR (`pr-land.sh`: commit, checks, squash-merge), then deploy every goatnet the component belongs on and verify each host's deploy ledger names the new ref (`deploy-component.sh`) |

`--dry-run` stops before any mutation. `--no-deploy` stops after the pin lands in main.

## Why `fleet:` and not a deviation

A ladder candidate is a *deviation on preprod* — deliberate, reasoned skew.
Fastlane's semantics are the opposite: this goes everywhere at once. So it moves
the value of record and deletes the component's deviations. **A fastlane leaves
the fleet converged, not skewed.** Anything else would leave phantom deviations
that say "preprod differs" when it does not.

### `absent: true` survives — it is topology, not skew

Fastlane clears a component's deviations because a value-deviation is redundant
once `fleet:` moves. **An `absent: true` deviation is not skew and must survive.**
It says the component does not belong on that member at all: `zagros_b_hosts`
carries one for the portal because that standby runs the zenoh router but *not*
the admin mutation surfaces. Delete it and `effective()` falls through to
`fleet:`, declaring a **second admin surface on the standby** — precisely what the
ADR 1084 §D3 guard exists to prevent.

The first version of fastlane cleared deviations with a single regex and would
have shipped exactly that. A `--dry-run` before the first live use printed
`would clear deviations: zagros, zagros_b_hosts, zagros_prod`, and `zagros_b_hosts` had
no business being in that list. **Always `--dry-run` first** is not politeness;
it is the step that catches this class.

## Why it reuses the `hotfix` record class

A fastlane is an expedited change that owes a backfill — precisely what `hotfix`
already means. `graduate.py status` renders an open hotfix record as
`hotfix-backfill-owed` whether or not the rungs agree (a fastlane makes them
agree; until 2026-09-10 that read `aligned` and hid the debt, F-514), and
`check` C9 fails the tree once such a record is older than three days with no
validation evidence. A new class would have needed grading, status rendering
and CI-gate changes to say what an existing one says.

## The floor it does not skip

Fastlane **defers ceremony; it never deletes correctness.** The distinction is
load-bearing, so it is written down rather than left to judgment:

| Kept | Why it is not ceremony |
|---|---|
| Image built by CI from a merged commit | fastlane never builds; a local build reaching a live goatnet is unreviewable |
| Digest-pinned, multi-arch, identical under both GHCR orgs | the pin is org-agnostic (`ghcr_org` derives from the deploying checkout), so a split digest would deploy different code from the same committed pin |
| Deploy-history ledger written on every host | `rollback.sh` reads it; F-323 is exactly the drift a ledger cannot see |
| Rollback ref in the pin comment | rollback must not require archaeology |
| ADR 1084 §D3 single-homed admin guard | prevents a second admin surface |
| Graduation-set atomicity (ADR 1125) | "these move together" is a constraint, not ceremony |

What it skips: ladder ordering, the soak window, the second PR, and
hand-authored records.

## Debt is written as data

Every fastlane emits a record naming what was skipped, the previous ref, and the
operator ack — and abandons whatever it superseded, with a reason a parser can
read.

This is not belt-and-braces. Of 17 abandoned release records in the tree,
**8 carried nothing but the original `reason:`** (which explains why the
candidate was *opened*, not why it was dropped), and several of the rest
explained themselves in a **YAML comment** — fully legible to a human opening
the file and completely invisible to `yaml.safe_load`. The convention asked
humans to supply the reason, and the convention lost.

So `graduate.py` grew:

- **`abandon <record> --reason … [--superseded-by …]`** — writes
  `abandoned_reason:` and `superseded_by:` as **fields**.
- **Check C8** — a record abandoned on/after `ABANDON_REASON_CUTOFF`
  (2026-08-13) must carry `abandoned_reason:`.

C8 is **scoped by open date on purpose.** Applying it to all 17 would force
someone to reconstruct reasons for 8 historical abandonments, several of them
other sessions', where the honest answer is "unknown" — inventing those is
exactly the failure the gate exists to prevent. Older records are grandfathered.

The gate was fired in **both** directions against the real tree before landing:
a record with no `abandoned_reason` produces one C8 failure; the same record
after `graduate.py abandon` produces zero. A gate that cannot fail is
F-321.

## When not to use it

Fastlane is for changes whose **rollback is a pin revert**. It is the wrong tool
for schema migrations, identity or ACL changes, or anything that leaves state
behind — there, the soak is doing real work and skipping it buys nothing back.

The ladder remains the default. Fastlane is the exception an operator invokes
deliberately, is attributed by `--ack`, and stays visible as
`hotfix-backfill-owed` until the backfill is paid: the live rows the change maps to run on dev and preprod, their PASS goes into `evidence.validation`, and after the soak `graduate.py graduate <component>` closes the hotfix record itself. `check` C9 refuses the tree when the record is older than three days with no validation evidence.

## Caveats

- **First live use is done** (2026-08-13, portal to both goatnets, operator-acked).
  The run itself is the strongest argument for the `--dry-run` rule: it printed
  `would clear deviations: zagros, zagros_b_hosts, zagros_prod`, and clearing that
  `absent: true` marker would have declared a second admin surface on the zagros-b
  standby — the exact thing ADR 1084 §D3 prevents. Fixed before the real run.
  zagros-prod moved `sha-3883f57` → `sha-91ca1db` and its `/api/topics/stream`
  returned a `: heartbeat` for the first time ever.
- **Fastlane deploys, and proves it.** From 2026-09-09 (the first month it only printed the deploy commands, contrary to ADR 1127 steps 14 and 15) it lands the pin PR itself, then runs `deploy-component.sh` per goatnet: the transport is chosen per goatnet (mesh when this laptop is on that profile, the goatnet's door otherwise, direct for zagros-dev), and every host's deploy ledger must name the new digest before the goatnet is called converged. A goatnet that does not converge is named, the pin stays in main, and `deploy-component.sh` or `rollback.sh` is the next move.