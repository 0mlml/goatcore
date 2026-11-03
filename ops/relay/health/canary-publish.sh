#!/usr/bin/env bash
# canary-publish.sh — an EXTERNAL, off-mesh reachability canary.
#
# The point of a canary is an OUTSIDE vantage: a cloud host in its own region
# that answers "can a newcomer get on this mesh, from out here?" — the signal a
# locked-out user needs and can't get from any mesh-only surface. It publishes
# OFF-MESH (to the cloud ingest over the public internet) so its verdict
# survives the very outage it reports. Run 2-3+ in distinct regions/providers so
# a single canary doing something idiosyncratic is outvoted, never taken as truth
# (the ingest quorums them; see relay-metrics-ingest.py /canary-ingest).
#
# Two classes of check, deliberately separated:
#   1. FRONT DOOR (no mesh needed) — raw TCP+TLS to each relay's public :443.
#      Works even if this canary is not enrolled; this is the "is the network's
#      front door up from the outside" signal, the one a locked-out user shares.
#   2. NEWCOMER JOURNEY (needs this canary enrolled) — signal + management from
#      `netbird status`, a mesh-name DNS resolve, and a real service round-trip.
#      This is what a peer that DID get on experiences.
#
# Env (all optional except NAME + INGEST_URLS + TOKEN):
#   CANARY_NAME            this canary's id (e.g. canary-eu-central)          [req]
#   CANARY_VANTAGE         human region label (e.g. "eu-central / hetzner")
#   GOATNET                goatnet id
#   ENROLL_HOST            enrollment front-door host (see default below)
#   RELAY_IPS              comma public relay IPs for the :443 front-door probe;
#                          empty -> resolve ENROLL_HOST A records
#   MESH_NAME              a mesh hostname to resolve (default isitup.<gn>.netbird.<ENROLL_HOST>)
#   MESH_SERVICE_URL       a mesh service to round-trip (default https://<MESH_NAME>/)
#   INGEST_URLS            comma off-mesh ingest URLs (…/canary-ingest/<NAME>)   [req]
#   CANARY_TOKEN           bearer token for the ingest                          [req]
#   STATE_DIR              scratch (default /var/lib/goat-canary)
set -uo pipefail

NAME="${CANARY_NAME:?CANARY_NAME required}"
VANTAGE="${CANARY_VANTAGE:-$NAME}"
GOATNET="${GOATNET:-zagros}"
ENROLL_HOST="${ENROLL_HOST:-netbird.zagros.example}"
MESH_NAME="${MESH_NAME:-isitup.${GOATNET}.netbird.${ENROLL_HOST}}"
MESH_SERVICE_URL="${MESH_SERVICE_URL:-https://${MESH_NAME}/}"
INGEST_URLS="${INGEST_URLS:?INGEST_URLS required}"
TOKEN="${CANARY_TOKEN:?CANARY_TOKEN required}"
STATE_DIR="${STATE_DIR:-/var/lib/goat-canary}"
mkdir -p "$STATE_DIR" 2>/dev/null || true
now=$(date +%s)

# helper: json-escape a short string
esc(){ printf '%s' "${1:-}" | sed 's/\\/\\\\/g; s/"/\\"/g'; }

# ---- 1. FRONT DOOR: raw TCP+TLS to each relay public IP on :443 -------------
# No mesh required. "up" = at least one relay completes a TLS handshake; the
# ratio is the honest breadth (1/5 answering is a degraded front door).
fd_ips="${RELAY_IPS:-}"
[ -z "$fd_ips" ] && fd_ips=$(getent ahostsv4 "$ENROLL_HOST" 2>/dev/null | awk '{print $1}' | sort -u | paste -sd, -)
[ -z "$fd_ips" ] && fd_ips=$(dig +short A "$ENROLL_HOST" 2>/dev/null | paste -sd, -)
fd_total=0; fd_ok=0
IFS=',' read -ra IPS <<< "$fd_ips"
for ip in "${IPS[@]}"; do
  ip=$(printf '%s' "$ip" | tr -d ' '); [ -z "$ip" ] && continue
  fd_total=$((fd_total+1))
  # -m timeout; SNI = ENROLL_HOST; success on a completed TLS handshake
  if curl -ksS -m 8 --resolve "${ENROLL_HOST}:443:${ip}" -o /dev/null "https://${ENROLL_HOST}/" 2>/dev/null \
     || echo | openssl s_client -connect "${ip}:443" -servername "$ENROLL_HOST" -verify_return_error 2>/dev/null | grep -q 'Verify return code: 0'; then
    fd_ok=$((fd_ok+1))
  fi
done
if [ "$fd_total" -eq 0 ]; then fd_st="grey"; fd_detail="no relay IPs"
elif [ "$fd_ok" -eq 0 ]; then fd_st="red"; fd_detail="0/${fd_total} answering"
elif [ "$fd_ok" -lt "$fd_total" ]; then fd_st="amber"; fd_detail="${fd_ok}/${fd_total} answering"
else fd_st="green"; fd_detail="${fd_ok}/${fd_total} answering"; fi

