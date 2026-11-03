#!/usr/bin/env bash
# relay-health-probe.sh — blackbox health + metrics prober for the goat relay fleet.
#
# Relays are NOT mesh/fabric peers, so the on-bus health surfaces (goat
# health / panoscope) can't see them. This probes each relay's public IP
# DIRECTLY — so it works precisely when the relays are broken and the mesh
# is unreachable. Writes relay-health.json for relay-health.html.
#
# FAST (default, no creds, ~seconds): from the prober vantage, per relay —
#   :443   enrollment TLS tunnel (Nx; reports the ok-ratio so the F-213
#          per-connection flap is visible, not hidden) + TCP-connect latency
#   :2333  rathole control-channel reachability (TCP)
#   :3478  coturn STUN binding request (real STUN, UDP)
#   :33073 the relay DATA PLANE, broken out by transport — the goat-relay
#          serves clients over BOTH WS/TCP and QUIC/UDP on the same port
#          (setup-vps.sh -p 33073/tcp + /udp; F-041 QUIC-preference race), and
#          the two fail independently (UDP/QUIC is the leg most likely to be
#          silently filtered while TCP stays up). ws_tcp = a real TCP connect;
#          quic_udp = a real QUIC Version-Negotiation handshake (RFC 9000 §6, no
#          TLS/crypto needed) so it's a genuine QUIC-liveness signal, not a bare
#          UDP send. No VN response -> reported false + rendered amber/info,
#          never a false green: can't distinguish "UDP filtered" from "VN
#          disabled" (and, like STUN, UDP egress from the site-host vantage is itself
#          unreliable, so quic_udp is informational on a tunnel-endpoint vantage
#          and health-driving only from the external laptop vantage).
#
# --deep (adds SSM service-state + metrics, needs AWS credentials,
# ~6-15s/relay): rathole-server / watchdog / coturn / goat-relay states, plus
# basic metrics — host NIC throughput (bytes/s + packets/s, rate-diffed
# across runs via a prev-sample cache), active peers (distinct PeerIds in the
# last 5m of goat-relay logs), and goat-relay CPU/mem.
#
# THROUGHPUT, ROLLING WINDOW + CAPACITY (--deep only): relays broker nothing —
# they forward opaque encrypted WireGuard packets — so the honest unit is
# relayed *traffic* (bytes/s + packets/s), not "messages". Each --deep sample
# is appended to a per-relay rolling history ring (last HIST_KEEP samples) so
# the dashboard can chart it over a window. "Capacity" is operator-DECLARED per
# relay in relays.conf (cap_mbit=, optional cap_pps=/cap_peers=) — there is no
# true line-rate to read blindly; utilization = rolling rate / declared
# capacity, and is rendered grey ("capacity not declared") when unset, never a
# fabricated %. This data exists ONLY under --deep (SSM), so the credential-
# free hosted timer shows throughput as "operator-deep only," never invented.
#
# Usage:
#   ./relay-health-probe.sh [--deep] [--conf relays.conf] [--out relay-health.json]
set -uo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CONF="${HERE}/relays.conf"
OUT="${HERE}/relay-health.json"
CACHE="${HERE}/.metrics-cache"
HIST_KEEP=40          # rolling-window samples kept per relay (~10 min at 15s)
DEEP=0
TLS_TRIES=3
# Vantage label + tunnel-endpoint flag. From the site host (the rathole tunnel
# ENDPOINT), a probe to relay:443 hairpins back through the relay's own tunnel
# to this same host — an unreliable self-referential path that real external
# clients never take. So on a tunnel-endpoint vantage the dashboard treats
# :443/STUN as informational and bases health on :2333 control reachability.
VANTAGE="${GOAT_RELAY_HEALTH_VANTAGE:-$(hostname -s 2>/dev/null || echo local)}"
TUNNEL_ENDPOINT=0
# --ingest-dir: fold in relay self-published metrics (ADR 1058). When set and
# NOT --deep, each relay's deep block comes from <dir>/<name>.json (written by
# the site ingest from the relay's own POST) — so the credential-free hosted
# timer auto-populates throughput without SSM. --deep (operator vantage) wins.
INGEST_DIR=""
INGEST_MAX_AGE="${INGEST_MAX_AGE:-3600}"   # hard cap; older -> drop to operator-deep-only
# --- Alerting (default-OFF; the prober stays pure-render unless a sink is set) ---
# When --alert-sink is given, AFTER writing the JSON the prober pushes alerts for
# the F-229 detection gap (relay disk filling / unbounded container log / a relay
# whose deep block is absent) to an Alertmanager v2 push endpoint
# (<sink>/api/v2/alerts). The disk% etc. come from a relay's deep block, which is
# populated either by --deep (operator SSM) OR --ingest-dir (the credential-free
# self-publish path, ADR 1058) — so this composes with the hosted timer with no
# AWS creds anywhere. Re-pushing each tick keeps an alert firing; when it clears
# the prober stops pushing and Alertmanager auto-resolves after resolve_timeout.
# This is the relay half of the W3 interlock (obs-resilience.md): it composes
# with — and only PAGES once — the Alertmanager receiver is un-nooped.
ALERT_SINK="${GOAT_RELAY_HEALTH_ALERT_SINK:-}"
ALERT_SITE="${GOAT_RELAY_HEALTH_SITE:-}"
DISK_WARN=85
DISK_CRIT=95
MAXLOG_WARN_MB=2048
EMIT_ONLY=0           # --emit-only: skip probing, push alerts from an existing --out JSON
while [ $# -gt 0 ]; do
  case "$1" in
    --deep) DEEP=1 ;;
    --conf) CONF="$2"; shift ;;
    --out) OUT="$2"; shift ;;
    --vantage) VANTAGE="$2"; shift ;;
    --tunnel-endpoint) TUNNEL_ENDPOINT=1 ;;
    --ingest-dir) INGEST_DIR="$2"; shift ;;
    --alert-sink) ALERT_SINK="$2"; shift ;;
    --site) ALERT_SITE="$2"; shift ;;
    --disk-warn) DISK_WARN="$2"; shift ;;
    --disk-crit) DISK_CRIT="$2"; shift ;;
    --maxlog-warn-mb) MAXLOG_WARN_MB="$2"; shift ;;
    --emit-only) EMIT_ONLY=1 ;;
    *) echo "unknown arg: $1" >&2; exit 2 ;;
  esac
  shift
