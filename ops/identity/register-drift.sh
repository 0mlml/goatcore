#!/usr/bin/env bash
# Two onboarding paths write two registers. This fails when they
# disagree, so the split cannot silently re-open once it is closed.
#
# IT COMPARES THE TWO DURABLE STORES, NOT THE LOCAL MANIFESTS.
# `ops/operators/<env>/*.yml` is GITIGNORED (ops/operators/.gitignore:8) because
# the files carry names and addresses — a manifest directory reflects whichever
# mints that laptop happened to run and is evidence of nothing fleet-wide. The
# committed, PII-safe projection is docs/operators/<env>/roster.json, and that
# is what this reads.
#
# IT READS THAT GOATNET'S ROSTER, NOT THE UNION. It compares one goatnet's
# declared half against one goatnet's onboard.db, so reading
# docs/operators/roster.json — the union across every environment — would
# report every other environment's people as drift on this one and drown the
# real signal. Identity does not cross a goatnet boundary (ADR 1172), and
# neither does the measurement of it.
#
# WHY A GUARD AND NOT A REPORT. Without something comparing the two, a gap
# survives until somebody reconciles the mesh by hand.
#
# usage: register-drift.sh <env> [--db <onboard.db>] [--allow N]
# bash 3.2 compatible (macOS operator laptops).
set -uo pipefail

ENVNAME="${1:-}"
[ -z "$ENVNAME" ] && { echo "usage: $0 <env> [--db <path>] [--allow N]" >&2; exit 2; }
shift
DB=""; ALLOW=0
while [ $# -gt 0 ]; do
  case "$1" in
    --db) DB="$2"; shift 2 ;;
    --allow) ALLOW="$2"; shift 2 ;;
    *) echo "unknown arg: $1" >&2; exit 2 ;;
  esac
done

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
ROSTER="$REPO_ROOT/docs/operators/$ENVNAME/roster.json"
if [ ! -r "$ROSTER" ]; then
  echo "cannot read $ROSTER" >&2
  echo "  That is $ENVNAME's own declared half. Build it with" >&2
  echo "    ops/operators/gen-roster.py --env $ENVNAME" >&2
  echo "  Do NOT substitute docs/operators/roster.json: that is the union across" >&2
  echo "  every environment and would report other goatnets' people as drift here." >&2
  exit 2
fi

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

python3 - "$ROSTER" > "$TMP/roster" <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
for m in d.get("members", []):
    h = (m or {}).get("handle")
    if h:
        print(h)
PY
sort -u -o "$TMP/roster" "$TMP/roster"

if [ -z "$DB" ]; then
  echo "NOTE: no --db given, so only the roster side was read. This reports its"
  echo "      size and CANNOT detect drift — pass --db to actually compare."
  echo "roster handles: $(wc -l < "$TMP/roster" | tr -d ' ')"
  exit 0
fi
# The path is resolved by the READER, not by this shell: with SQLITE_CMD set to a
# container the db path is the CONTAINER's, so testing it here would fail on a
# perfectly good invocation. Only check readability when we are the ones opening
# the file.
if [ -z "${SQLITE_CMD:-}" ] && [ ! -r "$DB" ]; then
  echo "cannot read $DB" >&2; exit 2
fi

# A MISSING READER MUST NOT LOOK LIKE TOTAL DRIFT. Without this, an absent
# sqlite3 yields an empty console list and every roster handle reads as missing —
# the guard then reports maximum drift with total confidence. Mgmt hosts do not
# carry sqlite3, so this is the normal case, not an edge
# one. `--sqlite` takes an alternative reader, e.g. a container:
#   --sqlite 'docker run --rm -v /var/lib/goat-id-console:/db:ro keinos/sqlite3:latest sqlite3'
SQLITE="${SQLITE_CMD:-sqlite3}"
if ! command -v ${SQLITE%% *} >/dev/null 2>&1; then
  echo "REFUSING: no sqlite3 reader ('${SQLITE%% *}' not found)." >&2
  echo "  The console side would read as EMPTY and every roster handle would be" >&2
  echo "  reported as drift. Set SQLITE_CMD to a working reader, e.g." >&2
  echo "    SQLITE_CMD='docker run --rm -v /var/lib/goat-id-console:/db:ro keinos/sqlite3:latest sqlite3'" >&2
  exit 2
fi

$SQLITE "$DB" "select distinct handle from onboard_proposals;" | sort -u > "$TMP/console"
# The reverse direction counts only rows that actually MINTED. A rejected,
# failed or archived row never produced an operator manifest and therefore was
# never meant to reach the roster — counting it as drift overstates the figure,
# which is the fastest way to make a guard ignorable.
$SQLITE "$DB" "select distinct handle from onboard_proposals \
  where status in ('"'"'onboarded'"'"','"'"'enrolled'"'"');" | sort -u > "$TMP/console_minted"
if [ ! -s "$TMP/console" ]; then
  echo "REFUSING: the console store returned NO handles. That is either an" >&2
  echo "  unreadable database or the wrong path — never a real roster of zero." >&2
  exit 2
fi

MISSING_ROW=$(comm -23 "$TMP/roster" "$TMP/console" | wc -l | tr -d ' ')
MISSING_ROSTER=$(comm -13 "$TMP/roster" "$TMP/console_minted" | wc -l | tr -d ' ')
BOTH=$(comm -12 "$TMP/roster" "$TMP/console" | wc -l | tr -d ' ')

echo "register drift — $ENVNAME"
echo "  in both                    : $BOTH"
echo "  roster, NO console row     : $MISSING_ROW   <- invisible to every lifecycle lane"
echo "  minted here, NOT in roster : $MISSING_ROSTER   (rejected/failed/archived excluded — they never minted)"

if [ "$MISSING_ROW" -gt "$ALLOW" ]; then
  echo
  echo "FAIL: $MISSING_ROW identities are in the roster with no console row."
  echo "      They cannot be archived, cannot be reported dormant, and appear in"
  echo "      no count an operator reads."
  comm -23 "$TMP/roster" "$TMP/console" | head -20 | sed 's/^/        /'
  [ "$MISSING_ROW" -gt 20 ] && echo "        ... and $((MISSING_ROW - 20)) more"
  exit 1
fi
echo "PASS"
