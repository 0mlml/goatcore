#!/usr/bin/env bash
# shellcheck disable=SC2034  # PFX, NET, PW are read by ../lib/two-site.sh
# Induced-partition revocation test (issue #1505).
#
# Two postgres-spock nodes replicate both ways, as a goatnet's two host
# sites do. The test cuts replication, revokes at site A, makes an ordinary
# later write to the SAME row at site B, heals, and reports what each
# revocation looks like on both sites afterwards.
#
#   GUARD=0  asserts the defect: last-writer-wins undoes revocations.
#            If Spock's behaviour ever changes, this fails and says so.
#   GUARD=1  installs ops/postgres/spock/revocation-guard/*.sql first and
#            asserts every revocation survives on both sites.
#
# The tables are the replicated NetBird and Kratos tables cut down to the
# columns the scenarios touch; the writes are the ones NetBird and Kratos
# make in normal use (cited per scenario below).
#
# Substrate: Linux Docker only (CI). Two bidirectional Spock nodes on macOS
# Docker Desktop wedge the daemon.
#
#   IMAGE=postgres-spock:smoke GUARD=0 ./run.sh

set -euo pipefail
cd "$(dirname "$0")"

IMAGE="${IMAGE:?set IMAGE to a postgres-spock image}"
GUARD="${GUARD:-0}"
GUARD_DIR="../../revocation-guard"
PFX="pr"
NET="partition-revoke-net"
PW="partition"
# shellcheck source=../lib/two-site.sh
source ../lib/two-site.sh

cleanup() {
  [[ "${KEEP:-0}" == 1 ]] && { echo "KEEP=1, leaving pr-a pr-b running"; return; }
  ts_down
}
trap cleanup EXIT
cleanup

echo "== bring up two sites"
ts_up
for s in a b; do q "$s" kratos "CREATE EXTENSION IF NOT EXISTS spock;"; done

echo "== schema (DDL is not replicated: both sites)"
for s in a b; do
  q "$s" netbird "
    CREATE TABLE setup_keys (id text PRIMARY KEY, account_id text, revoked boolean NOT NULL DEFAULT false,
                             used_times int NOT NULL DEFAULT 0, last_used timestamptz, updated_at timestamptz);
    CREATE TABLE users      (id text PRIMARY KEY, account_id text, blocked boolean NOT NULL DEFAULT false,
                             last_login timestamptz);
    CREATE TABLE peers      (id text PRIMARY KEY, account_id text, user_id text,
                             peer_status_last_seen timestamptz, peer_status_connected boolean NOT NULL DEFAULT false,
                             peer_status_login_expired boolean NOT NULL DEFAULT false,
                             peer_status_required_approval boolean NOT NULL DEFAULT false);"
  q "$s" kratos "
    CREATE TABLE identities (id uuid PRIMARY KEY, nid uuid, schema_id text, traits jsonb NOT NULL DEFAULT '{}',
                             state text NOT NULL DEFAULT 'active', state_changed_at timestamptz,
                             updated_at timestamptz NOT NULL DEFAULT now());"
done

if [[ "$GUARD" == 1 ]]; then
  echo "== install the revocation guard (both sites)"
  for s in a b; do
    docker exec -i "pr-$s" psql -U postgres -d netbird -v ON_ERROR_STOP=1 -q < "$GUARD_DIR/netbird.sql"
    docker exec -i "pr-$s" psql -U postgres -d kratos  -v ON_ERROR_STOP=1 -q < "$GUARD_DIR/kratos.sql"
  done
fi

if [[ "$GUARD" == 1 ]]; then
  echo "== history exists before subscription membership"
  for s in a b; do
    q "$s" netbird "INSERT INTO goat_revocations(kind,subject_id,action,at,actor) VALUES ('netbird.user','historical-$s','revoke','-infinity','bootstrap-fixture');"
    kid=00000000-0000-4000-8000-00000000010$([[ "$s" == a ]] && echo 1 || echo 2)
    q "$s" kratos "INSERT INTO goat_revocations(kind,subject_id,action,at,actor) VALUES ('kratos.identity','$kid','revoke','-infinity','bootstrap-fixture');"
  done
