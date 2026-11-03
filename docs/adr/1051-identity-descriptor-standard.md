# ADR 1051 — Identity descriptor standard: a portable card with a persistent spine and a dynamic, effective-dated relationship graph

| | |
|---|---|
| Status | Proposed (2026-06-18; **amended 2026-06-19 with v1.1 evidence-driven adaptations — see Amendment below**). Designed-not-built; demo pushed + runnable (`hello-galaxy-universal-id`, Hello Galaxy #3). |
| Related | ADR 1043 (the five-dimension grammar this packages), [ADR 1039](1039-slot-alias-display-layer.md) (the practical-name projection + never-in-a-machine-path discipline), [ADR 1044](1044-credential-rotation-in-place-reissue.md) (rotation = how a born-as stamp is re-baselined), [ADR 1025](1025-zenoh-dynamic-tenant-authz-slot-pool.md) (the UUID slot anchor + the CN-not-URI-SAN ACL match), [ADR 1026](1026-data-access-common-operating-picture.md) (the declared registry this resolves against), ADR 0412 (SPIFFE — same dual-field cert shape). Design: [`identity-descriptor-standard.md`](../design/identity-descriptor-standard.md). |

## Plain-language summary

When we onboard a peer we hand it a certificate whose Common Name is a UUID slot
anchor. That one UUID quietly load-bears across five roles — write-namespace,
cert CN, ACL subject, audit dedup key, provenance source-id — but the onboarding
bundle explains only one of them, and only by example. There is no single,
machine-readable document that states a peer's canonical identity, its practical
name, and its (changeable) organizational relationships in a form the recipient
or any downstream platform can consume, graph, and traverse.

This ADR defines that document: an **identity descriptor**, emitted into the
bundle and resolvable from the registry, split into a **persistent spine**
(immutable, cert-anchored — the UUID and cert binding) and **dynamic attributes**
(registry-resolved — affiliation, command relationship, role, echelon, site).
Relationships are recorded as **effective-dated edges**, so the same document
answers both "who does this report to now?" and "who did it report to on the
date of that audit event?" It is published as a small, versioned, extensible
standard so the same identity is legible across Zenoh, X.509/SPIFFE, NetBird,
Ory, and external partners.

This is mostly **synthesis**: the field schema (grammar, ADR 1043), the practical
name (slot alias, ADR 1039), and the source of truth (declared registry, ADR
1026) already exist. The new commitments are the portable artifact, the
cross-technology schema, the effective-dated graph surface, and per-field
provenance.

## Context

Evidence of the gap (verified from the mint path, not inferred): the portal's
`POST /api/vendors` *response to the operator* states plainly that "write
namespace, cert CN, and portal prefix are all the slot anchor," but the
`NOTES.md` shipped inside the tarball
(`ops/goat-zenoh-portal/internal/certmint/certmint.go`) prints only
`Your vendor prefix:` and `Your URI-SAN identity:` with no narrative tying the
UUID to its five machine roles. The binding is **explicit to operators,
implicit to recipients.** An external partner consuming our identity has even
less: a cert and a UUID, and no portable statement of what either means or how
the relationships behind them are structured or change.

Two facts about the existing substrate shape the decision:

1. **Affiliation already rides the cert URI-SAN** (`aff`/`aff-kind`) as
   attribution, and is deliberately **never** a NetBird group nor part of the
   Zenoh write anchor (ADR 1043).
2. **The Zenoh ACL matches the CN (the UUID), not the URI-SAN** (ADR 1025). So
   the URI-SAN affiliation value is never load-bearing for access — it is pure
   attribution.

## Decision

### 1. One descriptor, two layers by mutability

Define a self-describing `goat.identity.descriptor/v1` document with:

- **`spine`** — immutable, cert-anchored: `uuid` (the slot anchor; never reused),
  `goatnet`/`urn` (the URI-SAN scheme), `slot_index`/mint provenance, and
  `cert_fingerprint` (semi-persistent — rotates per ADR 1044; the anchor it
  certifies is constant). Changing a spine field is impossible (uuid) or a
  rotation (cert), never a routine edit.
- **`attributes`** — registry-resolved, mutable without touching the cert:
  `alias` (the ADR 1039 `system_handle`), `display_name`, the five grammar
  dimensions (ADR 1043), and the relationship `edges`.
- **`provenance`** — per field, `source: cert|registry` and
  `authority: born-as|currently-is`, so a consumer knows which to trust.
- **`extensions`** — a namespaced (`x-<owner>/…`) escape hatch; unknown keys are
  ignored, never fatal. This is the "loose but predictable" property: stable
  core, loose edges.

### 2. Born-as (cert) vs currently-is (registry)

Affiliation/comrel are dynamic. The cert URI-SAN keeps affiliation as a **frozen
born-as stamp** (origin record, tamper-evident, valuable *because* frozen); the
**registry is authoritative for current** affiliation/comrel, resolved live. The
two legitimately diverge between rotations; `provenance` tells consumers which to
read. A reorg is a **registry edit only — zero cert churn**; the born-as stamp is
re-baselined solely by a deliberate rotation (ADR 1044).

This keeps a tamper-evident origin record we already have for free (the stamp is
never load-bearing for access — fact 2 above), while putting all liveness in the
registry. We reject "cert carries only the spine, affiliation registry-only"
because it forfeits that origin record; the only argument for it — "the stamp
goes stale" — dissolves once the stamp is *defined* as born-as, not current.

### 3. The graph is effective-dated

Each relationship edge carries `[from, to_date)` validity. The same edge set
answers the **current** parent (open edge) and the **as-of-date** parent (the
edge whose window contains a given timestamp), so traceability — "who did this
node report to when *that* audit event was logged?" — is a graph walk against the
record we already keep, not a separate history store. v1 edges = `parent` only
(matching the grammar's current scope); lateral/support edges reserved.

### 4. It is a projection, never a new authority

The descriptor adds no enforcement path. The cert stays the trust anchor; the
UUID stays the sole canonical machine key; the registry stays the source of
truth. The descriptor makes the existing bindings legible and queryable —
mirroring the ADR 1039 discipline (the alias is never written into any machine
path; the descriptor never becomes one either).

### 5. Published as a cross-technology standard

JSON Schema for portability (+ protobuf for on-fabric resolution, sequenced).
Each technology consumes the subset it understands: Zenoh ← `spine.uuid`; X.509
← `uuid`/`urn`/born-as affiliation; SPIFFE ← `uuid` as SVID CN; Ory ←
`grammar.role`; external partner ← the whole descriptor + `extensions`.

## Consequences

- **Recipients and external platforms get a legible, portable identity** — the
  operator/recipient asymmetry closes; a partner can enforce off one document
  with no shared database or runtime.
- **Reorg is cheap and non-destructive** — relationship change is a registry
  edit; identity never re-keys.
- **Traceability for free** — current + historical from one effective-dated
  graph.
- **A new artifact to keep honest** — the descriptor must never be treated as
  authoritative where the cert/registry is; `provenance` is mandatory, and the
  roster-sanitization bar governs what's safe to publish externally.
- **Sequencing** — the runnable cross-platform demo
  (`hello-galaxy-universal-id`) is stood up early to pressure-test the
  schema; emission into the bundle + registry resolution + the protobuf wire
  follow.

## Alternatives considered

- **Leave the binding implicit (status quo).** Rejected — the recipient
  asymmetry is real and blocks external consumption.
- **Bake all attributes into the cert.** Rejected — makes every reorg a re-mint;
  contradicts dynamic affiliation.
- **Registry-only, drop the URI-SAN stamp.** Rejected — forfeits a free
  tamper-evident origin record; see Decision §2.
- **A new identity service/authority.** Rejected — re-introduces a central
  authority the brokerless/zero-trust posture deliberately avoids; the descriptor
  is a projection over existing anchors, not a new one.

## Open questions

- External-publication scope — what is safe to publish as a community standard vs goatnet-internal (roster-sanitization bar).
- When lateral/support edges land beyond v1 `parent`.

(Wire format + document signing, consistency-token concretization, and version negotiation are resolved in Amendment 2 below.)

## Amendment — v1.1 evidence-driven adaptations (2026-06-19)

A cross-industry deep-research review (23 sources, 22 adversarially-verified claims) judged each v1 choice against the documented success/failure record of comparable standards (SPIFFE, X.509 PMI/attribute-certs, CRL/OCSP revocation, W3C VC/DID, SCIM/SAML, Google Zanzibar, bitemporal modeling, protobuf/XML/JSON-LD extensibility, NIST ABAC/XACML). **Outcome: the architecture is validated — keep it; apply four targeted adaptations, no redesign or restart.**

Validated as-is (citations in [`identity-descriptor-standard.md`](../design/identity-descriptor-standard.md) §12): the two-layer split (RFC 5755 separation rationale); rotate-not-revoke / no CRL-OCSP (SPIFFE, smallstep, CA/B-Forum-2023, LE-OCSP-end-2025); registry-not-second-cert (PMI's ACRL failure is evidence *for* it); namespaced ignore-unknown extensibility (protobuf) and *not* adopting W3C VC wholesale (GS1); per-consumer subset reads (avoids XACML's whole-corpus cost, NIST SP 800-178); offline document with governed caching (NIST SP 800-162 §3.3.1, the "deployed ship" = our denied edge).

Adaptations folded into the design (§3 v1.1, §4.1, §4.2, §5):

1. **Edge freshness / consistency token** — embedding a mutable graph in a portable document is the staleness/"new enemy" hazard Zanzibar solved with zookies; add `resolved_at` + opaque `registry_snapshot` + `registry_ref`, and require grant/releasability decisions to use bounded staleness or a live query (display/forensic reads may use the cache).
2. **Narrow the cert** — keep the born-as URI-SAN stamp but specify it as *attribution-only, never an authorization input* (already true — ACL matches CN), with dropping it from the URI-SAN as the explicit v2 option. (The strong "certs must carry nothing" claim was refuted in review: minimize, not forbid.)
3. **Bitemporal edges** — add `recorded_at` (transaction-time) alongside valid-time so audit can reconstruct decision-time belief, not just eventual truth.
4. **Document signing + wire format + version negotiation** — optional detached `sig`, CBOR/COSE for the RF bearer, and an explicit version-negotiation/deprecation path; open items to settle before the first external mint.

The single strongest restart argument — "ship a consistency-stamped service, not a portable graph" (full Zanzibar) — was rejected because it applies to one layer (fixable with a consistency token), the denied-edge target *requires* a portable cached document NIST blesses, and every other choice is positively validated.

## Amendment 2 — open items concretized (2026-06-19)

The four adaptations above included items deliberately left open pending decisions ("settle before first external mint"). All are now decided and folded into the design doc:

1. **Consistency-token mechanism (§4.1 Decision).** `registry_snapshot` is a registry-issued, opaque, monotonic snapshot token (zookie analog), advanced on every mutation. One rule, two satisfy-paths: a grant runs against a snapshot proven current by a live token-equality check when connected, **or** a bounded-staleness check (`now − resolved_at ≤ max_staleness`) when the registry is unreachable; otherwise **fail closed**. Degrades cleanly to the denied edge; the registry keeps token→snapshot history for audit ("was token zk:N current at time t?"). The earlier three competing options collapse into this one.
2. **Document signing (§5.1).** Optional detached `sig` over the canonical document, made by the **registry signing key** (not the peer cert). Two independent roots: the cert vouches for the spine, the registry signature for the dynamic attributes. OPTIONAL intra-goatnet (mTLS covers integrity), RECOMMENDED for any cached/off-goatnet/partner distribution so a consumer can verify offline.
3. **Wire format (§5.2).** JSON is the canonical interchange form; a CBOR/COSE profile (1:1 field transcoding, `COSE_Sign1` signature) is the encoding for the constrained RF/Reticulum bearer ([ADR 1050](1050-reticulum-denied-edge-bridge.md)). Protobuf stays the on-fabric Zenoh-sample wire, not this portable artifact.
4. **Version negotiation (§5).** `schema = <name>/v<major>`; additive minors governed by ignore-unknown; `?v=<major>` Accept-style negotiation serving the highest mutually-supported major; breaking changes bump the major behind an announced deprecation window with both majors served through it; the `schema` field is authoritative and an unknown major fails closed.

These were implemented in the demo (`hello-galaxy-universal-id`): a real detached document signature (`lib/docsig.py`) verified offline by a consumer, alongside the already-shipped freshness/consistency-token act and bitemporal edges. Status stays **Proposed** (designed-not-built in goat core; emission in `certmint`, registry resolution, the signature/CBOR path, and version endpoint remain to build).
