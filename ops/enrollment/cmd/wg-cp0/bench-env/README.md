# wg-cp0 orchestrator bench-env smoke

Block 22R.2 Session A end-to-end smoke for the `wg-cp0` declarative-substrate orchestrator. Covers the read-only and reconcile surfaces (`validate` / `list` / `show` / `diff` / `reconcile`) end-to-end, including the `--apply` audit-emit path.

```
./run-smoke.sh
```

Exit 0 = PASS; non-zero = FAIL with a diagnostic line.

## What's covered

- Build the binary from the current tree (gate — won't run if it doesn't compile).
- `validate` PASS on the canonical 11-peer fixture.
- `list` (table + `--json`) prints the canonical peer set.
- `show <peer>` prints the expected detail; `show ghost` exits non-zero.
- `diff` exits 0 with `WG_CP0_OFFLINE=1` + zero relays in the fixture.
- `reconcile --dry-run` prints the `PLAN` header.
- `reconcile --apply` writes one JSONL row to `WG_CP0_AUDIT_PATH`.
- The audit row carries the contract fields (`ts` / `operator` / `command` / `intent_kind` / `correlation_id` / `plan_summary` / `outcome` / `per_step`).
- Re-apply is idempotent and grows the audit log by one row.

## What's deferred

**Real-relay docker integration** — the existing [`ops/wg-cp0-mesh-reach/`](../../../../wg-cp0-mesh-reach/) harness brings up wg-cp0 on alpine containers but does not run sshd; the orchestrator's [`SSHLiveStateReader`](../live_state.go) reaches relays over SSH-over-wg-cp0. Wiring an sshd-equipped bench-env or a `docker exec`-based `LiveStateReader` is a follow-on track.

The unit + apply tests in the parent directory already cover the apply pipeline against `MockLiveStateReader` + `MockRelayMutator` with realistic peer / relay / rotation / unreachable shapes — see [`applier_test.go`](../applier_test.go) (15+ apply-pipeline tests including PreFlight failure, partial failure, audit emission, rotation, unknown-peer-no-remove, unreachable-relay-no-mutation), [`live_state_test.go`](../live_state_test.go) (parser + dial-failure + agent-failure paths), and [`coverage_lift_test.go`](../coverage_lift_test.go) (CLI body coverage including `--include-live-state` cross-check, `cmdDiff` flag matrix, `buildIOLayer`, audit-emit failure non-fatal).

## Prereqs

- Go toolchain (the binary is built fresh from the current tree on every run).
- `python3` (the audit-row schema check uses `json` from the standard library; no third-party deps).
- A POSIX shell (`bash` is what the script uses).

No Docker, no sudo, no network. Runs in any developer environment.

## Composes with

- [`ops/wg-cp0-mesh-reach/`](../../../../wg-cp0-mesh-reach/) — substrate-invariant test for the wg-cp0 wire-layer (ADR 1017 first instance). Different altitude — proves *bundles + relays produce a reachable mesh*. The orchestrator bench here proves *the operator-facing CLI mutates the JSON-of-record + audit log correctly*. Both gates green is what's required before the orchestrator subcommand fans out across the production fleet.
- `docs/operations/wg-cp0-ceremony-v2.md` — operator runbook. Describes the same five subcommands this smoke exercises.
