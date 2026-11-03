#!/usr/bin/env bash
# issue-mgmt-stack-leaves.sh — turnkey ceremony to issue every mgmt-stack
# Traefik leaf cert for the prod fleet. Re-runnable; idempotent in the
# sense that each invocation re-issues every cert (each operator-fired
# rotation produces a fresh set). Output paths match the per-site
# `traefik_services[*].cert_src` / `key_src` paths in
# ops/ansible/group_vars/<site>_hosts/vars.yml so a follow-on
# `site-bootstrap.yml --tags traefik` (or per-service deploy) picks up
# the new material verbatim.
#
# Usage:
#
#   ./ops/scripts/issue-mgmt-stack-leaves.sh \
#     --ca-id dev-goat-ca-ecdsa-2026-05-09 \
#     --root-cert ops/enrollment/ca/dev-ecdsa/root-cert.pem \
#     --root-key  ops/enrollment/ca/dev-ecdsa/ca.key.age \
#     --age-identity "$HOME/.config/age/keys.txt" \
#     --out-dir "$HOME/.goat/state"
#
# Per-site SAN sets are encoded in the SITES table below. To add a new
# site, append one row to SITES and re-run; nothing else changes.
#
# Each site receives ONE multi-SAN leaf cert at <out-dir>/<site>/mgmt-stack-cert.pem
# covering: mgmt|auth|oauth2|dashboard.<site>.netbird-prod.goat.example + the
# site's wg-cp0 IP(s) + LAN IP(s) + 127.0.0.1.
#
# The ECDSA-P256 algorithm is selected automatically by cert-sign because
# the EKU is serverAuth (Block 79 closure — Chrome on macOS rejects Ed25519
# leaves at the TLS handshake; ECDSA-P256 is the universally-trusted
# operator-facing leaf algo).
#
# Block 79 / F-107 footnote: leaves are signed root-direct (no intermediate)
# because `ca-init` currently emits `pathLenConstraint=0` on both root and
# intermediate, which RFC 5280 §4.2.1.9 says forbids root → intermediate →
# leaf chains. ECDSA root imports cleanly into macOS keychain on its own
# so the F-090 closure path-B (install intermediate as trust anchor)
# rationale evaporates. The intermediate flags will land here once
# F-107's substrate fix (root pathlen lift + chain-verify smoke test in
# ca-init) ships.

set -euo pipefail

usage() {
  sed -n '/^# Usage:/,/^# Per-site SAN/p' "$0" | sed 's/^# //;s/^#//'
  exit 1
}

CA_ID=""
ROOT_CERT=""
ROOT_KEY=""
INTERMEDIATE_CERT=""
INTERMEDIATE_KEY=""
AGE_IDENTITY=""
OUT_DIR=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --ca-id)             CA_ID="$2"; shift 2 ;;
    --root-cert)         ROOT_CERT="$2"; shift 2 ;;
    --root-key)          ROOT_KEY="$2"; shift 2 ;;
    --intermediate-cert) INTERMEDIATE_CERT="$2"; shift 2 ;;
    --intermediate-key)  INTERMEDIATE_KEY="$2"; shift 2 ;;
    --age-identity)      AGE_IDENTITY="$2"; shift 2 ;;
    --out-dir)           OUT_DIR="$2"; shift 2 ;;
    -h|--help)           usage ;;
    *) echo "unknown flag: $1" >&2; usage ;;
  esac
done

# Required flags. Intermediate flags are optional (root-direct is the
# F-107-workaround default; will become required when F-107 lands).
for v in CA_ID ROOT_CERT ROOT_KEY AGE_IDENTITY OUT_DIR; do
  if [[ -z "${!v}" ]]; then
    echo "missing required flag for $v" >&2
    usage
  fi
done

# Build cert-sign on the fly so the operator doesn't have to remember.
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
CERT_SIGN_BIN="$REPO_ROOT/ops/enrollment/cmd/cert-sign/cert-sign"
if [[ ! -x "$CERT_SIGN_BIN" ]]; then
  echo "building cert-sign at $CERT_SIGN_BIN ..."
  ( cd "$REPO_ROOT/ops/enrollment" && go build -o "$CERT_SIGN_BIN" ./cmd/cert-sign )
fi

# SITES — append rows here when a site is added.
#   site_id : vega | prov-rla | prov-x | prov-conus | ...
#   ip_sans : space-separated; both Orin-A + Orin-B IPs for paired sites.
SITES=(
  "vega    198.18.0.1 198.18.0.5 192.168.1.132 192.168.1.128 127.0.0.1"
  "prov-rla  198.18.0.6 192.168.1.192 127.0.0.1"
  "prov-x  198.18.0.7 192.168.1.44 127.0.0.1"
  "prov-conus 198.18.0.8 192.168.1.191 127.0.0.1"
)

DNS_ZONE="netbird-prod.goat.example"

issue_one_site() {
  local site_id="$1"; shift
  local -a ip_sans=("$@")

  local site_dir="$OUT_DIR/$site_id"
  mkdir -p "$site_dir"

  local out_cert="$site_dir/mgmt-stack-cert.pem"
  local out_key="$site_dir/mgmt-stack-key.pem"

  # Roll backup if existing cert present (forward-recover safety net).
  # Existing key files are mode 0400 — cert-sign opens for write with
  # O_RDWR which fails on 0400 files. Backup, then remove so the next
  # write creates a fresh inode at the correct mode.
  local stamp
  stamp="$(date -u +%Y%m%dT%H%M%SZ)"
  if [[ -f "$out_cert" ]]; then
    cp "$out_cert" "$out_cert.bak.$stamp"
    rm -f "$out_cert"
  fi
  if [[ -f "$out_key" ]]; then
    cp "$out_key" "$out_key.bak.$stamp"
    rm -f "$out_key"
  fi

  # Build the cert-sign argv: per-service SAN block + IP SANs.
  local -a argv=(
    "$CERT_SIGN_BIN"
    "--root-cert"         "$ROOT_CERT"
    "--root-key"          "$ROOT_KEY"
    "--age-identity"      "$AGE_IDENTITY"
    "--ca-id"             "$CA_ID"
    "--site-id"           "$site_id"
    "--cn"                "mgmt.$site_id.$DNS_ZONE"
    "--validity"          "8760h"
    "--eku"               "serverAuth"
    "--out-cert"          "$out_cert"
    "--out-key"           "$out_key"
  )
  # Intermediate-as-issuer is opt-in. F-107: ca-init currently emits
  # pathlen:0 on root + intermediate, so root → intermediate → leaf
  # chain-verify fails. Until F-107 lands, default is root-direct.
  if [[ -n "$INTERMEDIATE_CERT" && -n "$INTERMEDIATE_KEY" ]]; then
    argv+=("--intermediate-cert" "$INTERMEDIATE_CERT")
    argv+=("--intermediate-key"  "$INTERMEDIATE_KEY")
  fi
  for svc in mgmt auth oauth2 dashboard; do
    argv+=("--san" "$svc.$site_id.$DNS_ZONE")
  done
  for ip in "${ip_sans[@]}"; do
    argv+=("--san" "$ip")
  done

  echo "==> $site_id: minting $out_cert"
  "${argv[@]}"
  echo "    OK ($(openssl x509 -in "$out_cert" -noout -subject -enddate))"
}

for row in "${SITES[@]}"; do
  read -ra fields <<<"$row"
  issue_one_site "${fields[@]}"
done

echo
echo "All mgmt-stack leaf certs minted under $OUT_DIR/<site>/mgmt-stack-{cert,key}.pem"
echo "Next: run site-bootstrap.yml --tags traefik against each site to push them."
