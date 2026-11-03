# ADR 0004 — Repo-scope tagging for monorepo separation

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | Multi |
| Supersedes | — |
| Superseded by | — |
| Related | [`0001-adr-template.md`](0001-adr-template.md), [`1005-monorepo-separation-map.md`](1005-monorepo-separation-map.md) |

## TL;DR (for a layperson)

The project will eventually split out of its monorepo into several
smaller repositories. We tag every ADR with which future repository it
belongs to, so the split becomes a directory move rather than a
rewrite. ADRs were designed to survive this split from day one.

## Context

goat is currently a monorepo containing the mesh management
plane, infrastructure code, the Zenoh substrate (data fabric pilot
graduating to universal peer-interaction substrate), the egress proxy
and policy engine, and several auxiliary services. A monorepo is the
right shape for the current pace of cross-cutting change. Some
components may eventually warrant their own repos; the planned split
lines are tracked in [`1005`](1005-monorepo-separation-map.md):

- **goat core** — mesh management plane, mgmt-host stack,
  infrastructure automation, relay + bridge VPS configuration, **Zenoh
  substrate** (router deployment, namespace conventions, in-tree
  reference services), policy engine until/unless it splits.
- **goat-bond** — WAN-link bonding (already a separate repo).
- **goatPolicy** — egress proxy, microsegmentation, per-site
  policy engine. Splits when partner federations actually exist.

Compliance bundles are a *curated snapshot subset* of the core ADR
series, not a separate repo. Payload services that ride on the Zenoh
substrate (mission flows, fusion, sensor integrations, vendor shims)
default to living **out-of-tree** wherever their authors live and join
the fabric using standard upstream Zenoh mechanisms — they don't get
scope tags here. See [`1005`](1005-monorepo-separation-map.md) for why
Zenoh is not a planned split.

If ADRs were organized by recency or by an arbitrary numbering scheme,
a split would force us to either (a) leave ADRs behind in the wrong
repo or (b) renumber and rewrite. Both options destroy the supersession
chain. The fix is to tag scope at authoring time and treat the tag as
load-bearing for future moves.

## Decision

Every ADR header table has a **Repo scope** field with one of:

- **`goat core`** — moves with the mesh-mgmt-plane repo.
  This is the default for anything tied to mesh identity, mgmt-host,
  Zenoh substrate, or in-tree reference services.
- **`goat-bond`** — moves with the WAN-bonding repo.
- **`goatPolicy`** — moves with the policy-engine repo when
  it splits.
- **`Multi`** — applies to multiple repos. Stays in core; sub-repos
  reference it by URL.

Rules:

1. **Numbers are global across the series.** A split moves an ADR with
   its number intact; the new repo may have gaps and that is fine.
2. **`Multi` is the default for cross-cutting strategy ADRs** (the
   0000-series and the 0100-series). They always live in core, and
   sub-repos reference them.
3. **The Compliance bundle is a curated subset, not a fork.** When
   we generate a compliance submission, we copy in the relevant
   ADRs by reference (snapshot for the submission timestamp). The
   originals stay in the engineering repo.
4. **Repo scope can be revised.** If we move an ADR's home, we update
   the field and note the move in the supersession chain (the move
   is a new ADR that supersedes the old one).

## Alternatives considered

- **Per-repo ADR series from the start** — no, premature. The
  cross-cutting strategy ADRs need to live somewhere stable while
  the split lines settle.
- **Wait until the split, then assign scope** — guarantees a painful
  triage exercise at the worst possible moment.
- **Filesystem subdirectories per scope** — couples the scope to a
  directory layout that will need to change at split time. Tags are
  cheaper to update than directory moves with `git log --follow`
  consequences.

## Consequences

**Positive.** A monorepo split is a `git mv` exercise plus an index
update, not a rewrite. ADR numbers stay stable, supersession chains
stay walkable.

**Negative.** Authors must think about scope at write time. We
mitigate by allowing `Multi` as the safe default for anything
cross-cutting.

## Assessor view (SCA-R)

The scope tag is the assessor's primary tool for understanding
boundary-of-evaluation. NIST SP 800-37 system boundary definitions map directly
onto repo scope. An assessor evaluating the core repo only needs
to read ADRs tagged `goat core` and `Multi`.

## Trigger conditions for revisit

- A monorepo split is executed. Audit every ADR's scope tag in the
  same PR.
- A new sub-repo emerges that is not in the table above. Add it to
  the enumeration; do not invent ad-hoc tags.

---

*See `GLOSSARY.md` for terms and acronyms.*
