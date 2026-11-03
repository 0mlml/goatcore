# ADR 1005 — Monorepo separation map and trigger conditions

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | Multi |
| Related | [`0004-repo-scope-tagging.md`](0004-repo-scope-tagging.md), [`0212-goat-vs-bond-split.md`](0212-goat-vs-bond-split.md), [`1004-account-migration-discipline.md`](1004-account-migration-discipline.md) |

## TL;DR (for a layperson)

goat is currently a monorepo and will mostly stay
that way. The Bond repo has already split (different stack,
different failure model). The policy engine may split later
once partner federations exist. Compliance bundles are a
snapshot pattern, not a separate repo. The Zenoh data fabric
**stays in core** — it's the substrate that mesh peers
interact through, not a separable subsystem; payload services
that ride on it can live wherever their authors live and
join the fabric using standard upstream Zenoh mechanisms.
The repo-scope tagging on every ADR
([`0004`](0004-repo-scope-tagging.md)) keeps any future
split cheap.

## Context

A monorepo is the right shape for the current pace of
cross-cutting change and the current contributor count
(~1 active author). As components stabilize and ownership
patterns settle, splits become productive — they reduce
coupling, allow independent release cadence, and let
authorization scopes be drawn per-repo.

The risk is that splits become traumatic events that
re-derive everything. The fix is to plan them in advance
and only execute when the trigger conditions actually fire.

The other risk — over-anticipating splits — is equally
real. A split entry in this table for a subsystem that turns
out to be glued to core (Zenoh, after the 2026-04-24 scope
expansion to universal peer-interaction substrate) creates
the wrong long-term mental model and invites premature
multi-repo coordination overhead. This ADR errs on the side
of *fewer* planned splits, with the bar for adding a new one
being a concrete trigger condition, not a feeling.

## Decision

Planned split lines (matching the repo-scope tags in
[`0004`](0004-repo-scope-tagging.md)):

| Sub-repo | Contents | Trigger condition |
|---|---|---|
| **goat core** (this repo) | Mesh management plane, mgmt-host stack, infrastructure automation, relay + bridge VPS configuration, **Zenoh substrate** (router deployment, namespace conventions, in-tree reference services), policy engine until/unless it splits. | Stays as parent; never splits. |
| **goat-bond** | WAN-link bonding. | Already split per [`0212`](0212-goat-vs-bond-split.md). |
| **goatPolicy** | Egress proxy, microsegmentation, per-site policy engine. | Block 17 + 18A ship to production AND policy engine has ≥2 partner federations using it. |

**No `goatFabric` split is planned.** The original
plan in this table treated Zenoh as a separable subsystem.
After the 2026-04-24 scope expansion documented in
`zenoh-data-fabric.md`
§"Phase 3+ scope expansion", Zenoh is the substrate the mesh
stack assumes — same hardware, same Ansible role tree, same
failure domain, same identity layer. Splitting it would be
analogous to splitting WireGuard out of the mesh repo:
technically possible, structurally incoherent. Phase 2 gate
passing is a *promotion-to-core* trigger, not a split
trigger.

Payload that rides on Zenoh (mission flows, fusion, sensor
integrations, vendor shims) is **out-of-tree by default** —
those services live wherever their authors / partners /
vendors live, and join the fabric by speaking standard
upstream Zenoh and following the namespace conventions in
`zenoh-data-fabric.md`.
Core stays compact because most of what would have grown it
never enters it.

**No `goatCompliance` split is planned.** ADR
[`0004`](0004-repo-scope-tagging.md) already specifies that
compliance bundles are a *curated snapshot subset* of the
core ADR series (snapshotted at submission timestamp), not a
fork. The scope-tag mechanism handles this without a repo
move. If a specific authorization vehicle ever requires an
independently-versioned compliance repo, revisit this ADR
then.

Migration mechanics (when a future split fires):

1. **Pre-split audit**: every ADR's `Repo scope` tag is
   verified against the new split.
2. **Move with `git mv`**: ADRs and their referenced design
   docs move together; numbering preserved.
3. **Multi-tagged ADRs stay in core**: sub-repos reference
   them by URL.
4. **Cross-repo CLAUDE.md updates**: each repo's CLAUDE.md
   gets pointers to the others.
5. **Compliance snapshot is a copy-in-by-reference**: when
   submitting, snapshot the relevant ADR set at the
   submission timestamp; originals stay in source repos.

Each split is *one ADR* in the new sub-repo's ADR series
documenting the split event.

## Alternatives considered

- **Never split anything.** Monorepo grows unwieldy as it
  expands. Held; we re-evaluate at each trigger condition.
- **Split now.** Premature; cross-cutting change is still
  high and there is one active author.
- **Per-component repos.** Too granular at our scale.
- **Keep `goatFabric` as a planned split.** Considered
  and rejected after the Zenoh scope expansion. Zenoh is
  substrate, not a separable subsystem; the trigger conditions
  (Phase 2 gate + ≥3 services in production) actually describe
  *promotion to load-bearing core component*, not separation.
  Trying to split substrate from the system that depends on it
  would create coordination overhead with no offsetting gain.
- **Pre-allocate per-vendor shim repos.** Considered and
  rejected. Shims default to living wherever the vendor
  relationship lives (out-of-tree). Pre-allocating repo
  slots for shims that don't yet exist would be the same
  category of mistake as the Fabric pre-allocation.

## Consequences

**Positive.** Splits are predictable, planned, and cheap.
Authorization scopes can be per-repo when that fits.
Independent release cadence per sub-repo when desired.

**Negative.** Cross-repo coordination during the transition.
Acceptable; we plan the split with a defined hand-off
window.

## Assessor view (SCA-R)

The split map serves CM-2 (baseline configuration — clearly
defined boundaries) and PM-9 (risk management strategy —
each sub-repo's authorization can be scoped). The repo-scope
tagging is itself an SR-x supply chain control: a sub-repo's
authorization narrative cites only the ADRs tagged for that
repo.

## Trigger conditions for revisit

- A trigger condition fires. Execute the split following the
  migration mechanics above; produce the split-event ADR.
- A planned split is no longer the right shape. Update this
  ADR; do not silently abandon the plan.

---

*See `GLOSSARY.md` for terms and acronyms.*
