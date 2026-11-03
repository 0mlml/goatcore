# Daily cross-site state reconciliation

> **Status: Proposed (ADR 0823, 2026-04-27).** This doc walks through the
> design; ADR 0823 captures the decision and decision criteria. Several
> open questions in §10 must be resolved before implementation begins.

## 1. Problem

Spock multi-master logical replication (`ha-multi-site.md` §4.2) is the primary cross-site replication mechanism. ADR 1010 §D5 adds the pgEdge ACE drift-watch hourly cron as a second layer to surface row-level divergence within an hour. Both layers are necessary but neither catches the class of failure that is **silent** — Spock subscriptions reporting `replicating` while slots are inactive (we observed this on Mac on 2026-04-25), apply workers silently skipping events, partitions that healed but left state divergent.

We need a third layer that's:

- **Lower-tech** than Spock — easy to reason about, well-understood primitives (`pg_dump`, `rsync`, `bash`)
- **Independent failure mode** — if Spock breaks, this still works; if this breaks, Spock still works
- **Different time horizon** — daily, not sub-second; backstop, not primary
- **Operator-visible** by default — the daily diff is the loudest signal in the system on broken days

## 2. Three-layer time-horizon model

| Layer | Cadence | Mechanism | What it catches |
| --- | --- | --- | --- |
| **L1 — Spock multi-master** | sub-second | Logical replication via Spock 5.x | Normal everyday churn — the primary path |
| **L2a — ACE drift-watch** | hourly | pgEdge ACE row-level diff per ADR 1010 §D5 | Row-level divergence within an hour |
| **L2b — Schema-coherence tracker** | hourly | `pg_dump -s` SHA-256 hash compare across sites | Schema drift (extension installed on one site only, migration applied on one site only, etc.) |
| **L3 — Daily cross-site reconciler** | 24h, 0000 UTC | `pg_dump` snapshot → cross-site exchange → canonical merge → operator-visible diff | What L1+L2 missed; the canonical-state anchor for operator decisions |

Each layer has independent failure modes. L1 dies → L2 catches within an hour. L2 dies → L3 catches within a day. All three die → real outage, but at that point Spock failing silently for >24h is something we'd want operator attention on regardless.

## 3. L3 reconciler — operating sequence

**At 0000 UTC each site** (cron + retry-with-jitter to avoid thundering herd):

1. **Snapshot identity-state tables.** `pg_dump --data-only --table=...` for the explicit reconcile-list (§5). Small (~10 MB compressed at our scale).
2. **Push dump to designated continuity store.** Per-site MinIO with cross-mount (Block 17D infrastructure). Hub: rear continuity site by default.
3. **At 0030 UTC the hub runs the reconciler.** Reads each site's dump; computes a canonical merged view per per-table conflict policy (§6); compares canonical merge vs each site's local state.
4. **If diverged, emit operator alert** through Block 17A.5 policy engine notification pipeline (Matrix room + email + dashboard widget).

## 4. L2b schema-coherence tracker — operating sequence

**Every hour at :17 past** (jitter to avoid bunching):

