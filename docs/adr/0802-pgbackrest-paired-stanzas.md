# ADR 0802 — pgBackRest paired stanzas — per-site sovereign backup

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Related | [`0801-postgresql-spock-multi-master.md`](0801-postgresql-spock-multi-master.md), [`0803-no-cross-site-pitr.md`](0803-no-cross-site-pitr.md), [`../design/pgbackrest.md`](../design/pgbackrest.md) |

## TL;DR (for a layperson)

Every site has two PostgreSQL nodes (Router-A and Router-B).
We back up each node independently using pgBackRest, with the
backup repo on local NVMe. This means each site has fully
sovereign backups — no cloud dependency in the backup path.
Cross-site durability comes from Spock replication; this
design accepts the trade-off that there's no cross-site
point-in-time recovery.

## Diagram



Source: [`diagrams/0802-pgbackrest-paired-stanzas.mmd`](diagrams/0802-pgbackrest-paired-stanzas.mmd).

## Context

Backup design has to balance:

- **Sovereignty** — no cloud backup path.
- **Island Mode** — backups must work without WAN.
- **PITR** — point-in-time recovery within a defined window.
- **Site loss** — surviving a site loss.

A single shared backup repo (cloud, central) violates
sovereignty. Per-router-shared repo within a site couples
the two routers. Per-router independent stanzas give each
router a complete backup that survives the other's loss.

Cross-site PITR is *not* offered ([`0803`](0803-no-cross-site-pitr.md))
— site-loss durability is carried by Spock replication.

## Decision

Two independent pgBackRest stanzas per site:

| Stanza | Location |
|---|---|
| `<site>-a-netbird` | Router-A's local NVMe at `/opt/docker-data/pgbackrest/` |
| `<site>-b-netbird` | Router-B's local NVMe at `/opt/docker-data/pgbackrest/` |

Properties:

- **Local NVMe** — sovereign, no cloud.
- **Independent stanzas** — each router runs its own backup
  to its own repo.
- **PITR window** — 7 days within site (configurable).
- **No cross-site PITR** ([`0803`](0803-no-cross-site-pitr.md)).
- **No off-site S3** in the routine path. Cloud PoCs may
  use S3 for the pgbackrest-wave18 / cloud rehearsal,
  not in production.

Wave 1.8 paired-stanza smoke test
(`../operations/pgbackrest-wave18-validation.md`)
validated the design end-to-end on real hardware
(2026-04-22 evidence).

## Alternatives considered

- **Single stanza shared between A and B.** Couples; single
  backup repo failure loses both. Rejected.
- **Cloud-only backups.** Loses sovereignty. Rejected.
- **Cross-site backup repo.** Adds WAN dependency. Rejected.
- **Off-site S3 mandatory in production.** Adds cloud
  dependency in the routine path. Rejected as default;
  permitted as a documented exception per site.

## Consequences

**Positive.** Sovereign per-site backups. Site-local PITR
within window. Cross-site site-loss durability via Spock.
No cloud dependency in backup path.

**Negative.** No cross-site PITR. Operationally significant;
documented as accepted trade-off in
[`0803`](0803-no-cross-site-pitr.md).

## Assessor view (SCA-R)

Paired stanzas serve CP-9 (system backup), CP-10 (system
recovery), and SC-28 (data-at-rest, paired with LUKS at
the storage layer). The accepted trade-off (no cross-site
PITR) is documented in [`0803`](0803-no-cross-site-pitr.md)
with the compensating control (Spock replication +
bootstrap-bundle for total-loss recovery).

## Trigger conditions for revisit

- A specific authorization requires cross-site PITR.
  Re-evaluate; design adds significant operational cost.
- NVMe capacity becomes the bottleneck. Add capacity per
  site; do not collapse to shared.

---

*See `GLOSSARY.md` for terms and acronyms.*
