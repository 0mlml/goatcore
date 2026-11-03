#!/usr/bin/env bash
# zenoh-health-probe.sh — the site host self-publishes its LOCAL zenoh ROUTER's
# service health to the relay-health ingest(s), so the relay dashboard gains a
# "zenoh router" band with the same drill-down grammar as the site-host band.
#
# WHY THIS EXISTS: the router can wedge in ways the container
# HEALTHCHECK can't see — it tests REST liveness only, but the transport accept
# path can seize (new-session opens stall to the 10s open_timeout while REST and
# established sessions keep flowing; CLOSE-WAIT climbs unbounded). The ONE signal
# that catches the seizure is a real mTLS new-session OPEN, and the trend that
# catches these early is CLOSE-WAIT / FD / open-latency over
# TIME. This probe measures exactly those, from the site host, and POSTs them to the
# ingest — which STORES the history in its own tiered sqlite, so it survives the
# router restarting (the store lives in a different container entirely). That
# durable-across-restart history is the whole point.
#
# VANTAGE: the probe runs ON the site host (co-located with the router) and reads the
# container's netns/proc + the router's own REST adminspace. It is therefore a
# site-host-vantage aspect — the laptop outage view (which probes relay PUBLIC IPs and
# cannot reach the router) renders this band absent, honestly. Every number is
# grounded or rendered "—"; nothing is fabricated.
#
# Runs as ROOT (systemd timer) — needs docker inspect, nsenter into the router
# netns, and /proc/<pid>/fd. Same pattern as ops/compose/goat-obs/textfile-cron.
#
# Payload: {"published_at":<unix>,"zenoh":{...}} — POSTed to /zenoh-health-ingest/
# <router-name>, bearer-gated (same trust domain as relay/host metrics; the token
# already lives on the site host). The ingest validates shape + token and stores it.
#
# Config via env (set by the systemd unit / zenoh-health-publish.env):
#   ZENOH_ROUTER_NAME        logical dashboard name for this router (e.g. site-a), required
#   ZENOH_ROUTER_CONTAINER   docker container name (e.g. zenoh-router), required
#   ZENOH_GOATNET            goatnet label (for example site-a), required
#   ZENOH_INGEST_URLS        comma-separated ingest bases, required
#                            e.g. https://relay-health.example.net/zenoh-health-ingest
#   RELAY_METRICS_BEARER_TOKEN  shared bearer token (required; same as relay/host metrics)
#   ZENOH_VANTAGE            honest vantage label (default: this host's hostname)
#   ZENOH_INTERVAL_S         declared publish interval (default 30; dashboard grades
#                            staleness at 3x this, so declare honestly)
#   ZENOH_REST_PORT          in-container REST adminspace port (default 8000)
#   ZENOH_TPORT              in-container transport TLS port (default 7447)
#   ZENOH_QUIC_PORT          in-container transport QUIC port (default 7448)
#   ZENOH_CLIENT_CERT        mTLS client leaf for the new-session OPEN probe (optional;
#   ZENOH_CLIENT_KEY         absent -> open latency renders "unmeasured", the rest works)
#   ZENOH_FIPS_ATTEST        path to the router FIPS attestation file (optional; role-written)
#   ZENOH_STORAGE_DIR        host path to the rocksdb storages dir (optional; enables
#                            storage count + bytes)
#   ZENOH_ROUTER_PEER_ALLOW  comma ZIDs of DECLARED router/peer-mode remote sessions
#                            (anything else counts toward remote_router_sessions, F-287)
#   ZENOH_FABRIC_WIDE_EXPECTED  how many whole-keyspace subscribers this goatnet
#                            has GRANTED (size of zenoh_fabric_wide_read). More
#                            live ones than that = a new one appeared.
#   ZENOH_PROM_OUT           write goat_zenoh_* Prometheus textfile metrics HERE,
#                            measured by this probe (optional; see "textfile" below)
#   WEDGE_WATCH_LOG          wedge-watch log, for restarts_24h (default /var/log/wedge-watch.log)
#   HIST_KEEP                rolling ring samples for the band sparkline (default 60)
#   STATE_DIR                rate/ring cache (default /var/lib/zenoh-health-publish)
set -uo pipefail

