#!/usr/bin/env bash
# metrics-publish.sh — relay self-publishes its OWN host metrics to the site host.
#
# Runs ON a relay (root, via relay-metrics-publish.timer). Reads the SAME
# metrics relay-health's --deep collects, but LOCALLY (no SSM, no AWS creds) —
# /proc/net/dev (rate-diffed), docker stats, docker logs, df/du, systemctl/ss —
# builds the identical relay-health "deep" JSON shape + a rolling history ring,
# and POSTs it to a token-gated ingest endpoint on the site host (reached over the
# relay's own rathole :443 path → site-host Traefik). The hosted relay-health prober
# folds the ingested file in, so the dashboard throughput panel auto-populates.
# ADR 1058. This script is the bash ROLLBACK copy of `relay-probe publish`
# (ops/relay-probe/deep.go) — the Go binary is the deployed, canonical
# definition of the shape (relay_metrics_publisher=binary on
# self-publishing fleets). The shape here MUST match
# deep.go's Deep struct. It overlaps, but is not identical to, deep_probe()
# in relay-health-probe.sh (the separate operator-run --deep SSM path): that
# function does not carry the `delivery` block below (#467 follow-on).
#
# Config via env (set by the systemd unit):
#   RELAY_NAME            this relay's short name (matches relays.conf), required
#   RELAY_METRICS_INGEST_URL   e.g. https://netbird.example.net/relay-metrics-ingest  (required)
#   RELAY_METRICS_BEARER_TOKEN shared bearer token (required)
#   RELAY_METRICS_INGEST_HOST  Host header / TLS SNI (default: derived from URL)
#   GOAT_RELAY_CONTAINER       container name (default: goat-relay)
#   HIST_KEEP                  rolling-window samples (default 40 ≈ 10 min @ 15s)
#   STATE_DIR                  rate/history cache (default /var/lib/relay-metrics)
set -uo pipefail

RELAY_NAME="${RELAY_NAME:?RELAY_NAME required}"
INGEST_URL="${RELAY_METRICS_INGEST_URL:?RELAY_METRICS_INGEST_URL required}"
TOKEN="${RELAY_METRICS_BEARER_TOKEN:?RELAY_METRICS_BEARER_TOKEN required}"
CONTAINER="${GOAT_RELAY_CONTAINER:-goat-relay}"
HIST_KEEP="${HIST_KEEP:-40}"
STATE_DIR="${STATE_DIR:-/var/lib/relay-metrics}"
# Host header for the ingest (the relay hairpins through its own rathole :443 to
# the site host's Traefik, which routes by Host). Default = the URL's host.
INGEST_HOST="${RELAY_METRICS_INGEST_HOST:-$(printf '%s' "$INGEST_URL" | sed -E 's#^https?://([^/]+)/.*#\1#')}"
mkdir -p "$STATE_DIR"

# --- service states (same commands as deep_probe, run locally) ---
rh=$(systemctl is-active rathole-server.service 2>/dev/null || echo unknown)
wd=$(systemctl is-active rathole-watchdog.timer 2>/dev/null || echo unknown)
heals=$(journalctl -t rathole-watchdog --since "-1h" 2>/dev/null | grep -c "threshold reached")
coturn=$(ss -lun 2>/dev/null | grep -c ":3478")
relay_up=$(docker ps --filter "name=${CONTAINER}" --filter status=running -q 2>/dev/null | head -1 | grep -c .)
dp=$(ss -lun 2>/dev/null | grep -c ":33073")
# Distinct peers that authenticated (entered the QUIC forwarding loop) in the
# window. APPROXIMATE, by design: a log-window count can't see long-lived peers
# that authed before the window (they forward quietly without re-logging), so
# this is a recent-activity signal, NOT a live session count — an accurate
# current-peer count + per-peer bytes need a relay stats exporter (ADR 1055).
# Widened from 5m→30m so stable peers aren't dropped to 0 (the old window did).
PEERS_WINDOW_MIN=30
peers=$(docker logs --since "${PEERS_WINDOW_MIN}m" "$CONTAINER" 2>&1 | grep -oE 'PeerId\("[^"]+"\)' | sort -u | wc -l | tr -d ' ')
# --- per-peer AUTH REJECTIONS over a 5m window, by reason + top offender IPs
#. expired_token = a peer presenting a stale/corrupt cached relay token
# (the profile-drift single-peer isolation); ws_upgrade_timeout = never finished
# the WS upgrade; ws_handoff_eof = benign QUIC-race-loser noise (F-041 — surfaced,
# never colored critical). Same log-grep as deep_probe(), run locally. Capture
# the window once so the counts share it. The shape here MUST match deep.go's
# AuthReject struct (deep_probe() carries the equivalent fields too, but is
# maintained separately — see the file header).
AUTH_WINDOW_MIN=5
al=$(docker logs --since "${AUTH_WINDOW_MIN}m" "$CONTAINER" 2>&1)
arexp=$(printf '%s\n' "$al" | grep -cE 'validator rejected.*expired auth token')
arwst=$(printf '%s\n' "$al" | grep -cE 'WS upgrade timed out')
areof=$(printf '%s\n' "$al" | grep -cE 'hand-off failed.*eof')
ar_off_json=$(printf '%s\n' "$al" | grep -E 'validator rejected.*expired auth token' \
  | grep -oE 'for [0-9.]+' | cut -d' ' -f2 | sort | uniq -c | sort -rn | head -3 \
  | while read -r c ip; do printf '%s{"ip":"%s","count":%s}' "${sep:-}" "$ip" "$c"; sep=,; done)
