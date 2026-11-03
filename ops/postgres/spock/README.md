# postgres-spock

Spock-enabled PostgreSQL 16 image for the goat multi-site mesh.

Design: `docs/design/ha-multi-site.md` §5.

## What it is

A thin overlay on `ghcr.io/pgedge/pgedge-postgres:16-spock5-minimal`:

- Config file baked in (`postgresql.conf`, `pg_hba.conf`) tuned for Spock.
- Init SQL (`init/*.sql`) runs on first boot: verifies config, creates the
  three Netbird databases, loads the Spock extension per replicated DB.

Node registration (`spock.node_create`) and subscriptions (`spock.sub_create`)
are driven by Ansible per-site, not baked in — every node needs site-specific
DSNs, peer lists, and a replicator password.

Published as `ghcr.io/usareur-af/postgres-spock` by the matching CI workflow
([.github/workflows/postgres-spock.yml](../../../.github/workflows/postgres-spock.yml)),
multi-arch (`linux/amd64` + `linux/arm64`).

## Layout

```
ops/postgres/spock/
├── Dockerfile
├── conf/
│   ├── postgresql.conf      baked into image; used via `-c config_file=...`
│   └── pg_hba.conf          same; Ansible renders site-specific replicator
│                            lines between the BEGIN/END markers at deploy time
├── init/
│   ├── 00-verify-config.sql fails fast if wal_level/spock preload missing
│   ├── 01-databases.sql     CREATE DATABASE netbird, netbird_idp, netbird_events
│   └── 02-spock-extension.sql CREATE EXTENSION spock in replicated DBs
└── README.md
```

## Run

Always start with the baked config path:

```
docker run -d --name pg-a \
  -p 5432:5432 \
  -e POSTGRES_PASSWORD=... \
  ghcr.io/usareur-af/postgres-spock:latest \
  postgres -c config_file=/etc/postgresql/postgresql.conf
```

The `init/00-verify-config.sql` script will abort startup if you forget the
`-c config_file=...` and end up with the initdb-default `postgresql.conf`
(which has `wal_level=replica`).

## Spock prerequisites encoded in the config

The bits of `postgresql.conf` that matter for replication correctness
(captured here because it's easy to drift):

| Setting | Value | Why |
|---|---|---|
| `wal_level` | `logical` | Required for any logical replication |
| `max_replication_slots` | `10` | One slot per remote subscriber per DB |
| `max_wal_senders` | `10` | Same — one per outbound replication stream |
| `max_slot_wal_keep_size` | `1GB` | Safety valve — invalidates a slow slot before disk fills. **Critical for island-mode scenarios.** |
| `track_commit_timestamp` | `on` | Required for LWW conflict resolution |
| `shared_preload_libraries` | `spock` | Loads the Spock extension at startup |

## Operational notes

- **Coordinated upgrades**: Netbird AutoMigrate-emitted DDL does not replicate
  through Spock. Follow `docs/design/ha-multi-site.md` §6
  before upgrading Netbird at any site.
- **Slot invalidation recovery**: if `max_slot_wal_keep_size` is exceeded
  during a multi-day partition, the slot goes invalid and the disconnected
  node must be rebuilt via `pg_basebackup` + re-subscribe. Runbook:
  [`docs/operations/post-partition-convergence-runbook.md`](../../../docs/operations/post-partition-convergence-runbook.md) §A.
