#!/usr/bin/env bash
# run-handshake-preservation.sh — Block 22R.3 bench-env driver SCAFFOLD.
#
# Invariant under test: adding or removing a peer via the wg-cp0-server
# role MUST NOT reset the `latest handshake` counter of any pre-existing
# peer. See README.md §"Second invariant" for the full algorithm.
#
# Status: ⏳ Substrate-build queued. This file is a runnable scaffold that
# explains what the operator-fired driver needs to do; it does NOT yet
# perform the cross-product. When implemented it will share the same
# docker-compose substrate as the sibling combinatorial-ping driver.
#
# Pre-reqs (when implemented):
#   - Linux host with WireGuard kernel module (operator workstation in
#     /iso enter wg-cp0-mesh-reach-bench-env, or a bench EC2 in
#     terraform-prod with the matching SG).
#   - docker + docker-compose v2+.
#   - One built copy of bundle-create + wg-cp0-bundle-apply in this dir's
#     bin/ (the same shape the combinatorial driver needs; share between
#     drivers).
#
# Acceptance for Block 22R.3:
#   - Step 5 asserts original peers' handshake counters monotonic across
#     a peer-add role re-run.
#   - Symmetric driver (TBD: run-handshake-reset-on-shape-change.sh)
#     asserts handshake counters DO reset when the [Interface] stanza
#     changes — confirming the role still notifies `restart wg-cp0` when
#     appropriate.

set -euo pipefail

cat <<'EOF' >&2
run-handshake-preservation.sh: ⏳ NOT YET IMPLEMENTED.

This driver is the bench-env scope of the Block 22R.3 substrate-invariant
test class (ADR 1017). Today's PR ships only the unit-scope coverage at:

  ops/ansible/roles/wg-cp0-server/tests/test-classify-conf-change.sh

That suite verifies the classifier emits the right notification target
for each change shape against eleven fixture confs. This driver upgrades
that from "classifier picks the right handler" to "the handler actually
preserves handshakes on a live wg interface" — but it requires a Linux
host with the WireGuard kernel module, which is outside the cross-platform
per-PR CI envelope (Tier-A).

To implement, see README.md §"Second invariant — handshake preservation
under allowlist change (Block 22R.3)" for the algorithm.
EOF
exit 78  # EX_CONFIG — substrate not ready; not a test failure.