NAME="${ZENOH_ROUTER_NAME:?ZENOH_ROUTER_NAME required}"
CT="${ZENOH_ROUTER_CONTAINER:?ZENOH_ROUTER_CONTAINER required}"
GOATNET="${ZENOH_GOATNET:?ZENOH_GOATNET required}"
INGEST_URLS="${ZENOH_INGEST_URLS:?ZENOH_INGEST_URLS required}"
TOKEN="${RELAY_METRICS_BEARER_TOKEN:?RELAY_METRICS_BEARER_TOKEN required}"
VANTAGE="${ZENOH_VANTAGE:-$(hostname -s 2>/dev/null || hostname)}"
INTERVAL_S="${ZENOH_INTERVAL_S:-30}"
REST_PORT="${ZENOH_REST_PORT:-8000}"
TPORT="${ZENOH_TPORT:-7447}"
QUIC_PORT="${ZENOH_QUIC_PORT:-7448}"
FIPS_ATTEST="${ZENOH_FIPS_ATTEST:-}"
STORAGE_DIR="${ZENOH_STORAGE_DIR:-}"
HIST_KEEP="${HIST_KEEP:-60}"
STATE_DIR="${STATE_DIR:-/var/lib/zenoh-health-publish}"
mkdir -p "$STATE_DIR"
now=$(date +%s)

# JSON helpers: numbers stay bare, absent reads become null — the dashboard
# renders null as an honest "—", never a fabricated zero.
num(){ [ -n "${1:-}" ] && printf '%s' "$1" || printf 'null'; }
jstr(){ [ -n "${1:-}" ] && printf '"%s"' "$1" || printf 'null'; }
jbool(){ case "${1:-}" in true) printf 'true';; false) printf 'false';; *) printf 'null';; esac; }
# docker-stats memory string ("782MiB" / "4GiB" / "1.5GB") -> integer MiB, or empty.
_mib(){ printf '%s' "${1:-}" | awk '{
  v=$0; u="MiB"; if(match(v,/[A-Za-z]+$/)){u=substr(v,RSTART);v=substr(v,1,RSTART-1)}
  if(v=="") exit
  m=1; if(u~/^GiB/||u~/^GB/)m=1024; else if(u~/^KiB/||u~/^kB/)m=1/1024; else if(u~/^B/)m=1/1048576
  printf "%d", v*m }'; }

command -v docker >/dev/null 2>&1 || { echo "zenoh-health-probe: docker not found" >&2; exit 3; }

# --- container liveness / identity -------------------------------------------
# One inspect, tab-separated, so a stopped/absent container degrades gracefully
# (every field falls to null and the dashboard grades it red/grey honestly).
running=""; cstatus=""; health=""; restarts=""; started=""; oom=""; image=""; digest=""; pid=""; logpath=""
insp=$(docker inspect "$CT" \
  --format '{{.State.Running}}	{{.State.Status}}	{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}	{{.RestartCount}}	{{.State.StartedAt}}	{{.State.OOMKilled}}	{{.Config.Image}}	{{.Image}}	{{.State.Pid}}	{{.LogPath}}' 2>/dev/null)
if [ -n "$insp" ]; then
  IFS=$'\t' read -r running cstatus health restarts started oom image digest pid logpath <<< "$insp"
fi
# uptime since StartedAt (RFC3339 -> epoch); null if unparseable
uptime_s=""
if [ -n "${started:-}" ] && [ "$started" != "0001-01-01T00:00:00Z" ]; then
  st_epoch=$(date -d "$started" +%s 2>/dev/null || echo "")
  [ -n "$st_epoch" ] && uptime_s=$(( now - st_epoch ))
fi
# image variant (stock / pqc / pqc-fips) from the image ref — the F-253 fix rides
# the pqc/pqc-fips images; a router still on bare eclipse/zenoh carries the wedge.
variant="unknown"
case "$image" in
  *zenoh-pqc-fips*) variant="pqc-fips";;
  *zenoh-pqc*)      variant="pqc";;
  *eclipse/zenoh*|*zenoh:*) variant="stock";;
esac
digest_short=""
[ -n "${digest:-}" ] && digest_short=$(printf '%s' "$digest" | sed 's/^sha256://' | cut -c1-12)

