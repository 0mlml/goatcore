---
adr: 0837
title: RCA-by-construction substrate — causal-edge first-class + black-box recorder + decision provenance
status: Proposed
date: 2026-05-07
authors:
  - contributor-x
audiences:
  technical: how three composable primitives — causal edges on the wide-event stream, an in-process flight-data-recorder per service, and decision provenance — turn the existing observability stack into a queryable causal DAG with content-addressable evidence at every node
  layperson: why making root-cause analysis fast on day one of an incident requires building diagnostic plumbing into the system on day zero, not after
  scar: how the substrate preserves audit-evidentiary integrity by routing decision provenance through the existing tamper-evident MinIO tier rather than a parallel store
repo_scope: goat core
glossary: docs/adr/GLOSSARY.md
---

# ADR 0837 — RCA-by-construction substrate

## Status

Proposed.

Promotion to Accepted is gated on:

1. The Block 73 triad (sub-blocks 73A + 73B + 73C) ships and clears its respective verdict gates per [`../design/rca-by-construction.md`](../design/rca-by-construction.md) §9.
2. The drill-via-RCA test (an operator picks an arbitrary historical F-NNN incident and reconstructs the RCA from the new substrate alone) returns time-to-understand under 30 minutes on a representative mid-complexity incident.
3. At least one Phase-2 best-practice extension lands and demonstrates measurable RCA-friction reduction (the natural candidate is 73D `correlation_id`-through-Spock — closes a known gap on cross-site investigations).

## Context

After-action review on a goat incident today is engineer-hours of
hand-stitching across stores. The substrate is unusually well
equipped for *some* RCA primitives —
[wide events](../design/observability-wide-events.md) join across
planes by `correlation_id`,
[audit integrity](0811-audit-log-integrity.md) provides a tamper-evident
timeline, [lineage](../design/lineage.md) carries forward / backward
data adjacency, [replay](0825-replay-substrate.md) reconstructs
historical fabric state,
[time-quality attestation](0514-time-quality-attestation.md)
keeps cross-site ordering trustable,
[continuous chaos probing](1013-continuous-architectural-chaos.md)
catches silent drift before it becomes an incident, and the
[F-NNN](1011-findings-register.md) /
[M-NNN](1012-load-testing-discipline.md) registers durably record
"what we ruled out." But the picture has gaps that turn meaningful
RCAs into bespoke archaeology.

The largest gaps:

- **Correlation but no causation.** `correlation_id` joins; it does
  not direct. You can ask "what happened around the same time?"
  but not "what caused this?" Every causal claim is an operator
  inference, not a queryable fact.
- **No black-box recorder.** When a service crashes, its last
  5 minutes of in-memory decisions, queue depth, hot caches, and
  in-flight requests die with it. Telemetry catches what was
  *exported*; the diagnostic gold is what was *internal*.
- **No decision provenance.** Outcomes are durable; the inputs +
  rule set + dep tree + env state at decision time are not. RCAs
  are unreproducible by construction.

The companion design document
[`../design/rca-by-construction.md`](../design/rca-by-construction.md)
catalogs the full set of gaps + extensions; this ADR codifies the
load-bearing substrate decision (the triad).

## Decision

**Three composable primitives become substrate-level invariants of
the goat fabric:**

1. **Causal-edge first-class.** Every emission on a designated
   *causal-chain topic* carries a `caused_by: [<event_id>, ...]`
   field. The schema-enforcer interceptor rejects emissions on
   causal-chain topics that omit it. Schema declarations gain a
   `causal_topic: true` opt-in flag. The fabric becomes a
   queryable directed graph, not a soup of timestamped events.
   No new substrate — `caused_by` is one more key on the OTLP
   record.
2. **Black-box recorder per service.** Every load-bearing service
   maintains an in-process ring buffer of recent decisions +
   high-fidelity telemetry + in-flight transactions. On SIGTERM
   and on crash (panic / segfault / OOM-kill), the buffer is
   flushed durably to `/var/lib/goat/fdr/<service>/<crash_ts>.cbor.gz`
   before exit. Forensic recoverability through 90 days via
   archival to the audit-integrity tier under Object-Lock
   COMPLIANCE.
3. **Decision provenance.** Every load-bearing decision logs
   `(decision_id, service, verb, input_hash, output, code_sha,
   deps_sha, env_fingerprint, decision_path, time_quality,
   caused_by)` to the audit-integrity tier. Decisions are
   reproducible — same inputs + same code-sha → same decision-id,
   except for explicitly-named non-deterministic-input cases
   (RNG, external-API call).

