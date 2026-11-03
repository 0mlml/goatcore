# `PUBSUB-ORG-ADMIN` — multi-publisher topics (co-publish)

| Field | Value |
|---|---|
| Status | Design canon for [ADR 1105](../adr/1105-pubsub-org-admin-co-publish-write-grants.md). Registry + grant gate + ACL render + resolver + CLI built + merged; **ZAGROS-LIVE-FIRED 2026-08-01 (four-point denial-by-absence PASS on the live router)**; default-off in steady state. |
| Scope | The *write* half of org-owned data sharing: letting an org's data-governance admin **deliberately create a marked, discoverable, org-scoped multi-publisher topic** and add several of its own publishers to it — **without ever touching a single-owner namespace root**. The read/encryption half is [ADR 1102](../adr/1102-organization-owned-data-sharing.md). |
| Related | [data-access-roles.md](data-access-roles.md) (the grammar + grant store) · grant-keyed-encryption.md §3a (the `data.govern.org` authority) · [slot-alias-display-layer.md](slot-alias-display-layer.md) (ADR 1039 — how the org name reaches a human without entering the key) · partner-read-partition.md (the B2.1 additive-lane precedent this mirrors on the write side) · [operator-action-audit.md](operator-action-audit.md) |

## 1. The invariant this protects (read first)

**One writer per namespace root is a feature, and it is inviolable.** The router grants each identity write on exactly its own `<anchor>/**` and nothing else (ADR 1025). A subscriber on `<anchor>/**` therefore knows, for free and with no per-message marking, that every sample there came from that one identity. That provenance guarantee is the thing this design refuses to spend.

So `PUBSUB-ORG-ADMIN` does **not** add a second writer to an existing single-owner root. That power does not exist. What it does instead: let an org **deliberately create a distinct topic that is born multi-writer, is obviously not anyone's private root, and is discoverable as such** — and add several of the org's own publishers to *that*. "More than one writer" only ever happens on a topic that announces itself as multi-writer.

> An earlier draft of this design granted a write onto an arbitrary anchor/topic. That was rejected (2026-08-01): it silently muddied an existing root's provenance. This version is the correction.

## 2. The `multipub/` class in one figure

```
  ORG O's data-governance admin  (holds data.govern.org:O — ADR 1102; the PUBSUB-ORG-ADMIN capability)
        │
        │  (1) CREATE a multi-publisher topic — a deliberate, justified, marked, discoverable act
        ▼
   multipub_topics row { topic_id, org: O, pool_handle: <O's stable opaque handle>,
                       display_name: "<human alias>", persist, status }
        │   key-expr:  multipub/<pool_handle>/<topic_id>/**   (+ volatile/ + /@adv/ mirrors)
        │   the reserved `multipub/` segment = "do NOT assume one writer here" (structural, immutable)
        │   <pool_handle> resolves to org O (registry) → "Acme" (alias, ADR 1039) — the NAME is never in the key
        │
        │  (2) ADD a publisher — a write grant whose TARGET is this multi-publisher topic (never an anchor root)
        ▼
   grant row { action: write, grantee: principal:X, producer: multipub/<pool>/<topic>, granted_by, ... }
        │            (admissible ONLY if the multi-publisher topic ∈ O  AND  X ∈ O — at-most-own-org, fail-closed)
        │            (GUARDRAIL: a target that is not a registered multi-publisher topic is REFUSED)
        │
        ├──▶ resolver: X's derived roles carry a CO-PUBLISH membership of multipub/<pool>/<topic>
        │              (NOT data.write.producer — a multi-publisher topic is not a producer) → COP + goat who
        │
        └──▶ ACL render (config.json5.j2): additive copub-w-<X>-<n> allow on
             multipub/<pool>/<topic>/** (+ volatile + @adv), bound in an EXTRA policy on slot-<X>
             — never edits X's own slot-w-<X> rule, never touches ANY <uuid>/** root.
             Plus a rocksdb store for multipub/<pool>/<topic>/** so it persists like any namespace.
```

Default with nothing created: `zenoh_copublish_enabled: false`, `zenoh_copublish_grants: {}`, `zenoh_multipub_topics: []` → the template renders byte-identically to today. Deploying the capability changes nothing until an admin creates a multi-publisher topic and grants a publisher.

## 3. How org ownership is discernible without putting the org name in the key

