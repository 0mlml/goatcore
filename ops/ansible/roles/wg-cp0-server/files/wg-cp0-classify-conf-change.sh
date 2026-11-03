#!/usr/bin/env bash
# wg-cp0-classify-conf-change.sh — Block 22R.3 substrate.
#
# Classifies the diff between a live /etc/wireguard/<iface>.conf and a
# staged candidate so the role can pick between the two notify targets:
#
#   - reload wg-cp0 peers   (wg syncconf — preserves handshaked sessions)
#   - restart wg-cp0        (systemctl restart — drops all sessions; only
#                            correct for listener-shape changes)
#
# Distinguishing peer-set changes from listener-shape changes is the load-
# bearing closure of F-114's substrate-side mitigation: today the role's
# template task notifies "restart wg-cp0" on EVERY config change, including
# pure allowlist deltas, which forces every wg-cp0 client to re-handshake
# from scratch.
#
# Output (stdout, exactly one of):
#   first_install   live file is absent; new conf is the first ever
#   shape_change    [Interface] stanza differs (privkey/address/port/MTU)
#   peer_change     [Interface] identical; [Peer] stanzas differ
#   no_change       both stanzas byte-identical after normalization
#
# Exit code: 0 on any classification result; 2 on usage error.
#
# Usage: wg-cp0-classify-conf-change.sh <live.conf> <staged.conf>
#
# The staged file is the freshly-rendered candidate; live is the existing
# /etc/wireguard/<iface>.conf. Either may be missing — the script never
# fails because the file isn't there; it just classifies the situation.
#
# Normalization: blank lines and full-line comments are stripped; whitespace
# around `=` is collapsed; the [Interface] stanza is sort-compared (order-
# independent) while [Peer] stanzas are diff-compared (order matters because
# WireGuard's "first match wins on overlapping prefixes" is order-sensitive
# on the send side per ADR 0321 amendment).

set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo "usage: $0 <live.conf> <staged.conf>" >&2
  exit 2
fi

LIVE="$1"
STAGED="$2"

if [[ ! -f "$STAGED" ]]; then
  echo "error: staged conf not found: $STAGED" >&2
  exit 2
fi

if [[ ! -f "$LIVE" ]]; then
  echo "first_install"
  exit 0
fi

# Extract [Interface] stanza (everything from [Interface] up to the first
# [Peer] or EOF). Strip blank lines and comments, normalize whitespace
# around '=', and sort so key order inside the stanza doesn't matter.
extract_iface() {
  awk '
    /^\[Peer\]/ { exit }
    /^\[Interface\]/ { in_iface=1; next }
    in_iface { print }
  ' "$1" \
    | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//' \
    | grep -vE '^([[:space:]]*$|#)' \
    | sed -e 's/[[:space:]]*=[[:space:]]*/=/' \
    | sort
}

# Extract [Peer] stanzas (everything from the first [Peer] to EOF).
# Strip blank lines and comments and normalize whitespace, but DO NOT sort —
# peer order is observable via wg's first-match-wins routing.
extract_peers() {
  awk '
    /^\[Peer\]/ { in_peers=1 }
    in_peers { print }
  ' "$1" \
    | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//' \
    | grep -vE '^([[:space:]]*$|#)' \
    | sed -e 's/[[:space:]]*=[[:space:]]*/=/'
}

if ! diff -q <(extract_iface "$LIVE") <(extract_iface "$STAGED") >/dev/null 2>&1; then
  echo "shape_change"
  exit 0
fi

if ! diff -q <(extract_peers "$LIVE") <(extract_peers "$STAGED") >/dev/null 2>&1; then
  echo "peer_change"
  exit 0
fi

echo "no_change"
