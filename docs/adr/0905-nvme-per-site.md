# ADR 0905 — NVMe per-site at `/opt/docker-data` — 1TB M.2 minimum

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Related | [`0802-pgbackrest-paired-stanzas.md`](0802-pgbackrest-paired-stanzas.md), [`0906-three-tier-storage-layout.md`](0906-three-tier-storage-layout.md), `../design/ha-multi-site.md` |

## TL;DR (for a layperson)

Every production site has at least a 1TB M.2 NVMe drive
mounted at `/opt/docker-data`, which holds the database,
backups, audit logs, and container persistent volumes. NVMe
performance is what makes the multi-master replication and
the audit-integrity tier work without operator pain.

## Context

The mgmt-host stack writes a lot of data (Postgres + Spock
replication, pgBackRest stanzas, audit logs, observability
metrics). Slow storage propagates into replication lag,
backup duration, and operator response time.

NVMe M.2 storage is dramatically faster than SATA SSD or
spinning disk and is the right baseline for the management-
host workload.

## Decision

Per-site storage requirement:

- **At least 1TB M.2 NVMe** at `/opt/docker-data` on every
  production mgmt-host.
- **Mount**: bind-mounted into containers that need
  persistence (Postgres, pgBackRest, MinIO, Prometheus,
  Loki/Promtail, etc.).
- **Encryption**: LUKS ([`0604`](0604-luks-data-at-rest.md)).
- **Layout**: three-tier storage layout per
  [`0906`](0906-three-tier-storage-layout.md).

Per `../design/ha-multi-site.md`
§14, every production Orin site gets a 1 TB M.2 NVMe.

## Alternatives considered

- **SATA SSD only.** Adequate for some workloads;
  Postgres + Spock replication latency degrades. Rejected as
  baseline.
- **Network-attached storage.** Loses Island Mode property
  if NAS is on a separate device with its own dependencies.
  Rejected.
- **Larger NVMe (2TB+).** Acceptable if budget allows.
  1TB is the floor.

## Consequences

**Positive.** Replication lag stays low. Backup duration
predictable. Audit-tier performance adequate.

**Negative.** Per-site cost. Acceptable.

## Assessor view (SCA-R)

NVMe per site serves CP-9 (system backup — backup
performance), AU-5 (response to audit logging failures —
audit tier write performance), and SI-4 (system monitoring
— metrics ingestion latency).

## Trigger conditions for revisit

- Workload outgrows 1TB. Upgrade per site; revise the
  baseline floor.
- A specific site cannot accommodate NVMe (rare). Document
  the exception with explicit performance impact analysis.

---

*See `GLOSSARY.md` for terms and acronyms.*
