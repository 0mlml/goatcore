# wg-cp0 mesh-reach — combinatorial substrate-invariant test (ADR 1017 first instance)

Per [ADR 0321](../../docs/adr/0321-wg-cp0-perimeter-not-acl.md) §"Implementation"
22R.0.2 + [ADR 1017](../../docs/adr/1017-substrate-invariant-test-class.md), this
directory is the substrate-invariant test for the wg-cp0 layer. The substrate
declares one combinatorial invariant: **any peer X can reach any peer Y over
wg-cp0**, for any X, Y in the active peer set, regardless of bundle ordering,
relay topology, or which peer received its bundle first. A regression of this
invariant is a release-blocking failure on any wg-cp0-touching change.

## Two altitudes — both load-bearing

The invariant is asserted at two altitudes:

### 1. Unit-scope (CI today)

`go test ./ops/enrollment/cmd/bundle-create/ -run TestRun_BundleSymmetryInvariant`

Authors bundles in-process via the Go `bundle-create` package across multiple
operator-flag combinations and asserts every issued bundle carries
`AllowedIPs = [MeshAddr/32, WGCP0FleetSubnet]` (canonical pair) on every
relay peer block. This catches the **issuer-side** half of the failure class:
any code path that constructs an asymmetric bundle trips the assertion at
test time.

Runs on every commit touching `ops/enrollment/` via the existing Go test
harness. Zero infrastructure required.

### 2. Bench-env scope (CI-gated + operator-fireable)

`./run.sh` (symmetric, PASS expected) / `./run-regression.sh` (asymmetric, FAIL expected).

Stands up N containerized peers + M containerized wg-cp0 relays inside a
single host network namespace, issues real offline-CA-signed bundles via
`bundle-create`, applies them on each peer via `wg-cp0-bundle-apply`, brings
up wg-cp0 on each peer, then runs the **combinatorial ping matrix**:

```bash
for src in peers; do
  for dst in peers; do
    if [ "$src" != "$dst" ]; then
      docker exec $src ping -c2 -W3 $dst.MeshAddr
      assert exit 0
    fi
  done
done
```

A pre-ADR-0321 asymmetric bundle would trip the matrix at some specific
`(src, dst)` pair where the bundles disagreed on `/24` placement; the
post-ADR-0321 symmetric default produces a green N × (N-1) matrix.

The matrix is the **load-bearing acceptance gate** for wg-cp0 substrate
changes per ADR 1017 first-instance language.

## Status

| Altitude | Status | Where |
| --- | --- | --- |
| 1. Unit-scope invariant assertion | ✅ Active CI gate | `ops/enrollment/cmd/bundle-create/main_test.go::TestRun_BundleSymmetryInvariant` |
| 2. Bench-env combinatorial ping | ✅ Active CI gate + operator-fireable | This directory; `./run.sh` (symmetric) + `./run-regression.sh` (asymmetric, FAIL expected) |

Both altitudes are active. The unit-scope test catches issuer-side
drift on every commit touching `ops/enrollment/`. The bench-env test
catches end-to-end network-layer regressions (device-side renderer,
Ansible-layer drift, packet-forwarding interactions) that the unit
test does not exercise.

Operator-facing runbook: [`docs/operations/wg-cp0-mesh-reach-harness.md`](../../docs/operations/wg-cp0-mesh-reach-harness.md).
CI workflow: [`.github/workflows/wg-cp0-mesh-reach.yml`](../../.github/workflows/wg-cp0-mesh-reach.yml).

## Why this layout (not absorbed into existing harnesses)

- `ops/ansible/tests/wg-cp0-syntax-check.sh` is a parse-only smoke; can't
  exercise the cross-product runtime path.
- `ops/ansible/roles/*/tests/` are per-role parse-only smokes for the same
  reason.
