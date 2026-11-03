# ADR 0512 — Rapid adaptation with AI — agent-class peer pattern, queryable-by-default fabric, two proof cases

| Field | Value |
|---|---|
| Status | Proposed |
| Date | 2026-04-26 |
| Repo scope | goat core |
| Supersedes | — |
| Superseded by | — |
| Related | [`0108-scope-discipline-own-the-transport.md`](0108-scope-discipline-own-the-transport.md), [`0208-phoenix-native-peer-discipline.md`](0208-phoenix-native-peer-discipline.md), [`0210-zenoh-universal-substrate.md`](0210-zenoh-universal-substrate.md), [`0411-ory-idp-mvp.md`](0411-ory-idp-mvp.md), `0412-spiffe-spire-workload-id.md`, [`0500-zenoh-data-fabric.md`](0500-zenoh-data-fabric.md), [`0501-rust-interceptors.md`](0501-rust-interceptors.md), [`0505-stateful-consumer-pattern.md`](0505-stateful-consumer-pattern.md), [`0506-fabric-schema-evolution.md`](0506-fabric-schema-evolution.md), [`0507-event-telemetry-schema.md`](0507-event-telemetry-schema.md), [`0508-audit-logger-interceptor.md`](0508-audit-logger-interceptor.md), [`0511-publisher-trust-model.md`](0511-publisher-trust-model.md), [`0805-policy-engine-active-passive.md`](0805-policy-engine-active-passive.md), [`0811-audit-log-integrity.md`](0811-audit-log-integrity.md) |

## TL;DR (for a layperson)

The point of bringing AI into the system is **rapid adaptation** — letting a small team produce trustable, mission-relevant work at the speed of changing missions, threats, and partnerships — not "an AI playground." This ADR commits to two complementary agent shapes on the existing Zenoh fabric: **preplanned specialists** (Tipper, the first) and **human-defined ad-hoc agents** (Scout, the first). The "bones" are a small, durable convention shared by both — agent-class peers, queryable-by-default fabric (our MCP analog), citation discipline, ephemeral-agent lifecycle. The honest framing of the posture: **a minimal semantic layer with a deliberate refusal to own ontology, transformation logic, or governance UI.** Calling this "anti-platform" would overstate the case — mandatory schema declaration plus a publish-time enforcer is, by construction, a platform-shaped commitment at the data-description layer, and adopters who climb onto these conventions take on a real (though scoped) portability cost. What the project does refuse is the org112x / org11x / Foundry shape — central ontology registries, transformation engines as products, governance-UI-as-product, controlled-vocabulary-as-platform-feature. The four-layer table below is the test.

## Maturity caveat