The triad is the load-bearing portion. The full Block 73 cluster
extends with five best-practice extensions (sub-blocks 73D–73H)
and five novel layer-up items (sub-blocks 73I–73M); each composes
with — does not replace — the triad. Sequencing, verification, and
out-of-scope items are in the design doc §8 / §9 / §11.

## Consequences

### Positive

- **Time-to-understand collapses from engineer-hours to minutes**
  on the typical mid-complexity incident, because the diagnostic
  surface exists on day zero rather than being assembled on day
  one of the incident.
- **Cross-plane RCA becomes a single query.** Causal edges +
  wide-event correlation + audit-trusted timeline join into one
  directed-graph traversal.
- **Crash forensics is recoverable.** Black-box recorder
  preserves the diagnostic state that telemetry retention loss
  and segfault would otherwise erase. Aircraft-FDR semantics:
  the box that survives the crash *is* what you wanted.
- **Decisions become reproducible.** Decision provenance lets
  the operator deterministically replay any load-bearing
  decision against the same inputs and code-sha — closes the
  "why was this denied?" / "would the same input deny again?"
  question class.
- **Composes with existing primitives.** Wide events
  ([Block 27](../design/observability-wide-events.md)), audit
  integrity ([ADR 0811](0811-audit-log-integrity.md)), lineage
  ([Block 23](../design/lineage.md)), replay
  ([ADR 0825](0825-replay-substrate.md)), time-quality
  ([ADR 0514](0514-time-quality-attestation.md)), chaos probing
  ([ADR 1013](1013-continuous-architectural-chaos.md)) all keep
  their current roles. The triad enriches; it does not replace.
