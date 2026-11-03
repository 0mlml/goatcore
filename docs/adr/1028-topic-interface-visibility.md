# ADR 1028 — Topic interface visibility: advisory contract metadata, not access control

**Status:** Proposed (2026-06-05)
**Repo scope:** goat core (declared-state / topic registry, discovery surfaces — panoscope, the portal, `goat-cli`, the Data-Access COP)
**Related:** [ADR 1025](1025-zenoh-dynamic-tenant-authz-slot-pool.md) (this concept was moved *out* of the access model into here) · [`pubsub-state-as-code.md`](../design/pubsub-state-as-code.md) + [`pubsub-best-practices.md`](../design/pubsub-best-practices.md) (the declared-topic registry this rides on) · [ADR 0844](0844-zenoh-deployment-topology-and-acl-v1.md) (namespace) · `panoscope.md` · [`data-access-cop.md`](../design/data-access-cop.md) · [ADR 1027](1027-partner-release-set-governance.md) (partner *access*, which this is **not**)

## Plain-language summary

Some topics on the fabric are a **published interface** — a stable thing other systems are meant to build on. Others are a system's **internal implementation detail** — scratch or intermediate data that happens to be on the bus but that nobody else should depend on, because it can change or disappear without notice. This is the same idea as public vs private (or exported vs internal) in software. Systems browsing the Zenoh namespace need to be told which is which, so they build on the stable interfaces and leave the internals alone.

This decision says that distinction is a **discoverability and contract signal, not an access-control rule.** "Internal" means *please don't depend on this*, not *you are forbidden to read this*. So we express it as **advisory metadata on the topic** (in the same registry that already records a topic's schema and format), surfaced by the tools people use to explore the fabric — **not** as a permission, and **not** as a mandatory segment baked into every topic's address.

## Context

An earlier draft of [ADR 1025](1025-zenoh-dynamic-tenant-authz-slot-pool.md) modeled "public/private" as a per-publisher namespace **access** region (`<uuid>/pub/**` community-readable, `<uuid>/priv/**` default-deny). On reflection that conflated two genuinely different axes:

- **Access** — *who may read a topic.* On this fabric that is community-default for onboarded members, with the rare exceptions handled by the `release/<set>/` namespace for partners ([ADR 1027](1027-partner-release-set-governance.md)) and by payload encryption for true confidentiality. It is **not** a per-publisher concern.
- **Interface visibility** — *is this topic a stable contract others should build on, or an internal detail they should leave alone.* This is a property of the topic's **design intent**, advisory, and changes meaning ("promote internal → public", "deprecate") over a topic's life.

Forcing the second through the first is wrong: it burdens every publisher with an access classification on every topic, bakes the signal into the wire address (so you cannot promote a topic to "public" without re-keying it), and — most importantly — tells consumers "you may not read this" when the real message is "don't couple to this." Marking it as access also doesn't even *do* anything at the sandbox stage, where reads are community-default.

So the concept comes out of the access model and gets its own, correctly-shaped home here.

## Decision

1. **Interface visibility is advisory topic metadata, carried in the declared-state / topic registry** ([`pubsub-state-as-code.md`](../design/pubsub-state-as-code.md)), alongside the schema and format a topic already declares — **not** a namespace-access region and **not** an ACL rule.

2. **The field expresses design intent, with room for maturity.** The core distinction is:
   - `visibility: public` — a **published interface**: a stable topic + schema that other systems may rely on.
   - `visibility: internal` — an **implementation detail**: on the bus, but not a contract; may change or disappear without notice; don't couple to it.

   It extends naturally to a lifecycle/maturity dimension where useful — `experimental` (public but not yet stable), `deprecated` (public but going away) — so the signal is a real "usefulness indication," not just a binary.

3. **It is advisory — never enforced.** Nothing denies a read based on `internal`. A consumer *can* read an internal topic; the metadata only tells it that it *shouldn't build on it*. (If a specific topic genuinely must be unreadable to some party, that is **access** — use `release/`/tiers or encryption, separately.)

4. **It is surfaced by the discovery surfaces, where people explore the namespace** — panoscope (namespace browser), the portal's topic registry, the Data-Access COP, and `goat-cli` topic listings — which filter/badge topics by visibility (e.g., "show public interfaces", "this topic is internal/experimental/deprecated"). The registry is the source of truth.

5. **An optional soft key convention is allowed but not required.** A reserved segment or marker (e.g. an `internal/` subtree) may be used for at-a-glance greppability and pattern-subscribe, but it is advisory and the registry metadata is authoritative. The default is metadata-only — no mandatory per-topic segment.

6. **Sandbox stage: capture now, build with the discovery surfaces.** The metadata field and its surfacing are an additive, advisory layer; they are adopted when a topic registry / discovery surface needs to express the distinction, not stood up speculatively. No access behavior changes.

## Alternatives considered

- **Per-publisher `pub`/`priv` access region (the ADR 1025 draft).** Rejected — conflates discoverability with access (see Context); pollutes the wire address; can't promote without re-keying; does nothing at the sandbox stage where reads are community-default.
- **Enforce it (deny reads of `internal`).** Rejected — that is access, not visibility, and a different (and rarely needed) requirement. Don't make a contract hint a permission.
- **Do nothing / leave it implicit.** Rejected — systems perusing the namespace then can't tell a stable interface from scratch, and end up coupling to internals that change underneath them. The signal has real value; it just belongs in metadata.
- **Key-segment convention as the source of truth.** Rejected as primary (kept as an optional soft aid) — same re-keying and address-pollution problems as the access-region form.

## Consequences

**Positive.** Clean separation of access from discoverability; ADR 1025 stays purely about authorization. The signal rides existing topic-contract machinery (schema/format already declared), surfaced where exploration already happens. Promotable/deprecatable without re-keying. Purely advisory, so it can never accidentally hide data or block a consumer.

**Negative / cost.** It depends on producers (or the onboarding flow) actually setting the field; an unset field defaults to a sensible value (treat as `internal` until declared `public`, so nothing is *implied* stable by omission). It is advisory, so a determined consumer can still couple to an internal topic — that's accepted; the goal is a clear signal, not a wall.

**Neutral.** Composes with the Data-Access COP (which can show visibility alongside access) and with schema/contract management.

## Open items

- The exact field name and value set (`visibility: public|internal` vs a combined `stability:` lifecycle), and the default for unset.
- Whether/where a soft key convention is worth the greppability vs the address noise.
- Surfacing details in panoscope / portal / `goat-cli` (badge, filter).

## Status

**Proposed** pending ratification.
