# shellcheck shell=bash disable=SC2034  # sourced by ../run.sh, which reads EXPECT
# ADR 1205 D3, override 1: the hub's change takes precedence over the owner's.
# Site B owns the identity. Cut off, B edits it and the hub (A) issues an
# identity_edit directive. A cannot carry it out (not the owner). After heal,
# B's timer carries it out, so the hub's value is the last write at both sites
# and one Kratos ever wrote the identity.

EMAIL="h2-$RUN@harness.test"
EXPECT=""

seed() { kratos_create b "$EMAIL" "H2 person" > "$SCRATCH/id"; }
write_b() { kapi b PATCH "/admin/identities/$(cat "$SCRATCH/id")" '[{"op":"replace","path":"/traits/name","value":"H2 edited by owner"}]' >/dev/null; }
write_a() {
  directive a identity_edit "$(cat "$SCRATCH/id")" \
    '{"patch":[{"op":"replace","path":"/traits/name","value":"H2 set by the hub"}]}' > "$SCRATCH/dir"
  apply_directives a
}
after_heal() { apply_directives b; }

check() {
  local id d; id=$(cat "$SCRATCH/id"); d=$(cat "$SCRATCH/dir")
  expect_eq "a: directive verdict" "$(verdict a "$d")" "not_owner site_a"
  expect_eq "b: directive verdict" "$(verdict b "$d")" "applied site_a"
  for s in a b; do expect_eq "$s: name" "$(kname "$s" "$id")" "H2 set by the hub"; done
}
