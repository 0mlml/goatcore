-- Install Spock in each database that participates in multi-master logical
-- replication. Extensions are database-scoped, so run once per database.
-- Node registration (spock.node_create) and subscriptions (spock.sub_create)
-- are driven by Ansible per-site, not here — the extension install is the
-- only thing that's universal across every node.
\c netbird
CREATE EXTENSION IF NOT EXISTS spock;

\c netbird_idp
CREATE EXTENSION IF NOT EXISTS spock;

-- netbird_events: per-site, not replicated. No Spock here.

-- Block 21C — Kratos requires pg_trgm + btree_gin for the
-- identity-credential-identifiers GIN index. Per-site sovereign per ADR
-- 0401.
--
-- Block 21G / ADR 1010 D8 — the Spock extension
-- install for kratos + hydra databases is intentionally NOT done in
-- this init script. The Pattern C audit chose to keep it
-- role-managed (`ops/ansible/roles/ory-narrow-repset/`) rather than
-- init-script-managed, because:
--
--   (a) Activation is per-site opt-in via `ory_spock_enabled: true`
--       group_var; init-script-installed would make every site
--       carry the extension whether or not it activates.
--   (b) The role that installs the extension ALSO creates the narrow
--       `ory_narrow` repset and adds the canonical 10 inclusion-list
--       tables in one atomic substrate operation, gated by Ory
--       container health.
--   (c) The role's idempotent EXISTS checks make re-runs no-ops;
--       this init script runs once at first DB create.
--
-- Historical comment context (now superseded): an earlier comment
-- here read "when intra-site Spock for kratos+hydra is validated,
-- add `CREATE EXTENSION IF NOT EXISTS spock`." That framing pre-dated
-- the canonical-simple posture (intra-site is streaming replication,
-- not Spock; Spock is inter-site only). D8 makes the decision
-- explicit and ory-narrow-repset makes activation per-site opt-in.
\c kratos
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE EXTENSION IF NOT EXISTS btree_gin;

\c hydra
-- Hydra's migrations don't require contribs but install for consistency
-- in case future Hydra versions add full-text indexes.
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE EXTENSION IF NOT EXISTS btree_gin;
