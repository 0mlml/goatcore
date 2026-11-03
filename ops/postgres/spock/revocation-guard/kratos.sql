-- Revocation guard, Ory Kratos database (issue #1505, ADR 1193).
--
-- Same four parts as netbird.sql (capture, ledger, guard, re-apply), for the
-- replicated Kratos identities table:
--   - an identity set inactive, or deleted, is recorded in goat_revocations;
--   - no later write, local or replicated, can make it active again or bring
--     a deleted identity back;
--   - when the ledger row lands at a site, that site's copy is corrected AND
--     that site's sessions for the identity are deleted. Sessions are not
--     replicated (ADR 1010 D8), so this is what ends a revoked person's
--     sessions at every site, not only the one the revoke was sent to.
--
-- Reinstating: goat_reinstate('kratos.identity', '<uuid>', '<actor>') first,
-- then set the identity active through the admin API. The reinstate row names
-- the revoke events it cancels; a revoke from another site that had not yet
-- arrived is not cancelled (see goat_revoked).
--
-- Idempotent. Runs after Kratos migrations have created its tables.

BEGIN;
SET LOCAL lock_timeout = '5s';
LOCK TABLE public.identities IN SHARE ROW EXCLUSIVE MODE;

CREATE TABLE IF NOT EXISTS public.goat_revocations (
    id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    kind        text        NOT NULL CHECK (kind IN (
                    'kratos.identity',            -- identities.state set inactive
                    'kratos.identity.deleted')),  -- identities row deleted
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

-- When the guard or the apply path forces a row inactive, it stamps
-- state_changed_at from the ledger, not from this site's clock: the ledger
-- rows are the same at every site, so the forced row is too. The latest
-- uncancelled revoke event; Kratos stores the column as UTC without zone.
CREATE OR REPLACE FUNCTION goat_revoked_at(p_kind text, p_subject text) RETURNS timestamp
LANGUAGE sql STABLE AS $$
  SELECT max(r.at) AT TIME ZONE 'UTC' FROM public.goat_revocations r
   WHERE r.kind = p_kind AND r.subject_id = p_subject AND r.action = 'revoke'
     AND NOT EXISTS (
       SELECT 1 FROM public.goat_revocations c
        WHERE c.kind = p_kind AND c.subject_id = p_subject AND c.action = 'reinstate'
          AND r.id = ANY (c.cancels))
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

-- Sessions live in this site's database only. The table exists wherever
-- Kratos runs; guard for a database without it (tests, partial installs).
CREATE OR REPLACE FUNCTION goat_revocation_apply(p_kind text, p_subject text) RETURNS integer
LANGUAGE plpgsql AS $$
DECLARE n integer := 0; m integer;
BEGIN
  IF NOT goat_revoked(p_kind, p_subject) THEN RETURN 0; END IF;
  CASE p_kind
    WHEN 'kratos.identity' THEN
      UPDATE public.identities SET state = 'inactive', state_changed_at = goat_revoked_at(p_kind, p_subject)
       WHERE id = p_subject::uuid AND state <> 'inactive';
      GET DIAGNOSTICS n = ROW_COUNT;
    WHEN 'kratos.identity.deleted' THEN
      -- The child rows too: Spock applies with foreign-key triggers off, so a
      -- cascade does not run at a site that receives the delete, and rows the
      -- owner re-issued while cut off would outlive the identity (ADR 1205).
      -- (Each only where Kratos created the table; test schemas carry fewer.)
      IF to_regclass('public.identity_credential_identifiers') IS NOT NULL THEN
        DELETE FROM public.identity_credential_identifiers
         WHERE identity_credential_id IN (SELECT id FROM public.identity_credentials WHERE identity_id = p_subject::uuid);
      END IF;
      IF to_regclass('public.identity_credentials') IS NOT NULL THEN
        DELETE FROM public.identity_credentials WHERE identity_id = p_subject::uuid;
      END IF;
      IF to_regclass('public.identity_verifiable_addresses') IS NOT NULL THEN
        DELETE FROM public.identity_verifiable_addresses WHERE identity_id = p_subject::uuid;
      END IF;
      IF to_regclass('public.identity_recovery_addresses') IS NOT NULL THEN
        DELETE FROM public.identity_recovery_addresses WHERE identity_id = p_subject::uuid;
      END IF;
      DELETE FROM public.identities WHERE id = p_subject::uuid;
      GET DIAGNOSTICS n = ROW_COUNT;
  END CASE;
  IF to_regclass('public.sessions') IS NOT NULL THEN
    DELETE FROM public.sessions WHERE identity_id = p_subject::uuid;
    GET DIAGNOSTICS m = ROW_COUNT; n := n + m;
  END IF;
  RETURN n;
END $$;

CREATE OR REPLACE FUNCTION goat_revocation_sweep() RETURNS integer
LANGUAGE plpgsql AS $$
DECLARE r record; n integer := 0;
BEGIN
  FOR r IN SELECT DISTINCT kind, subject_id FROM public.goat_revocations LOOP
    n := n + goat_revocation_apply(r.kind, r.subject_id);
  END LOOP;
  RETURN n;
END $$;

-- At the site that writes the ledger row, the correction is made in Spock
-- repair mode, so only the ledger row replicates. Each site corrects its own
-- copy when the row lands. A replicated correction would carry this site's
-- whole identity row, which then overwrites an edit the owner made while cut
-- off: the apply that lands the ledger row also marks the identity row as the
-- revoking site's, so the next change from that site is taken without a
-- conflict check (ADR 1205 D6).
CREATE OR REPLACE FUNCTION goat_revocations_on_insert() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE local boolean := NOT pg_replication_origin_session_is_setup()
                         AND to_regprocedure('spock.repair_mode(boolean)') IS NOT NULL;
BEGIN
  IF local THEN PERFORM spock.repair_mode(true); END IF;
  PERFORM goat_revocation_apply(NEW.kind, NEW.subject_id);
  IF local THEN PERFORM spock.repair_mode(false); END IF;
  RETURN NULL;
END $$;
DROP TRIGGER IF EXISTS goat_revocations_on_insert ON public.goat_revocations;
CREATE TRIGGER goat_revocations_on_insert AFTER INSERT ON public.goat_revocations
  FOR EACH ROW EXECUTE FUNCTION goat_revocations_on_insert();
ALTER TABLE public.goat_revocations ENABLE ALWAYS TRIGGER goat_revocations_on_insert;

-- CAPTURE (origin-only).
CREATE OR REPLACE FUNCTION goat_capture_identities() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    IF NOT goat_revoked('kratos.identity.deleted', OLD.id::text) THEN
      PERFORM goat_revoke('kratos.identity.deleted', OLD.id::text, 'kratos');
    END IF;
  ELSIF NEW.state = 'inactive' AND OLD.state <> 'inactive'
        AND NOT goat_revoked('kratos.identity', NEW.id::text) THEN
    PERFORM goat_revoke('kratos.identity', NEW.id::text, 'kratos');
  END IF;
  RETURN NULL;
END $$;
DROP TRIGGER IF EXISTS goat_capture ON public.identities;
CREATE TRIGGER goat_capture AFTER UPDATE OF state OR DELETE ON public.identities
  FOR EACH ROW EXECUTE FUNCTION goat_capture_identities();

-- GUARD (ALWAYS).
CREATE OR REPLACE FUNCTION goat_guard_identities() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF goat_revoked('kratos.identity.deleted', NEW.id::text) THEN RETURN NULL; END IF;
  IF goat_revoked('kratos.identity', NEW.id::text) AND NEW.state <> 'inactive' THEN
    NEW.state := 'inactive';
    NEW.state_changed_at := goat_revoked_at('kratos.identity', NEW.id::text);
  END IF;
  RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS goat_guard ON public.identities;
CREATE TRIGGER goat_guard BEFORE INSERT OR UPDATE ON public.identities
  FOR EACH ROW EXECUTE FUNCTION goat_guard_identities();
ALTER TABLE public.identities ENABLE ALWAYS TRIGGER goat_guard;

-- A deleted identity's child rows that arrive later (the owner re-issued its
-- credentials while cut off) are dropped on arrival.
CREATE OR REPLACE FUNCTION goat_guard_identity_children() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE owner uuid;
BEGIN
  IF TG_TABLE_NAME = 'identity_credential_identifiers' THEN
    -- Kratos inserts the credential before its identifiers, so a missing
    -- credential here is one this guard already dropped.
    SELECT identity_id INTO owner FROM public.identity_credentials WHERE id = NEW.identity_credential_id;
    IF owner IS NULL THEN RETURN NULL; END IF;
  ELSE
    owner := NEW.identity_id;
  END IF;
  IF goat_revoked('kratos.identity.deleted', owner::text) THEN RETURN NULL; END IF;
  RETURN NEW;
END $$;
DO $$
DECLARE t text;
BEGIN
  FOREACH t IN ARRAY ARRAY['identity_credentials', 'identity_credential_identifiers',
                           'identity_verifiable_addresses', 'identity_recovery_addresses'] LOOP
    CONTINUE WHEN to_regclass('public.' || t) IS NULL;
    EXECUTE format('DROP TRIGGER IF EXISTS goat_guard ON public.%I', t);
    EXECUTE format('CREATE TRIGGER goat_guard BEFORE INSERT OR UPDATE ON public.%I
                      FOR EACH ROW EXECUTE FUNCTION goat_guard_identity_children()', t);
    EXECUTE format('ALTER TABLE public.%I ENABLE ALWAYS TRIGGER goat_guard', t);
  END LOOP;
END $$;

-- Baseline inactive identities that existed before capture was installed.
-- The id is goat_revocation_baseline_id(kind, subject), so every site's
-- baseline for an identity is one event, cancelled by a reinstatement at any
-- site even when another site's copy arrives later. `at` is informational.
INSERT INTO public.goat_revocations (id, kind, subject_id, action, at, actor, site)
SELECT goat_revocation_baseline_id('kratos.identity', i.id::text), 'kratos.identity', i.id::text, 'revoke',
       '-infinity'::timestamptz, 'guard-backfill', NULL
  FROM public.identities i
 WHERE i.state = 'inactive' AND NOT EXISTS (
   SELECT 1 FROM public.goat_revocations r
    WHERE r.kind = 'kratos.identity' AND r.subject_id = i.id::text);


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


-- Re-issue an identity's rows with the same ids, so a site that discarded
-- them (a same-email collision at heal, ADR 1205 D7) receives them as fresh
-- inserts once its own colliding identity is gone. Run at the owner site by
-- spock-hub-directives for an identity_resave directive. Replica role keeps
-- the capture trigger from reading the delete as a revocation; logical
-- decoding still carries every row.
CREATE OR REPLACE FUNCTION goat_identity_resave(p_identity uuid) RETURNS integer
LANGUAGE plpgsql AS $$
DECLARE n integer;
BEGIN
  SET LOCAL session_replication_role = replica;
  CREATE TEMP TABLE r_i  ON COMMIT DROP AS SELECT * FROM public.identities WHERE id = p_identity;
  GET DIAGNOSTICS n = ROW_COUNT;
  IF n = 0 THEN RAISE EXCEPTION 'identity % is not here', p_identity; END IF;
  CREATE TEMP TABLE r_c  ON COMMIT DROP AS SELECT * FROM public.identity_credentials WHERE identity_id = p_identity;
  CREATE TEMP TABLE r_ci ON COMMIT DROP AS SELECT * FROM public.identity_credential_identifiers
                                            WHERE identity_credential_id IN (SELECT id FROM r_c);
  CREATE TEMP TABLE r_va ON COMMIT DROP AS SELECT * FROM public.identity_verifiable_addresses WHERE identity_id = p_identity;
  CREATE TEMP TABLE r_ra ON COMMIT DROP AS SELECT * FROM public.identity_recovery_addresses WHERE identity_id = p_identity;
  DELETE FROM public.identity_credential_identifiers WHERE id IN (SELECT id FROM r_ci);
  DELETE FROM public.identity_credentials           WHERE id IN (SELECT id FROM r_c);
  DELETE FROM public.identity_verifiable_addresses  WHERE id IN (SELECT id FROM r_va);
  DELETE FROM public.identity_recovery_addresses    WHERE id IN (SELECT id FROM r_ra);
  DELETE FROM public.identities                     WHERE id = p_identity;
  INSERT INTO public.identities                     SELECT * FROM r_i;
  INSERT INTO public.identity_credentials           SELECT * FROM r_c;
  INSERT INTO public.identity_credential_identifiers SELECT * FROM r_ci;
  INSERT INTO public.identity_verifiable_addresses  SELECT * FROM r_va;
  INSERT INTO public.identity_recovery_addresses    SELECT * FROM r_ra;
  SELECT count(*) INTO n FROM (SELECT 1 FROM r_c UNION ALL SELECT 1 FROM r_ci UNION ALL SELECT 1 FROM r_va UNION ALL SELECT 1 FROM r_ra) x;
  RETURN n + 1;
END $$;

-- Privileged management-host recovery only; the console receives no SQL
-- credentials. Deleted identities require a separate recovery decision.
CREATE OR REPLACE FUNCTION goat_reinstate_identity(p_identity uuid, p_actor text)
RETURNS boolean LANGUAGE plpgsql AS $$
BEGIN
  IF btrim(p_actor) = '' OR length(p_actor) > 800 THEN
    RAISE EXCEPTION 'an attributed reinstatement reason is required';
  END IF;
  PERFORM 1 FROM public.identities WHERE id=p_identity FOR UPDATE;
  IF NOT FOUND THEN RAISE EXCEPTION 'identity does not exist'; END IF;
  IF goat_revoked('kratos.identity.deleted',p_identity::text) THEN
    RAISE EXCEPTION 'deleted identity cannot be reinstated by activation';
  END IF;
  PERFORM goat_reinstate('kratos.identity',p_identity::text,p_actor);
  RETURN NOT goat_revoked('kratos.identity',p_identity::text);
END $$;

COMMIT;
