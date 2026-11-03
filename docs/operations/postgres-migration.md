# Migrate Netbird from SQLite to PostgreSQL

Migrates the three Netbird SQLite databases to PostgreSQL on the network host. Enables streaming replication for warm standby (separate runbook).

Total downtime: minutes. Existing WireGuard tunnels stay up the entire time — management server is not in the data path.

Scripts are in `ops/postgres/`. New deployments via `deploy.sh` include Postgres automatically.

---

## Quick path (scripted)

If you just want to run it:

```bash
# From LOCAL — copy files to the network host
ssh -t goat@100.127.0.10 'mkdir -p /tmp/postgres-setup'
scp ops/postgres/* goat@100.127.0.10:/tmp/postgres-setup/

# SSH to the network host
ssh -t goat@100.127.0.10

# On the network host — deploy Postgres, migrate, switch
sudo mkdir -p /opt/goat/postgres
sudo cp /tmp/postgres-setup/* /opt/goat/postgres/
cd /opt/goat/postgres

# Generate a password
PG_PASS=$(openssl rand -hex 24)
echo "NETBIRD_PG_PASSWORD=$PG_PASS" | sudo tee .env >/dev/null
sudo chmod 600 .env

# Start Postgres
sudo docker compose up -d

# Wait for ready
until sudo docker exec netbird-postgres pg_isready -U netbird; do sleep 1; done

# Install prerequisites
sudo apt-get install -y pgloader sqlite3

# Migrate (extracts SQLite, converts booleans, imports to Postgres)
sudo bash /tmp/postgres-setup/migrate.sh "$PG_PASS"

# STOP HERE if accounts = 0 — migration failed, do not proceed

# Switch Netbird to Postgres
sudo bash /tmp/postgres-setup/switch-to-postgres.sh "$PG_PASS"

# Verify from LOCAL
curl -s -o /dev/null -w '%{http_code}' https://netbird.example.net
sudo netbird status
```

The rest of this doc explains each step in detail.

---

## Prerequisites

- SSH to the network host via mesh IP: `ssh -t goat@100.127.0.10`
- Current Netbird version: v0.66.4+ (Postgres support for all three stores)
- Fresh backup: `bash ops/backup.sh`
- `sqlite3` and `pgloader` installed on the network host

## What gets migrated

| SQLite file | Contents | Gotcha |
|---|---|---|
| `store.db` | Peers, policies, setup keys, groups, routes | Text booleans ('true'/'false') must be converted to integers before pgloader |
| `idp.db` | Embedded IdP (Dex) auth data | Text booleans must be converted |
| `events.db` | Audit/activity log | Clean import, no issues |

All three are in the `netbird-server` container's volume, typically at `/var/lib/netbird/` inside the container.

### Boolean handling

Netbird's SQLite stores boolean values as text strings ('true'/'false'). pgloader 3.6.7 (Ubuntu 24.04) does NOT have the `sqlite-text-boolean` transform function. The migration script works around this by pre-converting text booleans to integers (1/0) using `sqlite3` on working copies before running pgloader. pgloader then handles integer→boolean conversion natively.

If you see `accounts` with 0 rows after migration, the boolean conversion failed. Check the `sqlite3` UPDATE commands in `migrate.sh`.

---

## Step 1: Backup

Run from LOCAL:

```bash
bash ops/backup.sh
```

Then SSH to the network host and take an additional raw copy of the SQLite files:

```bash
ssh -t goat@100.127.0.10
sudo mkdir -p /opt/goat/postgres-migration/backup
sudo docker cp netbird-server:/var/lib/netbird/store.db /opt/goat/postgres-migration/backup/
sudo docker cp netbird-server:/var/lib/netbird/idp.db /opt/goat/postgres-migration/backup/
sudo docker cp netbird-server:/var/lib/netbird/events.db /opt/goat/postgres-migration/backup/
```

Also capture the current encryption key from the management config — it must be preserved:

```bash
sudo docker exec netbird-server cat /etc/netbird/management.json | grep -i encryptionkey
```

Save this value. You'll need it in Step 5.

## Step 2: Deploy PostgreSQL on the network host

The `ops/postgres/docker-compose.yml` deploys a Postgres 16 container with localhost port mapping and an init script that creates all three databases. Copy the files and start it:

```bash
sudo mkdir -p /opt/goat/postgres
sudo cp ops/postgres/* /opt/goat/postgres/
cd /opt/goat/postgres

PG_PASS=$(openssl rand -hex 24)
echo "NETBIRD_PG_PASSWORD=$PG_PASS" | sudo tee .env >/dev/null
sudo chmod 600 .env

sudo docker compose up -d
```

Wait for healthy:

```bash
until sudo docker exec netbird-postgres pg_isready -U netbird; do sleep 1; done
```

Verify all three databases exist:

```bash
sudo docker exec netbird-postgres psql -U netbird -c "\l"
```

You should see `netbird`, `netbird_idp`, and `netbird_events`.

## Step 3: Install prerequisites on the network host

```bash
sudo apt-get update && sudo apt-get install -y pgloader sqlite3
```

Both are required. `sqlite3` pre-processes boolean values; `pgloader` handles the actual migration.

## Step 4: Migrate the databases

The scripted approach (`migrate.sh`) handles everything: extraction, boolean conversion, pgloader import, and orphan cleanup. Run it:

```bash
sudo bash /tmp/postgres-setup/migrate.sh "$PG_PASS"
```

