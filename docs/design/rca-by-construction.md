# RCA-by-construction — engineering for proactive root-cause analysis

**Status:** Design landed 2026-05-07 (Block 73, ADR [`0837`](../adr/0837-rca-by-construction-substrate.md) Proposed).

This is the anchoring document for Block 73. It does not duplicate
content carried in the ADR (the *decision*) or the implementation plan
(the *work breakdown*). It carries the architecture and the rationale
for *why this shape and not another*.

## 1. Purpose

When something goes wrong in goat — a peer fails to enroll, a service
misbehaves under load, a federation event vanishes mid-flight, a Spock
row converges to the wrong winner — we want the time-to-understand to
be measured in minutes, not engineer-days. Today the substrate is
unusually well-equipped for *some* RCA primitives (audit integrity,
wide-event correlation, lineage, replay) but the picture has gaps
that turn every meaningful RCA into bespoke archaeology. This block
closes those gaps.

The posture is **RCA-by-construction**: the substrate is engineered
so the diagnostic surface exists *before* an incident, not assembled
after one. Same shape as audit-by-construction ([ADR 0811](../adr/0811-audit-log-integrity.md)),
phoenix-native peer discipline ([ADR 0208](../adr/0208-phoenix-native-peer-discipline.md)),
and continuous architectural chaos ([ADR 1013](../adr/1013-continuous-architectural-chaos.md)).
The cost of building these primitives is paid up front against the
cost of every future RCA, in perpetuity.

## 2. What's already in good shape

The substrate already has roughly two-thirds of the foundation. This
section names the load-bearing pieces explicitly so the design picks
up where they leave off rather than re-inventing them.

