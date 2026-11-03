# shellcheck shell=bash disable=SC2034  # sourced by ../run.sh, which reads EXPECT
# ADR 1205 D2: only the hub's commits are directives. Site B (not the hub)
# writes a sever of the hub and an edit of an A-owned identity into
# goat_hub_directives. Both sites refuse both, naming B as the origin; nothing
# is severed and the identity is unchanged.

EMAIL="h3-$RUN@harness.test"
EXPECT=""

seed() { kratos_create a "$EMAIL" "H3 person" > "$SCRATCH/id"; }
write_b() {
  directive b sever_site site_a > "$SCRATCH/d1"
  directive b identity_edit "$(cat "$SCRATCH/id")" \
    '{"patch":[{"op":"replace","path":"/traits/name","value":"H3 forged"}]}' > "$SCRATCH/d2"
}
after_heal() { apply_directives a; apply_directives b; }

check() {
  local d s
  for d in d1 d2; do for s in a b; do
    expect_eq "$s: $d verdict" "$(verdict "$s" "$(cat "$SCRATCH/$d")")" "refused_origin site_b"
  done; done
  for s in a b; do
    expect_eq "$s: name" "$(kname "$s" "$(cat "$SCRATCH/id")")" "H3 person"
    expect_eq "$s: enabled subscriptions" "$(q "$s" netbird "SELECT count(*) FROM spock.subscription WHERE sub_enabled;")" 1
  done
}