fi

echo "== wire bidirectional Spock (netbird + kratos)"
for db in netbird kratos; do
  ts_node_create "$db"
  for s in a b; do
    q "$s" "$db" "SELECT spock.repset_add_all_tables('default', ARRAY['public']);" >/dev/null
  done
  ts_sub_create "$db" default
done

wait_replicating() { ts_wait_replicating netbird kratos; }
# A marker row written on each side after heal and seen on the other proves
# every earlier change from that side has been applied (per-origin order).
barrier() {
  local tag="barrier-$1-$RANDOM"
  for s in a b; do
    q "$s" netbird "INSERT INTO users (id, account_id) VALUES ('$tag-$s', 'barrier');"
    q "$s" kratos  "INSERT INTO identities (id, schema_id) VALUES (gen_random_uuid(), '$tag-$s');"
  done
  for s in a b; do
    for o in a b; do
      for _ in $(seq 1 60); do
        n=$(q "$s" netbird "SELECT count(*) FROM users WHERE id='$tag-$o';")
        k=$(q "$s" kratos  "SELECT count(*) FROM identities WHERE schema_id='$tag-$o';")
        [[ "$n" == 1 && "$k" == 1 ]] && break
        sleep 1
      done
      [[ "$n" == 1 && "$k" == 1 ]] || { echo "::error::barrier from $o never reached $s"; exit 1; }
    done
  done
}
wait_replicating

if [[ "$GUARD" == 1 ]]; then
  echo "== reconcile pre-membership history with original IDs and timestamps"
  for db in netbird kratos; do
    for pair in "a b" "b a"; do
      set -- $pair
      history=$(q "$2" "$db" "SELECT coalesce(jsonb_agg(to_jsonb(r) ORDER BY id),'[]'::jsonb) FROM goat_revocations r;")
      history=${history//\'/\'\'}
      q "$1" "$db" "SELECT goat_revocations_merge('$history'::jsonb);" >/dev/null
    done
    for s in a b; do
      count=$(q "$s" "$db" "SELECT count(*) FROM goat_revocations WHERE actor='bootstrap-fixture' AND at='-infinity';")
      [[ "$count" == 2 ]] || { echo "::error::historical ledger incomplete at $s/$db"; exit 1; }
    done
  done
  # These later rows prove subscriptions still carry new changes after import.
  barrier history-bootstrap
  echo "  historical union preserved; subsequent replication verified"
fi

# Kratos sessions are site-local (ADR 1010 D8): created after the repset is
# populated, so they stay out of it.
for s in a b; do
  q "$s" kratos "CREATE TABLE sessions (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), identity_id uuid NOT NULL);"
done

echo "== seed (at A, replicated to B)"
KID_LATE=00000000-0000-4000-8000-000000000001
KID_EARLY=00000000-0000-4000-8000-000000000002
q a netbird "
  INSERT INTO setup_keys (id, account_id) VALUES ('sk-late','acct'), ('sk-early','acct'), ('sk-del','acct');
  INSERT INTO users (id, account_id) VALUES ('u-late','acct'), ('u-bystander','acct'), ('u-skew','acct');
  INSERT INTO peers (id, account_id, user_id) VALUES ('p-late','acct','u-late'), ('p-del','acct','u-late');"
q a kratos "INSERT INTO identities (id, schema_id, traits) VALUES
  ('$KID_LATE','person','{\"name\":\"x\"}'), ('$KID_EARLY','person','{\"name\":\"y\"}');"
barrier seed
# One live session for the identity at EACH site.
for s in a b; do q "$s" kratos "INSERT INTO sessions (identity_id) VALUES ('$KID_LATE');"; done
if [[ "$GUARD" == 1 ]]; then
  # Skewed clocks (ADR 1193 cancel sets): u-skew is revoked while connected,
  # so both sites hold that revoke before the cut.
  q a netbird "UPDATE users SET blocked=true WHERE id='u-skew';"
  barrier skew-seed
fi

echo "== cut the link (both directions)"
ts_cut netbird kratos

# The revocations are the bare writes the goat revoke path makes today
# (crates/goat-cli/src/cmd/admin.rs revoke_one) through the NetBird and Kratos
# APIs. With GUARD=1 nothing about them changes: the guard captures them.
revoke_nb() {
  case "$1" in
    setup_key) q a netbird "UPDATE setup_keys SET revoked=true, updated_at=now() WHERE id='$2';" ;;
    user)      q a netbird "UPDATE users SET blocked=true WHERE id='$2';
                            UPDATE peers SET peer_status_login_expired=true WHERE user_id='$2';" ;;
    peer)      q a netbird "DELETE FROM peers WHERE id='$2';" ;;
    setup_key_delete) q a netbird "DELETE FROM setup_keys WHERE id='$2';" ;;
  esac
}
revoke_kr() { q "$3" kratos "UPDATE identities SET state='inactive', state_changed_at=now(), updated_at=now() WHERE id='$2';"; }