GOAT-Net (the project's externally-facing name for the data backbone goat realizes) is at proof-of-concept stage. We are in uncharted territory — sovereign tactical-edge data backbones at this shape and scope do not have established operational precedent at theater scale. The direction is sound and anchored in mainstream distributed-systems patterns; specific scaling, performance, and operational characteristics will be discovered, not asserted. A viable scaling plan exists (ADR [`0216-goat-net-federation-and-site-planes.md`](0216-goat-net-federation-and-site-planes.md)) and becomes operative *if* GOAT-Net earns it through the early phases. The project commits to the direction, not to specific numbers.

## Context

Vendor agent platforms (Google AgentSpace, AWS Bedrock Agents, similar) converge on a substrate-shape — message bus + identity + tool registry + policy + audit + observability. Two failure modes recur when projects build on those platforms:

1. **AI-playground drift.** Teams ship agents that demo well but don't change the operational tempo. The substrate exists; the value doesn't materialize.
2. **Top-down platform rot.** A "generic agent runtime" gets designed before any agent uses it, accumulates features, and never sheds them — convention shaped by speculation rather than real consumers.

goat needs to avoid both. The goal is **rapid adaptation with AI**, framed in mission terms:

- **A mission shifts.** Now we care about a new threat profile. We need a new always-on watcher peer in days, not weeks.
- **A question changes.** An operator needs "show me anomalous tracks in the last hour with a live cited map" — and a trustable answer in minutes, not an analyst day later.
- **TTPs drift.** Adversary behavior shifts. A new pattern detector ships under the same audit + identity + observability discipline as everything else.
- **A new partner shows up.** Release flow composes from existing primitives without bespoke per-partner integration code (cross-references ADR [`0821-mission-partner-sharing-pattern.md`](0821-mission-partner-sharing-pattern.md)).

What is **already in this repo** the AI-rapid-adaptation capability builds on:

- **Substrate.** Zenoh as universal peer-interaction substrate ([`0210`](0210-zenoh-universal-substrate.md), [`0500`](0500-zenoh-data-fabric.md)).
- **Identity.** Ory MVP for service-account workload identity ([`0411`](0411-ory-idp-mvp.md)); SPIFFE Phase 3+ (`0412`).
- **Discipline.** Phoenix-native peer pattern ([`0208`](0208-phoenix-native-peer-discipline.md)).
- **Audit.** Audit-logger interceptor ([`0508`](0508-audit-logger-interceptor.md)) + audit integrity ([`0811`](0811-audit-log-integrity.md)).
- **Policy.** Per-site policy engine ([`0805`](0805-policy-engine-active-passive.md)).
- **Trust model.** Publisher trust model ([`0511`](0511-publisher-trust-model.md)).
- **Schema evolution.** ADR ([`0506`](0506-fabric-schema-evolution.md)) governs schema versioning.

What is **missing**:

1. A deliberate **agent-class peer pattern** so the fifth agent reuses the convention shaped by the first.
2. A fabric-wide **queryable-by-default** convention so an LLM-backed planner peer can discover what data exists, what shape it has, and what it can ask of any state-holding peer — without bespoke per-peer tool wrappers. The project's analog of Anthropic's Model Context Protocol.
3. A **citation discipline** so AI-derived output is trustable: every answer carries the fabric topics it consumed, with timestamps + payload digests, so an operator can verify the work in seconds rather than trusting prose.
4. An **ephemeral-agent lifecycle** distinct from long-running peer lifetime — request-scoped identity, audit, teardown.
5. A **mandatory data dictionary** so data definitions are not anecdotal / out-of-band — every topic declares its schema (or declares itself binary-opaque) before publishing.
6. A **lineage** facility so provenance ("where did this come from, what derived from it") is queryable in O(1)-ish time, not reconstructed each time.

## Decision

goat ships **rapid adaptation with AI** as a first-class project capability, bottom-up, codified as substrate primitives ("the bones") and demonstrated by two complementary first agents ("the proof"). The posture is explicit: a minimal semantic layer plus a refusal to own ontology / transformation / governance shape — see §"Posture: minimal semantic layer; refusal to own ontology, transformation, or governance shape" below. Each layer is provided by the thinnest discipline that gets the value; the semantic layer is a narrow, named platform commitment; the others are conventions over individually replaceable components.

### The bones — durable substrate primitives

#### 1. Agent-class peers

An agent-class peer is a phoenix-native peer ([`0208`](0208-phoenix-native-peer-discipline.md)) that additionally:

- **Declares consumed topics + emitted topics + commands_issued** in its `services/agents/<name>/<instance>/announce` payload.
- **Emits only to `agents/<name>/...`** (or `agents/<name>/<request-id>/...` for ad-hoc). No agent publishes to `data/`, `release/`, or any other prefix. Cross-namespace publishing is forbidden by convention; the audit-logger / schema-enforcer interceptor enforces this against the announce manifest at publish time.
- **Bounds its action surface declaratively.** Outbound `cmd/` topics appear in `commands_issued`; the policy engine ([`0805`](0805-policy-engine-active-passive.md)) gates each. Publish-only agents are unbounded; an agent that issues `cmd/` requires per-action policy.
- **Carries an offline-CA-issued workload identity** (Ory service-account Phase 2; SPIFFE SVID Phase 3+).
- **Publishes its own health** under `health/agents/<name>/<instance>` and Prometheus metrics.

#### 2. Queryable-by-default fabric (our MCP analog)

**Every peer that holds interesting state exposes a `cmd/<peer>/query` queryable** in a structured, schema-described shape an LLM-backed planner can invoke without bespoke per-peer integration. The queryable's input + output schemas are published under `attest/queryable/<peer>` so a planner agent introspects "what queryables exist, what arguments do they take, what do they return."

The convention is opt-in by peer class (state-holders only); transient passthrough peers don't have to expose. The catalog is fabric-discoverable by walking `attest/queryable/**`, not centrally maintained.

#### 3. Citation discipline

**Every agent emission cites its sources.** The standard agent-emission envelope (extending [`0507`](0507-event-telemetry-schema.md)) carries a `sources` array — fabric topic + timestamp + payload digest for every input that contributed. Zero-citation emissions under `agents/...` are structurally invalid; the audit-logger / schema-enforcer rejects.

#### 4. Ephemeral-agent lifecycle

For ad-hoc agents (Scout-shape):

- Spawned from a request (typically a human question or a triggered upstream event).
- Receives a **request-scoped workload identity** (sub-identity of the requesting human, scoped to the request lifetime).
- Subscribes / queries within the bounds of its identity's authorization.
- Emits under `agents/<name>/<request-id>/...` with full citations.
- **Tears down on completion or timeout.** No accumulated state outside the audit trail. Phoenix-native at request scope, not just deployment scope.

The full transcript (request → plan → sources consumed → output) lives in the audit log under one request-id-scoped key for replay.

#### 5. Data dictionary — mandatory declaration, two-state

Every peer publishing to a data-carrying topic **must declare** that topic before publishing. Two valid states, no third:

- **`registered`** — full machine-introspectable schema at `attest/schema/<topic-pattern>`. JSON-Schema-shaped with project extensions for `unit`, `confidence`, `entity_ref` (pointing into [`../design/entity-vocabulary.md`](https://github.com/usareur-af/goatdocs/blob/main/docs/design/entity-vocabulary.md)), and plain-language `description`.
- **`binary-declared`** — at `attest/schema-declaration/<topic-pattern>`, the publisher declares `schema: binary-opaque` or `schema: proprietary <vendor>:<format>:<version>` with a plain-language description and any out-of-band schema reference. Still a registration; just declares "no machine-introspectable schema is offered."

Publishing without either declaration is **rejected at the audit-logger / schema-enforcer interceptor**. There is no "unregistered" state. Schema *evolution* (versioning, compatibility) remains governed by ADR [`0506`](0506-fabric-schema-evolution.md) — declaration is mandatory; central approval of schema content is *not*.

The data dictionary is the fabric: `attest/schema/**` ∪ `attest/schema-declaration/**` is the dictionary; a small **renderer** (`ops/data-dictionary/`, design at [`../design/data-dictionary.md`](../design/data-dictionary.md)) walks both prefixes and emits Swagger-like browsable HTML + OpenAPI / AsyncAPI exports for offline review. No central registry; no schema-as-a-service.

#### 6. Lineage — materialized index, audit-log-derivable

Provenance is a first-class concern, not a derived afterthought.

- **Audit log = source of truth** (live, append-only, integrity-tiered per [`0811`](0811-audit-log-integrity.md)).
- **Lineage materializer** — small Rust peer subscribing to the audit-logger output, maintaining forward + backward adjacency in **RocksDB** (already in the stack via Zenoh storage backend). Schema: `forward:<topic>` → list of `{output_topic, emission_time, identity, digest, agent}`; `backward:<topic>` → list of `{source_topic, …}`.
- **Lineage queryable peer** — serves `cmd/lineage/forward/<topic>` and `cmd/lineage/backward/<topic>` with depth + time-range parameters, returning a graph object Scout / UI / CLI consume directly. O(1) on direct hops; O(depth × fanout) on traversals; rebuild from audit log on corruption is minutes for typical volume.

**Upgrade trigger.** When RocksDB query latency exceeds ~20ms (10ms aspirational) on typical queries, OR when query patterns require cycle detection / shortest-path / attribute-filtered traversals, swap RocksDB for **KuzuDB** (embedded property graph, MIT, Cypher-compatible, native Rust bindings — same operational shape, real graph queries). Only the materializer's storage backend changes; the queryable peer's external interface is unchanged.

The materialized index is **not authoritative** — the audit log is. If the index goes wrong, nuke and re-walk. Caching for proc-to-proc reuse is acceptable; the index itself stays tight.

Vector store (Qdrant / LanceDB) is a *separate* concern (semantic search, not lineage). Deferred until Scout's planner needs it; named here to prevent scope-creep into lineage.

Full lineage design: [`../design/lineage.md`](../design/lineage.md).

### Posture: minimal semantic layer; refusal to own ontology, transformation, or governance shape

The bones above (queryable-by-default, citation discipline, ephemeral-agent lifecycle, mandatory data-dictionary declaration, schema-enforcer interceptor at the publish path, materialized lineage) constitute a thin **semantic layer**. Calling this "anti-platform" would overstate the case: a layer that mandates schema declaration and rejects undeclared publishes is, by construction, a platform-shaped commitment at the data-description layer. Adopters who climb onto these conventions take on a real portability cost if they later swap fabrics — declarations, citation envelopes, and queryable I/O shapes are portable in spirit but not byte-for-byte portable.

What the project explicitly refuses is the broader platform shape exemplified by commercial data-fabric products — central ontology registries, transformation engines as products, governance-UI-as-product, controlled-vocabulary-as-platform-feature. Each layer below the substrate is provided by the thinnest discipline that gets the value:

| Layer | What we own (and adopters inherit) | What we explicitly refuse |
|---|---|---|
| Semantic | Mandatory schema declaration; `unit` / `confidence` / `description` / `entity_ref`; schema-enforcer interceptor as a publish-time gate. **A narrow platform-shaped commitment, named here so adopters are not surprised by the lock-in cost.** | Central data dictionary as a service; controlled-vocabulary registration; field-meaning-as-platform-property. |
| Ontological | A living glossary in markdown ([`../design/entity-vocabulary.md`](https://github.com/usareur-af/goatdocs/blob/main/docs/design/entity-vocabulary.md)); `entity_ref` fields that point into it. Documents the ontology; doesn't enforce a controlled one. | Controlled ontology; mandatory entity-resolution pipeline; "object types must be registered before use." |
| Transformation | "A transformation is just a peer" (interceptors per [`0501`](0501-rust-interceptors.md), agent-class peers per this ADR, stateful consumers per [`0505`](0505-stateful-consumer-pattern.md)). Substrate provides pub/sub + storage shape; transformations are written in normal code in normal repos. | Transformation engine as a product; pipeline-as-a-service; visual workflow editor; opaque "platform manages your transforms." |
| Governance | Identity ([`0411`](0411-ory-idp-mvp.md), `0412`) + policy engine ([`0805`](0805-policy-engine-active-passive.md)) + audit-logger ([`0508`](0508-audit-logger-interceptor.md)) + citation discipline + lineage queryable. All composable, individually replaceable. | Governance UI as a product; lineage-as-a-service; central RBAC console as the only interface. |

**The honest commitment.** The semantic layer is a deliberate, narrow platform commitment. The ontology / transformation / governance layers are not platforms; they are conventions over individually replaceable components. Adopters get the leverage of the semantic layer; they pay a portability cost if they later swap fabrics. We document this tradeoff explicitly so downstream adopters are not surprised.

**Revisability.** Each piece is recoverable individually — if the queryable-by-default convention is wrong, revise it without rebuilding the substrate. The platform shape we refuse (where the vendor owns *all* layers and exit is structurally impossible) is what would be unrecoverable; the four-layer table above is the test.

### The five-failure-mode discipline

Ad-hoc LLM-driven agents over live data have five tactical-grade failure modes. The bones above plus the agent-design discipline below catch each:

| # | Failure mode | Substrate-side mitigation | Agent-design mitigation (in Scout) |
|---|---|---|---|
| 1 | Unit confusion (m/s vs knots) | `unit` field required in queryable I/O schemas; mandatory data-dictionary declaration | Planner reads units from `attest/schema/**` before composing answer |
| 2 | Entity confusion (sensor / source / emitter) | Cross-cutting entity-vocabulary glossary; `entity_ref` field in schemas | Planner resolves entity references via the glossary before joining |
| 3 | Confidence laundering (sparse data, confident prose) | `confidence` / `sample_count` fields in queryable outputs; citation density requirement | Pre-flight checks: refuse / warn when source data is sparse, stale, or contradictory |
| 4 | Cross-namespace leakage (agent publishes to `data/` or `release/`) | Strict output namespace gating: agents publish only to `agents/<name>/...`; audit-logger enforces against announce manifest | — |
| 5 | Audit opacity (can't reconstruct why the agent said that) | Citation discipline + lineage queryable | Citation always shown alongside prose in human-facing output |

### The proof — two named first cases

#### Proof case 1 — preplanned specialist: Tipper

Small radar-track anomaly tipper. Statistics-only (Welford online mean / stddev), no LLM, no tool-use planning. Subscribes `data/track/**`, flags tracks beyond *K* standard deviations on speed / altitude / trajectory_delta, queues at `agents/tipper/queue/<track-id>`. Publish-only; issues no outbound `cmd/`.

Tipper proves: an always-on specialist agent ships in days; the agent-class peer pattern + citation + queryable-by-default + audit-by-construction work at the long-running-peer scope. Design: [`../design/tipper.md`](https://github.com/usareur-af/goatstuff/blob/main/docs/design/tipper.md).

#### Proof case 2 — human-defined ad-hoc: Scout

Operator asks a natural-language tactical question. Scout spawns request-scoped, uses an LLM-backed planner to translate the question into a structured query plan over the fabric's queryables (per primitive #2), executes against state-holders, assembles a **cited live answer** (table, summary, or live visualization) under `agents/scout/<request-id>/...`, and tears down.

Scout proves: humans get trustable answers in minutes, not analyst-days; ephemeral lifecycle works under audit + identity discipline; queryable-by-default is sufficient for an LLM planner without bespoke per-peer wrappers.

LLM inference is **swappable at the planner-peer boundary** — the convention is the contract. Phase 2D bones can use a deterministic / rule-based planner against a fixture corpus to prove the substrate. Phase 3 can run a quantized local model. Higher-fidelity Phase 3+ can route to vendor LLM endpoints through the data-conditioning checkpoint ([`0806`](0806-egress-proxy-cascade.md)) for unclassified inquiries; sovereignty is preserved by the egress / policy gating, not by the LLM choice.

Design: [`../design/scout.md`](https://github.com/usareur-af/goatdocs/blob/main/docs/design/scout.md).

### New top-level prefix: `agents/`

Add a sixth top-level prefix to [`0500-zenoh-data-fabric.md`](0500-zenoh-data-fabric.md) §"Top-level key-expression prefixes":

| Prefix | Purpose | Example |
|---|---|---|
| `agents/` | Agent-class peer output (anomalies, queues, recommendations, ad-hoc-question answers, on-demand visualizations). All emissions are derived data with full citation arrays. | `agents/tipper/anomaly/<track-id>`, `agents/scout/<request-id>/answer` |

### Companion conventions

- `attest/queryable/<peer>` — published queryable schema (input arguments, output shape, citation behavior). Planners walk this catalog.
- `attest/schema/<topic-pattern>` — published machine-introspectable schemas.
- `attest/schema-declaration/<topic-pattern>` — published binary / proprietary schema declarations.
- `cmd/<peer>/query` — every state-holder's queryable.
- `cmd/lineage/forward/<topic>` and `cmd/lineage/backward/<topic>` — lineage queryable peer.

## Alternatives considered

- **Build a generic agent runtime first.** Top-down rot. Rejected.
- **Pick only one proof case.** Loses half the rapid-adaptation story. Both proofs needed to show the bones hold under both shapes. Rejected.
- **Treat AI as out-of-tree integration concern.** Wrong shape — rapid adaptation is *how the team operates*, not a peripheral feature. Rejected.
- **Ship a vendor agent platform on the secure plane.** Sovereignty + island-mode incompatible. Rejected for secure plane; revisitable for unclass partner agents under `release/<partner>/` with the vendor on the partner's side per ADR [`0821`](0821-mission-partner-sharing-pattern.md).
- **Prescribe a specific LLM model + serving stack today.** Premature lock-in. Convention is the contract; model/serving choice is a separate Phase 3 ADR. Rejected.
- **Skip queryable-by-default; let agents build bespoke per-peer wrappers.** Cruft engine. Rejected.
- **Skip citation discipline; trust LLM prose.** Operationally untenable. Rejected.
- **Allow unregistered topic publication (third schema state).** Rejected this turn — anecdotal definitions are exactly the failure mode mandatory declaration prevents. The `binary-declared` state covers proprietary / opaque cases honestly.
- **Build lineage as on-demand audit-log walks (no materialization).** Doesn't earn live fidelity at fabric scale; query latency unacceptable. Materialized index in RocksDB → KuzuDB upgrade path is the right shape. Rejected as sole solution; the on-demand walk remains the *rebuild* mechanism.
- **Lineage in a heavyweight graph DB from day one (Neo4j).** Premature; RocksDB adjacency lists handle 95% of queries cheaply. KuzuDB upgrade triggered by latency or query-complexity threshold, not by anticipation. Rejected.

## Consequences

**Positive.**

- Rapid adaptation as a project capability, not an AI feature gallery.
- Two proof cases share one substrate convention; the third agent reuses without re-deciding.
- Queryable-by-default + structured catalog + entity vocabulary is the project's MCP analog. LLM-backed planners discover the fabric by walking the catalog.
- Citation discipline makes AI-derived output verifiable.
- Ephemeral lifecycle keeps ad-hoc agents from accumulating cruft.
- Mandatory data-dictionary declaration eliminates anecdotal data definitions; renderer makes the dictionary usable.
- Lineage as materialized index gives O(1)-ish provenance without compromising audit-log authority; KuzuDB upgrade reserved.
- The posture is named, not implicit (the four-layer table). The narrow semantic-layer lock-in is acknowledged in writing rather than papered over with "anti-platform" rhetoric — adopters inherit a real, scoped portability cost in exchange for the leverage.
- LLM model + serving choice decoupled from substrate convention.
- Bones compose with mission partner sharing (ADR [`0821`](0821-mission-partner-sharing-pattern.md)) — partner-released topics under `release/<partner>/` are discoverable by Scout with the same citation discipline.

**Negative.**

- Two proof cases is more scope than one. Mitigation: shared convention work; marginal cost of second proof is ~half the first.
- Mandatory declaration adds publishing friction. Mitigation: `binary-declared` covers honest opaque cases; templates absorb most boilerplate; the friction is the feature.
- Queryable-by-default adds a per-peer obligation. Mitigation: opt-in by peer class; helper crate under `ops/agents/lib/`.
- Citation discipline adds emission overhead (digest, sources array). Mitigation: digests are cheap; bounded volume increase.
- Lineage materializer is one more peer to operate. Mitigation: pattern mirrors existing audit-logger.
- LLM-model deferral means Phase 2D Scout uses a deterministic / rule-based planner. Mitigation: that's the right sequencing; deterministic planner is itself useful for island-mode operations.

**Out of scope for this ADR.**

- Specific LLM model + serving stack choice (separate Phase 3 ADR).
- Multi-agent orchestration where one agent invokes another's `cmd/` as primary flow.
- Federated agent state across sites.
- Agent versioning + rollout discipline beyond phoenix-native rebuild semantics.
- Adversarial-prompt robustness for the LLM-backed planner.
- Real-time agent-vs-human conflict resolution.
- Vector-store-backed semantic search (deferred until Scout needs it).

## Assessor view (SCA-R)

This decision serves AC-3 (access enforcement), AC-6 (least privilege), AU-2/AU-12 (audit), AU-3 (content of audit records), IA-9 (service identification), and SI-4 (system monitoring). The agent-class peer pattern + audit-logger + policy-engine gating give auditable agent emissions by construction; citation discipline makes AI-derived content verifiable. The mandatory data-dictionary declaration serves CM-2 (baseline configuration) — every topic's expected shape is recorded and discoverable. The lineage queryable supports AU-6 (audit review and analysis) and IR-4 (incident handling) — provenance is queryable in O(1)-ish time, not reconstructed under pressure.

CM-7 (least functionality) is supported by the `agents/` prefix split + publish-only-by-default action surface; outbound `cmd/` requires explicit announce-time declaration and per-action policy. The decentralized `attest/queryable/**` and `attest/schema/**` catalogs avoid SPOFs incompatible with island-mode posture.

The posture is itself an SCA-R-relevant property: substrate primitives are sovereign + auditable + replaceable; the broader platform shape (central ontology / transformation engine / governance-UI-as-product) we refuse would create a vendor dependency outside the accreditation boundary. The narrow platform-shaped commitment at the semantic layer (mandatory schema declaration, schema-enforcer interceptor) is named explicitly in the four-layer table; the lock-in cost is scoped to data-description portability and is governed by the project itself, not by an external vendor.

LLM model + serving stack choice is deliberately out of this ADR. When selected, that returns through its own ADR with concrete sovereignty + accreditation analysis.

## Trigger conditions for revisit

- **First agent that issues outbound `cmd/`.** Action gating against [`0805`](0805-policy-engine-active-passive.md) requires a per-agent decision-review.
- **First conflict between agent-issued and operator-issued action.** Conflict resolution semantics need formalization.
- **LLM model + serving stack selection.** Returns through a separate ADR.
- **Multi-agent orchestration as primary flow.** Formalize agent-to-agent semantics.
- **Federated agent state across sites.** Decide between Spock replication and fabric-native storage.
- **Vendor agent platform becomes accreditation-acceptable on the secure plane.** Revisit whether the sovereign agent-class peer pattern remains the answer.
- **Citation overhead becomes load-bearing on emission latency.** Revisit digest computation strategy.
- **Lineage RocksDB query latency exceeds ~20ms typical** — execute the KuzuDB upgrade.
- **Queryable-by-default catalog growth crosses ~hundreds of entries.** Revisit whether the decentralized walk remains tractable for planner agents.
- **Vector-store-backed semantic search becomes load-bearing for Scout.** Pick Qdrant / LanceDB and add the convention.

---

*See `GLOSSARY.md` for terms and acronyms.*
