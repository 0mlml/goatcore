#!/bin/bash
# Run the wg-cp0 combinatorial mesh-reach test (symmetric mode, PASS expected).
#
# Invariant: every peer can reach every other peer over wg-cp0. A regression
# of this property is a release-blocking failure on any wg-cp0-touching
# change (ADR 1017 first instance, applied at bench-env scope per ADR 0321
# §"Implementation" 22R.0.2).
#
# Usage:
#   ./run.sh                 # floor: N=4 peers × M=3 relays
#   N=10 ./run.sh            # nightly shape
#   N=4 M=3 VERBOSE=1 ./run.sh
#
# Prereqs:
#   - Docker daemon reachable (`docker info` works)
#   - Linux host with the wireguard kernel module loaded (default on
#     kernel 5.6+ — ubuntu-24.04 GitHub Actions runners and recent
#     desktop distros)
#   - Go toolchain (the harness builds bundle-create + wg-cp0-bundle-apply
#     from the current tree on every run; that is the gate — if either
#     binary doesn't build, the harness fails before touching docker)
#
# Exits 0 on PASS (matrix all-green), 1 on FAIL, 2 on harness error.

set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "${HERE}"

: "${N:=4}"
: "${M:=3}"
: "${VERBOSE:=0}"

flags=(--peers "${N}" --relays "${M}")
if [[ "${VERBOSE}" == "1" ]]; then
    flags+=(--verbose)
fi

# Build harness once per invocation. CGO must be enabled because the
# enrollment module transitively pulls in piv-go (libpcsclite cgo dep);
# Linux CI runners need libpcsclite-dev apt-installed (matches the
# existing ops/enrollment workflow pattern).
echo "[run.sh] building harness..." >&2
go build -o ./harness ./cmd/harness

echo "[run.sh] running symmetric matrix N=${N} M=${M}..." >&2
./harness "${flags[@]}"
