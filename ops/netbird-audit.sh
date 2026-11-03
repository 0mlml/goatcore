#!/usr/bin/env bash
# netbird-audit.sh — Audit Netbird mesh configuration (ACLs, keys, peers, groups)
#
# Requires: NETBIRD_API_TOKEN set in environment (source .envrc.local)
#
# Usage:
#   ./ops/netbird-audit.sh              # full audit
#   ./ops/netbird-audit.sh peers        # peers only
#   ./ops/netbird-audit.sh keys         # setup keys only
#   ./ops/netbird-audit.sh policies     # ACL policies only
#   ./ops/netbird-audit.sh groups       # groups only

set -euo pipefail

: "${NETBIRD_API_TOKEN:?Set NETBIRD_API_TOKEN (source .envrc.local)}"
NB_URL="${NETBIRD_DOMAIN:-https://netbird.legacy.example}"

api() { curl -sf -H "Authorization: Token $NETBIRD_API_TOKEN" "$NB_URL/api/$1" 2>/dev/null; }

audit_peers() {
    echo "=== PEERS ==="
    api peers | jq -r '
        .[] | [.name, .ip, (if .connected then "ONLINE" else "offline" end), .os, .last_seen[:10]] | @tsv
    ' | column -t -s $'\t'
    echo
    echo "Total peers: $(api peers | jq length)"
    echo "Online: $(api peers | jq '[.[] | select(.connected)] | length')"
    echo
}

audit_groups() {
    echo "=== GROUPS ==="
    api groups | jq -r '.[] | [.name, .id[:8], (.peers // [] | length | tostring) + " peers"] | @tsv' | column -t -s $'\t'
    echo
}

audit_policies() {
    echo "=== ACL POLICIES ==="
    local policies
    policies=$(api policies)
    local count
    count=$(echo "$policies" | jq length)

    if [ "$count" -eq 0 ]; then
        echo "  No policies defined (default behavior applies)"
    else
        echo "$policies" | jq -r '
            .[] | "Policy: \(.name) [\(if .enabled then "ENABLED" else "DISABLED" end)]",
            (.rules[] | "  Rule: \(.name // "unnamed")",
                "    Action: \(.action) | Bidirectional: \(.bidirectional)",
                "    Sources: \([.sources[]] | join(", "))",
                "    Destinations: \([.destinations[]] | join(", "))",
                "    Protocol: \(.protocol // "all") Ports: \([(.ports // [])[] ] | if length == 0 then "all" else join(", ") end)"
            ), ""
        '
    fi
    echo
    echo "--- ASSESSMENT ---"
    local all_count
    all_count=$(echo "$policies" | jq '[.[] | select(.enabled) | .rules[] | select(.protocol == "all" or .protocol == null)] | length')
    if [ "$all_count" -gt 0 ]; then
        echo "WARNING: $all_count rule(s) allow ALL protocols — lateral movement risk from compromised peer"
    else
        echo "OK: No blanket allow-all rules found"
    fi
    echo
}

audit_keys() {
    echo "=== SETUP KEYS ==="
    api setup-keys | jq -r '
        .[] | [
            .name,
            .type,
            .state,
            "used:" + (.used_times | tostring),
            (if .type == "reusable" then "REUSABLE" else "one-time" end),
            "expires:" + (.expires_at[:10] // "never")
        ] | @tsv
    ' | column -t -s $'\t'
    echo
    echo "--- ASSESSMENT ---"
    local reusable
    reusable=$(api setup-keys | jq '[.[] | select(.type == "reusable" and .state == "valid")] | length')
    if [ "$reusable" -gt 0 ]; then
        echo "WARNING: $reusable reusable key(s) active — a leaked key allows anyone to enroll"
        api setup-keys | jq -r '.[] | select(.type == "reusable" and .state == "valid") | "  \(.name): expires \(.expires_at[:10] // "NEVER"), used \(.used_times) times"'
    else
        echo "OK: No reusable keys active"
    fi
    echo
}

case "${1:-all}" in
    peers)    audit_peers ;;
    groups)   audit_groups ;;
    policies) audit_policies ;;
    keys)     audit_keys ;;
    all)
        audit_peers
        audit_groups
        audit_policies
        audit_keys
        ;;
    *)
        echo "Usage: $0 [peers|groups|policies|keys|all]"
        exit 1
        ;;
esac