echo "== site A revokes"
revoke_nb setup_key sk-late
revoke_nb user u-late
revoke_nb peer p-del
revoke_nb setup_key_delete sk-del
revoke_kr identity "$KID_LATE" a
sleep 1

echo "== site B, still cut off, makes its normal later writes to the same rows"
# One transaction per row, as NetBird writes them.
# NetBird IncrementSetupKeyUsage (store/sql_store.go:3007): column update on enrolment.
q b netbird "UPDATE setup_keys SET used_times=used_times+1, last_used=now() WHERE id='sk-late';"
q b netbird "UPDATE setup_keys SET used_times=used_times+1, last_used=now() WHERE id='sk-del';"
# NetBird SaveUserLastLogin (sql_store.go:2714): full-row save of B's copy on login.
q b netbird "UPDATE users SET blocked=false, last_login=now() WHERE id='u-late';"
# NetBird MarkPeerConnected -> SavePeerStatus (peer.go:121-151): connect clears login_expired.
q b netbird "UPDATE peers SET peer_status_connected=true, peer_status_login_expired=false,
             peer_status_last_seen=now() WHERE id='p-late';"
q b netbird "UPDATE peers SET peer_status_connected=true, peer_status_last_seen=now() WHERE id='p-del';"
# One transaction that touches a row A deleted AND an unrelated row. Shows
# what Spock does with the rest of a transaction when one change has no row.
q b netbird "BEGIN;
             UPDATE peers SET peer_status_last_seen=now() WHERE id='p-del';
             UPDATE users SET last_login='2001-01-01' WHERE id='u-bystander';
             COMMIT;"
# Kratos self-service settings: B's identity row rewritten with a profile change.
q b kratos "UPDATE identities SET traits='{\"name\":\"x2\"}', updated_at=now() WHERE id='$KID_LATE';"
# Control: B writes FIRST, then A revokes later. Last-writer-wins alone keeps this one.
q b kratos "UPDATE identities SET traits='{\"name\":\"y2\"}', updated_at=now() WHERE id='$KID_EARLY';"
sleep 1
revoke_kr identity "$KID_EARLY" a

if [[ "$GUARD" == 1 ]]; then
  echo "== skewed clocks: B revokes u-skew stamped an hour early; A, not seeing it, reinstates"
  # B's clock runs an hour behind, so its revoke sorts before A's reinstate by
  # `at`. A's reinstate names only the revoke it can see (the pre-cut one).
  q b netbird "INSERT INTO goat_revocations (kind, subject_id, action, at, actor)
               VALUES ('netbird.user', 'u-skew', 'revoke', clock_timestamp() - interval '1 hour', 'site-b-operator');"
  q a netbird "SELECT goat_reinstate('netbird.user', 'u-skew', 'site-a-operator');"
  # NetBird SaveUserLastLogin: full-row save at A after the reinstate.
  q a netbird "UPDATE users SET blocked=false, last_login=now() WHERE id='u-skew';"
fi

echo "== heal"
ts_heal netbird kratos
wait_replicating
barrier heal

