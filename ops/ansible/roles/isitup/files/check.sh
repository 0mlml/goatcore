#!/usr/bin/env bash
# check.sh — generalized health checker for the canonical mgmt-host isitup.
#
# Runs inside the `isitup` nginx container on each cron tick (every 5 min).
# Reads APP_SERVICES from /etc/isitup/services.env (bind-mounted from the
# host), checks each entry, and writes:
#   /usr/share/nginx/html/data/current.json   — latest snapshot
#   /usr/share/nginx/html/data/history.json   — per-service, per-day uptime
#
# Both files are served by nginx at /data/{current,history}.json; the
# index.html SPA polls /data/current.json every 60 s.
#
# Each emitted service object carries: id, name, group, status, detail, url,
# kind, access, icon, desc. status is one of up / down / degraded / unknown.
#
#   unknown  — the probe itself could not run (docker socket/CLI gone, no HTTP
#              response at all). The absence of a measurement, NOT downtime;
#              the SPA renders it a hollow grey ring, never counts it as a
#              degraded service, and the history accumulator excludes it from
#              the uptime % entirely.
#   down     — we reached the service and it answered wrong, OR the docker
#              daemon is up and the container DOES NOT EXIST. That second case
#              is graded down, not unknown: unknown would hide a service that is
#              declared but never deployed behind an all-green board.
#   degraded — measured, real, but not a dead process: today only F-197, a
#              declared hostname that does not resolve. Counts against uptime.
#
# The url field (https://<fqdn>/) is rendered as an active
# link in the page. group = the per-service `category` (page section); icon =
# the per-row glyph key (see index.html ICONS). Both default empty/app so
# unclassified sites render unchanged. desc = the one-sentence "what breaks if
# this is down" line the SPA renders under the row name; empty on any row that
# hasn't declared one, which renders exactly as before.
#
# Differs from ops/status/check.sh: no relay tier, no
# rathole tunnel, no Netbird DNS probe — those concerns live in the relay
# tier. Mgmt-host sites get app-services-only visibility.

set -euo pipefail

# Fixed PATH: cron gives this script a minimal environment and the container
# installs docker-cli at start, so the lookup set must not depend on whatever
# the caller had. ISITUP_TEST_BIN prepends one directory and exists ONLY for
# the role's tests, which stub curl/docker/getent to drive the grading logic
# without a container or a network. Nothing in the deploy sets it; the cron
# entry and the compose command both leave it unset.
export PATH="${ISITUP_TEST_BIN:+$ISITUP_TEST_BIN:}/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

# Paths are the container's real ones by default. They are overridable ONLY so
# the role's tests can drive this script against a fixture without a container
# (ops/ansible/roles/isitup/tests/front-door-probe.sh). Nothing in the deploy
# sets them.
DATA_DIR="${ISITUP_DATA_DIR:-/usr/share/nginx/html/data}"
CURRENT="$DATA_DIR/current.json"
HISTORY="$DATA_DIR/history.json"
SERVICES_ENV="${ISITUP_SERVICES_ENV:-/etc/isitup/services.env}"
VERBOSE=false

[[ "${1:-}" == "--verbose" ]] && VERBOSE=true

mkdir -p "$DATA_DIR"

TIMESTAMP=$(date -u +%Y-%m-%dT%H:%M:%SZ)
TODAY=$(date -u +%Y-%m-%d)

SERVICES_JSON="["
FIRST=true

emit() {
    local id="$1" name="$2" group="$3" status="$4" detail="$5" url="${6:-}" kind="${7:-ui}" access="${8:-}" icon="${9:-}" desc="${10:-}"
    $FIRST || SERVICES_JSON+=","
    FIRST=false
    detail=$(printf '%s' "$detail" | tr -d '\n\r\t' | sed 's/"/\\"/g')
    url=$(printf '%s' "$url" | tr -d '\n\r\t' | sed 's/"/\\"/g')
    icon=$(printf '%s' "$icon" | tr -d '\n\r\t' | sed 's/"/\\"/g')
    # Operator-written prose — same escaping as detail so a stray quote or a
    # line break in a group_vars `desc` can't emit invalid JSON and blank the
    # whole page.
    desc=$(printf '%s' "$desc" | tr -d '\n\r\t' | sed 's/\\/\\\\/g; s/"/\\"/g')
    # The row name is operator-written too, and services.env.j2 now lets a `"`
    # through in it (escaped for bash), so it needs the same JSON treatment.
    name=$(printf '%s' "$name" | tr -d '\n\r\t' | sed 's/\\/\\\\/g; s/"/\\"/g')
    SERVICES_JSON+=$(printf '{"id":"%s","name":"%s","group":"%s","status":"%s","detail":"%s","url":"%s","kind":"%s","access":"%s","icon":"%s","desc":"%s"}' \
        "$id" "$name" "$group" "$status" "$detail" "$url" "$kind" "$access" "$icon" "$desc")
    if $VERBOSE; then printf "  %-20s %-10s %-8s %-6s %s\n" "$id" "${group:-—}" "${access:-—}" "$status" "$detail"; fi
}

