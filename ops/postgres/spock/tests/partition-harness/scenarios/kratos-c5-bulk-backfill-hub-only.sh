# shellcheck shell=bash disable=SC2034  # sourced by ../run.sh, which reads EXPECT
# Rows K7, K8, K13. Class 5: bulk writes that run at the hub only, after heal.
# Pass beyond the common conditions: runs at the hub only; result equal at
# both sites.
#
# Driven by: the Kratos admin API at the hub (site A) after heal:
# `POST /admin/identities` for the bootstrap admin (K13, roles/ory) and a
# JSON `PATCH` replacing metadata_public on existing identities
# (K7, ops/scripts/ory-group-mirror.sh). Nothing runs at B.

EXPECT=""

seed() {
  kratos_create a "bf1-$RUN@harness.test" "Backfill one" >  "$SCRATCH/ids"
  kratos_create a "bf2-$RUN@harness.test" "Backfill two" >> "$SCRATCH/ids"
}
after_heal() {
  kratos_create a "admin-$RUN@harness.test" "Bootstrap admin" >> "$SCRATCH/ids"
  local id
  while read -r id; do
    kapi a PATCH "/admin/identities/$id" '[{"op":"add","path":"/metadata_public","value":{"groups":["harness-mirrored"]}}]' >/dev/null
  done < "$SCRATCH/ids"
}

check() {
  local ids; ids="'$(paste -sd, "$SCRATCH/ids" | sed "s/,/','/g")'"
  for s in a b; do
    expect_eq "$s: identities carrying the mirrored group" \
      "$(kr "$s" "SELECT count(*) FROM identities WHERE id IN ($ids) AND metadata_public->'groups' ? 'harness-mirrored';")" 3
  done
}
