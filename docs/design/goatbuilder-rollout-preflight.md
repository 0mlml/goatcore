# goatbuilder rollout preflight

`goatbuilder preflight <component> --env <env>` answers one question before a
rollout: if this goes wrong, how cleanly can we undo it? `terraform plan` shows
what will change; this shows what undo would cost. It prints three facts and the
readiness the other verbs already prove, honesty-graded, and `deploy` runs it as
its first phase.

Canon: [ADR 1211](../adr/1211-rollout-preflight-readiness-and-reversibility.md).
Builds on the deploy ledger ([ADR 1060](../adr/1060-universal-deploy-rollback-ledger.md),
[universal-deploy-rollback.md](universal-deploy-rollback.md)), the fastlane
safe-class rule ([ADR 1127](../adr/1127-fastlane-urgent-fleet-wide-delivery.md)), graduation
sets ([ADR 1120](../adr/1120-release-graduation-ladder.md) /
[ADR 1125](../adr/1125-fleet-reconciliation-join-and-graduation-sets.md)), the declared end state
([ADR 1206](../adr/1206-goatnet-declared-end-state.md)) and both-avenues parity
([ADR 1201](../adr/1201-two-avenues-scripts-documented-cli-supported.md)).

## The three facts

The readout is three facts, never a single editorial "risk" word. Each is
printed on its own row, with the one-line summary `<state> · rollback: <r> · <scope>`.

| Fact | Values | Means |
|---|---|---|
| **State** | `stateless` \| `stateful` | whether the rollout creates or migrates persistent data |
| **Rollback** | `pin-revert` \| `restore-required` \| `none` | how undo is done: flip the pin, restore from a named backup, or there is no return |
| **Scope** | `single` \| `coupled` | whether undo is independent, or the component is one member of a graduation set that moves together |

`restore-required` prints its restore pointer and the days left in its retention
window, so the reader sees both where undo comes from and whether that source is
still there. The live SQLite→Postgres cutovers (`id-admin-console`, `portal`,
[ADR 1165](../adr/1165-island-mode-multi-writer-every-site-admits.md)) are the canonical
`stateful · restore-required` rollout: undo means restoring the pre-cutover
SQLite archive, which is kept for 28 days.

## One declared home, fail-closed

The facts are declared per component in `ops/pins/fleet-pins.yaml`, beside the
`artifact_type` the deploy contract already carries — one fact, one home. A
component that does not declare them reads `stateful · rollback: none
(undeclared)`, which grades not-clean: an undeclared rollout is a gap made
visible, never a hopeful green guessed from a name.

| Field | Type | Meaning |
|---|---|---|
| `owns_state` | bool | the State fact. A `data` artifact is stateful whatever this says |
| `rollback` | `pin-revert`\|`restore-required`\|`none` | the Rollback fact |
| `restore_from` | `{pointer, retention_days, as_of}` | required for `restore-required`: where undo restores from, how long the source is kept, and the date the window started |
| `coupled_with` | `[component, …]` | the Scope fact, for coupling the ladder does not already record |

Coupling also derives from the release ladder: if the component is in an OPEN
graduation set (`ops/release/sets/`), Scope reads `coupled` and names the set and
its other members, without a second declaration. `consistency-console` reads
coupled because it sits in the open set `consistency-rj1` with
`consistency-health`.

On a fixture (`--mock <dir>`) the same facts are declared in the fixture's
`declared/artifacts.yaml`, so the grading is exercised end to end without a
goatnet. The `preflight` fixture carries one component per case the grading must
get right: a stateless pin-revert, a stateful restore-required with its window,
an undeclared component, and a declared coupling.

**Declaring the live components.** The reader and the grading are built and
tested; the fields are not yet written onto the live components in
`ops/pins/fleet-pins.yaml`, because the signed consistency intent
(`ops/consistency/intent/<goatnet>/intent.json`) hashes the whole `fleet-pins.yaml`
in its `sources`, so any edit to that file must regenerate and re-sign the three
intents with the per-goatnet dev-CA keys (`ops/enrollment/cmd/intent-sign`). The
canonical declarations to write — `id-admin-console` and `portal` as `stateful ·
restore-required` with the pre-cutover SQLite archive (28-day window from the
ADR 1165 cutover), `netvis` and the `consistency-*` pair as `stateless ·
pin-revert` — are the worked example in the fixture today; they land on the live
pins in the change that holds the CA keys. Until then those components read
fail-closed in `preflight`, which is correct, not a false green.

