#!/usr/bin/env bash
# host-net-publish.sh — a site network host (mgmt-host) self-publishes its
# HOST NETWORK HEALTH to the relay-health ingest(s).
#
# Why: a degraded site uplink (bursty loss) flaps the host's relay tunnels, and
# the mesh's own signaling retries then saturate the thin pipe, and none of that
# is visible on any dashboard, because every surface either lives behind the
# broken path or does not measure the host's uplink at all. This publisher
# pushes those metrics outbound (a single small POST rides ~2 KB — it
# works on a degraded link when nothing else does), to the CLOUD relay-health
# box (reachable when the mesh is down) and optionally the local ingest.
# The ingest STORES history, so the dashboard shows last-known state even
# while the host is dark.
#
# Payload: {"published_at":<unix>,"host_net":{...}} — see the field comments
# inline. The ingest validates shape + bearer token (same trust domain as
# relay metrics — the token already lives on mgmt-hosts for the peer-names
# resolver, so no new secret distribution).
#
# Config via env (set by the systemd unit / host-net-publish.env):
#   HOSTNET_NAME              this host's short name (e.g. mgmt-site-01), required
#   HOSTNET_GOATNET           goatnet label (for example site-a), required
#   HOSTNET_INGEST_URLS       comma-separated ingest bases, required
#                             e.g. https://relay-health.example.net/host-net-ingest
#   RELAY_METRICS_BEARER_TOKEN  shared bearer token (required; same as relay metrics)
#   HOSTNET_INTERVAL_S        declared publish interval (default 60; the dashboard
#                             grades staleness against 3x this, so declare honestly)
#   HOSTNET_PING_TARGET       loss/rtt probe target (default 1.1.1.1)
#   HOSTNET_PING_COUNT        pings per pass (default 10, 0.2s apart ≈ 2s)
#   HOSTNET_SIGNAL_CONTAINER  netbird signal container (default signal-a; "" skips)
#   HOSTNET_MGMT_CONTAINER    netbird mgmt container (default management-a; "" skips)
#   HIST_KEEP                 rolling ring samples (default 60 ≈ 1h @ 60s)
#   STATE_DIR                 rate cache + ring (default /var/lib/host-net-publish)
set -uo pipefail

NAME="${HOSTNET_NAME:?HOSTNET_NAME required}"
GOATNET="${HOSTNET_GOATNET:?HOSTNET_GOATNET required}"
INGEST_URLS="${HOSTNET_INGEST_URLS:?HOSTNET_INGEST_URLS required}"
TOKEN="${RELAY_METRICS_BEARER_TOKEN:?RELAY_METRICS_BEARER_TOKEN required}"
INTERVAL_S="${HOSTNET_INTERVAL_S:-60}"
PING_TARGET="${HOSTNET_PING_TARGET:-1.1.1.1}"
PING_COUNT="${HOSTNET_PING_COUNT:-10}"
SIGNAL_CT="${HOSTNET_SIGNAL_CONTAINER-signal-a}"
MGMT_CT="${HOSTNET_MGMT_CONTAINER-management-a}"
HIST_KEEP="${HIST_KEEP:-60}"
STATE_DIR="${STATE_DIR:-/var/lib/host-net-publish}"
mkdir -p "$STATE_DIR"
now=$(date +%s)

# JSON helpers: numbers stay bare, absent reads become null — the dashboard
# renders null as an honest "—", never a fabricated zero.
num(){ [ -n "${1:-}" ] && printf '%s' "$1" || printf 'null'; }

# Escape a value for use INSIDE JSON double quotes (the format strings below
# supply the quotes). Backslash first, then quote, then fold control chars —
# order matters or the escapes escape each other.
#
# Needed the moment the mesh management string stopped being truncated: the
# netbird failure reason embeds a gRPC dump containing literal double quotes,
# which interpolated raw would produce invalid JSON and silently break every
# publish from this host. The old truncation hid that by cutting before the
# first quote.
jstr(){
  printf '%s' "${1:-}" \
    | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g' \
    | tr -d '\000-\037'
}

