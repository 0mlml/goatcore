#!/usr/bin/env bash
# ops/ci/check-affiliation-id-trait-sync.sh — fail-closed guard that every org id
# the onboarding flow can produce is an id the Ory *site* Kratos identity schema
# accepts. Sources (what can be minted):
#   1. the operator-manifest JSON Schema's affiliation.id pattern, and
#   2. the org registry's id pattern (goat-id-console orgstore idRe), which the
#      console copies into affiliation.id when a sponsor pins an org.
# Targets (what Kratos enforces at identity-create):
#   - traits.org.short_tag (the native onboard copies affiliation.id into it), and
#   - traits.aff.id.
#
# WHY: the registry and the manifest allow 64 characters. If traits.org.short_tag
# allows fewer, an over-long org id passes the invite, the intake, both approvals
# and the Zenoh slot mint, then Kratos refuses the identity with HTTP 400 and the
# runner marks the proposal failed. A pattern change to any of the four must keep
# this green.
#
# Rollback: rm this file + its step in .github/workflows/roster-registry.yml.
set -euo pipefail

MANIFEST="${1:-ops/operators/schema/operator-manifest.schema.json}"
ORGSTORE="${2:-ops/goat-id-console/internal/orgstore/store.go}"
ORY_TPL="${3:-ops/ansible/roles/ory/templates/ory/identity-schema.json.j2}"

for f in "$MANIFEST" "$ORGSTORE" "$ORY_TPL"; do
  [[ -f "$f" ]] || { echo "FAIL: missing file: $f" >&2; exit 1; }
done

python3 - "$MANIFEST" "$ORGSTORE" "$ORY_TPL" <<'PY'
import json, re, sys
manifest, orgstore, ory = sys.argv[1], sys.argv[2], sys.argv[3]

def fail(msg):
    sys.exit(f"FAIL: {msg}")

sources = {
    "manifest affiliation.id": json.load(open(manifest))["properties"]["affiliation"]["properties"]["id"]["pattern"],
}
m = re.search(r'var idRe = regexp\.MustCompile\(`([^`]+)`\)', open(orgstore).read())
if not m:
    fail(f"could not locate idRe in {orgstore}")
sources["org registry idRe"] = m.group(1)

# The template is Jinja, not JSON: take the last "pattern" above each title.
txt = open(ory).read()
def pattern_above(title):
    idx = txt.find(f'"title": "{title}"')
    if idx == -1:
        fail(f'could not locate the "{title}" block in {ory}')
    found = re.findall(r'"pattern"\s*:\s*"((?:[^"\\]|\\.)*)"', txt[max(0, idx - 300):idx])
    if not found:
        fail(f'no pattern found in the "{title}" block of {ory}')
    return json.loads(f'"{found[-1]}"')
targets = {
    "Kratos traits.org.short_tag": pattern_above("Org short tag"),
    "Kratos traits.aff.id": pattern_above("Affiliation id (the unit)"),
}

# Boundary witnesses: every length up to past the widest ceiling, in the shapes
# the patterns distinguish (letters, digits, inner hyphens, edge hyphens).
witnesses = set()
for n in range(1, 81):
    witnesses.update({"a" * n, "0" * n, "-" * n})
    if n >= 2:
        witnesses.update({"a" + "-" * (n - 2) + "a", "-" + "a" * (n - 1), "a" * (n - 1) + "-"})

bad = []
for sname, spat in sources.items():
    for w in sorted(witnesses, key=len):
        if not re.fullmatch(spat, w):
            continue
        for tname, tpat in targets.items():
            if not re.fullmatch(tpat, w):
                bad.append((sname, tname, w))

if bad:
    print("FAIL: an org id the onboarding flow can mint is one Kratos refuses.", file=sys.stderr)
    for sname, spat in sources.items():
        print(f"  source {sname}: {spat}", file=sys.stderr)
    for tname, tpat in targets.items():
        print(f"  target {tname}: {tpat}", file=sys.stderr)
    seen = set()
    for sname, tname, w in bad:
        if (sname, tname) in seen:
            continue
        seen.add((sname, tname))
        print(f"  e.g. {len(w)}-char id valid for {sname}, refused by {tname}: {w}", file=sys.stderr)
    print("  Widen the Kratos pattern, or narrow the source, so every mintable id validates.", file=sys.stderr)
    sys.exit(1)

print("OK: every org id the manifest schema and the org registry allow validates as "
      "Kratos traits.org.short_tag and traits.aff.id")
PY