done
# --emit-only implies the JSON's own deep-ness, not this run's: read it from the
# file so RelayProbeUnreachable fires iff the snapshot was a deep one.
if [ "$EMIT_ONLY" = 1 ]; then
  DEEP=$(python3 -c 'import json,sys; print(int(bool(json.load(open(sys.argv[1])).get("deep"))))' "$OUT" 2>/dev/null || echo 0)
fi
mkdir -p "$CACHE"

# --- fold a relay's self-published deep block (ingest file) if fresh ---
# Prints the deep JSON object, or "null" if missing / no deep / past the hard
# age cap. Per-panel freshness in the dashboard grades anything within the cap.
ingest_deep() {
  local f="${INGEST_DIR}/$1.json"
  [ -n "${INGEST_DIR:-}" ] && [ -f "$f" ] || { echo null; return; }
  python3 - "$f" "$INGEST_MAX_AGE" <<'PY'
import json, sys, time
try:
    d = json.load(open(sys.argv[1])); maxage = int(sys.argv[2])
    deep = d.get("deep")
    if not isinstance(deep, dict):
        print("null"); sys.exit()
    recv = d.get("received_at") or 0
    if recv and (time.time() - recv) > maxage:
        print("null"); sys.exit()           # truly stale -> operator-deep-only
    print(json.dumps(deep))
except Exception:
    print("null")
PY
}

