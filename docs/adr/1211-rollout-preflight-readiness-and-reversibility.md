# ADR 1211 — A rollout's readiness and reversibility are declared, graded and acknowledged before deploy

- **Status:** **Proposed.** On merge nothing changes on a live goatnet: `goatbuilder preflight` reads only, the three facts are declared per component (fail-closed until declared), and `deploy`'s ack gate reuses the `--ack` it already takes. The gate fires for a `none` or `restore-required` rollout once the facts are declared; it never hard-blocks.
- **Date:** 2026-10-30
- **Deciders:** operator
- **Related:** [`goatbuilder-rollout-preflight.md`](../design/goatbuilder-rollout-preflight.md) · [ADR 1060](1060-universal-deploy-rollback-ledger.md) · [ADR 1127](1127-fastlane-urgent-fleet-wide-delivery.md) · [ADR 1120](1120-release-graduation-ladder.md) · [ADR 1125](1125-fleet-reconciliation-join-and-graduation-sets.md) · [ADR 1206](1206-goatnet-declared-end-state.md) · [ADR 1201](1201-two-avenues-scripts-documented-cli-supported.md)

## Context

The deploy ledger (ADR 1060) records an artifact type and a `reversible` flag,
and `rollback` refuses an unsafe undo — but only once a rollout is already live
and the operator reaches for the undo. Nothing states, before the rollout, how
cleanly it could be undone. A stateful cutover (a SQLite→Postgres migration, ADR
1165) and a stateless image bump are deployed by the same command with the same
one-word confirmation, and the record afterward does not show that the operator
knew the difference.

## Decision

`goatbuilder preflight <component> --env <env>` prints three facts before a
rollout, and `deploy` runs it as its first phase:

- **State** `stateless | stateful` — does the rollout create or migrate data.
- **Rollback** `pin-revert | restore-required | none` — flip the pin, restore
  from a named backup (with its retention window), or no return.
- **Scope** `single | coupled` — independent, or a member of a graduation set
  that moves together (ADR 1120/1125).

They are three explicit facts, never one editorial "risk" word, because the
three are orthogonal and a reader acts on each differently.

**Declared, one home, fail-closed.** The facts are declared per component in
`ops/pins/fleet-pins.yaml` beside `artifact_type` (`owns_state`, `rollback`,
`restore_from`, `coupled_with`); coupling also derives from an open graduation
set. An undeclared component reads `stateful · rollback: none (undeclared)` and
grades not-clean — a visible gap, never a guessed green.

**Graded against the ledger, not the hope.** A declaration is trusted only where
it agrees with the recorded artifact type and `reversible` flag. The fastlane
safe-class rule (ADR 1127) is the grading logic: a pin flip is the safe class, a
schema/identity/ACL/data migration is not, so `pin-revert` declared over a `data`
artifact is a lie and grades not-clean.

**Measured, honesty-graded.** Readiness reuses the existing checks (ref pullable,
the deploy-time secrets gate, `pins --check`, fresh source, declared end state
per ADR 1206); validation proves the rollback fact (`rollback --dry-run` for
pin-revert, the retention window for restore-required). What cannot be checked
reads `UNKNOWN`, never a false pass — as `assay` grades.

**Acknowledged, recorded, not blocked.** A `none` or `restore-required` rollout
proceeds on `deploy` only with an explicit `--ack`, and the accepted facts are
written into the deploy ledger row (`rollback_preflight_accept`), so the record
shows what was knowingly accepted and by whom. It does not hard-block — a
stateful cutover stays possible.

Both avenues stay coherent (ADR 1201): `ops/scripts/deploy-component.sh
--preflight` and `ops/scripts/rollout-preflight.py` read the same fields and
print the same facts.

## Alternatives rejected

- **One "risk: low/medium/high" word** — collapses three orthogonal facts an
  operator acts on separately, and invites arguing the label instead of reading
  the facts.
- **Infer the facts from `artifact_type` alone** — artifact type does not
  distinguish a stateless image bump from one that writes a migration, nor name
  the restore source; the undo-cost facts need their own declaration.
- **Hard-block a non-reversible rollout** — the Postgres cutovers are
  deliberate; the right control is a recorded acknowledgment, not a refusal.
- **A separate risk-manifest file** — a second home for a component fact drifts
  from the pins; it belongs beside `artifact_type`, one fact one home.
