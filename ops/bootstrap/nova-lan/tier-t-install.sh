#!/usr/bin/env bash
# tier-t-install.sh — closed-LAN Tier-T client installer.
#
# Imports the nova-lan-ca root certificate into the host trust store
# and configures the resolver for the nova.lan zone to point at
# nova-lan-A's lan-coredns instance.
#
# Phase B.3 of docs/design/nova-lan-instance.md. Pack layout at
# ops/bootstrap/nova-lan/pack-layout.md.
#
# Linux + systemd-resolved is the floor target. macOS / Windows
# operator workstations follow per-OS instructions in
# docs/operations/nova-lan-cert-issuance.md § "Importing the CA root."
#
# Usage:
#   sudo ./tier-t-install.sh --pack <dir>
#   sudo ./tier-t-install.sh --pack <dir> --force
#   sudo ./tier-t-install.sh --help

set -euo pipefail

STATE_DIR=/var/lib/goat
STATE_FILE="$STATE_DIR/nova-lan-tier-t-state.json"
LOG_DIR=/var/log/goat
JSONL="$LOG_DIR/nova-lan-tier-t.jsonl"

TRUST_DIR=/usr/local/share/ca-certificates
TRUST_ANCHOR="$TRUST_DIR/nova-lan-ca-root.crt"

PACK=""
FORCE=0

usage() {
  cat <<EOF
tier-t-install.sh — closed-LAN Tier-T client installer

Usage:
  sudo $0 --pack <dir>           Install from pack directory
  sudo $0 --pack <dir> --force   Re-run even if previously complete
  $0 --help                      Show this message

Pack layout: ops/bootstrap/nova-lan/pack-layout.md

Importing the CA root manually on non-Debian-family Linux, macOS, or
Windows: see docs/operations/nova-lan-cert-issuance.md.
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --pack)  PACK="$2"; shift 2 ;;
    --force) FORCE=1; shift ;;
    --help|-h) usage; exit 0 ;;
    *) echo "unknown arg: $1" >&2; usage >&2; exit 2 ;;
  esac
done

mkdir -p "$STATE_DIR" "$LOG_DIR"

emit() {
  # emit <phase> <status> [<extra-json>]
  local phase=$1 status=$2 extra=${3:-"{}"}
  local now ts_unix
  now=$(date -u +%Y-%m-%dT%H:%M:%SZ)
  ts_unix=$(date +%s)
  printf '{"ts":"%s","ts_unix":%s,"phase":"%s","status":"%s","extra":%s}\n' \
    "$now" "$ts_unix" "$phase" "$status" "$extra" >> "$JSONL"
}

die() {
  emit "tier-t-install.fail" "fail" "{\"reason\":\"$1\"}"
  echo "tier-t-install.sh: $1" >&2
  exit 1
}

# -- Re-run guard --------------------------------------------------------
if [ -f "$STATE_FILE" ] && [ "$FORCE" -eq 0 ]; then
  if grep -q '"complete":[[:space:]]*true' "$STATE_FILE" 2>/dev/null; then
    emit "tier-t-install.skip" "ok" '{"reason":"already-complete"}'
    echo "tier-t-install.sh: already complete; pass --force to re-run."
    exit 0
  fi
fi

[ "$EUID" -eq 0 ] || die "must run as root (CA-trust-store write + resolver config require it)"

emit "tier-t-install.start" "ok"

# -- Phase 1: validate pack ----------------------------------------------
emit "pack.validate" "start"
[ -n "$PACK" ] || die "--pack <dir> is required"
[ -d "$PACK" ] || die "pack dir not found: $PACK"
PACK_CA="$PACK/ca/nova-lan-ca-root.pem"
PACK_RESOLVER="$PACK/dns/resolver-ip.txt"
PACK_MANIFEST="$PACK/manifest.yml"

[ -f "$PACK_CA" ] || die "missing $PACK_CA"
[ -f "$PACK_RESOLVER" ] || die "missing $PACK_RESOLVER"
[ -f "$PACK_MANIFEST" ] || die "missing $PACK_MANIFEST"

# Schema-version check (manifest authoritative; refuse unknown versions).
SCHEMA_VERSION="$(grep -E '^schema_version:' "$PACK_MANIFEST" | awk '{print $2}')"
[ "$SCHEMA_VERSION" = "1" ] || die "unsupported manifest schema_version: $SCHEMA_VERSION"

# CA SHA-256 check.
EXPECTED_SHA="$(grep -E '^ca_sha256:' "$PACK_MANIFEST" | awk '{print $2}')"
[ -n "$EXPECTED_SHA" ] || die "manifest missing ca_sha256"
ACTUAL_SHA="$(sha256sum "$PACK_CA" | awk '{print $1}')"
[ "$EXPECTED_SHA" = "$ACTUAL_SHA" ] \
  || die "CA SHA-256 mismatch: pack manifest says $EXPECTED_SHA, on-disk is $ACTUAL_SHA"

