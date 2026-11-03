#!/usr/bin/env bash
# ops/ci/check-ory-groups-enum.sh — keep the operator-manifest schema's
# ory_site.groups enum in lock-step with the Ory service-permission SoT.
#
# WHY: there are TWO group axes and they must not be conflated (the root of the
# ADMIN-vs-ADMINS confusion, F-213):
#   - mesh ROLE / node-class  → netbird.auto_groups, SoT ops/ansible/netbird_acl_taxonomy.yml
#   - Ory SERVICE-PERMISSION  → ory_site.groups,     SoT ops/permissions/group-permissions.yml
# The schema's ory_site.groups enum duplicates the group NAMES from
# group-permissions.yml; this guard asserts they match exactly so the enum can't
# drift from the source of truth (and can't silently re-acquire mesh-role names).
set -euo pipefail

SCHEMA="${1:-ops/operators/schema/operator-manifest.schema.json}"
PERMS="${2:-ops/permissions/group-permissions.yml}"

python3 - "$SCHEMA" "$PERMS" <<'PY'
import json, sys, yaml
schema_path, perms_path = sys.argv[1], sys.argv[2]
schema = json.load(open(schema_path))
enum = set(schema["properties"]["ory_site"]["properties"]["groups"]["items"]["enum"])
sot = set(g["name"] for g in yaml.safe_load(open(perms_path))["groups"])
missing = sot - enum   # in the SoT but not allowed by the schema
extra   = enum - sot   # allowed by the schema but not a real Ory group
ok = True
if missing:
    print("FAIL: ory_site.groups enum is MISSING SoT groups:", sorted(missing)); ok = False
if extra:
    print("FAIL: ory_site.groups enum has groups NOT in", perms_path, "(mesh-role leak / drift):", sorted(extra)); ok = False
if not ok:
    print()
    print("ory_site.groups (Ory SERVICE-PERMISSION axis) must match the SoT exactly:")
    print("  SoT:", sorted(sot))
    print("Mesh ROLE / node-class names belong in netbird.auto_groups, not here. See F-213.")
    sys.exit(1)
print("ok: ory_site.groups enum matches", perms_path, "(%d groups)" % len(sot))
PY