# --- delivery health --------------------------------------------
# The wedge class that cost months of investigation was INVISIBLE from every
# surface: the relay counted its silent drops on the ADR 1055 stats endpoint
# but nothing ever COLLECTED them (169k peer_not_found sat unread on
# 127.0.0.1:9098), and the one log line said "writer exiting" without naming
# a peer. Publish both halves so a dead-writer eviction is a dashboard event,
# not an archaeology project.
#
#   drops.*             lifetime counters from the relay's own stats endpoint
#   writer_death        a QUIC writer hit a CONNECTION-level failure (we now
#                       close the conn so the client reconnects) — should be ~0
#   evictions           peers deregistered because their writer was dead
#   datagram_too_large  per-frame oversize drops; NON-fatal after F-275, but a
#                       sustained rate means a WS peer is pushing frames a QUIC
#                       peer cannot receive (cross-transport size asymmetry)
STATS_ADDR="${GOAT_RELAY_STATS_ADDR:-127.0.0.1:9098}"
# Fetch the relay's white-box stats ONCE, then derive both the drop counters
# and the per-transport connection split (which is UDP/QUIC vs WS/TCP — the
# question that was un-answerable from outside the relay; the stats peers[]
# rows carry a per-peer "transport" field since the F-275 transport-visibility
# change).
relay_stats=$(curl -s --max-time 3 "http://${STATS_ADDR}/" 2>/dev/null)
drops_json=$(printf '%s' "$relay_stats" | python3 -c 'import json,sys
try:
    d=json.load(sys.stdin); x=d.get("drops",{})
    print(json.dumps({"writer_full":x.get("writer_full",0),
                      "writer_closed":x.get("writer_closed",0),
                      "peer_not_found":x.get("peer_not_found",0),
                      "datagram_too_large":x.get("datagram_too_large",0),
                      "connections":d.get("connections",0)}))
except Exception:
    print("null")' 2>/dev/null)
[ -z "$drops_json" ] && drops_json=null
read -r conns_quic conns_ws <<EOF2
$(printf '%s' "$relay_stats" | python3 -c 'import json,sys
try:
    p=json.load(sys.stdin).get("peers",[])
    print(sum(1 for x in p if x.get("transport")=="quic"), sum(1 for x in p if x.get("transport")=="ws"))
except Exception:
    print(0,0)' 2>/dev/null)
EOF2
conns_quic=${conns_quic:-0}; conns_ws=${conns_ws:-0}
wdeath=$(printf '%s\n' "$al" | grep -cE 'send_datagram failed.*closing connection|writer exiting')
wevict=$(printf '%s\n' "$al" | grep -cE 'routing: evicted')
wtoobig=$(printf '%s\n' "$al" | grep -cE 'datagram too large')

cpu=$(docker stats --no-stream --format "{{.CPUPerc}}" "$CONTAINER" 2>/dev/null | tr -d '% ')
mem=$(docker stats --no-stream --format "{{.MemPerc}}" "$CONTAINER" 2>/dev/null | tr -d '% ')
disk=$(df --output=pcent / 2>/dev/null | tail -1 | tr -dc 0-9)
maxlog=$(du -m /var/lib/docker/containers/*/*.log 2>/dev/null | sort -rn | head -1 | cut -f1)

# --- throughput: rate-diff /proc/net/dev (default-route iface) vs prev sample ---
iface=$(ip route show default 2>/dev/null | awk '{print $5; exit}')
read -r rxb rxp txb txp < <(awk -v i="${iface}:" '$1==i{print $2,$3,$10,$11}' /proc/net/dev 2>/dev/null)
now=$(date +%s); rxbps=null; txbps=null; rxpps=null; txpps=null
prev="${STATE_DIR}/${RELAY_NAME}.prev"
if [ -f "$prev" ] && [ -n "${rxb:-}" ]; then
  read -r pts prxb prxp ptxb ptxp < "$prev"
  dt=$((now - pts))
  if [ "$dt" -gt 0 ] 2>/dev/null && [ "$rxb" -ge "$prxb" ] 2>/dev/null; then
    rxbps=$(( (rxb - prxb) / dt )); txbps=$(( (txb - ptxb) / dt ))
    rxpps=$(( (rxp - prxp) / dt )); txpps=$(( (txp - ptxp) / dt ))
  fi
fi
[ -n "${rxb:-}" ] && echo "$now $rxb $rxp $txb $txp" > "$prev"

# --- rolling history ring (same shape deep_probe emits) ---
hdir="${STATE_DIR}/history"; mkdir -p "$hdir"; hfile="${hdir}/${RELAY_NAME}.tsv"
if [ "$rxbps" != null ]; then
  echo "$now $rxbps $txbps $rxpps $txpps ${peers:-0}" >> "$hfile"
  tail -n "$HIST_KEEP" "$hfile" > "${hfile}.tmp" 2>/dev/null && mv "${hfile}.tmp" "$hfile"
fi
hist_json=$(awk 'BEGIN{printf "["} {printf "%s[%s,%s,%s,%s,%s,%s]",(NR>1?",":""),$1,$2,$3,$4,$5,$6} END{printf "]"}' "$hfile" 2>/dev/null)
[ -z "$hist_json" ] && hist_json="[]"

# --- assemble the deep JSON (identical shape to deep_probe) ---
payload=$(printf '{"published_at":%s,"deep":{"rathole":"%s","watchdog":"%s","heals_1h":%s,"coturn_listen":%s,"relay_up":%s,"dataplane_listen":%s,"peers_recent":%s,"peers_window_min":%s,"cpu_pct":"%s","mem_pct":"%s","disk_pct":%s,"max_container_log_mb":%s,"rx_bps":%s,"tx_bps":%s,"rx_pps":%s,"tx_pps":%s,"auth_reject":{"expired_token":%s,"ws_upgrade_timeout":%s,"ws_handoff_eof":%s,"window_min":%s,"top_offenders":[%s]},"delivery":{"drops":%s,"conns_quic":%s,"conns_ws":%s,"writer_death":%s,"evictions":%s,"datagram_too_large":%s,"window_min":%s},"history":%s}}' \
  "$now" "${rh:-unknown}" "${wd:-unknown}" "${heals:-0}" "${coturn:-0}" "${relay_up:-0}" "${dp:-0}" \
  "${peers:-0}" "${PEERS_WINDOW_MIN:-30}" "${cpu:-?}" "${mem:-?}" "${disk:-0}" "${maxlog:-0}" "$rxbps" "$txbps" "$rxpps" "$txpps" \
  "${arexp:-0}" "${arwst:-0}" "${areof:-0}" "${AUTH_WINDOW_MIN:-5}" "${ar_off_json:-}" \
  "${drops_json:-null}" "${conns_quic:-0}" "${conns_ws:-0}" "${wdeath:-0}" "${wevict:-0}" "${wtoobig:-0}" "${AUTH_WINDOW_MIN:-5}" "$hist_json")

# --- POST to the site host ingest, forced through the relay's own rathole :443 ---
# --resolve pins the ingest host to localhost so it traverses THIS relay's
# rathole tunnel to the site host (deterministic; no dependence on public DNS RR).
# No -k: the site host presents the publicly trusted certificate for INGEST_HOST,
# and this request carries the bearer token.
code=$(curl -sS -m 10 -o /dev/null -w '%{http_code}' \
  --resolve "${INGEST_HOST}:443:127.0.0.1" \
  -H "Authorization: Bearer ${TOKEN}" -H "Content-Type: application/json" \
  -X POST --data-binary "$payload" \
  "${INGEST_URL}/${RELAY_NAME}" 2>/dev/null)
echo "relay-metrics: ${RELAY_NAME} -> ${INGEST_URL} http=${code} rx=${rxbps} tx=${txbps} peers=${peers}" >&2

# --- optional SECOND target: a DIRECT (non-hairpin) ingest — e.g. the cloud
# relay-health box on its public :443 , so
# BOTH dashboards get throughput parity (ADR 1058 §"repoint publishers"). This
# posts straight over public DNS (no --resolve hairpin), because the cloud box
# is NOT on this relay's rathole path. Best-effort: it does NOT gate the timer,
# so a cloud-ingest hiccup never marks the primary (site host) publish failed.
# No -k: this leg crosses the public internet with a bearer token (the primary
# one when no second token is set), so the certificate must verify.
if [ -n "${RELAY_METRICS_INGEST_URL2:-}" ]; then
  code2=$(curl -sS -m 10 -o /dev/null -w '%{http_code}' \
    -H "Authorization: Bearer ${RELAY_METRICS_BEARER_TOKEN2:-$TOKEN}" -H "Content-Type: application/json" \
    -X POST --data-binary "$payload" \
    "${RELAY_METRICS_INGEST_URL2}/${RELAY_NAME}" 2>/dev/null)
  echo "relay-metrics: ${RELAY_NAME} -> ${RELAY_METRICS_INGEST_URL2} http=${code2} (direct, best-effort)" >&2
fi

[ "$code" = "200" ] || [ "$code" = "204" ]