RESOLVER_IP="$(tr -d '\n\r' < "$PACK_RESOLVER")"
[ -n "$RESOLVER_IP" ] || die "dns/resolver-ip.txt is empty"

# Manifest resolver_ip cross-check.
MANIFEST_RESOLVER="$(grep -E '^resolver_ip:' "$PACK_MANIFEST" | awk '{print $2}')"
[ "$MANIFEST_RESOLVER" = "$RESOLVER_IP" ] \
  || die "resolver_ip mismatch: manifest $MANIFEST_RESOLVER, dns/resolver-ip.txt $RESOLVER_IP"

emit "pack.validate" "ok" "{\"schema_version\":$SCHEMA_VERSION,\"ca_sha256\":\"$EXPECTED_SHA\",\"resolver_ip\":\"$RESOLVER_IP\"}"

# -- Phase 2: import CA root --------------------------------------------
emit "ca.import" "start"
if ! command -v update-ca-certificates >/dev/null 2>&1; then
  die "update-ca-certificates not found — non-Debian-family distro. See docs/operations/nova-lan-cert-issuance.md for per-OS import."
fi
install -m 0644 "$PACK_CA" "$TRUST_ANCHOR"
update-ca-certificates >/dev/null
emit "ca.import" "ok" "{\"path\":\"$TRUST_ANCHOR\"}"

# -- Phase 3: configure resolver ----------------------------------------
emit "resolver.configure" "start"
RESOLVER_MODE=""
if systemctl is-active --quiet systemd-resolved 2>/dev/null; then
  # Preferred path: systemd-resolved per-domain routing on the primary link.
  # We don't know the primary link name programmatically without parsing
  # ip(8); pick the first link that has a default route.
  LINK="$(ip -4 route show default | awk '{print $5; exit}')"
  [ -n "$LINK" ] || die "could not determine primary network link for systemd-resolved"
  # Configure DNS on the link + restrict its scope to nova.lan via SetLinkDomains.
  resolvectl dns "$LINK" "$RESOLVER_IP"
  resolvectl domain "$LINK" "~nova.lan"
  RESOLVER_MODE="systemd-resolved:${LINK}"
else
  # Fallback: classic /etc/resolv.conf. This is a clobber-write; refuse
  # if the file isn't a static file (e.g. it's a symlink to systemd-resolved's
  # stub) AND systemd-resolved isn't running — operator confusion otherwise.
  if [ -L /etc/resolv.conf ]; then
    die "/etc/resolv.conf is a symlink but systemd-resolved is not active. Fix the resolver state before re-running."
  fi
  # Preserve existing entries; prepend our nameserver + add nova.lan to search.
  TMP="$(mktemp)"
  {
    echo "# nova-lan tier-t-install.sh: added $(date -u +%Y-%m-%dT%H:%M:%SZ)"
    echo "search nova.lan"
    echo "nameserver $RESOLVER_IP"
    grep -v '^nameserver\|^search\|^# nova-lan tier-t-install' /etc/resolv.conf 2>/dev/null || true
  } > "$TMP"
  install -m 0644 "$TMP" /etc/resolv.conf
  rm -f "$TMP"
  RESOLVER_MODE="resolv.conf"
fi
emit "resolver.configure" "ok" "{\"mode\":\"$RESOLVER_MODE\",\"resolver\":\"$RESOLVER_IP\"}"

# -- Phase 4: smoke test ------------------------------------------------
emit "smoke.test" "start"
if ! command -v dig >/dev/null 2>&1; then
  die "dig not found — install bind-utils / dnsutils or extend the script for non-dig hosts"
fi
SMOKE_RESULT="$(dig @"$RESOLVER_IP" portal.nova.lan +short +time=2 +tries=1 || true)"
[ -n "$SMOKE_RESULT" ] || die "smoke test failed: portal.nova.lan did not resolve at $RESOLVER_IP. lan-coredns reachable? Pack resolver_ip correct?"
emit "smoke.test" "ok" "{\"portal.nova.lan\":\"$SMOKE_RESULT\"}"

# -- Phase 5: state marker ----------------------------------------------
emit "state.write" "start"
cat > "$STATE_FILE" <<EOF
{
  "complete": true,
  "completed_at": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "trust_domain": "nova-lan-instance",
  "ca_sha256": "$EXPECTED_SHA",
  "resolver_ip": "$RESOLVER_IP",
  "resolver_mode": "$RESOLVER_MODE"
}
EOF
chmod 0644 "$STATE_FILE"
emit "state.write" "ok"

emit "tier-t-install.complete" "ok"
echo "tier-t-install.sh: complete. State: $STATE_FILE. Log: $JSONL"
