# Identity descriptor standard — a portable, graph-traversable identity card

| | |
|---|---|
| Status | Design (authored 2026-06-18; §3 decision made; **v1.1 evidence-driven adaptations 2026-06-19, §12**). [ADR 1051](../adr/1051-identity-descriptor-standard.md) (Proposed). Designed-not-built; demo pushed + runnable (`hello-galaxy-universal-id`). A cross-industry evidence review (deep-research, §12) found the architecture sound — keep it, with four targeted adaptations: an edge **freshness/consistency token**, **bitemporal** edges, a **narrowed cert**, and a **document-signing/wire-format + version policy** — **all four now concretized (2026-06-19): consistency-token mechanism §4.1, document signing §5.1, CBOR/COSE wire profile §5.2, version negotiation §5.** |
| Problem | A peer's onboarding bundle hands it a UUID slot anchor that load-bears across five machine roles (write-namespace, cert CN, ACL subject, audit `src_zid`, provenance source-id), but the bundle explains only one of them, and only implicitly. There is no single, self-describing, machine-readable artifact that states a peer's canonical identity, its practical name, and its (changeable) organizational relationships in a form the recipient — or anyone downstream — can consume, graph, and traverse transitively. |
| Proposal | Define a versioned, extensible **identity descriptor** emitted into the bundle and resolvable from the registry, split into a **persistent spine** (cert-anchored, immutable) and a **dynamic attribute set** (registry-resolved, effective-dated, traversable as a graph). Publish it as a loosely-but-predictably standardized schema usable across technologies (Zenoh / cert URI-SAN / SPIFFE / NetBird / Ory / external partners). |
| Invariant | The descriptor is a **projection and attribution layer, never a new authority.** The cert stays the trust anchor; the UUID stays the sole canonical machine key; the registry stays the source of truth for mutable relationships. The descriptor adds no new enforcement path — it makes the existing bindings legible and queryable. (Mirror of the [slot-alias-display-layer](slot-alias-display-layer.md) discipline.) |

## 0. BLUF

Today the cert↔UUID binding is **explicit at the operator/architect layer and implicit at the recipient layer** (evidence: the portal mint *response* states "write namespace, cert CN, and portal prefix are all the slot anchor," but the `NOTES.md` shipped in the tarball prints only `Your vendor prefix:` + `Your URI-SAN identity:` with no narrative tying the UUID to its five roles — `ops/goat-zenoh-portal/internal/certmint/certmint.go`). This doc proposes closing that asymmetry by shipping one self-describing identity descriptor, and formalizing it as a small published standard so the same identity is legible across our stack and to external consumers.

This is **mostly synthesis, not greenfield**: the five-dimension grammar (identity-grammar.md, ADR 1043) is already the field schema; the affiliation edge model (parent edges, `vendor`/`organic`/`partner`) is already the graph; the slot-alias layer ([ADR 1039](../adr/1039-slot-alias-display-layer.md)) is already the practical-name projection; the Data-Access COP's declared registry ([data-access-cop.md](data-access-cop.md)) is already the source. The new work is (1) emitting a portable descriptor into the bundle, (2) a versioned/extensible cross-tech schema, (3) exposing the affiliation graph as a resolvable, effective-dated surface.

## 0.5 What this makes possible — capabilities we didn't have before

The point of the standard is not "a nicer identity file." It is a set of things that were previously impossible, hard, or bespoke-per-integration, and become routine once one portable, signed, freshness-stamped, graph-bearing identity record exists. Before → now:

| Before | Now (with the descriptor) |
|---|---|
| Each platform kept its own identity/attribute mapping; sharing an entity across two stacks meant a bespoke integration per pair. | Hand over **one record**; every platform enforces access + attribution off it — no shared database, no per-pair integration, no runtime in common. |
| Re-task-organizing a unit (changing who it answers to) risked re-issuing certs/credentials and touching every consumer. | A reorg is **one registry edit**; the identity (UUID/cert) never moves and every consumer updates coherently. |
| "Who did this entity report to *at the time of that event*?" was unanswerable without reconstructing from logs. | A **bitemporal graph walk** answers current *and* historical — including what was *believed* at the moment of a past decision. |
| Identity/attribute decisions needed a live call to an enterprise IdP/attribute store — impossible at the denied edge. | A **signed, freshness-stamped** record lets an edge node decide **offline**, with governed bounded-staleness (NIST-blessed for disconnected ops). |
| Releasability/coalition sharing was static lists or hand-marking. | "Is this entity under a partner I share with, now / as-of then?" is a **transitive graph query** off one record. |
| The same entity meant different things in each system (a Zenoh ACL subject ≠ a SPIFFE ID ≠ an Ory group ≠ a NetBird peer), so cross-stack reasoning was manual. | One descriptor **maps to each** — the entity is recognizable across the whole stack and to an external partner who has none of our infrastructure. |
| A partner had to trust whatever we asserted over a call, or stand up a federation. | The record is **independently verifiable offline** — its integrity + origin check against a signature, and its identity spine against the cert, with no call back to us. |

