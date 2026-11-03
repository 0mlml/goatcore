#!/usr/bin/env bash
# parity-fastlane.sh — the parity gate for `goatbuilder fastlane` against
# ops/scripts/fastlane.sh (goatbuilder-conformance.md §The parity gate).
#
# Both run with --dry-run for the same component and ref, which exercises the
# three checks before any mutation (the component and its role and play, the
# open graduation set, the digest resolved under ghcr.io/usareur-af) and the dry
# run's statement of what the mutation would do. Every line is compared, and the
# binary's side is read from -o json (docs/design/goatbuilder-output.md rule 1). Where
# the GHCR packages API is not readable (a CI token), both refuse at the same
# step with the same words and that refusal is what is compared.
#
# usage: parity-fastlane.sh <goatbuilder binary> <component> <sha-tag|ref>
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"; REPO="$(cd "$HERE/../.." && pwd)"
BIN="${1:?}"; COMP="${2:?}"; REF="${3:?}"

norm() { sed -E 's#\x1b\[[0-9;]*m##g; /^[[:space:]]*$/d; s/^    //; s/^(fastlane|goatbuilder): REFUSE/REFUSE/'; }
a="$("$REPO/ops/scripts/fastlane.sh" "$COMP" --ref "$REF" --reason "parity gate" --ack "Parity Gate" --dry-run 2>&1 | norm || true)"
b="$("$BIN" -C "$REPO" -o json fastlane "$COMP" --ref "$REF" --reason "parity gate" --ack "Parity Gate" --dry-run 2>/dev/null \
  | python3 -c 'import json,sys; print("\n".join(c["detail"] for c in json.load(sys.stdin)["checks"]))' | norm || true)"
if [ -z "$a" ]; then echo "PARITY FAIL: fastlane $COMP $REF — the script produced nothing"; exit 1; fi
if [ "$a" != "$b" ]; then
  echo "PARITY FAIL: fastlane $COMP $REF"
  { diff <(printf '%s\n' "$a") <(printf '%s\n' "$b") || true; } | sed 's/^/    /' | head -30
  exit 1
fi
echo "parity ok: fastlane $COMP $REF --dry-run ($(printf '%s\n' "$a" | grep -c .) lines identical)"
