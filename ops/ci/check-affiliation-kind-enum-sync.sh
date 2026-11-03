#!/usr/bin/env bash
# ops/ci/check-affiliation-kind-enum-sync.sh — fail-closed guard that the
# affiliation.kind enum is IDENTICAL in the two schemas the onboarding flow
# validates against:
#   1. the operator-manifest JSON Schema (SoT the intake form + linters use), and
#   2. the Ory *site* Kratos identity-schema template's `aff.kind` enum (what
#      Kratos enforces when Phase-D aff-trait forwarding is live).
#
# WHY: a new affiliation kind (e.g. `civil`) added only to the manifest schema
# lints green in the intake form + onboard Phase-0 gate, then is SILENTLY
# REJECTED by Kratos at identity-create once aff forwarding is on — the exact
# manifest-vs-Kratos drift this guard prevents. Additive/destructive changes to
# affiliation.kind must touch BOTH files.
#
# Rollback: rm this file + its step in .github/workflows/roster-registry.yml.
set -euo pipefail

MANIFEST="${1:-ops/operators/schema/operator-manifest.schema.json}"
ORY_TPL="${2:-ops/ansible/roles/ory/templates/ory/identity-schema.json.j2}"

for f in "$MANIFEST" "$ORY_TPL"; do
  [[ -f "$f" ]] || { echo "FAIL: missing file: $f" >&2; exit 1; }
done

python3 - "$MANIFEST" "$ORY_TPL" <<'PY'
import json, re, sys
manifest, ory = sys.argv[1], sys.argv[2]

mk = json.load(open(manifest))["properties"]["affiliation"]["properties"]["kind"]["enum"]

txt = open(ory).read()
idx = txt.find('"title": "Affiliation kind"')
if idx == -1:
    sys.exit(f"FAIL: could not locate the aff.kind block in {ory}")
# The aff.kind enum is the last "enum": [...] just above its title.
window = txt[max(0, idx - 300):idx]
matches = re.findall(r'"enum"\s*:\s*(\[[^\]]*\])', window)
if not matches:
    sys.exit(f"FAIL: no enum found in the aff.kind block of {ory}")
ok = json.loads(matches[-1])

if set(mk) != set(ok):
    print("FAIL: affiliation.kind enum drift between the manifest schema and Ory identity-schema.", file=sys.stderr)
    print(f"  manifest ({manifest}): {sorted(mk)}", file=sys.stderr)
    print(f"  ory      ({ory}): {sorted(ok)}", file=sys.stderr)
    print("  A new/removed affiliation kind must change BOTH — else onboarding lints green but Kratos rejects it.", file=sys.stderr)
    sys.exit(1)

print(f"OK: affiliation.kind enum in lockstep across manifest schema + Ory identity-schema: {sorted(mk)}")
PY
