# ADR 1027 — Partner release-set governance: releasability markings over groups of partners

**Status:** Proposed (2026-06-05)
**Repo scope:** goat core (release gateway / conditioning checkpoint, partner-peering portal, release-set registry, `goat-moon-pod` integration)
**Related:** design [`partner-data-exchange.md`](../design/partner-data-exchange.md) (the full plan) · [ADR 0821](0821-mission-partner-sharing-pattern.md) + [`mission-partner-sharing.md`](../design/mission-partner-sharing.md) (per-partner `release/` namespace this generalizes) · [ADR 1020](1020-zenoh-federated-peering-trust-model.md) (inbound origin-attributed quarantine) · [ADR 1025](1025-zenoh-dynamic-tenant-authz-slot-pool.md) (namespace discipline) · [`goat-moon-pod.md`](https://github.com/usareur-af/goatstuff/blob/main/docs/design/goat-moon-pod.md) · `partner-peering-portal.md` · [`data-access-cop.md`](../design/data-access-cop.md)

## Plain-language summary

When we deliberately share data with outside partners, each multi-publisher topic has to say **who is allowed to receive it**. We already have a way to release to a *single* named partner. What we lack is a way to release to a *group* of partners at once — for example, "release this to everyone in the group called zagros." This decision adds **release sets**: a release set is a named group of partner organizations, a producer marks data for a set by publishing it under that set's release path, and a gateway forwards it to whichever partners are currently in the set. It also places this next to the already-decided rule for data coming *the other way* — data from partners is held in a lower-trust, clearly-labeled area tagged with which organization sent it — so that both directions of partner data exchange are governed consistently and tie into how partners are onboarded and the appliance they run.

## Context

We are at the sandbox stage, standing up a sandbox goatnet to exercise **rapid onboarding** of partners both internal to that environment and external, including other organizations and sovereign partners. The governance below is written to hold at operational scale but kept coarse and revisable for now, so it does not slow onboarding.

Deliberate outbound release today uses a **per-partner** namespace — `release/<partner>/...`, default-deny, gated per topic ([ADR 0821](0821-mission-partner-sharing-pattern.md)). It expresses "release to this one partner." It cannot express "release to every partner in a group," which is the common case: most releases go to a standing group of partners, and the group's membership changes over time independently of the data.

Inbound is already decided: [ADR 1020](1020-zenoh-federated-peering-trust-model.md) holds partner data in an origin-attributed **quarantine** area (`<peering-entity>/<peering-id>/...`), separate from first-class internal namespaces and labeled as lower-trust. That already satisfies the requirement that inbound data be trust-differentiated and carry its origin.

What is missing is the outbound group mechanism, and a single place that ties outbound release, inbound quarantine, partner onboarding, and the partner appliance together.

## Decision

1. **Add release sets.** A **release set** is a named group of partner organizations whose membership is held in a **release-set registry** (the set name maps to its current member partners). A producer marks data for a set by publishing under `release/<release-set>/<topic>…`. **The release-set name in the key path is the releasability marking** — the data's own address states which group may receive it. This keeps release routable and auditable by prefix, consistent with the namespace discipline in [ADR 1025](1025-zenoh-dynamic-tenant-authz-slot-pool.md).

2. **The sandbox goatnet is, at the sandbox stage, a single coarse release set.** The sandbox (for example `zagros`) is the environment stood up to exercise rapid partner onboarding; its release set is its participating partners, and at this stage releasing to it means releasing to all of them. Finer per-set or per-partner governance is deferred to the operational definition rather than built now. The mechanism does not depend on how the sandbox set is later subdivided.

3. **The release gateway forwards by membership.** The conditioning checkpoint ([`mission-partner-sharing.md`](../design/mission-partner-sharing.md)) reads the release-set registry and forwards each release-set path to the moon pods of that set's current members. Adding or removing a partner is a registry edit, not a republish. The **default-deny baseline and the per-topic policy gate from [ADR 0821](0821-mission-partner-sharing-pattern.md) are retained.**

4. **Per-partner and release-set release coexist.** `release/<partner>/...` stays for one-to-one release; `release/<release-set>/...` is the one-to-many generalization. A single-member set is equivalent to per-partner release.

5. **Inbound is unchanged, and placed alongside.** Inbound partner data remains governed by [ADR 1020](1020-zenoh-federated-peering-trust-model.md): origin-attributed quarantine. This ADR does not alter that mechanism; it states the symmetry — **outbound marks who may receive; inbound marks who produced** — and brings both under one governance picture.

6. **Onboarding and the appliance are the integration points.** The partner-peering portal (`partner-peering-portal.md`) sets a partner's peering identifier (inbound origin) and its release-set memberships (outbound distribution) at onboarding, from one place. The [`goat-moon-pod`](https://github.com/usareur-af/goatstuff/blob/main/docs/design/goat-moon-pod.md) is the partner-side endpoint of both directions. The [Data-Access Common Operating Picture](../design/data-access-cop.md) reads the release-set and peering registries so an administrator can see and edit which partners are in which sets.

## Alternatives considered

- **Per-message releasability attribute only (no key segment).** Rejected as the primary form: Zenoh's access-control list cannot filter on message attributes (F-181), so an attribute-only marking is not prefix-routable or ACL-enforceable and would push all enforcement into gateway code. Per-message metadata remains available as an *additional* gateway-applied refinement if a future requirement needs it.
- **Per-partner release only (status quo).** Rejected: it cannot express "release to a group," forcing a producer to publish N copies or the gateway to fan out per-partner with no named group to manage.
- **A brand-new outbound mechanism unrelated to `release/`.** Rejected: it would duplicate the default-deny baseline, conditioning checkpoint, and audit already built for `release/<partner>/` ([ADR 0821](0821-mission-partner-sharing-pattern.md)). Release sets reuse all of it.

## Consequences

**Positive.** "Release to a group" becomes a first-class, routable, auditable marking; group membership changes without touching data or producers; outbound and inbound partner exchange are governed consistently and visible in one place; the existing release baseline and partner onboarding are reused, not replaced.

**Negative.** A new registry (release-set membership) becomes a governance-critical source of truth that must be authoritative and consistent across sites, and whose edits change who receives data — so its edits must be audited and controlled. Release-set and per-partner markings can overlap, needing a precedence rule.

**Neutral.** Release sets sit on top of the existing `release/` namespace and conditioning checkpoint; inbound quarantine is referenced unchanged.

## Open items

- Whether the operational definition keeps the sandbox as one coarse set (all participants) or introduces several named sets, and what policy attaches to each. The sandbox stage assumes one coarse set.
- Release-set membership change propagation (how fast, by what path, and whether removal must stop in-flight or cached data).
- Precedence when a topic carries both a release-set and a per-partner marking.
- Whether conditioning policy is expressed per set, per partner, or both.
- Inbound open items carry over from [ADR 1020](1020-zenoh-federated-peering-trust-model.md): peering-id reservation, quarantine retention window, gateway trust domain.

## Status

**Proposed** pending ratification.
