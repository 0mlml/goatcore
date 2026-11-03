# ADR 1026 — Establish a Data-Access Common Operating Picture

**Status:** Proposed (2026-06-05)
**Repo scope:** goat core (new visualization surface; `goat-cli`; reads from Netbird, Ory, Zenoh, and the partner-release registry)
**Related:** design [`data-access-cop.md`](../design/data-access-cop.md) (the full plan) · [ADR 1025](1025-zenoh-dynamic-tenant-authz-slot-pool.md) (the four-layer access model this surface observes) · [ADR 1027](1027-partner-release-set-governance.md) (partner data exchange) · `goat-cli.md` · `panoscope.md` · `id-admin-console.md`

## Plain-language summary

Access to data in this system is decided in four separate places — the Netbird mesh (which machines are on the network), Ory (which people and services are signed in), the Zenoh access-control list (which publisher may write where), and a partner-release mechanism (which outside organizations may receive shared data). Answering a simple question like "why can't this partner publish?" or "who can read this sensor feed?" today means opening four tools and matching them up by hand. This decision is to build one read-only view — the **Data-Access Common Operating Picture** — that pulls from all four and answers, in both directions, *who can access which data and why*. It ships as a web surface and as `goat` command-line commands. A later phase lets an administrator fix access problems from the same place by triggering each layer's own controls. The view never enforces anything itself; if it is offline, access works exactly as before.

## Context

[ADR 1025](1025-zenoh-dynamic-tenant-authz-slot-pool.md) settles how authorization works, and in doing so makes explicit that enforcement is spread across **four layers**: the Netbird mesh, Ory identity, the Zenoh write-integrity access-control list, and partner release. That model is correct, but it has a cost: no operator can see the whole access state in one place, and every access question requires correlating four systems manually. The same ADR introduces a large pre-cut pool of publisher write-slots and short-lived certificates, which makes the Zenoh side more structured but also larger and faster-changing — increasing the need for a tool that can show, at a glance, which identity holds which permission and why.

The user requirement is direct: **see who has which permissions, troubleshoot access problems in both directions, and be able to tighten, sever, or restore access** — from both a visual surface and the `goat` command line.

## Decision

Build the **Data-Access Common Operating Picture** as described in [`data-access-cop.md`](../design/data-access-cop.md), with these commitments:

**One pattern, two arms.** The access picture and the runtime expected-vs-actual monitor ([ADR 1022](1022-zenoh-denial-observability.md)) are not two systems — they are the **visual arm** and the **alarm arm** of the *same* declared-vs-actual reconciliation (the third first principle in [ADR 1025](1025-zenoh-dynamic-tenant-authz-slot-pool.md)): one model of "who should have access ⊕ who actually does," surfaced two ways. The picture answers "who can access what, and why" on demand; the monitor alarms when observed diverges from declared. Build one reconciliation model; render it as a picture and an alert.

1. **It is read-only first and non-load-bearing.** It assembles an access graph by reading each layer's existing interface (Netbird management API, Ory admin API, Zenoh router admin space plus the publisher slot registry, and the partner-release registry). It stores no enforcement state of its own and enforces nothing. If it is down, the four layers still enforce exactly as before.

2. **It answers the access question in both directions** — forward (from an identity to everything it may write and read) and reverse (from a topic to every identity that may write, read, or receive it) — and returns the **chain of reasons** at each layer, not just a yes/no.

3. **It ships as two surfaces:** a fresh web surface, and a set of `goat access …` administrator commands in the `goat` operator CLI. The web surface is **new rather than a bolt-on** because each existing surface is scoped to a single layer; it **deep-links into** those surfaces (panoscope for fabric detail, the identity admin console for Ory detail, the mesh view for peers) for per-layer drill-down, reusing the established cross-surface deep-link contract.

4. **It is built in three phases:** *see it* (read-only graph, both queries, CLI queries), *explain it* (trace a single decision across the four layers and flag common misconfigurations), *act on it* (tighten, sever, restore — each action calling the owning layer's own control: revoke a mesh peer, disable an Ory identity, stop renewing a Zenoh certificate, edit release-set membership). Every action is audited and reversible.

5. **It is administrator-only and audited**, gated by an explicit Ory admin group, because it concentrates read access across every identity layer and, in the final phase, the power to revoke.

## Alternatives considered

- **Extend panoscope instead of a new surface.** Rejected as the primary home: panoscope is scoped to the Zenoh fabric, while the access picture spans the mesh, identity, fabric, and partner layers. The access picture deep-links *into* panoscope for fabric detail rather than living inside it.
- **Keep using the four per-layer tools separately (status quo).** Rejected: the manual four-system correlation is precisely the problem this solves.
- **Put enforcement into the access picture.** Rejected: enforcement stays in the four layers; the picture stays non-load-bearing so that a failure or compromise of the picture cannot itself open or close access.
- **Adopt a third-party identity-graph product.** Rejected: the access model is bespoke (a four-layer mix of mesh, OIDC, fabric ACL, and partner release) and runs in sovereign, often disconnected environments; an external product would not model these layers and would add an outside dependency.

## Consequences

**Positive.** One place to answer "who can access what, and why," in both directions; far faster troubleshooting; a single administrator path to tighten or sever access that still routes through each layer's real control.

**Negative.** A new surface to build and maintain. It is a concentrated privilege — broad read access across every identity layer and, in Phase 3, revocation power — so it is a high-value target that must be tightly gated, audited, and have its own credentials held and rotated carefully. The picture can be stale relative to fast-changing mesh and certificate state.

**Neutral.** It is derivative: it adds no new source of truth, only a combined view over existing ones, and links back to the per-layer surfaces for detail.

## Open items

Carried in [`data-access-cop.md`](../design/data-access-cop.md): data-freshness model per layer; distinguishing "cut off" from "never had access"; whether Phase-3 actions need a second approver; access-graph scale at full fleet size; and confirming the build home (standalone-with-deep-links vs. a lens in an existing surface).

## Status

**Proposed** pending ratification.