# --- Alert emission. Default-OFF: only when --alert-sink
# is set. Reads the JSON the prober just wrote + pushes Alertmanager v2 alerts.
# Never fabricates: alerts derive only from real deep.disk_pct /
# max_container_log_mb / a relay whose deep block is absent while deep data was
# EXPECTED (--deep SSM or --ingest-dir self-publish). Best-effort — a sink
# failure logs but never fails the probe (the dashboard render must not depend on
# the pager being reachable). This is the relay half of the W3 interlock
# (obs-resilience.md): it composes with — and only PAGES once — the Alertmanager
# receiver is un-nooped. ---
emit_alerts() {
  [ -n "$ALERT_SINK" ] || return 0
  local ingest_mode=0; [ -n "${INGEST_DIR:-}" ] && ingest_mode=1
  python3 - "$OUT" "$ALERT_SINK" "$ALERT_SITE" "$VANTAGE" "$DISK_WARN" "$DISK_CRIT" "$MAXLOG_WARN_MB" "$DEEP" "$ingest_mode" <<'PY'
import json, sys, urllib.request, time
out, sink, site, vantage, dwarn, dcrit, maxlog_warn, deep, ingest_mode = sys.argv[1:10]
dwarn, dcrit, maxlog_warn, deep, ingest_mode = int(dwarn), int(dcrit), int(maxlog_warn), int(deep), int(ingest_mode)
try:
    doc = json.load(open(out))
except Exception as e:
    print(f"alert: cannot read {out}: {e}", file=sys.stderr); sys.exit(0)
now = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
relays = doc.get("relays", [])
# We treat an absent deep block as a fault only when deep data was EXPECTED:
# --deep (SSM), --ingest-dir (self-publish), or — for --emit-only against a
# snapshot — when at least one relay DID report a deep block (so a null among
# reporting peers means that relay stopped). A pure FAST snapshot never alerts.
expect_deep = bool(deep or ingest_mode or any(isinstance(r.get("deep"), dict) for r in relays))
alerts = []
def add(name, sev, relay, region, summary, desc):
    labels = {"alertname": name, "severity": sev, "component": "relay",
              "relay": relay, "instance": relay, "vantage": vantage}
    if region: labels["region"] = region
    if site:   labels["site"] = site
    alerts.append({"labels": labels, "startsAt": now,
                   "annotations": {"summary": summary, "description": desc}})
for r in relays:
    name = r.get("name", "?"); region = r.get("region", "")
    d = r.get("deep")
    if expect_deep and d is None:
        # F-229 terminal symptom: no fresh deep metrics for a relay we expected
        # to hear from. Under self-publish (ADR 1058) the relay stopped POSTing —
        # a genuine liveness loss (at 100% disk it cannot publish). Under --deep
        # the SSM probe returned nothing. A stopped instance / lost SSM reads the
        # same, so the wording stays honest.
        add("RelayProbeUnreachable", "critical", name, region,
            f"relay {name} is reporting no fresh metrics",
            f"Relay {name} ({region}) has no fresh deep metrics. Under self-publish "
            f"(ADR 1058) it stopped POSTing — a liveness loss (at 100% disk it "
            f"cannot, F-229); under --deep the SSM probe returned nothing (full "
            f"disk / stopped instance / lost SSM access). Probe vantage: {vantage}.")
        continue
    if not isinstance(d, dict):
        continue
    disk = d.get("disk_pct")
    if isinstance(disk, (int, float)) and disk > 0:
        if disk >= dcrit:
            add("RelayDiskUsageHigh", "critical", name, region,
                f"relay {name} root disk {disk}% (>= {dcrit}%)",
                f"Relay {name} ({region}) root disk at {disk}%. At 100% the relay "
                f"stops publishing/SSM dies and it flaps off the fabric. "
                f"Check the largest container log and rotate/clear it.")
        elif disk >= dwarn:
            add("RelayDiskUsageHigh", "warning", name, region,
                f"relay {name} root disk {disk}% (>= {dwarn}%)",
                f"Relay {name} ({region}) root disk at {disk}% and rising. "
                f"Investigate before it reaches {dcrit}%.")
    maxlog = d.get("max_container_log_mb")
    if isinstance(maxlog, (int, float)) and maxlog >= maxlog_warn:
        add("RelayContainerLogLarge", "warning", name, region,
            f"relay {name} container log {maxlog} MB (>= {maxlog_warn} MB)",
            f"Relay {name} ({region}) has a {maxlog} MB container log — the "
            f"F-229 leading indicator (an unbounded log fills the disk). Confirm "
            f"the log_driver cap is in place.")
if not alerts:
    print("alert: nothing to emit (all relays healthy)", file=sys.stderr); sys.exit(0)
url = sink.rstrip("/") + "/api/v2/alerts"
try:
    req = urllib.request.Request(url, data=json.dumps(alerts).encode(),
                                 headers={"Content-Type": "application/json"}, method="POST")
    with urllib.request.urlopen(req, timeout=5) as resp:
        print(f"alert: pushed {len(alerts)} alert(s) -> {url} ({resp.status})", file=sys.stderr)
except Exception as e:
    print(f"alert: push to {url} failed (best-effort): {e}", file=sys.stderr)
PY
}

# --emit-only: skip probing entirely; just push alerts from an existing --out
# JSON (decouples the credential-free render timer from a deep alert pass, and
# makes the alert logic testable without AWS/SSM).
if [ "$EMIT_ONLY" = 1 ]; then emit_alerts; exit 0; fi

# --- single TCP connect (returns 0 if port open) ---
tcp_ok() { timeout 3 bash -c "exec 3<>/dev/tcp/$1/$2" 2>/dev/null; }

