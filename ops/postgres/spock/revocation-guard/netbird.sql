-- Revocation guard, NetBird management database (issue #1505, ADR 1193).
--
-- Spock settles a conflict per row by last writer wins. A revocation made at
-- one site during a partition is undone on rejoin by any later write to the
-- same row at the other site: a setup-key use, a user login, a peer connect.
-- This file makes revocations absorbing on the replicated NetBird tables:
--
--   1. CAPTURE  - local writes that revoke (key revoked, user blocked, peer or
--                 key deleted) append a row to goat_revocations. Origin-only
--                 triggers: they fire for NetBird's own writes, not for Spock
--                 apply, so no revoke path has to change.
--   2. LEDGER   - goat_revocations is insert-only and replicated. Inserts with
--                 fresh uuid keys never conflict, so after a rejoin every site
--                 holds every revocation.
--   3. GUARD    - ALWAYS triggers (they fire for Spock apply too) stop any
--                 write, local or replicated, from undoing a recorded
--                 revocation, and drop writes that would bring a deleted peer
--                 or key back.
--   4. RE-APPLY - when a ledger row lands at a site, the revocation is applied
--                 to that site's copy, so a stale row that won the merge is
--                 corrected.
--
-- Undoing a revocation is a deliberate act: goat_reinstate(kind, subject,
-- actor) appends a reinstate row naming the revoke events it cancels, after
-- which the normal unblock works. A revoke made at another site that the
-- reinstating site had not yet received is not cancelled by it.
-- An unblock through the NetBird API alone leaves the user blocked.
--
-- Idempotent: safe to re-run. Requires the NetBird tables to exist (NetBird
-- creates them on first start), so it runs after the management service has
-- started once, alongside the Spock repset wiring.

-- Install and capture the existing state atomically. Refuse a busy database
-- rather than leave a gap between dropping and recreating its triggers.
BEGIN;
SET LOCAL lock_timeout = '5s';
LOCK TABLE public.setup_keys, public.users, public.peers IN SHARE ROW EXCLUSIVE MODE;

CREATE TABLE IF NOT EXISTS public.goat_revocations (
    id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    kind        text        NOT NULL CHECK (kind IN (
                    'netbird.setup_key',          -- setup_keys.revoked set
                    'netbird.setup_key.deleted',  -- setup_keys row deleted
                    'netbird.user',               -- users.blocked set
                    'netbird.peer.deleted')),     -- peers row deleted
    subject_id  text        NOT NULL,
    action      text        NOT NULL CHECK (action IN ('revoke', 'reinstate')),
    at          timestamptz NOT NULL DEFAULT clock_timestamp(),
    actor       text        NOT NULL DEFAULT current_user,
    site        text        DEFAULT current_setting('cluster_name', true),
    cancels     uuid[]
);
CREATE INDEX IF NOT EXISTS goat_revocations_subject ON public.goat_revocations (kind, subject_id, at DESC, id DESC);

-- A reinstate row names the revoke events it cancels. Kept on the reinstate
-- row itself, not in a side table: one insert carries the whole decision, so
-- Spock can never apply a reinstatement without its cancel set or a cancel
-- set without its reinstatement. Added in place for a ledger installed before
-- the column existed (expand only; no existing row is rewritten).
ALTER TABLE public.goat_revocations ADD COLUMN IF NOT EXISTS cancels uuid[];
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint
                  WHERE conrelid = 'public.goat_revocations'::regclass
                    AND conname = 'goat_revocations_cancels_on_reinstate') THEN
    ALTER TABLE public.goat_revocations ADD CONSTRAINT goat_revocations_cancels_on_reinstate
      CHECK (action = 'reinstate' OR cancels IS NULL);
  END IF;
END $$;

-- Insert-only. A mistaken revocation is undone by a reinstate row, never by
-- editing history.
CREATE OR REPLACE FUNCTION goat_revocations_immutable() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'goat_revocations is insert-only; use goat_reinstate()';
END $$;
DROP TRIGGER IF EXISTS goat_revocations_immutable ON public.goat_revocations;
CREATE TRIGGER goat_revocations_immutable BEFORE UPDATE OR DELETE ON public.goat_revocations
  FOR EACH ROW EXECUTE FUNCTION goat_revocations_immutable();
