#!/bin/sh
# revoke-stale-consent.sh — one-shot helper that revokes a user's stored
# OAuth2 consent sessions on a site-Hydra admin endpoint.
#
# Use case: F-161 patch (ory-consent scope-fallback) ships in a new
# kratos-selfservice-ui-node-patched image, but Hydra's stored consent
# sessions are persisted in Postgres and DON'T re-run through the
# consent UI on every login (they short-circuit at the "is this client
# already consented for this subject?" check). So an already-affected
# user — one whose first login wrote a stored session with empty
# granted_scope while patch 3 wasn't installed yet — keeps hitting the
# same empty-userinfo failure even AFTER the new image is live.
#
# Revoking their stored consent forces the next login through the
# consent UI again, where patch 3 now runs and writes a fresh session
# with the correct granted_scope. From then on the user is healthy.
#
# Usage:
#
#   # Revoke for a specific subject + client (most common):
#   revoke-stale-consent.sh <kratos-identity-id> <oauth2-client-id>
#
#   # Revoke ALL consents for a subject (all clients):
#   revoke-stale-consent.sh <kratos-identity-id> all
#
# Examples:
#
#   ./revoke-stale-consent.sh cdc21793-93e1-430e-a76e-93da38586adb grafana
#   ./revoke-stale-consent.sh cdc21793-93e1-430e-a76e-93da38586adb all
#
# Defaults to nova-a's Hydra admin endpoint (loopback inside the
# selfservice-ui / hydra-admin container network); override via
# HYDRA_ADMIN env var for vega / other sites:
#
#   HYDRA_ADMIN=http://127.0.0.1:4445 ./revoke-stale-consent.sh ...
#
# Designed to be run from inside the mgmt-host container network (where
# the loopback admin endpoint resolves), or from an operator laptop
# with the mesh-IP + admin-endpoint port-forward set up.
#
# Safe to run multiple times — Hydra accepts repeat revoke requests
# idempotently. No state-changing side effects on the identity itself
# (Kratos session, traits, anything in Kratos) — this ONLY touches
# Hydra's stored OAuth2 consent session table.

set -eu

HYDRA_ADMIN="${HYDRA_ADMIN:-http://127.0.0.1:4445}"
SUBJECT="${1:?usage: revoke-stale-consent.sh <kratos-identity-id> <oauth2-client-id|all>}"
CLIENT="${2:?usage: revoke-stale-consent.sh <kratos-identity-id> <oauth2-client-id|all>}"

if ! command -v curl >/dev/null 2>&1; then
  echo "revoke-stale-consent.sh: curl not available" >&2
  exit 1
fi

# Pre-flight Hydra admin reachability.
if ! curl -sf "${HYDRA_ADMIN}/health/ready" >/dev/null; then
  echo "revoke-stale-consent.sh: Hydra admin not reachable at ${HYDRA_ADMIN}/health/ready" >&2
  echo "  Override via HYDRA_ADMIN env var if you're running from a different network." >&2
  exit 1
fi

if [ "$CLIENT" = "all" ]; then
  URL="${HYDRA_ADMIN}/admin/oauth2/auth/sessions/consent?subject=${SUBJECT}&all=true"
  echo "revoke-stale-consent.sh: revoking ALL consent sessions for subject=${SUBJECT}"
else
  URL="${HYDRA_ADMIN}/admin/oauth2/auth/sessions/consent?subject=${SUBJECT}&client=${CLIENT}&all=true"
  echo "revoke-stale-consent.sh: revoking consent sessions for subject=${SUBJECT} client=${CLIENT}"
fi

# DELETE returns 204 No Content on success.
http_code=$(curl -sf -o /dev/null -w '%{http_code}' -X DELETE "$URL" || echo "FAIL")
case "$http_code" in
  204)
    echo "revoke-stale-consent.sh: OK (HTTP 204)"
    ;;
  *)
    echo "revoke-stale-consent.sh: FAIL — Hydra returned HTTP ${http_code}" >&2
    exit 1
    ;;
esac

# Also revoke any outstanding tokens for the subject so a session
# still cached in a relying party (Grafana keeps refresh tokens for
# ~1h by default) doesn't paper over the fix.
TOKEN_URL="${HYDRA_ADMIN}/admin/oauth2/tokens?client_id=${CLIENT}"
if [ "$CLIENT" != "all" ]; then
  echo "revoke-stale-consent.sh: revoking access tokens for client=${CLIENT}"
  curl -sf -o /dev/null -X DELETE "$TOKEN_URL" 2>/dev/null || true
fi

echo "revoke-stale-consent.sh: done. Next login from this subject re-runs through the consent UI."
