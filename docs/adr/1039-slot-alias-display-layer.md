# ADR 1039 — Slot alias display layer: human names over UUID anchors

**Status:** Accepted (2026-06-11) — built across the resolver tier, `goat` CLI, panoscope, and stethoscope (PR #1055); deploy + live-fire is the remaining operator step.
**Repo scope:** goat core — `goat-zenoh-portal` (alias registry: `system_handle` constraints + read/rename endpoints), `panoscope-core` (shared resolver), `goat-cli` (input resolve + output render), panoscope / stethoscope (render).
**Related:** [ADR 1025](1025-zenoh-dynamic-tenant-authz-slot-pool.md) (the per-uuid slot anchor this projects over — UUID = cert CN = ACL slot = write anchor) · [ADR 0844](0844-zenoh-deployment-topology-and-acl-v1.md) (the taxonomy nesting below the anchor) · [ADR 1026](1026-data-access-common-operating-picture.md) (the access picture that renders aliases; shared-resolver precedent) · [ADR 1020](1020-zenoh-federated-peering-trust-model.md) (cross-goatnet refs carry UUIDs, render local aliases) · F-177 (the silent-denial trap a fail-closed resolver must not re-create) · [`slot-alias-display-layer.md`](../design/slot-alias-display-layer.md) (the design).

## Plain-language summary

The slot UUID (`0123456789abcdef0123456789abcdef`) is the right identity for the machine but unreadable for humans, and it clutters every operator surface. Give each slot a short human **alias** (`user-1-operator`) that humans see and type everywhere, while the UUID stays the only thing the router, ACL, certs, storage, and audit ever touch. The portal already records this handle at mint time; this formalizes it (unique, validated), adds a shared resolver, and surfaces it across `goat`, panoscope, stethoscope, and the portal — with one hard rule that keeps it from becoming a fragility: **the alias is never written into any machine path.**

## Context

[ADR 1025](1025-zenoh-dynamic-tenant-authz-slot-pool.md) binds one 32-hex UUID to three machine roles at once (cert CN, ACL slot rule `<uuid>/**`, enforced write anchor). That binding is correct and irreducible; the UUID cannot be replaced with a friendly name without undoing the prefix-prediction and forge-resistance ADR 1025 bought. The pain is purely human-factors. The portal already stores `SlotID` (UUID) + `SystemHandle` (human handle) + `DisplayName` (free text) per slot, and panoscope already truncates UUIDs lossily for display — so the appetite and the raw data both exist; what's missing is a constrained, surfaced, consistently-resolved alias.

## Decision

Add a **presentation-only slot alias** projected over the UUID, governed by one invariant: **the alias is never written into any load-bearing path (ACL, cert, wire topic key, storage key, audit record, `--json`).** It appears only at input boundaries (resolved to a UUID before any action) and output boundaries (rendered from a settled UUID).

1. **Registry (portal = SoT).** Formalize `system_handle` as a unique, shape-constrained alias: `[a-z0-9][a-z0-9-]{1,62}`, not 32-hex-shaped, no `/`, reserved-word-guarded against namespace classes; DB-unique per goatnet; goatnet-local (not global). Exposed via a cacheable read endpoint + an operator-gated rename.
2. **Resolver (shared, `panoscope-core`).** `resolve_input(token)` (UUID-shape → verbatim; else alias lookup; **unknown alias fails closed**, never silent passthrough) and `render(uuid)` (alias, else truncated-UUID fallback). Rewrites only the leading key segment; the ADR 0844 taxonomy below the anchor is untouched.
3. **Resilience.** Surfaces cache the map and degrade to UUID-only display + UUID-only input when the portal is unreachable. Aliases never block operation.
4. **Open decisions settled:** (a) `system_handle` is the strict alias, `display_name` stays free-text title; (b) rename is **operator-only** at the sandbox stage (matches `topic` write authority; revisit for self-service later); (c) audit stores **UUID only** and renders the **current** alias with a `(was: X)` affordance — snapshot-at-event is rejected because it would require writing the alias into the record.

## Consequences

- **Operators stop seeing/typing UUIDs.** `goat doctor --topic user-1-operator/hello`, panoscope shows names, deep links render aliases. `--uuid`/`--json` still expose the canonical value.
- **Renaming is cheap and safe** — a projection update with zero ACL/cert/topic/storage churn, precisely because the alias never load-bears. This is the property that makes aliases usable.
- **No new outage surface.** The portal-down path degrades to today's UUID behavior; the alias layer can't break the fabric.
- **One resolver, four surfaces** — `goat`/panoscope/stethoscope/portal can't drift, the same way the access picture shares one graph.
- **Coordination cost:** the in-flight panoscope UUID-shortening must become a consumer of this registry rather than a parallel mechanism. Migration backfills `system_handle` for existing slots; un-backfillable ones keep UUID display (no hard failure).

## Alternatives considered

- **Replace the UUID with the friendly name as the anchor.** Rejected — re-introduces forgeable namespaces and unpredictable prefixes that ADR 1025 closed; renames would churn ACLs + certs. The whole point is to *project*, not replace.
- **Lossy truncation only (status quo in panoscope).** Insufficient — collides (shared hex prefixes) and carries no meaning. Truncation stays only as the *fallback* when no alias is known.
- **Snapshot the alias into each audit record** (so historical logs show the name-as-of-then). Rejected — writing the alias into a stored record violates the invariant and couples audit integrity to a mutable field. Render-time join with `(was: X)` gives the same readability without the coupling.
- **Globally-unique aliases across goatnets.** Rejected — forces coordination across isolated goatnets for no benefit; aliases are goatnet-local, UUIDs carry across federation. (Zooko's triangle: UUID = secure+global, alias = memorable+local.)
- **Alias resolution in each surface independently.** Rejected — guarantees drift and divergent fail modes; one shared `panoscope-core` resolver instead.