- The bench-env shape needs Docker + WireGuard kernel module + multi-peer
  topology — too heavy for the per-PR Tier-A CI tier, fits the operator-fired
  Tier-C tier per [`docs/design/pr-governance.md`](https://github.com/usareur-af/goatdocs/blob/main/docs/design/pr-governance.md).

## Bench-env shape (as built)

- M relay containers (default M=3; CI floor) running the wg-cp0 server
  role: kernel WireGuard listener on UDP 51821, IPv4 forwarding enabled
  in the container netns so relays forward peer-to-peer.
- N peer containers (default N=4; CI floor): each gets its own bundle
  applied by `wg-cp0-bundle-apply` at startup, brings up `wg-cp0` via
  `wg-quick up wg-cp0`.
- Single host-side orchestrator (`cmd/harness/main.go`) — generates a
  throwaway dev CA, mints bundles via the real in-tree `bundle-create`
  binary (built from the current source tree on every run), extracts
  per-peer wg-cp0 pubkeys from each bundle, renders per-relay
  wg-cp0.conf with every peer as a `[Peer]` block, brings up the
  Docker bridge network + containers, then `docker exec` ping
  cross-product.
- All containers attached to a single Docker bridge network
  (`172.30.0.0/24` by default) simulating the inner control-plane
  fabric.

Total ~$0 substrate cost; ~30-60s wall-clock per N=4 M=3 matrix run on
ubuntu-24.04.

## Second invariant — handshake preservation under allowlist change (Block 22R.3)

`./run-handshake-preservation.sh` (scaffolded — driver TBD; see status table)

Same bench-env layout (N peers + M relays); different invariant. Asserts the
Block 22R.3 substrate property: **adding or removing a peer via the
`wg-cp0-server` role MUST NOT reset the `latest handshake` counter of any
pre-existing peer**. The driver:

1. Bring up the initial peer set (e.g. 4 peers, 3 relays).
2. Wait for all peers to handshake; record each peer's `latest handshake`
   monotonic timestamp on every relay (`wg show wg-cp0 dump`).
3. Add a fifth peer to the allowlist and re-run the `wg-cp0-server` role
   against every relay.
4. Re-record `latest handshake` for the original 4 peers on every relay.
5. **Assert: original peers' handshake counters are monotonically newer-or-
   equal** (i.e. never reset to "never handshaked" or to a stale prior value).
6. **Assert: the new peer reaches the relay and handshakes within 60s.**

A pre-22R.3 role implementation (`restart wg-cp0` notified on every render)
would trip step 5 because the wg-quick restart drops every peer's session
and resets `latest handshake` to "(none)" until the next keepalive cycle.
The post-22R.3 implementation, which notifies `reload wg-cp0 peers`
(`wg syncconf`) for allowlist-only deltas, leaves established sessions
intact.

Symmetric coverage for the listener-shape case: a separate driver verifies
that changing `wg_cp0_listen_port` in inventory + re-running the role DOES
reset handshakes — confirming the role still notifies `restart wg-cp0` when
the [Interface] stanza actually changes. Both directions together close
the substrate-invariant gap.

The unit-scope sibling for this invariant is
[`ops/ansible/roles/wg-cp0-server/tests/test-classify-conf-change.sh`](../../ops/ansible/roles/wg-cp0-server/tests/test-classify-conf-change.sh),
which asserts the classifier emits the right notification target for each
change shape against eleven fixture confs. The bench-env scope here
upgrades that from "classifier picks the right handler" to "the handler
actually preserves handshakes on a live wg interface."

## Status — handshake-preservation altitude

| Altitude                                   | Status                    | Where                                                                       |
| ------------------------------------------ | ------------------------- | --------------------------------------------------------------------------- |
| 1. Classifier unit suite                   | ✅ Active CI gate         | `ops/ansible/roles/wg-cp0-server/tests/test-classify-conf-change.sh`        |
| 2. Bench-env handshake-preservation matrix | ⏳ Substrate-build queued | This directory (driver TBD)                                                 |

## Cross-refs

- [ADR 0321 — wg-cp0 is a perimeter, not an ACL layer](../../docs/adr/0321-wg-cp0-perimeter-not-acl.md)
- [ADR 1017 — substrate-invariant test class](../../docs/adr/1017-substrate-invariant-test-class.md)
- F-114 — wg-cp0 rotation serialization gap — drove the 22R.3 substrate change this dir covers.
- F-116 — wg-cp0 asymmetric AllowedIPs misapplied zero-trust
- [`docs/design/wg-cp0-peer-lifecycle.md`](../../docs/design/wg-cp0-peer-lifecycle.md) §"Substrate default: `wg syncconf` for allowlist-only changes" — design spec for 22R.3.
- `docs/project/implementation-plan.md` Block 22R.0.2 + 22R.3.