| Capability | Substrate | Why it matters for RCA |
|---|---|---|
| **Cross-plane joinability** | [Block 27 wide events](observability-wide-events.md) — OTLP → OpenObserve on MinIO; `correlation_id` propagation | One query joins mesh + signal + fabric + audit + identity + NTP + policy. Most orgs never reach this primitive. |
| **Tamper-evident timeline** | [Block 17D](../adr/0811-audit-log-integrity.md) — Object-Lock + integrity ledger + cross-mounted sibling | The reconstructed timeline is *trusted*. Forensic RCA against memory-only logs is unsafe; this isn't. |
| **Replay-as-substrate** | [Block 37](fabric-replay.md) — wide-event-only / audit-grounded / lineage-aware modes | Operator says "show me yesterday at 14:32:15"; rest of the toolchain renders the past identically to live. |
| **Data lineage** | [Block 23 §lineage](lineage.md) — RocksDB adjacency, forward + backward queries | Answers "what fed into this output?" and "what consumed it?" |
| **Time-quality attestation** | [Block 36](time-quality-attestation.md) — `time_quality` envelope on every emission | Cross-site event ordering is trustable. Without this, distributed RCA is a coin toss. |
| **Schema declaration mandatory** | [Block 23](data-dictionary.md) — schema-enforcer interceptor rejects undeclared publishes | Every byte on the fabric is interpretable post-hoc. Not archaeology. |
| **Per-service failure-mode declaration** | [ADR 0317](../adr/0317-per-service-failure-mode-declaration.md) — fail-stale / fail-open / fail-closed / fail-rebuild | Tells the operator *what should have happened* on dependency failure. Removes "is this expected?" from intuition. |
| **Continuous chaos probing** | [Block 38](continuous-architectural-chaos.md) — invariants are runnable assertions; failed probe drafts F-NNN | Catches silent-drift before it becomes an incident. |
| **Findings + measurements registers** | [ADR 1011](../adr/1011-findings-register.md) F-NNN + [ADR 1012](../adr/1012-load-testing-discipline.md) M-NNN | Durable record of "what we ruled out" — the half of RCA most orgs lose. |
| **AAR-derived hardening pattern** | [Block 48N](https://github.com/usareur-af/goatdocs/blob/main/docs/design/multi-agent-team-management.md) — one incident → 5 ADRs codified | The substrate evolves from RCAs. Not a one-off. |

This stack is what makes the rest of the design viable. The new
primitives compose with these; they do not replace them.

## 3. Where the foundation is weak

These are the gaps that turn meaningful RCAs into bespoke work.
Each maps to a sub-block in §6.

| Gap | Why it hurts | Sub-block |
|---|---|---|
| Correlation but no causation. `correlation_id` joins; it doesn't direct. You can ask "what happened around the same time?" but not "what caused this?" | The RCA-defining question is *causal*. Joining without direction means every causal claim is an operator inference, not a queryable fact. | 73A |
| No black-box recorder. When a service crashes, its last 5 min of in-memory decisions, queue depth, hot-cache, in-flight requests die with it. | Telemetry catches what was *exported*. The diagnostic gold is what was *internal*. | 73B |
| No decision provenance. We log outcomes; we don't capture "why this decision, given those inputs and that env state." | Reproducible post-mortem requires (input, output, code-sha, deps-sha, env-sha). Today only the output is durable. | 73C |
| `correlation_id` doesn't traverse Spock. Cross-site causality is best-effort. | Replication strips the chain. RCA across sites becomes "same time, different site" — not "same transaction." | 73D |
| External-API I/O not captured. AWS / Cloudflare / Anthropic / Ory responses are not durable. | Most production RCAs against external systems are unwinnable without their actual response bytes. | 73E |
| No deploy/config-change overlay. "What changed?" is the first RCA question. | Today the answer is `git log` + intuition. Should be a first-class event stream merged with the failure timeline. | 73F |
| No staleness contracts. We probe rate, not presence. | Loud failures fire alerts. Silent regressions ("service X stopped emitting Tuesday") go undetected for days. | 73G |
| Telemetry retention too short for slow-discovery RCA. 7 d hot / 90 d warm. | Discovering a problem 4 months later: evidence is gone. Audit tier holds 1 y but lacks diagnostic detail. | 73H |
| No fleet weirdness map. Threshold-based alerting misses subtle correlated regressions. | "Three services drifted 30% but no single threshold tripped" goes unnoticed until it becomes an incident. | 73I |
| No closed-loop "RCA → permanent regression test". Every RCA produces a one-off finding; structurally same failure can recur. | Block 38 chaos probing is a manual list today. Auto-promoting RCAs into the probe set turns institutional learning into substrate. | 73J |
| Lineage only covers data, not decisions. | "This denial caused this enrollment failure caused this escalation" is not queryable. | 73K |
| No operator-friendly time-travel. Replay (Block 37) is forensic-grade; operators want a queryable peer. | Adoption gap. Unless operators have a one-line query, replay stays a power-user tool. | 73L |
| Architectural claims are not all probes. Block 47 names this for security claims; the rest of the architecture is honor-system. | Every CLAUDE.md / ADR / design-doc claim is a load-bearing assertion. Drift between claim and reality is silent. | 73M |

## 4. The core triad

The leverage is concentrated in three primitives. These are the
load-bearing pieces of the substrate. Everything else in §5 and §6
composes on top.

### 4.1 Causal-edge first-class (sub-block 73A)

**Decision.** Every emission on a designated causal-chain topic
carries a `caused_by: [<event_id>, ...]` field. The schema-enforcer
interceptor rejects emissions on causal-chain topics that omit it.
Event IDs are content-addressable hashes (SHA-256 truncated; same
shape as audit-integrity ledger entries).

**Why this shape.** `correlation_id` answers "what is the same
request?"; `caused_by` answers "what is the parent of this event?"
The former is a *bag*; the latter is a *directed edge*. Stitching
edges into a DAG is the difference between "show me everything
around 14:32:15" and "show me the causal subtree rooted at this
incident." The fabric becomes a queryable causal graph, not a soup
of timestamped events.

**Who carries the discipline.** Schema-enforcer ([Block 23](data-dictionary.md))
already enforces schema declarations at publish-time. Adding a
required `caused_by` on causal-chain topics is one more clause.
Today's data-dictionary discipline carries the structural cost; the
new primitive piggybacks.

**What is *not* a causal-chain topic.** High-rate sensor / video /
metric streams stay flat (no `caused_by`). The discipline applies to
**event-shaped emissions** — peer enrollments, policy decisions,
cert issuances, replication operations, cross-site federations,
operator commands, agent actions, alert firings — where a directed
ancestry is operationally meaningful. The schema-declaration carries
a `causal_topic: true` flag that opts in.

**Composition with `correlation_id`.** Both fields coexist.
`correlation_id` keeps its existing role (transaction-scope join
key); `caused_by` adds the directed-edge dimension. A query like
"give me the causal subtree of correlation_id X" composes both.

**Storage.** Causal edges are redundant with wide events
([Block 27](observability-wide-events.md)) — the field is just one
more key on the OTLP record. No new substrate, no new bucket, no
new daemon.

**Query peer.** A `cmd/causal/{ancestors,descendants,subtree}/<event_id>`
queryable peer (Scout-class) traverses the graph by walking the
wide-event index. Native OpenObserve queries answer the simple
case; the queryable peer carries the ergonomic case.

### 4.2 Black-box recorder per service (sub-block 73B)

**Decision.** Every load-bearing service maintains a tight
in-process ring buffer ("FDR" — flight data recorder analogy) of:

- The last N (configurable, default 1000) decision records — full
  input, output, internal state snapshot at decision time.
- The last M (configurable, default 5 minutes) telemetry samples
  at higher fidelity than is practical to export continuously.
- Currently in-flight requests / transactions, with their input
  and intermediate state.

The buffer lives on **tmpfs** during steady-state. On SIGTERM and
on segfault (via crash handler), the buffer is **flushed to durable
disk** at `/var/lib/goat/fdr/<service>/<crash_ts>.cbor.gz` before
exit. On recovery, an operator-facing `goat fdr show <service> <ts>`
CLI loads and renders the recovered buffer.

**Why this shape.** Telemetry catches what was *exported*. The
diagnostic gold is what was *internal*. When mgmt-host-bootstrap
crashes mid-bundle-issuance, the operator wants to see the inputs,
the half-rendered output, and the in-flight assertion that fired —
not just "process exited 1." Aircraft-FDR semantics: the box that
survives the crash *is* what you wanted.

**What's load-bearing.** Crash-time durability. A ring buffer that
disappears on segfault is no better than logs. The substrate must
guarantee the flush survives panic.

**Implementation per language.**

- **Go.** A small library (`ops/fdr/go/`) wraps a goroutine-safe
  ring buffer; a `runtime.SetFinalizer` hook + `signal.Notify` on
  SIGTERM + `runtime/debug.SetPanicOnFault(true)` plus a
  `sync.Pool`-backed deferred writer drain the buffer on either
  SIGTERM or panic. For unrecoverable crashes (segfault, OOM
  kill), a sidecar inotify watcher on `/proc/<pid>/status` + a
  pre-allocated lock-free CBOR snapshot held in `/dev/shm/`
  carries the buffer to disk via the watcher when the process
  vanishes.
- **Rust.** Same shape; `crossbeam-queue::ArrayQueue` ring buffer +
  `signal-hook` for SIGTERM + `std::panic::set_hook` for panic +
  `nix::sys::signal` for ABRT/SEGV. tokio-uring services use a
  dedicated single-threaded reactor for the flush path so a hung
  main reactor doesn't lose the buffer.

The crash-handler path is the trickiest piece; the design budget
includes hands-on iteration in 73B.1 against deliberately-crashed
test services before any production peer adopts it.

**Buffer scope.** Decision records are domain-specific (a policy
engine logs `(input, decision, expiration, role-roster, env-fingerprint)`;
a relay logs `(remote_addr, transport, alpn, time_to_handshake_ms,
decision_path)`). The shared library provides the lifecycle; the
service provides the schema. Schema-enforcer validates at flush time.

**Retention.** FDR snapshots stay 30 days at `/var/lib/goat/fdr/`,
then archive to MinIO under `audit-integrity/fdr/<site>/<service>/`
under Object-Lock COMPLIANCE 90 d (joining the audit retention
profile). Forensic recoverability through 90 d; PII redaction via the
domain-specific schema.

### 4.3 Decision provenance (sub-block 73C)

**Decision.** Every load-bearing decision logs a structured
provenance record:

```
{
  "decision_id": "<sha256-of-canonical-form>",
  "service": "policy-engine",
  "verb": "egress.approve",
  "input_hash": "<sha256-of-canonicalized-input>",
  "output": <decision>,
  "code_sha": "<git-sha-of-running-binary>",
  "deps_sha": "<sha-of-resolved-dep-tree>",
  "env_fingerprint": "<sha-of-relevant-env-vars-and-config>",
  "decision_path": ["<rule-id-1>", "<rule-id-2>", ...],
  "time_quality": <envelope-from-block-36>,
  "caused_by": [<event-ids>]
}
```

**Why this shape.** Reproducibility. Today the operator can read
"policy-engine denied bundle X at 14:32:15" but cannot answer "if I
re-run with the same inputs, do I get the same denial?" — because
the rule set, the dep tree, the env state, and the time-quality
envelope at decision time are all gone. With provenance, the
operator can reconstruct the input, point at the same code-sha, and
deterministically replay the decision. Where reproducibility is
*not* achievable (RNG, external-API call), the record names that
explicitly via a `non_deterministic_inputs: [...]` field.

**Where it lands.** Provenance records ride the audit-integrity
tier ([Block 17D](../adr/0811-audit-log-integrity.md)), not just
telemetry. They are *the* durable record of why a decision was
made; telemetry retention loss must not erase them.

**Scope.** Phase 1 covers six load-bearing services: policy engine,
mgmt-host-bootstrap, cert issuance (`bundle-create` + `cert-sign`),
schema-enforcer denials, snitch fleet-policy admin operations, and
agent action-class submissions ([Block 69 goat-sre-agent](../adr/0515-sre-agent-scout-class-peer.md)).
Phase 2 expands to the long tail.

**Composition with audit-logger Zenoh interceptor.** [ADR 0508](../adr/0508-audit-logger-interceptor.md)
already audits Zenoh emissions. Decision provenance is the
*sender-side* analog — the service records why it published before it
publishes, and the audit-logger records that the publish happened.
The two compose; one does not replace the other.

## 5. Best-practice extensions

These are the mid-leverage moves. Independent of the triad in
mechanics; each closes a known RCA gap.

### 5.1 `correlation_id` propagation through Spock (sub-block 73D)

Today signal-server propagates request IDs end-to-end within a
site. Spock replication strips it. Cross-site RCA falls back to
"same time at the other site," which is not the same as "same
transaction at the other site." Fix: add a `correlation_chain`
column to replicated tables that carry causal substance (Ory
identities, mesh ACL changes, cert issuances, policy decisions,
peer enrollments). Spock copies the column; consumers join on it.

### 5.2 External-API request/response capture (sub-block 73E)

Per-service ring buffer of the last N (request, response, latency,
code-path, http-status) tuples to non-cacheable external endpoints,
mounted at `/var/lib/goat/external-api/<service>/`. PII redaction by
domain (Ory tokens, AWS access-key prefixes, Cloudflare bearer
tokens). Survives crash. Composes with FDR (sub-block 73B) but
distinct retention because of the redaction discipline.

### 5.3 Deploy / config-change as wide events (sub-block 73F)

Every Ansible play, every `terraform apply`, every `work release`,
every `bundle-create` issuance, every Spock subscription create /
drop emits a structured event into the OTLP stream with verb
(`deploy.start`, `deploy.complete`, `config.apply`, `track.release`),
target, code-sha, operator identity, and outcome. RCA queries
auto-overlay change-events on the failure timeline. Closes the
"what changed?" question without `git log` archaeology.

### 5.4 Presence / staleness contracts (sub-block 73G)

Schema declarations gain an optional `expected_emit_interval_seconds`
field. Schema-enforcer (or a sibling watchdog peer) tracks last-seen
per topic + producer-identity tuple; staleness beyond 3× expected
fires an alert and drafts an F-NNN entry. Closes the "service stopped
emitting and nobody noticed" failure mode. Brings forward the parked
concept memory item from 2026-04-26.

### 5.5 Long-tail RCA telemetry tier (sub-block 73H)

The 7-day-hot / 90-day-warm tier on OpenObserve covers fast RCA but
loses the 4-month-stale-discovery class. Adds a third tier:
1-in-100 sampled OTLP archive (with full-fidelity at
correlation-id boundaries that span >1 service) at parquet on
MinIO, 1-year retention, queryable on demand. Cheap;
~5% storage delta over today's MinIO budget per [Block 27](observability-wide-events.md).

## 6. Layer-up novel items

Each composes on top of §4 + §5. Each is independently valuable,
but the architectural elegance is in the composition.

### 6.1 Health envelopes / weirdness scores (sub-block 73I)

Every service emits a `weirdness` envelope alongside its primary
output: a baseline-relative score across its internal metrics
(retry rate, lock-wait time, cache-miss rate, schema-mismatch rate,
external-API latency p95, queue depth). The envelope is computed
locally (each service knows its own normal); the aggregator
([Block 27 OTLP](observability-wide-events.md)) renders a fleet-wide
"weirdness heatmap." Surfaces correlated subtle regressions before
they become incidents — the "three services drifted 30% but no
single threshold tripped" class.

Baselines are EMA-derived from the last 7 d of operation per
service-and-version tuple. A new code-sha gets a 24 h baseline-warmup
window where weirdness is suppressed. The envelope is honest about
its own confidence: `weirdness: { score: 0.42, confidence: 0.91, baseline_age: "5d" }`.

### 6.2 Replay-as-regression-test (sub-block 73J)

Every closed RCA (R-NNN entry — see [Block 73 register design](#)) produces
a [Block 38 chaos probe](continuous-architectural-chaos.md)
automatically. The probe replays the failure trigger via [Block 37
replay](fabric-replay.md) and asserts the post-condition the RCA
identified. A failure pattern that has *ever* been understood
becomes structurally incapable of silently recurring. Closes the
loop between RCA and chaos: the RCA register and the chaos register
share a feedback edge.

The auto-promotion is not free of friction — a generated probe needs
a human review pass before joining the probe set, because RCA
context can encode unsafe-to-replay actions. The discipline is:
RCA → drafted probe → reviewer accepts / modifies / rejects →
accepted probes join the chaos set. Mirrors the F-NNN / ADR
authoring discipline.

### 6.3 Decision lineage extending data lineage (sub-block 73K)

[Block 23 lineage](lineage.md) tracks data → derivation
adjacency. Extended: decision → consequence adjacency. "This policy
denial at T+0 caused this enrollment failure at T+30s caused this
operator escalation at T+45s." Stored in the same RocksDB lineage
substrate; queried via the same forward / backward queryable.
Composes natively with `caused_by` (sub-block 73A) — the causal
DAG is the lineage of decisions.

### 6.4 Time-travel peer (sub-block 73L)

A queryable Scout-class peer (`cmd/timetravel/<service>/<window>`)
that reconstructs a window of any service's state from wide events
+ audit + lineage + black-box recorder. Operator says "show me
mgmt-host-A at 14:32:15 yesterday," gets a coherent reconstructed
view. Wraps the existing replay substrate in an operator-friendly
query surface. The rationale: replay (Block 37) is forensic-grade,
which is the right shape for evidentiary use — but operators want
the same capability with one-line ergonomics. The time-travel peer
is the ergonomic layer; it depends on but does not replace replay.

### 6.5 Probe-as-claim generalization (sub-block 73M)

Block 47 continuous compliance probing
makes every claim in the security comparative-posture artifact a
runnable probe. Generalize: every CLAUDE.md / ADR / design-doc
claim of the form "the system does X" gets a machine-checkable
probe. The whole architectural assertion surface becomes RCA-ready
by construction; when something breaks, you immediately know which
claim is now false.

The discipline is mechanical: each ADR has a `probe:` field in its
front-matter pointing at the probe binary or `manual` / `n/a`.
Mirrors [ADR 1014 Codex pattern](../adr/1014-codex-pattern-adr-to-enforced-lint.md)
which already does this for the lint subset of claims. Block 73M
extends the same shape to behavioral claims that lint cannot check.

## 7. The composition picture

The fabric, after Block 73 lands, has this shape:

> **A queryable causal DAG with content-addressable evidence at every node.**

- **Wide events** (Block 27) provide the join layer.
- **Audit integrity** (Block 17D) provides the trust layer.
- **Lineage** (Block 23 + sub-block 73K extension) provides the
  directed-edges layer for both data and decisions.
- **Black-box recorder** (sub-block 73B) provides the survives-the-crash layer.
- **Decision provenance** (sub-block 73C) provides the reproducibility layer.
- **Replay** (Block 37) provides the time-axis layer.
- **Weirdness envelopes** (sub-block 73I) provide the early-warning layer.
- **Probes-as-claims** (sub-block 73M) keep the claim surface honest against
  the architecture.
- **Causal-edges** (sub-block 73A) are the unifying primitive — the directed
  edge that ties wide events into a graph.
- **Time-travel peer** (sub-block 73L) is the operator-facing query surface.

None of this is new substrate. It's incremental on Block 17D / 23 /
27 / 36 / 37 / 38 / 47 — substrate that is already designed,
substantially landed, or imminently landing. The leverage is in
**causal edges + black-box recorder + decision provenance**, which
unlocks the rest.

## 8. Sequencing

The triad lands first. Best-practice extensions and novel layer-ups
parallelize once the triad's primitives are available.

```
Phase 1 — Triad (the load-bearing primitives, ~6-8 wks)
  73A  causal-edge schema + enforcer + first producer + query peer
  73B  black-box recorder library + first integration + crash-handler validation
  73C  decision provenance schema + first integration + audit-tier wiring

Phase 2 — Best-practice extensions (parallel, ~4-6 wks)
  73D  correlation_id through Spock
  73E  external-API I/O capture
  73F  deploy/config-change as wide events
  73G  presence/staleness contracts
  73H  long-tail RCA telemetry tier

Phase 3 — Layer-up novel items (parallel, ~6-10 wks)
  73I  weirdness envelopes (depends on 73B for baseline storage)
  73J  replay-as-regression-test (depends on Block 38 + Block 37 + 73A)
  73K  decision lineage (depends on 73C + Block 23)
  73L  time-travel peer (depends on Block 37 + 73K)
  73M  probe-as-claim generalization (depends on Block 47 substrate)
```

Total elapsed across phases: ~16-24 wks if a single owner; ~8-12
wks with parallelization across 2-3 workers under a Block-48
cluster pattern. Block 73 is a candidate for cluster ownership once
the [Block 48 substrate](https://github.com/usareur-af/goatdocs/blob/main/docs/design/multi-agent-team-management.md) ships.

## 9. Verification

This is the section that prevents Block 73 from becoming a
diagnostic-theater paper exercise. Each phase has a verdict gate
that is *not* "the code compiles."

**73A verdict gate.** A simulated five-event causal chain
(`event_A causes event_B causes event_C` etc.) is published; the
causal-DAG query peer correctly returns the subtree rooted at
`event_A`. Schema-enforcer correctly rejects an emission on a
causal-chain topic that omits `caused_by`. One real production
producer (signal-server is the first candidate, since it is already
the correlation-id source) is wired and emitting causal edges into
the OTLP stream.

**73B verdict gate.** A test service is deliberately panic-killed;
the black-box recorder buffer is recovered intact from `/var/lib/goat/fdr/`.
A test service is deliberately segfault-killed; same recovery
verdict. A test service is OOM-killed; same recovery verdict.
First production integration is mgmt-host-bootstrap (its
crash-mid-bundle-issuance failure mode is the canonical RCA blind
spot today).

**73C verdict gate.** A synthetic policy-engine decision is logged;
re-running with identical inputs against the same code-sha
produces an identical decision-id. A re-run with a different
input produces a different decision-id. Audit-integrity tier
verifies the provenance record under Object Lock.

**Phase-2 / Phase-3 verdict gates** are sub-block-specific and
named in the Block 73 entry of `implementation-plan.md`.

**The drill-via-RCA test.** Once the triad lands, an operator picks
an arbitrary historical incident from the F-NNN register and
attempts an RCA from the new substrate alone. Time-to-understand
is measured. Result joins `validation-state.md`.
PASS = time-to-understand < 30 minutes for a representative
mid-complexity incident; FAIL → identifies the next gap to close.

## 10. Open questions and pilot gates

- **Causal-chain topic coverage.** The §4.1 design opts in via
  `causal_topic: true`. The first soak determines whether the opt-in
  bar is set right. Too narrow → the DAG has gaps. Too broad →
  high-rate topics carry causal overhead. Pilot with the six
  load-bearing services; expand based on F-NNN evidence.
- **FDR storage budget.** 30-day local + 90-day Object-Lock per
  service. First soak measures actual size. Mitigation if > 5% of
  per-site disk: shorten local retention; shrink decision-buffer
  default; sample.
- **Decision-provenance per-decision overhead.** Every
  load-bearing decision adds an audit-integrity write. First soak
  measures latency delta on the policy engine and on
  mgmt-host-bootstrap. Mitigation if > 50 ms p99 added: batch
  writes (loses some real-time RCA friendliness); separate
  audit-integrity stanza for provenance vs. emission audits.
- **Weirdness envelope baselines under deployment churn.** A new
  code-sha gets a 24 h warmup. Frequent deploys could keep the
  baseline permanently warm (low-confidence). Pilot determines if
  the warmup window is right; alternative is per-version baselines
  with explicit transitions.
- **Probe-as-claim discoverability.** Block 73M needs a way to
  *find* every claim in the doc tree. First pass is grep over
  CLAUDE.md / ADRs / design docs for the patterns "the system
  does", "by construction", "guaranteed", "invariant". Pilot
  measures recall.

## 11. Out of scope

- **Distributed tracing as a parallel substrate.** OTLP traces
  already exist as part of [Block 27](observability-wide-events.md);
  Block 73 does not introduce a parallel tracing system. Causal
  edges enrich OTLP; they do not replace it.
- **An RCA register R-NNN.** The RCA register is engineering
  *process* (parallel to F-NNN / M-NNN — outcome surface); Block
  73 is engineering *substrate* (causal edges, FDR, decision
  provenance — diagnostic primitives). The two compose, but the
  register lands separately if and when adopted.
- **AI-driven RCA.** [Block 69 goat-sre-agent](https://github.com/usareur-af/goatdocs/blob/main/docs/design/sre-agent.md)
  consumes the substrate Block 73 builds. Block 73 itself does not
  build the agent.
- **Unique-per-incident playbooks.** Block 73 builds primitives;
  per-incident playbooks live in [`docs/operations/`](../operations/)
  per the existing discipline.
- **Replacing existing observability.** Wide events / Prometheus
  metrics / audit integrity / Falco / osquery all keep their
  current roles. Block 73 *composes* with them.

## 12. Cross-refs

- ADR: [`0837`](../adr/0837-rca-by-construction-substrate.md) — the substrate decision
- Implementation: Block 73 in implementation-plan.md
- Substrate dependencies: [ADR 0811](../adr/0811-audit-log-integrity.md), [ADR 0822](../adr/0822-wide-event-observability-otlp-openobserve.md), [ADR 0825](../adr/0825-replay-substrate.md), [ADR 0317](../adr/0317-per-service-failure-mode-declaration.md), [ADR 0514](../adr/0514-time-quality-attestation.md), [ADR 1011](../adr/1011-findings-register.md), [ADR 1012](../adr/1012-load-testing-discipline.md), [ADR 1013](../adr/1013-continuous-architectural-chaos.md), [ADR 1014](../adr/1014-codex-pattern-adr-to-enforced-lint.md)
- Companion designs: [`observability-wide-events.md`](observability-wide-events.md), [`fabric-replay.md`](fabric-replay.md), [`continuous-architectural-chaos.md`](continuous-architectural-chaos.md), [`data-dictionary.md`](data-dictionary.md), [`lineage.md`](lineage.md), [`time-quality-attestation.md`](time-quality-attestation.md), [`ai-rapid-adaptation.md`](https://github.com/usareur-af/goatdocs/blob/main/docs/design/ai-rapid-adaptation.md)
- Worker discipline that this substrate enables: [`multi-agent-team-management.md`](https://github.com/usareur-af/goatdocs/blob/main/docs/design/multi-agent-team-management.md) §6.4 hypothesis-first / progress-shape spin-detection (RCAs surface the falsifiable predictions worker discipline turns on)

## 13. Glossary

- **Causal edge.** A directed `caused_by` reference from one event
  to its parent event(s). Multiple parents are allowed (e.g., a
  decision caused by both a request and a config-change event).
- **Causal-chain topic.** A schema-declared topic that opts into
  the `caused_by` discipline. Schema-enforcer rejects emissions
  on these topics that omit the field.
- **FDR.** Flight Data Recorder — the in-process ring buffer that
  captures the last N decisions and last M minutes of telemetry,
  durably flushed on crash.
- **Decision provenance.** The structured `(input_hash, output,
  code_sha, deps_sha, env_fingerprint, decision_path, time_quality,
  caused_by)` record that makes a load-bearing decision
  reproducible.
- **Weirdness envelope.** Baseline-relative score across a
  service's internal metrics, attached to each emission, used by
  the aggregator to render fleet-wide regression heatmaps.
- **Time-travel peer.** A Scout-class queryable peer that
  reconstructs any service's state at any past timestamp by
  composing wide events + audit + lineage + black-box recorder.

See `docs/adr/GLOSSARY.md` for project-wide
acronyms.