# ---- 2. NEWCOMER JOURNEY (needs this canary enrolled) ----------------------
# signal + management come from `netbird status -j`; grey if netbird absent
# (an unenrolled canary still reports the front door above).
sig_st="grey"; sig_d="netbird not present"; mgmt_st="grey"; mgmt_d="netbird not present"
if command -v netbird >/dev/null 2>&1; then
  st_json=$(netbird status -j 2>/dev/null)
  if [ -n "$st_json" ]; then
    scon=$(printf '%s' "$st_json" | jq -r '.signal.connected // empty' 2>/dev/null)
    mcon=$(printf '%s' "$st_json" | jq -r '.management.connected // empty' 2>/dev/null)
    [ "$scon" = "true" ] && { sig_st="green"; sig_d="connected"; } || { sig_st="red"; sig_d="disconnected"; }
    [ "$mcon" = "true" ] && { mgmt_st="green"; mgmt_d="connected"; } || { mgmt_st="red"; mgmt_d="disconnected"; }
  fi
fi

# name service: resolve a mesh hostname (mesh CoreDNS when enrolled)
name_st="grey"; name_d="—"
resolved=$(getent hosts "$MESH_NAME" 2>/dev/null | awk '{print $1; exit}')
[ -z "$resolved" ] && resolved=$(dig +short "$MESH_NAME" 2>/dev/null | head -1)
if [ -n "$resolved" ]; then name_st="green"; name_d="→ ${resolved}"; else name_st="red"; name_d="NXDOMAIN"; fi

# data plane: a real service round-trip (WG up + HTTP reachable)
dp_st="grey"; dp_d="—"
dp_code=$(curl -ksS -m 10 -o /dev/null -w '%{http_code}' "$MESH_SERVICE_URL" 2>/dev/null || echo 000)
case "$dp_code" in
  2*|3*) dp_st="green"; dp_d="${dp_code}";;
  000)   dp_st="red";   dp_d="no route";;
  4*|5*) dp_st="amber"; dp_d="${dp_code}";;
esac

# ---- overall + plain-English implication -----------------------------------
# worst-of the meaningful dimensions; grey (unknown) never worsens the verdict.
worst="green"
for s in "$fd_st" "$sig_st" "$mgmt_st" "$name_st" "$dp_st"; do
  case "$s" in red) worst="red";; amber) [ "$worst" != red ] && worst="amber";; esac
done
if [ "$worst" = red ] && [ "$fd_st" = red ]; then
  impl="the mesh front door is unreachable from ${VANTAGE} — a real peer here cannot get on; it is the network, not them"
elif [ "$worst" = red ]; then
  impl="a newcomer in ${VANTAGE} reaches the front door but cannot complete the journey (signal/name/service) — degraded for them"
elif [ "$worst" = amber ]; then
  impl="partial: a peer in ${VANTAGE} may get on but with trouble on some path"
else
  impl="a newcomer in ${VANTAGE} gets on cleanly and reaches services"
fi

report=$(printf '{"peer":"%s","goatnet":"%s","vantage":"%s","reported_at":%s,"overall":"%s","journey":{"front_door":{"st":"%s","detail":"%s"},"signal":{"st":"%s","detail":"%s"},"management":{"st":"%s","detail":"%s"},"name_service":{"st":"%s","detail":"%s"},"data_plane":{"st":"%s","detail":"%s"}},"implication":"%s"}' \
  "$(esc "$NAME")" "$(esc "$GOATNET")" "$(esc "$VANTAGE")" "$now" "$worst" \
  "$fd_st" "$(esc "$fd_detail")" "$sig_st" "$(esc "$sig_d")" "$mgmt_st" "$(esc "$mgmt_d")" \
  "$name_st" "$(esc "$name_d")" "$dp_st" "$(esc "$dp_d")" "$(esc "$impl")")
payload="{\"published_at\":${now},\"canary\":${report}}"
echo "canary ${NAME}: overall=${worst} front_door=${fd_detail} signal=${sig_st} name=${name_st} service=${dp_d}" >&2

# ---- publish OFF-MESH, best-effort to each ingest (any landed = success) ----
landed=1
IFS=',' read -ra URLS <<< "$INGEST_URLS"
for u in "${URLS[@]}"; do
  u=$(printf '%s' "$u" | tr -d ' '); [ -z "$u" ] && continue
  code=$(curl -ksS -m 10 -o /dev/null -w '%{http_code}' \
    -H "Authorization: Bearer ${TOKEN}" -H "Content-Type: application/json" \
    -X POST --data-binary "$payload" "$u" 2>/dev/null || echo 000)
  [ "$code" = "200" ] && landed=0
  echo "  -> ${u} : ${code}" >&2
done
exit $landed