# --- LAN: default-route iface, addr, gateway + gateway rtt -----------------
iface=$(ip route show default 2>/dev/null | awk '{print $5; exit}')
lan_ip=$(ip -4 -br addr show "$iface" 2>/dev/null | awk '{print $3}' | cut -d/ -f1)
gw=$(ip route show default 2>/dev/null | awk '{print $3; exit}')
gw_rtt=$(ping -c 2 -W 1 -i 0.3 "$gw" 2>/dev/null | awk -F/ '/rtt|round-trip/{print $5}')

# --- WAN: loss + rtt series to a stable anycast target ---------------------
# ~2s of probing per pass. loss_pct is THE tunnel-killer metric (loss breaks
# long-lived TCP regardless of bandwidth).
pout=$(ping -c "$PING_COUNT" -i 0.2 -W 1 "$PING_TARGET" 2>/dev/null)
loss=$(printf '%s' "$pout" | awk -F, '/packet loss/{gsub(/[^0-9.]/,"",$3); print $3}')
rtt_line=$(printf '%s' "$pout" | awk -F= '/rtt|round-trip/{print $2}')
rtt_min=$(printf '%s' "$rtt_line" | cut -d/ -f1 | tr -d ' ')
rtt_avg=$(printf '%s' "$rtt_line" | cut -d/ -f2)
rtt_max=$(printf '%s' "$rtt_line" | cut -d/ -f3)
rtt_mdev=$(printf '%s' "$rtt_line" | cut -d/ -f4 | awk '{print $1}' | tr -d 'ms ')
# Public IP: best-effort, cached — a fetch failure must not blank the field
# (the WAN can be up while the probe endpoint hiccups). Cache marks provenance.
pub_ip_f="$STATE_DIR/public-ip"
pub_ip=$(curl -s --max-time 5 https://ifconfig.me 2>/dev/null | tr -dc '0-9a-fA-F.:')
if [ -n "$pub_ip" ]; then echo "$pub_ip" > "$pub_ip_f"; else pub_ip=$(cat "$pub_ip_f" 2>/dev/null); fi

# --- iface rates: /proc/net/dev diffed vs prev sample -----------------------
read -r rxb rxp txb txp < <(awk -v i="${iface}:" '$1==i{print $2,$3,$10,$11}' /proc/net/dev 2>/dev/null)
rxbps=null; txbps=null; rxpps=null; txpps=null
prev="$STATE_DIR/ifrate.prev"
if [ -f "$prev" ] && [ -n "${rxb:-}" ]; then
  read -r pts prxb prxp ptxb ptxp < "$prev"
  dt=$((now - pts))
  if [ "$dt" -gt 0 ] 2>/dev/null && [ "$rxb" -ge "$prxb" ] 2>/dev/null; then
    rxbps=$(( (rxb - prxb) / dt )); txbps=$(( (txb - ptxb) / dt ))
    rxpps=$(( (rxp - prxp) / dt )); txpps=$(( (txp - ptxp) / dt ))
  fi
fi
[ -n "${rxb:-}" ] && echo "$now $rxb $rxp $txb $txp" > "$prev"

# PEAK over the interval (the "up to N Mbps" a speed test shows). The 60s avg
# above smears bursts away; host-net-ratesample.sh continuously samples 1s rates
# into a ring, and we report the MAX within the last interval alongside the avg.
# Read-only here — the sampler is the sole writer, so no reset race.
rxbps_peak=null; txbps_peak=null
ring="$STATE_DIR/ratesamples.tsv"
if [ -f "$ring" ]; then
  read -r rxbps_peak txbps_peak < <(awk -v cut=$((now - INTERVAL_S)) \
    '$1>=cut{if($2>mrx)mrx=$2; if($3>mtx)mtx=$3} END{printf "%d %d", mrx+0, mtx+0}' "$ring")
  [ -z "${rxbps_peak:-}" ] && { rxbps_peak=null; txbps_peak=null; }
fi

# --- rathole tunnels: unit count + error rate (the flap signal) -------------
tun_total=$(systemctl list-units --plain --no-legend 'rathole-client@*.service' 2>/dev/null | wc -l | tr -d ' ')
tun_active=$(systemctl list-units --plain --no-legend --state=active 'rathole-client@*.service' 2>/dev/null | wc -l | tr -d ' ')
# NEVER PASS A GLOB TO journalctl -u. This is the whole fix, and it is worth
# 1400x.
#
# journald can satisfy an EXACT `-u name.service` from its unit index. A PATTERN
# forces it to scan, and the scan grows with the journal — so the cost is
# unbounded and gets worse every time anything else on the host logs. Against a
# multi-gigabyte journal, same 15-minute window, same five rathole units:
#
#   glob, two passes, all fields                  about 115 s
#   glob, one pass, two fields                     about 55 s
#   explicit -u per unit, one pass, two fields      0.08 s
#
# The publisher runs on a timer, so the glob form can never finish before the
# next tick. Enumerate the units
# — `systemctl list-units` with a pattern is cheap, it is matching unit names,
# not journal entries — and hand journalctl exact names.
#
# --output-fields matters too: journald serialises two fields per entry instead
# of the whole record. Keep both; do not "simplify" either away.
#
# BEHAVIOUR FIX, deliberate and worth flagging in review: the old per-relay
# split counted EVERY entry per unit, not the error entries, despite being
# named tun_by and documented as "which tunnel is sick". So a chatty-but-healthy
# tunnel outranked a quiet-but-failing one — the opposite of the intent. The
# split below counts only WARN/ERROR entries, so it now agrees with tun_errs
# and with its own name.
_tun_uargs=$(systemctl list-units --plain --no-legend 'rathole-client@*.service' 2>/dev/null \
  | awk '{printf " -u %s", $1}')
# shellcheck disable=SC2086  # word splitting is the point: one -u per unit
_tun_json=$([ -n "$_tun_uargs" ] && journalctl $_tun_uargs --since "-15 min" --no-pager \
  -o json --output-fields=_SYSTEMD_UNIT,MESSAGE 2>/dev/null || true)
_tun_agg=$(printf '%s\n' "$_tun_json" | awk '
  /WARN|ERROR/ {
    n++
    if (match($0, /"_SYSTEMD_UNIT":"rathole-client@[a-z0-9-]+/)) {
      u = substr($0, RSTART, RLENGTH); sub(/.*@/, "", u); c[u]++
    }
  }
  END {
    printf "%d\n", n + 0
    s = ""; first = 1
    for (k in c) { s = s (first ? "" : ",") "\"" k "\":" c[k]; first = 0 }
    printf "%s\n", s
  }')
tun_errs=$(printf '%s' "$_tun_agg" | sed -n 1p)
# per-relay error split, as compact JSON ("arn":3,...) — which tunnel is sick
tun_by=$(printf '%s' "$_tun_agg" | sed -n 2p)
unset _tun_json _tun_agg _tun_uargs

# --- control plane: signal/mgmt container state + signal message rates ------
# Byte rates come from the container's OWN netns via nsenter (exact kernel
# counters; docker stats prints 3-sig-fig strings too coarse to rate-diff).
# Message rates come from the signal log: total lines + "relay miss …
# notfound" forwards-that-failed — notfound ≈ msgs means a signaling storm
# hitting streams that aren't there (the self-DDoS signature).
sig_state=null; sig_rxbps=null; sig_txbps=null; sig_msgs=null; sig_notfound=null
# Backpressure = signal outcome "outbound_full": the recipient IS connected here
# but its receive queue is saturated — genuine signal-plane distress. This, NOT
# notfound (which just means the recipient is OFFLINE — retry overhead, not a
# fault), is what should color signal health. Rate over the window; filled from
# the :9090 counter alongside the federation read below.
sig_backpressure=null
if [ -n "$SIGNAL_CT" ] && command -v docker >/dev/null 2>&1; then
  st=$(docker inspect -f '{{.State.Running}}' "$SIGNAL_CT" 2>/dev/null)
  if [ "$st" = "true" ]; then sig_state='"up"'; else sig_state='"down"'; fi
  cpid=$(docker inspect -f '{{.State.Pid}}' "$SIGNAL_CT" 2>/dev/null)
  if [ -n "$cpid" ] && [ "$cpid" != "0" ]; then
    read -r srxb stxb < <(nsenter -t "$cpid" -n cat /proc/net/dev 2>/dev/null \
      | awk '$1=="eth0:"{print $2,$10}')
    sprev="$STATE_DIR/signal.prev"
    if [ -f "$sprev" ] && [ -n "${srxb:-}" ]; then
      read -r spts psrxb pstxb < "$sprev"
      sdt=$((now - spts))
      if [ "$sdt" -gt 0 ] 2>/dev/null && [ "$srxb" -ge "$psrxb" ] 2>/dev/null; then
        sig_rxbps=$(( (srxb - psrxb) / sdt )); sig_txbps=$(( (stxb - pstxb) / sdt ))
      fi
    fi
    [ -n "${srxb:-}" ] && echo "$now $srxb $stxb" > "$sprev"
  fi
  sig_msgs=$(docker logs --since 60s "$SIGNAL_CT" 2>&1 | wc -l | tr -d ' ')
  sig_notfound=$(docker logs --since 60s "$SIGNAL_CT" 2>&1 | grep -c '"outcome":"notfound"')
fi
mgmt_state=null
if [ -n "$MGMT_CT" ] && command -v docker >/dev/null 2>&1; then
  mst=$(docker inspect -f '{{.State.Running}}' "$MGMT_CT" 2>/dev/null)
  if [ "$mst" = "true" ]; then mgmt_state='"up"'; else mgmt_state='"down"'; fi
fi

# --- mesh: this host's own netbird view --------------------------------------
nb=$(netbird status 2>/dev/null)
# Everything after the FIRST ": ", not awk -F': ' $2.
#
# netbird writes the failure cause into this same line:
#   Management: Disconnected, reason: rpc error: ... x509: certificate is
#   valid for ...traefik.default, not netbird.example.net
#
# Splitting on every ": " truncated that at the second delimiter and published
# the literal string "Disconnected, reason" — the word "reason" where the
# reason should be. the dashboard rendered exactly that while
# the dropped remainder named the entire outage: a Traefik recreated without
# its ACME overlay, serving a default self-signed cert, which took every
# netbird client on the goatnet off management. The panel had the diagnosis
# and threw it away at a delimiter.
#
# Truncated to 400 chars: these strings carry a gRPC dump and the dashboard
# has to render it. Enough for the x509/DNS clause that actually names the
# fault, bounded so one pathological line can't blow up the payload.
nb_mgmt=$(printf '%s' "$nb" | sed -n 's/^Management: //p' | head -1 | cut -c1-400)
peers_conn=$(printf '%s' "$nb" | awk '/^Peers count:/{split($3,a,"/"); print a[1]}')
peers_total=$(printf '%s' "$nb" | awk '/^Peers count:/{split($3,a,"/"); print a[2]}')

# netbird-boot-selfheal watchdog signal. The AUTHORITATIVE wedge flag:
# "control plane up but peers sustained-low" — the exact lockout
# signature (a host reading Connected everywhere while no peer can reach it).
# Plus how many self-heal restarts fired this boot. Absent (role not deployed
# on this host) = null, honest. This is what the dashboard verdict keys on so
# it can't tell users "it's you" while a site node is wedged.
sh_wedged=null; sh_restarts=null; sh_last=null
shf=/var/lib/netbird-boot-selfheal/metrics.prom
if [ -r "$shf" ]; then
  sh_wedged=$(awk '/^netbird_selfheal_wedged /{print $2}' "$shf" 2>/dev/null)
  sh_restarts=$(awk '/^netbird_selfheal_restart_total /{print $2}' "$shf" 2>/dev/null)
  sh_last=$(awk '/^netbird_selfheal_last_restart_timestamp /{print $2}' "$shf" 2>/dev/null)
  [ -z "$sh_wedged" ] && sh_wedged=null
  [ -z "$sh_restarts" ] && sh_restarts=null
  [ -z "$sh_last" ] && sh_last=null
fi
# Cross-boot restart TIMELINE — the last few self-heal restart
# events as [unixtime, connected, total], newest last. Lets the dashboard show
# WHEN this node has been self-healing, not just a per-boot count. Absent ledger
# (never restarted / role predates the ledger) = [] (honest empty, not null).
sh_history='[]'
shl=/var/lib/netbird-boot-selfheal/restart-ledger.tsv
if [ -r "$shl" ]; then
  sh_history=$(tail -n 12 "$shl" 2>/dev/null | awk -F'\t' \
    'BEGIN{printf "["} NF>=4{printf "%s[%s,%s,%s]",(n++?",":""),$1,$3,$4} END{printf "]"}')
  [ -z "$sh_history" ] && sh_history='[]'
fi

# --- federation: cross-node signal transport health --------------------------
# Only meaningful where a federation-proxy runs — the host-local dual-path alias
# that signal's --federation-peers dials (ADR 1085 Amendment 1 / F-256). Absent
# = null (single-signal goatnet: no cross-shard forwarding to measure), honest.
# All three signals are localhost on the mgmt host:
#   leg           which bearer carries cross-shard offers RIGHT NOW —
#                 "mesh" (host-to-host WireGuard, fast/partition-fragile) or
#                 "backup" (public via the relay fleet, partition-independent).
#                 backup = a mesh flap failed over exactly as F-256 intended:
#                 the band reads it as working-degraded, not broken. "down" =
#                 primary down AND no backup leg up = transport severed.
#   backup_ready  relay backup legs health-checked UP = failover depth.
#   signal_streams this node's live signal streams (signal :9090
#                 connected_peers) — the active-active split reads across both.
#   fwd_60s       cross-shard forwards COMPLETED this window (relay_total
#                 forwarded delta) = federation actually delivering, not just up.
FEDPROXY_CT="${HOSTNET_FEDPROXY_CONTAINER-federation-proxy}"
SIG_METRICS="${HOSTNET_SIGNAL_METRICS_ADDR-127.0.0.1:9090}"
# Federation MODE — what this goatnet's signal federation is MEANT to be, so the
# dashboard grades against intent instead of assuming active-active (ADR 1107):
#   single        one signal node by design; no cross-shard federation exists to
#                 fail — a lone node reporting is HEALTHY, not "peer dark".
#   loose-site    each site homes its own peers; single-homing per site is the
#                 DESIGN, not active-passive degradation. Cross-site path down =
#                 island-mode (each site serves locally), not a severed fabric.
#   active-active peers source-hashed across both signals; split-across-nodes is
#                 the health ideal and single-homing is a real amber (legacy).
# Default active-active preserves the existing reading for goatnets that don't
# set it. A goatnet with one drained site sets `single` -> `loose-site` at standup.
FED_MODE="${HOSTNET_FEDERATION_MODE:-active-active}"
fed_leg=null; fed_backup_ready=null; fed_streams=null; fed_fwd60=null
if command -v docker >/dev/null 2>&1 && \
   [ "$(docker inspect -f '{{.State.Running}}' "$FEDPROXY_CT" 2>/dev/null)" = "true" ]; then
  # haproxy logs every backend transition ("Server federation/mesh is UP/DOWN
  # … N backup servers online"); the LAST such line is the current leg state.
  legline=$(docker logs --tail 600 "$FEDPROXY_CT" 2>&1 \
    | grep -E 'Server federation/mesh is (UP|DOWN)' | tail -1)
  case "$legline" in
    *"is UP"*)   fed_leg='"mesh"'   ;;
    *"is DOWN"*) fed_leg='"backup"' ;;
  esac
  br=$(printf '%s' "$legline" | grep -oE '[0-9]+ backup servers (online|left)' | grep -oE '^[0-9]+')
  [ -n "$br" ] && fed_backup_ready=$br
  # primary down AND zero backups up = no path at all (distinguish from failover)
  if [ "$fed_leg" = '"backup"' ] && [ "${fed_backup_ready:-0}" = "0" ]; then fed_leg='"down"'; fi
fi
if command -v curl >/dev/null 2>&1; then
  sm=$(curl -s --max-time 4 "http://${SIG_METRICS}/metrics" 2>/dev/null)
  if [ -n "$sm" ]; then
    cp=$(printf '%s' "$sm" | awk '/^signal_server_connected_peers/{print $2; exit}')
    [ -n "$cp" ] && fed_streams=${cp%.*}
    fwd=$(printf '%s' "$sm" | awk '/outcome="forwarded"/{s+=$2} END{if(s)printf "%d",s}')
    if [ -n "$fwd" ]; then
      fprev="$STATE_DIR/fed.prev"
      if [ -f "$fprev" ]; then
        read -r _fpts pfwd < "$fprev"
        [ "$fwd" -ge "${pfwd:-0}" ] 2>/dev/null && fed_fwd60=$((fwd - pfwd))
      fi
      echo "$now $fwd" > "$fprev"
    fi
    # Backpressure rate (outbound_full delta) — the real signal-distress signal
    # for the control-plane health color (see sig_backpressure note above).
    of=$(printf '%s' "$sm" | awk '/outcome="outbound_full"/{s+=$2} END{if(s)printf "%d",s}')
    if [ -n "$of" ]; then
      ofprev="$STATE_DIR/sig_of.prev"
      if [ -f "$ofprev" ]; then
        read -r _ofts pof < "$ofprev"
        [ "$of" -ge "${pof:-0}" ] 2>/dev/null && sig_backpressure=$((of - pof))
      fi
      echo "$now $of" > "$ofprev"
    fi
  fi
fi

# --- system ------------------------------------------------------------------
load1=$(awk '{print $1}' /proc/loadavg)
mem_pct=$(free 2>/dev/null | awk '/^Mem:/{printf "%.0f",($3/$2)*100}')
disk_pct=$(df --output=pcent / 2>/dev/null | tail -1 | tr -dc 0-9)
uptime_s=$(awk '{printf "%d",$1}' /proc/uptime)

# --- rolling ring (short window; the ingest keeps the long tiers) -----------
# row = [t, rx_bps, tx_bps, loss_pct, rtt_avg_ms, tun_errs_15m, peers_conn,
#        sig_msgs_60s, sig_notfound_60s]
hfile="$STATE_DIR/ring.tsv"
if [ "$rxbps" != null ]; then
  echo "$now $rxbps $txbps ${loss:-0} ${rtt_avg:-0} ${tun_errs:-0} ${peers_conn:-0} ${sig_msgs:-0} ${sig_notfound:-0}" >> "$hfile"
  tail -n "$HIST_KEEP" "$hfile" > "${hfile}.tmp" 2>/dev/null && mv "${hfile}.tmp" "$hfile"
fi
hist=$(awk 'BEGIN{printf "["} {printf "%s[%s,%s,%s,%s,%s,%s,%s,%s,%s]",(NR>1?",":""),$1,$2,$3,$4,$5,$6,$7,$8,$9} END{printf "]"}' "$hfile" 2>/dev/null)
[ -z "$hist" ] && hist="[]"

# Last measured CAPACITY (capacity-probe.sh writes this hourly + on-demand). It
# is a speed-test result — what the link CAN do — distinct from the iface rate
# (usage). Decoupled from this 60s publish: we just forward the last measurement
# with its own timestamp. Absent (probe never ran / not configured) = null.
cap_json='null'
[ -s "$STATE_DIR/capacity.json" ] && cap_json=$(tr -d '\n' < "$STATE_DIR/capacity.json")

payload=$(printf '{"published_at":%s,"host_net":{"host":"%s","goatnet":"%s","interval_s":%s,
"lan":{"iface":"%s","ip":"%s","gw":"%s","gw_rtt_ms":%s},
"wan":{"public_ip":"%s","ping_target":"%s","sent":%s,"loss_pct":%s,"rtt_min_ms":%s,"rtt_avg_ms":%s,"rtt_max_ms":%s,"jitter_ms":%s},
"iface":{"rx_bps":%s,"tx_bps":%s,"rx_pps":%s,"tx_pps":%s,"rx_bps_peak":%s,"tx_bps_peak":%s,"peak_window_s":%s},
"tunnels":{"total":%s,"active":%s,"errs_15m":%s,"errs_by_relay":{%s}},
"control":{"signal_container":%s,"mgmt_container":%s,"signal_rx_bps":%s,"signal_tx_bps":%s,"signal_msgs_60s":%s,"signal_notfound_60s":%s,"signal_backpressure_60s":%s},
"mesh":{"management":"%s","peers_connected":%s,"peers_total":%s,"selfheal_wedged":%s,"selfheal_restart_total":%s,"selfheal_last_restart":%s,"selfheal_restart_history":%s},
"federation":{"mode":"%s","leg":%s,"backup_ready":%s,"signal_streams":%s,"fwd_60s":%s},
"sys":{"load1":%s,"mem_pct":%s,"disk_pct":%s,"uptime_s":%s},
"history":%s,"capacity":%s}}' \
  "$now" "$NAME" "$GOATNET" "$INTERVAL_S" \
  "${iface:-?}" "${lan_ip:-?}" "${gw:-?}" "$(num "${gw_rtt:-}")" \
  "${pub_ip:-?}" "$PING_TARGET" "$PING_COUNT" "$(num "${loss:-}")" "$(num "${rtt_min:-}")" "$(num "${rtt_avg:-}")" "$(num "${rtt_max:-}")" "$(num "${rtt_mdev:-}")" \
  "$rxbps" "$txbps" "$rxpps" "$txpps" "$rxbps_peak" "$txbps_peak" "$INTERVAL_S" \
  "$(num "${tun_total:-}")" "$(num "${tun_active:-}")" "$(num "${tun_errs:-}")" "${tun_by:-}" \
  "$sig_state" "$mgmt_state" "$sig_rxbps" "$sig_txbps" "$(num "${sig_msgs:-}")" "$(num "${sig_notfound:-}")" "$(num "${sig_backpressure:-}")" \
  "$(jstr "${nb_mgmt:-unknown}")" "$(num "${peers_conn:-}")" "$(num "${peers_total:-}")" "$sh_wedged" "$sh_restarts" "$sh_last" "${sh_history:-[]}" \
  "$FED_MODE" "$fed_leg" "$(num "${fed_backup_ready:-}")" "$(num "${fed_streams:-}")" "$(num "${fed_fwd60:-}")" \
  "$(num "${load1:-}")" "$(num "${mem_pct:-}")" "$(num "${disk_pct:-}")" "$(num "${uptime_s:-}")" \
  "$hist" "$cap_json")

# --- POST to every declared ingest. Best-effort each; exit 0 if ANY landed —
# on a degraded uplink one target reachable is a win, and the timer must not
# flap the unit red when the OTHER dashboard's box is what's unreachable.
ok=1
IFS=',' read -ra URLS <<< "$INGEST_URLS"
for u in "${URLS[@]}"; do
  u=$(printf '%s' "$u" | tr -d ' ')
  [ -z "$u" ] && continue
  code=$(curl -ksS -m 10 -o /dev/null -w '%{http_code}' \
    -H "Authorization: Bearer ${TOKEN}" -H "Content-Type: application/json" \
    -X POST --data-binary "$payload" \
    "${u}/${NAME}" 2>/dev/null)
  echo "host-net-publish: ${NAME} -> ${u} http=${code} loss=${loss:-?}% rtt=${rtt_avg:-?}ms tun_errs=${tun_errs:-?} sig_notfound=${sig_notfound:-?}" >&2
  { [ "$code" = "200" ] || [ "$code" = "204" ]; } && ok=0
done
exit $ok
