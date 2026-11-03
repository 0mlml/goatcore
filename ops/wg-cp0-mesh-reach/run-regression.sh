#!/bin/bash
# Regression driver — proves the harness would catch F-116 had it existed.
#
# Mints bundles via the real bundle-create, then post-processes each one
# to drift AllowedIPs into the pre-ADR-0321 asymmetric shape (catchall on
# one specific relay per peer) and re-signs with the same dev CA. The
# resulting bench env has the same F-116 failure mode that bricked 4
# Orins .
#
# Expected outcome: the matrix FAILS. The harness emits a JSON verdict
# with pass=false. This script inverts the exit code so:
#
#   exit 0 = harness correctly identified the asymmetric bundles as broken
#   exit 1 = harness FAILED to identify them (i.e., the substrate-invariant
#            test is itself broken — release-blocking on the harness)
#
# Usage:
#   ./run-regression.sh
#   N=10 ./run-regression.sh
#   VERBOSE=1 ./run-regression.sh

set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "${HERE}"

: "${N:=4}"
: "${M:=3}"
: "${VERBOSE:=0}"

flags=(--peers "${N}" --relays "${M}" --regression)
if [[ "${VERBOSE}" == "1" ]]; then
    flags+=(--verbose)
fi

echo "[run-regression.sh] building harness..." >&2
# CGO required for transitive piv-go dep — see run.sh comment.
go build -o ./harness ./cmd/harness

echo "[run-regression.sh] running ASYMMETRIC matrix N=${N} M=${M} (FAIL expected)..." >&2

# Capture verdict regardless of exit code.
verdict_path="$(mktemp -t regression-verdict.XXXXXX.json)"
trap 'rm -f "${verdict_path}"' EXIT

set +e
./harness "${flags[@]}" --json-out "${verdict_path}"
harness_exit=$?
set -e

if [[ ! -s "${verdict_path}" ]]; then
    echo "[run-regression.sh] harness produced no verdict (exit=${harness_exit})" >&2
    exit 1
fi

# Re-print the verdict to stdout so the operator can see it.
cat "${verdict_path}"

# Parse pass field. We avoid jq as a hard dependency; use grep on the
# canonical "pass": false/true line.
if grep -q '"pass": false' "${verdict_path}"; then
    echo "[run-regression.sh] OK: harness correctly identified asymmetric bundles as broken (matrix failed as expected)" >&2
    exit 0
fi
if grep -q '"pass": true' "${verdict_path}"; then
    echo "[run-regression.sh] FAIL: harness reported PASS in regression mode — the substrate-invariant test is itself broken" >&2
    exit 1
fi
echo "[run-regression.sh] verdict file has no parseable pass field; failing safe" >&2
exit 1
