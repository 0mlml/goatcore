# ADR 1101 — Zenoh lazy per-slot storage: declare a rocksdb store per *live* slot, not per pool slot

**Status:** Accepted (2026-07-29) — built, live on ZAGROS, load-validated.
**Repo scope:** goat core (`nova-zenoh` role, `goat-zenoh-portal`)
**Related:** [ADR 1025](1025-zenoh-dynamic-tenant-authz-slot-pool.md) (the slot pool this extends on the *storage* axis) · findings F-186 (store-per-slot multiplication) · F-268 (the deadlock whose cost scales with store count) · F-156 (runtime store add/remove is via admin-REST, not SIGHUP) · design `zenoh-storage-taxonomy-sharding.md` · runbook `zenoh-lazy-storage-cutover-runbook.md`

## Context

ADR 1025 pre-cuts N ACL slots up front so onboarding is restart-free — correct. But
the *implementation* also declared **one rocksdb storage per pool slot** (a
`range(pool_size)` loop → a `<anchor>/**` store each). Measured on a live goatnet: **roughly three
in four stores held no data** — empty stores for slots that never onboarded. Store
count is the O(N) that the `adminspace_getter` fan-out, the restart-reopen time,
and the FD/thread overhead all scale with. The ACL pool size (cheap headroom) was
wrongly coupled to the store count (should follow actual onboards).

## Decision

Declare a slot's rocksdb store **only when the slot is live**, not for the whole pre-cut
pool. Everything else is unchanged: the per-`<uuid>/**` store (clean isolation),
`rm`-the-dir offboarding, and the **ACL slot pool** (a *separate* template loop — write
isolation is reproduced, not touched).

Two components:
1. **Config (ansible):** the storage loop iterates the **active** slot anchors, not
   `range(pool_size)`. Interim driver = anchors whose dir already holds `.sst` data
   (discovered by a role task); registry-derived long-term.
2. **Portal (Go):** on slot mint, the portal live-adds the slot's `<anchor>/**` store via
   the admin-REST API so the store exists before first
   write. Offboard removes the dir (unchanged).

This **decouples ACL pool size from store count** — a 10k-slot ACL pool can run with only
as many stores as there are live tenants.

## Consequences

- **Cutover (rollback-armed):** store count fell to the live-slot count, every
  empty orphan dir reclaimed, **ACL rules unchanged**. Single admin walk
  ~70 ms → 16 ms; 32 concurrent walks @ 1 CPU 2.7–4.5 s → 0.2–0.5 s, no wedge. No data
  migration — the active stores already existed on disk; the new config just stops
  declaring the empty ones.
- **Rollback:** `zenoh_lazy_slot_storage=false` + redeploy (re-declares the full pool), or
  restore the pre-cutover config backup. No store is deleted by the change.
- **Read/write model untouched:** partner/non-ally read control stays ACL (M1),
  subset-multilateral stays encryption (M2); neither operates on stores.

## Alternatives considered

- **Single prefix-keyed store over `**`** — removes the ×N entirely, but
  per-slot retention/quota then needs a key-prefix lifecycle mechanism instead of
  per-store config, and offboarding is no longer an atomic `rm`. Rejected as a larger
  change for no additional headroom at sandbox scale.
- **Per-taxonomy store (one per ADR-0844 class)** — a discarded earlier draft: live keys
  showed publishers write arbitrary topic names under the uuid (an unbounded set, not ~12
  fixed classes), so it would make a store per topic name. See the design doc's design note.