# --- everything below needs a live PID (netns / proc / probes) ---------------
fd_used=""; fd_cap=""; close_wait=""; time_wait=""; estab=""; estab_tls=""
tls_bytes_rx=""; tls_bytes_tx=""; quic_dgram_rx=""; quic_dgram_tx=""; quic_listen=""
tls_bps_rx="null"; tls_bps_tx="null"; quic_dps_rx="null"; quic_dps_tx="null"
cpu_pct=""; mem_mb=""; mem_lim_mb=""; rest_ms=""; rest_ok=""; version=""
open_ms=""; open_ok=""; open_reason="not_configured"; kx_group=""
if [ -n "${pid:-}" ] && [ "$pid" != "0" ] && [ "${running:-}" = "true" ]; then
  # File descriptors: current vs the nofile soft limit (F-248 was FD exhaustion;
  # ~6200 of the baseline is the structural rocksdb set — emit both, never a %).
  fd_used=$(ls "/proc/$pid/fd" 2>/dev/null | wc -l | tr -d ' ')
  fd_cap=$(awk '/Max open files/{print $4}' "/proc/$pid/limits" 2>/dev/null)

  # Socket census INSIDE the container netns (the host netns can't see them).
  # CLOSE-WAIT is the F-253 leading indicator: 0 healthy, climbing = the wedge
  # building. Split ESTAB by transport port (TLS 7447 vs QUIC 7448).
  ss_all=$(nsenter -t "$pid" -n ss -tan 2>/dev/null)
  if [ -n "$ss_all" ]; then
    close_wait=$(printf '%s\n' "$ss_all" | awk 'NR>1 && $1=="CLOSE-WAIT"' | wc -l | tr -d ' ')
    time_wait=$(printf '%s\n' "$ss_all"  | awk 'NR>1 && $1=="TIME-WAIT"'  | wc -l | tr -d ' ')
    estab=$(printf '%s\n' "$ss_all"      | awk 'NR>1 && $1=="ESTAB"'      | wc -l | tr -d ' ')
    estab_tls=$(printf '%s\n' "$ss_all"  | awk -v p=":$TPORT" 'NR>1 && $1=="ESTAB" && index($4,p)' | wc -l | tr -d ' ')
  fi

  # --- transport split: TLS (TCP) bytes vs QUIC (UDP) datagrams ---------------
  # Zenoh 1.9.0 exposes NO per-transport message counters (the no-metrics-plugin
  # consequence), and QUIC multiplexes every flow over a SINGLE demuxed UDP socket
  # on :QUIC_PORT — so QUIC "sessions" are not socket-countable. The honest,
  # transport-level signal: TLS byte counters (summed over the :TPORT sessions via
  # `ss -ti`) and UDP datagram counters (the router netns carries no significant
  # UDP other than QUIC). Cumulative counters → rates are diffed vs the last sample.
  ss_ti=$(nsenter -t "$pid" -n ss -tin 2>/dev/null)
  if [ -n "$ss_ti" ]; then
    tls_bytes_tx=$(printf '%s\n' "$ss_ti" | grep -A1 ":$TPORT" | grep -oE 'bytes_sent:[0-9]+'     | awk -F: '{s+=$2} END{print s+0}')
    tls_bytes_rx=$(printf '%s\n' "$ss_ti" | grep -A1 ":$TPORT" | grep -oE 'bytes_received:[0-9]+' | awk -F: '{s+=$2} END{print s+0}')
  fi
  # /proc/net/snmp Udp line — the values row (header row's $2 is non-numeric):
  #   Udp: InDatagrams NoPorts InErrors OutDatagrams ...
  read -r quic_dgram_rx quic_dgram_tx < <(nsenter -t "$pid" -n cat /proc/net/snmp 2>/dev/null \
    | awk '/^Udp:/ && $2 ~ /^[0-9]+$/ {print $2, $5}')
  # Is the QUIC transport even enabled? (a UDP listener on :QUIC_PORT). If not,
  # the datagram counters aren't QUIC and the band says so honestly.
  if nsenter -t "$pid" -n ss -uln 2>/dev/null | grep -q ":$QUIC_PORT "; then quic_listen="true"; else quic_listen="false"; fi

  # CPU / memory from docker stats (one-shot). mem in MiB; the router runs under a
  # 4G bulkhead (prior OOM), so surface both used + limit, never just a %.
  stats=$(docker stats --no-stream --format '{{.CPUPerc}}	{{.MemUsage}}' "$CT" 2>/dev/null)
  if [ -n "$stats" ]; then
    cpu_pct=$(printf '%s' "$stats" | awk -F'\t' '{gsub(/%/,"",$1); print $1}')
    memraw=$(printf '%s' "$stats" | awk -F'\t' '{print $2}')   # e.g. "782MiB / 4GiB"
    mem_mb=$(_mib "$(printf '%s' "$memraw" | awk '{print $1}')")
    mem_lim_mb=$(_mib "$(printf '%s' "$memraw" | awk '{print $3}')")
  fi

  # The UNRECLAIMABLE half, read from the cgroup rather than docker stats.
  #
  # `docker stats` reports a working set that still includes active page cache,
  # which the kernel drops under pressure. On a router holding hundreds of
  # rocksdb instances that cache is GiBs and sits near the limit as a matter of
  # course: most of the reading can be cache, right at the cap. Alerting on that
  # number cries wolf every day, and an
  # alert that cries wolf is one somebody switches off.
  #
  # anon + swap is what the kernel CANNOT reclaim, so it is the number that
  # decides whether the next allocation is an OOM kill.
  mem_anon_b=""; mem_swap_b=""
  _cg=$(awk -F: '/^0::/{print $3}' "/proc/$pid/cgroup" 2>/dev/null)
  if [ -n "${_cg:-}" ] && [ -r "/sys/fs/cgroup${_cg}/memory.stat" ]; then
    mem_anon_b=$(awk '/^anon /{print $2}' "/sys/fs/cgroup${_cg}/memory.stat" 2>/dev/null)
    mem_swap_b=$(cat "/sys/fs/cgroup${_cg}/memory.swap.current" 2>/dev/null || true)
  fi

  # REST adminspace latency + router version. The healthcheck hits this exact
  # endpoint on 127.0.0.1 inside the container; REST seizes in step with the
  # transport wedge (shared single-thread runtime), so its latency is a real
  # signal. curl runs in the router netns so it reaches the in-container bind.
  rt0=$(date +%s%3N)
  rbody=$(nsenter -t "$pid" -n curl -s -m 12 "http://127.0.0.1:${REST_PORT}/@/local/router" 2>/dev/null)
  rc=$?
  rt1=$(date +%s%3N)
  if [ "$rc" = "0" ] && [ -n "$rbody" ]; then
    rest_ok="true"; rest_ms=$(( rt1 - rt0 ))
    version=$(printf '%s' "$rbody" | grep -oE '"version"[[:space:]]*:[[:space:]]*"[^"]+"' | head -1 | sed -E 's/.*"version"[^"]*"([^"]+)".*/\1/')
  else
    rest_ok="false"; rest_ms=$(( rt1 - rt0 ))
  fi
