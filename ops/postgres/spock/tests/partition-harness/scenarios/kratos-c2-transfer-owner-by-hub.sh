# shellcheck shell=bash disable=SC2034  # sourced by ../run.sh, which reads EXPECT
# ADR 1205 D5: an identity's owner moves only by a transfer_owner directive
# from the hub, carried out by the current owner. Site A owns the identity;
# the hub (A) transfers it to site B while cut off. After heal both sites hold
# owner_site = site_b, the person's login identifier still resolves to the
# same identity at both, and B is now the site whose edit is carried out.
# B's applier runs after A's row has reached it, so B is by then the owner and
# applies the same directive again: a transfer_owner is idempotent (the same
# owner_site), so both sites record it applied.

EMAIL="h5-$RUN@harness.test"
EXPECT=""

seed() { kratos_create a "$EMAIL" "H5 person" > "$SCRATCH/id"; }
write_a() {
  directive a transfer_owner "$(cat "$SCRATCH/id")" '{"site":"site_b"}' > "$SCRATCH/dir"
  apply_directives a
}
after_heal() {
  apply_directives b
  kapi b PATCH "/admin/identities/$(cat "$SCRATCH/id")" '[{"op":"replace","path":"/traits/name","value":"H5 edited by new owner"}]' >/dev/null
  sync_all
}

check() {
  local id s; id=$(cat "$SCRATCH/id")
  expect_eq "a: directive verdict" "$(verdict a "$(cat "$SCRATCH/dir")")" "applied site_a"
  expect_eq "b: directive verdict" "$(verdict b "$(cat "$SCRATCH/dir")")" "applied site_a"
  for s in a b; do
    expect_eq "$s: owner_site" "$(kr "$s" "SELECT metadata_admin->>'owner_site' FROM identities WHERE id='$id';")" site_b
    expect_eq "$s: name"       "$(kname "$s" "$id")" "H5 edited by new owner"
    expect_eq "$s: login identifier rows for the email" \
      "$(kr "$s" "SELECT count(*) FROM identity_credential_identifiers ci JOIN identity_credentials c ON c.id = ci.identity_credential_id WHERE c.identity_id='$id' AND ci.identifier='$EMAIL';")" 1
  done
}
