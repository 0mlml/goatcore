#!/usr/bin/env bash
# host-net-ratesample — continuous peak-hold sampler for the default-route iface.
#
# WHY THIS EXISTS: host-net-publish.sh runs once per 60s and reports the byte
# rate as (counter_delta / ~60s) — a 60-SECOND AVERAGE. A real burst is smeared
# away by that window: a link doing about 850 Mbps for 14s reads as about
# 150 Mbps averaged over the 60s window, and a few-second burst averages down
# to about 1 MB/s. So a speed-test-grade peak NEVER appears on the dashboard.
#
# This lightweight always-on loop samples /proc/net/dev every SAMPLE_INTERVAL_S
# and appends the 1s-normalized rx/tx rate to a short ring. host-net-publish.sh
# reads the ring and reports the MAX over the last publish interval as
# iface.rx_bps_peak / tx_bps_peak alongside the 60s average. The publisher only
# READS the ring (never mutates it), so there is no reset race — the sampler is
# the sole writer.
#
# Cost: one awk over /proc/net/dev every 2s. No docker, no network, no sudo.
# See docs/design/node-net-health.md.
set -u

: "${STATE_DIR:=/var/lib/host-net-publish}"
: "${SAMPLE_INTERVAL_S:=2}"
# Keep enough samples to cover the publish interval with margin (default 60s
# interval / 2s sample = 30 rows; keep 60 so a slow publish still sees a full
# window). Old rows are ignored by the publisher's time cutoff regardless.
: "${RING_KEEP:=60}"

RING="$STATE_DIR/ratesamples.tsv"
mkdir -p "$STATE_DIR" 2>/dev/null || true

def_iface() { ip route show default 2>/dev/null | awk '{print $5; exit}'; }
read_ctr() { awk -v i="$1:" '$1==i{print $2, $10}' /proc/net/dev 2>/dev/null; }  # rxbytes txbytes

prxb=""; ptxb=""; piface=""; pnow=""
while :; do
  iface=$(def_iface)
  if [ -z "$iface" ]; then sleep "$SAMPLE_INTERVAL_S"; continue; fi
  read -r rxb txb < <(read_ctr "$iface")
  now=$(date +%s.%N)
  if [ -n "$prxb" ] && [ "$iface" = "$piface" ] && [ -n "${rxb:-}" ]; then
    # Skip across counter resets (reboot) or iface change — never emit a
    # negative/garbage rate. dt must be sane (>0.2s) to avoid divide blow-ups.
    if awk -v a="$pnow" -v b="$now" 'BEGIN{exit !((b-a)>0.2)}' \
       && [ "$rxb" -ge "$prxb" ] 2>/dev/null && [ "$txb" -ge "$ptxb" ] 2>/dev/null; then
      read -r rxr txr < <(awk -v pa="$prxb" -v a="$rxb" -v pb="$ptxb" -v b="$txb" \
        -v t0="$pnow" -v t1="$now" 'BEGIN{dt=t1-t0; printf "%d %d", (a-pa)/dt, (b-pb)/dt}')
      printf '%d %s %s\n' "$(printf '%.0f' "$now")" "$rxr" "$txr" >> "$RING"
      # trim to the last RING_KEEP rows (bounded file, no unbounded growth)
      if [ "$(wc -l < "$RING" 2>/dev/null || echo 0)" -gt "$((RING_KEEP + 20))" ]; then
        tail -n "$RING_KEEP" "$RING" > "$RING.tmp" 2>/dev/null && mv "$RING.tmp" "$RING"
      fi
    fi
  fi
  prxb=$rxb; ptxb=$txb; piface=$iface; pnow=$now
  sleep "$SAMPLE_INTERVAL_S"
done