fi

# --- TCP/TLS admission proxy -----------------------------------------
# Times a TCP connection and TLS handshake with a client leaf. This exercises
# the accept path, not a Zenoh application session OPEN. In particular, TLS 1.3
# can return from the client handshake before a server's client-auth rejection
# is read. A fast result therefore does not prove application admission.
# Local certificate/key errors are unmeasured, not failed router connections.
if [ -n "${ZENOH_CLIENT_CERT:-}" ]; then
  open_reason="probe_error"
  if [ -n "${pid:-}" ] && [ "${running:-}" = "true" ] \
     && command -v python3 >/dev/null 2>&1; then
    # Probe the router's OWN listen address inside its netns (transport binds the
    # container IP, not loopback). Discover it from the container network config.
    cip=$(docker inspect "$CT" --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{"\n"}}{{end}}' 2>/dev/null | grep -m1 -E '^[0-9]')
    [ -z "$cip" ] && cip="127.0.0.1"
    openout=$(nsenter -t "$pid" -n env \
      ZH_CERT="$ZENOH_CLIENT_CERT" ZH_KEY="${ZENOH_CLIENT_KEY:-$ZENOH_CLIENT_CERT}" \
      python3 - "$cip" "$TPORT" <<'PY' 2>/dev/null
import os, socket, ssl, sys, time
host, port = sys.argv[1], int(sys.argv[2])
try:
    ctx = ssl.create_default_context()
    ctx.check_hostname = False
    ctx.verify_mode = ssl.CERT_NONE
    ctx.load_cert_chain(os.environ["ZH_CERT"], os.environ["ZH_KEY"])
except Exception:
    print("UNMEASURED null client_config_error")
    sys.exit(0)
t0 = time.monotonic()
def failed(reason):
    print(f"FAIL {int((time.monotonic() - t0) * 1000)} {reason}")
try:
    with socket.create_connection((host, port), timeout=12) as raw:
        with ctx.wrap_socket(raw, server_hostname="zenoh-health-probe"):
            print(f"OK {int((time.monotonic() - t0) * 1000)} ok")
except TimeoutError:
    failed("timeout")
except ConnectionRefusedError:
    failed("connection_refused")
except (ssl.SSLEOFError, ssl.SSLZeroReturnError):
    # A closed/truncated transport is not evidence of rejected client auth.
    failed("connection_error")
except ssl.SSLError as error:
    # Only explicit certificate/auth alerts get the TLS-rejection page exemption.
    # Unknown SSL/protocol failures must retain immediate transport detection.
    auth_alerts = {
        "SSLV3_ALERT_BAD_CERTIFICATE", "SSLV3_ALERT_CERTIFICATE_EXPIRED",
        "SSLV3_ALERT_CERTIFICATE_REVOKED", "SSLV3_ALERT_CERTIFICATE_UNKNOWN",
        "SSLV3_ALERT_NO_CERTIFICATE", "SSLV3_ALERT_UNSUPPORTED_CERTIFICATE",
        "TLSV13_ALERT_CERTIFICATE_REQUIRED", "TLSV1_ALERT_ACCESS_DENIED",
        "TLSV1_ALERT_UNKNOWN_CA",
    }
    failed("tls_error" if getattr(error, "reason", None) in auth_alerts else "connection_error")
except PermissionError:
    print("UNMEASURED null probe_error")
except OSError:
    failed("connection_error")
except Exception:
    print("UNMEASURED null probe_error")
PY
    )
    if [ -n "$openout" ]; then
      read -r probe_result probe_ms probe_reason probe_extra <<< "$openout"
      if [ -z "${probe_extra:-}" ]; then
        case "$probe_result:$probe_reason" in
          OK:ok|FAIL:timeout|FAIL:connection_refused|FAIL:connection_error|FAIL:tls_error)
            case "$probe_ms" in
              ''|*[!0-9]*|0[0-9]*) ;; # malformed helper output stays unmeasured
              *) open_ms="$probe_ms"; open_reason="$probe_reason"
                 case "$probe_result" in OK) open_ok="true";; FAIL) open_ok="false";; esac ;;
            esac ;;
          UNMEASURED:client_config_error|UNMEASURED:probe_error)
            [ "$probe_ms" = "null" ] && open_reason="$probe_reason" ;;
        esac
      fi
    fi
  fi
