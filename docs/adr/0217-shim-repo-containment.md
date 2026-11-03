# ADR 0217 — Shim repo containment: per-class split, substrate in-tree

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-05-01 |
| Repo scope | Multi |
| Supersedes | — |
| Superseded by | — |
| Related | [`0209-shim-pattern.md`](0209-shim-pattern.md), [`1005-monorepo-separation-map.md`](1005-monorepo-separation-map.md), [`0111-vendor-friendly-license-posture.md`](0111-vendor-friendly-license-posture.md), `../design/shim-pattern.md` §11 |

## TL;DR (for a layperson)

We will integrate with several outside data systems — open-standard
formats (CoT, ISA, SAPIENT, OMNI) and commercial vendor SDKs. Each integration is a small adapter
called a "shim." This decision says where each shim's source code
lives. Open standards go into one shared repo. Each commercial vendor
gets its own dedicated repo. Shared boring scaffolding lives in the
goat trunk and is published as a library every shim repo depends on.
Putting them in one giant mega-repo would entangle licensing,
deprecation, and permissions in ways we don't want.

## Context

ADR [`0209`](0209-shim-pattern.md) established the shim pattern. ADR
[`1005`](1005-monorepo-separation-map.md) said shims default
out-of-tree. Neither resolved the question that arises once the
ecosystem grows past one shim: **how should several shim repos be
organized relative to each other?**

As of 2026-05-01 we have a planned mirror of one commercial vendor's
platform, a planned open-standards bundle (CoT, ISA, SAPIENT, OMNI),
and several vendor SDKs on the watchlist (including TAK-server
specifics beyond CoT). The question forced itself: one mega-repo, one
repo per shim, or something in between?

Three options were weighed:

1. **One mega-repo `goat-shims`.** Single dependency, shared CI,
   central docs, one versioning story. Cons: license entanglement
   across heterogeneous deps; deprecation of one vendor entangled with
   all others; permissions hard to scope per-vendor; one bad merge
   affects every shim.
2. **One repo per shim, full split.** Maximum severability. Cons:
   genuine duplication for open standards (which share schema/audit
   shape and synchronized release cadences); N CI configs; no obvious
   place for cross-shim conventions to live as code.
3. **Per-class split with substrate in-tree.** Open standards
   consolidate; commercial vendor SDKs each get their own repo; the
   shared scaffolding (auth helpers, schema-decl, audit hooks, WASM
   sandbox plumbing, namespace policer) is a library inside the goat
   trunk that every shim depends on.

## Decision

Adopt **option 3: per-class repo split with substrate in-tree.**

Containment classes:

| Class | Repo home | Examples |
|---|---|---|
| Open data standards | Single repo `goat-maintainer/goat-open-standards` | CoT, ISA, SAPIENT, OMNI |
| Commercial vendor SDK | One repo per vendor (`goat-maintainer/goat-<vendor>-shim`) | `<vendor-a>`, `<vendor-b>`, TAK-product |
| Shared substrate | In-tree at `goat-shim-framework/` (this repo); published as Rust crate + Go module | Identity, schema-decl, audit, WASM sandbox plumbing, namespace policer, restart scaffolding |

The existing in-tree TAK-to-Zenoh shim at `ops/shims/tak/` (Block 20D)
stays in-tree as the reference implementation — see
`../design/shim-pattern.md` §11.3.

A new shim repo registers in
`../integrations/INDEX.md` in the same
change that creates it. The integrations index is the canonical "what
exists" surface; per-shim README files defer there for cross-shim
context.

Decision tree for a new shim is in
`../design/shim-pattern.md` §11.5.

## Why this and not mega-repo

Three load-bearing reasons:

1. **License entanglement.** AGPL deps on one shim, Apache 2.0 on
   another, vendor-EULA-bound on a third. A mega-repo forces
   lowest-common-denominator license posture. Per-class split keeps
   each repo's license posture clean per
   [`0111`](0111-vendor-friendly-license-posture.md).
