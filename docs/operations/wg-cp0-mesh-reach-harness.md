# wg-cp0 mesh-reach harness — operator runbook

The wg-cp0 combinatorial mesh-reach harness is the bench-env half of the
[ADR 1017](../adr/1017-substrate-invariant-test-class.md) substrate-invariant
test class for wg-cp0, applied per [ADR 0321](../adr/0321-wg-cp0-perimeter-not-acl.md)
§"Implementation" 22R.0.2.

**Invariant under test.** Any peer X can reach any peer Y over wg-cp0 for
every (X, Y) pair in an N-peer × M-relay deployment, regardless of bundle
ordering. A regression of this property is a release-blocking failure on
any wg-cp0-touching change.

## When to run

- **Per-PR (mandatory).** CI runs the floor (N=4, M=3) on every PR that
  touches `ops/enrollment/**` or `ops/wg-cp0-mesh-reach/**`. The CI job
  is the gate; do not merge red.
- **Nightly (broader coverage).** Same workflow runs N=10, M=3 on the
  scheduled cron. Catches issues that only surface at slightly higher
  peer counts (e.g. routing-table or ARP-cache interactions).
- **Operator-fired before a wg-cp0 substrate change.** Run locally
  against a working tree to confirm the new code path doesn't regress
  the invariant. Faster signal than waiting on CI.
- **As part of the Block-22R completion review.** Per [ADR 1015](../adr/1015-code-review-discipline.md)
  the block-completion checklist names this harness as the substrate-invariant-
  test row; cite the green run in the review.

## Prerequisites

- Linux host with the WireGuard kernel module loaded (default on kernel
  5.6+; standard on GitHub Actions `ubuntu-24.04` runners and modern
  desktop distros). `lsmod | grep -q wireguard` should match.
- Docker daemon reachable. `docker info` must succeed.
- Go toolchain (any 1.25+; the harness compiles the real `bundle-create`
  and `wg-cp0-bundle-apply` binaries from the current tree on every run
  — that is the gate. If either binary doesn't build, the harness fails
  before touching docker).

No further setup. The harness builds its own container image on first
run from the in-tree [Dockerfile](../../ops/wg-cp0-mesh-reach/Dockerfile);
subsequent runs reuse the cached image.

## How to run — symmetric (PASS expected)

```
cd ops/wg-cp0-mesh-reach
./run.sh
```

Knobs:

```
N=10 ./run.sh           # 10 peers × 3 relays (nightly shape)
M=4 ./run.sh            # 4 peers × 4 relays
VERBOSE=1 ./run.sh      # phase logs to stderr
```

The harness emits a JSON verdict to stdout. Exit codes:

- `0` — PASS. Matrix is all-green; the substrate-invariant holds.
- `1` — FAIL. At least one (src, dst) pair could not ping. The `matrix`
  field in the JSON identifies which pairs failed; the `output` for
  each failed cell carries the ping output for triage.
- `2` — harness error (could not build a binary, docker network create
  failed, container failed to come up, etc.). Re-run with `VERBOSE=1`.

A symmetric-mode FAIL is the substrate signaling "the wg-cp0 mesh is
broken." Treat it the same as a Block 22R ADR-violation. Open a finding
per [ADR 1011](../adr/1011-findings-register.md).

## How to run — regression mode (FAIL expected; proves harness teeth)

```
cd ops/wg-cp0-mesh-reach
./run-regression.sh
```

This driver mints normal bundles via `bundle-create`, then mutates each
one to drift `AllowedIPs` into the pre-ADR-0321 asymmetric shape
(catchall placed on a single relay per peer, different relay per peer)
and re-signs with the same throwaway dev CA. The bench env then has
the exact F-116 failure shape.

`run-regression.sh` **inverts** the exit-code interpretation:

- `0` — the harness correctly reported a matrix FAIL (substrate-invariant
  test detected the asymmetry). This is the **healthy** outcome.
- `1` — the harness reported PASS. The substrate-invariant test is itself
  broken (the drift didn't propagate, the bundle-apply renderer
  silently corrected the issue, or the test is otherwise not exercising
  what it claims to exercise). Release-blocking on the harness.

The regression driver is what gives the harness teeth. Without it we
would have no in-band evidence that the matrix actually catches the
failure mode it is supposed to catch.

## Reading the JSON verdict

```json
{
  "schema": "wg-cp0-mesh-reach.v1",
  "started_at": "2026-05-13T22:30:00Z",
  "finished_at": "2026-05-13T22:31:42Z",
  "n_peers": 4,
  "m_relays": 3,
  "mode": "symmetric",
  "pass": true,
  "total_pairs": 12,
  "failed_pairs": 0,
  "matrix": [
    {"src": "peer-0", "dst": "peer-1", "dst_ip": "198.18.0.11", "ok": true},
    ...
  ]
}
```

- `total_pairs` = N×(N-1) (every ordered pair).
- `matrix[].output` is populated only when `ok: false`; carries the
  raw ping output for triage.
- `mode` is `symmetric` for `run.sh`, `regression-asymmetric` for
  `run-regression.sh`.

## Common failure modes

| Symptom | Likely cause | Where to look |
|---|---|---|
| harness exits 2 saying "cannot locate harness source dir" | run.sh launched from outside `ops/wg-cp0-mesh-reach/` | cd into the dir first; the harness anchors on `$WGCP0_MESH_REACH_SRC` if set |
| `docker build` fails | Docker daemon not reachable; or apt repo network down | `docker info`; `docker pull alpine:3.20` |
| "containers not ready" timeout | wireguard kernel module not loaded on the host | `lsmod | grep wireguard`; `sudo modprobe wireguard` |
| matrix has all-pair FAIL with "no route to host" | relay-side IP forwarding didn't enable inside containers | container `sysctl -n net.ipv4.ip_forward` should be 1; the entrypoint sets it but a hardened seccomp / AppArmor profile could refuse |
| matrix has exactly some pairs failing in symmetric mode | a real bundle-create or wg-cp0-bundle-apply regression — this is the F-116 class. **Open a finding.** |

## How to extend

- **Higher N.** The harness supports up to N=200 / M=250 (bridge subnet
  exhaustion is the floor; harness asserts). For larger sweeps the
  10-min CI budget on `ubuntu-24.04-large` is the practical ceiling.
- **Different relay topologies.** Today every peer's bundle carries all
  M relays. To exercise partial-membership topologies (e.g. peer
  pinned to 2 of 3 relays) extend `mintBundles` to filter the
  relays.json passed to `bundle-create`. Out of scope for v1.
- **Other invariants.** ADR 1017 names several substrate-invariant
  candidates (Spock replication, signal-server federation, bundle
  format, work-CLI schema, OCI image distribution, Zenoh router
  topology). This harness is wg-cp0-only; each other substrate gets
  its own test under its own substrate directory.

## Cross-refs

- [ADR 0321 — wg-cp0 is a perimeter, not an ACL layer](../adr/0321-wg-cp0-perimeter-not-acl.md)
- [ADR 1017 — substrate-invariant test class](../adr/1017-substrate-invariant-test-class.md)
- F-116 — wg-cp0 asymmetric AllowedIPs
- [ADR 1015 — code-review discipline](../adr/1015-code-review-discipline.md) — block-completion checklist row
- [`ops/wg-cp0-mesh-reach/README.md`](../../ops/wg-cp0-mesh-reach/README.md) — internal-facing developer notes
- `docs/operations/wg-cp0-fleet-bundle-re-issue.md` — fleet re-issue ceremony (the 22R.0.3 sibling)
