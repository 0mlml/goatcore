#!/usr/bin/env bash
set -euo pipefail

# goat — Switch Netbird management server to PostgreSQL backend
#
# Run ON THE NETHOST after migrate.sh has completed successfully
# (verify accounts > 0 before running this).
#
# This stops Netbird, adds Postgres env vars via a new env_file,
# connects the Postgres container to the Netbird Docker network,
# and restarts. Existing WireGuard tunnels stay up during the switch.
#
# Usage: sudo bash switch-to-postgres.sh <PG_PASSWORD>

if [ $# -lt 1 ]; then
  echo "Usage: sudo bash switch-to-postgres.sh <PG_PASSWORD>"
  exit 1
fi

PG_PASS="$1"
if [ -z "$PG_PASS" ]; then
  echo "ERROR: Password is empty. Pass the Postgres password as the first argument."
  exit 1
fi
NB_DIR="/opt/netbird"
COMPOSE_FILE="$NB_DIR/docker-compose.yml"

if [ ! -f "$COMPOSE_FILE" ]; then
  echo "ERROR: $COMPOSE_FILE not found."
  exit 1
fi

# -----------------------------------------------
# Step 1: Verify migration succeeded before proceeding
# -----------------------------------------------
echo "[1/5] Verifying migration data..."
ACCT_COUNT=$(docker exec netbird-postgres psql -U netbird -d netbird -t -c "SELECT count(*) FROM accounts;" 2>/dev/null | tr -d ' ')
if [ -z "$ACCT_COUNT" ] || [ "$ACCT_COUNT" = "0" ]; then
  echo "  ERROR: accounts table has $ACCT_COUNT rows. Migration did not succeed."
  echo "  Re-run migrate.sh before running this script."
  echo "  Netbird was NOT stopped — no changes made."
  exit 1
fi
PEER_COUNT=$(docker exec netbird-postgres psql -U netbird -d netbird -t -c "SELECT count(*) FROM peers;" 2>/dev/null | tr -d ' ')
echo "  Postgres has $ACCT_COUNT account(s) and $PEER_COUNT peer(s). Proceeding."

# -----------------------------------------------
# Step 2: Stop Netbird
# -----------------------------------------------
echo "[2/5] Stopping Netbird..."
cd "$NB_DIR"
docker compose down

# -----------------------------------------------
# Step 3: Create postgres.env and wire it into docker-compose.yml
# -----------------------------------------------
echo "[3/5] Configuring Postgres backend..."

PG_ENV="$NB_DIR/postgres.env"
if [ -f "$PG_ENV" ]; then
  echo "  $PG_ENV already exists, skipping creation."
else
  # NETBIRD_STORE_ENGINE is the Go-code env var that controls the main store engine.
  # NETBIRD_STORE_CONFIG_ENGINE is a template var for management.json — only works
  # if management.json contains $NETBIRD_STORE_CONFIG_ENGINE as a placeholder.
  # We set both for compatibility.
  printf '%s\n' \
    "# PostgreSQL backend for Netbird management server" \
    "# Created by switch-to-postgres.sh" \
    "NETBIRD_STORE_ENGINE=postgres" \
    "NETBIRD_STORE_CONFIG_ENGINE=postgres" \
    "NETBIRD_STORE_ENGINE_POSTGRES_DSN=host=netbird-postgres user=netbird password=${PG_PASS} dbname=netbird port=5432 sslmode=disable" \
    "NB_STORE_ENGINE_POSTGRES_DSN=host=netbird-postgres user=netbird password=${PG_PASS} dbname=netbird port=5432 sslmode=disable" \
    "NB_ACTIVITY_EVENT_STORE_ENGINE=postgres" \
    "NB_ACTIVITY_EVENT_POSTGRES_DSN=host=netbird-postgres user=netbird password=${PG_PASS} dbname=netbird_events port=5432 sslmode=disable" \
    > "$PG_ENV"
  chmod 600 "$PG_ENV"
  echo "  Created $PG_ENV"
fi

# Add env_file reference to netbird-server service in docker-compose.yml
if grep -q 'postgres.env' "$COMPOSE_FILE"; then
  echo "  docker-compose.yml already references postgres.env."
else
  # Insert env_file directive after the netbird-server service's "restart:" line.
  # This is the safest insertion point — it's inside the service block.
  sed -i '/container_name: netbird-server/{
    N
    s/\(restart: unless-stopped\)/\1\n    env_file:\n      - .\/postgres.env/
  }' "$COMPOSE_FILE"

  if grep -q 'postgres.env' "$COMPOSE_FILE"; then
    echo "  Added env_file: postgres.env to netbird-server service."
  else
    echo "  ERROR: Failed to inject env_file into docker-compose.yml."
    echo "  Add this manually under the netbird-server service:"
    echo "    env_file:"
    echo "      - ./postgres.env"
    echo "  Then run: docker compose up -d"
    exit 1
  fi
