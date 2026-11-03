#!/bin/bash
# relay-health-allowlist-publish.sh — ADR 1093 Phase 1 allowlist auto-sync.
#
# Runs on the site mgmt host (which has Kratos). Reads the ACTIVE identity roster from
# Kratos, and POSTs the email list to the relay-health box's token-gated
# allowlist-ingest. The box writes it IN-PLACE to the oauth2-proxy
# authenticated-emails file, so "active member ⇒ dashboard access, revoked ⇒
# removed" is structural, not a manual redeploy.
#
# Fail-safe by construction: the box refuses a list below its floor (ALLOWLIST_MIN)
# and keeps the last-good file, so a broken Kratos read here can never lock the
# fleet out. This script derives the SAME set the operator would deploy by hand
# (state==active, traits.email), so it never widens scope on its own.
#
# Env:
#   KRATOS_ADMIN_URL           default http://kratos:4434 (mgmt docker network)
#   ALLOWLIST_INGEST_URL       REQUIRED, e.g.
#     https://relay-health.example.net/relay-health-allowlist-ingest/<publisher>
#   RELAY_METRICS_BEARER_TOKEN REQUIRED, the shared ingest bearer token
#   ALLOWLIST_INGEST_EXTRA_TARGETS  optional, comma-separated url|token pairs —
#     the SAME roster published to additional boxes (each box holds its own
#     ingest bearer, so every extra target carries its own token). Used to make
#     a sibling goatnet's off-mesh dashboard door follow THIS goatnet's
#     membership (e.g. one goatnet's box admitting another's roster).
set -euo pipefail
: "${KRATOS_ADMIN_URL:=http://kratos:4434}"
: "${ALLOWLIST_INGEST_URL:?ALLOWLIST_INGEST_URL required}"
: "${RELAY_METRICS_BEARER_TOKEN:?RELAY_METRICS_BEARER_TOKEN required}"
: "${ALLOWLIST_INGEST_EXTRA_TARGETS:=}"

# Query Kratos + build {"emails":[...]} from active identities, on the mgmt network.
payload="$(docker run --rm --network mgmt curlimages/curl -fsS \
    "${KRATOS_ADMIN_URL}/admin/identities?per_page=1000&page=0" \
  | python3 -c '
import json, sys
d = json.load(sys.stdin)
em = sorted({(i.get("traits") or {}).get("email", "").strip().lower()
             for i in d
             if i.get("state") == "active" and (i.get("traits") or {}).get("email")})
print(json.dumps({"emails": em}))')"

n="$(printf '%s' "$payload" | python3 -c 'import json,sys; print(len(json.load(sys.stdin)["emails"]))')"

# POST to every target box; --network mgmt (each box is reached via its public
# FQDN over egress). Targets are independent: one box down must not stop the
# roster reaching the others, so failures collect and the exit reflects the set.
targets=("${ALLOWLIST_INGEST_URL}|${RELAY_METRICS_BEARER_TOKEN}")
if [ -n "$ALLOWLIST_INGEST_EXTRA_TARGETS" ]; then
  IFS=',' read -ra _extra <<< "$ALLOWLIST_INGEST_EXTRA_TARGETS"
  targets+=("${_extra[@]}")
fi

rc=0
for t in "${targets[@]}"; do
  url="${t%%|*}"
  token="${t#*|}"
  code="$(printf '%s' "$payload" | docker run --rm -i --network mgmt curlimages/curl -s \
      -o /dev/null -w '%{http_code}' -X POST "$url" \
      -H "Authorization: Bearer ${token}" \
      -H "Content-Type: application/json" --data-binary @-)"
  echo "relay-health-allowlist-publish: POST ${url%%\?*} -> HTTP ${code} (${n} active emails)"
  # 409 = box kept last-good because our list was below its floor; that is a WARN,
  # not a hard failure of the timer (the gate stays correct). Any other non-200 fails.
  case "$code" in
    200) : ;;
    409) echo "relay-health-allowlist-publish: WARN ${url} rejected below-floor list; last-good kept" >&2 ;;
    *)   echo "relay-health-allowlist-publish: ERROR ${url} HTTP ${code}" >&2; rc=1 ;;
  esac
done
exit "$rc"
