# Portal + id-console: what has to change before multi-site, and what does not

**Status:** Superseded 2026-10-29 by [`island-mode-multi-writer.md`](island-mode-multi-writer.md), which re-measures these stores (30 tables, ~40 SQLite-specific sites, 10 scripts opening the files) and sets the multi-writer design ([ADR 1165](../adr/1165-island-mode-multi-writer-every-site-admits.md)). Kept as the 2026-09-06 record.

**Why this doc exists.** The island-mode program moves the portal registry and the
console's onboarding store onto replicated Postgres and lets more than one site
write them. Both codebases grew from need rather than from a single design, so the
question asked before starting was whether they need a structural reset first.

**Answer: no.** The layering the multi-site work needs already exists. The changes
required are four narrow ones, listed in §6, and every one is cheaper before the
migration than after. Two genuine design gaps are named in §5 — neither is a code
defect, and one is not covered by the program plan as written.

Read with [ADR 1165](../adr/1165-island-mode-multi-writer-every-site-admits.md) (registry on Spock-Postgres) and
[`hub-spoke-admit-split.md`](hub-spoke-admit-split.md).

Measured 2026-09-06 by reading the trees at `ops/goat-zenoh-portal/` and
`ops/goat-id-console/`. Line references are to that state.

---

## 1. The store boundary is already clean

No SQL exists outside the three store packages. Every file containing a `SELECT`,
`INSERT INTO` or `UPDATE … SET` sits in `internal/registry/` (portal),
`internal/onboardstore/` or `internal/orgstore/` (console). The `internal/api/`
layer holds none, and the `*sqlx.DB` handle is opened in exactly four files, all
inside those packages.

This is the property the migration depends on and the one that would have been
expensive to retrofit. Swapping the driver and dialect touches the store packages
and nothing above them.

---

## 2. Two ID conventions, and the newer one is already correct

29 tables across the three schemas. **Six use `INTEGER PRIMARY KEY AUTOINCREMENT`;
the other 23 already use a natural or opaque text key.**

| Store | AUTOINCREMENT tables | Text-key tables |
|---|---|---|
| portal `registry` | `topics`, `audit_events`, `topic_schemas`, `topic_code_examples`, `trashgoat_specs` | `vendors` (prefix), `slots` (slot_id), `grants` (`grant-<hex>`), `govern_admins`, `machine_grants`, `envelopes`, `org_multipub_pools`, `multipub_topics`, `govern_meta`, `data_access_requests`, `probation`, `topic_budgets`, `vendor_progress`, `vendor_topic_activity`, `audit_replay_cursor` |
| console `onboardstore` | — | `onboard_proposals`, `onboard_invites`, `ssh_keys`, `operator_keys` |
| console `orgstore` | `org_parent_edges` | `orgs`, `org_aliases`, `org_match_overrides` |

The split tracks age. The ADR 1075-era tables (`grants`, `probation`,
`data_access_requests`) mint opaque string ids; the trial-era tables lean on the
SQLite rowid. So the multi-master ID work is **finishing a convention already in
progress**, not imposing a new one — and its scope is six tables, not twenty-nine.

Of those six, two probably should not replicate at all (§4), which leaves **four
tables needing an id conversion**: `topics`, `topic_schemas`, `topic_code_examples`,
`org_parent_edges`.

---

## 3. The SQLite-specific surface is small enough to enumerate

| Idiom | Count | Sites | Disposition |
|---|---|---|---|
| `LastInsertId()` | 4 | `registry.go:760`, `schemas.go:108`, `code_examples.go:85`, `trashgoat.go:123` | Disappears with the id conversion in §2 — these are exactly the AUTOINCREMENT tables |
| `PRAGMA table_info` | 2 | `registry.go:336`, `onboardstore/store.go:89` | The additive-column migration helper. Postgres has `information_schema.columns`; one function each |
| `INSERT OR IGNORE` | 1 | `envelope.go:187` | `ON CONFLICT DO NOTHING` |

Nothing else. No `datetime('now')`, no `strftime`, no SQLite date functions —
timestamps are Go-side epoch `INTEGER`s throughout, which port unchanged and, more
usefully, keep time handling in application code where a partitioned site's clock
discipline can be reasoned about.

---

## 4. Two tables that should stay local rather than replicate