The org **name** cannot go in the key. A key-expr is the write-ACL match, the storage key, and every subscriber's subscription — freeze-critical — while org ownership is *mutable* (reorgs, transfers). Encoding the name would make every reorg a fleet event (ACL re-cut + storage migration + broken subscriptions) and cannot even be parsed reliably (`acme-systems` vs `acmesystems`). This is the standing invariant (data-access-roles.md §1, ADR 1043 affiliation-never-in-the-anchor, ADR 1051 born-as vs currently-is).

So the org is made discernible three ways, none of which freeze a mutable name into a key:

1. **A stable opaque `<org-pool>` handle** minted once per org (`org_multipub_pools`). It is a *born-under* marker: every one of an org's multi-publisher topics clusters under its handle, so even a raw-key reader sees that two `multipub/<same-handle>/…` topics belong to the same org. If a multi-publisher topic ever moves orgs, the registry re-points ownership and the key never changes.
2. **Registry resolution** — the handle → org join every discovery surface performs.
3. **Alias display** (ADR 1039) — the handle renders as "Acme" and the topic-id as its human name, on the surfaces, always current, never in a machine path.

Net on discovery: from the key alone → "multipub class; these share an org-pool"; from the one resolve every surface does → "MULTI-PUBLISHER topic, owned by Acme, writers X/Y/Z."

## 4. At-most-own-org, fail-closed, and the mechanistic guardrail

A co-publish grant is admissible **only if**:

1. the **target is a registered multi-publisher topic** (`multipub/`-class, a `multipub_topics` row) — a target that is an ordinary anchor root, or an unregistered key, is **refused** (the guardrail — the admin literally cannot open a single-owner root); **and**
2. the multi-publisher topic's owning org O and the publisher X **both** resolve to an org the caller holds `data.govern.org:<O>` for (`mayCoPublishGrant`, mirroring ADR 1102 `mayGovernProducer` + sponsor at-most-own-tier ADR 1071).

Creating the multi-publisher topic itself is likewise gated to `data.govern.org:<O>` for the naming org, justification-recorded. Cross-org co-publish is refused — a platform decision, never an org one. Fail-closed on an unresolvable org/roster.

## 5. Enforcement: the additive ACL allow + multipub-topic storage

**File:** `ops/ansible/roles/goat-zenoh/templates/config.json5.j2`, the `per-uuid-slot-pool` branch.

**Render inputs, both default-off / empty, set per-goatnet in `group_vars/zagros_hosts/data-access.yml`:**
- `zenoh_copublish_grants` — `{ <publisher-anchor>: [ "multipub/<pool>/<topic>", … ] }`. Each entry renders one additive `copub-w-<publisher>-<n>` allow (`put/delete/liveliness_token`, ingress) on `multipub/<pool>/<topic>/**` + `volatile/multipub/<pool>/<topic>/**` + `multipub/<pool>/<topic>/**/@adv/**`, bound in an extra policy on the publisher's existing `slot-<publisher>` subject. Purely additive; it never rewrites the publisher's own `slot-w` rule and never references any `<uuid>/**` root.
- `zenoh_multipub_topics` — `[ "multipub/<pool>/<topic>", … ]`. Each renders a rocksdb store so the multi-publisher topic persists like any producer namespace (a multi-publisher topic can exist, and be stored, before it has any publisher).

Materialize both from the portal's authoritative registry: `goat access co-publish export` (`GET /api/access/co-publish-grants`) yields the grant map, and the multipub-topic list yields the store list. The registry stays authoritative; the operator captures the snapshot into group_vars at deploy time (v1 seam; auto-export is the named next step, mirroring the topic-budget crawler's resolver-fetch).

