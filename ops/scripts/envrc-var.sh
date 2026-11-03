#!/usr/bin/env bash
# envrc-var.sh — print the `export NAME="…"` line a mint just appended to the
# operator envrc, so a running driver can load that ONE variable and go on,
# instead of stopping for the operator to re-source the file.
#
# Used by the standup's Forgejo mirror-bot step (goat-standup.sh and
# goatbuilder standup): forgejo-mint-pubsub-bot.sh writes the token to the
# MASTER checkout's .envrc.local.d/30-site-services.envrc, which a running
# shell has not re-read. The caller evals the output; nothing is echoed to the
# terminal, and no other variable from the file is loaded.
#
# usage: envrc-var.sh NAME [--envrc FILE]     exit 1 when NAME is not in the file
set -euo pipefail
NAME="${1:?usage: envrc-var.sh NAME [--envrc FILE]}"; shift || true
ENVRC=""
[ "${1:-}" = "--envrc" ] && ENVRC="${2:-}"
case "$NAME" in *[!A-Z0-9_]*|"") echo "envrc-var: bad variable name '$NAME'" >&2; exit 2 ;; esac
if [ -z "$ENVRC" ]; then
  SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  GIT_COMMON="$(git -C "$SCRIPT_DIR" rev-parse --path-format=absolute --git-common-dir 2>/dev/null || echo)"
  if [ -n "$GIT_COMMON" ]; then
    MASTER_ROOT="$(cd "$(dirname "$GIT_COMMON")" 2>/dev/null && pwd)" || MASTER_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
  else
    MASTER_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
  fi
  ENVRC="$MASTER_ROOT/.envrc.local.d/30-site-services.envrc"
fi
line="$(grep -E "^export ${NAME}=" "$ENVRC" 2>/dev/null | tail -n 1 || true)"
[ -n "$line" ] || { echo "envrc-var: $NAME is not in $ENVRC" >&2; exit 1; }
printf '%s\n' "$line"