# --- STUN binding request (real, UDP); prints "true"/"false" ---
stun_ok() {
python3 - "$1" "${2:-3478}" <<'PY'
import socket, struct, os, sys
host, port = sys.argv[1], int(sys.argv[2])
txid = os.urandom(12)
msg = struct.pack('>HHI', 0x0001, 0, 0x2112A442) + txid
s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM); s.settimeout(2)
ok = False
try:
    s.sendto(msg, (host, port)); data, _ = s.recvfrom(1024)
    ok = len(data) >= 20 and struct.unpack('>H', data[:2])[0] == 0x0101
except Exception:
    ok = False
finally:
    s.close()
print("true" if ok else "false")
PY
}

# --- QUIC reachability via a forced Version Negotiation; prints "<ok> <rtt_ms>" ---
# Sends a QUIC long-header packet with a reserved version (0x?a?a?a?a per RFC
# 9000 §15) padded to 1200 bytes (servers MUST NOT answer smaller datagrams),
# then checks the reply is a Version Negotiation packet (long-header bit set +
# version field == 0). No TLS handshake needed, so this is a dependency-free
# genuine QUIC-liveness signal — not a bare UDP connect (which can't tell open
# from filtered). The send->VN round-trip is timed = the QUIC/UDP-path RTT (a
# real apples-to-different-transport companion to the :443 tcp-connect RTT).
# Prints "true <ms>" / "false -1" (no reply -> false, rendered amber/info).
quic_ok() {
python3 - "$1" "${2:-33073}" <<'PY'
import socket, os, sys, time
host, port = sys.argv[1], int(sys.argv[2])
# Long header: header-form bit (0x80) + fixed bit (0x40). Reserved version
# 0x1a2a3a4a (matches 0x?a?a?a?a) makes a compliant server reply with VN.
dcid = os.urandom(8); scid = os.urandom(8)
pkt = bytes([0xC0]) + b'\x1a\x2a\x3a\x4a' + bytes([len(dcid)]) + dcid + bytes([len(scid)]) + scid
pkt = pkt + b'\x00' * (1200 - len(pkt))   # anti-amplification minimum
s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM); s.settimeout(2)
ok = False; ms = -1
try:
    t0 = time.monotonic()
    s.sendto(pkt, (host, port)); data, _ = s.recvfrom(2048)
    rtt = time.monotonic() - t0
    # Version Negotiation: long-header form bit set + 4-byte version == 0.
    ok = len(data) >= 7 and (data[0] & 0x80) != 0 and data[1:5] == b'\x00\x00\x00\x00'
    if ok: ms = int(rtt * 1000)
except Exception:
    ok = False
finally:
    s.close()
print(("true" if ok else "false") + " " + str(ms))
PY
}

# --- timed TCP connect; prints connect-RTT in ms (or -1 if refused/timeout) ---
tcp_rtt() {
python3 - "$1" "$2" <<'PY'
import socket, time, sys
host, port = sys.argv[1], int(sys.argv[2])
s = socket.socket(); s.settimeout(3)
try:
    t0 = time.monotonic(); s.connect((host, port)); print(int((time.monotonic()-t0)*1000))
except Exception:
    print(-1)
finally:
    s.close()
PY
}

# AWS invocation uses the AMBIENT session — env creds (from one outer
# `aws-vault exec`, which serve.sh does for you) or an EC2 instance profile (the
# automation path). This is the fix for the ~70-keychain-prompts-per-pass
# foot-gun: the prober NEVER calls aws-vault itself, so a whole --deep pass /
# serve.sh loop runs under ONE unlock. Set GOAT_RELAY_HEALTH_AWS_VAULT_PROFILE
# to make each call wrap in aws-vault (legacy per-call; prompts a lot — only for
# a single one-shot, never a loop).
_aws() {
  if [ -n "${GOAT_RELAY_HEALTH_AWS_VAULT_PROFILE:-}" ]; then
    aws-vault exec "$GOAT_RELAY_HEALTH_AWS_VAULT_PROFILE" --no-session -- aws "$@"
  else
    aws "$@"
  fi
}

# --- run a remote script on a relay via SSM; polls until terminal. echoes stdout ---
ssm_run() {
  local inst="$1" region="$2" script="$3" cid st tries=0
  cid=$(_aws ssm send-command --region "$region" \
        --instance-ids "$inst" --document-name AWS-RunShellScript \
        --parameters "commands=[\"$script\"]" --query 'Command.CommandId' --output text 2>/dev/null </dev/null)
  [ -z "$cid" ] && return 1
  while [ $tries -lt 12 ]; do
    sleep 3
    st=$(_aws ssm get-command-invocation --region "$region" \
         --command-id "$cid" --instance-id "$inst" --query 'Status' --output text 2>/dev/null </dev/null)
    case "$st" in Success|Failed|Cancelled|TimedOut) break ;; esac
    tries=$((tries+1))
  done
  _aws ssm get-command-invocation --region "$region" \
    --command-id "$cid" --instance-id "$inst" --query 'StandardOutputContent' --output text 2>/dev/null </dev/null
}