# Is the docker daemon reachable from this checker? Answers the question that
# an empty `docker inspect` result cannot: "no IP" means the container is
# missing OR the daemon is gone, and those grade differently (down vs unknown).
#
# Memoized in DOCKER_DAEMON_STATE for the life of one tick. Only called on the
# missing-container path, so a healthy board never pays for it. `docker info`
# is the cheapest call that touches the daemon; --format keeps the output to
# one line so a hung daemon can't flood the log.
DOCKER_DAEMON_STATE=""
docker_daemon_reachable() {
    if [[ -z "$DOCKER_DAEMON_STATE" ]]; then
        if timeout 10 docker info --format '{{.ServerVersion}}' >/dev/null 2>&1; then
            DOCKER_DAEMON_STATE=up
        else
            DOCKER_DAEMON_STATE=down
        fi
    fi
    [[ "$DOCKER_DAEMON_STATE" == "up" ]]
}

check_app() {
    local id="$1" name="$2" host="$3" type="${4:-container}" container="${5:-}" port="${6:-}" health_path="${7:-/}" kind="${8:-ui}" access="${9:-}" category="${10:-app}" icon="${11:-}" desc="${12:-}" link_path="${13:-}" expect_codes="${14:-2|3}" probe_front_door="${15:-true}"
    local code
    # link_path is where the ROW'S LINK points; health_path is where the PROBE
    # goes. They are usually both "/" and were once the same value, which is why
    # this used to be a bare host root — a service whose useful page is a
    # sub-path (the cross-goatnet relay overview at /goatnets.html) could not be
    # linked at all. Default "/" keeps every existing row byte-identical.
    local url="https://${host}${link_path:-/}"

    # probe_unavailable distinguishes "the checker couldn't run the probe"
    # (docker socket/CLI gone, no container IP, no HTTP response at all) from
    # "the service answered with a bad code". The former is NOT downtime — it's
    # the absence of a measurement — so it must never paint a row red. A
    # transient docker hiccup (e.g. the isitup container itself was just
    # recreated by compose-watch and is still reinstalling docker-cli) would
    # otherwise fail EVERY row's `docker inspect` at once and report a
    # full-board "outage" that never actually happened. Honesty-graded like the
    # fabric/mesh health surfaces: unknown, not false-red.
    local probe_unavailable=false
    if [[ "$type" == "container" && -n "$container" && -n "$port" ]]; then
        # Direct container-IP probe via the bind-mounted docker socket.
        # docker-cli is installed at container start (see compose command).
        # `|| true` on each step so a missing container or pipefail does not
        # abort the whole check.sh tick — the cron loop must finish every row
        # so one bad entry doesn't blackhole the dashboard.
        local ip="" attempt
        # Multi-network containers: prefer the `mgmt` network IP (the
        # network isitup itself is on). Fall back to the first listed IP
        # only if the container isn't on `mgmt`. The single-IP case
        # works identically — the mgmt-prefer query just returns nothing
        # and we drop to the fallback. Retry once on an empty IP to ride out
        # a momentary docker daemon/socket blip before declaring the probe
        # unavailable.
        for attempt in 1 2; do
            ip=$(docker inspect "$container" -f '{{with index .NetworkSettings.Networks "mgmt"}}{{.IPAddress}}{{end}}' 2>/dev/null || true)
            if [[ -z "$ip" ]]; then
                ip=$(docker inspect "$container" -f '{{range .NetworkSettings.Networks}}{{.IPAddress}} {{end}}' 2>/dev/null | awk '{print $1}' || true)
            fi
            [[ -n "$ip" ]] && break
            [[ "$attempt" == 1 ]] && sleep 1
        done
        if [[ -n "$ip" ]]; then
            # `|| true` instead of `|| echo "000"` — curl's
            # `-w '%{http_code}'` prints "000" on connection failure
            # regardless of exit code, so adding `echo "000"` on
            # non-zero rc concatenates to "000000". The fallback below
            # handles the empty-output case. NB: a 000 here (valid container
            # IP, connection refused/timed out) IS treated as down — the
            # container is known-present but its port isn't answering, a real
            # service fault, not a probe-infra failure.
            code=$(curl -s --connect-timeout 5 --max-time 10 \
                -o /dev/null -w '%{http_code}' "http://${ip}:${port}${health_path}" 2>/dev/null || true)
        else
            # No container IP. TWO different faults produce this identical
            # signal and the old code called both "probe unavailable":
            #
            #   a) the docker socket/CLI is gone — the checker cannot measure
            #      anything, so unknown is right; and
            #   b) the daemon is fine and the container SIMPLY DOES NOT EXIST
            #      — never deployed, or removed. That is a REAL fault about
            #      the service, and grading it unknown hides it: the SPA
            #      renders unknown grey, excludes it from the degraded count
            #      and from uptime history, so an undeployed service reads as
            #      an all-green board forever.
            #
            # (b) is the common case: a service declared in isitup_services but never
            # deployed reports "probe unavailable" indefinitely, and a mistyped container
            # name reads the same way. Both have to be graded here, not row by row.
            #
            # `docker info` separates them: it succeeds iff the daemon is
            # reachable. Memoized — this only runs when a container is missing,
            # and the answer cannot change mid-tick in a way we'd act on.
            if docker_daemon_reachable; then
                # Daemon up, container absent. Down, and say which container,
                # because the actionable fact is the NAME — a typo in the
                # isitup_services row and a genuinely undeployed service look
                # identical on the page otherwise.
                emit "$id" "$name" "$category" "down" "container '${container}' does not exist" \
                    "$url" "$kind" "$access" "$icon" "$desc"
                return
            fi
            probe_unavailable=true
            code="000"
        fi
    else
        # Fallback path — Traefik-routed services. For mgmt-host stacks
        # the recommended posture is type=container so traffic doesn't
        # round-trip through Traefik's authn middleware on the health
        # probe.
        code=$(curl -sk --connect-timeout 5 --max-time 10 \
            -o /dev/null -w '%{http_code}' "https://${host}${health_path}" 2>/dev/null || true)
        # No HTTP response at all (000) on the Traefik path can't tell
        # "service down" from "couldn't reach the probe path" — treat it as
        # unknown rather than a false-red.
        [[ "$code" =~ ^[0-9]{3}$ && "$code" != "000" ]] || probe_unavailable=true
    fi
    # Anything that didn't yield a 3-digit code becomes "000".
    [[ "$code" =~ ^[0-9]{3}$ ]] || code="000"

    # A declared service whose hostname does not resolve is the F-197 class:
    # declared-but-unreachable-by-name, i.e. deployed dark. The container probe
    # above deliberately bypasses DNS (it dials the container IP directly), so
    # a service with a broken name shows a cheerful green here while no browser
    # can reach it. A service can be undeployed and NXDOMAIN in the site CoreDNS
    # zone at once and still read as fine without this check.
    #
    # Its own state, not down: the process may be perfectly healthy, and it is
    # not unknown either, because we DID measure something real. Mesh names resolve
    # from inside the isitup container, so this cannot paint a whole board amber on
    # a goatnet where the zone is fine.
    if [[ -n "$host" ]] && ! getent hosts "$host" >/dev/null 2>&1; then
        emit "$id" "$name" "$category" "degraded" "declared but ${host} does not resolve" \
            "$url" "$kind" "$access" "$icon" "$desc"
        return
    fi

    # FRONT-DOOR check. The container probe above dials the container
    # IP directly, and the DNS check above only asks whether the name resolves.
    # Neither notices the case in between: the name resolves, the container is
    # healthy, and the PUBLIC HOSTNAME serves nothing because no Traefik router
    # matched it. The row then shows green next to an FQDN no browser can use.
    #
    # A portal router that references a middleware its host's config does not
    # define is the common way to get here: Traefik puts the router in error state
    # and answers 404 to every caller, including every bundle download link in every
    # welcome email, while the container behind it stays perfectly healthy. Without
    # this probe isitup reports that as up.
    #
    # DELIBERATELY NARROW, so this cannot paint the board red for normal
    # reasons. Only three front-door answers count as a fault:
    #   404 — no router matched, or the router is in error state
    #   502/503 — a router matched but its service is unreachable
    #   000 — nothing answered on :443 at all
    # Everything else, INCLUDING 401/403/30x, is treated as fine: those mean
    # routing works and an auth middleware did its job, which is exactly why
    # type=container is the recommended posture in the first place.
    #
    # Graded DEGRADED, never down: the service itself is up and something
    # real was measured. The operator needs to know the door is shut, not to
    # be told the process died.
    #
    # OPT-OUT. A row may declare that its FQDN is not a front door at
    # all. Some services are deliberately reachable ONLY over the container
    # network: the zenoh-router row is probed on its REST plugin at :8000 and
    # no Traefik router is ever declared for the `zenoh.<site>` name, so that
    # name 404s BY DESIGN on every goatnet. Probing it grades a perfectly
    # healthy router degraded forever — which is exactly the standing-amber rot
    # this file's own honesty grading exists to remove, and exactly what
    # `probe_gate: false` (ADR 1133) was already set on that row to prevent.
    # Opting out at the gate plane does not reach a front-door probe on its own, and
    # a healthy Zenoh router would then read DEGRADED.
    #
    # The declaration is `probe_front_door: false`, and it DEFAULTS to the
    # row's `probe_gate` — a row that has already said "no external prober may
    # dial this name" is saying the same thing twice otherwise. Set it
    # independently only where the two genuinely differ.
    if [[ "$type" == "container" && -n "$host" && "$probe_front_door" != "false" ]] && ! $probe_unavailable; then
        local front_code
        front_code=$(curl -sk --connect-timeout 5 --max-time 10 \
            -o /dev/null -w '%{http_code}' "https://${host}${health_path}" 2>/dev/null || true)
        [[ "$front_code" =~ ^[0-9]{3}$ ]] || front_code="000"
        case "$front_code" in
            404|502|503|000)
                emit "$id" "$name" "$category" "degraded" \
                    "container healthy, but https://${host} answers ${front_code} — public route down" \
                    "$url" "$kind" "$access" "$icon" "$desc"
                return
                ;;
        esac
    fi

    # Per-service expect_codes overrides the default 2xx/3xx. APIs that
    # answer 401/403 to anonymous probes (mgmt-API, etc) declare
    # `expect_codes: "2|3|401|403"` in isitup_services to count those
    # as "up". Default is `2|3` (HTTP code starts with 2 or 3).
    # Three outcomes remain at this point — the container-absent and
    # name-unresolvable cases already returned above (see the file header for
    # the full four-state grading):
    #   unknown — the probe couldn't run (no measurement)
    #   up      — HTTP code matched expect_codes
    #   down    — reached the service, got a non-matching real code
    local up_re="^(${expect_codes})"
    if $probe_unavailable; then
        emit "$id" "$name" "$category" "unknown" "probe unavailable" "$url" "$kind" "$access" "$icon" "$desc"
    elif [[ "$code" =~ $up_re ]]; then
        emit "$id" "$name" "$category" "up" "HTTP $code" "$url" "$kind" "$access" "$icon" "$desc"
    else
        emit "$id" "$name" "$category" "down" "HTTP $code" "$url" "$kind" "$access" "$icon" "$desc"
    fi
}

