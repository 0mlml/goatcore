# Data dictionary — mandatory two-state declaration + Swagger-like rendering

| Field | Value |
|---|---|
| Status | Draft (Phase 2D-late / Phase 3 commitment) |
| ADR anchor | [`../adr/0512-rapid-adaptation-with-ai.md`](../adr/0512-rapid-adaptation-with-ai.md) §"Data dictionary — mandatory declaration, two-state" |
| Companion | [`lineage.md`](lineage.md), [`entity-vocabulary.md`](https://github.com/usareur-af/goatdocs/blob/main/docs/design/entity-vocabulary.md), `fabric-schema-evolution.md` |
| Implementation tracker | `../project/implementation-plan.md` Block 23 |

## Why this exists

Without enforced data definitions, every team that touches the fabric reinvents what fields mean. Out-of-band Word docs and tribal knowledge produce the unit-confusion / entity-confusion failure modes named in [`ai-rapid-adaptation.md`](https://github.com/usareur-af/goatdocs/blob/main/docs/design/ai-rapid-adaptation.md) §"five-failure-mode discipline." The data dictionary closes that gap **without** becoming a Foundry-shape platform — the fabric itself carries the dictionary; a small renderer makes it browsable; enforcement is a publish-path interceptor, not a separate service.

## The two-state model

Every peer publishing to a data-carrying topic **must declare** that topic before publishing. **Two states only**, no third:

### State 1 — `registered`

Full machine-introspectable schema published at `attest/schema/<topic-pattern>`. Schema follows JSON Schema (draft 2020-12) with project extensions:

```yaml
$schema: https://legacy.example/schema/data-dictionary-v1
$id: attest/schema/data/track/<source-id>/<track-id>
description: |
  Radar track sample as emitted by ground-based surveillance radars.
  See entity-vocabulary entry "track" for the entity model.
type: object
required: [track_id, source_id, timestamp, position, velocity]
properties:
  track_id:
    type: string
    description: Unique identifier for this track within the emitting source.
    entity_ref: track
  source_id:
    type: string
    description: Identifier of the radar source emitting this track.
    entity_ref: sensor
  timestamp:
    type: string
    format: date-time
    description: RFC3339 UTC timestamp of the sample.
    freshness_target_seconds: 5
  position:
    type: object
    properties:
      lat:
        type: number
        unit: degrees
        range: {min: -90, max: 90}
      lon:
        type: number
        unit: degrees
        range: {min: -180, max: 180}
      alt_m:
        type: number
        unit: meters
        description: Altitude above mean sea level (MSL).
        range: {min: -500, max: 100000}
  velocity:
    type: object
    properties:
      speed_mps:
        type: number
        unit: m/s
        description: Ground-relative speed.
      heading_deg:
        type: number
        unit: degrees
        range: {min: 0, max: 360}
        description: True heading.
```

Project extensions on top of stock JSON Schema:

- **`unit`** — required on numeric fields. Standard SI where applicable. The renderer + Scout's planner both use this.
- **`entity_ref`** — points at an entry in [`entity-vocabulary.md`](https://github.com/usareur-af/goatdocs/blob/main/docs/design/entity-vocabulary.md). Cross-cuts the schema with the project's entity ontology.
- **`range`** — declared bounds for numeric fields. Used by Scout for sanity-checking and by the schema-enforcer for runtime validation when enabled.
- **`freshness_target_seconds`** — the declared expectation for how recent samples should be. Scout uses this for staleness pre-flight checks.
- **`confidence`** / **`sample_count`** — convention for aggregate fields, surfacing source-data quality to consumers.

### State 2 — `binary-declared`

For data formats that are intrinsically binary, proprietary, or otherwise not machine-introspectable in a JSON-Schema sense. Published at `attest/schema-declaration/<topic-pattern>`:

```yaml
$schema: https://legacy.example/schema/binary-declaration-v1
$id: attest/schema-declaration/data/video/<sensor-id>
declaration_type: binary | proprietary
format:
  vendor: <vendor-name>
  format: <format-name>
  version: <format-version>
description: |
  H.265-encoded video stream from <vendor> sensor model X. Frames are
  GOP-prefixed; metadata sidecar at data/video/<sensor-id>/meta.
out_of_band_schema_ref: |
  Vendor specification document <doc-id> rev <rev>; available at
  <internal location>. Vendors signing the Data Services Provider Charter
  must submit a remediation plan; see remediation_plan_ref below.
encoding:
  mime: video/H265
  byte_order: network
  framing: GOP
remediation_plan_ref: <link-or-id-of-the-remediation-plan>
```

A `binary-declared` entry is itself a registration. It explicitly says "no machine-introspectable schema is offered, here is what you can know about the bytes, here is the plan to migrate to an open standard if the publisher is a vendor."

### What is **not** a state

There is no "unregistered" or "schema-pending" state. A peer that attempts to publish to a topic with no entry under either `attest/schema/...` or `attest/schema-declaration/...` is **rejected** by the schema-enforcer interceptor. This is a runtime gate, not a flag.

## Enforcement — schema-enforcer interceptor

Implemented as a sibling of the audit-logger interceptor (per ADR [`../adr/0501-rust-interceptors.md`](../adr/0501-rust-interceptors.md)), running on the publish path of every Zenoh router in the fabric.

For each publication:

1. Look up the topic in the in-memory cache of `attest/schema/**` ∪ `attest/schema-declaration/**` (kept warm by subscribing to those prefixes at startup, plus a 60s refresh).
2. If a matching declaration exists → allow. Stamp the publication's audit-log entry with `schema_status: registered` or `schema_status: binary-declared`.
3. If no declaration exists → **reject**. Return Zenoh's standard publish-failure response. Increment a Prometheus counter `schema_enforcer_rejections_total{topic, publisher}`.

**Optional content validation** (Phase 3+): if the registered schema has been marked `validate: strict`, the interceptor parses the payload (where introspectable) and validates against the schema. Validation failures reject. Default is `validate: declared-only` — the *existence* of a declaration is enforced; *content* validation is opt-in per topic.

### Rollout curve

- **Phase 2D**: schema-enforcer runs in **audit mode** — no rejections, only audit-log stamping + Prometheus counters. Lets publishers see their compliance status.
- **Phase 2D end / Phase 3 start**: switch to **enforce mode** — rejections enabled. Publishers without declarations stop publishing; the enforcement is the trigger to declare.
- **Phase 3+**: per-topic `validate: strict` opt-in for high-value topics.

This curve keeps the cutover safe — every publisher has a window to declare before runtime rejection lands.

## Schema evolution

Governed by ADR [`../adr/0506-fabric-schema-evolution.md`](../adr/0506-fabric-schema-evolution.md). The data dictionary discipline overlays:

- A schema bump produces a new `attest/schema/<topic>/v<N>` entry.
- The publisher tags emissions with `schema_version`.
- Old + new versions coexist during the rollout window per the schema-evolution ADR.
- Schemas have an explicit `compatibility` field (`backward` / `forward` / `none`) the renderer surfaces.

Central approval of schema *content* is **not** required — schemas are owned by the publishing peer. The data-dictionary discipline mandates *declaration*; ADR 0506 governs *evolution*; nothing centrally approves field-by-field meaning.

## The renderer (Swagger-like)

A small static-site generator that walks `attest/schema/**` ∪ `attest/schema-declaration/**` and produces:

- **Browsable HTML** — one page per topic, search by topic prefix or entity, table of all units, cross-links to entity-vocabulary entries.
- **OpenAPI 3.1 export** — for queryables (which have HTTP-shaped request/response).
- **AsyncAPI 2.x export** — for pub/sub topics.
- **JSON Schema bundle** — single tarball of all registered schemas, signed by the renderer's identity.
- **Per-publisher compliance dashboard** — for each publisher, what topics they emit to, declaration state (`registered` / `binary-declared`), schema version, last update. The "operating-principles compliance" view.

### Implementation surface

| Path | Purpose |
|---|---|
| `ops/data-dictionary/` | Rust crate — renderer (CLI + static-site generator) |
| `ops/data-dictionary/templates/` | Handlebars / Tera templates for HTML output |
| `zenoh-plugins/schema-enforcer/` | Rust interceptor crate |
| `ops/ansible/roles/data-dictionary/` | Renderer deploy role (runs as a periodic job, publishes generated HTML to a dashboard service) |
| `ops/ansible/roles/schema-enforcer/` | Interceptor deploy role |

### Output access

The rendered dictionary is served at `dictionary.netbird.legacy.example` (mesh-only, per the `*.netbird.legacy.example` service convention). Refresh on `attest/schema/**` topic update; full regeneration is seconds.

A CLI (`db schema list`, `db schema show <topic>`, `db schema scaffold <topic>`) gives operators command-line access; `db schema scaffold` produces a starter declaration template a publisher fills in.

## Trial-fabric instantiation (Block 85)

The model above presumes peers `zenoh put attest/schema/<topic>` themselves and a sidecar renderer walks the namespace. The nova trial fabric (Block 82A) ships a deliberately simpler intermediate shape: vendors register through a portal UI rather than publishing schemas directly, and the portal stores the uploaded schema body for anyone to fetch back. Full v1 design — including the five explicit trial-fabric expedients and the long-term evolution path to fabric-native publishing — at [`portal-schema-publishing.md`](portal-schema-publishing.md).

**Components shipped today:**

- **Portal Topics registry** at [`ops/goat-zenoh-portal/`](../../ops/goat-zenoh-portal/) — per-vendor topic registration with key pattern, format (`json` / `cbor` / `msgpack` / `text` / `raw` / `protobuf:<MsgName>`), `schema_ref` pointer field, description, bulk YAML/JSON import/export. Cross-vendor browseable at `portal.nova.netbird.nova.example/topics.html`. SQLite-backed; idempotent migration; per-vendor cert mint + Zenoh write capability via offline-CA URI-SAN already wired.

**Gap closed by Block 85 (in-flight → block-85):**

- Today the portal stores `schema_ref` as a string but nobody publishes the schema body. The pointer points nowhere. The schema-enforcer interceptor described in §"Enforcement" above remains backlogged proper (Block 23), so there is no runtime rejection on the trial fabric — but consumers also have no way to *fetch* a schema even if a producer were disciplined enough to declare one.
- Block 85 v1 adds: (a) schema-body upload on the Topics page, (b) portal-side storage of the uploaded file on local disk, (c) a fetch API at `GET /api/schemas/<vendor>/<system>/.../v<N>` so the inspector and other consumers can read the descriptor back. Inspector flips from "raw hex" to real decode for `protobuf:<MsgName>` topics once the fetch wiring lands. All five v1 decisions (storage location, signing identity, versioning UI shape, fetch path, format starting set) are explicit trial-fabric expedients — see [`portal-schema-publishing.md`](portal-schema-publishing.md) §"The five decisions" for what each one is and §"Long-term evolution" for where each one goes in the long-term goat-net portal.

**What stays canonical across both eras:** the on-wire namespace (`attest/schema/<topic-pattern>` and `attest/schema-declaration/<topic-pattern>`), the versioning semantics ([ADR 0506](../adr/0506-fabric-schema-evolution.md)), the schema-enforcer runtime gate (Block 23 proper). The v1 portal-local files migrate to Zenoh by a one-time replay when the long-term portal lands; the registry `schema_ref` column keeps the same pointer string; the only thing that changes is what answers when something dereferences the pointer.

## What this is NOT (anti-platform reminder)

- **Not a central data dictionary as a service.** The fabric is the dictionary. The renderer is read-only output.
- **Not a controlled vocabulary.** Field names + descriptions are owned by the publishing peer. Cross-cutting consistency is encouraged via [`entity-vocabulary.md`](https://github.com/usareur-af/goatdocs/blob/main/docs/design/entity-vocabulary.md) but not enforced platform-side.
- **Not a schema registry that owns versioning.** Versioning is fabric-native per ADR 0506.
- **Not a governance tool.** Governance is identity + policy engine + audit-logger + citation discipline + lineage. The data dictionary is *one input* to those disciplines.

## Performance + storage

- Schema declarations are tiny — typical `attest/schema/<topic>` is <10 KB. Cumulative dictionary at hundreds of topics is a few MB total.
- Schema-enforcer cache is in-memory; lookup is O(1) hash.
- Renderer regeneration is bounded by topic count; under 10s for thousands of topics.
- No additional storage burden on Zenoh — schemas use the existing `attest/` storage.

## Test plan

- **Unit tests.** Schema-enforcer correctness (allow / reject / stamp); renderer output validity (HTML parses, OpenAPI validates, AsyncAPI validates).
- **Smoke test (CI).** Publish without declaration → rejected. Publish with declaration → allowed. Renderer produces non-empty output for known fixtures.
- **Integration soak (integdev).** All running peers (Tipper, Scout, audit-logger, etc.) declare schemas; schema-enforcer in enforce mode runs 24h with no false rejections; renderer generates dashboard with per-publisher compliance.

## Trigger conditions for revisit

- **Schema declaration friction blocks normal work.** If publishers consistently struggle to write declarations, revisit the meta-schema or the `db schema scaffold` template.
- **Renderer output insufficient for an audience.** If sponsors / partners / vendors need a different view, add an export format; do not move toward a registry-as-a-service.
- **Schema content validation becomes load-bearing.** Phase 3+ may default `validate: strict` for high-value topics; revisit when triggered.
- **Federation across sites surfaces declaration drift.** If the same topic-pattern has different declarations across sites under partition, revisit reconciliation discipline.

## References

- ADR [`0512`](../adr/0512-rapid-adaptation-with-ai.md) — anchor decision
- ADR [`0506`](../adr/0506-fabric-schema-evolution.md) — schema evolution governance
- ADR [`0501`](../adr/0501-rust-interceptors.md) — interceptor pattern
- ADR [`0508`](../adr/0508-audit-logger-interceptor.md) — companion interceptor
- [`ai-rapid-adaptation.md`](https://github.com/usareur-af/goatdocs/blob/main/docs/design/ai-rapid-adaptation.md) — bones + five-failure-mode discipline
- [`entity-vocabulary.md`](https://github.com/usareur-af/goatdocs/blob/main/docs/design/entity-vocabulary.md) — referenced by `entity_ref`
- [`lineage.md`](lineage.md) — uses schema declarations to label nodes
- [`scout.md`](https://github.com/usareur-af/goatdocs/blob/main/docs/design/scout.md) — Scout reads units / freshness / entity refs from declarations
- [`tipper.md`](https://github.com/usareur-af/goatstuff/blob/main/docs/design/tipper.md) — Tipper publishes its emission schemas
- [`../vendor-templates/data-services-provider-charter.md`](https://github.com/usareur-af/goatdocs/blob/main/docs/vendor-templates/data-services-provider-charter.md) — vendor schema obligations
- [`../partner-templates/data-sharing-charter.md`](https://github.com/usareur-af/goatdocs/blob/main/docs/partner-templates/data-sharing-charter.md) — partner schema obligations
- `../project/implementation-plan.md` Block 23
