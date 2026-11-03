# ADR 1200 — The deployment engineering is proved end to end in three tiers, with the ledger as the only proof

| Field | Value |
|---|---|
| Status | Accepted 2026-10-28 |
| Design | [deployment-e2e.md](../design/deployment-e2e.md) |
| Related | [ADR 1198](1198-goatbuilder-config-driven-deploy-release-cli.md) (goatbuilder), [ADR 1173](1173-release-candidate-tags-trigger-affected-flow-validation.md) (release candidates), [ADR 1060](1060-universal-deploy-rollback-ledger.md) (the ledger), [ADR 1117](1117-per-env-doors-cross-env-management.md) (doors), [ADR 1201](1201-two-avenues-scripts-documented-cli-supported.md) (both avenues) |

## Decision

1. **Three tiers, three questions.** A hermetic tier in CI proves the committed drivers move a component dev → preprod → prod and back against container hosts; a live canary from an operator laptop proves a deploy on the dev goatnet's management host today; a daily rehearsal dry-runs the next step of every open candidate. The host's ledger row is the only proof in every tier.
2. **The real drivers, unmodified.** The hermetic tier runs `deploy-component.sh`, `rc.sh`, `rollback.sh`, `fastlane.sh`, `pr-land.sh`, `ansible-preflight.sh`, `graduate.py`, `generate-pins.py` and `goatbuilder` as committed, on a clone whose declarations are an overlay (`ops/e2e/overlay.py`). What stands in — a bare git remote, a local registry the runner resolves as `ghcr.io`, stubs for `gh`, `crane` and `aws-vault` — is named in the stubs' own headers and recorded on every call, so a hermetic pass is never mistaken for live proof.
3. **A canary component, declared like any other.** `e2e-canary` is a fleet-pins component with a role, a play, the ledger meta-dependency and an image workflow; on the live table it is absent everywhere until the operator points the dev goatnet at a built image.
4. **A run is recorded, never narrated.** Every step writes one row to a machine-readable record (`ops/e2e/report.schema.json`); the rendered page replaces the operator logs people kept by hand on 2026-10-28.
5. **Two gates on what no tier moves.** Every variable a deploy templates has a reference line (`check-secrets-coverage.py`, judged where the private references live); every tool the release path invokes is named by its preflight (`check-deps-named.py`, with `rc.sh`'s `RC_DEPS`).
6. **Rows run where the context is.** A rung the laptop is not meshed to runs its live rows on its own management host through the door (`goat_via: door:<env>`); a row that cannot run is UNTESTED, never a dead candidate.
7. **Both avenues, every tier.** Per [ADR 1201](1201-two-avenues-scripts-documented-cli-supported.md) the scripts and the CLI both stay functional for the three golden paths; each tier runs both, and a failure on either is a failure of the run. The deployment path is covered today; founder standup and the federating node are the next increment.

## Why

Two external releases on 2026-10-28 — the first through `rc.sh` — needed six
retries and a day respectively, on defects the dry-run parity gates could not
see: those gates compare what two drivers *say* they would do; nothing ran a
play against a host and read a ledger back until an operator did. The operators
found the stale checkout, the unset secret, the missing tool, the unmapped
evidence path, the open-candidate refusal and the toolchain-less browser row
one at a time, by running. The design doc's corpus table holds each one and the
gate that now holds it.

## Alternatives

- **A real staging goatnet for CI.** Reaches a real host, costs a host and a credential path from CI, and no runner reaches a goatnet today (`goatnet-runners.yaml`). The live canary gets the real host from where the operator already stands; the hermetic tier gets CI.
- **Mocking the drivers' inner calls in unit tests.** Tests the model of the driver, not the driver; the defects in the corpus were in the glue (a `set -e` exit, an unset path, a discarded stderr), which only running the script finds.
- **Recording the hermetic run's answers for later replay.** Replays what the drivers did once; the point is to run them again on every change.

## Consequences

- Every PR touching a driver pays a 10–20 minute hermetic run; a change that breaks a release step is red before merge.
- `ops/e2e/` and the two canary roles are part of the open-source export: anyone adopting the drivers gets the harness that proves them.
- The preprod and prod management hosts need their goat contexts minted before their rows run through the door; until then those rows read UNTESTED in the record.
