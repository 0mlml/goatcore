# shellcheck shell=bash
# Two Spock sites in Docker. Sourced by the partition tests in this directory
# (partition-revoke, partition-harness); not run on its own.
#
# Caller sets, before sourcing:
#   IMAGE  postgres-spock image (built from ops/postgres/spock/Dockerfile)
#   PFX    container prefix; the sites are $PFX-a and $PFX-b
#   NET    docker network name
#   PW     postgres + replicator password (test fixture only)
# Optional:
#   TS_PORT_a / TS_PORT_b  publish the site's 5432 on 127.0.0.1:<port>
#
# Substrate: Linux Docker only (CI). Two bidirectional Spock nodes on macOS
# Docker Desktop wedge the daemon.

q() { # q <site a|b> <db> <sql>   (no -i: SQL comes from -c, and -i would drain a caller's while-read input)
  docker exec "$PFX-$1" psql -U postgres -d "$2" -v ON_ERROR_STOP=1 -tAXq -c "$3"
}

ts_wait_ready() { # ts_wait_ready <container>
  for _ in $(seq 1 90); do
    docker exec "$1" pg_isready -h 127.0.0.1 -p 5432 -U postgres >/dev/null 2>&1 && return 0
    sleep 2
  done
  echo "::error::$1 never accepted a TCP connection"; docker logs "$1" 2>&1 | tail -40; return 1
}

# Start both sites, check the ADR 1193 apply settings baked into the image,
# and create the replicator role the subscriptions connect as.
ts_up() {
  docker network create "$NET" >/dev/null
  local s port pub
  for s in a b; do
    port="TS_PORT_$s"; pub=()
    [[ -n "${!port:-}" ]] && pub=(-p "127.0.0.1:${!port}:5432")
    docker run -d --name "$PFX-$s" --network "$NET" -e POSTGRES_PASSWORD="$PW" "${pub[@]}" "$IMAGE" \
      postgres -c config_file=/etc/postgresql/postgresql.conf -c cluster_name="site_$s" >/dev/null
  done
  for s in a b; do ts_wait_ready "$PFX-$s"; done

  local got
  for s in a b; do
    got="$(q "$s" postgres "SELECT current_setting('spock.exception_behaviour') || ' ' || current_setting('spock.save_resolutions');")"
    [[ "$got" == "discard on" ]] || { echo "::error::$PFX-$s spock.exception_behaviour/save_resolutions = '$got', want 'discard on'"; return 1; }
  done
  echo "  exception_behaviour=discard save_resolutions=on"

  for s in a b; do
    q "$s" postgres "CREATE ROLE replicator WITH LOGIN SUPERUSER PASSWORD '$PW';"
    docker exec --user root "$PFX-$s" bash -c \
      "echo 'host all replicator 0.0.0.0/0 scram-sha-256' >> /etc/postgresql/pg_hba.conf"
    q "$s" postgres "SELECT pg_reload_conf();" >/dev/null
  done
}

ts_down() { # ts_down [extra containers...]
  docker rm -f "$PFX-a" "$PFX-b" "$@" >/dev/null 2>&1 || true
  docker network rm "$NET" >/dev/null 2>&1 || true
}

ts_node_create() { # ts_node_create <db>
  local s
  for s in a b; do
    q "$s" "$1" "SELECT spock.node_create('site_$s', 'host=$PFX-$s dbname=$1 user=replicator password=$PW');" >/dev/null
  done
}

# Subscriptions both ways, as spock-mesh-peering and ory-narrow-repset create
# them: no initial sync, no forwarded origins.
ts_sub_create() { # ts_sub_create <db> <repset> [repset...]
  local db="$1"; shift
  local sets; sets="ARRAY['$(IFS=,; echo "$*" | sed "s/,/','/g")']"
  local sub prov
  for pair in "a b" "b a"; do
    read -r sub prov <<<"$pair"
    q "$sub" "$db" "SELECT spock.sub_create(
        subscription_name := 'sub_${prov}_to_${sub}',
        provider_dsn      := 'host=$PFX-$prov dbname=$db user=replicator password=$PW',
        replication_sets  := $sets,
        synchronize_data  := false,
        forward_origins   := ARRAY[]::text[]);" >/dev/null
  done
}

ts_wait_replicating() { # ts_wait_replicating <db>...
  local db sub prov st
  for db in "$@"; do
    for pair in "a b" "b a"; do
      read -r sub prov <<<"$pair"
      st=""
      for _ in $(seq 1 60); do
        st=$(q "$sub" "$db" "SELECT status FROM spock.sub_show_status() WHERE subscription_name='sub_${prov}_to_${sub}';" | tr -d '[:space:]')
        [[ "$st" == replicating ]] && break
        sleep 1
      done
      [[ "$st" == replicating ]] || { echo "::error::$db sub_${prov}_to_${sub} is '$st'"; return 1; }
    done
  done
}

# Partition = both subscriptions disabled; heal = both enabled.
ts_cut() { # ts_cut <db>...
  local db
  for db in "$@"; do
    q a "$db" "SELECT spock.sub_disable('sub_b_to_a', immediate := true);" >/dev/null
    q b "$db" "SELECT spock.sub_disable('sub_a_to_b', immediate := true);" >/dev/null
  done
}
ts_heal() { # ts_heal <db>...
  local db
  for db in "$@"; do
    q a "$db" "SELECT spock.sub_enable('sub_b_to_a', immediate := true);" >/dev/null
    q b "$db" "SELECT spock.sub_enable('sub_a_to_b', immediate := true);" >/dev/null
  done
}
