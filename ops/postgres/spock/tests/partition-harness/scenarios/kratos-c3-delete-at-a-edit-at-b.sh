# shellcheck shell=bash disable=SC2034  # sourced by ../run.sh, which reads EXPECT
# Rows C44, K11 (--purge). Class 3: a delete is a revocation, ADR 1193 ledger
# (kratos.identity.deleted), written as ADR 1205 D6 has it.
#
# Site B owns the identity. Cut off: A, not the owner, deletes it through the
# ledger; B, the owner, edits a trait. Pass beyond the common conditions: no
# identity, credential, identifier or address row survives at either site.

EMAIL="k5-$RUN@harness.test"
# B's cut-off edit reaches A after A deleted the identity: its row changes
# find nothing to change and are logged, and the guard drops its re-issued rows.
EXPECT="a kratos exception identities UPDATE:other 1
a kratos exception identity_credential_identifiers DELETE:other 1
a kratos exception identity_credential_identifiers INSERT:other 1
a kratos exception identity_credentials DELETE:other 1
a kratos exception identity_credentials INSERT:other 1
a kratos resolution identity_credential_identifiers delete_delete:skip 1
a kratos resolution identity_credentials delete_delete:skip 1"

seed() { kratos_create b "$EMAIL" "K5 person" > "$SCRATCH/id"; }
write_a() { kr a "SELECT goat_revoke('kratos.identity.deleted', '$(cat "$SCRATCH/id")', 'harness');" >/dev/null; }
write_b() { sleep 1; kapi b PATCH "/admin/identities/$(cat "$SCRATCH/id")" '[{"op":"replace","path":"/traits/name","value":"K5 edited at B"}]' >/dev/null; }

check() {
  local id; id=$(cat "$SCRATCH/id")
  for s in a b; do
    expect_eq "$s: identity rows"      "$(kr "$s" "SELECT count(*) FROM identities WHERE id='$id';")" 0
    expect_eq "$s: credential rows"    "$(kr "$s" "SELECT count(*) FROM identity_credentials WHERE identity_id='$id';")" 0
    expect_eq "$s: identifier rows"    "$(kr "$s" "SELECT count(*) FROM identity_credential_identifiers WHERE identifier='$EMAIL';")" 0
    expect_eq "$s: address rows"       "$(kr "$s" "SELECT (SELECT count(*) FROM identity_verifiable_addresses WHERE identity_id='$id')
                                                         + (SELECT count(*) FROM identity_recovery_addresses WHERE identity_id='$id');")" 0
  done
}