echo "== state after heal"
state() { # state <site>
  q "$1" netbird "
    SELECT 'setup_key sk-late revoked='  || coalesce((SELECT revoked::text FROM setup_keys WHERE id='sk-late'),'<absent>');
    SELECT 'setup_key sk-del present='   || (SELECT count(*) FROM setup_keys WHERE id='sk-del')::text
           || coalesce(' revoked=' || (SELECT revoked::text FROM setup_keys WHERE id='sk-del'),'');
    SELECT 'user u-late blocked='        || coalesce((SELECT blocked::text FROM users WHERE id='u-late'),'<absent>');
    SELECT 'user u-skew blocked='        || coalesce((SELECT blocked::text FROM users WHERE id='u-skew'),'<absent>');
    SELECT 'user u-bystander last_login=' || coalesce((SELECT last_login::date::text FROM users WHERE id='u-bystander'),'<null>');
    SELECT 'resolutions recorded=' || CASE WHEN (SELECT count(*) FROM spock.resolutions) > 0 THEN 'yes' ELSE 'no' END;
    SELECT 'peer p-late login_expired='  || coalesce((SELECT peer_status_login_expired::text FROM peers WHERE id='p-late'),'<absent>');
    SELECT 'peer p-del present='         || (SELECT count(*) FROM peers WHERE id='p-del')::text
           || coalesce(' login_expired=' || (SELECT peer_status_login_expired::text FROM peers WHERE id='p-del'),'');"
  q "$1" kratos "
    SELECT 'identity late state='  || (SELECT state FROM identities WHERE id='$KID_LATE');
    SELECT 'identity early state=' || (SELECT state FROM identities WHERE id='$KID_EARLY');
    SELECT 'identity late sessions=' || (SELECT count(*) FROM sessions WHERE identity_id='$KID_LATE')::text;"
}
state a > state-a.txt; state b > state-b.txt
paste -d'|' state-a.txt state-b.txt | awk -F'|' '{printf "  A: %-44s B: %s\n", $1, $2}'

echo "== conflict evidence"
for s in a b; do
  echo "-- site $s: spock.resolutions"
  q "$s" netbird "SELECT CASE WHEN to_regclass('spock.resolutions') IS NULL THEN 'no spock.resolutions table'
                  ELSE (SELECT count(*)::text || ' rows' FROM spock.resolutions) END;" || true
  echo "-- site $s: spock.exception_log"
  q "$s" netbird "SELECT CASE WHEN to_regclass('spock.exception_log') IS NULL THEN 'no spock.exception_log table'
                  ELSE (SELECT count(*)::text || ' rows' FROM spock.exception_log) END;" || true
  q "$s" netbird "SELECT operation || ' ' || coalesce(table_name,'') || ': ' || left(coalesce(error_message,''),160)
                  FROM spock.exception_log ORDER BY 1;" 2>/dev/null || true
  echo "-- site $s: spock.conflict_log  (what the SpockConflictLogged alert reads)"
  q "$s" netbird "SELECT CASE WHEN to_regclass('spock.conflict_log') IS NULL THEN 'no spock.conflict_log table' ELSE 'present' END;"
  echo "-- site $s: CONFLICT lines in the postgres log"
  docker logs "pr-$s" 2>&1 | grep -iE 'conflict' | head -20 || echo "   none"
  echo "-- site $s: apply errors and warnings in the postgres log"
  docker logs "pr-$s" 2>&1 | grep -E 'ERROR|WARNING|exception|discard' | grep -v 'already exists' | head -20 || echo "   none"
done

echo "== verdict (GUARD=$GUARD)"
fail=0
# The post-partition runbook §3 queries, verbatim in shape: they must run.
for s in a b; do
  if q "$s" netbird "SELECT count(*) FROM (SELECT * FROM spock.resolutions WHERE log_time > NOW() - INTERVAL '1 hour' ORDER BY log_time DESC) r;" >/dev/null \
     && q "$s" netbird "SELECT count(*) FROM (SELECT * FROM spock.exception_log ORDER BY remote_commit_ts DESC LIMIT 50) e;" >/dev/null; then
    echo "  ok    $s: runbook §3 queries run"
  else
    echo "  FAIL  $s: a runbook §3 query does not run"; fail=1
  fi
