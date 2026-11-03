# shellcheck shell=bash disable=SC2034  # sourced by ../run.sh, which reads EXPECT
# Row N9. Class 5: both sites compute the same membership from the warrant
# files and write it.
# Pass beyond the common conditions: membership equal at both sites.
#
# Driven by: SQL. `goat warrant apply --membership` from
# warrant-membership.timer at every site
# (crates/goat-cli/src/cmd/warrant_membership.rs apply_plan) sends
# `PUT /api/groups/{id}` with a fresh read; NetBird updates the groups row,
# inserts and deletes the group_peers diff, and bumps
# accounts.network_serial, in one transaction (inventory §5). Both sites
# apply the same plan: remove P1, add P3.

G=$(xid); P1=$(xid); P2=$(xid); P3=$(xid)
EXPECT="
a netbird resolution group_peers delete_delete:skip 1
a netbird resolution group_peers insert_exists:apply_remote 1
b netbird resolution accounts update_update:keep_local 1
b netbird resolution group_peers delete_delete:skip 1
b netbird resolution group_peers insert_exists:keep_local 1
b netbird resolution groups update_update:keep_local 1
"

seed() {
  nb a "INSERT INTO groups (id, account_id, name, issued, resources) VALUES ('$G', '$ACCT', 'HARNESS-WARRANT', 'api', '[]');"
  nb_peer a "$P1" harness-w1
  nb_peer a "$P2" harness-w2
  nb_peer a "$P3" harness-w3
  nb a "INSERT INTO group_peers (account_id, group_id, peer_id) VALUES ('$ACCT','$G','$P1'), ('$ACCT','$G','$P2');"
}
apply_plan() {
  nb "$1" "BEGIN;
           UPDATE groups SET name = 'HARNESS-WARRANT', resources = '[]' WHERE id = '$G';
           DELETE FROM group_peers WHERE group_id = '$G' AND peer_id = '$P1';
           INSERT INTO group_peers (account_id, group_id, peer_id) VALUES ('$ACCT', '$G', '$P3');
           UPDATE accounts SET network_serial = network_serial + 1 WHERE id = '$ACCT';
           COMMIT;"
}
write_a() { apply_plan a; }
write_b() { apply_plan b; }

check() {
  local want; want="$(printf '%s\n' "$P2" "$P3" | sort | paste -sd, -)"
  for s in a b; do
    expect_eq "$s: members of HARNESS-WARRANT" \
      "$(nb "$s" "SELECT string_agg(peer_id, ',' ORDER BY peer_id) FROM group_peers WHERE group_id='$G';")" "$want"
  done
}
