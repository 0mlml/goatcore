#!/usr/bin/env bash
# reissue-3-relay-bundles.sh — F-057 / Block 22K.8 follow-on helper
#
# Re-issues the 3 cutover bundles (vega-A + vega-B + prov-rla) with
# relay-rla restored to the roster after F-057 recovery. Output bundles
# are written to ~/.goat/state/<peer>/bundle.cbor (mode 0600),
# matching the location used by docs/operations/wg-cp0-retroactive-re-enrollment.md.
#
# PRE-REQS:
#   - relay-rla ssm-agent ConnectionLost has been resolved (PingStatus=Online)
#     and the wg-cp0 pubkey for relay-rla has been captured.
#   - Operator has aws-vault usareur-af creds AND the offline-CA YubiKey/age key
#     (whichever mode is active per docs/adr/0406).
#   - bundle-create binary is built (cd ops/enrollment/cmd/bundle-create && go build).
#   - The operator-curated wg-cp0-bundle-allowlist.json exists and references all
#     three peers.
#
# Usage:
#
#   1. Capture relay-rla wg-cp0 pubkey (after recovery):
#      OP_IP=<your IP>
#      ssh ubuntu@<relay-rla public IP> 'sudo cat /etc/wireguard/wg-cp0-priv.key | wg pubkey'
#      (Then close SSH cidr per ops/scripts/diagnose-relay-rla-ssm.sh.)
#
#   2. Update ops/enrollment/wg-cp0-relays-prod.json to re-add relay-rla:
#      The git-ignored operator-side roster — append a relay-rla entry with
#      endpoint 203.0.113.1:51821, address 198.18.0.2, pubkey <captured>.
#
#   3. Run this script. It auto-discovers existing peer bundles in
#      ~/.goat/state/<peer>/, re-mints them with the 3-relay roster,
#      and replaces in place. Existing bundle's CA-mode (dev/prod) is preserved.
#
# After re-issue: re-run docs/operations/wg-cp0-retroactive-re-enrollment.md
# Path B (per-peer) to push fresh bundles. Each peer's wg-cp0-bundle-apply will
# render a 3-relay wg-cp0.conf this time. Cutover is idempotent — same procedure
# as the original 2-relay cutover.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
RELAYS_FILE="${REPO_ROOT}/ops/enrollment/wg-cp0-relays-prod.json"
ALLOWLIST_FILE="${REPO_ROOT}/ops/enrollment/wg-cp0-bundle-allowlist.json"
BUNDLE_CREATE_BIN="${REPO_ROOT}/ops/enrollment/cmd/bundle-create/bundle-create"
BUNDLE_EXTRACT_BIN="${REPO_ROOT}/ops/enrollment/cmd/bundle-extract/bundle-extract"
CA_KEY_AGE="${REPO_ROOT}/ops/enrollment/ca/dev/ca.key.age"
CA_ROOT_CERT="${REPO_ROOT}/ops/enrollment/ca/dev/root-cert.pem"
CA_ID_DEFAULT="dev-goat-ca-2026-04-20"
AGE_IDENTITY="${HOME}/.config/age/keys.txt"
STATE_DIR="${HOME}/.goat/state"
PEERS=("vega-a" "vega-b" "prov-rla")
FIRST_RELAY_ROUTE_SUBNET="198.18.0.0/24"

err() { echo "ERROR: $*" >&2; exit 2; }

[ -x "$BUNDLE_CREATE_BIN" ]  || err "bundle-create binary not found at $BUNDLE_CREATE_BIN — build via: (cd ops/enrollment/cmd/bundle-create && go build)"
[ -x "$BUNDLE_EXTRACT_BIN" ] || err "bundle-extract binary not found at $BUNDLE_EXTRACT_BIN — build via: (cd ops/enrollment/cmd/bundle-extract && go build)"
[ -r "$RELAYS_FILE" ]        || err "$RELAYS_FILE missing — re-add relay-rla entry per script header"
[ -r "$ALLOWLIST_FILE" ]     || err "$ALLOWLIST_FILE missing — operator-curated peer allowlist required"
[ -r "$CA_KEY_AGE" ]         || err "$CA_KEY_AGE missing — dev-mode CA key required"
[ -r "$AGE_IDENTITY" ]       || err "$AGE_IDENTITY missing — operator's age identity for unwrap"