## Grading: the declaration checked against the ledger

A declaration is trusted only where it agrees with what the deploy contract
already records. The fastlane rule ([ADR 1127](../adr/1127-fastlane-urgent-fleet-wide-delivery.md))
is the grading logic: a pin flip is the safe class; a schema, identity, ACL or
data migration is not.

- `pin-revert` is clean only for an `image` or `config` artifact that is not
  flagged `reversible: false`. Declared over a `data`, `cert` or `forward-only`
  component, or one marked `reversible: false`, it is a lie and grades not-clean
  — a ref swap cannot undo that rollout, whatever the field says.
- `restore-required` is clean only with a `restore_from` pointer. Without one,
  there is no named return, and it grades not-clean.
- `none` is always a valid declared fact (a first-ever deploy has no prior ref),
  but it is not cleanly reversible, so `deploy` requires it be acknowledged.

## It measures; it does not assume

The readiness and validation halves are honesty-graded like `assay`: a check
that cannot run reads `UNKNOWN`, never a false pass. The verb grades clean (exit
0) only when every row is a proven pass.

**Readiness** reuses the checks the other verbs prove:

| Row | How |
|---|---|
| candidate ref | digest-pinned (always), and pullable when a registry probe can run (`docker manifest inspect`; `UNKNOWN` with no docker or `GOAT_PREFLIGHT_PULL=0`) |
| secrets gate | the deploy-time secrets the play templates are all satisfied (#2011, reused from `push::plan`) |
| pins --check | no drift, every intent signed (`goatbuilder pins --check`) |
| fresh source | the checkout is current with the trunk (C3, `ops/scripts/fresh-source.sh`) |
| end state | the component's end state is declared in `ops/goatbuilder/endstate/catalog.yaml` ([ADR 1206](../adr/1206-goatnet-declared-end-state.md)) |

**Validation** proves the Rollback fact against the live state:

- `pin-revert` → `rollback --dry-run` decides the flip without touching a host.
  A refusal here contradicts the declared fact and grades not-clean; no reachable
  ledger reads `UNKNOWN`.
- `restore-required` → the retention window is computed from `retention_days`
  and `as_of`. An expired window grades not-clean (there is no in-window source);
  a window that cannot be computed reads `UNKNOWN`. The archive itself is off-box
  and is not reached from here — the row says to verify it before deploy.

## deploy runs it first

`deploy`'s first phase is the preflight readout (the prep phase): it prints the
three facts and the summary line before the fresh-source, secrets and transport
phases, reusing the same declaration read (no second resolution, no reinvented
machinery).

A rollout graded `none` or `restore-required` is not cleanly reversible, so a
real `deploy` of it proceeds only with an explicit `--ack "<name>"`. Without one,
`deploy` refuses and names the risk; with one, it proceeds and writes the
accepted facts into the deploy ledger row (`rollback_preflight_accept`, recorded
by the `rollback-ledger` role), so the record shows what was knowingly accepted
and by whom. It does **not** hard-block: a stateful cutover stays possible,
knowingly. `apply` of a reviewed plan supplies the plan author as the ack.

## Both avenues

The script avenue is `ops/scripts/deploy-component.sh --preflight` and the shared
`ops/scripts/rollout-preflight.py`, which reads the same declared fields and
prints the same three facts ([ADR 1201](../adr/1201-two-avenues-scripts-documented-cli-supported.md)).
The CLI verb is the supported path; the script is documented and kept coherent.

## Exit codes

| Exit | Meaning |
|---|---|
| 0 | ready and cleanly reversible — every fact declared and validated, every readiness row a pass |
| 3 | not clean (`GB-076`) — a readiness check failed, a fact is undeclared (fail-closed), a declared rollback contradicts the artifact type, or a validation could not answer |
| 1 | cannot run (`GB-069`) — unknown component, or the pins could not be read |

## Boundaries

Preflight reads only; it deploys nothing and rolls back nothing. It does not
reach the restore archive (off-box) — it reads the declared retention window and
tells the operator to verify the source. The `deploy` prep phase prints the
readout and enforces the ack; the exhaustive readiness and validation are the
standalone `preflight` verb, so a `deploy` is not slowed by a rollback dry-run on
every run.
