# shellcheck shell=bash disable=SC2034  # sourced by ../run.sh, which reads EXPECT
# Row N3. Class 3: revocations, ADR 1193 ledger (netbird.setup_key,
# netbird.setup_key.deleted). Carries over PR #1538's sk-late and sk-del.
# Pass beyond the common conditions: key revoked at both sites.
#
# Driven by: SQL. A: `PUT /api/setup-keys/{id}` revoked=true and
# `DELETE /api/setup-keys/{id}` (admin.rs revoke_keys; lifecycle-sweep.py
# purge lane). B, cut off: IncrementSetupKeyUsage on enrolment
# (store/sql_store.go), the ordinary later write to the same rows.

K_LATE=$(xid); K_DEL=$(xid)
EXPECT="
a netbird exception setup_keys UPDATE:other 1
b netbird resolution setup_keys update_update:keep_local 1
"

seed() {
  nb_mint_key a "$K_LATE" harness-revoke-late
  nb_mint_key a "$K_DEL"  harness-revoke-del
}
write_a() {
  nb a "UPDATE setup_keys SET revoked = true, updated_at = now() WHERE id = '$K_LATE';"
  nb a "DELETE FROM setup_keys WHERE id = '$K_DEL';"
}
write_b() {
  nb b "UPDATE setup_keys SET used_times = used_times + 1, last_used = now() WHERE id = '$K_LATE';"
  nb b "UPDATE setup_keys SET used_times = used_times + 1, last_used = now() WHERE id = '$K_DEL';"
}

check() {
  for s in a b; do
    expect_eq "$s: revoked key stays revoked" "$(nb "$s" "SELECT coalesce((SELECT revoked::text FROM setup_keys WHERE id='$K_LATE'),'<absent>');")" true
    expect_eq "$s: deleted key stays deleted" "$(nb "$s" "SELECT count(*) FROM setup_keys WHERE id='$K_DEL';")" 0
  done
}
