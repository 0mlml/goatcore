# shellcheck shell=bash disable=SC2034  # sourced by ../run.sh, which reads EXPECT
# Rows P10, N1. Class 1: new rows with site-unique ids.
# Pass beyond the common conditions: two keys.
#
# Driven by: SQL. The write is NetBird's SaveSetupKey for
# `POST /api/setup-keys`, the call `goat admin onboard <h> --native` makes
# (crates/goat-cli/src/cmd/admin.rs provision_netbird_native): one setup_keys
# row with an xid id; no accounts.network_serial bump (inventory §5).
# Both sites mint for the same handle, the worst case for "issued twice".

KA=$(xid); KB=$(xid)
EXPECT=""

write_a() { nb_mint_key a "$KA" harness-alice-device; }
write_b() { nb_mint_key b "$KB" harness-alice-device; }

check() {
  for s in a b; do
    expect_eq "$s: keys minted for harness-alice-device" \
      "$(nb "$s" "SELECT count(*) FROM setup_keys WHERE id IN ('$KA','$KB');")" 2
  done
}
