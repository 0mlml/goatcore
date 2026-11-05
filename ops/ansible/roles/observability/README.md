# observability role

Deploys the Phase 2 observability MVP onto an mgmt-host: Prometheus +
Alertmanager + node-exporter + postgres-exporter, scraping local services
and the peer mgmt-host across the Netbird mesh.

Composes on top of the existing mgmt-host base stack (`docker-compose.yml`)
via `docker-compose.obs.yml` — no changes to the base file.

## Variables

Required per-host (set in inventory):

| Var | Example | Meaning |
|---|---|---|
| `site_name` | `vega-a` | Prometheus `external_labels.site` |
| `peer_node_exporter_target` | `100.127.1.12:9100` | peer mgmt-host mesh IP + node-exporter port |
| `peer_signal_metrics_target` | `100.127.1.12:9090` | peer mgmt-host mesh IP + signal-server /metrics port |

Required at play invocation time (from env / 1Password):

| Var | Meaning |
|---|---|
| `postgres_superuser_password` | needed to bootstrap the `monitoring` PG role |
| `postgres_exporter_password` | password granted to the `monitoring` role |

## Idempotency

- PG role creation uses `CREATE ROLE IF NOT EXISTS`.
- Password reset runs every play (rotation = re-run).
- Compose pull + up are declarative; Docker skips no-op cases.

## Source of truth

Alert rules / prometheus.yml / alertmanager.yml live in
`ops/compose/mgmt-host/monitoring/` and are copied verbatim by this role.
The `ops/monitoring/` tree is the original skeleton and should stay in
sync — when you update rules here, mirror them there.