**Hard constraints:** F-156 (no live reload — takes effect on router restart; ledger the render), F-177 (ungranted keys denied **silently** — verify a grant by presence at a subscriber and an ungranted key by absence, never an error), F-258 (ZAGROS-only; nova-a's hybrid ACL is a separate migration). CI: `render-check.py` asserts a copub target is always a `multipub/` key, that a single-owner anchor is **never** a copub target, and that the default (no multi-publisher topics) render stays byte-identical.

## 6. The registry objects (currently-is, never a cert stamp)

- `org_multipub_pools` — `{pool_handle (PK, stable opaque), org, created_by, created_at}`. `EnsureOrgMultipubPool(org)` mints once and returns the stable handle; the org field is currently-is (a reorg updates the row, the handle never changes).
- `multipub_topics` — `{topic_id (PK, stable opaque), org, pool_handle, display_name, persist, justification, created_by, created_at, status}`. Deprecate-never-delete. The key-base is `multipub/<pool_handle>/<topic_id>`.
- The publisher set = write grant rows (§4) — no parallel store.

None of this touches a cert URI-SAN (ADR 1051/1043; guarded by `check-portal-cert-identity-override.sh`). The write anchor stays per-UUID; a multi-publisher topic is a separate namespace, and co-publishing to it is an additive allow.

## 7. Surfaces

| Surface | What it does |
|---|---|
| **Grammar / SoT** (`data-access-roles.yaml`) | `grants` gains `action`; a doc note names `PUBSUB-ORG-ADMIN` = the write facet of `data.govern.org`, and states co-publish targets a `multipub/`-class topic (membership), not a producer write role. Grammar regex unchanged. |
| **Registry** (`schema.sql`, `multipub_topics.go`, `grants.go`) | the two new tables + CRUD; write-grant rows target a multipub-topic key-base. |
| **Multi-publisher topic API** (`data_access.go`, `server.go`) | `POST/GET /api/access/multipub-topics` (create=deliberate, list=discovery). |
| **Grant gate** (`data_access.go`) | `mayCoPublishGrant` — target must be a registered multi-publisher topic owned by an org the caller governs; publisher in that org; anchor targets refused. |
| **Resolver** (`dataroles.go`) | `MergeGrants` folds a write grant into a `CoPublish` membership list + a note; the COP invariant note amended. |
| **ACL render** (`config.json5.j2`, defaults, group_vars, `render-check.py`) | the additive `copub-w-*` allow on the `multipub/` key + per-multipub-topic storage, default-off, guardrail render-checks. |
| **CLI** (`crates/goat-cli/src/cmd/access.rs`) | `goat access co-publish create-topic \| list-topics \| grant \| revoke \| export`; `goat who` surfaces a principal's co-publish memberships via the resolver. |
| **Audit** (`crates/goat-cli/src/cmd/audit.rs`) | `pubsub.multipub-topic.create` / `pubsub.write-scope.set` / `.revoke` `OperatorActionV1`. |
| **Discoverability** | portal catalog + `list-topics` + `goat access topic` render `MULTI-PUBLISHER` + owning org (handle→name) + publisher set. Panoscope badge is a sibling-surface follow-on. |
| **Best-practices** (external, required) | `hello-galaxy-zenoh-patterns` `docs/co-publish.md` — when to create a multi-publisher topic, when not, the attribution trade-off. |

## 8. Built vs. next (honest map)

**Built this track (code, CI-guarded):** the `org_multipub_pools` + `multipub_topics` registry + CRUD; the multipub-topic create/list endpoints; the reworked at-most-own-org grant gate (target must be a registered multi-publisher topic); the resolver co-publish-membership surfacing + COP note; the additive `multipub/`-targeted ACL render + per-multipub-topic storage + `render-check.py` guardrail checks; `goat access co-publish` with F-207 audit; the SoT/vocabulary rows; the best-practices doc.

**Next (deploy/operator-gated, not code):**
1. **ZAGROS live-fire — DONE (2026-08-01, four-point PASS).** Portal bumped to sha-3ccf51b (#1987); the live portal verified create/discover/guardrail (an org-less writer is refused `403 cross_org`). A transient router render+restart with a real grant then proved the four-point denial-by-absence on the live router: a granted co-publisher's sample **lands** on the multi-publisher topic; an ungranted writer is silently **denied**; a co-publisher **cannot** write another peer's single-owner root (the inviolability invariant); own-anchor publishing is intact (control). Reverted to default-off after (the F-258 anti-clobber guard required `-e goat_zenoh_allow_acl_rerender=true` on the revert; router healthy across both restarts). A real co-publish now just needs an org-placed publisher granted through the portal + the same render/restart.
2. **Portal → group_var auto-materialization** — v1 renders from the hand-set group_vars; the export step that projects in-effect grants + multi-publisher topics into them is the named seam.
3. **Panoscope `MULTI-PUBLISHER` badge** — the portal catalog + CLI carry the marker now; the graphical inspection surface renders it as a follow-on.

## 9. B2.1 interaction

Co-publish is write-side, strictly at-most-own-org, on the `multipub/` class; the B2.1 partner read partition + release lanes are read-side and cross-org (`release/`). Disjoint namespaces, disjoint rule sets (`copub-w-*` vs `partner-r-*`/`partner-deliver-*`), disjoint group_vars. No conflict; both are default-off additive blocks atop the same deny floor.
