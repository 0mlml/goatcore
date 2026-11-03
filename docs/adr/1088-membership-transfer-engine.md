# ADR 1088 — Membership transfer engine: mechanistic canonical↔satellite moves

| Field | Value |
|---|---|
| Status | Accepted; **engine core built in code** (`goat admin members move` — mesh plane + Ory groups-swap/markers, all invariants enforced; live dry-run-verified on ZAGROS 2026-07-17). Consumers (exercise tooling, probation cron, console reactivate) build on it per membership-lifecycle-plan.md |
| Date | 2026-07-17 |
| Related | [membership-lifecycle-and-transfer.md](../design/membership-lifecycle-and-transfer.md), [ADR 1089](1089-probation-isolation-lifecycle.md), [ADR 1087](1087-advanced-grant-catalog.md), [ADR 1031](1031-credential-revocation-mesh-first-offboarding.md), exercise-group-sets |

## Context

Three needs — exercise/activity sets, probation isolation, and unforeseen
reorganizations — all reduce to *moving a set of entities between groups*.
The VENDOR→DEVELOPER and MGMT-HOST→INFRA retirements (2026-07-16) proved the
correct manual pattern (mirror-first, coverage-gated, audited, re-runnable) at
the cost of hand-driven scripts each time. The operator asked for this to be a
mechanistic process usable in bulk, on individuals, or on selected sets.

## Decision

One primitive — **transfer(source, dest, selector, planes)** — with fixed
invariants, exposed as a `goat admin members move` verb and a library the
console + probation cron call:

- **Planes**: the caller declares mesh (NetBird peer group), Ory (identity
  `traits.groups`), or both. Default for reach-only moves (exercise, probation)
  is mesh-only; the identity's canonical Ory role is untouched unless named.
- **Selector**: individual (handle/peer id), explicit list, or filter (e.g.
  `role=DEVELOPER AND last_seen>14d`). Bulk = "the whole source group".
- **Symmetry**: shed and absorb are the same call with source/dest swapped.
- **Fail-closed coverage gate**: refuse any move that would leave a peer with no
  policy-bearing group, unless the destination is itself covered (probation's
  isolation group is covered — ADR 1089). This is the invariant every 2026-07
  cutover enforced, now mechanized.
- **Mirror-first for absorb-into-shared**: when absorbing into a canonical group,
  add to dest, verify, then remove from source — never the reverse.
- **Audited**: one `OperatorActionV1` per move (who/source/dest/count/reason)
  through the `goat admin` chokepoint.
- **Idempotent + resumable**: skip entities already at dest; a killed bulk run
  re-runs cleanly.

Not a daemon: a library + CLI leg + the existing onboard-runner apply path.

## Alternatives

- **Hand-scripted per event** (status quo): proven but unscalable, no audit
  chokepoint, easy to orphan a peer. Rejected.
- **Direct dashboard edits**: no bulk, no coverage gate, no audit. Rejected for
  anything beyond a one-off.
- **A membership daemon reconciling desired-state**: heavier; a desired-state
  controller is a future option but the imperative verb is the minimal thing
  that serves all three consumers now.

## Consequences

A single audited, coverage-safe path for all group churn. Exercise
stamp/absorb/teardown and probation shed/restore become thin callers. The
CLI verb is immediately useful for the reorganizations we can't foresee.
Risk: a bulk move is powerful — mitigated by the coverage gate, the dry-run
default (print the plan + who-moves before applying, like the cutover
`propose` mode), and reversibility.
