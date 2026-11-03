# ADR 0502 — RocksDB ops/** storage with 7-day TTL

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Related | [`0500-zenoh-data-fabric.md`](0500-zenoh-data-fabric.md), [`0506-fabric-schema-evolution.md`](0506-fabric-schema-evolution.md) |

## TL;DR (for a layperson)

Some data on the fabric needs to be queryable later — late
joiners want to catch up, operators want to inspect recent
events. We keep that data in a small embedded database
(RocksDB) attached to each Zenoh router, scoped to the
`ops/**` keyspace, with a 7-day expiration. After 7 days the
data is gone; if it needs to live longer, it goes to a different
storage class.

## Context

Two distinct persistence requirements on the fabric:

1. **Short-term replay** — late-joining peers, recent-history
   queries, observability backfill. Days, not months.
2. **Long-term archive** — audit, compliance, mission record.
   Weeks to years. Different storage class
   ([`0811`](0811-audit-log-integrity.md) covers audit).

For (1), RocksDB embedded in the Zenoh router is the right
fit: cheap, fast, no separate operational dependency, scoped
to the keyspace it serves.

## Decision

Zenoh's storage_manager plugin holds `ops/**` in RocksDB:

- **Backend:** RocksDB embedded.
- **Scope:** `ops/**` keyspace. Telemetry, gatewatch events,
  status, isitup data.
- **TTL:** 7 days. Expired keys are auto-deleted.
- **Per-router:** every Zenoh router has its own storage;
  cross-router replication via the federation pattern.
- **Sizing:** sized for ~7 days of expected throughput; alerts
  fire on disk-pressure.

For data needing longer retention, the destination is
explicit:

- **Audit logs** → MinIO Object Lock
  ([`0811`](0811-audit-log-integrity.md)).
- **Metrics** → Prometheus / long-term metrics store.
- **Mission archive** → out-of-scope (not our role per
  [`0108`](0108-scope-discipline-own-the-transport.md)).

## Alternatives considered

- **No storage layer (pure pub/sub).** Loses replay; loses
  late-join semantics. Rejected.
- **External database (PostgreSQL).** Adds operational
  dependency; not optimized for the keyspace pattern.
  Rejected.
- **In-memory only.** Loses persistence across router
  restart. Rejected.

## Consequences

**Positive.** Replay and late-join work. Storage scoped to
the use case. TTL prevents unbounded growth.

**Negative.** Disk pressure on the router host if traffic
spikes. We size and monitor.

## Assessor view (SCA-R)

RocksDB storage serves SI-12 (information output handling
and retention) for short-term replay use cases; longer-term
retention has separate storage classes with appropriate
controls. AU-11 (audit record retention) is *not* satisfied
here — it is satisfied at the audit-log-integrity tier.

## Trigger conditions for revisit

- 7-day TTL proves wrong (too short, too long). Adjust.
- Storage volume growth outpaces the per-router host
  capacity. Investigate sharding within keyspace; do not
  abandon embedded storage casually.

---

*See `GLOSSARY.md` for terms and acronyms.*
