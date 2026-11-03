# shellcheck shell=bash disable=SC2034  # sourced by ../run.sh, which reads EXPECT
# Rows C41, K1, K9. Class 4: the same email created at both sites while cut
# off. Spock logs the collision (the resolution queue, ADR 1165 D4). The hub
# resolves it (ADR 1205 D7): A's identity survives; identity_delete of B's is
# carried out by B, then identity_resave of A's by A, which re-issues A's
# credential rows so they reach B, which discarded them at heal.
# Pass beyond the common conditions: one identity with the email at both
# sites, and it is A's.

EMAIL="c4-$RUN@harness.test"
# The collision itself, at each site (the resolution queue, ADR 1165 D4), and
# the resolution's deletes of rows that site had already discarded.
EXPECT="a kratos exception identities INSERT:other 1
a kratos exception identity_credential_identifiers INSERT:unique 1
a kratos exception identity_credentials INSERT:other 1
a kratos exception identity_recovery_addresses INSERT:unique 1
a kratos exception identity_verifiable_addresses INSERT:unique 1
a kratos resolution identity_credential_identifiers delete_delete:skip 1
a kratos resolution identity_recovery_addresses delete_delete:skip 1
a kratos resolution identity_verifiable_addresses delete_delete:skip 1
b kratos exception identities INSERT:other 1
b kratos exception identity_credential_identifiers INSERT:unique 1
b kratos exception identity_credentials INSERT:other 1
b kratos exception identity_recovery_addresses INSERT:unique 1
b kratos exception identity_verifiable_addresses INSERT:unique 1
b kratos resolution identity_credential_identifiers delete_delete:skip 1
b kratos resolution identity_recovery_addresses delete_delete:skip 1
b kratos resolution identity_verifiable_addresses delete_delete:skip 1"

write_a() { kratos_create a "$EMAIL" "C4 at A" > "$SCRATCH/id-a"; }
write_b() { kratos_create b "$EMAIL" "C4 at B" > "$SCRATCH/id-b"; }
after_heal() {
  dump_logs "$SCRATCH/collision.logs"
  directive a identity_delete "$(cat "$SCRATCH/id-b")" > /dev/null
  directive a identity_resave "$(cat "$SCRATCH/id-a")" > /dev/null
  sync_all
  apply_directives b
  sync_all
  apply_directives a
}

check() {
  local emails s
  emails=$(grep -h -o '[a-z0-9.-]*@harness.test' "$SCRATCH/collision.logs" | sort -u | paste -sd, -)
  expect_eq "collisions logged name one email" "$emails" "$EMAIL"
  for s in a b; do
    expect_eq "$s: identities with the email" \
      "$(kr "$s" "SELECT string_agg(id::text, ',') FROM identities WHERE traits->>'email' = '$EMAIL';")" "$(cat "$SCRATCH/id-a")"
  done
}
