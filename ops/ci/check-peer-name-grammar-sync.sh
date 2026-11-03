#!/usr/bin/env bash
# ops/ci/check-peer-name-grammar-sync.sh — fail-closed guard that every
# implementation of the NetBird peer-name grammar reproduces the one SoT
# (ops/identity/peer-name-grammar.yaml) byte-for-byte.
#
# WHY: under setup-key enrollment NetBird leaves `peer.user_id` empty, so the
# peer NAME is the only link from a mesh peer to the principal that owns it
# (docs/design/peer-identity-resolution.md, Option B). That made the name a
# load-bearing cross-store join, and the grammar has independent implementations
# across Go, Rust and an Ansible playbook that need a fence between them.
#
# The failure mode is silent in BOTH directions, which is why it needs a fence
# rather than a test:
#   - a matcher that drops the trailing separator attributes an ORG-MATE's peer
#     to the wrong principal (a mis-targeted revoke, not an error message);
#   - a matcher that never fires reports an honest-looking ZERO (peers
#     unresolvable to a person, no error).
#
# Two legs, because the consumers are not all generatable:
#   1. Generated targets (Go + Rust) — `generate-peer-name.py --check` proves
#      each checked-in file is exactly what the SoT produces. Re-sync with
#      `python3 ops/identity/generate-peer-name.py --write`.
#   2. The Ansible onboard playbook templates the same grammar in Jinja and is
#      the live onboarding path, so it is asserted, not rewritten: every
#      peer-name-constructing expression must still place the fields in the
#      SoT's order. `min_sites` keeps the assertion from passing vacuously if
#      the playbook stops constructing peer names in that shape at all.
#
# Full-tree (not diff-scoped): asserts a property that passes today, so there
# are no grandfathered violations.
#
# Rollback: rm this file + .github/workflows/peer-name-grammar.yml. The SoT and
# the generated code are inert without them (nothing at runtime reads the yaml).
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$REPO_ROOT"

SOT="ops/identity/peer-name-grammar.yaml"
GEN="ops/identity/generate-peer-name.py"

for f in "$SOT" "$GEN"; do
  if [[ ! -f "$f" ]]; then
    echo "FAIL: missing $f" >&2
    exit 1
  fi
done

if ! python3 -c 'import yaml' 2>/dev/null; then
  echo "FAIL: PyYAML required (pip install pyyaml)" >&2
  exit 1
fi

# ---- leg 1: the generated Go + Rust consumers reproduce the SoT -------------
if ! python3 "$GEN" --check; then
  echo >&2
  echo "  A peer-name consumer no longer matches $SOT." >&2
  echo "  Re-sync with:" >&2
  echo "    python3 $GEN --write" >&2
  echo "  If you MEANT to change the grammar, edit the SoT (not the generated" >&2
  echo "  file), bump its version, and re-run --write. Changing the prefix," >&2
  echo "  separator or field order renames every peer in the fleet and breaks" >&2
  echo "  every revoke selector — that is a MAJOR bump plus a migration." >&2
  exit 1
fi

# ---- leg 2: the Ansible onboard playbook still uses the SoT's field order ---
python3 - "$SOT" <<'PY'
import re, sys, pathlib, yaml

sot = yaml.safe_load(open(sys.argv[1]))
spec = sot["ansible_assertions"]
path = pathlib.Path(spec["file"])
if not path.exists():
    sys.exit(f"FAIL: {path} does not exist — the peer-name fence's Ansible leg has no target")

text = path.read_text()
ok = True
for a in spec["assertions"]:
    hits = len(re.findall(a["regex"], text))
    if hits < a["min_sites"]:
        ok = False
        print(f"FAIL: {path}: {a['what']} — found {hits} site(s), expected at least {a['min_sites']}.")
        print(f"  pattern: {a['regex']}")
        print("  Either a peer-name expression stopped placing the handle immediately after")
        print("  the prefix (a FIELD-ORDER drift from the SoT — fix the playbook), or the")
        print("  playbook legitimately stopped building names that way (update the assertion")
        print(f"  in {sys.argv[1]}, and say why in the commit).")
    else:
        print(f"OK: {path}: {a['what']} — {hits} site(s) in the SoT's field order.")

sys.exit(0 if ok else 1)
PY

echo "OK: every peer-name implementation agrees with $SOT."