ALTER TABLE public.goat_revocations ENABLE ALWAYS TRIGGER goat_revocations_immutable;

-- A subject is revoked while any revoke event for it is not named in the
-- cancel set of a reinstate event for it. Set membership only: no event's
-- timestamp or arrival order enters the decision, so a site with a skewed
-- clock cannot order an older reinstatement after a newer revocation.
-- A reinstate row written before cancel sets existed (cancels IS NULL)
-- cancels nothing: such a subject reads as revoked until reinstated again.
CREATE OR REPLACE FUNCTION goat_revoked(p_kind text, p_subject text) RETURNS boolean
LANGUAGE sql STABLE AS $$
  SELECT EXISTS (
    SELECT 1 FROM public.goat_revocations r
     WHERE r.kind = p_kind AND r.subject_id = p_subject AND r.action = 'revoke'
       AND NOT EXISTS (
         SELECT 1 FROM public.goat_revocations c
          WHERE c.kind = p_kind AND c.subject_id = p_subject AND c.action = 'reinstate'
            AND r.id = ANY (c.cancels)))
$$;

CREATE OR REPLACE FUNCTION goat_revoke(p_kind text, p_subject text, p_actor text DEFAULT current_user)
RETURNS void LANGUAGE sql AS $$
  INSERT INTO public.goat_revocations (kind, subject_id, action, actor) VALUES (p_kind, p_subject, 'revoke', p_actor)
$$;

-- Cancels every revoke event for the subject visible here now. A revoke made
-- elsewhere and not yet replicated is not named, so it stands when it lands.
CREATE OR REPLACE FUNCTION goat_reinstate(p_kind text, p_subject text, p_actor text DEFAULT current_user)
RETURNS void LANGUAGE sql AS $$
  INSERT INTO public.goat_revocations (kind, subject_id, action, actor, cancels)
  SELECT p_kind, p_subject, 'reinstate', p_actor, coalesce(array_agg(r.id ORDER BY r.id), '{}')
    FROM public.goat_revocations r
   WHERE r.kind = p_kind AND r.subject_id = p_subject AND r.action = 'revoke'
$$;

-- Historical baseline events get an id derived from (kind, subject) and no
-- site, so every site's baseline for a subject is the same event: a
-- reinstatement that cancels it at one site cancels the copy any other site
-- backfilled, including one that arrives later.
CREATE OR REPLACE FUNCTION goat_revocation_baseline_id(p_kind text, p_subject text) RETURNS uuid
LANGUAGE sql IMMUTABLE AS $$
  SELECT md5('goat-revocation-baseline:' || p_kind || ':' || p_subject)::uuid
$$;

-- Apply one revocation to this site's copy. Returns rows changed.
CREATE OR REPLACE FUNCTION goat_revocation_apply(p_kind text, p_subject text) RETURNS integer
LANGUAGE plpgsql AS $$
DECLARE n integer := 0; m integer;
BEGIN
  IF NOT goat_revoked(p_kind, p_subject) THEN RETURN 0; END IF;
  CASE p_kind
    WHEN 'netbird.setup_key' THEN
      UPDATE public.setup_keys SET revoked = true WHERE id = p_subject AND NOT revoked;
      GET DIAGNOSTICS n = ROW_COUNT;
    WHEN 'netbird.setup_key.deleted' THEN
      DELETE FROM public.setup_keys WHERE id = p_subject;
      GET DIAGNOSTICS n = ROW_COUNT;
    WHEN 'netbird.user' THEN
      UPDATE public.users SET blocked = true WHERE id = p_subject AND NOT blocked;
      GET DIAGNOSTICS n = ROW_COUNT;
      UPDATE public.peers SET peer_status_login_expired = true
       WHERE user_id = p_subject AND NOT peer_status_login_expired;
      GET DIAGNOSTICS m = ROW_COUNT; n := n + m;
    WHEN 'netbird.peer.deleted' THEN
      DELETE FROM public.peers WHERE id = p_subject;
      GET DIAGNOSTICS n = ROW_COUNT;
  END CASE;
  RETURN n;
END $$;

