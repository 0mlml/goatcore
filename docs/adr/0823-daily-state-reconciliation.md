# ADR 0823 — Daily cross-site state reconciliation as a low-tech fallback to Spock

| Field | Value |
| --- | --- |
| Status | **Proposed** |
| Date | 2026-04-27 |
| Author | Claude (drafted on author's behalf) |
| Repo scope | goat core |
| Related ADRs | 0801 (Spock multi-master), 1010 §D5 (ACE drift-watch), 1010 §D7 (Hydra pre-1970 pre-seed), 1008 (soak-testing discipline), 0205 (intra-site VRRP), 0411 (Ory MVP) |
| Supersedes | none |
| Superseded by | none |

## Context

Spock multi-master logical replication is the primary cross-site replication mechanism (per `docs/design/ha-multi-site.md` §4.2). Today's evidence (integration-env cycle 3, soak v4 in flight) confirms it works under our workload. ADR 1010 §D5 adds the ACE drift-watch hourly cron as a second layer to surface row-level divergence within an hour.

Both layers are necessary but not sufficient against the class of failure mode that is **silent**: a Spock subscription that reports `replicating` while its slot is inactive (we observed this exact state on a lab instance); an apply worker that has caught a non-fatal exception and decided to skip events; a network partition that healed but left state divergent. None of these throw alerts; all of them slowly corrupt the cluster's view of reality.

Defense-in-depth is the goal. We don't expect to need this fallback; we want it present so we can fall back to it if Spock proves operationally unstable in production scenarios we couldn't have predicted.

## Decision

Add a **third time-horizon layer**: a daily cross-site state reconciliation cron, run at 0000 UTC, that:

1. Snapshots identity-state tables on each site via `pg_dump`
2. Pushes the dump to a designated continuity store (per-site MinIO with cross-mount)
3. The hub computes a canonical merged view per per-table conflict policy
4. Emits operator-visible diff if any site's local state differs from canonical

In **advisory mode** (default), the reconciler only reports the diff. In **authoritative mode** (opt-in per site, after ≥30 days of clean advisory evidence), the reconciler can write canonical state back to drifted sites.

A companion **schema-coherence tracker** runs hourly (faster cadence than the daily reconciler because schema changes are rare and divergence is a real bug that should halt data reconciliation).

Full design at [`../design/daily-state-reconciliation.md`](../design/daily-state-reconciliation.md).

## Why this complements Spock rather than replacing it

| Layer | Cadence | Mechanism | What it catches |
| --- | --- | --- | --- |
| L1 — Spock multi-master | sub-second | Logical replication | Normal everyday churn |
| L2 — ACE drift-watch + schema-coherence tracker | hourly | pgEdge ACE row-diff + `pg_dump -s` hash compare | Row-level divergence; schema drift between sites |
| L3 — Daily cross-site reconciler (this ADR) | 24h | `pg_dump` + per-table-conflict-policy merge + diff | What L1+L2 missed; the canonical-state anchor for operator decisions |

Layers have independent failure modes. L1 dies → L2 catches within an hour. L2 dies → L3 catches within a day. The reconciler is not a replacement for Spock; on healthy days it's a no-op verification.

## Decision criteria

| # | Criterion | Threshold |
| --- | --- | --- |
| 1 | Does the daily cron complete reliably? | 30 consecutive days of successful runs in advisory mode |
| 2 | Does the diff output stay small / categorizable on healthy days? | <100 row diff after threshold filtering; >95% of diffs categorized as in-flight Spock lag (auto-suppressed) |
| 3 | When divergence DOES occur, does the reconciler correctly identify the canonical state? | Operator review concurs with reconciler's choice on every alerted event for 30 days |
| 4 | Is the storage cost bounded? | <10 GB cumulative across all sites for 90-day retention |
| 5 | Does it run without disturbing Spock? | No measurable Spock lag introduced by the snapshot operation; pg_dump uses ACCESS SHARE so no write blocking |

## Phasing

| Phase | What lands |
| --- | --- |
| Phase 2D (now) | This ADR + design doc + dry-run script (snapshots taken, diffs computed, **no alerts wired**). Validates L3 mechanics on real production state. |
| Phase 3 | Advisory mode promotion (alerts wired via Block 17A.5 policy engine pipeline). Begins 30-day clean-evidence soak. |
| Phase 3+ | If 30-day clean: authoritative-mode option becomes available — opt-in per site. Some sites may stay advisory-only forever (operator-decision is the safety belt). |

## Open questions before implementation

This ADR is **Proposed** because the following design questions need to be wrestled with before the code can be trusted:

1. **Per-table conflict policies are messier than one-line rules.** "Earliest `created_at` wins" for `identities` causes silent loss of the loser's credentials, sessions, audit trail. Real per-table policies will be in the design doc; each one needs operator + author sign-off.
2. **Non-data divergence beyond schema:** sequence values, FK constraints, extensions installed. Schema-coherence tracker handles extensions; the others are subtler.
3. **Hub-and-spoke SPOF:** 24h election fallback may be too slow during a hub outage. Hub-less mesh exchange (n×n) is the alternative; weigh complexity.
4. **`pg_dump` on a live DB at 0000 UTC across sites is not a coordinated snapshot.** Sites' 0000 UTC snapshots are different points in the WAL stream → diffs include in-flight Spock lag. Need a lag-threshold suppression filter.
5. **Operator diff fatigue:** if the daily output is always non-empty, operators learn to ignore it. Threshold-based alerting + categorization required.
6. **"Authoritative" mode is dangerous.** Wrong canonical pick + auto-write = data destruction. 30-day soak is necessary but not sufficient; need dry-run preview + Block 17A.5 two-of-N approval per write + per-write rollback snapshot.
7. **"Do NOT reconcile" list:** `netbird_events` (per-site by design), Kratos `selfservice_*_flows` (ephemeral), short-lived OAuth tokens, anything with TTL.
8. **Schema migration windows:** when a Kratos schema bump deploys, sites are briefly on different versions → schema-coherence tracker fires. Need a maintenance-window concept ("expected divergence" mode).
9. **NTP discipline is a hard dependency.** "Earliest `created_at` wins" depends on clocks agreeing. Already covered by `docs/design/ntp-strategy.md` + ntp-health-agent — the reconciler must depend on the NTP attestation being green before it trusts timestamps.

These items live as TODO sections in the companion design doc. Implementation does not commence on any item until its open question is resolved.

## Alternatives considered

- **Trust Spock alone.** Rejected: silent failure modes are exactly what the reconciler catches. The cost of L3 (small daily cron) is negligible relative to the value of catching one silent corruption event.
- **Schema-coherence tracker only, no data reconciliation.** Rejected: schema is one failure axis among several. Data divergence (e.g., today's near-miss with Hydra pre-1970 nid) wouldn't be caught.
- **Continuous reconciliation (sub-hour).** Rejected for v1: continuous reconciliation duplicates Spock's job and is harder to operator-reason-about. Daily is enough for a backstop; ACE drift-watch fills the hourly slot.
- **Patroni-style synchronous replication.** Rejected per ADR 0801 — synchronous defeats island mode (a partitioned site couldn't commit identity writes locally), which is a load-bearing property.
- **Point-in-time recovery from pgBackRest.** Different problem — backup is for disaster recovery, not for surfacing live state divergence.

## Consequences

**Positive:**
- Defense-in-depth against silent Spock failure modes, including ones we couldn't predict
- Daily cross-cut diff is a debugging tool even when nothing is broken
- Schema-coherence tracker catches an entire failure class (today's pinned-SHA-without-contribs pattern would have been caught instantly)
- Dry-run advisory mode is harmless; operators learn the system before trusting it

**Negative:**
- Additional operational surface: cron, snapshots, MinIO bucket policy, alerting
- Operator discipline required: respond to advisory alerts, eventually decide on authoritative-mode promotion per site
- Storage cost: ~10 MB/day/site × 90 days ≈ 1 GB per site; manageable, watch for growth
- Authoritative mode introduces a new write path that could destroy data if conflict policies are wrong

**Neutral:**
- Forces explicit articulation of per-table conflict policy (a thing we should have documented anyway)
- Forces operator workflow design for "advisory diff arrives, what do I do?"

## Cross-references

- Design doc: [`../design/daily-state-reconciliation.md`](../design/daily-state-reconciliation.md)
- Parent: [`0801-postgresql-spock-multi-master.md`](0801-postgresql-spock-multi-master.md) (the thing this is fallback for)
- Discipline: [`1008-soak-testing-discipline.md`](1008-soak-testing-discipline.md) (advisory→authoritative promotion follows soak-discipline shape)
- Operating: [`1010-ory-on-spock-mindful-operation.md`](1010-ory-on-spock-mindful-operation.md) §D5 (ACE drift-watch — sibling layer)
- Storage: Block 17D MinIO Object Lock integrity tier (snapshot durability)
- Alerting: Block 17A.5 policy engine notification pipeline (operator alerts)

---

*See GLOSSARY for acronyms.*