The one-line version: **a single identity that travels, stays current, proves itself, and means the same thing everywhere — including off-network and across vendors who share nothing else.**

## 1. Vocabulary

Define before architecture (per house style). The rest of the doc uses these exactly.

- **Identity descriptor** — the portable, machine-readable document this standard defines. One per peer/system/machine identity. Self-describing (carries its own schema version + field semantics).
- **Persistent spine** — the descriptor fields that are immutable for the life of the identity: born with it, survive reorg/relocation/re-key, and may be cert-anchored. The UUID anchor is the spine's primary key.
- **Dynamic attributes** — the descriptor fields that legitimately change over time without re-issuing identity: affiliation, command relationship (comrel), role assignment, echelon, site. Resolved live from the registry; effective-dated.
- **Effective-dated edge** — a relationship (e.g. affiliation parent) carrying a `[from, to)` validity window, so the graph answers both "current" and "as-of-date" queries.
- **Born-as vs currently-is** — the resolution rule when the cert's stamped attribution and the registry's live attribute disagree: the cert records what was true at issuance/rotation (*born-as*); the registry is authoritative for *currently-is*.

## 2. The two layers, by mutability

The central design fact (your refinement): **identity is persistent; posture/relationships are dynamic.** They have different physics and therefore live in different places.

### 2.1 Persistent spine (cert-anchored, immutable)

| Field | Role(s) it load-bears | Where it lives | Mutability |
|---|---|---|---|
| `uuid` (slot anchor) | write-namespace, cert CN, ACL subject, audit `src_zid` dedup, provenance source-id | cert CN + registry PK | **never** changes / never reused |
| `goatnet` / instance | which fabric this identity belongs to | cert URI-SAN scheme + registry | per-instance, fixed at mint |
| `slot_index` + mint provenance | derivation + onboarding lineage | registry | fixed at mint |
| `cert_fingerprint` | the current leaf cert | cert + registry | **semi-persistent** — rotates on renewal/re-key ([ADR 1044](../adr/1044-credential-rotation-in-place-reissue.md)); the *anchor it certifies* is constant |

Spine = the trust anchor's content. Changing a spine field is either impossible (uuid) or a credential rotation (cert), never a routine edit.

### 2.2 Dynamic attributes (registry-resolved, effective-dated)

| Field | Grammar dimension | Where authoritative | Changes via |
|---|---|---|---|
| `affiliation` (`vendor`/`organic`/`partner`/`civil`) + parent/lateral edges | affiliation | registry (live); cert URI-SAN `aff`/`aff-kind` = born-as stamp | registry edit (reorg/retasking) |
| `nationality` (ISO alpha-3) | **the person, not the affiliation** | the person's own manifest (`identity.nationality`); registry (live) | the person, at intake — corrected only as a field edit |
| `relationship` (`constituent`/`assigned`/`liaison`) | the membership | registry | admin decision at approval |
| `composition` (`national`/`multinational`) | affiliation | registry | admin decision at org registration |
| `comrel` (command relationship) | affiliation-adjacent | registry | registry edit |
| `unit_code` (`sovereign`/`nato-logfas`/`sap` scheme + value) | affiliation | registry (live); manifest seeds current | registry edit (comrel/position change) |
| `role` family | role | Ory `groups` / registry | registry / IdP edit |
| `echelon` | echelon | registry | registry edit (re-org/promotion) |
| `site` | site | registry | registry edit (relocation) |
| `node_class` / `autonomy` | node-class | registry (mostly fixed, but allowed to change) | registry edit |

Dynamic attributes change **without touching the cert.** A reorg is a registry edit, not a re-mint — so it never weakens or re-keys anyone's identity.

