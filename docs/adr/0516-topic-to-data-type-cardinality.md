# ADR 0516 — Topic-to-data-type cardinality: one topic, one type (default), oneof for variants

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-05-18 |
| Repo scope | goat core |
| Supersedes | — |
| Superseded by | — |
| Related | ADR-0506 (fabric-schema-evolution), ADR-0844 (zenoh-deployment-topology-and-acl-v1), ADR-0512 (rapid-adaptation-with-ai), Block 23 (schema-enforcer) |

## TL;DR (for a layperson)

We're recording the cardinality rule between a Zenoh topic and the
shape of data flowing through it: by default, every topic carries
exactly one message type. When a producer genuinely needs to send
several related variants on the same channel (e.g. open / closed /
fault for the same door sensor), it uses a single root message with a
protobuf `oneof` block — the wire still carries one type, but the
variants are explicit in the schema. We deliberately rule out the
"envelope plus opaque payload" pattern where one topic carries
arbitrary types discriminated at runtime. This choice keeps the
schema viewer, the ACL, the audit logger, and Block 23 simple.

## Context

The Block 85 schema-viewer work surfaced an open question that had
never been explicitly decided in goat: can a topic publish multiple
data types? Three coherent postures exist in real-world pub/sub
systems:

- **A. Topic IS the type.** One topic → one message. New type → new
  key. This is what every existing goat surface today assumes
  (`topics.format` is a single string; `topic_schemas.UNIQUE(topic_id,
  version)` allows versions over time but not parallel types).
- **B. Topic is a channel, payload self-describes.** One topic → many
  types, each payload carries a type tag (envelope + payload, or
  protobuf `Any`). This is Kafka-with-Confluent-Schema-Registry shaped.
- **C. Topic carries a oneof root.** One topic → one root protobuf
  message that contains a `oneof variants { … }`. Wire is still one
  type; semantic variants are explicit in the .proto.

Until this ADR, the registry was in posture A by accident — the
`format` field is singular, the schemas table has one row per
(topic, version). The Block 85 schema viewer renders any protobuf
message including `oneof` blocks correctly, so posture C is
structurally supported today; it's just unused.

The trial fabric (`docs/design/nova-goatnet.md`)
is the live demonstrator where this matters. Real partner vendors
landing under Block 84 will follow whatever pattern we set here.

## Decision

**A is the default; C is the polymorphism escape hatch; B is rejected
at the goat-net layer.**

Concretely:

- **A — topic ↔ type.** Every new topic registered through the portal
  is one Zenoh key publishing exactly one protobuf message (or one
  JSON / CBOR / msgpack body whose shape matches one
  documented dictionary). The `topics.format` field
  (`protobuf:<MsgName>` / `json` / `cbor` / …) is the contract. The
  schema viewer renders that one message as the primary. The
  inspector decodes against that one message.

- **C — variants in one root.** When a vendor genuinely has a small
  closed set of related shapes that publishers want to multiplex on
  one key, they declare a single root message in their `.proto` with
  a `oneof variants { … }` block. The topic format stays
  `protobuf:<RootMsgName>`. Consumers `switch (msg.variants_case())`
  on the wire. The schema viewer already renders `oneof` blocks
  correctly per ADR 0506.

- **B — rejected.** Topics that publish unrelated, dynamically-typed
  payloads with a type-tag envelope are not supported on goat-net.
  Vendors that want this pattern must use multiple keys instead. The
  registry's UNIQUE(topic_id, version) on `topic_schemas` enforces
  this structurally; the API surface won't accept multiple parallel
  schemas for one topic at the same version.

**Operator guidance.** When in doubt, prefer A. Adopt C only when
the variants are tightly related (same producer, same lifecycle,
same ACL scope, same audit treatment). If a candidate "variants on
one topic" group fails any of those tests, it's two topics.

## Alternatives considered

- **Posture B (envelope + opaque payload).** Rejected. Carries a
  permanent discrimination tax on every consumer; coarsens the ACL
  (cannot scope writes to a specific variant); confuses the audit
  logger (every event is the same key_expr regardless of what was
  actually published); fights protobuf's strengths.
- **Topic-level `oneof` discovery (auto-detect).** Letting the portal
  inspect uploaded `.fds` for a top-level `oneof` and surface variants
  separately in the UI. Rejected for v1 because the rendering
  ([`schema.html`](../../ops/goat-zenoh-portal/web/static/schema.html))
  already shows oneof blocks as part of the message — no automation
  needed beyond what the viewer does.
- **Multiple parallel schemas per topic in `topic_schemas`.** Drop
  the UNIQUE(topic_id, version) constraint and add a `discriminator`
  column. Rejected — that's structurally posture B; the schema, ACL,
  and audit logger would all need to grow.

## Consequences

**Positive.**

- Schema viewer stays simple — one primary message per topic, version
  picker for evolution, oneof blocks render in-place.
- ACL stays simple — a publish either matches the per-vendor write
  rule or doesn't; no per-variant ACL grammar to invent.
- Audit logger stays simple — one (vendor, key_expr) tuple per event;
  no envelope-decoding step inside the subscriber.
- Block 23 schema-enforcer (when it lands) is enforceable: incoming
  payloads either decode against the topic's declared message or they
  don't. No "decode the envelope, dispatch to the type-specific
  validator" indirection.

**Negative.**

- Vendors with genuinely polymorphic feeds will publish on more keys
  than they would in a "channel + envelope" shape. This is a
  deliberate tradeoff for the simpler ACL + audit surface.
- Migrating from one shape to another (adding a variant)
  necessarily bumps the `oneof` and ships under a new schema version
  (v1 → v2) per ADR 0506. There's no in-place "add a parallel type"
  path.
- We accept that B-shaped systems exist in the world and integration
  partners may emit them. The bridge layer at the partner boundary
  (Block 72 / Block 84) is where envelope-decode lives if it ever
  needs to; goat-internal keys don't carry that shape.

**Residual risk.** A vendor could put a too-permissive `oneof` in their
root message (effectively re-inventing posture B inside posture C).
Mitigation: code review during the partner-peering portal approval
flow (Block 84); the schema viewer rendering the oneof block makes the
permissiveness visible.

## Assessor view (SCA-R)

This decision narrows the data-flow attack surface compared to
B-shaped pub/sub. Per NIST SP 800-53 SI-10 (Information Input
Validation), every topic now has exactly one declared input shape the
goat schema-enforcer (Block 23) can validate against; SC-7 (Boundary
Protection) gets a clean topic-level boundary instead of a
per-payload type discriminator. CMMC L2 AU-2 (Audit Events) is
simpler because every recorded event maps 1:1 to a key + a known
message type. No waiver depends on this ADR. Compensating control for
the residual oneof-as-B risk is the partner-peering portal review
(Block 84).

## Trigger conditions for revisit

- A partner integration arrives whose existing producer cannot be
  decomposed into per-key streams AND cannot use a `oneof` root
  message (e.g. the upstream system emits truly type-erased blobs).
  Revisit whether B is needed at the goat-net bridge layer
  specifically, not goat-net-wide.
- Block 23 schema-enforcer concludes that per-key validation is too
  coarse for the failure modes it sees in production.
- A future "polymorphic catalog stream" use case emerges — e.g. a
  unified moon-pod topic that genuinely needs to carry every vendor's
  shape on one key for cross-vendor consumers.
