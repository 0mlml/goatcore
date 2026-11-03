#!/usr/bin/env bash
# goat-moon-pod conformance smoke-test entry point.
#
# Status: skeleton. Test logic lands as Block 60A-extension per
# docs/project/implementation-plan.md.
#
# See README.md for full design.

set -euo pipefail

POD_ENDPOINT="${1:-}"
VENDOR="${VENDOR:-open}"

if [[ -z "$POD_ENDPOINT" ]]; then
  echo "usage: $0 <pod-endpoint>" >&2
  echo "  env: VENDOR={open,lattice,foundry,catalyst,custom} (default: open)" >&2
  exit 64
fi

echo "goat-moon-pod conformance smoke-test"
echo "  pod-endpoint: $POD_ENDPOINT"
echo "  vendor:       $VENDOR"
echo
echo "STATUS: skeleton — test logic not yet implemented."
echo "        Tracked as Block 60A-extension."
echo "        See tests/moon-pod-conformance/README.md."
exit 78  # EX_CONFIG — skeleton; not an actual verdict
