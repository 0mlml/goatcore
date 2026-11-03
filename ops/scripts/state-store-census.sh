#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# state-store-census.sh — enumerate every persistent store on a mgmt host and
# answer two questions per store that nothing else on this fleet answers:
#
#   1. Is it on the engine the config claims?
#   2. Is anything backing it up?
#
# WHY THIS EXISTS. The NetBird management config declares
# StoreConfig.Engine = postgres and the compose stack creates a `netbird_events`
# Postgres database. Both are true and neither is the whole truth: the ACTIVITY
# store — the audit trail that records every peer-add and the setup key that
# admitted it — is SQLite-only in this NetBird version and writes to
# /var/lib/netbird/events.db inside the management container. The Postgres
# database of the same name sits beside it with zero tables, which reads as
# "the events are in Postgres" to anyone who looks there first. Someone did.
#
# The consequence is not academic. that SQLite file was the only
# surviving link between a peer named `goatnet-og-pubsub` and the person who
# enrolled it; the peer name identified nobody. pgBackRest — where it runs at
# all — backs up Postgres and would not have contained it.
#
# So the failure mode this guards is: state that matters lives somewhere nobody
# has enumerated, and the absence of a backup is discovered by needing one.
#
# usage:
#   ops/scripts/state-store-census.sh                 # census, human table
#   ops/scripts/state-store-census.sh --json          # machine-readable
#   ops/scripts/state-store-census.sh --check         # exit 1 on an unbacked store
#
# Runs ON the mgmt host (needs docker). For a remote host:
#   ssh <host> 'bash -s' < ops/scripts/state-store-census.sh --check
set -uo pipefail

MODE="table"
for a in "$@"; do
  case "$a" in
    --json) MODE="json" ;;
    --check) MODE="check" ;;
  esac
done

DOCKER="sudo docker"
rows=""   # engine|name|location|size_bytes|rows_or_tables|backed_up|note

add_row() { rows="${rows}${1}"$'\n'; }

# ── 1. Postgres databases ───────────────────────────────────────────────────
pg_container=""
for c in postgres-a postgres postgres-b; do
  $DOCKER inspect "$c" >/dev/null 2>&1 && { pg_container="$c"; break; }
done

stanza_count=0
if [ -n "$pg_container" ]; then
  stanza_count=$($DOCKER exec "$pg_container" pgbackrest info 2>/dev/null | grep -c '^stanza:')
  stanza_count=${stanza_count:-0}
  for db in $($DOCKER exec "$pg_container" psql -U postgres -At \
        -c "SELECT datname FROM pg_database WHERE datistemplate=false" 2>/dev/null); do
    tables=$($DOCKER exec "$pg_container" psql -U postgres -d "$db" -At \
      -c "SELECT count(*) FROM information_schema.tables WHERE table_schema='public'" 2>/dev/null)
    tables=${tables:-0}
    size=$($DOCKER exec "$pg_container" psql -U postgres -At \
      -c "SELECT pg_database_size('$db')" 2>/dev/null)
    if [ "$stanza_count" -gt 0 ]; then backed="yes"; else backed="NO"; fi
    note=""
    # A database with zero tables that something is configured to use is the
    # exact shape of the F-343 trap: it implies a home the data does not have.
    [ "$tables" = "0" ] && note="empty — implies a home the data does not have"
    add_row "postgres|${db}|${pg_container}:${db}|${size:-0}|${tables} tables|${backed}|${note}"
  done
fi

