# shellcheck shell=bash disable=SC2034  # sourced by ../run.sh, which reads EXPECT
# Row N18. Class 1: new rows with site-unique ids.
# Pass beyond the common conditions: two users.
#
# Driven by: SQL. The writes are NetBird's for `POST /api/users` (service
# user) and `POST /api/users/{id}/tokens`, the calls
# ops/scripts/netbird-service-identities.py --mint makes: one users row and
# one personal_access_tokens row, xid ids, no serial bump (inventory §5).

UA=$(xid); UB=$(xid); TA=$(xid); TB=$(xid)
EXPECT=""

mint_service_user() { # <site> <user id> <token id>
  nb "$1" "INSERT INTO users (id, account_id, role, is_service_user, non_deletable, service_user_name,
                              auto_groups, blocked, pending_approval, created_at, issued)
           VALUES ('$2', '$ACCT', 'admin', true, false, 'svc-harness', '[]', false, false, now(), 'api');
           INSERT INTO personal_access_tokens (id, user_id, name, hashed_token, expiration_date, created_by, created_at)
           VALUES ('$3', '$2', 'harness', encode(sha256(random()::text::bytea), 'base64'),
                   now() + interval '1 day', '$2', now());"
}
write_a() { mint_service_user a "$UA" "$TA"; }
write_b() { mint_service_user b "$UB" "$TB"; }

check() {
  for s in a b; do
    expect_eq "$s: service users" "$(nb "$s" "SELECT count(*) FROM users WHERE id IN ('$UA','$UB');")" 2
    expect_eq "$s: tokens"        "$(nb "$s" "SELECT count(*) FROM personal_access_tokens WHERE id IN ('$TA','$TB');")" 2
  done
}
