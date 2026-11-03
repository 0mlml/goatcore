#!/usr/bin/env bash
# B2.1 partner read-partition render guard (partner-read-partition.md).
# Proves two invariants against config.json5.j2 without a live zenohd:
#   1. partition OFF (default) renders BYTE-IDENTICAL to origin/main — the
#      default-off safety guarantee (a partner carve can never be a silent
#      fabric-wide read change);
#   2. partition ON produces the partner subject + key-scoped read/deliver
#      rules, narrows trial-peer-read off link_protocols (partner excluded),
#      and grants the partner NO write on its lanes.
# Live ENFORCEMENT validation (a real partner cert on a running zenohd) remains
# the deploy gate — see partner-read-partition.md §"Validation gate".
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
tpl="$here/../../templates/config.json5.j2"
main_tpl="$(mktemp)"; trap 'rm -f "$main_tpl"' EXIT
git show origin/main:"ops/ansible/roles/goat-zenoh/templates/config.json5.j2" > "$main_tpl"
python3 "$here/render.py" off-diff "$tpl" "$main_tpl" | tee /dev/stderr | grep -q "OFF byte-identical: True"
python3 "$here/render.py" on "$tpl" "$main_tpl"
echo "[PASS] partition off = byte-identical; on = scoped partner read, narrowed universal read, no write grant"