done
check() { # check <site> <expected line>
  if grep -qxF "$2" "state-$1.txt"; then echo "  ok    $1: $2"; else echo "  FAIL  $1: expected '$2'"; fail=1; fi
}
if [[ "$GUARD" == 1 ]]; then
  for s in a b; do
    check "$s" "setup_key sk-late revoked=true"
    check "$s" "setup_key sk-del present=0"
    check "$s" "user u-bystander last_login=2001-01-01"
    check "$s" "user u-late blocked=true"
    check "$s" "peer p-late login_expired=true"
    check "$s" "peer p-del present=0"
    check "$s" "identity late state=inactive"
    check "$s" "identity early state=inactive"
    check "$s" "identity late sessions=0"
    # B's revoke is stamped before A's reinstate, which never named it.
    check "$s" "user u-skew blocked=true"
    order=$(q "$s" netbird "SELECT (SELECT at FROM goat_revocations WHERE actor='site-b-operator')
                                 < (SELECT at FROM goat_revocations WHERE actor='site-a-operator');")
    [[ "$order" == t ]] && echo "  ok    $s: skewed revoke sorts before the reinstate by clock" \
                        || { echo "  FAIL  $s: skew fixture did not stamp the revoke earlier"; fail=1; }
    # Converged by the triggers alone: the backstop sweep has nothing to do.
    sw="$(q "$s" netbird "SELECT goat_revocation_sweep();")+$(q "$s" kratos "SELECT goat_revocation_sweep();")"
    if [[ "$sw" == "0+0" ]]; then echo "  ok    $s: sweep changed nothing"; else echo "  FAIL  $s: sweep changed $sw rows"; fail=1; fi
  done

  echo "== reinstate is deliberate"
  # An unblock alone does not stick...
  q a netbird "UPDATE users SET blocked=false WHERE id='u-late';"
  [[ "$(q a netbird "SELECT blocked FROM users WHERE id='u-late';")" == t ]] \
    && echo "  ok    a: unblock without reinstate stays blocked" \
    || { echo "  FAIL  a: unblock without reinstate took effect"; fail=1; }
  # ...a reinstate first, then the unblock, does, at both sites.
  q a netbird "SELECT goat_reinstate('netbird.user', 'u-late', 'test');"
  q a netbird "UPDATE users SET blocked=false WHERE id='u-late';"
  barrier reinstate
  for s in a b; do
    [[ "$(q "$s" netbird "SELECT blocked FROM users WHERE id='u-late';")" == f ]] \
      && echo "  ok    $s: reinstated user unblocked" \
      || { echo "  FAIL  $s: reinstated user still blocked"; fail=1; }
  done
  # The ledger is insert-only.
  q a netbird "DELETE FROM goat_revocations;" >/dev/null 2>&1 \
    && { echo "  FAIL  a: ledger rows could be deleted"; fail=1; } \
    || echo "  ok    a: ledger refuses deletes"
else
  # The defect, as last-writer-wins produces it. Both sites agree on the
  # wrong answer, which is why a drift check comparing sites cannot see it.
  for s in a b; do
    check "$s" "user u-bystander last_login=2001-01-01"
    check "$s" "setup_key sk-late revoked=false"
    check "$s" "user u-late blocked=false"
    check "$s" "peer p-late login_expired=false"
    check "$s" "identity late state=active"
    check "$s" "identity early state=inactive"
    check "$s" "identity late sessions=1"
  done
  # Spock records a conflict only where the local row is KEPT (keep_local).
  # The site whose revocation was overwritten (A) records nothing.
  check b "resolutions recorded=yes"
  check a "resolutions recorded=no"
  echo "  (recorded, not asserted: delete-vs-update outcome for sk-del and p-del, above)"
fi
rm -f state-a.txt state-b.txt
[[ "$fail" == 0 ]] && echo "PASS" || { echo "FAIL"; exit 1; }
