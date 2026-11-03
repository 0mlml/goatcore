#!/usr/bin/env bash
# warm-geolite.sh — fill testdata/geolite/ with the MaxMind databases
# netbird-mgmt would otherwise download during every integration-test startup.
#
# Why this exists: testdata/geolite/README.md. Short version — the download is
# ~82 MB from a third-party CDN inside a readiness window, and when that CDN's
# edge is cold it takes minutes and fails the gate on a deadline that says
# nothing about netbird.
#
# There is no artifact to fetch directly: the mmdb comes out of a tarball, and
# the geonames database is SQLite that netbird BUILDS from a CSV zip. So the
# honest way to produce them is to let netbird produce them, once, and keep the
# result. That is all this script does.
#
#   ./testdata/warm-geolite.sh          # default 30-minute budget
#   WARM_TIMEOUT_S=3600 ./testdata/warm-geolite.sh
#   GEOLITE_VERSION=20260602 ./testdata/warm-geolite.sh   # CI: the version netbird will ask for
#
# Idempotent: exits immediately if both databases are already present. With
# GEOLITE_VERSION set (the workflow passes the version it keyed the cache on)
# "present" means THAT version: an older pair restored from a stale cache is
# not a warm fixture, netbird would prune it and download anyway.
#
# Fails fast, not on the budget, when management dies during startup: since
# netbird 0.68 a geolocation database it cannot load is fatal
# (management/internals/server/modules.go), and one such case is the mirror's
# own doing, a sha256 pointer that names a newer file than
# the tarball it sits beside (#1069). Nothing here can warm through that; the
# script says so and exits 1 within a poll interval instead of 40 minutes.
#
# Every failure names its reason on $GITHUB_OUTPUT (reason=mirror-checksum,
# management-died or timeout) because the workflow treats them differently:
# only the mirror's refusal earns the run-without-geolocation fallback; the
# others fail the run, so a management that dies on something of its own is
# never read as a mirror outage (PR #1070 review). tests/warm-geolite.bats.
set -euo pipefail

cd "$(dirname "$0")/.."
COMPOSE_FILE=testdata/docker-compose.yml
GEO_DIR=testdata/geolite
TIMEOUT_S="${WARM_TIMEOUT_S:-1800}"

# Array-glob rather than compgen: an unmatched glob stays literal, so testing
# element 0 with -e is the portable "did this glob match anything" (works on
# macOS's bash 3.2 as well as the runners' 5.x).
have_both() {
  local mmdb geonames ver="${GEOLITE_VERSION:-}"
  case "$ver" in
    ''|unresolved|*[!0-9]*)
      mmdb=( "$GEO_DIR"/GeoLite2-City_*.mmdb )
      geonames=( "$GEO_DIR"/geonames_*.db )
      [ -e "${mmdb[0]}" ] && [ -e "${geonames[0]}" ] ;;
    *)
      [ -e "$GEO_DIR/GeoLite2-City_$ver.mmdb" ] && [ -e "$GEO_DIR/geonames_$ver.db" ] ;;
  esac
}

# Why management is not going to write the databases, or nothing: the first
# FATL line it logged (netbird's own startup failures), else "exited" when the
# container is gone without one (a bad config path dies on a plain Error
# line). Either way waiting for the files is waiting for the budget.
management_dead() {
  local fatal
  fatal="$(docker compose -f "$COMPOSE_FILE" logs --no-log-prefix management 2>/dev/null \
    | grep -m1 ' FATL ' || true)"
  if [ -n "$fatal" ]; then
    echo "$fatal"
    return 0
  fi
  if docker compose -f "$COMPOSE_FILE" ps --all --status exited --services 2>/dev/null \
    | grep -qx management; then
    echo "exited"
    return 0
  fi
  return 1
}

# The one word the workflow reads to decide what a failure means. A local
# run has no GITHUB_OUTPUT and loses nothing: the lines before it say the
# same thing in words.
report_reason() {
  echo "reason=$1" >> "${GITHUB_OUTPUT:-/dev/null}"
}

if have_both; then
  echo "warm-geolite: already warm:"
  ls -la "$GEO_DIR"/*.mmdb "$GEO_DIR"/*.db
  exit 0
fi

mkdir -p "$GEO_DIR"
# netbird runs as root in this image and writes the databases itself; the
# directory has to be writable by it, not only by us.
chmod 0777 "$GEO_DIR"

# A failed warm prints the postgres and management logs BEFORE the teardown:
# `down -v` removes the containers and their logs, so the workflow's own
# "Dump fixture logs on failure" step, which runs after this trap, finds
# nothing (#1521).
cleanup() {
  local rc=$?
  if [ "$rc" -ne 0 ]; then
    local svc
    for svc in postgres management; do
      echo "warm-geolite: last $svc logs before teardown:" >&2
      docker compose -f "$COMPOSE_FILE" logs --no-log-prefix --tail 60 "$svc" >&2 || true
    done
  fi
  docker compose -f "$COMPOSE_FILE" down -v --remove-orphans >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "warm-geolite: bringing the fixture stack up (budget ${TIMEOUT_S}s)"
docker compose -f "$COMPOSE_FILE" up -d

deadline=$(( $(date +%s) + TIMEOUT_S ))
while :; do
  if have_both; then
    echo "warm-geolite: both databases written"
    break
  fi
  if why="$(management_dead)"; then
    echo "warm-geolite: management died during startup ($why). Last management logs:" >&2
    docker compose -f "$COMPOSE_FILE" logs --no-log-prefix --tail 20 management >&2 || true
    case "$why" in
      *"checksum mismatch"*)
        echo "warm-geolite: pkgs.netbird.io's sha256 pointer does not describe the tarball it serves;" >&2
        echo "warm-geolite: netbird refuses the download and nothing here can warm through it (#1069)." >&2
        report_reason mirror-checksum ;;
      *)
        report_reason management-died ;;
    esac
    exit 1
  fi
  if [ "$(date +%s)" -ge "$deadline" ]; then
    echo "warm-geolite: TIMED OUT after ${TIMEOUT_S}s. Last management logs:" >&2
    docker compose -f "$COMPOSE_FILE" logs --tail 40 management >&2 || true
    echo "warm-geolite: contents of $GEO_DIR:" >&2
    ls -la "$GEO_DIR" >&2 || true
    report_reason timeout
    exit 1
  fi
  sleep 10
done

# The files are root-owned inside the container; hand them back so the cache
# step (and a later `git clean`) can touch them without sudo.
if [ "$(id -u)" -ne 0 ]; then
  sudo chown -R "$(id -u):$(id -g)" "$GEO_DIR" 2>/dev/null ||
    echo "warm-geolite: could not chown $GEO_DIR — continuing (files are readable)" >&2
fi

ls -la "$GEO_DIR"
echo "warm-geolite: done"