fi

# --- delivery-stall signals ---------------------------------
# The slow-consumer stall is invisible to everything above: REST, healthcheck
# and new-session opens all stay green while one stuck subscriber blocks the
# router's receive threads. It shows only in the router log (transport closes,
# orphaned replies) and in the session census (router-mode remote peers).
# zenoh-router-signals.py tails the log by byte offset and returns cumulative
# counters; null when it could not read, never a made-up zero.
delivery="null"
signals_py="$(dirname "$0")/zenoh-router-signals.py"
if [ -f "$signals_py" ] && command -v python3 >/dev/null 2>&1; then
  localnet=$(docker inspect "$CT" --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}/{{.IPPrefixLen}}{{"\n"}}{{end}}' 2>/dev/null | grep -m1 -E '^[0-9]')
  # Subscriber census, for the whole-keyspace detector. Cheap and read-only (a
  # REST GET the router answers from its own adminspace); skipped silently if
  # the router is not answering, which the REST probe above already graded.
  census_file=""
  if [ "${rest_ok:-}" = "true" ] && [ -n "${pid:-}" ]; then
    census_file="$STATE_DIR/subscriber-census.json"
    nsenter -t "$pid" -n curl -s -m 15 \
      "http://127.0.0.1:${REST_PORT}/@/*/router/subscriber/**" \
      > "${census_file}.tmp" 2>/dev/null \
      && mv "${census_file}.tmp" "$census_file" \
      || { rm -f "${census_file}.tmp"; census_file=""; }
  fi
  delivery=$(printf '%s' "${rbody:-}" | python3 "$signals_py" \
    --log-path "${logpath:-}" --state-dir "$STATE_DIR" \
    --allow-router-zids "${ZENOH_ROUTER_PEER_ALLOW:-}" \
    ${localnet:+--local-net "$localnet"} \
    ${census_file:+--subscriber-census "$census_file"} 2>/dev/null)
  [ -z "$delivery" ] && delivery="null"
fi

# --- crypto posture (best-effort, honest) ------------------------------------
# Router version came from REST above. FIPS attestation: the goat-zenoh role
# writes a marker file the crypto-posture publisher also reads; surface presence
# + freshness. KX group is left to the crypto domain (goat health --domain crypto)
# — not re-probed here to avoid a PQ-openssl dependency; null is honest.
attest_present=""; attest_fips=""; attest_age_s=""
if [ -n "$FIPS_ATTEST" ] && [ -f "$FIPS_ATTEST" ]; then
  attest_present="true"
  grep -qiE 'fips|module' "$FIPS_ATTEST" 2>/dev/null && attest_fips="true" || attest_fips="false"
  a_mtime=$(stat -c %Y "$FIPS_ATTEST" 2>/dev/null || echo "")
  [ -n "$a_mtime" ] && attest_age_s=$(( now - a_mtime ))
else
  [ -n "$FIPS_ATTEST" ] && attest_present="false"
fi

# --- storage / persistence (optional; needs the storages dir) ----------------
# One rocksdb dir per slot (the ~5000-storage load is the "scale smell", F-248).
storages=""; rocksdb_mb=""
if [ -n "$STORAGE_DIR" ] && [ -d "$STORAGE_DIR" ]; then
  storages=$(find "$STORAGE_DIR" -mindepth 1 -maxdepth 1 -type d 2>/dev/null | wc -l | tr -d ' ')
  rb=$(du -sm "$STORAGE_DIR" 2>/dev/null | awk '{print $1}')
  [ -n "$rb" ] && rocksdb_mb="$rb"
