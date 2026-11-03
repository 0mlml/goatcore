#!/usr/bin/env bash
# F-258 acl-assemble round-trip test. Proves the assembler is lossless:
# extract(live) -> seed + org, then assemble(seed, org) must reproduce the
# original live config byte-for-byte. Also checks that the seed's per-org
# regions are empty (role-renderable) and the org fragment carries the rules.
#
# Runs on the synthetic fixture (no real tenant identities). The same tool was
# validated byte-exact against nova-a's real 104-rule live config off-box.
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
TOOL="$HERE/../../files/acl-assemble.py"
FIX="$HERE/fixture-live.json5"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

python3 "$TOOL" extract  --live "$FIX" --seed "$WORK/seed" --org "$WORK/org"
python3 "$TOOL" assemble --seed "$WORK/seed" --org "$WORK/org" --out "$WORK/rebuilt"

if ! diff -q "$FIX" "$WORK/rebuilt" >/dev/null; then
  echo "[FAIL] acl-assemble round-trip is LOSSY:"; diff "$FIX" "$WORK/rebuilt" | head -30; exit 1
fi

# seed must have NO per-org rules between the markers (a re-render is safe);
# org fragment must carry them.
seed_org_rules=$(python3 "$TOOL" extract --live "$FIX" --seed "$WORK/s2" --org "$WORK/o2" >/dev/null 2>&1; \
  awk '/BEGIN: per-vendor write rules/{f=1;next} /END: per-vendor write rules/{f=0} f&&/permission:/{c++} END{print c+0}' "$WORK/s2")
org_rules=$(grep -c 'permission:' "$WORK/o2")
if [ "$seed_org_rules" != "0" ]; then
  echo "[FAIL] seed still carries $seed_org_rules per-org write rules (should be 0)"; exit 1
fi

# --- init-org + empty-fragment no-op (the fresh-standup invariant) -----------
# On a FRESH standup the org fragment is empty, and assemble(seed, empty) must
# reproduce the seed byte-for-byte — i.e. splicing empty content at each END
# marker is a no-op. Proven here against the whole fixture used AS a seed, so a
# new goatnet renders exactly as if the assembler were not in the path.
python3 "$TOOL" init-org --org "$WORK/empty-org" >/dev/null
# header-only fragment: one line per region, no body
hdr_lines=$(grep -c '^##ACL-ORG-REGION ' "$WORK/empty-org")
body_lines=$(grep -vc '^##ACL-ORG-REGION ' "$WORK/empty-org" || true)
if [ "$hdr_lines" != "4" ] || [ "$body_lines" != "0" ]; then
  echo "[FAIL] init-org fragment is not header-only ($hdr_lines headers, $body_lines body lines)"; exit 1
fi
python3 "$TOOL" assemble --seed "$FIX" --org "$WORK/empty-org" --out "$WORK/noop"
if ! diff -q "$FIX" "$WORK/noop" >/dev/null; then
  echo "[FAIL] assemble(seed, empty-org) is NOT a no-op:"; diff "$FIX" "$WORK/noop" | head -20; exit 1
fi

# --- init-org must NEVER clobber a fragment that already has org content ------
# A role re-run calls init-org idempotently; it must leave a populated fragment
# (portal-added vendors) untouched, or a re-render would drop them.
before=$(grep -c 'permission:' "$WORK/org")
python3 "$TOOL" init-org --org "$WORK/org" >/dev/null
after=$(grep -c 'permission:' "$WORK/org")
if [ "$before" != "$after" ] || [ "$before" = "0" ]; then
  echo "[FAIL] init-org clobbered a populated fragment ($before -> $after rules)"; exit 1
fi

echo "[PASS] acl-assemble round-trip byte-exact; seed per-org regions empty; org fragment carries $org_rules write rules; assemble(seed,empty)==seed no-op; init-org preserves a populated fragment ($after rules)."
