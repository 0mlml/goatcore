# Lineage — materialized provenance index, RocksDB → KuzuDB upgrade path

| Field | Value |
|---|---|
| Status | Draft (Phase 2D-late / Phase 3 commitment) |
| ADR anchor | [`../adr/0512-rapid-adaptation-with-ai.md`](../adr/0512-rapid-adaptation-with-ai.md) §"Lineage — materialized index, audit-log-derivable" |
| Companion | [`data-dictionary.md`](data-dictionary.md), [`ai-rapid-adaptation.md`](https://github.com/usareur-af/goatdocs/blob/main/docs/design/ai-rapid-adaptation.md) |
| Implementation tracker | `../project/implementation-plan.md` Block 23 |

## Why this exists

Provenance — "where did this come from, what derived from it" — is a first-class concern for both operator trust (Scout's citation chains) and incident review (audit forensics). Walking the audit log on demand each time a question is asked does not earn live fidelity at fabric scale. The right shape is a **materialized index** built from the audit log, served via O(1)-ish queryables, with the audit log retaining authoritative status. This doc is the design.

## Architecture

```
                   ┌────────────────────────────────────┐
                   │         Zenoh fabric               │
                   │                                    │
agent emissions ──▶│  audit-logger interceptor (0508)   │
                   │            │                       │
                   │            ▼                       │
                   │  ops/audit/* (audit-log topic)     │
                   └────────────┬───────────────────────┘
                                │ subscribe
                                ▼
                   ┌────────────────────────────────────┐
                   │   lineage materializer peer        │
                   │   (Rust, phoenix-native)           │
                   │                                    │
                   │   subscribes audit-log,            │
                   │   updates RocksDB adjacency        │
                   │   (or KuzuDB after upgrade)        │
                   │                                    │
                   │   storage at /opt/docker-data/     │
                   │     lineage/{forward,backward}/    │
                   └────────────┬───────────────────────┘
                                │ exposes
                                ▼
                   ┌────────────────────────────────────┐
                   │   lineage queryable peer           │
                   │   cmd/lineage/forward/<topic>      │
                   │   cmd/lineage/backward/<topic>     │
                   └────────────────────────────────────┘
```

The audit log is **authoritative**. The materialized index is **derived** — if it ever drifts from the audit log, nuke and re-walk. No data-loss risk; only a temporary latency increase during rebuild.

## Phase 2D-late starting backend — RocksDB

Already in the stack via Zenoh's storage backend. Schema:

### Forward index (`forward:<topic>`)

For each topic, a list of emissions whose `sources` array cites that topic:

```
forward:data/track/sensor-01/T1234 →
  [
    {
      output_topic: agents/tipper/anomaly/T1234,
      emission_time: 2026-04-26T14:23:11Z,
      identity:     spiffe://goat/agents/tipper/instance-a,
      digest:       sha256:abc123...,
      agent:        tipper
    },
    {
      output_topic: agents/scout/req-7891/answer,
      emission_time: 2026-04-26T15:01:42Z,
      identity:     ory:operator-jane/req-7891,
      digest:       sha256:def456...,
      agent:        scout
    }
  ]
```

### Backward index (`backward:<topic>`)

For each topic, the citations carried in its emission:

```
backward:agents/tipper/anomaly/T1234 →
  [
    {
      source_topic: data/track/sensor-01/T1234,
      cited_at:     2026-04-26T14:23:11Z,
      digest:       sha256:abc123...,
      sample_count: 1
    },
    {
      source_topic: data/track/sensor-01/T1234?window,
      cited_at:     2026-04-26T14:23:11Z,
      digest:       sha256:def456...,
      sample_count: 1000
    }
  ]
```

### Update path

Materializer subscribes to `ops/audit/**` (the audit-logger output topic). For each audit-log entry that represents an emission with a `sources` array:

1. Read `sources` array from the emission.
2. For each source `{topic, timestamp, digest}`:
   - Append to `forward:<source>` (the source feeds the new emission forward).
   - Append to `backward:<emission-topic>` (the new emission was fed by this source).
3. Write transactionally in RocksDB.

Throughput: bounded by audit-log emission rate; well within RocksDB's typical write bandwidth on the existing storage tier.

### Query path

`cmd/lineage/forward/<topic>?depth=N&since=T&until=T2`:

- Look up `forward:<topic>` → direct citations.
- For depth > 1, recurse: each direct citation's `output_topic` becomes the next-hop key.
- Filter by time window if `since` / `until` provided.
- Return as a graph object: nodes = topics, edges = citations.

`cmd/lineage/backward/<topic>?depth=N&since=T&until=T2`:

- Look up `backward:<topic>` → direct sources.
- For depth > 1, recurse on each source.
- Same filters; same graph-object output shape.

Latency target: **<20ms typical** (1-2 hop queries on warm cache); 10ms aspirational. Direct lookups are O(1) RocksDB get; graph traversal cost dominates only at depth ≥ 3 or fanout > 100.

## The KuzuDB upgrade trigger

Swap the RocksDB backend for **KuzuDB** (embedded property graph, MIT, Cypher-compatible, native Rust bindings) when any of the following fires:

- **Latency.** RocksDB query latency exceeds ~20ms (10ms aspirational) on typical 1-3 hop queries. Caching helps but doesn't substitute for proper graph indexing.
- **Cycle detection.** Tactical fusion loops or self-referencing transformations need cycle awareness. Possible in RocksDB but expensive; KuzuDB native.
- **Shortest-path queries.** "Shortest provenance chain from sensor X to output Y." KuzuDB Cypher; RocksDB requires manual BFS.
- **Attribute-filtered traversals.** "All paths from X to Y where every hop's `confidence` > 0.8 and every emitter identity is in `trusted-set`." KuzuDB native; RocksDB is a custom traversal.
- **Subgraph extraction at scale.** Operator pulls "everything within 3 hops of this anomaly." RocksDB scans become unbounded; KuzuDB optimizes.

When triggered, the upgrade work:

1. Add a KuzuDB embedded-storage backend behind the same `LineageStore` trait the materializer uses against RocksDB.
2. Run both stores in parallel for one validation cycle (audit-log replay against KuzuDB; verify graph queries match RocksDB on overlapping subsets).
3. Switch the queryable peer to KuzuDB; keep RocksDB as fallback for one window.
4. Retire RocksDB lineage path.

The queryable peer's external interface (`cmd/lineage/forward/<topic>`, `cmd/lineage/backward/<topic>`) is unchanged. Consumers (Scout, UI, CLI) see no protocol difference.

### Why KuzuDB and not Neo4j / SurrealDB / Memgraph

- **KuzuDB** — embedded (no separate server), MIT, Rust bindings, columnar property graph, fast for analytical-shape queries. **Default choice.** Same operational shape as RocksDB (single embedded store).
- **SurrealDB** — Rust-native, multi-model, single binary, BSL (acceptable per ADR [`0106-sovereign-tooling-preference.md`](../adr/0106-sovereign-tooling-preference.md)). More capable beyond just lineage, but heavier (separate server). Reserve as alternative if multi-model needs surface beyond lineage.
- **Memgraph** — neo4j-compatible, fast, BSL. Acceptable but separate server; KuzuDB embedded is preferable for our deployment shape.
- **Neo4j** — JVM, community-vs-enterprise licensing complications, heavyweight. Rejected unless an external requirement forces it.

The same materializer pattern applies regardless of backend; only the storage adapter swaps.

## Caching for proc-to-proc reuse

The lineage queryable peer maintains a small in-memory LRU of recent query results. Cache keys: `(query-shape, topic, depth, time-window)`. Invalidation: TTL (30s default) plus subscribe-driven (when a new audit-log entry arrives that touches a cached topic, evict that cache entry).

The cache is opportunistic — never blocks correctness. A miss falls through to the backing store.

## Vector store — explicitly NOT lineage

A vector DB serves a *different* purpose:

- Semantic search ("find topics whose schema descriptions are similar to this question intent").
- Pattern matching ("find historical anomalies whose embedding is close to today's").
- TTP-drift detection.

This is **not** lineage. Vector store is deferred until Scout's planner or a pattern-matching agent needs it. Candidates at that time: **Qdrant** (Rust-native, Apache 2.0, single binary) or **LanceDB** (Rust embedded, Apache 2.0, columnar). Both sovereign-OK and tech-strategy-aligned.

Naming this here so future contributors don't accidentally extend lineage scope to cover semantic search.

## Implementation surface

| Path | Purpose |
|---|---|
| `ops/agents/lineage/` | Rust crate — materializer + queryable peer |
| `ops/agents/lineage/store/rocksdb.rs` | Phase 2D RocksDB backend |
| `ops/agents/lineage/store/kuzudb.rs` | Upgrade-triggered KuzuDB backend |
| `ops/agents/lineage/schema/` | `attest/schema/agents/lineage/**` declarations |
| `ops/ansible/roles/lineage/` | Deploy role |
| `docs/operations/lineage-runbook.md` | Operator runbook (created at deploy) |

## Identity, audit, observability, schema

- **Identity:** lineage materializer + queryable peer carry their own workload identity (Phase 2 Ory; Phase 3+ SPIFFE).
- **Audit:** lineage queries are audited too — each `cmd/lineage/...` invocation records who asked, what they asked, what they got. Lineage of lineage queries; turtles all the way.
- **Observability:** Prometheus on the materializer — backlog (audit-log entries pending materialization), write rate, store size. On the queryable peer — query rate, p50/p99 latency, cache hit rate, depth distribution.
- **Schema declarations (mandatory):** at `attest/schema/agents/lineage/**` and `attest/queryable/agents/lineage`.
- **Lineage of lineage:** the lineage materializer's emissions are themselves cited (the audit-log entries that fed each materialization edge). Self-consistent.
- **Health:** `health/agents/lineage/<instance>`.

## Rebuild from audit log

If the materialized index is corrupt, lost, or drifts from the audit log:

1. Stop the queryable peer (or fail-open with cached responses + warning).
2. Truncate the materializer store.
3. Replay audit log from the integrity tier ([`../adr/0811-audit-log-integrity.md`](../adr/0811-audit-log-integrity.md)) from the earliest retained entry forward. Each replayed emission feeds the materializer same as a live event.
4. When replay catches up, restart the queryable peer.

Estimated time: minutes-to-an-hour for typical audit volume; bounded by audit-log read throughput, not materializer write throughput.

This is the **escape hatch** that justifies treating the materialized index as derived rather than authoritative. Authority lives in the audit log; the index is a fast-query convenience.

## Out of scope

- Cross-site lineage federation (lineage of cross-site emissions where audit logs are partitioned). Phase 3+ once Spock-replicated audit-log integrity is proven.
- Mutable lineage (correcting historical edges). Audit logs are append-only; lineage edges are append-only. A "supersede" relation would be a separate edge type, deferred.
- UI / visualization beyond the queryable peer's graph-object output. UIs (Scout's "show provenance," operator dashboards) consume the queryable; we don't ship a lineage UI as a product.
- Real-time streaming lineage updates pushed to subscribers (vs the current pull-via-queryable). Possible later if a use case warrants.

## Test plan

- **Unit tests.** Materializer correctness (audit-log entry → expected forward + backward writes); queryable correctness (graph traversal at various depths).
- **Smoke test (CI).** Inject synthetic audit entries with known citation chains; query forward + backward; assert correct graph structure.
- **Integration soak (integdev).** Run alongside Tipper + Scout for 24h. Assert query latency stays under 20ms at all observed depths; assert no missed audit entries (replay correctness check).
- **Rebuild test.** Truncate store, replay audit log, assert resulting graph matches original.
- **Upgrade-trigger gate.** When latency exceeds threshold or query patterns force KuzuDB, the trigger fires explicitly per the conditions above.

## References

- ADR [`0512`](../adr/0512-rapid-adaptation-with-ai.md) — anchor decision
- ADR [`0508`](../adr/0508-audit-logger-interceptor.md) — audit-logger source
- ADR [`0811`](../adr/0811-audit-log-integrity.md) — audit-log integrity tier
- ADR [`0502`](../adr/0502-rocksdb-ops-storage.md) — RocksDB Zenoh storage
- [`ai-rapid-adaptation.md`](https://github.com/usareur-af/goatdocs/blob/main/docs/design/ai-rapid-adaptation.md) — bones doc
- [`scout.md`](https://github.com/usareur-af/goatdocs/blob/main/docs/design/scout.md) — primary lineage consumer
- [`data-dictionary.md`](data-dictionary.md) — schema labels for graph nodes
- `tech-strategy.md` — Rust default
- `../project/implementation-plan.md` Block 23
