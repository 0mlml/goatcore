#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# rosenpass-remediate.sh — find every peer still reporting RosenpassEnabled=true
# and produce the per-owner remediation list.
#
# WHY THIS EXISTS: turning `rosenpass_enabled: false` in group_vars stops NEW
# enrollments from getting the flag. It does NOT touch a peer that is already
# enrolled — the setting is client-local and persists across `netbird down/up`.
# Every already-enrolled peer keeps shipping a 524,160-byte McEliece static
# public key in every signal OFFER and ANSWER until its owner clears it.
#
# There is no central off-switch. The management server only OBSERVES the flag:
# `Flags` lives inside `PeerSystemMeta` (shared/management/proto/management.proto),
# which is client→server telemetry. Management cannot push it down. That is why
# this is a roster-and-contact problem rather than a deploy.
#
# READ-ONLY by default. It reports; it does not revoke, expire or mutate.
#
# usage:
#   ops/scripts/rosenpass-remediate.sh            # roster, grouped by owner
#   ops/scripts/rosenpass-remediate.sh --csv      # machine-readable
#   ops/scripts/rosenpass-remediate.sh --connected-only
#
# env:
#   THOR_HOST   mgmt host to query (default the ZAGROS Thor mesh IP)
#   PG_CONTAINER  postgres container name (default postgres-a)
set -euo pipefail

THOR_HOST="${THOR_HOST:-100.127.0.175}"
PG_CONTAINER="${PG_CONTAINER:-postgres-a}"
SSH_USER="${SSH_USER:-goat}"
MODE="${1:-report}"

# psql quoting is hostile through two layers of shell; write the file locally
# and copy it, exactly as the investigation did.
tmp="$(mktemp)"
cat > "$tmp" <<'SQL'
\pset format unaligned
\pset fieldsep '|'
\pset tuples_only on
SELECT name,
       CASE WHEN peer_status_connected THEN 'UP' ELSE 'down' END,
       meta_go_os,
       meta_wt_version,
       to_char(peer_status_last_seen,'YYYY-MM-DD HH24:MI')
FROM peers
WHERE (meta_flags::json->>'RosenpassEnabled')='true'
ORDER BY peer_status_connected DESC, name;
SQL

# 0600 from mktemp is unreadable by the `postgres` uid inside the container, and
# psql's "Permission denied" goes to stderr where an earlier version of this
# script swallowed it and printed a confident "0 peers affected". A guard that
# reports all-clear because it could not read anything is worse than no guard.
chmod 644 "$tmp"

scp -q -o BatchMode=yes "$tmp" "${SSH_USER}@${THOR_HOST}:/tmp/rp-remediate.sql"
raw="$(ssh -o BatchMode=yes "${SSH_USER}@${THOR_HOST}" \
  "sudo docker cp /tmp/rp-remediate.sql ${PG_CONTAINER}:/tmp/rp.sql >/dev/null 2>&1;
   sudo docker exec ${PG_CONTAINER} psql -U postgres -d netbird -f /tmp/rp.sql 2>/dev/null;
   rm -f /tmp/rp-remediate.sql")"
rm -f "$tmp"

# Fail loudly rather than reporting an empty roster we did not actually observe.
if [ -z "${raw//[[:space:]]/}" ]; then
  echo "rosenpass-remediate: FAILED to read the peer table (empty result)." >&2
  echo "  This is NOT an all-clear. Check ssh to ${THOR_HOST}, sudo docker," >&2
  echo "  and that ${PG_CONTAINER} is running." >&2
  exit 2
fi

python3 - "$MODE" <<PYEOF
import re, sys, collections
mode = sys.argv[1]
raw = """$raw"""
rows = [l.strip().split("|") for l in raw.splitlines() if "|" in l]
by_owner = collections.defaultdict(list)
for r in rows:
    if len(r) < 5:
        continue
    name, state, os_, ver, seen = r[0], r[1], r[2], r[3], r[4]
    m = re.match(r"peer-(.+?)-zagros-\d{4}-\d{2}-\d{2}-(.*)\$", name)
    owner = m.group(1) if m else "(infrastructure — no handle in peer name)"
    host = m.group(2) if m else name
    by_owner[owner].append((state, host, os_, ver, seen))

if mode == "--csv":
    print("owner,host,state,os,version,last_seen")
    for o, v in sorted(by_owner.items()):
        for state, host, os_, ver, seen in v:
            print("%s,%s,%s,%s,%s,%s" % (o, host, state, os_, ver, seen))
    raise SystemExit

connected_only = (mode == "--connected-only")
total = sum(len(v) for v in by_owner.values())
up = sum(1 for v in by_owner.values() for s, *_ in v if s == "UP")
print()
print("  Peers still reporting RosenpassEnabled=true: %d across %d owners (%d connected now)"
      % (total, len(by_owner), up))
print("  Each one ships a 524,160-byte McEliece key in EVERY signal offer.")
print()
for o, v in sorted(by_owner.items(), key=lambda x: (not any(s == "UP" for s, *_ in x[1]), x[0])):
    if connected_only and not any(s == "UP" for s, *_ in v):
        continue
    flag = "CONNECTED" if any(s == "UP" for s, *_ in v) else "offline"
    print("  %-10s %s" % (flag, o))
    for state, host, os_, ver, seen in v:
        print("       %-4s %-40s %-8s %-8s last %s" % (state, host[:40], os_, ver, seen))
print()
print("  REMEDIATION each owner runs on their own machine:")
print("      netbird up --enable-rosenpass=false --rosenpass-permissive=false")
print("      netbird status | grep Quantum      # must read 'false'")
print()
print("  Plain 'netbird up' does NOT clear it — the setting persists across down/up.")
print("  There is no central off-switch: management only OBSERVES the flag.")
PYEOF