**`nationality` is the one dynamic attribute that is not about the organisation.** Every other row above answers *where does this principal sit in the structure*, and a reorg changes it. Nationality answers *who is this person*, is true regardless of who employs them, and does not change when the org does — so a descriptor that carried only the affiliation's nation could not distinguish a Canadian officer at a US command from an American one at the same desk. It is effective-dated like the rest (citizenship can change) but it is sourced from the person, never from the unit around them. [ADR 1171](../adr/1171-person-nationality-and-org-ownership-are-two-facts.md).

**Relationship edges are the growth path, not more scalar fields.** The downstream questions unit identity will have to answer — who a unit belongs to, who commands it, who holds C2 authority, who owns its data, who provisions/sustains it, who replaces it (RIP-TOA) — are **typed edges** in the reserved `relationships: [{type, target, valid_from?}]` list keyed by affiliation `id` (the id that `unit_code` identifies), **not** sub-fields of `unit_code`. They are effective-dated dynamic attributes (this table's discipline), resolved through the actor-relationship-graph projection. Reserved edge-type catalog + the future-developer rule: identity-grammar.md §3.1–3.2 (ADR 1043 §4). Because this edge set is multi-type + property-bearing + traversal-heavy, the rear-side read-model that aggregates it is now a **first-class property-graph** candidate on workload-shape grounds (actor-relationship-graph.md §0.2, [ADR 1065](../adr/1065-graph-engine-property-graph-read-model.md), engine spike-gated) — a derived projection, never the SoT (the registry stays authoritative) and never in the descriptor's own resolution path (the edge carries its local subgraph + consistency token).

## 3. The decision this forces: cert stamp vs registry truth

Codified today: affiliation rides the **cert URI-SAN** as `aff`/`aff-kind` attribution (identity-grammar.md; CLAUDE.md identity section). If affiliation is dynamic, the URI-SAN value is necessarily a **point-in-time stamp** — what was true at issuance/rotation. Therefore:

1. **Registry is authoritative for current affiliation/comrel.** The cert is authoritative for *born-as* attribution and for the immutable spine.
2. **Resolution rule:** consumers needing the current relationship read the registry; consumers needing tamper-evident attribution-at-issuance read the cert. They can legitimately disagree; the descriptor states which is which per field (`source: cert | registry`, `authority: born-as | currently-is`).
3. **Changing the stamped value is a rotation** ([ADR 1044](../adr/1044-credential-rotation-in-place-reissue.md), in-place re-issue for the same anchor). Changing the live value is a registry edit with zero cert churn.

This keeps the trust model honest: immutable spine in the trust anchor, mutable graph in the resolvable registry.

### Decision (resolves the former open question)

**Keep affiliation in the cert URI-SAN as a frozen born-as attribution, and make the registry authoritative for current affiliation. The two are not expected to agree, and that is the design — not drift.**

- The URI-SAN `aff`/`aff-kind` is a **born-as stamp**: what the peer was affiliated as *at the moment this leaf was issued*. It is immutable and tamper-evident, and its value is precisely that it is *frozen* — it is the cryptographic record of origin, not a live status field.
- The **registry is authoritative for current affiliation/comrel**, resolved live and effective-dated (§4). A reorg / re-task-organization is a **registry edit only — it never touches the cert.**
- The two legitimately diverge between rotations, and consumers are told which to read by the per-field `provenance` (`source: cert|registry`, `authority: born-as|currently-is`). Forensics reads born-as; the live COP reads currently-is.
- You **re-stamp** the born-as value only by **rotation** ([ADR 1044](../adr/1044-credential-rotation-in-place-reissue.md)) — and only when you deliberately want a fresh cryptographic baseline (e.g. a permanent re-assignment worth attesting), never for transient task-org.

Why this and not "cert carries only the immutable spine, all affiliation registry-only": dropping the stamp would forfeit a tamper-evident origin record we already have for free (codified today — affiliation already rides the URI-SAN as *attribution*, and the Zenoh ACL already matches **CN**, not the URI-SAN, so the stamp is never load-bearing for access and costs nothing to keep). The only real risk of keeping it — "the stamp goes stale" — dissolves once the stamp is *defined* as born-as rather than current. Net: keep the forensic anchor, put all liveness in the registry, and reorg stays a zero-cert-churn edit. (Rotation mechanics: credential-rotation.md.)

> **v1.1 refinement (evidence — §12).** The cross-industry review rated this *adapt, not wrong*: SPIFFE keeps the SVID minimal and PMI/attribute-certs warn against any mutable attribute in a long-lived cert, so the stamp must be **narrowed, not load-bearing**. Concretely: (a) the URI-SAN affiliation is **attribution metadata only — never an authorization input** (already true; the ACL matches the CN), and the spec/NOTES must say so in those words; (b) we keep the stamp because it is bounded and free, but **dropping affiliation from the URI-SAN entirely is the explicit v2 option** if any consumer is ever tempted to authorize off it; (c) the strong claim "an SVID may carry *nothing* but an ID" was **refuted** in review — the lesson is *minimize*, not *forbid* — so keeping one frozen attribution field is defensible. The spine UUID/URN stay; no other attribute is ever added to the cert.

## 4. The graph must be temporal

For the **traceability** value (the original motivation), the affiliation/comrel edges cannot be a flat current snapshot. Each edge is **effective-dated** (`[from, to)`), so the same graph answers:

- "Who does this node report to **now**?" — live COP / `goat who` forward query.
- "Who did it report to **on the date of that audit event**?" — forensic, as-of-date traversal against the same edge set.

Transitivity (your "graphed and transitively applicable") falls out of the edge model: "everything under org X," "all partners of coalition Y," "every system custodied by person P" become graph walks. v1 edges = `parent` only (matching the grammar's current scope); lateral/support edges reserved.

### 4.1 Freshness + consistency (v1.1 — the load-bearing adaptation, §12)

The review's sharpest finding: **embedding a mutable graph in a portable document recreates the staleness hazard Google Zanzibar deliberately avoided by *not* shipping a portable graph.** Zanzibar keeps relationship tuples in a consistency-stamped *service* and hands out opaque tokens (zookies) that bind a decision to a snapshot; the canonical "new enemy" bug is an access check evaluated against an ACL snapshot staler than the content it guards → a **wrongful grant**. A cached goat descriptor is stale the instant a reorg edit lands, and a releasability decision walking its edges can use a pre-reorg graph.

The adaptation (does **not** change the architecture — it adds a freshness contract):

- **Snapshot stamp.** The dynamic-attribute block carries `resolved_at` (when this snapshot was read from the registry) and an opaque `registry_snapshot` consistency token (our zookie analog) — so any consumer can tell *how old* the graph it holds is, and the registry can later answer "was this token current at decision time?"
- **Decision rule by altitude.** A *grant/releasability* decision (the new-enemy-sensitive case) MUST be made against either a live registry query or a snapshot no staler than a per-decision `max_staleness`; a *display/forensic* read may use the cached snapshot freely. This mirrors the layered NIST posture (§12): cache is fine for disconnected ops, but high-consequence grants need bounded freshness.
- **Fetch-fresh pointer.** The descriptor carries a `registry_ref` (where to re-resolve), so a holder of a stale copy knows how to refresh — exactly what even W3C kept (a status endpoint) after abandoning OCSP.
- **Denied-edge exception.** At the RF/denied edge (Reticulum bearer) a live query is impossible; there the snapshot stamp + an explicit, policy-governed `max_staleness` is the honest substitute (NIST SP 800-162 blesses governed local caching for exactly this case — §12).

**Decision — what the consistency token concretely is (was §9 open item).** `registry_snapshot` is a **registry-issued, opaque, monotonic snapshot token** (a Zanzibar-zookie analog), minted by the registry on every resolve and advanced on every mutation. It is *not* a wall-clock time and carries no structure a consumer should parse. The decision rule is **single, with two satisfy-paths** so it degrades cleanly to the denied edge:

> A grant/releasability decision MUST run against a snapshot the consumer can show is current — satisfied **either** by a cheap **liveness check** (`registry_ref` returns the current token; equal ⇒ fresh) **or**, when the registry is unreachable, by a **bounded-staleness** check (`now − resolved_at ≤ max_staleness`, a per-decision policy value). If neither can be satisfied, **fail closed** (deny / re-resolve), never grant off an unprovable snapshot. Display/forensic reads skip the check.

So connected consumers get exact freshness (token equality); disconnected/edge consumers get governed bounded-staleness; nobody silently grants off a stale graph. The registry keeps token→snapshot history so it can later answer "was token *zk:N* current at decision time *t*?" for audit. This is the one mechanism, not three competing ones — the earlier "signed-snapshot vs TTL vs live-only" options collapse into "live-check when you can, bounded-staleness when you can't, fail closed otherwise."

### 4.2 Bitemporal edges (v1.1 — valid-time + transaction-time)

v1 edges carry **valid-time** only (`from`/`to_date` = when the relationship was *true*). For honest audit the review flagged the missing **transaction-time** axis — *when we recorded/learned it*. Without it the document can answer "who was the parent then?" but not "who did we *believe* the parent was, with the information we had, at the moment of that past decision?" — and Zanzibar's new-enemy reasoning is precisely about the snapshot actually applied, not the ground truth. v1.1 adds `recorded_at` (and optional `retracted_at`) per edge alongside the valid-time window, making edges bitemporal (cf. SQL:2011 system+application time, XTDB). Forensic queries can then reconstruct the decision-time belief, not just the eventual truth.

## 5. Schema sketch (loosely-but-predictably extensible)

Goal: stable where it load-bears, loose where it must absorb the unknown. Proposed shape (illustrative — finalize in the ADR; likely both a JSON Schema for portability and a protobuf for the wire):

```yaml
# identity-descriptor v1 (illustrative)
schema: "goat.identity.descriptor/v1"      # self-describing version
spine:                                       # immutable, cert-anchored
  uuid: "e7a9c75b532e3d434bb09fefd3ce1ec7"
  goatnet: "zagros"
  urn: "urn:goat:zagros:org=e7a9c75b…"        # the URI-SAN scheme
  slot_index: 14
  cert_fingerprint: "sha256:…"               # semi-persistent (rotates)
attributes:                                  # dynamic, registry-resolved
  resolved_at: "2026-06-19T00:00:00Z"        # v1.1: when this snapshot was read (freshness, §4.1)
  registry_snapshot: "zk:8f2c…"              # v1.1: opaque consistency token (zookie analog)
  registry_ref: "https://portal.../api/identity/<uuid>"  # v1.1: where to re-resolve fresh
  alias: "maintainer-operator"                     # practical name (ADR 1039 system_handle)
  display_name: "the maintainer — Operator"
  grammar:                                   # the five dimensions (ADR 1043)
    role: "OPERATOR"
    affiliation: { kind: "organic", value: "…" }
    echelon: "COMPONENT"
    node_class: "PLATFORM"
    site: "zagros"
  edges:                                     # the bitemporal graph (§4.2)
    - rel: "parent"
      to: "urn:goat:zagros:org=…"
      from: "2026-06-01T00:00:00Z"           # valid-time start (when true)
      to_date: null                          # open = current
      recorded_at: "2026-06-01T00:03:00Z"    # v1.1: transaction-time (when we learned it)
provenance:                                  # per-field source + authority
  affiliation: { source: "registry", authority: "currently-is" }
  urn:         { source: "cert", authority: "born-as" }  # attribution only, never an authz input
extensions:                                  # namespaced escape hatch
  "x-partner-acme/clearance": "…"
# document-level:
sig: "…"                                      # optional detached registry signature over the doc (§5.1; cert covers spine, sig covers attributes)
```

**Extensibility contract:** core spine + `grammar` field names are stable and reserved; anything experimental or consumer-specific goes under `extensions` with a namespace prefix (`x-<owner>/…`). Unknown extension keys are ignored by conformant readers, never fatal (protobuf-style round-trip tolerance — validated in review, §12). This is the "loose but predictable" property — predictable core, loose edges.

**Versioning + negotiation (decided, was §12 item 4).** The `schema` string carries `<name>/v<major>`. Minor revisions are additive only and governed by ignore-unknown (a v1 reader safely consumes a v1.<n> document). A consumer signals its supported major via `GET /api/identity/<uuid>?v=<major>` (an `Accept`-style hint); the registry serves the highest mutually-supported major and labels the document with the exact `schema` it produced. A breaking change bumps the major, ships behind a **deprecation window announced before the first external partner mints against the new major**, and the registry serves both majors through the window. The `schema` field on every document is authoritative — a reader that doesn't recognize the major refuses rather than guesses (fail-closed, no silent misparse).

### 5.1 Document signing (decided, was §12 item 4)

The spine is already verifiable via the X.509 cert. The **dynamic half is not** — a partner who caches the descriptor and consumes it offline has no way to prove the attributes/edges came from the registry and weren't tampered with in transit or at rest. Decision: the descriptor carries an **optional detached signature `sig` over the canonical document, made by the registry's signing key** (a JWS/COSE-style detached signature, not the peer cert — the registry is authoritative for the dynamic half, so it is the right signer). Two independent roots, by design: the **cert** vouches for the spine, the **registry signature** vouches for the attributes. A consumer verifies what it relies on — a spine-only consumer needs only the cert; an attribute/edge consumer verifies `sig` too. `sig` is OPTIONAL on the wire (intra-goatnet, transport mTLS already covers integrity) and RECOMMENDED for any off-goatnet / cached / partner distribution. Canonicalization for signing is the deterministic-encoding question below — JCS for JSON, deterministic CBOR for the RF path.

### 5.2 Wire format (decided, was §12 item 4)

**JSON is the canonical interchange form** — broadest interop, human-debuggable, matches the audience (heterogeneous external platforms) and the demo. For the **constrained RF / denied-edge bearer (Reticulum, [ADR 1050](../adr/1050-reticulum-denied-edge-bridge.md))** define a **CBOR/COSE profile**: a 1:1 transcoding of the same fields (compact, deterministic encoding) with the signature carried as `COSE_Sign1`. Same model, two encodings; a gateway transcodes JSON↔CBOR at the bearer boundary. Not protobuf for the portable artifact (schema-coupled, not self-describing for an external partner) — protobuf stays the *on-fabric Zenoh sample* wire, which is a separate concern from this portable descriptor.

## 6. Cross-technology mapping (the "usable across technologies" claim)

The descriptor is a neutral document; each technology consumes the subset it understands. (Skeleton — fill per integration in the ADR.)

| Technology | Consumes | Maps to |
|---|---|---|
| Zenoh ACL | `spine.uuid` | write-rule subject (`cert_common_names`) + `<uuid>/**` namespace |
| Cert (X.509) | `spine.uuid`, `spine.urn`, born-as `grammar.affiliation` | CN + URI-SAN |
| SPIFFE/SPIRE | `spine.uuid` (= CN) | SVID CN (ADR 0412) — same dual-field shape, different issuer |
| NetBird | identity ↔ peer | mesh peer (affiliation is **never** a NetBird group — codified) |
| Ory (`groups`) | `grammar.role` | groups claim → policy |
| External partner | whole descriptor + `extensions` | their own tooling; graph-traversable |

## 7. What's built vs designed (honesty)

- **Built today:** the UUID tri-binding (ADR 1025), URI-SAN affiliation stamp, slot-alias projection (ADR 1039), `goat who` identity browser (ADR 1047), the declared registry (data-access-cop). The raw material exists.
- **Designed-not-built (this track):** the portable descriptor artifact in the bundle; the versioned cross-tech schema; the effective-dated affiliation graph as a resolvable surface; the per-field source/authority provenance.
- **Decision-gated (ADR):** URI-SAN affiliation stamp vs registry-only (§3 open question).

## 8. Relationship to existing docs

- Field schema: identity-grammar.md / ADR 1043. This doc **packages and ships** that grammar; it does not redefine the dimensions.
- Practical-name projection: [slot-alias-display-layer.md](slot-alias-display-layer.md) / [ADR 1039](../adr/1039-slot-alias-display-layer.md). The descriptor's `alias` is that layer's `system_handle`; the never-in-a-machine-path invariant carries over.
- Source of truth: [data-access-cop.md](data-access-cop.md) declared registry.
- Rotation mechanics for born-as changes: credential-rotation.md / [ADR 1044](../adr/1044-credential-rotation-in-place-reissue.md).
- Emission point: `ops/goat-zenoh-portal/internal/certmint/` (NOTES.md + bundle); registry resolution via the portal API.
- Ecosystem-scale rollup: actor-relationship-graph.md — the rear-side derived read-model that aggregates these per-record edges into whole-ecosystem transitive + as-of-date queries and an ontology anchor (exploration).
- External-facing call-for-input: `docs/rfc/universal-identity.html` — the self-contained RFC write-up of this whole arc (descriptor + decision + value + self-review + demo + graph + scale) with a closing 20-question set inviting industry feedback. Framed as an open proposal, not a ratified standard; the "sound" verdict inside is explicitly our own self-review.

## 9. Open questions

1. ~~URI-SAN affiliation stamp vs registry-only resolution~~ — **resolved (§3 + §12):** keep the URI-SAN stamp as frozen born-as *attribution only, never an authz input*; registry authoritative for current; reorg never touches the cert; dropping it from the URI-SAN is the explicit v2 option.
2. ~~Does the descriptor travel in the bundle only, or live-resolvable~~ — **resolved (§4.1):** both — bundle ships a born-as snapshot with `resolved_at` + `registry_snapshot`; grant decisions require bounded freshness or a live query; `registry_ref` is the fetch-fresh pointer.
3. ~~Valid-time only vs bitemporal~~ — **resolved (§4.2):** add `recorded_at` (transaction-time) so audit can reconstruct decision-time belief.
4. ~~Wire format + document signing~~ — **resolved (§5.1 + §5.2):** JSON canonical + a CBOR/COSE profile for the RF bearer; optional detached registry-signed `sig` over the document (cert vouches for the spine, registry signature for the attributes).
5. ~~Consistency-token concretization~~ — **resolved (§4.1 Decision):** a registry-issued opaque monotonic snapshot token; grants satisfy freshness by live token-equality when connected or bounded-staleness when not, fail-closed otherwise.
6. ~~Version negotiation~~ — **resolved (§5):** `?v=<major>` Accept-style negotiation, additive minors, announced deprecation window, `schema` field authoritative, fail-closed on unknown major.
7. External-publication scope — what's safe to publish as a community standard vs goatnet-internal (roster-sanitization bar applies). *(open)*
8. Edge model v1 = `parent` only; when do lateral/support edges land? *(open)*

## 10. Reference demonstration repo (cross-platform scenario)

The standard's external value is best shown by a **runnable, multi-platform scenario** rather than prose: a sibling repo **`hello-galaxy-universal-id`** (pattern precedent: `goat-qos-demo`, `moon-pod`, `hello-galaxy-zagros-pod`) that demonstrates several heterogeneous "platforms" — standing in for how an arbitrary vendor stack might consume a **generalized, universally-enforced identity standard** — each ingesting the *same* identity descriptor and enforcing access/attribution consistently off it.

Demonstration shape (skeleton — detail in the repo's own README):

- **Multiple platform stand-ins**, each in its native idiom (e.g. a Zenoh consumer, an X.509/SPIFFE-gated service, an external "partner C2" that only has the descriptor + cert), all reading one descriptor.
- **The universal-enforcement claim:** the *same* persistent UUID spine + effective-dated relationship graph drives access and provenance identically across all of them — change the registry relationship once, and every platform's view/authorization updates coherently; the immutable spine never moves.
- **The transitivity payoff:** a graph query ("everything under org X," "as-of-date Y") resolved once and honored across platforms.
- **Honesty-matrix discipline** (per `goat-qos-demo`): clearly label what is live-enforced vs simulated stand-in, so the demo never over-claims.
- **Scrubbed/external-safe** (no us-internal leakage; roster-sanitization bar). ZAGROS-style neutral scenario.

This repo is a **capstone deliverable** — the runnable demo consumes the descriptor + schema + registry resolution, so the running end-to-end version can't precede them. The **scaffold exists** (`hello-galaxy-universal-id`, Hello Galaxy series #3, private): README + the `RAVEN-03` three-platform / one-reorg scenario (`docs/scenario.md`) + the candidate descriptor artifact (`examples/raven-03.descriptor.json`, both affiliation edges effective-dated) + honesty matrix. It is deliberately stood up *early* to pressure-test the schema — building real consumers against the candidate artifact is how §9's open questions get answered. Runnable platform stand-ins land as the schema firms up.

## 11. Track deliverables

- [ ] This design doc → reviewed, status promoted from skeleton.
- [ ] ADR (verify next free number; cited here as 1051) — the standard + the §3 decision.
- [ ] Descriptor schema (JSON Schema + protobuf) under a stable path.
- [ ] Emit descriptor into the onboarding bundle (`certmint`); recipient-facing NOTES narrative tying the UUID to its roles.
- [ ] Registry resolution endpoint + effective-dated edge storage + `goat who`/`access` traversal.
- [ ] Cross-tech mapping validated per integration (§6).
- [x] **Reference demonstration repo** `hello-galaxy-universal-id` (Hello Galaxy #3) — **runnable** (scenario + candidate descriptor + honesty matrix + three platform stand-ins + a staleness/consistency-token demonstration, §10/§12). Co-evolves with the schema.

## 12. Evidence review — cross-industry success/failure (deep-research, 2026-06-19)

A fan-out research pass (23 sources, 22 verified claims, adversarial 2-of-3 voting) judged each v1 choice against the documented track record of comparable standards. **Verdict: the architecture is sound; keep it with four targeted adaptations — not a redesign and not a restart.** The only layer with a genuine case for reconsideration was the embedded edge graph, and even there the fix is "add a consistency token," not "discard."

**Sound (keep as-is), with citations:**

- **Two-layer split (cert-anchored spine vs registry-held attributes)** — the most validated choice; it restates RFC 5755's own rationale for separating long-lived identity from short-lived authority (different lifetimes, different issuing authorities). PMI/attribute-certs *failed* partly by putting mutable authz in a signed cert and reintroducing revocation lists (ACRLs) — so our **registry-not-second-cert** instinct is exactly right.
- **Rotate-not-revoke, no CRL/OCSP** — on the right side of the industry pivot to short-lived credentials (SPIFFE; smallstep "don't revoke, issue short-lived"; RFC 5755/9608 `noRevAvail`; CA/Browser Forum made OCSP optional in 2023; Let's Encrypt ended OCSP Aug 2025; Firefox → CRLite). Short validity is a standards-accepted substitute for revocation.
- **Namespaced `extensions` + ignore-unknown** — protobuf's winning round-trip-tolerance side of history. And not adopting W3C VC/DID/JSON-LD wholesale is corroborated by a neutral body (GS1): ~10 years on, no dominant approach, non-interoperable "flavours," "ubiquitous use may be a decade away."
- **Per-consumer subset reads** — avoids XACML's documented whole-policy-corpus evaluation cost (NIST SP 800-178); matches the NGAC/Zanzibar "evaluate only the relevant portion" model. Matters because coalition/ABAC is the incumbent for our target users.
- **Offline document carrying mutable attributes** — *sound with governance*: NIST SP 800-162 §3.3.1 explicitly endorses local attribute caching for disconnected ops (the "deployed ship" = our denied-edge case), provided freshness is policy-governed and risk-analyzed.

**Adapt (the four changes made in v1.1):**

1. **Edge freshness / consistency token (§4.1)** — the most consequential gap. Embedding a mutable graph in a portable document is the staleness/"new enemy" hazard Zanzibar solved with zookies. Added `resolved_at` + `registry_snapshot` + `registry_ref` and a freshness rule that grant-decisions need bounded staleness or a live query.
2. **Narrow the cert (§3 v1.1 refinement)** — keep the born-as stamp but make it attribution-only-never-authz, in those words, with drop-from-URI-SAN as the v2 option. (The strong "certs must carry nothing" claim was *refuted* in review — minimize, not forbid.)
3. **Bitemporal edges (§4.2)** — add `recorded_at` transaction-time so audit can reconstruct decision-time belief, not just eventual truth.
4. **Document signing + wire format + version negotiation (§5.1, §5.2, §5 versioning)** — **now settled** (2026-06-19, was open pre-mint): optional detached registry-signed `sig` (cert for spine, signature for attributes); JSON canonical + CBOR/COSE profile for the RF bearer; `?v=<major>` negotiation with additive minors + announced deprecation window. The consistency-token mechanism (§4.1 Decision) is likewise concretized.

**Strongest case for a restart, and why we rejected it:** the edge-graph staleness critique is real and is the one place a "ship a service, not a document" (full Zanzibar) redesign could be argued. We reject a restart because (a) it applies to *one layer*, fixable with a consistency token; (b) our target includes the denied edge, where a portable cached document is a *requirement* NIST explicitly blesses and a live-only authorization service would fail; and (c) every other choice is positively validated. Net: targeted adaptations, architecture intact.

Full cited report retained with the research run; load-bearing verdicts rest on primary sources (RFC 5755, ITU-T X.509, NIST SP 800-162/178, the W3C VC + Bitstring Status List Recommendations, the Zanzibar paper, SPIFFE specs).