What it does:

1. Extracts SQLite files from the `netbird-server` container
2. Saves the encryption key
3. Copies SQLite files to a work directory (never modifies backups)
4. Converts text booleans ('true'/'false') to integers (1/0) using `sqlite3`
5. Drops and recreates Postgres databases (clean slate for re-runs)
6. Runs `pgloader --type sqlite` for each database (no CAST rules needed after pre-processing)
7. Cleans up orphaned records that SQLite's missing FK enforcement allowed

**Check the output.** The script prints row counts at the end. If `accounts = 0`, the migration failed — do not proceed to Step 5.

### Manual migration (if script fails)

If the script fails, you can run each step manually. See the script source for the exact commands. The key insight: convert booleans in SQLite BEFORE running pgloader:

```bash
sqlite3 /path/to/work/store.db "UPDATE accounts SET is_domain_primary_account = CASE WHEN lower(is_domain_primary_account) = 'true' THEN 1 ELSE 0 END;"
```

Repeat for each boolean column listed in `migrate.sh`, then run pgloader with `--type sqlite` (no CAST block needed).

## Step 5: Reconfigure Netbird to use PostgreSQL

Run the switch script:

```bash
sudo bash /tmp/postgres-setup/switch-to-postgres.sh "$PG_PASS"
```

What it does:

1. Verifies Postgres has data (accounts > 0) — refuses to proceed if not
2. Stops Netbird (`docker compose down`)
3. Creates `postgres.env` with Postgres DSN and engine env vars
4. Adds `env_file: postgres.env` to the netbird-server service in docker-compose.yml
5. Sets `engine: "postgres"` in `config.yaml` (the env var alone is not enough — Netbird reads the engine from config.yaml first, and only falls back to the env var when config is empty)
6. Connects the Postgres container to the `netbird_netbird` Docker network
7. Starts Netbird and verifies Postgres is active

Look for `using Postgres store engine` in the logs.

### Environment variables added

```
NETBIRD_STORE_ENGINE=postgres
NETBIRD_STORE_CONFIG_ENGINE=postgres
NETBIRD_STORE_ENGINE_POSTGRES_DSN=host=netbird-postgres user=netbird password=<PG_PASS> dbname=netbird port=5432 sslmode=disable
NB_STORE_ENGINE_POSTGRES_DSN=host=netbird-postgres user=netbird password=<PG_PASS> dbname=netbird port=5432 sslmode=disable
NB_ACTIVITY_EVENT_STORE_ENGINE=postgres
NB_ACTIVITY_EVENT_POSTGRES_DSN=host=netbird-postgres user=netbird password=<PG_PASS> dbname=netbird_events port=5432 sslmode=disable
```

The DSN uses `netbird-postgres` as the host because both containers share the `netbird_netbird` Docker network.

### Env var naming gotcha

Netbird has two separate env var mechanisms for the store engine:

| Env var | How it works |
|---|---|
| `NETBIRD_STORE_CONFIG_ENGINE` | Template variable — only works if `management.json` contains `$NETBIRD_STORE_CONFIG_ENGINE` as a placeholder. If the config was already rendered with a hardcoded value, this does nothing. |
| `NETBIRD_STORE_ENGINE` | Go code fallback — read directly by `getStoreEngineFromEnv()` in `store.go`. Only consulted when the config's engine field is empty. |

Both are set in `postgres.env` for compatibility. If the main store still shows SQLite after switching, the persisted config in the data volume has a non-empty engine value that takes precedence over both env vars.

### Password requirements

Generate passwords with `openssl rand -hex 24` (not `-base64`). Base64 output contains `/`, `+`, `=` characters that break Postgres DSN parsing in the Go `pgx` driver. Hex output is alphanumeric only.

## Step 6: Verify

```bash
# Dashboard accessible
curl -s -o /dev/null -w '%{http_code}' https://netbird.example.net

# Peers still enrolled
sudo netbird status
```

Check row counts match pre-migration:

```bash
sudo docker exec netbird-postgres psql -U netbird -d netbird -c "SELECT count(*) FROM peers;"
sudo docker exec netbird-postgres psql -U netbird -d netbird -c "SELECT count(*) FROM accounts;"
sudo docker exec netbird-postgres psql -U netbird -d netbird -c "SELECT count(*) FROM policies;"
sudo docker exec netbird-postgres psql -U netbird -d netbird_events -c "SELECT count(*) FROM events;"
```

Log into the dashboard and verify peers, policies, and setup keys are intact.

---

## Rollback

If anything goes wrong, revert to SQLite:

```bash
cd /opt/netbird

# Switch config.yaml back to sqlite
sudo sed -i 's/engine: "postgres"/engine: "sqlite"/' config.yaml

# Remove the env_file reference (or just remove postgres.env)
sudo rm -f postgres.env

sudo docker compose down && sudo docker compose up -d
```

Netbird falls back to SQLite when no Postgres config is present. The original SQLite files are untouched in the Docker volume — the migration only reads copies.

---

## What this enables

With Postgres running, the warm standby path is:

1. **Streaming replication** to a second network host or cloud VM (`pg_basebackup` + WAL shipping)
2. **Signal and relay** already stateless — run them active on the standby now
3. **Failover** = promote Postgres replica + start Netbird management + update DNS
4. **Recovery time** = minutes

Warm standby setup is a separate runbook.