fi

# --- transport rates: diff the cumulative counters vs the last sample --------
# Same pattern as host-net-publish: cache prev in STATE_DIR, compute per-second
# rates. TLS in B/s, QUIC in datagrams/s. Absent/rollover -> null (honest "—").
xprev="$STATE_DIR/xput.prev"
if [ -f "$xprev" ] && [ -n "${tls_bytes_tx:-}" ] && [ -n "${quic_dgram_tx:-}" ]; then
  read -r pts ptx prx pqtx pqrx < "$xprev"
  dt=$(( now - pts ))
  if [ "$dt" -gt 0 ] 2>/dev/null; then
    [ "$tls_bytes_tx" -ge "$ptx" ]   2>/dev/null && tls_bps_tx=$(( (tls_bytes_tx  - ptx ) / dt ))
    [ "$tls_bytes_rx" -ge "$prx" ]   2>/dev/null && tls_bps_rx=$(( (tls_bytes_rx  - prx ) / dt ))
    [ "$quic_dgram_tx" -ge "$pqtx" ] 2>/dev/null && quic_dps_tx=$(( (quic_dgram_tx - pqtx) / dt ))
    [ "$quic_dgram_rx" -ge "$pqrx" ] 2>/dev/null && quic_dps_rx=$(( (quic_dgram_rx - pqrx) / dt ))
  fi
fi
[ -n "${tls_bytes_tx:-}" ] && echo "$now ${tls_bytes_tx:-0} ${tls_bytes_rx:-0} ${quic_dgram_tx:-0} ${quic_dgram_rx:-0}" > "$xprev"

# --- rolling ring (short window; the ingest keeps the long tiers) ------------
# row = [t, open_ms, close_wait, estab, fd_used] — the band sparkline source.
hfile="$STATE_DIR/ring.tsv"
echo "$now ${open_ms:-0} ${close_wait:-0} ${estab:-0} ${fd_used:-0}" >> "$hfile"
tail -n "$HIST_KEEP" "$hfile" > "${hfile}.tmp" 2>/dev/null && mv "${hfile}.tmp" "$hfile"
hist=$(awk 'BEGIN{printf "["} {printf "%s[%s,%s,%s,%s,%s]",(NR>1?",":""),$1,$2,$3,$4,$5} END{printf "]"}' "$hfile" 2>/dev/null)
[ -z "$hist" ] && hist="[]"

payload=$(printf '{"published_at":%s,"zenoh":{"router":"%s","goatnet":"%s","vantage":"%s","interval_s":%s,
"container":{"name":"%s","running":%s,"status":%s,"health":%s,"restart_count":%s,"uptime_s":%s,"oom":%s,"image":%s,"variant":"%s","digest":%s},
"admission":{"open_ms":%s,"open_ok":%s,"open_reason":%s,"rest_ms":%s,"rest_ok":%s,"estab":%s},
"wedge":{"close_wait":%s,"time_wait":%s},
"sessions":{"estab":%s,"tls_7447":%s,"tls_bps_rx":%s,"tls_bps_tx":%s,"tls_bytes_rx":%s,"tls_bytes_tx":%s,"quic_listen":%s,"quic_dgram_rx":%s,"quic_dgram_tx":%s,"quic_dps_rx":%s,"quic_dps_tx":%s},
"capacity":{"fd_used":%s,"fd_cap":%s,"cpu_pct":%s,"mem_mb":%s,"mem_limit_mb":%s,"storages":%s},
"storage":{"storages":%s,"rocksdb_mb":%s},
"crypto":{"version":%s,"kx_group":%s,"attest_present":%s,"attest_fips":%s,"attest_age_s":%s},
"delivery":%s,
"history":%s}}' \
  "$now" "$NAME" "$GOATNET" "$VANTAGE" "$INTERVAL_S" \
  "$CT" "$(jbool "${running:-}")" "$(jstr "${cstatus:-}")" "$(jstr "${health:-}")" "$(num "${restarts:-}")" "$(num "${uptime_s:-}")" "$(jbool "${oom:-}")" "$(jstr "${image:-}")" "$variant" "$(jstr "${digest_short:-}")" \
  "$(num "${open_ms:-}")" "$(jbool "${open_ok:-}")" "$(jstr "$open_reason")" "$(num "${rest_ms:-}")" "$(jbool "${rest_ok:-}")" "$(num "${estab:-}")" \
  "$(num "${close_wait:-}")" "$(num "${time_wait:-}")" \
  "$(num "${estab:-}")" "$(num "${estab_tls:-}")" "${tls_bps_rx}" "${tls_bps_tx}" "$(num "${tls_bytes_rx:-}")" "$(num "${tls_bytes_tx:-}")" "$(jbool "${quic_listen:-}")" "$(num "${quic_dgram_rx:-}")" "$(num "${quic_dgram_tx:-}")" "${quic_dps_rx}" "${quic_dps_tx}" \
  "$(num "${fd_used:-}")" "$(num "${fd_cap:-}")" "$(num "${cpu_pct:-}")" "$(num "${mem_mb:-}")" "$(num "${mem_lim_mb:-}")" "$(num "${storages:-}")" \
  "$(num "${storages:-}")" "$(num "${rocksdb_mb:-}")" \
  "$(jstr "${version:-}")" "$(jstr "${kx_group:-}")" "$(jbool "${attest_present:-}")" "$(jbool "${attest_fips:-}")" "$(num "${attest_age_s:-}")" \
  "$delivery" \
  "$hist")

