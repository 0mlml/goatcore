#!/usr/bin/env bash
# front-door-probe.sh — pin the F-405 front-door probe and its F-430 opt-out.
#
# Why this test exists: F-405 added a probe that dials a row's public FQDN on
# :443 and grades DEGRADED on 404/502/503/000. It had no opt-out, so it fired
# on the zenoh-router row — whose FQDN 404s BY DESIGN, because the router is
# probed on its REST plugin over the container network and no Traefik router is
# ever declared for the `zenoh.<site>` name. That row had ALREADY declared
# `probe_gate: false` (ADR 1133) for exactly this reason, and the new probe did
# not honour it, so a healthy Zenoh router read DEGRADED.
#
# The pattern is easy to re-introduce: the next prober added to this file will
# also default to "dial every row". So this test asserts BOTH halves — the
# probe still catches a genuinely shut front door, AND a row that opted out is
# left alone — rather than only the bug that was fixed.
#
# Runs check.sh for real against a fixture services.env, with curl/docker/getent
# stubbed on PATH. No container, no network, no ansible.
set -euo pipefail
cd "$(dirname "$0")"

CHECK_SH="$(cd ../files && pwd)/check.sh"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

mkdir -p "$WORK/bin" "$WORK/data"

# --- stubs -----------------------------------------------------------------
# docker: every container exists and has a mgmt IP, so the container probe path
# is the one under test rather than the missing-container path.
cat >"$WORK/bin/docker" <<'STUB'
#!/usr/bin/env bash
case "$1" in
  info)    exit 0 ;;
  inspect) echo "172.20.0.2" ;;
  *)       exit 0 ;;
esac
STUB

# getent: every hostname resolves, so the F-197 branch never short-circuits.
cat >"$WORK/bin/getent" <<'STUB'
#!/usr/bin/env bash
echo "172.20.0.2 $2"
STUB

# curl: the whole point. A container-IP probe (http://<ip>:<port>) answers 200.
# A front-door probe (https://<fqdn>) answers 404 — the shut-door condition.
cat >"$WORK/bin/curl" <<'STUB'
#!/usr/bin/env bash
url="${!#}"
case "$url" in
  https://*) echo -n "404" ;;
  *)         echo -n "200" ;;
esac
STUB
chmod +x "$WORK/bin"/*

# --- fixture ---------------------------------------------------------------
# Two rows, identical but for probe_front_door. Format 3.
#   id|name|fqdn|type|container|port|health_path|kind|access|category|icon|desc|link_path|probe_front_door|expect_codes
cat >"$WORK/services.env" <<'FIXTURE'
APP_SERVICES_FORMAT=3
APP_SERVICES=(
  "zenoh-router|Zenoh Router|zenoh.site.example|container|zenoh-router|8000|/@/local/router|api|admins|platform||no public route by design||false|2|3"
  "portal|Portal|portal.site.example|container|portal|8090|/api/health|ui|public|platform||front door must serve||true|2|3"
)
FIXTURE

# --- run -------------------------------------------------------------------
ISITUP_TEST_BIN="$WORK/bin" \
ISITUP_DATA_DIR="$WORK/data" \
ISITUP_SERVICES_ENV="$WORK/services.env" \
  bash "$CHECK_SH" >/dev/null

status_of() {
  python3 -c "
import json,sys
d=json.load(open('$WORK/data/current.json'))
for s in d.get('services',d):
    if s['id']==sys.argv[1]:
        print(s['status']); sys.exit(0)
sys.exit('row $1 not emitted at all')
" "$1"
}

fail=0

# 1. The opt-out row must NOT be dialled on :443, so a 404 there is invisible
#    and the healthy container probe stands.
got=$(status_of zenoh-router)
if [[ "$got" == "up" ]]; then
  echo "[isitup/front-door-probe] PASS  probe_front_door=false row is up (404 on :443 ignored)"
else
  echo "[isitup/front-door-probe] FAIL  probe_front_door=false row graded '$got', expected 'up'"
  echo "                                 F-430 has regressed: the front-door probe is ignoring the opt-out."
  fail=1
fi

# 2. The default row MUST still be caught. Without this half, deleting the
#    probe entirely would pass the test above.
got=$(status_of portal)
if [[ "$got" == "degraded" ]]; then
  echo "[isitup/front-door-probe] PASS  default row is degraded (shut front door still caught)"
else
  echo "[isitup/front-door-probe] FAIL  default row graded '$got', expected 'degraded'"
  echo "                                 F-405 has regressed: a healthy container behind a 404 front door reads clean."
  fail=1
fi

# 3. A format-2 file (no probe_front_door column) must keep the pre-F-430
#    behaviour — every row dialled — so a staggered bind-mount update, which is
#    why APP_SERVICES_FORMAT exists at all, mis-grades nothing.
cat >"$WORK/services.env" <<'FIXTURE'
APP_SERVICES_FORMAT=2
APP_SERVICES=(
  "portal|Portal|portal.site.example|container|portal|8090|/api/health|ui|public|platform||front door must serve||2|3"
)
FIXTURE
ISITUP_TEST_BIN="$WORK/bin" \
ISITUP_DATA_DIR="$WORK/data" \
ISITUP_SERVICES_ENV="$WORK/services.env" \
  bash "$CHECK_SH" >/dev/null
got=$(status_of portal)
if [[ "$got" == "degraded" ]]; then
  echo "[isitup/front-door-probe] PASS  format-2 file still probes the front door"
else
  echo "[isitup/front-door-probe] FAIL  format-2 row graded '$got', expected 'degraded'"
  fail=1
fi

exit "$fail"