1. `pg_dump --schema-only --no-owner --no-acl <db> | sha256sum` per database per site.
2. Push hash to a known location (continuity store key per site per DB).
3. Hub reads all sites' hashes, alerts if any database's hash differs across sites.
4. **If schema drift detected, the daily L3 reconciler enters HALT mode for the affected DB** (don't try to merge across schema mismatch).

The hourly cadence is deliberate: schema changes are deliberate operator events (migrations) and should be alerted-on faster than data drift.

## 5. Reconcile-list — what gets snapshotted

| Database | Tables included | Tables EXCLUDED (and why) |
| --- | --- | --- |
| `kratos` | `identities`, `identity_credentials`, `identity_credential_identifiers`, `networks` | `selfservice_*_flows` (ephemeral, in-flight); `sessions` (TTL); `continuity_containers` (per-site) |
| `hydra` | `hydra_client`, `hydra_jwk`, `networks` | `hydra_oauth2_*` short-lived tokens (TTL); `hydra_oauth2_authentication_session` (per-site) |
| `netbird` | `accounts`, `peers`, `users`, `policies`, `groups` | `events` (per-site, unreplicated by design — see ha-multi-site.md §5.2); `nameservers` (per-site config) |
| `netbird_idp` | (none — Dex single-tenant per site) | All — Dex is per-site by design (ADR 0411 + 0401) |

**The exclusion list is load-bearing.** Including ephemeral / TTL'd / per-site tables produces non-divergence "diffs" every day (table contents legitimately differ across sites) → operator fatigue. The reconciler MUST trust this list.

## 6. Per-table conflict policy

| Table | Policy | Caveats |
| --- | --- | --- |
| `kratos.identities` | Earliest `created_at` wins on ID collision | If two sites created different identities with the same email during partition: loser's credentials, sessions, audit trail vanish. **This is real data loss from the user's perspective.** Operator review on every collision in advisory mode. |
| `kratos.identity_credentials` | Union by `(identity_id, type)` tuple | Kratos enforces `identity_credentials.identifier_unique` at insert time — two sites adding same identifier to different identity_ids will conflict at apply time. Operator decision required. |
| `kratos.identity_credential_identifiers` | Union; on identifier collision, attach to whichever identity won under the `identities` policy | Cascades from `identities` decision |
| `kratos.networks` (and `hydra.networks`) | Must be IDENTICAL across all sites | This is the pre-1970 pre-seed shared row from ADR 1010 §D7. Divergence = real bug, not a merge case. **HALT reconciliation; alert operator.** |
| `hydra.hydra_client` | Earliest `created_at` wins on `id` collision | Same shape as `identities`. Cross-site OAuth-client uniqueness assumed (UUIDs). |
| `hydra.hydra_jwk` | Never reconcile | Key rotation is per-site by design. Excluded from reconcile-list. |
| `netbird.accounts` | Manual review on conflict — never auto-reconcile | Account-level changes (settings, DNS) are operator decisions, not auto-mergeable. |
| `netbird.peers` | Latest `last_seen` wins on `id` collision | Other state on the row (groups, ACL group memberships) may diverge. The "latest last_seen" policy drops the other site's group changes. **Open question §10.2.** |
| `netbird.users` | Earliest `created_at` wins | Same shape as identities |
| `netbird.policies` / `netbird.groups` | Manual review on conflict | Operator decisions, like accounts. |

**These policies need per-table operator + author sign-off before authoritative mode is enabled.** The advisory-mode period is intended to collect real-world conflict cases that test these policies.

## 7. Operating modes

### Advisory (default)

Reconciler computes the diff, alerts operator, **does NOT write back.** Operator investigates each alerted divergence, decides what (if anything) to do. This is what ships in Phase 3.

### Authoritative (opt-in, post-soak)

Same compute step, but reconciler ALSO writes the canonical state back to any site whose local state differs.

Promotion criteria:

- ≥30 days clean evidence in advisory mode (no false positives requiring operator discounting)
- Per-table policies validated against real-world conflict cases observed in those 30 days
- Block 17A.5 two-of-N approval workflow wired up for each authoritative write
- Per-write rollback snapshot mechanism (snapshot before write; rollback on operator demand)

**Each site can independently choose to stay advisory-only forever.** The operator-decision is the safety belt.

## 8. Topology — hub-and-spoke with rotating-hub fallback

**Default hub:** rear continuity site (resourced HQ; designated continuity anchor in scope envelope per ADR 0215).

**Election fallback:** if hub unreachable for >24h, forward sites elect via lexicographic-by-site-name (deterministic, no consensus needed). The 24h delay is a known issue (open question §10.3).

**Hub-less alternative:** every site exchanges with every other site (n×n). Considered for v2 if hub election proves problematic in practice. Higher message volume but no SPOF.

## 9. Storage

- **Per-site MinIO bucket** (already deployed for Block 17D audit-log integrity tier)
- **Cross-mount** to peer sites' MinIOs via Spock-replicated MinIO config
- **Retention:** 90 days hot (queryable); older snapshots archive to S3 (cold storage; forensic-only)
- **Estimated volume:** ~10 MB/day/site × 90 days ≈ 1 GB per site. Manageable. If identity churn or mission-data growth changes the picture, retention shortens before storage costs become noticeable.
- **Snapshot integrity:** snapshots themselves are subject to MinIO Object Lock (Block 17D pattern) — tamper-evident.

## 10. Open questions — must be resolved before implementation

The ADR is **Proposed** because these need real wrestling. Each lives as a TODO until resolved.

### 10.1 Per-table conflict policy refinement

The §6 policies are sketches. Real per-table treatment needed for:

- `identities` data-loss-on-collision: do we even auto-reconcile, or always operator-review?
- `identity_credentials.identifier_unique` constraint violations during apply: how does the reconciler handle them?
- `peers` non-`last_seen` columns (groups, ACL membership): which site wins for each?

### 10.2 Non-data divergence beyond schema

Schema-coherence tracker (§4) handles extensions installed on one side only. Doesn't handle:

- Sequence value collisions (two sites' auto-increment values overlap → reconcile insert fails)
- FK constraint violations on reconciler insert (target row not on receiving side)
- Index state divergence (one site has REINDEX'd, another hasn't)

### 10.3 Hub-and-spoke SPOF

24h election fallback is too slow during a hub outage — no reconciliation happens during exactly the window when it's most likely to be useful. Either:

- Shrink election window (hours, not 24h) with explicit operator override
- Switch to hub-less n×n exchange

Trade-off needs to be made before Phase 3 promotion.

### 10.4 Snapshot coordination

Sites' 0000 UTC snapshots are different points in the WAL stream → diffs include in-flight Spock lag (~seconds) every day. Need either:

- Lag-threshold suppression (only alert if diff is older than M minutes)
- Logical-clock anchor (Spock LSN-based snapshot coordinator) instead of wall-clock

### 10.5 Operator diff fatigue

If the daily output is always non-empty, operators learn to ignore it. Need:

- Diff categorization (true divergence vs in-flight Spock lag)
- Threshold-based alerting (only page if diff > N rows OR older than M minutes)
- Snooze + acknowledge for known-transient cases

### 10.6 Authoritative-mode safety

Wrong canonical state + auto-write = data destruction. The 30-day advisory soak is necessary but not sufficient. Need:

- Dry-run preview for each authoritative write
- Block 17A.5 two-of-N approval per write
- Rollback snapshot per write

### 10.7 Schema migration windows

When a Kratos schema bump deploys, sites are briefly on different versions → schema-coherence tracker fires. Need a maintenance-window concept ("expected divergence" mode) so operators can announce migrations and the tracker enters degraded-but-not-paging state.

### 10.8 Reconciler config consistency

The reconciler itself has configuration (conflict policies, table inclusion list). That config is shared across sites. Need a bootstrap-consistency story for the config.

### 10.9 NTP discipline dependency

"Earliest `created_at` wins" depends on clocks agreeing. Already covered by [`ntp-strategy.md`](ntp-strategy.md) + ntp-health-agent — the reconciler must check NTP attestation green before trusting timestamps.

## 11. Phasing

| Phase | What lands |
| --- | --- |
| Phase 2D (now) | This doc + ADR 0823 + dry-run script (snapshots taken, diffs computed, **no alerts wired**). Validates L3 mechanics on real production state. |
| Phase 3 | Advisory mode promotion (alerts wired via Block 17A.5). Begins 30-day clean-evidence soak. |
| Phase 3+ | Authoritative-mode option becomes available — opt-in per site. |

## 12. Failure modes covered + NOT covered

**Covered:**

- Spock subscription `replicating`-but-slot-inactive (silent)
- Apply worker silently skipping events
- Network partition healed but state divergent
- Schema drift between sites (via L2b)
- Pinned-image-without-required-extension scenarios (would surface as Kratos schema validation failure on the affected site, then schema-coherence tracker would alert)
- Today's near-miss with Hydra pre-1970 nid (would have shown `kratos.networks` non-identical)

**NOT covered:**

- **Byzantine corruption** (a site lying about its state) — needs audit-log-integrity (Block 17D)
- **Schema drift WITHIN a planned migration window** — operator must announce
- **Per-row encryption key divergence** — at the reconciler level, encrypted columns look like opaque bytes; divergence in cipher itself isn't visible

## 13. Cross-references

- ADR: [`0823-daily-state-reconciliation.md`](../adr/0823-daily-state-reconciliation.md) (the decision)
- Parent: `ha-multi-site.md` (Spock as L1)
- Sibling: ADR 1010 §D5 (ACE drift-watch as L2a)
- Storage: Block 17D MinIO Object Lock (snapshot durability)
- Alerting: Block 17A.5 policy engine notification pipeline (operator alerts)
- Time discipline: [`ntp-strategy.md`](ntp-strategy.md) (timestamp comparison dependency)
- Validation: `../project/validation-state.md` (this design lands an entry once dry-run script ships)