# Sanity: relays file must contain all three relays
n_relays=$(jq 'length' "$RELAYS_FILE")
if [ "$n_relays" -lt 3 ]; then
  err "$RELAYS_FILE has $n_relays entries; need 3 (relay-rla + relay-rlb + relay-rlc). Re-add relay-rla per script header."
fi
if ! jq -e '.[] | select(.name == "relay-rla")' "$RELAYS_FILE" > /dev/null; then
  err "$RELAYS_FILE does not contain relay-rla. Re-add per script header."
fi

echo "Roster (3 relays expected):"
jq -r '.[] | "  " + .name + " @ " + .endpoint + " (" + .address + ")"' "$RELAYS_FILE"
echo

# Look up CA mode default — this script does NOT switch modes. If the
# existing peer bundles were minted in dev-mode, the new ones stay dev-mode.
# Operator can override with --mode prod via env.
MODE="${BUNDLE_CREATE_MODE:-dev}"
echo "CA mode: $MODE (override via BUNDLE_CREATE_MODE=prod)"
echo

for peer in "${PEERS[@]}"; do
  peer_dir="${STATE_DIR}/${peer}"
  if [ ! -d "$peer_dir" ]; then
    echo "WARN: ${peer}: no existing bundle dir at ${peer_dir}. Skipping."
    continue
  fi
  bundle_path="${peer_dir}/bundle.cbor"
  if [ ! -e "$bundle_path" ]; then
    echo "WARN: ${peer}: no existing bundle at ${bundle_path}. Skipping."
    continue
  fi

  # Read site + device_id + acl_groups from the existing bundle so we
  # preserve whatever was issued previously (vega uses VEGA,VEGA-MGMT;
  # prov-rla uses PROV-FRA,PROV-FRA-MGMT — different sites have different
  # ACL groups per docs/design/mesh-acl.md).
  device_id=$("$BUNDLE_EXTRACT_BIN" --field device_id "$bundle_path")
  site=$("$BUNDLE_EXTRACT_BIN" --field site "$bundle_path")
  acl_groups=$("$BUNDLE_EXTRACT_BIN" --field acl_groups "$bundle_path")
  ca_id=$("$BUNDLE_EXTRACT_BIN" --field ca_id "$bundle_path" 2>/dev/null || echo "$CA_ID_DEFAULT")

  # Address inside wg-cp0 mesh — name → address per inventory.
  case "$peer" in
    vega-a)  cp_address="198.18.0.1/24" ;;
    vega-b)  cp_address="198.18.0.5/24" ;;
    prov-rla)  cp_address="198.18.0.6/24" ;;
    *)         err "unknown peer address: $peer" ;;
  esac

  echo "=== ${peer}"
  echo "  device_id:  $device_id"
  echo "  site:       $site"
  echo "  acl_groups: $acl_groups"
  echo "  ca_id:      $ca_id"
  echo "  address:    $cp_address"
  echo "  pre size:   $(stat -f%z "$bundle_path" 2>/dev/null || stat -c%s "$bundle_path") bytes"
  echo "  out:        $bundle_path"

  bundle_args=(
    --mode "$MODE"
    --site "$site"
    --device-id "$device_id"
    --wg-cp0-address "$cp_address"
    --wg-cp0-relays "$RELAYS_FILE"
    --acl-groups "$acl_groups"
    --ca-id "$ca_id"
    --update-allowlist "$ALLOWLIST_FILE"
    --first-relay-route-subnet "$FIRST_RELAY_ROUTE_SUBNET"
    --out "$bundle_path"
  )
  if [ "$MODE" = "dev" ]; then
    bundle_args+=(--ca-key "$CA_KEY_AGE" --age-identity "$AGE_IDENTITY")
  fi
  if [ -r "$CA_ROOT_CERT" ]; then
    bundle_args+=(--tls-trust-anchor "$CA_ROOT_CERT")
  fi

  "$BUNDLE_CREATE_BIN" "${bundle_args[@]}"
  chmod 600 "$bundle_path"
  echo "  post size:  $(stat -f%z "$bundle_path" 2>/dev/null || stat -c%s "$bundle_path") bytes"
  echo
done

echo "Done. Re-run docs/operations/wg-cp0-retroactive-re-enrollment.md Path B"
echo "for each peer to push the new 3-relay bundles. Cutover is idempotent;"
echo "each peer will render a 3-relay wg-cp0.conf in the next wg-cp0-bundle-apply."
