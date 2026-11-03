#!/usr/bin/env bash
# goatnet-summary-publish.sh — publish THIS goatnet's relay rollup to sibling
# relay-health boxes, so goatnets.html (the level above the fleet page) can show
# every goatnet on one page.
#
# WHY A PUBLISH AND NOT A FETCH
# The obvious design is for the browser to fetch each sibling dashboard's JSON
# and aggregate client-side. It cannot work: every dashboard sits behind its own
# oauth2-proxy on its own domain, and oauth2-proxy sets its session cookie
# SameSite=Lax, so a cross-origin fetch arrives unauthenticated, is 302'd to the
# IdP, and fails CORS. Fixing THAT would mean SameSite=None on the cookie
# guarding a recon-sensitive surface (ADR 1066) — a real security loosening
# bought for convenience. Publishing is the same reveal-only idiom every other
# health band already uses, and costs no new exposure.
#
# WHAT CROSSES THE BOUNDARY — deliberately almost nothing:
#   generated, vantage, relays_up, relays_total, worst_lat_ms, stale_after_s
# No relay names, no addresses, no per-relay detail, no peers. Relay topology is
# recon-sensitive; a neighbour needs to know THAT a goatnet is healthy, not how
# it is built. The receiver whitelist-copies these fields and drops the rest, so
# widening the payload here cannot widen what is stored there.
#
# CREDENTIAL: CROSS_FLEET_TOKEN, distinct from the box's own ingest token. A
# production goatnet does not hand a sibling its full ingest credential; this
# one authorizes exactly the six fields above. Unset on the receiver ⇒ the route
# 404s and the feature is off.
#
# Best-effort by design: a sibling being unreachable is normal (it may be down —
# which is exactly when its neighbour's dashboard matters) and must never fail
# this timer. A goatnet whose summary stops arriving renders UNKNOWN on the
# overview, never "down": absence of evidence is not evidence of failure.
set -uo pipefail

JSON="${RELAY_HEALTH_JSON:-/opt/relay-health/www/relay-health.json}"
GOATNET="${GOATNET_ID:?GOATNET_ID required}"
TARGETS="${GOATNET_SUMMARY_TARGETS:-}"     # comma-separated base URLs
TOKEN="${CROSS_FLEET_TOKEN:-}"
STALE_AFTER_S="${GOATNET_SUMMARY_STALE_AFTER_S:-300}"

[ -n "$TARGETS" ] || { echo "goatnet-summary: no targets configured; nothing to do" >&2; exit 0; }
[ -n "$TOKEN" ]   || { echo "goatnet-summary: CROSS_FLEET_TOKEN unset; refusing to publish" >&2; exit 0; }
[ -r "$JSON" ]    || { echo "goatnet-summary: $JSON unreadable; probe has not run yet" >&2; exit 0; }

# Build the rollup from the local probe. A relay counts as up on the two signals
# that drive health on BOTH vantages (enrollment reachable + rathole control),
# which is the same basis the fleet page's own rollup uses — so a goatnet cannot
# look healthier to its neighbour than it does to itself.
payload="$(python3 - "$JSON" "$STALE_AFTER_S" <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
relays = d.get("relays") or []
up = sum(1 for r in relays
         if (r.get("enroll") or {}).get("ok", 0) > 0 and r.get("rathole_ctl") is True)
lats = [r["lat_ms"] for r in relays if isinstance(r.get("lat_ms"), (int, float))]
print(json.dumps({
    "generated":     d.get("generated"),
    "vantage":       d.get("vantage"),
    "relays_up":     up,
    "relays_total":  len(relays),
    "worst_lat_ms":  max(lats) if lats else None,
    "stale_after_s": int(sys.argv[2]),
}))
PY
)" || { echo "goatnet-summary: could not build payload" >&2; exit 0; }

IFS=',' read -ra urls <<< "$TARGETS"
for u in "${urls[@]}"; do
  u="$(echo "$u" | xargs)"; [ -n "$u" ] || continue
  code=$(curl -ksS -m 10 -o /dev/null -w '%{http_code}' \
    -H "Authorization: Bearer ${TOKEN}" -H "Content-Type: application/json" \
    -X POST --data-binary "$payload" "${u}/${GOATNET}" 2>/dev/null)
  echo "goatnet-summary: ${GOATNET} -> ${u} http=${code}" >&2
done

# Exit 0 even when every target refused: see the best-effort note above. The
# timer's health is not a proxy for the siblings' reachability, and treating it
# as one would page on the neighbour's outage instead of showing it.
exit 0