# --- Prometheus textfile, measured HERE (optional) ---------------------------
# The goat_zenoh_* alerts (rules/zenoh-wedge.yml) were fed only by
# zenoh-health-to-prom.py, which renders the SITE-HOST-LOCAL ingest's JSON. Where that
# dashboard is retired (relay_health_enabled: false) the
# bridge is deliberately off, so no goat_zenoh_* series would exist and every
# rule in zenoh-wedge.yml would be unable to fire. This writes the same
# router-health series from this probe's own measurements, plus the delivery-
# stall counters. The unit sets ZENOH_PROM_OUT only when the bridge is off, so
# the two never emit the same series.
if [ -n "${ZENOH_PROM_OUT:-}" ]; then
  wedge_log="${WEDGE_WATCH_LOG:-/var/log/wedge-watch.log}"
  restarts_24h=""
  if [ -r "$wedge_log" ]; then
    since=$(date -u -d "@$(( now - 86400 ))" +%Y-%m-%dT%H:%M:%SZ)
    restarts_24h=$(awk -v s="$since" '/AUTORESTART/ && $1 >= s' "$wedge_log" | wc -l | tr -d ' ')
  fi
  wedged=""
  case "${health:-}" in unhealthy) wedged=1;; healthy|starting) wedged=0;; esac
  mem_b=""; [ -n "${mem_mb:-}" ] && mem_b=$(( mem_mb * 1048576 ))
  meml_b=""; [ -n "${mem_lim_mb:-}" ] && meml_b=$(( mem_lim_mb * 1048576 ))
  python3 - "$ZENOH_PROM_OUT" "$NAME" "$now" "$delivery" <<PY 2>/dev/null
import json, os, sys
out, router, now, delivery = sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4]
L, seen = [], set()
def m(name, value, labels=None, help_text="", mtype="gauge"):
    if value in (None, "", "null"):
        return
    if name not in seen:
        L.append(f"# HELP {name} {help_text}"); L.append(f"# TYPE {name} {mtype}"); seen.add(name)
    lab = {"router": router, **(labels or {})}
    L.append(name + "{" + ",".join(f'{k}="{v}"' for k, v in lab.items()) + "} " + str(value))
b = lambda v: {"true": 1, "false": 0}.get(v)
m("goat_zenoh_healthy", 1 if "${health:-}" == "healthy" else 0, help_text="1 if the router container HEALTHCHECK is healthy")
m("goat_zenoh_open_ms", "${open_ms:-}", help_text="TCP/TLS admission-proxy attempt latency in ms")
m("goat_zenoh_open_ok", b("${open_ok:-}"), help_text="1 if the TCP/TLS admission proxy returned successfully; does not confirm application admission")
m("goat_zenoh_open_probe_status", 1, {"reason": "$open_reason"}, help_text="bounded result of the TCP/TLS admission proxy")
m("goat_zenoh_rest_ok", b("${rest_ok:-}"), help_text="1 if the REST adminspace answered")
m("goat_zenoh_close_wait", "${close_wait:-}", help_text="CLOSE-WAIT sockets in the router netns")
m("goat_zenoh_fd_used", "${fd_used:-}", help_text="open file descriptors")
m("goat_zenoh_fd_cap", "${fd_cap:-}", help_text="file descriptor soft limit")
m("goat_zenoh_uptime_seconds", "${uptime_s:-}", help_text="router container uptime")
m("goat_zenoh_mem_bytes", "${mem_b}", help_text="router container memory in use")
m("goat_zenoh_mem_limit_bytes", "${meml_b}", help_text="router container memory limit")
m("goat_zenoh_mem_anon_bytes", "${mem_anon_b:-}", help_text="router anonymous memory (unreclaimable; excludes page cache)")
m("goat_zenoh_mem_swap_bytes", "${mem_swap_b:-}", help_text="router memory pushed to swap (counts toward the OOM ceiling)")
m("goat_zenoh_report_timestamp_seconds", now, help_text="unix time of the last probe report (staleness canary)")
m("goat_zenoh_restarts_24h", "${restarts_24h}", help_text="wedge-watch auto-restarts of the router in 24h")
m("goat_zenoh_currently_wedged", "${wedged}", help_text="1 if the router healthcheck is failing right now")
try:
    d = json.loads(delivery)
