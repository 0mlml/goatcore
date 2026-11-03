-- Three databases consumed by the split Netbird management containers.
-- Spock replicates `netbird` and `netbird_idp` across sites.
-- `netbird_events` is per-site by design (audit log; keep local for
-- sovereignty unless centralized audit becomes a requirement).
CREATE DATABASE netbird;
CREATE DATABASE netbird_idp;
CREATE DATABASE netbird_events;
-- Block 21C — Ory IdP logical databases.
-- Kratos and Hydra each get their own database. Per ADR 0411 + ADR 0401:
-- per-site sovereign (NOT replicated cross-site — see Spock subscription
-- config in ops/ansible/roles/postgres-spock-subscriptions/). pg_trgm and
-- btree_gin contribs (required by Kratos's identity-search-index migration)
-- are installed in 02-spock-extension.sql.
CREATE DATABASE kratos;
CREATE DATABASE hydra;
-- ADR 1165 step A — the id console's onboarding store and org registry, one
-- database per store. Created here so a new host has them; owned by postgres
-- until the id-admin-console role (tasks/postgres-db.yml) creates the
-- `id_console` login role and hands both to it, which it also does on existing
-- hosts. Per-site: no spock extension, no replication set (island-mode program
-- step 6 is where they would join one).
CREATE DATABASE id_console_onboard;
CREATE DATABASE id_console_orgs;
-- ADR 1165 step A — the goat-zenoh-portal registry. Created here so a new host
-- has it; owned by postgres until the goat-zenoh-portal-store role
-- (tasks/postgres-db.yml) creates the `portal` login role and hands the database
-- to it, which it also does on existing hosts. Per-site: no spock extension, no
-- replication set (island-mode program step 6 is where it would join one).
CREATE DATABASE portal_registry;