# --- deep SSM probe of one relay (state + metrics); prints a JSON fragment ---
deep_probe() {
  local inst="$1" region="$2" name="$3"
  local s=''
  s+='echo RH=$(systemctl is-active rathole-server.service 2>/dev/null);'
  s+='echo WD=$(systemctl is-active rathole-watchdog.timer 2>/dev/null);'
  s+='echo HEALS=$(journalctl -t rathole-watchdog --since \"-1h\" 2>/dev/null | grep -c \"threshold reached\");'
  s+='echo COTURN=$(ss -lun 2>/dev/null | grep -c \":3478\");'
  s+='echo RELAY=$(docker ps --filter name=goat-relay --filter status=running -q 2>/dev/null | head -1 | grep -c .);'
  s+='echo DP=$(ss -lun 2>/dev/null | grep -c \":33073\");'
  s+='IF=$(ip route show default 2>/dev/null | awk \"{print \\$5; exit}\");'
  s+='echo NETDEV=$(awk -v i=\"$IF:\" \"\\$1==i{print \\$2\\\",\\\"\\$3\\\",\\\"\\$10\\\",\\\"\\$11}\" /proc/net/dev);'
  s+='echo PEERS=$(docker logs --since 30m goat-relay 2>&1 | grep -oE \"PeerId\\(\\\"[^\\\"]+\\\"\\)\" | sort -u | wc -l | tr -d \" \");'
  # F-269 — per-peer AUTH REJECTIONS over a 5m window, broken out by reason, plus
  # the top offending source IPs. One laptop with a stale cached relay token can
  # be auth-rejected repeatedly from its one egress IP while every liveness
  # surface stays green — the relay KNOWS
  # (it logs every rejection) but nothing else surfaces WHICH peer. Capture the log once
  # so the three counts share one window. expired_token = stale/corrupt local
  # profile's dead token; ws_upgrade_timeout = never finished the WS upgrade;
  # ws_handoff_eof = benign QUIC-race-loser noise — surfaced, never critical.
  s+='AL=$(docker logs --since 5m goat-relay 2>&1);'
  s+='echo AREXP=$(printf \"%s\\n\" \"$AL\" | grep -cE \"validator rejected.*expired auth token\");'
  s+='echo ARWST=$(printf \"%s\\n\" \"$AL\" | grep -cE \"WS upgrade timed out\");'
  s+='echo AREOF=$(printf \"%s\\n\" \"$AL\" | grep -cE \"hand-off failed.*eof\");'
  s+='echo ARTOP=$(printf \"%s\\n\" \"$AL\" | grep -E \"validator rejected.*expired auth token\" | grep -oE \"for [0-9.]+\" | cut -d\" \" -f2 | sort | uniq -c | sort -rn | head -3 | while read c ip; do printf \"%s:%s,\" \"$ip\" \"$c\"; done);'
  s+='echo CPU=$(docker stats --no-stream --format \"{{.CPUPerc}}\" goat-relay 2>/dev/null | tr -d \"% \");'
  s+='echo MEM=$(docker stats --no-stream --format \"{{.MemPerc}}\" goat-relay 2>/dev/null | tr -d \"% \");'
  # F-229 — root-disk fill + biggest container log. An unbounded container
  # log (pre-fix per-frame flood) filled the 19 GB relay disk → SSM dead +
  # mesh flap, and NOTHING alerted. Surface disk%% + the largest container
  # log (MB) so the dashboard can warn BEFORE 100%% (at which point SSM dies
  # and the relay just drops off this prober entirely).
  s+='echo DISK=$(df --output=pcent / 2>/dev/null | tail -1 | tr -dc 0-9);'
  s+='echo MAXLOG=$(du -m /var/lib/docker/containers/*/*.log 2>/dev/null | sort -rn | head -1 | cut -f1)'
  local out; out=$(ssm_run "$inst" "$region" "$s")
  [ -z "$out" ] && { echo 'null'; return; }
  local rh wd heals coturn relay dp netdev peers cpu mem disk maxlog
  rh=$(echo "$out" | sed -n 's/^RH=//p'); wd=$(echo "$out" | sed -n 's/^WD=//p')
  heals=$(echo "$out" | sed -n 's/^HEALS=//p'); coturn=$(echo "$out" | sed -n 's/^COTURN=//p')
  relay=$(echo "$out" | sed -n 's/^RELAY=//p'); dp=$(echo "$out" | sed -n 's/^DP=//p')
  netdev=$(echo "$out" | sed -n 's/^NETDEV=//p'); peers=$(echo "$out" | sed -n 's/^PEERS=//p')
  cpu=$(echo "$out" | sed -n 's/^CPU=//p'); mem=$(echo "$out" | sed -n 's/^MEM=//p')
  disk=$(echo "$out" | sed -n 's/^DISK=//p'); maxlog=$(echo "$out" | sed -n 's/^MAXLOG=//p')

  # F-269 auth-reject counts + top offenders (parse the flat KEY= lines; build the
  # top_offenders JSON array here on the site host where bash quoting is easy).
  local arexp arwst areof artop pair pip pcnt; local ar_off_json=""; local _offs
  arexp=$(echo "$out" | sed -n 's/^AREXP=//p'); arwst=$(echo "$out" | sed -n 's/^ARWST=//p')
  areof=$(echo "$out" | sed -n 's/^AREOF=//p'); artop=$(echo "$out" | sed -n 's/^ARTOP=//p')
  case "${arexp:-}" in ''|*[!0-9]*) arexp=0 ;; esac
  case "${arwst:-}" in ''|*[!0-9]*) arwst=0 ;; esac
  case "${areof:-}" in ''|*[!0-9]*) areof=0 ;; esac
  IFS=',' read -ra _offs <<<"${artop}"
  for pair in "${_offs[@]}"; do
    [ -z "$pair" ] && continue
    pip="${pair%%:*}"; pcnt="${pair##*:}"
    case "$pip"  in ''|*[!0-9.]*) continue ;; esac
    case "$pcnt" in ''|*[!0-9]*)  continue ;; esac
    ar_off_json="${ar_off_json:+$ar_off_json,}{\"ip\":\"$pip\",\"count\":$pcnt}"
  done

  # throughput rate: diff NETDEV (rxb,rxp,txb,txp) vs the previous sample.
  local now rxb rxp txb txp rxbps txbps rxpps txpps prev pts prxb prxp ptxb ptxp dt
  now=$(date +%s); rxbps=null; txbps=null; rxpps=null; txpps=null
  IFS=',' read -r rxb rxp txb txp <<<"$netdev"
  prev="${CACHE}/${name}.prev"
  if [ -f "$prev" ] && [ -n "${rxb:-}" ]; then
    read -r pts prxb prxp ptxb ptxp < "$prev"
    dt=$((now - pts))
    if [ "$dt" -gt 0 ] 2>/dev/null && [ "$rxb" -ge "$prxb" ] 2>/dev/null; then
      rxbps=$(( (rxb - prxb) / dt )); txbps=$(( (txb - ptxb) / dt ))
      rxpps=$(( (rxp - prxp) / dt )); txpps=$(( (txp - ptxp) / dt ))
    fi
  fi
  [ -n "${rxb:-}" ] && echo "$now $rxb $rxp $txb $txp" > "$prev"

  # Rolling-window history ring: append this sample's rates (only once we have a
  # real rate — not the first, prev-less sample) and cap to HIST_KEEP. Emitted
  # as a compact [[t,rxbps,txbps,rxpps,txpps,peers],...] array the dashboard
  # charts. Lives under CACHE (gitignored); only grows under --deep.
  local hdir="${CACHE}/history" hfile hist_json
  mkdir -p "$hdir"; hfile="${hdir}/${name}.tsv"
  if [ "$rxbps" != null ]; then
    echo "$now $rxbps $txbps $rxpps $txpps ${peers:-0}" >> "$hfile"
    tail -n "$HIST_KEEP" "$hfile" > "${hfile}.tmp" 2>/dev/null && mv "${hfile}.tmp" "$hfile"
  fi
  hist_json=$(awk 'BEGIN{printf "["} {printf "%s[%s,%s,%s,%s,%s,%s]",(NR>1?",":""),$1,$2,$3,$4,$5,$6} END{printf "]"}' "$hfile" 2>/dev/null)
  [ -z "$hist_json" ] && hist_json="[]"

  printf '{"rathole":"%s","watchdog":"%s","heals_1h":%s,"coturn_listen":%s,"relay_up":%s,"dataplane_listen":%s,"peers_recent":%s,"peers_window_min":30,"cpu_pct":"%s","mem_pct":"%s","disk_pct":%s,"max_container_log_mb":%s,"rx_bps":%s,"tx_bps":%s,"rx_pps":%s,"tx_pps":%s,"auth_reject":{"expired_token":%s,"ws_upgrade_timeout":%s,"ws_handoff_eof":%s,"window_min":5,"top_offenders":[%s]},"history":%s}' \
    "${rh:-unknown}" "${wd:-unknown}" "${heals:-0}" "${coturn:-0}" "${relay:-0}" "${dp:-0}" \
    "${peers:-0}" "${cpu:-?}" "${mem:-?}" "${disk:-0}" "${maxlog:-0}" "$rxbps" "$txbps" "$rxpps" "$txpps" \
    "${arexp:-0}" "${arwst:-0}" "${areof:-0}" "$ar_off_json" "$hist_json"
}

