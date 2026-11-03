# ADR 1031 — Credential revocation: mesh-first, inventory-driven, principal-scoped offboarding

| | |
|---|---|
| Status | Proposed (2026-06-06) |
| Related | [ADR 1025](1025-zenoh-dynamic-tenant-authz-slot-pool.md) (mesh-first revocation; per-uuid slot pool), [ADR 0407](0407-internal-x509-ca-for-service-tls.md) (offline CA, no CRL/OCSP), [ADR 0307](0307-setup-key-rotation.md)/[0310](0310-setup-key-kill-switch.md) (setup-key TTL + kill switch), [ADR 0311](0311-peer-naming-convention.md) (peer naming), [ADR 0859](0859-easy-rollback-engineering-principle.md) (easy rollback). Design: `credential-revocation-and-offboarding.md`. |

## Plain-language summary

When someone or some system should lose access to the fabric, we cut them off by **running the onboarding process backward**: the same `goat admin onboard <handle> --revoke` walks the surfaces a principal was enrolled on (NetBird mesh, Ory identity, Zenoh cert) and removes them. We do **not** build a certificate revocation list — instead we cut at the mesh and identity layers, which is instant and is where goat already has negative control. Because a credential can span many peers, keys, and certs, revocation **enumerates every artifact** a principal holds rather than assuming one of each, and it understands that an org contains both **people and systems as siblings** — so revoking a departing operator does **not** take down the systems they happened to bring online.

## Context

goat has the per-domain cut-off mechanisms (NetBird peer delete, Ory identity disable, portal vendor delete, setup-key kill switch) but **no coherent offboarding process** that ties them together. The onboarding playbook (`operator-onboard.yml`) declares an `action=revoke` path but it is a stub: 44 task gates run on `enroll`, 0 on `revoke`, and the bundle/audit phases would mis-fire on a revoke run. There is no design doc, no ADR, and no operator runbook for "remove a principal."

Three realities shape the decision:
1. **No CRL/OCSP** (ADR 0407): the offline/YubiKey CA cannot sign a revocation list on demand, and the Zenoh router has no CRL support at all (ADR 1025 §5). Cut-off must live at the mesh/identity layers, not in PKI.
2. **One principal → many artifacts.** A principal can hold many peers, many setup keys (or one *reusable* key that enrolled many peers — revoking the key does **not** disconnect already-enrolled peers), and many certs. "Delete the peer" is not a proxy for "cut the principal."
3. **The cert model is migrating** per-vendor (shared CN, today) → per-uuid (one cert per system, ADR 1025), which changes revocation granularity over time.

## Decision

1. **Symmetric playbook (Option A).** Offboarding is `operator-onboard.yml -e action=revoke` walking surfaces in reverse — not a separate kill-switch tool. One mental model, one tested surface. The fleet-wide setup-key kill switch (ADR 0310) remains the blunt incident tool, orthogonal to per-principal offboarding.
2. **Mesh-first cut-off.** Authoritative cut-off is NetBird (machines) + Ory (humans/services); Zenoh write-grants are removed by **ACL de-authorization** (pull the subject/policy → cert is inert under default-deny), not PKI revocation. CRL/OCSP is deferred and scoped to the service-TLS / offline-bundle CA-compromise case only.
3. **Inventory-driven enumerate-and-cut.** Revocation discovers and cuts **every** artifact a principal holds: all peers (via the `peer-<handle><short_tag>-*` naming prefix + NetBird `setup_key_id`), all setup keys, all cert grants. Default to one-off setup keys; reusable keys are allowed but mandate peer enumeration.
4. **Principal graph, scoped selector.** An org contains **persons and systems as siblings**; the person→system link is **custody/provenance, not ownership**. Revocation **cascades along org→member containment, never along person→system custody**. The selector is `(type, id)` ∈ {`uuid`, `person`, `org`} or a subset: revoking a person cuts the person's own artifacts and re-attributes their custody edges but leaves org-owned systems running; revoking an org cascades to all members; revoking a uuid cuts one system.
5. **Reversible by default.** Ory identities are **deactivated** (reversible), not deleted, unless `--purge`. The NetBird peer is deleted (terminal — instant transport denial is the security guarantee; re-admission is a fresh enrollment). Aligns with ADR 0859.
6. **Issuance registry is the source of truth for the selector.** Enroll records the containment + custody graph (org ↔ member ↔ peer/cert; person → system); revoke resolves the selector against it. Staged: Stage 1 person-scope via live discovery (no new state); Stage 2 registry; Stage 3 `uuid`/`org` selectors with the per-uuid cert model.
7. **Receipt + audit.** Every revoke renders a receipt (artifacts enumerated/cut, scope, last-one-out decision, registry-vs-live discrepancy) and an `action=revoke` audit row.

## Consequences

**Positive.** Closes the designed-not-built revocation gap with one symmetric, idempotent, reversible command; instant cut-off on stock images, no fork, no CRL infra; the principal graph prevents the cascade footgun (departing operator ≠ dead systems); forward-compatible with the per-uuid cert model.

**Negative / cost.** The selector needs a new issuance registry (a governance-critical SoT to keep consistent). Completeness depends on enrollment going through the tracked path — out-of-band peers are a residual gap, backstopped by dark-by-default + a peer-reconciliation probe. Reusable keys carry an enumeration burden.

**Neutral.** Last-one-out shared-vendor handling is transitional and retires when per-uuid lands. CRL remains unbuilt; service-TLS/bundle CA-compromise revocation is a separate future effort.

## Alternatives considered

- **Separate kill-switch tool for offboarding.** Rejected — duplicates the onboarding surfaces and audit; the symmetric playbook reuses them. (The *fleet-wide* key kill switch, ADR 0310, stays for incidents.)
- **CRL/OCSP for per-principal revocation.** Rejected/deferred — offline CA can't sign on demand, Zenoh can't consume a CRL, and mesh + ACL de-authorization already neutralizes the cert. Reserved for the service-TLS/bundle CA-compromise case.
- **Person-as-parent hierarchy (cascade person→systems).** Rejected — would silently take down org-owned systems when an operator who enrolled them leaves. Person→system is custody, not ownership.

## Open items

1. Issuance-registry schema (containment + custody relations) and enroll-side population (Stage 2).
2. NetBird `peers.setup_key_id` linkage confirmation as the stronger enumeration path.
3. Peer-reconciliation probe (flag live peers absent from the registry) — the out-of-band backstop.
4. `--purge` semantics for the Zenoh/Hydra surfaces (hard vs. deactivate) per surface.
