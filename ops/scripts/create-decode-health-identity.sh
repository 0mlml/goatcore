#!/usr/bin/env bash
# Mint the portal service key topic-decode-health-guard needs, on one goatnet.
#
# OPERATOR-RUN, NEVER A PLAYBOOK SIDE EFFECT (live-state-guard-scheduling.md §4).
# The role consumes what this prints and REFUSES to install without it.
#
# ONE plane, unlike create-provenance-guard-identity.sh: this guard reads the
# portal registry and the goatnet's own zenoh router, and the router takes no
# auth on the paths it reads. So there is no NetBird PAT here and nothing in
# this script needs mesh admin — which is worth noticing, because it makes this
# the cheapest of the three guards to stand up.
#
# Usage:
#   ops/scripts/create-decode-health-identity.sh <goatnet> [--dry-run]
#
# Needs, in the environment:
#   MGMT_HOST         ssh target of the mgmt host, for the portal key
#   MGMT_SSH_OPTS     extra ssh options, UNQUOTED and word-split on purpose —
#                     e.g. through an env door (ADR 1117):
#                       MGMT_HOST=goat@127.0.0.1
#                       MGMT_SSH_OPTS="-p 12202 -o StrictHostKeyChecking=no"
#                     Folding these into MGMT_HOST does not work: ssh gets one
#                     quoted argument and reports "Bad port".
#   PORTAL_URL        that goatnet's portal, for the load check
#   PORTAL_KEYS_DIR   portal operator-key dir (default below)
set -euo pipefail

GOATNET="${1:-}"
DRY_RUN=0
[ "${2:-}" = "--dry-run" ] && DRY_RUN=1

if [ -z "$GOATNET" ]; then
  echo "usage: $0 <goatnet> [--dry-run]" >&2
  exit 64
fi

# NOTE: no apostrophes inside ${VAR:?...}. Bash starts a quote context on one
# even inside double quotes, and the whole script fails to parse with an error
# pointing at a line far below the real one.
: "${MGMT_HOST:?set MGMT_HOST — ssh target of the mgmt host for this goatnet}"
PORTAL_KEYS_DIR="${PORTAL_KEYS_DIR:-/opt/goat-zenoh-portal/operator-keys.d}"
# Deliberately unquoted at use: these are multiple ssh words, not one argument.
MGMT_SSH_OPTS="${MGMT_SSH_OPTS:-}"

# Shell var names take no hyphens; site-a -> SITE_A. Printing a name with
# a hyphen in it looks copy-pasteable and is not.
# tr does the upcasing too — ${var^^} is bash 4 only and this script runs on
# operator laptops, where macOS ships bash 3.2.
ENV_PREFIX="$(printf %s "$GOATNET" | tr "a-z" "A-Z" | tr -c "A-Z0-9" "_")"

say() { printf '\n\033[1m%s\033[0m\n' "$*"; }

say "1/1  Portal operator service key"
portal_key="gdh_$(head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n')"
if [ "$DRY_RUN" = "1" ]; then
  echo "     [dry-run] would write ${PORTAL_KEYS_DIR}/topic-decode-health-guard.key on ${MGMT_HOST}"
  say "dry run — nothing created."
  exit 0
fi

# The portal hot-reloads this directory, so registering the key is a file write
# and revoking it is a file delete — no portal redeploy either way.
#
# TWO THINGS THAT FAIL SILENTLY FROM THE WRITER'S SIDE, both avoided here:
#
#   1. FORMAT. The file must contain `handle:key` lines, not a bare key —
#      config.OperatorAPIKeysDir parses one colon-separated pair per line and
#      returns a hard error otherwise. A bare key makes the portal log
#      "operator-key reload failed; SERVING THE PREVIOUS SET" every 30s. No
#      existing key breaks (that fail-safe is deliberate and good), but the new
#      one never loads and the only evidence is in the portal's own log.
#   2. OWNERSHIP. The portal container runs as uid 65532 and cannot read a
#      root-owned 0640 file. Every other key in that directory is 65532:65532;
#      `sudo tee` made this one root:root.
printf 'topic-decode-health-guard:%s\n' "$portal_key" | ssh $MGMT_SSH_OPTS "$MGMT_HOST" \
  "sudo install -d -m 0750 -o 65532 -g 65532 '${PORTAL_KEYS_DIR}' && sudo tee '${PORTAL_KEYS_DIR}/topic-decode-health-guard.key' >/dev/null && sudo chown 65532:65532 '${PORTAL_KEYS_DIR}/topic-decode-health-guard.key' && sudo chmod 0640 '${PORTAL_KEYS_DIR}/topic-decode-health-guard.key'"
echo "     registered at ${PORTAL_KEYS_DIR}/topic-decode-health-guard.key"

# Prove it loaded rather than assuming the write was the outcome. The portal
# rereads the directory on its own interval, so poll instead of racing it.
echo "     waiting for the portal to load it..."
for _ in $(seq 1 10); do
  code="$(ssh $MGMT_SSH_OPTS "$MGMT_HOST" "curl -s -o /dev/null -w '%{http_code}' -H 'Authorization: Bearer ${portal_key}' '${PORTAL_URL:?set PORTAL_URL}/api/orgs'" 2>/dev/null || true)"
  [ "$code" = "200" ] && { echo "     portal accepts it (200)"; break; }
  sleep 6
done
if [ "${code:-}" != "200" ]; then
  echo "WARN: the portal still answers ${code:-<none>} for this key. Check its log:" >&2
  echo "      ssh ${MGMT_SSH_OPTS} ${MGMT_HOST} 'sudo docker logs --since 5m goat-zenoh-portal | grep operator-key'" >&2
  echo "      A reload error there names the offending line and keeps serving the previous set." >&2
fi

cat <<EOF

──────────────────────────────────────────────────────────────────────────────
Put this in this goatnet's .envrc.local.d entry, then set the group_vars to
read it.

  ${ENV_PREFIX}_DECODE_HEALTH_PORTAL_KEY=${portal_key}

You also need two NON-SECRET values in that goatnet's group_vars, and the
router one is the easy thing to get wrong — it is the router's REST endpoint as
reachable FROM THE MGMT HOST, which is a container name, not a mesh hostname:

  topic_decode_health_guard_portal_url: "https://portal.<...>"
  topic_decode_health_guard_router_url: "http://<zenoh_stack>-router:8000"

To revoke later:
  ssh ${MGMT_SSH_OPTS} ${MGMT_HOST} 'sudo rm ${PORTAL_KEYS_DIR}/topic-decode-health-guard.key'
  (hot-reloaded; no redeploy)

Then deploy the role. Its first run happens inline with you watching, and the
play FAILS if that run exits 2 — a guard that cannot read its sources must not
be installed quietly.
──────────────────────────────────────────────────────────────────────────────
EOF