-- Backstop: re-apply every standing revocation. Returns rows changed; 0 on a
-- converged site.
CREATE OR REPLACE FUNCTION goat_revocation_sweep() RETURNS integer
LANGUAGE plpgsql AS $$
DECLARE r record; n integer := 0;
BEGIN
  FOR r IN SELECT DISTINCT kind, subject_id FROM public.goat_revocations LOOP
    n := n + goat_revocation_apply(r.kind, r.subject_id);
  END LOOP;
  RETURN n;
END $$;

-- 4. RE-APPLY when a ledger row lands (local insert or Spock apply).
CREATE OR REPLACE FUNCTION goat_revocations_on_insert() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  PERFORM goat_revocation_apply(NEW.kind, NEW.subject_id);
  RETURN NULL;
END $$;
DROP TRIGGER IF EXISTS goat_revocations_on_insert ON public.goat_revocations;
CREATE TRIGGER goat_revocations_on_insert AFTER INSERT ON public.goat_revocations
  FOR EACH ROW EXECUTE FUNCTION goat_revocations_on_insert();
ALTER TABLE public.goat_revocations ENABLE ALWAYS TRIGGER goat_revocations_on_insert;

-- 1. CAPTURE (origin-only: NetBird's own writes at this site).
CREATE OR REPLACE FUNCTION goat_capture_setup_keys() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    IF NOT goat_revoked('netbird.setup_key.deleted', OLD.id) THEN
      PERFORM goat_revoke('netbird.setup_key.deleted', OLD.id, 'netbird');
    END IF;
  ELSIF NEW.revoked AND NOT OLD.revoked AND NOT goat_revoked('netbird.setup_key', NEW.id) THEN
    PERFORM goat_revoke('netbird.setup_key', NEW.id, 'netbird');
  END IF;
  RETURN NULL;
END $$;
DROP TRIGGER IF EXISTS goat_capture ON public.setup_keys;
CREATE TRIGGER goat_capture AFTER UPDATE OF revoked OR DELETE ON public.setup_keys
  FOR EACH ROW EXECUTE FUNCTION goat_capture_setup_keys();

CREATE OR REPLACE FUNCTION goat_capture_users() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.blocked AND NOT OLD.blocked AND NOT goat_revoked('netbird.user', NEW.id) THEN
    PERFORM goat_revoke('netbird.user', NEW.id, 'netbird');
  END IF;
  RETURN NULL;
END $$;
DROP TRIGGER IF EXISTS goat_capture ON public.users;
CREATE TRIGGER goat_capture AFTER UPDATE OF blocked ON public.users
  FOR EACH ROW EXECUTE FUNCTION goat_capture_users();

CREATE OR REPLACE FUNCTION goat_capture_peers() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF NOT goat_revoked('netbird.peer.deleted', OLD.id) THEN
    PERFORM goat_revoke('netbird.peer.deleted', OLD.id, 'netbird');
  END IF;
  RETURN NULL;
END $$;
DROP TRIGGER IF EXISTS goat_capture ON public.peers;
CREATE TRIGGER goat_capture AFTER DELETE ON public.peers
  FOR EACH ROW EXECUTE FUNCTION goat_capture_peers();

-- 3. GUARD (ALWAYS: local writes and Spock apply).
CREATE OR REPLACE FUNCTION goat_guard_setup_keys() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF goat_revoked('netbird.setup_key.deleted', NEW.id) THEN RETURN NULL; END IF;
  IF goat_revoked('netbird.setup_key', NEW.id) THEN NEW.revoked := true; END IF;
  RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS goat_guard ON public.setup_keys;
CREATE TRIGGER goat_guard BEFORE INSERT OR UPDATE ON public.setup_keys
  FOR EACH ROW EXECUTE FUNCTION goat_guard_setup_keys();
ALTER TABLE public.setup_keys ENABLE ALWAYS TRIGGER goat_guard;

CREATE OR REPLACE FUNCTION goat_guard_users() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF goat_revoked('netbird.user', NEW.id) THEN NEW.blocked := true; END IF;
  RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS goat_guard ON public.users;
CREATE TRIGGER goat_guard BEFORE INSERT OR UPDATE ON public.users
  FOR EACH ROW EXECUTE FUNCTION goat_guard_users();
ALTER TABLE public.users ENABLE ALWAYS TRIGGER goat_guard;

CREATE OR REPLACE FUNCTION goat_guard_peers() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF goat_revoked('netbird.peer.deleted', NEW.id) THEN RETURN NULL; END IF;
  IF NEW.user_id IS NOT NULL AND NEW.user_id <> '' AND goat_revoked('netbird.user', NEW.user_id) THEN
    NEW.peer_status_login_expired := true;
  END IF;
  RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS goat_guard ON public.peers;
CREATE TRIGGER goat_guard BEFORE INSERT OR UPDATE ON public.peers
  FOR EACH ROW EXECUTE FUNCTION goat_guard_peers();
ALTER TABLE public.peers ENABLE ALWAYS TRIGGER goat_guard;

-- Existing revoked rows predate capture triggers. Each baseline event's id is
-- goat_revocation_baseline_id(kind, subject), so the same historical fact
-- backfilled at two sites is one event, cancelled by a reinstatement at
-- either. `at = -infinity` is informational only. Never reinterpret a subject
-- with existing history during a reinstall.
INSERT INTO public.goat_revocations (id, kind, subject_id, action, at, actor, site)
SELECT goat_revocation_baseline_id('netbird.setup_key', k.id), 'netbird.setup_key', k.id, 'revoke',
       '-infinity'::timestamptz, 'guard-backfill', NULL
  FROM public.setup_keys k
 WHERE k.revoked AND NOT EXISTS (
   SELECT 1 FROM public.goat_revocations r
    WHERE r.kind = 'netbird.setup_key' AND r.subject_id = k.id);

INSERT INTO public.goat_revocations (id, kind, subject_id, action, at, actor, site)
SELECT goat_revocation_baseline_id('netbird.user', u.id), 'netbird.user', u.id, 'revoke',
       '-infinity'::timestamptz, 'guard-backfill', NULL
  FROM public.users u
 WHERE u.blocked AND NOT EXISTS (
   SELECT 1 FROM public.goat_revocations r
    WHERE r.kind = 'netbird.user' AND r.subject_id = u.id);


-- Membership uses synchronize_data=false. Import historical events without
-- changing their IDs, timestamps or payloads; reused IDs must match exactly.
-- This is an ordinary invoker-rights function, not a privilege grant.
CREATE OR REPLACE FUNCTION goat_revocations_merge(events jsonb) RETURNS integer
LANGUAGE plpgsql AS $$
DECLARE added integer;
BEGIN
  IF jsonb_typeof(events) IS DISTINCT FROM 'array' THEN
    RAISE EXCEPTION 'revocation history must be a JSON array';
  END IF;
  IF EXISTS (
    SELECT 1 FROM jsonb_array_elements(events) e
     WHERE jsonb_typeof(e) IS DISTINCT FROM 'object'
        OR NOT (e ?& ARRAY['id','kind','subject_id','action','at','actor','site','cancels'])
        OR (SELECT count(*) FROM jsonb_object_keys(e)) <> 8
  ) THEN
    RAISE EXCEPTION 'revocation history must contain complete event records';
  END IF;
  INSERT INTO public.goat_revocations (id,kind,subject_id,action,at,actor,site,cancels)
    SELECT id,kind,subject_id,action,at,actor,site,cancels
      FROM jsonb_populate_recordset(NULL::public.goat_revocations,events)
    ON CONFLICT (id) DO NOTHING;
  GET DIAGNOSTICS added = ROW_COUNT;
  -- Check after INSERT so a conflicting concurrent insertion is also seen.
  IF EXISTS (
    SELECT 1 FROM jsonb_populate_recordset(NULL::public.goat_revocations,events) e
    LEFT JOIN public.goat_revocations r ON r.id=e.id
    WHERE r.id IS NULL OR
      ROW(r.kind,r.subject_id,r.action,r.at,r.actor,r.site,r.cancels) IS DISTINCT FROM
      ROW(e.kind,e.subject_id,e.action,e.at,e.actor,e.site,e.cancels)
  ) THEN
    RAISE EXCEPTION 'revocation event ID collision; history was not overwritten';
  END IF;
  RETURN added;
END $$;

COMMIT;
