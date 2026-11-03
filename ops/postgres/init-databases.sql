-- Created automatically on first Postgres start.
-- The default database (netbird) is created by POSTGRES_DB env var.
-- These two are additional databases for the IdP and events stores.
CREATE DATABASE netbird_idp;
CREATE DATABASE netbird_events;

-- Replication user for HA streaming replication.
-- Password is set via ALTER ROLE after container starts (from .env).
-- If NETBIRD_REPL_PASSWORD is not set, the role exists but cannot log in
-- until a password is assigned manually.
DO $$
BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'replicator') THEN
    CREATE ROLE replicator WITH REPLICATION LOGIN PASSWORD 'changeme';
  END IF;
END
$$;
