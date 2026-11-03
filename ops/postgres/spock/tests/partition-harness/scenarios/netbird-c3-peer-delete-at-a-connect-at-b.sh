# shellcheck shell=bash disable=SC2034  # sourced by ../run.sh, which reads EXPECT
# Row N4. Class 3: revocations, ADR 1193 ledger (netbird.peer.deleted).
# Carries over PR #1538's p-del.
# Pass beyond the common conditions: peer absent at both sites.
#
# Driven by: SQL. A: `DELETE /api/peers/{id}` (admin.rs run_peer,
# `goat admin peer cut`): NetBird deletes the peer's group_peers rows and the
# peer, and bumps accounts.network_serial (inventory §5). B, cut off:
# MarkPeerConnected -> SavePeerStatus when the peer's client connects there.

P=$(xid); G=$(xid)
EXPECT="
a netbird exception peers UPDATE:other 1
"

seed() {
  nb a "INSERT INTO groups (id, account_id, name, issued, resources) VALUES ('$G', '$ACCT', 'HARNESS-PEER-CUT', 'api', '[]');"
  nb_peer a "$P" harness-peer-cut
  nb a "INSERT INTO group_peers (account_id, group_id, peer_id) VALUES ('$ACCT', '$G', '$P');"
}
write_a() {
  nb a "BEGIN;
        DELETE FROM group_peers WHERE peer_id = '$P';
        DELETE FROM peers WHERE id = '$P';
        UPDATE accounts SET network_serial = network_serial + 1 WHERE id = '$ACCT';
        COMMIT;"
}
write_b() {
  nb b "UPDATE peers SET peer_status_connected = true, peer_status_login_expired = false,
                         peer_status_last_seen = now() WHERE id = '$P';"
}

check() {
  for s in a b; do
    expect_eq "$s: deleted peer absent"     "$(nb "$s" "SELECT count(*) FROM peers WHERE id='$P';")" 0
    expect_eq "$s: deleted peer in no group" "$(nb "$s" "SELECT count(*) FROM group_peers WHERE peer_id='$P';")" 0
  done
}