2. **Procurement / criticism severability.** "We are critical about
   vendor A" must operationally mean we can deprecate
   `goat-<vendor-a>-shim` without touching the open-standards bundle or
   any other vendor shim. Mega-repo entangles deprecation with
   everything else.
3. **Blast radius and permission scope.** Per-vendor repo lets us
   scope GitHub permissions per-vendor, hand off ownership per-vendor
   (FFRDC / vendor-internal team / another goat consumer), and audit
   per-vendor. Mega-repo can't.

## Why not one-repo-per-everything

Open standards genuinely converge — they share the same
schema-declaration shape, the same audit hooks, the same lineage
edges, and they're maintained by interop-minded standards bodies that
release on synchronized cadences. Splitting ISA / SAPIENT / OMNI / CoT
each into separate repos costs duplication without buying
severability.

## Why substrate in-tree (not its own repo)

The shared scaffolding is the lever that keeps per-shim repos thin. If
it lived in a third sibling repo, every shim adopter would clone two
repos to start. Putting it in-tree — published as a normal Rust crate
and Go module — gives the same import experience while keeping the
substrate's evolution in lockstep with goat's identity, audit, and
schema invariants. Out-of-tree shims depend on it the same way as
in-tree.

## Edge cases

- **Vendor publishes their SDK as an open standard** (e.g. a vendor
  publishes its platform protobufs publicly). The vendor-SDK rule still
  wins. Published-API does not equal community-governed-standard.
  That vendor goes in `goat-<vendor>-shim`, not `goat-open-standards`.
- **TAK straddles both.** CoT-the-format is open and is the in-tree
  reference. TAK-server-product specifics (mission packages,
  federation bundles, server-admin RPCs) go in a separate
  `goat-tak-product-shim` if/when that work materializes.
- **A future shim is genuinely tied to goat-trunk identity** (e.g. it
  needs to ride alongside the offline-CA validator's process). It
  goes in-tree, not in a sibling repo. The in-tree TAK shim
  predated this ADR but matches this rule by accident.

## Consequences

- **Adopters discover available shims through one index** at
  `../integrations/INDEX.md`, even though
  the code lives across N repos. Discoverability stays in goat-trunk.
- **Cross-shim convention changes** (e.g. a new audit-hook field) are
  PR'd against `goat-shim-framework` in this repo; per-shim repos
  pick up the change on their own cadence by bumping the framework
  version.
- **Block 49** (implementation-plan.md)
  tracks the cross-repo coordination work — substrate framework
  scaffolding, repo creation, namespace reservations, integration
  index maintenance.
- **Severability is enforced by structure**, not policy. Deprecating
  one vendor shim is `gh repo archive`, not a roadmap negotiation.

## Addendum 2026-06-03 — off-fabric translator renamed `polygloat`, homed in usareur-af

The off-fabric translation service (Block 49H) is renamed **`goat-translator` →
`polygloat`** and its canonical home moves to
**`polygloat`** (`internal`
visibility), mirrored fresh from the original `goat-maintainer/goat-translator` scaffold.
The repo-containment rule of this ADR is unchanged: `polygloat` is the standalone
CLI + HTTP service that composes the open-standards adapters (CoT / SAPIENT / ISA /
OMNI from `goat-open-standards`) and build-tag-gated vendor adapters; it is
**off-fabric** and does not replace the on-fabric per-format shims. Maturity is
unchanged by the rename — scaffolding tier (engine + CLI + HTTP skeleton + echo
adapter only; no real format adapters yet).

**Open decision (deferred to operator): `polygloat`'s open/closed status.** The
fresh mirror is `internal`, consistent with the closed-core Phase-1 posture in
`../project/edge-attachment-growth-strategy.md`;
the original `goat-maintainer/goat-translator` is still public and needs reconciliation
(archive / redirect) once decided. Until then, docs describe `polygloat` as Apache
2.0 without asserting public availability. Dated reports and completed-work records
retain the historical `goat-translator` name per the project naming convention.

## Acronyms / project-specific terms

See `GLOSSARY.md`.