echo "probing $(grep -cvE '^\s*#|^\s*$' "$CONF") relays (deep=$DEEP)..." >&2
RELAY_JSON=""
while read -r name ip region inst goatnet rest; do
  [ -z "${name:-}" ] && continue
  case "$name" in \#*) continue ;; esac

  # Operator-DECLARED capacity from optional trailing relays.conf tokens
  # (cap_mbit=, cap_pps=, cap_peers=, cap_src=). Never invented; unset -> null ->
  # the dashboard renders utilization grey. cap_src is the PROVENANCE of the
  # number (nic = NIC line-rate ceiling / measured = load-test high-water /
  # target = ops plan), shown so a NIC ceiling is never mistaken for the relay's
  # operational limit. cap_mbit -> bytes/s for direct comparison.
  cap_mbit=null; cap_pps=null; cap_peers=null; cap_src=null
  # Placement descriptor tokens (ADR 1080) — declared-only: anything absent or
  # malformed emits null, and the dashboard renders the relay "unplaced".
  # Coordinates are never derived from the region column.
  p_provider=null; p_az=null; p_lat=null; p_lon=null; p_place=null
  # Enrollment FQDN for the :443 probe. Defaulted below from the goatnet column
  # for back-compat; declared per-relay when a goatnet's enrollment name is not
  # netbird.<goatnet>-backbone.net (e.g. a goatnet with its own apex).
  p_enroll=""
  for tok in ${rest:-}; do
    case "$tok" in
      enroll_host=*) p_enroll="${tok#enroll_host=}" ;;
      cap_mbit=*)  cap_mbit="${tok#cap_mbit=}" ;;
      cap_pps=*)   cap_pps="${tok#cap_pps=}" ;;
      cap_peers=*) cap_peers="${tok#cap_peers=}" ;;
      cap_src=*)   cap_src="${tok#cap_src=}" ;;
      provider=*)  p_provider="${tok#provider=}" ;;
      az=*)        p_az="${tok#az=}" ;;
      geo=*)       g="${tok#geo=}"; p_lat="${g%%,*}"; p_lon="${g#*,}"
                   [ "$p_lat" = "$g" ] && { p_lat=null; p_lon=null; } ;;
      place=*)     p_place="${tok#place=}" ;;
    esac
  done
  case "$cap_mbit"  in ''|*[!0-9]*) cap_mbit=null ;; esac
  case "$cap_pps"   in ''|*[!0-9]*) cap_pps=null ;; esac
  case "$cap_peers" in ''|*[!0-9]*) cap_peers=null ;; esac
  case "$cap_src"   in ''|*[!a-zA-Z0-9_-]*) cap_src=null ;; esac
  if [ "$cap_mbit" != null ]; then cap_bps=$(( cap_mbit * 1000000 / 8 )); else cap_bps=null; fi
  if [ "$cap_src" != null ]; then cap_src_json="\"$cap_src\""; else cap_src_json=null; fi
  # geo must parse as a full decimal pair; a half-valid pair goes null,null
  case "$p_lat" in ''|null|*[!0-9.-]*) p_lat=null; p_lon=null ;; esac
  case "$p_lon" in ''|null|*[!0-9.-]*) p_lat=null; p_lon=null ;; esac
  case "$p_provider" in ''|*[!a-zA-Z0-9_-]*) p_provider=null ;; esac
  case "$p_az"       in ''|*[!a-zA-Z0-9_-]*) p_az=null ;; esac
  case "$p_place"    in ''|*[!a-zA-Z0-9._-]*) p_place=null ;; esac
  [ "$p_provider" != null ] && p_provider="\"$p_provider\""
  [ "$p_az" != null ] && p_az="\"$p_az\""
  [ "$p_place" != null ] && p_place="\"$p_place\""

  # The enrollment name this relay fronts. The <goatnet>-backbone.net default
  # is one shape and is not universal: probing a name the goatnet does not
  # own returns 000 forever, which reads on the dashboard as "the relay is
  # down" when the relay is fine and only the probe target was wrong.
  case "$p_enroll" in ''|*[!a-zA-Z0-9.-]*) p_enroll="netbird.${goatnet}-backbone.net" ;; esac
  enroll_fqdn="$p_enroll"

  # :443 enrollment TLS — N tries; ok-ratio + min TCP-connect latency (ms)
  ok443=0; code=""; tls=""; lat=-1
  for _ in $(seq 1 "$TLS_TRIES"); do
    r=$(curl -ksS -m 5 -o /dev/null -w '%{http_code}|%{time_appconnect}|%{time_connect}' \
        --resolve "${enroll_fqdn}:443:${ip}" "https://${enroll_fqdn}/" 2>/dev/null)
    c="${r%%|*}"; rest="${r#*|}"; t="${rest%%|*}"; tc="${rest##*|}"
    [ "$c" != "000" ] && [ -n "$c" ] && { ok443=$((ok443+1)); code="$c"; tls="$t"; }
    if [ -n "$tc" ]; then
      ms=$(awk -v x="$tc" 'BEGIN{printf "%d", x*1000}')
      { [ "$lat" -lt 0 ] || [ "$ms" -lt "$lat" ]; } && lat="$ms"
    fi
  done

  tcp_ok "$ip" 2333 && rh2333=true || rh2333=false
  stun=$(stun_ok "$ip" 3478)

  # :33073 data plane, broken out by transport (WS/TCP vs QUIC/UDP) + per-
  # transport RTT from this vantage: tcp-connect RTT and QUIC-handshake RTT
  # (the VN round-trip), the apples-to-different-transport companions to the
  # :443 tcp-connect latency. -1 = unreachable/no-reply.
  tcp_ok "$ip" 33073 && ws_tcp=true || ws_tcp=false
  ws_rtt=$(tcp_rtt "$ip" 33073)
  read -r quic_udp quic_rtt <<<"$(quic_ok "$ip" 33073)"
  : "${quic_udp:=false}" "${quic_rtt:=-1}"

  # deep block: --deep (operator SSM) wins; else fold in the relay's self-
  # published ingest file (ADR 1058) so the credential-free timer auto-populates.
  deep='null'
  if [ "$DEEP" = 1 ]; then
    deep=$(deep_probe "$inst" "$region" "$name")
  elif [ -n "${INGEST_DIR:-}" ]; then
    deep=$(ingest_deep "$name")
  fi

  frag=$(printf '{"name":"%s","ip":"%s","region":"%s","goatnet":"%s","enroll":{"ok":%d,"tries":%d,"code":"%s","tls_s":"%s"},"lat_ms":%d,"rathole_ctl":%s,"stun":%s,"dataplane":{"ws_tcp":%s,"ws_rtt_ms":%s,"quic_udp":%s,"quic_rtt_ms":%s},"cap":{"mbit":%s,"bps":%s,"pps":%s,"peers":%s,"src":%s},"placement":{"provider":%s,"az":%s,"lat":%s,"lon":%s,"place":%s},"deep":%s}' \
    "$name" "$ip" "$region" "$goatnet" "$ok443" "$TLS_TRIES" "$code" "$tls" "$lat" "$rh2333" "$stun" "$ws_tcp" "$ws_rtt" "$quic_udp" "$quic_rtt" "$cap_mbit" "$cap_bps" "$cap_pps" "$cap_peers" "$cap_src_json" "$p_provider" "$p_az" "$p_lat" "$p_lon" "$p_place" "$deep")
  RELAY_JSON="${RELAY_JSON:+$RELAY_JSON,}$frag"
  echo "  $name $ip  :443=$ok443/$TLS_TRIES lat=${lat}ms  :2333=$rh2333  stun=$stun  :33073 ws/tcp=$ws_tcp(${ws_rtt}ms) quic/udp=$quic_udp(${quic_rtt}ms)" >&2
done < "$CONF"

printf '{"generated":%s,"deep":%s,"vantage":"%s","tunnel_endpoint":%s,"relays":[%s]}\n' \
  "$(date +%s)" "$DEEP" "$VANTAGE" "$([ "$TUNNEL_ENDPOINT" = 1 ] && echo true || echo false)" "$RELAY_JSON" > "$OUT"
echo "wrote $OUT" >&2

emit_alerts