`audit_events` (portal) is append-only operator audit with a rowid key. It is the
same class as `netbird_events`, which `ha-multi-site.md:656` already lists as
per-site and not replicated. Replicating it buys nothing and forces an id
conversion on the highest-write-rate table in the store.

`trashgoat_specs` is chaos-experiment definitions. An experiment is run against a
fabric from somewhere; there is no evident reason a second site needs the first
site's specs, and every reason a partitioned site should be able to write its own.

Both are decisions for the ADR, not conclusions of this review.

---

## 5. Two design gaps

### 5.1 Natural keys collide, and per-site id prefixes do not help

`vendors.prefix` is the primary key and, by design, *is* the identity — "example-vendor",
"org-2". `orgs.id` is the same shape, a kebab slug that is deliberately immutable.
`machine_grants.custodian` and `envelopes.anchor` key on handles.

Per-site id prefixes solve concurrent inserts into a *surrogate* key space. They do
nothing for a natural key: two partitioned sites can each register the prefix
`example-vendor`, both correctly, and last-write-wins will silently pick one and discard
the other's rows by cascade.

The program plan does not currently cover this. Registration of a new vendor, org or
custodian is an **admit**, so the mechanism is the same capability envelope used for
identity mints — either the envelope bounds who may create these at a partitioned
site, or the namespace is partitioned in advance. It is not a merge problem and it
cannot be fixed at the id layer.

### 5.2 Three database files hold one logical graph

The portal registry, the console's `onboardstore` and the console's `orgstore` are
three separate SQLite files in two processes. `govern_admins.org` and
`org_multipub_pools.org` reference org ids owned by `orgstore`, across that boundary,
with no foreign key and no transaction spanning them.

Consolidating onto one Postgres would make those references enforceable. But the
`orgstore` schema header states the separation is deliberate — *"its own DB file
beside the onboarding store — separate file so the rollback lever is a file-level
concern, never a table surgery."* Collapsing the files removes a safety property
someone chose on purpose.

The recommendation is to keep them as separate Postgres **databases** rather than
separate schemas in one, preserving a per-store rollback unit while gaining a real
replication mechanism. The cross-store references stay conventions, as they are
today, and the reconciliation surface in workstream C3 is where a dangling reference
would surface.

---

## 6. What to change, and when

All four are small, and all are cheaper before the migration.

| # | Change | Why before |
|---|---|---|
| 1 | Convert `topics`, `topic_schemas`, `topic_code_examples`, `org_parent_edges` to opaque text ids matching the `grants` convention | The conversion is a data migration. Doing it on one site's data, once, is a different exercise from doing it across replicating sites |
| 2 | Decide `audit_events` and `trashgoat_specs` local-only, and record it in the ADR alongside the `netbird_events` precedent | Avoids an id conversion on the busiest table and a repset membership nobody wants |
| 3 | Replace the three SQLite idioms in §3 with portable equivalents behind the existing store functions | Removes the last dialect coupling while there is still one writer to test against |
| 4 | Extend the capability envelope to cover natural-key registration (§5.1) | It is a gap in the plan, not a step in it — it needs deciding before any site can register locally |

Nothing here is a refactor in the structural sense. The boundary that would have
made this expensive — business logic reaching into SQL — does not exist.

---

## 7. What this review did not cover

- **Bytes on disk.** `topic_schemas.file_path` (under `SchemaDir`),
  `vendors.bundle_path` and `slots.bundle_path` point at files, not rows.
  Replicating Postgres does not replicate them. The existing pattern for this is
  MinIO async replication ([ADR 0216](../adr/0216-goat-net-federation-and-site-planes.md));
  applying it is separate work and is not sized here.
- **Query performance under Postgres.** Every read in these stores is small and
  indexed today, but no plan was compared.
- **The API layer's own assumptions.** This review traced the data layer. Handler-level
  assumptions about a single portal URL or a single console origin were not audited.

---

## Related

- [`hub-spoke-admit-split.md`](hub-spoke-admit-split.md) — the admit/revoke taxonomy §5.1 depends on
- `ha-multi-site.md` §6 — the DDL coordination these stores inherit
- [`portal-architecture-review.md`](portal-architecture-review.md) — the prior portal review, component-level
- `org-hierarchy-and-dedup.md` §3 — why `orgstore` is its own file
