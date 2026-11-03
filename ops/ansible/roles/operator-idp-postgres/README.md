# operator-idp-postgres

Single-instance Postgres on Orin-C backing the operator IdP per [ADR 0842](../../../../docs/adr/0842-dedicated-operator-idp-for-observability.md).

## What this is (and is not)

This is a **sealed-unit** Postgres dedicated to the per-site operator IdP (Kratos + Hydra processes also on Orin-C).

It is **not**:

- The postgres-spock cluster on the mgmt-host pair (Orin-A/B). That cluster backs site Hydra + netbird-mgmt + signal-server + ACL data + everything that needs cross-site Spock replication.
- A streaming-replica setup. Single instance. No failover. Couples to Orin-C's failure domain — when Orin-C is down the obs stack is down anyway, so the IdP coupling is moot.
- A pgBackRest-managed cluster. Backup is `pg_dump` cron + age-encrypt + retention-by-age. ADR 0842 §"Backup grade" justifies why pgBackRest properties (PITR, fast restore at scale) are not value-additive at this DB's scale (~1 MB).

## Required env vars

Per-site, resolved at runtime via `group_vars/<site>_obs_hosts/vars.yml` from `.envrc.local.d/30-site-services.envrc`:

| Var | Purpose |
|---|---|
| `operator_idp_pg_superuser_password` | Postgres `postgres` superuser password |
| `operator_idp_pg_app_password` | Password for the `operator_idp` application role (Kratos + Hydra connect as this user) |
| `operator_idp_pg_dump_age_recipient` | age public key the daily dump cron encrypts to. Generate via `age-keygen`; private key stays on operator workstation |

For vega specifically, the env-var names are `VEGA_C_OPERATOR_IDP_PG_*`. The group_vars layer maps them onto the role-internal names.

## What the role does

1. Creates compose dir + data dir + dump dir on the host (`/opt/operator-idp-postgres/`, `/opt/docker-data/operator-idp-postgres/`, dump dir at the latter).
2. Renders `docker-compose.yml` + `init/01-create-app-user-and-dbs.sh` + `.env` (mode 0600, env_file source for compose).
3. Renders the daily `pg-dump-and-encrypt.sh` script + installs a system cron entry (default 03:17 UTC).
4. Pulls the image, brings up the container, waits for the docker healthcheck.
5. Verifies the `operator_idp` app user can connect to the `kratos` database (smoke).

## Recovery from dump

```
age -d -i ~/.config/age/operator-idp-<site>.key /opt/docker-data/operator-idp-postgres/dumps/operator-idp-<TS>.sql.age \
  | docker exec -i operator-idp-postgres psql -U postgres
```

The dump is `pg_dumpall --clean --if-exists`, so it is fully self-contained — drops + recreates each DB + role from scratch.

## Tests

`tests/syntax-check.sh` — Ansible parse-only smoke. Behavioral coverage (real pg_isready, real pg_dump round-trip) lives in the operator-fired bringup smoke per Block 21H.1 `Validation` row.
