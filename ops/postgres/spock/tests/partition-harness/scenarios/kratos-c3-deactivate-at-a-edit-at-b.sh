# shellcheck shell=bash disable=SC2034  # sourced by ../run.sh, which reads EXPECT
# Rows C44, K4, K11. Class 3: revocations, ADR 1193 ledger (kratos.identity),
# written as ADR 1205 D6 has it. Carries over PR #1538's identity `late`.
#
# Site B owns the identity. Cut off: A, not the owner, deactivates it through
# the ledger (goat_revoke, never Kratos PATCH /state, which rewrites the
# credential rows: F-590); B, the owner, edits a trait through its Kratos.
# Pass beyond the common conditions: inactive at both sites, B's edit kept,
# one revoke event in the ledger at each site.

EMAIL="k4-$RUN@harness.test"
EXPECT=""

seed() { kratos_create b "$EMAIL" "K4 person" > "$SCRATCH/id"; }
write_a() { kr a "SELECT goat_revoke('kratos.identity', '$(cat "$SCRATCH/id")', 'harness');" >/dev/null; }
write_b() { sleep 1; kapi b PATCH "/admin/identities/$(cat "$SCRATCH/id")" '[{"op":"replace","path":"/traits/name","value":"K4 edited at B"}]' >/dev/null; }

check() {
  local id; id=$(cat "$SCRATCH/id")
  for s in a b; do
    expect_eq "$s: identity state"         "$(kr "$s" "SELECT state FROM identities WHERE id='$id';")" inactive
    expect_eq "$s: state through the API"  "$(kapi "$s" GET "/admin/identities/$id" | jq -r .state)" inactive
    expect_eq "$s: name"                   "$(kname "$s" "$id")" "K4 edited at B"
    expect_eq "$s: revoke events in the ledger" \
      "$(kr "$s" "SELECT count(*) FROM goat_revocations WHERE kind='kratos.identity' AND subject_id='$id' AND action='revoke';")" 1
  done
}