# ── 2. SQLite files under container mounts and volumes ──────────────────────
# Distroless images have no shell, so the container cannot be asked. Look at the
# host side of every mount instead — that sees named volumes and binds alike.
seen_paths=""
for c in $($DOCKER ps --format '{{.Names}}' 2>/dev/null); do
  srcs=$($DOCKER inspect "$c" --format '{{range .Mounts}}{{.Source}}{{"\n"}}{{end}}' 2>/dev/null)
  for s in $srcs; do
    # sudo test, NOT [ -d ]. Docker's volume root is mode 0700 root:root, so an
    # unprivileged [ -d ] answers "not a directory" for a directory that plainly
    # exists — and the store this whole script is about (events.db, inside
    # mgmt-host_management-a-data) is the one it silently skips. Caught only
    # because the census came back missing the file it was written to find.
    sudo test -d "$s" || continue
    # A container that mounts host root (cadvisor, node-exporter) would sweep in
    # every unrelated file on the box and attribute it to that container. Those
    # observers own no state; skip them rather than mislabel the whole host.
    [ "$s" = "/" ] && continue
    hits=$(sudo find "$s" -maxdepth 3 -type f \
      \( -name '*.db' -o -name '*.sqlite' -o -name '*.sqlite3' \) -size +0c 2>/dev/null)
    for h in $hits; do
      # skip OS package databases and vendored routing tables shipped in images
      case "$h" in
        # OS package databases, vendored routing tables shipped inside images,
        # and scratch copies an operator left behind while debugging. None of
        # these are state the fleet depends on.
        /usr/*|/tmp/*|/var/tmp/*|*/rpmdb*|*/dnf/*|*fontconfig*|*man-db*) continue ;;
        # Static reference datasets shipped with or downloaded into an image.
        # Losing one costs a re-download, not a fact about the fleet.
        *geonames_*|*GeoLite2*|*/lft.db|*/traceproto.db|*/traceroute.db) continue ;;
      esac
      case "$seen_paths" in *"|$h|"*) continue ;; esac
      seen_paths="${seen_paths}|$h|"
      # confirm it really is SQLite rather than trusting the extension
      hdr=$(sudo head -c 15 "$h" 2>/dev/null | tr -d '\0')
      [ "$hdr" = "SQLite format 3" ] || continue
      size=$(sudo stat -c %s "$h" 2>/dev/null || echo 0)
      add_row "sqlite|$(basename "$h")|${h}|${size}|-|NO|written by ${c}; pgBackRest does not cover files"
    done
  done
done

# ── 3. Declared vs observed: the management activity store ──────────────────
declared=$($DOCKER exec management-a cat /etc/netbird/management.json 2>/dev/null \
  | tr -d ' \n' | grep -oE '"Engine":"[a-z]+"' | cut -d'"' -f4)
observed="sqlite"
$DOCKER exec management-a ls /var/lib/netbird/events.db >/dev/null 2>&1 || observed="absent"

# ── output ──────────────────────────────────────────────────────────────────
unbacked=$(printf '%s' "$rows" | awk -F'|' '$6=="NO" && $5!="0 tables"' | grep -c . || true)

if [ "$MODE" = "json" ]; then
  printf '{\n  "declared_store_engine": "%s",\n  "observed_activity_store": "%s",\n' \
    "${declared:-unknown}" "$observed"
  printf '  "pgbackrest_stanzas": %s,\n  "unbacked_stores": %s,\n  "stores": [\n' \
    "$stanza_count" "$unbacked"
  printf '%s' "$rows" | awk -F'|' 'NF>=6{
    printf "%s    {\"engine\":\"%s\",\"name\":\"%s\",\"location\":\"%s\",\"bytes\":%s,\"contents\":\"%s\",\"backed_up\":\"%s\",\"note\":\"%s\"}",
      (n++?",\n":""), $1,$2,$3,($4==""?0:$4),$5,$6,$7 }'
  printf '\n  ]\n}\n'
  exit 0
fi

echo "state-store census — $(hostname)"
echo
printf '%-9s %-26s %12s  %-12s %-9s %s\n' ENGINE STORE BYTES CONTENTS BACKED-UP NOTE
printf '%.0s-' {1..108}; echo
printf '%s' "$rows" | awk -F'|' 'NF>=6{printf "%-9s %-26s %12s  %-12s %-9s %s\n",$1,$2,$4,$5,$6,$7}'
echo
echo "declared management store engine : ${declared:-unknown}"
echo "observed activity store          : ${observed}"
if [ "${declared:-}" = "postgres" ] && [ "$observed" = "sqlite" ]; then
  echo "  ^ MISMATCH — the activity trail is NOT in Postgres. This NetBird version"
  echo "    keeps it in SQLite regardless of StoreConfig.Engine. See F-343."
fi
echo "pgbackrest stanzas               : ${stanza_count}"
[ "$stanza_count" = "0" ] && echo "  ^ NO STANZA — Postgres on this host has never been backed up."
echo "stores holding data with no backup: ${unbacked}"

if [ "$MODE" = "check" ] && { [ "$unbacked" -gt 0 ] || [ "$stanza_count" = "0" ]; }; then
  echo
  echo "FAIL — see above. A store holding rows with no backup is a data-loss"
  echo "       exposure discovered only when a restore is needed."
  exit 1
fi
exit 0
