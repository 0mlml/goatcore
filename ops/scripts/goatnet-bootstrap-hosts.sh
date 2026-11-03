#!/usr/bin/env bash
# goatnet-bootstrap-hosts.sh — add, then RETIRE, the temporary /etc/hosts entries
# a new goatnet needs for its very first browser login.
#
# THE TRAP THIS EXISTS TO CLOSE 
# ------------------------------------------------------------
# A new goatnet has a bootstrap paradox at Phase 8:
#
#   - the mgmt names (dashboard / auth / oauth2 / mgmt.<site>.<zone>) resolve via
#     the goatnet's own mesh CoreDNS, which answers with the mgmt host's MESH ip;
#   - but nobody is on that mesh yet — the first peer cannot enrol until an
#     operator completes the first OIDC login, which mints the account Owner;
#   - so for exactly one step the operator laptop needs those names pinned to the
#     mgmt host's LAN ip.
#
# Those entries are correct for ~30 minutes and harmful forever after. Once the
# laptop enrols, /etc/hosts still wins over mesh DNS, so the browser keeps using
# the LAN path — which Traefik's mesh-only ipAllowList refuses. The symptom is
# baffling: the dashboard works, then an OIDC redirect to oauth2.<site> dies with
# ERR_ADDRESS_UNREACHABLE or a bare 403, long after the standup, with no obvious
# link to a hosts file somebody edited hours earlier.
#
# So: never hand-edit /etc/hosts for a standup. Use this, and run `retire` the
# moment the laptop is a mesh peer of that goatnet.
#
# Usage:
#   sudo ops/scripts/goatnet-bootstrap-hosts.sh add <site-id> <zone> <mgmt-lan-ip>
#   sudo ops/scripts/goatnet-bootstrap-hosts.sh retire <site-id>
#   ops/scripts/goatnet-bootstrap-hosts.sh check          # warn about stale blocks
#
# Example:
#   sudo ops/scripts/goatnet-bootstrap-hosts.sh add site-a netbird.example.net 192.0.2.10
#   ... first OIDC login, then enrol the laptop on the mesh ...
#   sudo ops/scripts/goatnet-bootstrap-hosts.sh retire site-a
set -euo pipefail

HOSTS=/etc/hosts
CMD="${1:-}"; SITE="${2:-}"

marker_begin() { echo "# >>> goat bootstrap-hosts: $1 (TEMPORARY — retire after mesh enrolment) >>>"; }
marker_end()   { echo "# <<< goat bootstrap-hosts: $1 <<<"; }

flush_dns() {
  if command -v dscacheutil >/dev/null 2>&1; then
    dscacheutil -flushcache 2>/dev/null || true
    killall -HUP mDNSResponder 2>/dev/null || true
  fi
}

case "$CMD" in
  add)
    ZONE="${3:-}"; IP="${4:-}"
    [ -n "$SITE" ] && [ -n "$ZONE" ] && [ -n "$IP" ] || {
      echo "usage: $0 add <site-id> <zone> <mgmt-lan-ip>" >&2; exit 64; }
    if grep -q "goat bootstrap-hosts: $SITE " "$HOSTS" 2>/dev/null; then
      echo "already present for '$SITE' — retire it first if the IP changed"; exit 0
    fi
    {
      echo ""
      marker_begin "$SITE"
      echo "# Added $(date -u +%Y-%m-%dT%H:%M:%SZ) so the FIRST OIDC login can happen"
      echo "# before any peer exists. RETIRE with:"
      echo "#   sudo $0 retire $SITE"
      for n in dashboard auth oauth2 mgmt; do
        echo "$IP $n.$SITE.$ZONE"
      done
      marker_end "$SITE"
    } >> "$HOSTS"
    flush_dns
    echo "added bootstrap entries for '$SITE' -> $IP"
    echo "REMINDER: these MUST be retired once the laptop enrols on the $SITE mesh."
    ;;

  retire)
    [ -n "$SITE" ] || { echo "usage: $0 retire <site-id>" >&2; exit 64; }
    if ! grep -q "goat bootstrap-hosts: $SITE " "$HOSTS" 2>/dev/null; then
      echo "no bootstrap block for '$SITE' — nothing to retire"; exit 0
    fi
    tmp=$(mktemp)
    awk -v site="$SITE" '
      $0 ~ ("goat bootstrap-hosts: " site " ") && /^# >>>/ { skip=1; next }
      $0 ~ ("goat bootstrap-hosts: " site " ") && /^# <<</ { skip=0; next }
      !skip { print }
    ' "$HOSTS" > "$tmp"
    cat "$tmp" > "$HOSTS"; rm -f "$tmp"
    flush_dns
    echo "retired bootstrap entries for '$SITE' — mesh DNS is now authoritative"
    ;;

  check)
    if grep -q "goat bootstrap-hosts:" "$HOSTS" 2>/dev/null; then
      echo "WARNING: bootstrap /etc/hosts blocks still present:"
      grep -oE "goat bootstrap-hosts: [a-z0-9-]+" "$HOSTS" | sort -u | sed 's/^/  - /'
      echo "If the laptop is a mesh peer of that goatnet, these are STALE and will"
      echo "break OIDC redirects. Retire with: sudo $0 retire <site-id>"
      exit 1
    fi
    echo "OK: no bootstrap /etc/hosts blocks"
    ;;

  *)
    echo "usage: $0 {add <site> <zone> <ip> | retire <site> | check}" >&2; exit 64 ;;
esac