if $VERBOSE; then echo "Checking services at $TIMESTAMP"; fi

if [[ -f "$SERVICES_ENV" ]]; then
    # shellcheck disable=SC1090
    source "$SERVICES_ENV"
    for entry in "${APP_SERVICES[@]}"; do
        # NB: expect_codes is the LAST var so it absorbs its own internal
        # pipes ("2|3", "2|3|401|403"). category + icon + desc are positioned
        # BEFORE expect_codes — inserting a column after expect_codes would
        # split the value across fields. See the services.env.j2 header.
        # category drives the page section; icon
        # selects the per-row glyph (see index.html ICONS map); desc is the
        # one-sentence consequence line under the row name, and a `|` in it
        # would shift every field after it, which is why the template header
        # forbids one; link_path is where the row's LINK points, distinct from
        # health_path, which is where the PROBE goes.
        #
        # The column set is DECLARED by the file (APP_SERVICES_FORMAT), never
        # guessed from the data. This file and check.sh are separate
        # bind-mounts written by separate tasks, so an aborted play can leave a
        # new services.env beside the previous check.sh and the next cron tick
        # reads that pair. Inferring the shape from field contents
        # fails on the one case that matters, a `desc` that happens to look
        # like an expect_codes token, and a wrong guess here silently
        # mis-grades every row.
        if [[ "${APP_SERVICES_FORMAT:-1}" == "3" ]]; then
            IFS='|' read -r id name host type container port health_path kind access category icon desc link_path probe_front_door expect_codes <<< "$entry"
        elif [[ "${APP_SERVICES_FORMAT:-1}" == "2" ]]; then
            IFS='|' read -r id name host type container port health_path kind access category icon desc link_path expect_codes <<< "$entry"
            probe_front_door="true"
        else
            # Unmarked file. Two shapes ever existed: the original
            # ...|icon|expect_codes, and the short-lived desc-only shape from
            # #2517 that never carried a marker. Read the original, then fall
            # back if what landed in expect_codes is prose rather than codes —
            # the only way those two differ, and a `desc` of nothing but digits
            # and pipes is not a sentence anyone writes.
            IFS='|' read -r id name host type container port health_path kind access category icon expect_codes <<< "$entry"
            desc=""
            link_path=""
            probe_front_door="true"
            if [[ -n "$expect_codes" && ! "$expect_codes" =~ ^[0-9]+(\|[0-9]+)*$ ]]; then
                IFS='|' read -r id name host type container port health_path kind access category icon desc expect_codes <<< "$entry"
            fi
        fi
        check_app "$id" "$name" "$host" "${type:-container}" "${container:-}" "${port:-}" "${health_path:-/}" "${kind:-ui}" "${access:-}" "${category:-app}" "${icon:-}" "${desc:-}" "${link_path:-}" "${expect_codes:-2|3}" "${probe_front_door:-true}"
    done
