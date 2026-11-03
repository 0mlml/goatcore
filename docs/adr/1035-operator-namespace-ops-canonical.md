# ADR 1035 — `ops/` is the single canonical reserved operator/control namespace; deprecate the trial fabric's `system/`

**Status:** Accepted (2026-06-08)
**Repo scope:** goat core (nova + nova-lan Zenoh roles, trashgoat, goat-zenoh-portal, panoscope)
**Related:** [ADR 0844](0844-zenoh-deployment-topology-and-acl-v1.md) (reserves `ops/` for production — the standard this reaffirms) · F-183 (the divergence this fixes) · [`zenoh-trial-fabric.md`](../design/zenoh-trial-fabric.md) (where `system/` was reserved) · [`trashgoat.md`](https://github.com/usareur-af/goatstuff/blob/main/docs/design/trashgoat.md) (the sole live `system/` occupant) · [ADR 1034](1034-fabric-health-surface.md) (already publishes operator surfaces under `ops/`)

## Plain-language summary

The fabric reserves a few top-level namespaces for operator/control traffic — the stuff the platform itself emits, not vendor mission data. Production already standardized that namespace as `ops/` (ADR 0844). But the nova trial fabric separately invented `system/` for the same purpose, with a one-line spec and only one service ever using it (trashgoat's control plane). Meanwhile newer nova services (fabric-health, the load generator) correctly used `ops/`. So we had two operator namespaces and a name — `system` — that means nothing because everything on a fabric is "the system." This decision makes `ops/` the one canonical operator namespace everywhere, gives it a governed sub-structure so it can't be casually sprayed again, and deprecates `system/` with a safe staged migration.

## Context

A reserved top-level namespace is a high-leverage governance object: it carves the keyspace, drives default-deny ACL rules, and shapes how every inspection surface clusters traffic. ADR 0844 treated it that way for production — `ops/` = "operational events emitted by any service running on the fabric," role-shaped.

The trial fabric did not. Per F-183: `zenoh-trial-fabric.md` reserved `system/` "for operator" in a single line with two never-used example topics; the `nova-zenoh` / `nova-lan-zenoh` router ACLs grant operator write on `system/**`; trashgoat planted `system/trashgoat/control/**` there and became its only occupant; and the newer fabric-health + loadgen surfaces used `ops/` per 0844. The panoscope infrastructure-participant-class work surfaced the result — trashgoat's control plane rendering as an isolated, meaningless "system" group.

This is not a vendor-namespace question (the trial fabric's deliberate `<vendor>/<system>/…` vendor-scoped experiment is out of scope and unchanged — note that `<vendor>/system/…` as a *vendor's own* second segment is a different thing and is untouched). It is specifically about the **top-level operator/control prefix**.

## Decision

1. **`ops/` is the single canonical reserved operator/control namespace, fleet-wide**, reaffirming ADR 0844. No other top-level prefix is reserved for operator traffic.

2. **Governed sub-structure** (so "operator namespace" is no longer a one-line blank cheque):
   - `ops/<service>/control/**` — per-service lifecycle/control commands the operator issues to a fabric service (e.g. `ops/trashgoat/control/<id>/v1`).
   - `ops/fabric/**` — fabric-wide health + footprint (already live: `ops/fabric/health/<site>`, `ops/fabric/footprint/<peer>`). Added 2026-07-30 (zenoh-cardinality-budget-and-lazy-crawler.md §4): `ops/fabric/cardinality/<site>` (the single lazy crawler's budget-sensor rollup, `FabricCardinalityV1`) + `ops/fabric/census/<site>` (its byte-faithful adminspace census mirror, JSON — the poll-once/fan-out replacement for per-tool adminspace walks).
   - `ops/announcements` — operator broadcasts (notices, sunset banners); everyone may read.
   - `ops/audit-stream` — operator/audit emission stream.
   - `ops/loadgen/**` — load-test traffic (already live).
   - Write under `ops/**` is operator-identity-only; read is broad (operators publish *to* participants). New sub-prefixes are added deliberately, documented here and in `zenoh-trial-fabric.md`.

3. **`system/` is deprecated.** No new traffic may use it. The router ACL retains a transitional `system/**` operator-write allow until the live migration completes, then it is removed.

4. **panoscope classifies `ops/`-only (and, transitionally, `system/`-only) publishers as infrastructure** (the `operator-system` subkind). The reserved-prefix set used by `PeerInfo::node_class()` gains `ops/`. A bonus the namespace consolidation buys: all operator infra (`ops/fabric` health, `ops/<service>/control`, `ops/announcements`) now clusters under one coherent `ops` prefix instead of scattering, so the inspection surface reads as a single operator plane.

5. **Migration is staged for easy rollback** (ADR 0859), publisher+subscriber being a coordinated change on a running fabric:
   - **(a)** Router ACL grants `ops/**` operator-write alongside `system/**` (additive, no break). Code/config/doc rename + the panoscope classifier land in this change-set.
   - **(b)** Operator redeploys the router (ACL is restart-required per F-156), then flips trashgoat's control key + the portal's publisher to `ops/trashgoat/control/**` together and redeploys both; verify control round-trips on `ops/`.
   - **(c)** Once nothing publishes `system/**`, remove the transitional grant (set the instance's `*_legacy_system_namespace` back to `false`).

6. **The transitional `system/**` grant is opt-in per instance, default off** (added in the follow-on after the live-migration constraints below surfaced). New deployments (ZAGROS + future) render clean `ops/`-only; only an instance with legacy `system/` traffic in flight sets `nova_zenoh_legacy_system_namespace` / `nova_lan_zenoh_legacy_system_namespace: true` in its group_vars. This makes "going forward" structurally clean rather than relying on each new deployment to remember to strip `system/`.

### nova-a live migration is DEFERRED — clobber-safe path is a prerequisite

The nova-a router restart (step b) is **not** a plain role redeploy. nova-a's live ACL is **hybrid**: the ansible seed set **plus the portal-added vendor rules** appended at runtime (the portal mints per-vendor ACL on onboarding). A full role re-render templates only the seed set, so it would **clobber every portal-added vendor's write grant** (CLAUDE.md `Running Ansible` footgun; memory `project_zenoh_acl_portal_vs_seed_clobber`). Therefore step (b) on nova-a is blocked until a **clobber-safe ACL-update path** exists (surgical in-place insert of the `ops/**` storage + `operator-w-ops` grant while preserving the portal vendor rules, or a portal-emitted full-ACL regeneration). Until then nova-a keeps running trashgoat on `system/` via what is already deployed (the transitional grant is real on the live config); there is no urgency because nothing breaks. A secondary prerequisite is resolving the **role-path ambiguity** for nova-a: `nova-zenoh-redeploy.yml` uses the generic `zenoh` role (already `ops/`-only) while nova-a's live config is the `nova-zenoh` shape — the authoritative deploy path must be confirmed before any re-render. New/greenfield instances have neither problem (no portal-vendor backlog, clean role selection), so they migrate for free via the default-off gate.

## Consequences

- One operator namespace, one name that states its role, a sub-structure that must be edited deliberately — the casual-spray failure mode is closed.
- The trial fabric stops silently violating ADR 0844.
- Short-lived dual-grant window (`ops/**` + `system/**`) is the cost of a no-outage migration on a live fabric; tracked to removal in step (c).
- trashgoat's control plane briefly exists in code as `ops/` before the live flip; the transitional ACL keeps `system/` working until then so there is no flag-day.

## Alternatives considered

- **Keep `system/`, just document it better.** Rejected: it still duplicates `ops/` from 0844 and keeps a meaningless name; documenting a divergence is not reconciling it.
- **Amend ADR 0844 in place instead of a new ADR.** Rejected: 0844 is the *production* topology/ACL record and already says the right thing; the work here is the trial-fabric reconciliation + governance + migration, which deserves its own decision rather than mutating the production ADR's history.
- **Hard-cut `system/` → `ops/` (no transitional dual-grant).** Rejected: a flag-day on a live fabric where publisher and subscriber deploy separately; the dual-grant removes the outage window for one extra cleanup step.
