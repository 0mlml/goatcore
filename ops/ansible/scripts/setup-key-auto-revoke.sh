#!/usr/bin/env bash
# ops/ansible/scripts/setup-key-auto-revoke.sh — cron-scheduled job that
# auto-revokes setup keys past their expires_at. Emits an audit row per
# revocation.
#
# Runs ON the mgmt-host (not from operator shell). Installed by the
# setup-key-rotation Ansible role as a cron entry
#   */15 * * * * /opt/mgmt-host/scripts/setup-key-auto-revoke.sh
#
# Postgres: auto-denies expired keys at enrollment time natively — this
# script does NOT gate denial, it just marks the row explicitly so the
# audit log has a tombstone and Prometheus counters advance.
set -euo pipefail

docker exec -i postgres-a psql -U postgres netbird <<'SQL'
CREATE TABLE IF NOT EXISTS setup_keys_audit (
  id TEXT PRIMARY KEY, ts TIMESTAMPTZ DEFAULT NOW(),
  event TEXT NOT NULL, key_id TEXT NOT NULL, key_name TEXT,
  role TEXT, created_by TEXT, note TEXT, ttl_days INT, usage_limit INT
);
WITH expired AS (
  SELECT id, name FROM setup_keys
   WHERE revoked = false
     AND expires_at < NOW() - INTERVAL '1 hour'
)
UPDATE setup_keys
   SET revoked = true, updated_at = NOW()
 WHERE id IN (SELECT id FROM expired);
INSERT INTO setup_keys_audit (id, event, key_id, key_name, created_by, note)
  SELECT 'audit_auto_rev_'||id||'_'||extract(epoch FROM now())::bigint,
         'auto_revoke', id, name, 'cron', 'expired'
    FROM setup_keys
   WHERE revoked = true
     AND updated_at >= NOW() - INTERVAL '2 minutes'
     AND id NOT IN (SELECT key_id FROM setup_keys_audit WHERE event IN ('revoke','auto_revoke'));
SELECT count(*) AS revoked_this_run
  FROM setup_keys_audit
 WHERE event = 'auto_revoke' AND ts >= NOW() - INTERVAL '2 minutes';
SQL
