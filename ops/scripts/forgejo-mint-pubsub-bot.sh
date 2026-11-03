#!/usr/bin/env bash
# forgejo-mint-pubsub-bot.sh — one-time-per-goatnet mint of the pub/sub
# state-of-record mirror bot on a goatnet's Forgejo (Block 86 turnkey step).
#
# Creates the `pubsub-state-mirror` user AS AN ADMIN (deliberate — a non-admin
# user cannot create the goat-pubsub-state org via the API even with a full-scope
# token; the org-create permission is a user attribute, and Forgejo 7.x has no
# CLI to flip it on an existing user), generates a full-scope PAT, verifies it
# can create the org, and writes the token to the operator envrc under the
# requested variable name. Idempotent-ish: re-running recreates the bot + a fresh
# token (harmless; old tokens can be pruned in the Forgejo UI).
#
# The token is never printed to stdout/stderr.
#
# Usage:
#   ops/scripts/forgejo-mint-pubsub-bot.sh <ssh-target> [--envvar NAME] [--envrc FILE]
#
#   <ssh-target>  user@mesh-ip of the host running the Forgejo container, e.g.
#                 operator@obs-host.example (the obs host). On a flat goatnet
#                 this is the single mgmt host.
#   --envvar      env var to write the token to (default FORGEJO_PUBSUB_TOKEN;
#                 give a per-site name like SITE_A_FORGEJO_PUBSUB_TOKEN when a
#                 second goatnet with its own Forgejo also uses Repl).
#   --envrc       target envrc (default: 30-site-services.envrc in the MASTER
#                 checkout — resolved so it works from a worktree).

set -euo pipefail

SSH_TARGET="${1:-}"; shift || true
[ -n "$SSH_TARGET" ] || { echo "usage: forgejo-mint-pubsub-bot.sh <user@mesh-ip> [--envvar NAME] [--envrc FILE]" >&2; exit 1; }

ENVVAR="FORGEJO_PUBSUB_TOKEN"
ENVRC=""
while [ $# -gt 0 ]; do case "$1" in
  --envvar) ENVVAR="$2"; shift 2 ;;
  --envrc)  ENVRC="$2";  shift 2 ;;
  *) echo "unknown arg: $1" >&2; exit 1 ;;
esac; done

# Resolve the MASTER repo root even from a linked worktree (envrc lives there).
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# --path-format=absolute is load-bearing: bare --git-common-dir prints a path
# RELATIVE to the -C directory in a main-worktree checkout, and the cd below
# resolves that against the CALLER's cwd (git >= 2.31). dirname alone strips
# the /.git component — the old ${GIT_COMMON%/.git} stripped it a second time
# and landed one level above the repo even when the path was absolute.
GIT_COMMON="$(git -C "$SCRIPT_DIR" rev-parse --path-format=absolute --git-common-dir 2>/dev/null || echo)"
# Empty GIT_COMMON (git absent/failed) must take the fallback explicitly:
# dirname "" is "." and would silently resolve to the CALLER's cwd. The
# previous unbraced `cd A && pwd || cd B && pwd` chain also left-associated,
# so pwd ran again after the success branch — MASTER_ROOT came back as
# "path\npath" and the default ENVRC test could never pass.
if [ -n "$GIT_COMMON" ]; then
  MASTER_ROOT="$(cd "$(dirname "$GIT_COMMON")" 2>/dev/null && pwd)" \
    || MASTER_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
else
  MASTER_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
fi
ENVRC="${ENVRC:-${MASTER_ROOT}/.envrc.local.d/30-site-services.envrc}"
[ -f "$ENVRC" ] || { echo "envrc not found: $ENVRC" >&2; exit 1; }

SSH=(ssh -o ProxyJump=none -o StrictHostKeyChecking=no -o ConnectTimeout=8 -o BatchMode=yes "$SSH_TARGET")

echo "==> minting pubsub-state-mirror ADMIN bot on $SSH_TARGET"
"${SSH[@]}" 'sudo docker exec -u git forgejo forgejo admin user delete --username pubsub-state-mirror' >/dev/null 2>&1 || true
"${SSH[@]}" 'sudo docker exec -u git forgejo forgejo admin user create \
  --username pubsub-state-mirror --email pubsub-state-mirror@goat.local \
  --random-password --must-change-password=false --admin' 2>&1 | grep -vi password | tail -1

TOKEN="$("${SSH[@]}" "sudo docker exec -u git forgejo forgejo admin user generate-access-token \
  --username pubsub-state-mirror --scopes all --token-name pubsub-mirror --raw" 2>/dev/null | tail -1 | tr -d '[:space:]')"
[ "${#TOKEN}" -ge 30 ] || { echo "ERROR: token generation failed (len ${#TOKEN})" >&2; exit 1; }

echo "==> verifying the token can create the goat-pubsub-state org"
CODE="$("${SSH[@]}" "curl -s -o /dev/null -w '%{http_code}' -X POST http://127.0.0.1:3030/api/v1/orgs \
  -H 'Authorization: token $TOKEN' -H 'Content-Type: application/json' \
  -d '{\"username\":\"goat-pubsub-state\",\"visibility\":\"private\"}'" 2>/dev/null)"
case "$CODE" in
  201) echo "    org created (201)";;
  422) echo "    org already exists (422) — OK";;
  *)   echo "ERROR: org-create returned $CODE (expected 201/422); bot lacks org-create rights" >&2; exit 1;;
esac

echo "==> writing \$$ENVVAR to $ENVRC"
if grep -q "^export ${ENVVAR}=" "$ENVRC"; then
  # portable in-place update (GNU or BSD sed)
  tmp="$(mktemp)"; sed "s|^export ${ENVVAR}=.*|export ${ENVVAR}=\"${TOKEN}\"|" "$ENVRC" > "$tmp" && mv "$tmp" "$ENVRC"
  echo "    updated existing ${ENVVAR}"
else
  { echo ""; echo "# pubsub-state mirror bot PAT (Forgejo on ${SSH_TARGET}) — $(date -u +%Y-%m-%d)"; \
    echo "export ${ENVVAR}=\"${TOKEN}\""; } >> "$ENVRC"
  echo "    appended ${ENVVAR}"
fi
echo "done. direnv reload / new shell to pick it up, then run pubsub-state-mirror-deploy.yml."