- **Closes a structural gap in worker-discipline mechanism set.**
  [Block 48I §6.4 hypothesis-first with falsifiability gate](https://github.com/usareur-af/goatdocs/blob/main/docs/design/multi-agent-team-management.md)
  requires workers to predict observable outcomes and check
  them. Today the observable set is only as rich as the operator
  can stitch by hand. RCA-by-construction makes the observable
  set queryable, which strengthens the discipline.
- **Enables AI-augmented RCA without unsafe corner-cutting.**
  [Block 69 goat-sre-agent](0515-sre-agent-scout-class-peer.md)
  consumes this substrate. A causal DAG + reproducible
  decisions + recoverable in-process state is exactly the
  diagnostic input shape an LLM-driven planner needs to reason
  *correctly* rather than narrate plausibly. Substrate decisions
  in Block 73 reduce hallucination surface for downstream agent
  work.

### Negative

- **Engineering cost.** Triad alone is ~6-8 weeks; the full Block
  73 cluster is ~16-24 weeks single-owner / ~8-12 weeks under a
  Block-48 cluster pattern. Mitigated by phased delivery — 73A
  delivers value before the rest ships.
- **Crash-handler complexity in 73B.** Reliable buffer flush on
  unrecoverable crash (segfault, OOM-kill) is the trickiest
  piece. Mitigated by hands-on iteration in 73B.1 against
  deliberately-crashed test services *before* any production peer
  adopts it.
- **Per-decision audit-tier write cost in 73C.** Every
  load-bearing decision adds one audit-integrity write. First
  soak measures latency delta; mitigation if > 50 ms p99 added
  is batching (loses some real-time RCA friendliness) or a
  separate audit-integrity stanza for provenance vs. emission
  audits.
- **Schema-enforcer becomes load-bearing for an additional class
  of correctness.** Today schema-enforcer rejects undeclared
  schemas; tomorrow it also rejects causal-chain emissions
  missing `caused_by`. Schema-enforcer outage class widens.
  Mitigated by [ADR 0317 fail-stale](0317-per-service-failure-mode-declaration.md)
  declaration on schema-enforcer (already in scope).
- **FDR storage budget.** Per-service ring buffer at 30-day local
  + 90-day archive. First soak measures actual size against
  per-site disk. Mitigation if > 5% of per-site disk: shorten
  local retention; shrink decision-buffer default; sample.

### Neutral

- **Causal-chain topic coverage is opt-in.** Phase 1 covers six
  load-bearing services (signal-server, mgmt-host-bootstrap,
  policy-engine, schema-enforcer, snitch fleet-policy,
  goat-sre-agent action submissions). Coverage expands based on
  F-NNN evidence, not exhaustively at first.
- **No backfill for historical incidents.** The triad is forward-only.
  Past F-NNN findings retain their existing reconstruction depth;
  the substrate makes future RCAs better.
- **Replaces no existing artifact.** ADRs and design docs and
  runbooks all keep their current roles. The diagnostic surface
  becomes richer; the documentary surface is unchanged.

## Alternatives considered

### A. Adopt distributed tracing (Jaeger / Tempo / Zipkin) as a parallel substrate

Rejected. OTLP traces already exist on the wide-event stream
([Block 27](../design/observability-wide-events.md)). Adding a
parallel tracing system would duplicate the substrate and split the
operator's mental model across two query surfaces. The decision
makes traces richer (causal edges, decision provenance) within the
existing OTLP plane rather than adding a parallel one.

### B. Build only the black-box recorder

Rejected as the only mechanism. FDR alone solves the
crash-forensics problem but leaves correlation-without-causation
intact, and leaves decisions unreproducible. The leverage is in
the triad, not in any single primitive.

### C. Build only the causal-edge primitive

Rejected. Causal edges without FDR mean the ancestor of the
crashing event is queryable but the *internal state at crash* is
gone. Causal edges without decision provenance mean the operator
sees the parent of a denial event but cannot reproduce the
denial. The three primitives are individually useful and
mutually-amplifying.

### D. Defer until Block 38 chaos probing earns the substrate

Rejected on operational grounds. Chaos probing
([Block 38](../design/continuous-architectural-chaos.md)) catches
silent drift; RCA-by-construction enables understanding when
drift becomes incident. The two are complementary — Block 38
predicts the failures that Block 73 helps diagnose. Building 73
*after* 38 means every incident in the meantime burns
engineer-hours we could have not burned. The marginal cost of
building 73 now is the engineering time; the marginal cost of
waiting is recurring after-action-review hand-stitching across
every Phase 3 incident.

### E. Make every topic a causal-chain topic by default

Rejected. High-rate sensor / video / metric streams should not
carry the `caused_by` overhead — the discipline applies to
event-shaped emissions where directed ancestry is operationally
meaningful. The opt-in flag (`causal_topic: true`) keeps the
discipline targeted; the design doc §10 commits to expanding
coverage based on F-NNN evidence, not by default.

### F. Use kernel ftrace / eBPF as the diagnostic substrate

Rejected as the *primary* substrate. ftrace / eBPF are
indispensable for low-level performance work and live in
Block 18G under Falco; they
operate at the syscall layer. RCA-by-construction operates at
the application-event layer (decisions, emissions, causal
edges). The two compose — eBPF for "what was the kernel doing?",
RCA-by-construction for "what was the application thinking?" —
but they are not interchangeable substrates.

## Implementation tracker

Block 73 in `../project/implementation-plan.md`.
Sub-blocks 73A (causal-edge), 73B (black-box recorder), 73C
(decision provenance) form the triad; 73D-73H best-practice
extensions; 73I-73M layer-up novel items. Sequencing in the
design doc §8.

## Cross-refs

- Design: [`../design/rca-by-construction.md`](../design/rca-by-construction.md)
- ADR [`0822`](0822-wide-event-observability-otlp-openobserve.md) — wide-event substrate (primary input)
- ADR [`0825`](0825-replay-substrate.md) — replay substrate (primary consumer of historical events the new primitives enrich)
- ADR [`0811`](0811-audit-log-integrity.md) — audit-integrity tier (durable home for FDR archive + decision provenance)
- ADR [`0508`](0508-audit-logger-interceptor.md) — Zenoh audit-logger (sender-side analog: audit-logger captures emissions, decision provenance captures *why* they were emitted)
- ADR [`0317`](0317-per-service-failure-mode-declaration.md) — failure-mode declaration (composes with FDR — declared mode tells operator what to expect; FDR shows what actually happened)
- ADR [`0514`](0514-time-quality-attestation.md) — time-quality attestation (decision provenance carries the envelope)
- ADR [`1011`](1011-findings-register.md) — F-NNN register (RCA outputs land here)
- ADR [`1013`](1013-continuous-architectural-chaos.md) — continuous chaos (sub-block 73J auto-promotes RCA outputs into chaos probes)
- ADR [`1014`](1014-codex-pattern-adr-to-enforced-lint.md) — Codex pattern (sub-block 73M extends the pattern from lint claims to behavioral claims)
- ADR [`0515`](0515-sre-agent-scout-class-peer.md) — goat-sre-agent (primary AI consumer of this substrate)
- [`../design/lineage.md`](../design/lineage.md) — lineage substrate (sub-block 73K extends from data to decisions)
- [`../design/multi-agent-team-management.md`](https://github.com/usareur-af/goatdocs/blob/main/docs/design/multi-agent-team-management.md) — worker-discipline mechanism set (this substrate strengthens hypothesis-first / progress-shape spin-detection)

## Glossary pointer

See `GLOSSARY.md` for project acronyms used in this ADR.