fi

# -----------------------------------------------
# Step 3b: Set engine to postgres in config.yaml
# -----------------------------------------------
# The env var NETBIRD_STORE_ENGINE is only a fallback — Netbird reads
# the engine from config.yaml first. If config.yaml says "sqlite",
# the env var is ignored.
NB_CONFIG="$NB_DIR/config.yaml"
if [ -f "$NB_CONFIG" ]; then
  if grep -q 'engine: "sqlite"' "$NB_CONFIG"; then
    sed -i 's/engine: "sqlite"/engine: "postgres"/' "$NB_CONFIG"
    echo "  Updated config.yaml: engine set to postgres."
  elif grep -q 'engine: "postgres"' "$NB_CONFIG"; then
    echo "  config.yaml already has engine: postgres."
  else
    echo "  WARN: Could not find engine setting in config.yaml. Check manually."
  fi
else
  echo "  WARN: $NB_CONFIG not found. Engine must be set via env var or management.json."
fi

# -----------------------------------------------
# Step 4: Ensure Postgres is on the Netbird network
# -----------------------------------------------
echo "[4/5] Connecting Postgres to Netbird network..."
# The network is named "netbird" in compose but Docker prefixes with project name
NB_NETWORK=$(docker network ls --format '{{.Name}}' | grep -E '^netbird[_-]netbird$' | head -1)
if [ -z "$NB_NETWORK" ]; then
  echo "  WARN: Could not find netbird network. Trying netbird_netbird..."
  NB_NETWORK="netbird_netbird"
fi
docker network connect "$NB_NETWORK" netbird-postgres 2>/dev/null || echo "  Already connected."
echo "  Postgres connected to $NB_NETWORK"

# -----------------------------------------------
# Step 5: Start Netbird
# -----------------------------------------------
echo "[5/5] Starting Netbird with Postgres backend..."
docker compose up -d

echo ""
echo "Waiting 15s for startup..."
sleep 15

echo ""
echo "Checking logs for store engine confirmation..."
STORE_LOG=$(docker logs netbird-server 2>&1 | grep -i "store engine" | head -5)
echo "$STORE_LOG"

# Verify Postgres is actually being used for the main store
if echo "$STORE_LOG" | grep -qi "using Postgres store engine"; then
  echo ""
  echo "  Main store: Postgres"
else
  echo ""
  echo "  WARNING: Main store may still be SQLite."
  echo "  NETBIRD_STORE_ENGINE env var is only a fallback — it is ignored when the"
  echo "  stored config already has an engine value. If the main store shows SQLite,"
  echo "  the config persisted in the data volume has 'sqlite' hardcoded."
  echo ""
  echo "  To force Postgres, find and edit the stored config:"
  echo "    docker exec netbird-server find /var/lib/netbird -name '*.json' 2>/dev/null"
  echo "  Or check if management.json needs StoreConfig.Engine set to 'postgres'."
fi

# Verify activity event store
if echo "$STORE_LOG" | grep -qi "using postgres as activity event store"; then
  echo "  Event store:  Postgres"
else
  echo "  WARNING: Event store may not be using Postgres."
fi

echo ""
echo "Container status:"
docker compose ps

# Check for restart loop
sleep 5
if docker compose ps | grep -q "Restarting"; then
  echo ""
  echo "  ERROR: netbird-server is in a restart loop."
  echo "  Check: docker compose logs netbird-server --tail 20"
  exit 1
fi

echo ""
echo "============================================"
echo "  Switch complete"
echo "============================================"
echo ""
echo "  Verify from LOCAL:"
echo "    curl -s -o /dev/null -w '%{http_code}' https://netbird.legacy.example"
echo "    sudo netbird status"
echo ""
echo "  Rollback: remove env_file line from docker-compose.yml, delete postgres.env, restart."
