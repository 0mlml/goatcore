# Replay substrate — operator-facing rewind over wide events + audit + lineage

## Conclusion

A `replay/` queryable peer class that, given `(t_start, t_end,
namespace_filter)`, reconstructs the fabric's state at that interval
from the existing storage layers and serves it as a *replayed* Zenoh
session. Same key expressions, same schemas, same citations, same
lineage edges — emitted on a `replay/<session_id>/<original_keyexpr>`
namespace and tagged with a `replay_session_id` attestation marker
so live consumers cannot confuse replayed traffic with current
traffic.

Operators (and Scout — [`scout.md`](https://github.com/usareur-af/goatdocs/blob/main/docs/design/scout.md)) ask the same questions
of past fabric state as of present. After-action review collapses
from days of hand-stitching across stores to operator-self-service.

This is not a new substrate. It rides three things goat already has:

1. **Wide events on OpenObserve / MinIO** ([`observability-wide-events.md`](observability-wide-events.md), ADR [`0822`](../adr/0822-wide-event-observability-otlp-openobserve.md)) — joinable by `correlation_id`, hot 7d / warm 90d.
2. **Tamper-evident audit log on MinIO with Object Lock** (Block 17D, ADR [`0811`](../adr/0811-audit-log-integrity.md)) — durable, verifiable, year-plus retention.
3. **Materialized lineage adjacency lists** ([`lineage.md`](lineage.md)) — `forward(<topic>)` / `backward(<topic>)` graph queries.

Decision: ADR [`0825`](../adr/0825-replay-substrate.md).
Implementation: `../project/implementation-plan.md` Block 37.

## Why this is the missing layer

Today's after-action review on a goat site, in priority order:

1. Find the affected `correlation_id` from a Prometheus alert or operator report.
2. SQL-query OpenObserve for every wide event in a 30-minute window around the incident.
3. Cross-reference against the audit-integrity store on MinIO for the durable record.
4. Walk the lineage graph by hand to figure out which sources contributed to a derived emission.
5. Re-derive what the operator's UI would have shown at the time of incident, by mentally reconstructing topic state from the wide-event sequence.

Step 5 is where the cost is. Every after-action review re-implements the same reconstruction logic, and the reconstruction is operator-by-operator inconsistent. The proposal: build the reconstruction *once*, expose it as a Zenoh peer, let any operator or Scout invocation drive it via the same query interface they use for live traffic.

The same machinery serves three operationally distinct demands:

| Demand | Today | With replay |
|---|---|---|
| After-action review of a specific incident | Multi-day engineer effort | Operator runs a query at `cmd/replay/start?t_start=...&t_end=...` and inspects the resulting session |
| Training / red-team scenario authoring | Recorded log files cobbled together | Replay an existing real session into a training mesh and let trainees observe |
| Scout reasoning over historical fabric state | Not possible — Scout is live-only | Scout opens a replay session and treats it as a live one for the duration of its query |
| Schema migration testing | Untested against real historical traffic | Replay yesterday's traffic through a new schema-enforcer interceptor before promoting |

## Architecture

### Per-site topology

```
┌─────────────────────────────────────────────────────────────┐
│ Operator / Scout / training tool                            │
│   queries: cmd/replay/start, cmd/replay/list,                │
│            sub: replay/<session_id>/**                       │
└────────────────┬────────────────────────────────────────────┘
                 │ Zenoh
                 ▼
┌─────────────────────────────────────────────────────────────┐
│ goat-replay  (Rust binary, per site, active-passive)         │
│   - session manager (id, ttl, isolation)                    │
│   - reconstruction engine                                   │
│   - publishes on `replay/<session_id>/...`                  │
└────────────────┬────────────────────────────────────────────┘
                 │
       ┌─────────┼──────────┐
       ▼         ▼          ▼
┌──────────┐ ┌────────┐ ┌────────────────────┐
│OpenObserve│ │  MinIO │ │ Lineage adjacency  │
│ (wide ev) │ │ (audit)│ │ (RocksDB)          │
└──────────┘ └────────┘ └────────────────────┘
```

**HA pattern.** Same active-passive behind keepalived VIP that the rest of the mgmt plane uses (ADR [`1010`](../adr/1010-ory-on-spock-mindful-operation.md) D1).

**Site scope.** v1 is per-site only — replay does not federate across sites in the same session. A Phase 3+ extension can stitch cross-site replays via `correlation_id` projection if operational demand surfaces.

### Reconstruction engine

The engine builds an in-memory event timeline by joining:

- OpenObserve query for the time window — gives the per-emission stream including OTLP attributes, schemas, payload-flattened fields.
- Audit-integrity query for the same window — gives the authoritative ledger entries; used for cross-checking and for events outside OpenObserve's retention.
- Lineage adjacency for the topics in the window — gives the derived-from / derived-to graph, replayed as a separate metadata stream on `replay/<session_id>/_meta/lineage`.

For each reconstructed emission, the engine publishes on
`replay/<session_id>/<original_keyexpr>` with attributes preserved
plus three replay-specific markers:

| Marker attribute | Value |
|---|---|
| `replay_session_id` | uuid |
| `replay_original_ts_ntp_ns` | original emission timestamp |
| `replay_emit_ts_ntp_ns` | when the replay peer emitted (for measuring replay latency) |

The replay peer also publishes session-level metadata on
`attest/queryable/replay/<session_id>` so live consumers can discover
active replay sessions.

### Live-vs-replayed isolation

Live consumers MUST NOT receive replayed emissions on the original
key expressions. Two enforcement layers:

1. **Namespace prefix** — replayed emissions ride on `replay/<session_id>/...`, not on the original keyexpr. Subscribing to `**/track/...` does not match `replay/.../track/...` — Zenoh keyexpr semantics are strict here.
2. **Schema-enforcer interceptor extension** — emissions carrying `replay_session_id` attributes must arrive on the `replay/...` prefix or be rejected as malformed. Defense in depth in case a misconfigured tool tries to write to original key expressions with replay markers.

Subscribers that *want* replay (training tools, Scout, after-action UI) explicitly subscribe to `replay/<session_id>/**`.

### Session lifecycle

| Phase | Trigger | Behavior |
|---|---|---|
| Open | `cmd/replay/start` query with `(t_start, t_end, namespace_filter, ttl)` | Replay peer assigns `session_id`, begins reconstruction; emissions begin within ≤2s |
| Streaming | Reconstruction in flight | Replay peer publishes at original timestamps' relative pacing × `playback_rate` (default 1.0; 0 = as-fast-as-possible; >1 = sped up) |
| Paused | `cmd/replay/pause` | Replay peer suspends emission; resumable |
| Closed | TTL expires OR `cmd/replay/stop` | Session torn down; metadata preserved at `attest/queryable/replay/<session_id>` for audit |

Default TTL is 1 hour. Maximum is 24 hours (operator override).
Replay sessions are themselves audit-logged to the audit-integrity
tier — operators reviewing past state cannot do so silently.

### Reconstruction modes

**Mode A — Wide-event-only (default, fast).** Reconstruct from OpenObserve only. Available within 90-day warm tier. Fast (seconds to minutes for a 1-hour window). Sufficient for almost all operator after-action review.

**Mode B — Audit-grounded.** Cross-check against audit-integrity. Slower (minutes for a 1-hour window) because audit storage is append-only / read-amplified. Available for the full audit retention window (1 year hot + 6 year cold per ADR [`0811`](../adr/0811-audit-log-integrity.md)). Required for incident-investigation contexts where evidentiary integrity matters.

**Mode C — Lineage-aware.** Same as A or B, plus the lineage graph is replayed as a parallel metadata stream. Use when the operator's question is *causal* ("what fed this fusion track?"). Adds ~10% reconstruction time.

The mode is part of the `start` query; the replay peer reports the chosen mode back in session metadata so the consumer knows what guarantees apply.

## Composition with other systems

| System | How it composes |
|---|---|
| [`scout.md`](https://github.com/usareur-af/goatdocs/blob/main/docs/design/scout.md) | Scout opens a replay session for time-bounded questions ("what did we believe at 14:32?"). The session_id flows through Scout's citation graph. |
| ADR [`0811`](../adr/0811-audit-log-integrity.md) | Audit storage is the durability backstop. Mode B reads through the integrity ledger; replay sessions themselves are audit-logged. |
| ADR [`0822`](../adr/0822-wide-event-observability-otlp-openobserve.md) | OpenObserve is the primary input; OTLP attribute mapping is preserved in replayed emissions. |
| [`lineage.md`](lineage.md) | Lineage adjacency is the third input; mode C surfaces it during replay. |
| [`mission-partner-sharing.md`](mission-partner-sharing.md) | A replay of `release/<partner>/...` traffic must re-execute the partner's WASM conditioning module on the replayed events — replays do not bypass conditioning. |
| ADR [`0513`](../adr/0513-wasm-conditioning-sandbox.md) | Replay through a partner-release path runs the partner's signed WASM module against replayed events; same fuel/memory/time bounds as live. |
| Block 38 [`continuous-architectural-chaos.md`](continuous-architectural-chaos.md) | Chaos peer can probe invariants against replayed historical state — confirms invariants held in past windows; useful for "did we always meet this?" questions. |
| [`time-quality-attestation.md`](time-quality-attestation.md) | Time-quality envelopes are preserved in replayed emissions; consumers filter on quality the same way live. |

## Storage budget

Replay does **not** persist its own data. The session manager keeps an
in-memory + RocksDB-backed session catalog (≤1 KB per session, capped
at 100 active sessions per peer) and that's it. Reconstruction is
pull-from-substrate, emit-to-Zenoh, no intermediate storage.

Memory pressure during reconstruction is bounded by `playback_rate`:
default 1.0 means the replay peer holds at most one emission's
worth of state in flight at a time. Faster modes (`playback_rate > 1`)
proportionally increase memory pressure; the peer rejects rates that
would exceed a configured per-session ceiling (default 100 MB).

## Phased implementation

| Phase | Scope | Duration |
|---|---|---|
| **37A** | Mode A reconstruction (OpenObserve only), single-namespace filter, default TTL only | ~1 wk |
| **37B** | Replay peer joins fabric (queryable peer + audit-by-construction + data-dictionary entry) | 2 days |
| **37C** | Mode C lineage-aware reconstruction | ~1 wk |
| **37D** | Mode B audit-grounded reconstruction (Object-Lock-aware reads) | ~1 wk |
| **37E** | Pause / resume / variable playback-rate | 3 days |
| **37F** | Scout integration — Scout can request a replay session as part of an answer plan | ~1 wk (after Block 23 Scout MVP lands) |
| **37G** | Cross-site replay (Phase 3+; defer until first operational demand) | TBD |

Each phase is independently shippable. 37A delivers operator value (operators can run mode-A replays) before 37B-C ship Scout / lineage integration.

## What this is not

- **Not a recording mechanism.** The substrate already records (audit + wide events). Replay reconstructs from the substrate; it does not capture anything new.
- **Not a debugger.** Operators do not "step through" emissions in the dev-tools sense. They subscribe to the replayed session and let their existing tools — Scout, dashboards, ATAK clients via federation — render it.
- **Not a time-travel database.** Point-in-time consistency across topics is best-effort within the resolution of the wide-event timestamps. For point-in-time-consistent state of a *specific* row in Spock-replicated tables, use the existing PITR-equivalent path (which the project does not have cross-site by deliberate choice — see ADR [`0803`](../adr/0803-no-cross-site-pitr.md)).
- **Not authoritative for evidentiary purposes alone.** Mode B is required for evidentiary use; mode A is operator-helpful but not court-ready. The operator chooses the mode at `start` time and the session metadata records the choice.
- **Not free of partner-release implications.** Replay through `release/<partner>/...` paths re-runs WASM conditioning. A partner module that returns deny in the present must return deny in replay; modules that have changed since the original emission produce different verdicts. This is intentional: replay reflects today's policy applied to yesterday's traffic.

## Open questions

- **What latency does Mode B impose?** Object-Lock-protected reads against MinIO are slower than OpenObserve queries; the first 37D measurement run will produce the number that drives whether Mode B is interactive or batch.
- **Schema-evolution boundary.** A topic whose schema bumped between original emission and replay needs a deterministic rule. Default proposal: replay emissions carry the original schema version; consumers handle multi-version subscription (already required by `fabric-schema-evolution.md`). Confirm this against a real schema bump in the first Block 37 soak.
- **Cross-replay race conditions.** Two simultaneous replay sessions on overlapping namespaces — both publish on disjoint `replay/<session_id>/...` prefixes, so no traffic collision; resource pressure is the only contention. The 100-session cap is a guardrail.

## Related artefacts

- ADR [`0825`](../adr/0825-replay-substrate.md) — the decision
- ADR [`0822`](../adr/0822-wide-event-observability-otlp-openobserve.md) — wide-event substrate (primary input)
- ADR [`0811`](../adr/0811-audit-log-integrity.md) — audit-integrity tier (durability backstop)
- ADR [`0508`](../adr/0508-audit-logger-interceptor.md) — Zenoh audit-logger
- [`observability-wide-events.md`](observability-wide-events.md) — wide-event design
- [`lineage.md`](lineage.md) — lineage propagation
- [`scout.md`](https://github.com/usareur-af/goatdocs/blob/main/docs/design/scout.md) — Scout agent class (primary consumer)
- [`continuous-architectural-chaos.md`](continuous-architectural-chaos.md) — chaos peer probes can run against replayed state
- [`time-quality-attestation.md`](time-quality-attestation.md) — replayed emissions preserve quality envelopes

---

*See `../adr/GLOSSARY.md` for terms and acronyms.*