else
    echo "WARN: $SERVICES_ENV missing — nothing to check" >&2
fi

SERVICES_JSON+="]"

printf '{"timestamp":"%s","services":%s}\n' "$TIMESTAMP" "$SERVICES_JSON" > "$CURRENT.tmp"
mv "$CURRENT.tmp" "$CURRENT"

python3 - "$CURRENT" "$HISTORY" "$TODAY" <<'PYEOF'
import json, sys, os
from datetime import datetime, timedelta, timezone

current_path = sys.argv[1]
history_path = sys.argv[2]
today = sys.argv[3]

with open(current_path) as f:
    current = json.load(f)

if os.path.exists(history_path):
    with open(history_path) as f:
        history = json.load(f)
else:
    history = {}

for svc in current["services"]:
    sid = svc["id"]
    status = svc["status"]
    # "unknown" = the probe couldn't run; it is the absence of a measurement,
    # not downtime, so it counts toward neither the numerator nor the
    # denominator of the uptime %.
    #
    # "degraded" DOES count, and counts against: it is a real measurement of a
    # service nobody can reach by name. Excluding it would let a
    # service sit unreachable for a month and still show 100% uptime.
    if status not in ("up", "down", "degraded"):
        continue
    if sid not in history:
        history[sid] = {}
    if today not in history[sid]:
        history[sid][today] = [0, 0]
    history[sid][today][1] += 1
    if status == "up":
        history[sid][today][0] += 1

# Trim to 31 days (today + 30 prior).
cutoff = (datetime.now(timezone.utc) - timedelta(days=31)).strftime("%Y-%m-%d")
for sid in list(history.keys()):
    history[sid] = {d: v for d, v in history[sid].items() if d >= cutoff}

tmp = history_path + ".tmp"
with open(tmp, "w") as f:
    json.dump(history, f, separators=(",", ":"))
os.rename(tmp, history_path)
PYEOF

if $VERBOSE; then echo "Done. Results written to $DATA_DIR"; fi
