# ops/

Operational scripts and out-of-band state for goat infrastructure.

## Contents

| File | Purpose |
|---|---|
| `backup.sh` | Back up network host state to LOCAL, then upload to 1Password |
| `restore.sh` | Download backup from 1Password, SCP to the network host, restore services |
| `preflight-enrollment.sh` | Block 1D — validate prerequisites before `netbird up` (CLI installed, no stale wt0, setup key present, mgmt URL reachable, clock within ±60s) |
| `dns/` | Mesh DNS server (dnsmasq) — resolves `*.netbird.legacy.example` locally |
| `postgres/` | PostgreSQL backend for Netbird (docker-compose, migration scripts) |
| `snapshots/` | Local backup snapshots (gitignored) |
| `restore-tmp/` | Temporary restore workspace (gitignored) |

## Mesh DNS

Runs dnsmasq on the network host to serve `*.netbird.legacy.example` records from a local hosts file. Works without internet (Island Mode). All mesh peers query it via the network host's Netbird overlay IP.

Deploy: `scp -r ops/dns/ goat@<HOST>:/opt/goat/dns/` then `docker compose up -d` on the network host.

Update peers: edit `dns/mesh-hosts`, then `docker compose restart dnsmasq`.

Auto-sync: `dns/sync-peers.sh` queries the Netbird API and regenerates the hosts file. Requires a Personal Access Token (PAT) from the dashboard.

## Backup

```
bash ops/backup.sh
```

Creates a timestamped snapshot of the network host's critical state, saves locally, and uploads to 1Password.

If the `op` upload hangs (needs interactive signin), upload manually:
```
op document create ops/snapshots/<snapshot-name>.tar.gz --title "<snapshot-name>" --vault <vault>
```

## Restore

```
bash ops/restore.sh                              # latest backup
bash ops/restore.sh goat-backup-20260324  # specific backup
```

Downloads from 1Password → extracts on LOCAL → SCPs to the network host → restores configs and volumes → restarts services. Confirms before overwriting.

**1Password CLI runs on LOCAL only.** The network host never has `op` installed or any 1Password credentials. The restore flow is:

```
1Password → op get → LOCAL disk → scp → network host → extract → restart
```

## What's backed up

- `/opt/netbird/` — Docker Compose config, Netbird environment, dashboard config
- `/opt/goat/postgres/` — PostgreSQL config and credentials
- Docker volumes (`netbird_netbird_data`, `netbird_netbird_traefik_letsencrypt`, `postgres_pgdata`) — management state, TLS certs, Postgres data
- `/etc/hosts` — Domain resolution overrides
- System state snapshot (IP addresses, netbird status, container status)