except Exception:
    d = None
d = d if isinstance(d, dict) else {}
m("goat_zenoh_log_tail_ok", 1 if d.get("log_tail_ok") else 0, help_text="1 if the router log was readable this run (0 = delivery counters unmeasured)")
m("goat_zenoh_slow_consumer_closes_total", d.get("slow_consumer_closes_total"), mtype="counter",
  help_text="router transport closes because a subscriber could not take a non-droppable message (the delivery-stall mechanism, F-287)")
for zid, ts in sorted((d.get("recent_close_zids") or {}).items()):
    m("goat_zenoh_slow_consumer_last_close_timestamp_seconds", ts, {"zid": zid},
      help_text="last time the router closed this subscriber as a slow consumer (last hour, top 5)")
m("goat_zenoh_orphan_replies_total", d.get("orphan_replies_total"), mtype="counter",
  help_text="query replies the router could not route back (orphaned by timeouts)")
for kind, n in sorted((d.get("sessions_by_whatami") or {}).items()):
    m("goat_zenoh_sessions", n, {"whatami": kind}, help_text="router sessions by mode")
# The session table and its limit (#1922). Teardown that never finishes leaves
# linkless entries that still count toward max_sessions; at the limit the router
# refuses every new transport, logging it only at trace level.
by_whatami = d.get("sessions_by_whatami")
if isinstance(by_whatami, dict):
    m("goat_zenoh_sessions_total", sum(by_whatami.values()),
      help_text="sessions in the router's transport table (counts toward max_sessions)")
m("goat_zenoh_sessions_linkless", d.get("linkless_sessions"),
  help_text="router sessions that reference no network link (unfinished teardown, #1922)")
m("goat_zenoh_remote_router_sessions", d.get("remote_router_sessions"),
  help_text="router/peer-mode sessions from remote addresses not on the declared allow list")
# Whole-keyspace subscribers. The ACL refuses the ** spelling to peers that
# are not named, but cannot refuse the equivalent */** — so the shape is
# counted here and the alert names it. expected is the size of the goatnet's
# grandfather list: more sessions than that means a NEW one appeared.
subs = d.get("fabric_wide_subscribers")
if subs is not None:
    m("goat_zenoh_fabric_wide_subscribers", len(subs),
      help_text="sessions subscribing to the WHOLE keyspace (any all-wildcard key expression)")
    expected = "${ZENOH_FABRIC_WIDE_EXPECTED:-}"
    if expected.strip():
        m("goat_zenoh_fabric_wide_expected", expected.strip(),
          help_text="how many whole-keyspace subscribers this goatnet has granted (the named list)")
    for shape in d.get("fabric_wide_shapes") or []:
        m("goat_zenoh_fabric_wide_shape_present", 1, {"shape": shape},
          help_text="1 = this whole-keyspace spelling is in use; anything but ** is an ACL workaround")
m("goat_zenoh_prom_last_run_timestamp_seconds", now, help_text="unix time this probe last wrote its textfile (staleness guard)")
tmp = out + ".tmp"
with open(tmp, "w") as f:
    f.write("\n".join(L) + "\n")
os.replace(tmp, out)
PY
fi

# --- POST to every declared ingest. Best-effort each; exit 0 if ANY landed. --
ok=1
IFS=',' read -ra URLS <<< "$INGEST_URLS"
for u in "${URLS[@]}"; do
  u=$(printf '%s' "$u" | tr -d ' ')
  [ -z "$u" ] && continue
  code=$(curl -ksS -m 10 -o /dev/null -w '%{http_code}' \
    -H "Authorization: Bearer ${TOKEN}" -H "Content-Type: application/json" \
    -X POST --data-binary "$payload" \
    "${u}/${NAME}" 2>/dev/null)
  echo "zenoh-health-probe: ${NAME} (${CT}) -> ${u} http=${code} health=${health:-?} open=${open_ms:-?}ms cw=${close_wait:-?} estab=${estab:-?} fd=${fd_used:-?}/${fd_cap:-?}" >&2
  { [ "$code" = "200" ] || [ "$code" = "204" ]; } && ok=0
done
exit $ok
