-- Hub directives, NetBird management database (ADR 1205).
--
-- The hub is the goatnet's highest-echelon site. It is the only site whose
-- changes take precedence over another site's, and the only site that can
-- sever another. It acts on other sites only through rows in
-- goat_hub_directives, which every site's spock-hub-directives timer carries
-- out locally.
--
--   1. LEDGER   - goat_hub_directives is insert-only and in the netbird_mgmt
--                 repset, so every directive reaches every site. Fresh uuid
--                 keys never conflict.
--   2. ORIGIN   - goat_hub_directive_origin(id) names the Spock node whose
--                 commit wrote the row: this node for a local commit, else the
--                 node Spock records as the commit's replication origin
--                 (its node_id, kept with the commit timestamp). The
--                 timer accepts a directive only when that node is the hub it
--                 has in its own configuration, so a row written at any other
--                 site is refused even if it claims to come from the hub.
--   3. APPLIED  - goat_hub_directive_applied records what this site did with
--                 each directive. It is in no repset: each site keeps its own.
--
-- Requires track_commit_timestamp = on, which Spock already requires.
-- Idempotent: safe to re-run.

BEGIN;

CREATE TABLE IF NOT EXISTS public.goat_hub_directives (
    id       uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    kind     text        NOT NULL CHECK (kind IN (
                 'sever_site',        -- subject: Spock node name to cut off
                 'restore_site',      -- subject: Spock node name, after re-seed
                 'identity_edit',     -- subject: Kratos identity id; body.patch: JSON Patch
                 'identity_delete',   -- subject: Kratos identity id
                 'identity_resave',   -- subject: Kratos identity id
                 'transfer_owner')),  -- subject: Kratos identity id; body.site: new owner
    subject  text        NOT NULL,
    body     jsonb       NOT NULL DEFAULT '{}'::jsonb,
    at       timestamptz NOT NULL DEFAULT clock_timestamp(),
    actor    text        NOT NULL DEFAULT current_user
);
CREATE INDEX IF NOT EXISTS goat_hub_directives_at ON public.goat_hub_directives (at, id);

CREATE OR REPLACE FUNCTION goat_hub_directives_immutable() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'goat_hub_directives is insert-only (ADR 1205)';
END $$;
DROP TRIGGER IF EXISTS goat_hub_directives_immutable ON public.goat_hub_directives;
CREATE TRIGGER goat_hub_directives_immutable BEFORE UPDATE OR DELETE ON public.goat_hub_directives
  FOR EACH ROW EXECUTE FUNCTION goat_hub_directives_immutable();
ALTER TABLE public.goat_hub_directives ENABLE ALWAYS TRIGGER goat_hub_directives_immutable;

CREATE TABLE IF NOT EXISTS public.goat_hub_directive_applied (
    id       uuid        PRIMARY KEY,
    verdict  text        NOT NULL CHECK (verdict IN ('applied', 'not_owner', 'refused_origin', 'failed')),
    origin   text,
    detail   text,
    at       timestamptz NOT NULL DEFAULT clock_timestamp()
);

-- The Spock node whose commit wrote directive p_id, or NULL when it cannot
-- be told (no commit timestamp kept, or an origin that is no Spock node).
-- Spock's apply stamps each applied commit with the originating node's
-- node_id as its replication origin; a local commit has origin 0.
CREATE OR REPLACE FUNCTION goat_hub_directive_origin(p_id uuid) RETURNS text
LANGUAGE sql STABLE AS $$
  SELECT CASE
           WHEN o.roident = 0 THEN
             (SELECT n.node_name FROM spock.local_node l JOIN spock.node n ON n.node_id = l.node_id)
           ELSE
             (SELECT n.node_name FROM spock.node n WHERE n.node_id = o.roident)
         END
    FROM public.goat_hub_directives d,
         LATERAL pg_xact_commit_timestamp_origin(d.xmin) o
   WHERE d.id = p_id AND o.timestamp IS NOT NULL;
$$;

-- Directives this site has not yet acted on, oldest first.
CREATE OR REPLACE VIEW goat_hub_directives_pending AS
  SELECT d.*, goat_hub_directive_origin(d.id) AS origin
    FROM public.goat_hub_directives d
   WHERE NOT EXISTS (SELECT 1 FROM public.goat_hub_directive_applied a WHERE a.id = d.id)
   ORDER BY d.at, d.id;

COMMIT;
