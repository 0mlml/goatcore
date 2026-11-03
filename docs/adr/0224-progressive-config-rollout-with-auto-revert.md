# ADR 0224 — Progressive configuration rollout with automatic revert

| Field | Value |
|---|---|
| Status | Proposed |
| Date | 2026-05-04 |
| Repo scope | goat core (informs every consumer-repo whose services participate as consumers under this pattern) |
| Supersedes | — |
| Superseded by | — |
| Related | [`0006-multi-agent-human-concurrency-governance.md`](0006-multi-agent-human-concurrency-governance.md), [`0204-multi-site-active-active.md`](0204-multi-site-active-active.md), [`0208-phoenix-native-peer-discipline.md`](0208-phoenix-native-peer-discipline.md), [`0210-zenoh-universal-substrate.md`](0210-zenoh-universal-substrate.md), [`0500-zenoh-data-fabric.md`](0500-zenoh-data-fabric.md), [`0811-audit-log-integrity.md`](0811-audit-log-integrity.md), [`0822-wide-event-observability-otlp-openobserve.md`](0822-wide-event-observability-otlp-openobserve.md), [`1009-ory-shared-network-nid-preseed.md`](1009-ory-shared-network-nid-preseed.md), [`1010-ory-on-spock-mindful-operation.md`](1010-ory-on-spock-mindful-operation.md). Companion ADRs landing together: [`0317-per-service-failure-mode-declaration.md`](0317-per-service-failure-mode-declaration.md), [`1014-codex-pattern-adr-to-enforced-lint.md`](1014-codex-pattern-adr-to-enforced-lint.md). Prior art: [Cloudflare, "Code Orange: Fail Small, Fail Complete," May 2026](https://blog.cloudflare.com/code-orange-fail-small-complete/). |

## TL;DR (for a layperson)

Today, when an operator changes a setting that affects every site — say, a new firewall rule or a new piece of policy — the change is copied to every site at once and every service starts using it immediately. If the change is bad, every site is broken at the same time. This ADR commits to the discipline of separating *the moment a setting reaches a site* from *the moment that site starts using it*, so a small number of sites can try the new setting first under health monitoring, and the change is automatically reversed before it reaches the rest if anything goes wrong. Cloudflare adopted the same pattern (called "Snapstone" internally) after their November and December 2025 outages, both of which were bad-data-as-configuration events. We adopt it here for the same reason.

## §1 Vocabulary

These five terms appear throughout the rest of the ADR. They are defined here so the rest of the document can be read without ambiguity.

- **Replication** — the act by which Spock copies a configuration row from one mgmt-host to every other mgmt-host. Immediate, automatic, unchanged from today.
- **Activation** — the act by which a consuming service reads a replicated row and applies the change, altering the service's runtime behavior. New under this ADR; previously implicit and immediate.
- **Coordinator** — a peer that publishes activation signals to consuming services, monitors health between cohorts, and publishes revert signals when health degrades. Singular per fleet.
- **Cohort** — a named set of consuming services that activate a configuration change together. Cohorts are derived from each peer's existing Phoenix-native declarations of what configuration topics it consumes.
- **Activation gate** — the named health condition that must hold true before the coordinator publishes the next cohort's activation signal. One gate per change class.

## §2 Context

Three forces converged to make this ADR necessary.

**1. Bad-data-as-configuration is a real failure class with operational precedent.** Cloudflare's November 18 and December 5, 2025 global outages were both caused by configuration data — a bad ML classifier file in November, a bad control flag in December — replicating fleet-wide instantly. Their post-mortem and remediation ("Code Orange: Fail Small, Fail Complete," May 2026) committed to progressive rollout with automatic revert as the load-bearing fix. The same shape applies to several of our load-bearing flows: a netbird-mgmt ACL push, a Block 23 schema declaration, a Block 17A.5 egress policy update, a Block 28 WASM module pin. Each is configuration that replicates fleet-wide via Spock and takes effect immediately. None has a progressive-rollout gate today.

**2. F-039 surfaced the same failure class internally.** The CoreDNS hosts-plugin permissions silent-failure was a configuration change (cron-published mode-0600 zone file) that broke fleet-wide DNS without any health gate to catch it. The fix at the cron-script layer is correct but local. The structural answer is a substrate-level pattern that catches *any* configuration change of this shape before it reaches the whole fleet.

**3. Spock active-active rules out replication-side cohorting.** Spock's flat active-active replication (ADR 0204) is load-bearing for our resilience model: every site can write, every site converges via last-writer-wins. We cannot pause Spock selectively without violating this property. The cohorting layer therefore must sit at the consumer (the service that activates the change), not at the replication layer (Spock itself). This is structurally different from Cloudflare's Snapstone — they gate at the deployment system because their config fan-out is centralized; we gate at the consumer because ours is not.

## §3 Decision

Adopt **progressive configuration rollout with automatic revert** as the substrate-level pattern for configuration changes that affect Phoenix-native peers.

### 3.1 Replication and activation are distinct steps

Spock continues to replicate every configuration row to every site immediately, as today. Each consuming service reads the replicated row and stores it locally but does not necessarily activate it. The coordinator publishes a Zenoh-keyed activation signal scoped to a cohort, and the consumers in that cohort activate on receipt of the signal.

This separation is the core architectural commitment of this ADR. Every other property (cohort staging, automatic revert, partition-mode policy) follows from it.

### 3.2 Cohort taxonomy is derived, not declared

Each Phoenix-native peer already declares the Zenoh key expressions it consumes (ADR 0208). The coordinator constructs cohorts at activation time by querying these declarations. There is no separate cohort registry to maintain.

For the four named consumers in scope at v1 — netbird-mgmt (ACL), schema-enforcer (schema declarations), db-policy-engine (egress + microsegmentation policy), WASM module loader (Block 28) — cohort assignment follows the per-class scheme defined in [`docs/design/progressive-config-rollout.md`](../design/progressive-config-rollout.md) §3.

### 3.3 Two-mode revert

When the activation gate fails for a change in cohort N, the coordinator publishes a revert signal scoped to that cohort. Two revert modes are defined:

- **In-place revert** (default, fast): the consumer reads the previously-active version of the configuration row and re-activates it.
- **Rebuild from clean** (Phoenix escape hatch): the consumer tears down and rebuilds from the supply chain, then rejoins. Available because every consumer in scope is Phoenix-native (ADR 0208) and the cruft test guarantees a clean rebuild path. Used when in-place revert cannot restore a known-good state — for example, when the bad change corrupted local state or when the consumer's previous version is no longer compatible.

The change class declares which mode is the default revert path. Per-change overrides are published with the activation signal.

### 3.4 Per-class partition-mode policy

A site that is partitioned from the coordinator at the time a configuration row replicates cannot receive an activation signal. Each configuration class declares one of two policies:

| Policy | Behavior on partition | Used when |
|---|---|---|
| **activate-on-arrival** | Consumer activates the change at the moment the row replicates, without waiting for an activation signal. | Operational urgency outweighs the rollout-bypass risk. ACL changes, credential rotations, security policy. |
| **hold-until-rejoin** | Consumer stores the row but defers activation until the partition heals and the activation signal arrives. | The change is non-urgent and the rollout-bypass risk dominates. Schema declarations, cosmetic changes. |

Convergence on partition heal follows Block 52's post-partition convergence runbook unchanged.

### 3.5 Revert emits a structured event

A revert expressed only as a configuration-row update (re-write the previous version) can be erased at the data layer when a later legitimate change overwrites the row under Spock LWW. To preserve the revert as an inspectable event, the coordinator emits a structured event into Block 27 (wide-event observability via OpenObserve) and Block 17D (tamper-evident audit via MinIO Object Lock) at the moment of revert.

Required fields: change identifier, configuration class, activation timestamp, revert timestamp, triggering activation-gate failure, cohorts affected, revert mode chosen.

This event is the durable record of the revert regardless of subsequent data-layer state.

### 3.6 Scope boundary: Phoenix-native peers only

This ADR applies to **Phoenix-native peers** (ADR 0208) — services, agents, shims that satisfy the cruft test. Substrate that is not Phoenix-native (Spock itself, Zenoh routers, Postgres, Caddy/Traefik, kernel, OS packages) continues to use traditional Ansible-deploy procedures. Attempting to bring substrate under this pattern would require those components to support deferred activation, which they do not.

The boundary is enforced by the cohort derivation step (§3.2): only Phoenix-native peers declare consumed Zenoh key expressions, so only Phoenix-native peers appear in any cohort.

**Amendment (2026-08-02, [ADR 1109](1109-fleet-management-dimension-and-pull-convergence.md) D4).** This boundary is extended for the *staged-rollout pattern* — wave staging, declared halt criteria, automatic halt, health-gated automatic revert — which now also applies to substrate deploys (Spock, Zenoh routers, Postgres, Caddy/Traefik, kernel, OS packages). Substrate cohorts derive from the **site and node-class dimensions of the identity grammar** (ADR 1043) rather than from consumed key expressions, so §3.2's no-cohort-registry rule is preserved. The **deferred-activation mechanism of this ADR does not extend**: substrate cannot defer activation, so a substrate wave stages at the deploy step and its safety property is automatic revert rather than delayed activation. The reasoning for the extension — that substrate deploys are where fleet-wide blast radius actually lives — is in [ADR 1109](1109-fleet-management-dimension-and-pull-convergence.md) and [`fleet-management.md`](../design/fleet-management.md) §6.1. Everything above this line stands as written for Phoenix-native peers.

### 3.7 Ory carve-out

ADR 1010 D4 requires identity mutations to flow through the Ory admin API only — never via out-of-band SQL — because kratos and hydra cache `nid` per-instance and out-of-band mutations leave state visible in postgres but invisible to the running process. Configuration rows in `kratos.*` and `hydra.*` schemas are excluded from this ADR's scope. Identity-related rollouts continue to use Ory's admin API plus ADR 1010's lifespan-budget plus bounded-retry pattern.

### 3.8 Compound change-set composition is reserved for Block 63A.8

A configuration change that requires two or more rows to activate atomically (for example, a schema v2 declaration plus a downstream consumer's filter v2 that depends on it) is a compound change. Activating one row but not the other leaves the system in an invalid intermediate state. This ADR does not define the compound-change-set abstraction; it commits Block 63A.8 to land that design when the first compound-change need arises in production. Until then, single-row rollouts are the only supported mode.

This is a known deferred problem, not an unknown gap. See §6 (consequences) for the residual risk framing.

## §4 Alternatives considered

- **Status quo: immediate activation everywhere.** No fail-small property; rejected on the same grounds Cloudflare rejected it after the December 2025 outage.
- **Replication-side cohorting (drop-in Snapstone copy).** Requires pausing Spock selectively, which violates ADR 0204 active-active. Rejected.
- **Per-consumer ad-hoc rollout (each service rolls out its own changes).** No coordinator means no central health-gating and no automatic revert. Reduces to status quo with extra complexity. Rejected.
- **Block 17A.5 policy engine as the rollout gate.** Block 17A.5 handles human approval *before* a change deploys, not health gating *during* deployment. Composable but not a substitute. Adopted as the upstream approval layer; this ADR adds the downstream health-gated rollout layer.
- **Feature flags as the activation mechanism.** Feature flags are an acceptable special case of activation gating for code-side changes; this ADR generalizes the pattern to data-side changes (configuration rows replicated by Spock) which are not amenable to in-process flag checks. Compatible, not competing.

## §5 Consequences

**Positive:**

- Bad-configuration blast radius shrinks from fleet-wide instant to one cohort with automatic revert before the next cohort.
- F-039-class silent failures gain a substrate-level catch independent of the cron-script-layer fix.
- Phoenix-native peer discipline becomes load-bearing in a new way: cohort taxonomy is free, and rebuild-from-clean is available as a revert mode no other architecture has.
- The audit trail for configuration changes (Block 17D + Block 27) gains revert events as first-class records, addressable by `correlation_id`.

**Negative / accepted trade-offs:**

- **Per-consumer integration cost.** Each consumer in scope must adopt the deferred-activation library (61A.2). Four consumers at v1; more as the pattern expands. This is the dominant implementation cost.
- **Coordinator is a new load-bearing service.** Adds a new failure mode (coordinator down → no new activations published → consumers fall back to either activate-on-arrival per §3.4 or store-and-wait depending on class). Mitigated by treating the coordinator as Phoenix-native and replicating its state via Spock.
- **Compound-change-set deferred to 61A.8.** If a compound-change need arises in production before 61A.8 lands, we hit a real gap. Residual risk: a contributor lands a multi-row configuration change without realizing the substrate doesn't support atomic activation. Mitigation: schema-enforcer interceptor (Block 23) gains a check that flags multi-row PRs touching configuration classes; the check warns until 61A.8 lands.

**Operational:**

- The four named consumer integrations (61A.3 through 61A.6) gate the pattern's operational utility. Until the first integration lands and is exercised in soak (ADR 1008), goat-rollout is paper. Block 63A.3 (netbird-mgmt ACL) is the named first integration because ACL changes are the highest-frequency change class today.
- Detection-signal definitions per change class (61A.7) are the genuinely-hard design call. What counts as "degraded" for an ACL push is different from what counts as degraded for a schema declaration. This subtask is gated on Block 27 wide-event coverage of each consumer.

## §6 Assessor view (SCA-R)

This ADR maps to NIST SP 800-171 rev 2 §3.4 (configuration management) — specifically 3.4.3 (track, review, approve / disapprove, log changes) and 3.4.5 (define, document, approve, enforce physical and logical access restrictions associated with changes). The progressive-rollout pattern strengthens 3.4.3's "log changes" by emitting structured revert events into the tamper-evident audit substrate (Block 17D). The two-mode revert (§3.3) and per-class partition-mode policy (§3.4) are explicit operational controls, not vague "we will be careful" language.

The Phoenix dependency is worth flagging: the strength of the rebuild-from-clean revert mode is exactly the strength of the Phoenix discipline. An assessor reviewing this ADR should also review ADR 0208 to confirm the cruft test is enforced in practice, not just declared.

The compound-change-set deferral (§3.8) is the single residual risk worth highlighting. The mitigation (schema-enforcer warning check in 61A.7) is concrete but not yet implemented; an assessor reviewing during the implementation window should confirm it lands before 61A.6 closes.

## §7 Trigger conditions for revisit

- **Three or more configuration changes in the activate-on-arrival class cause harm in production within a 90-day window.** Revisit the activate-on-arrival default; consider raising the threshold for that class or adding a per-change override.
- **A compound-change need arises in production before 61A.8 lands.** Escalate immediately; either accelerate 61A.8 or land a compensating manual procedure.
- **Coordinator outages exceed three per quarter or cause an activation-signal backlog larger than one cohort window.** The coordinator's single-instance model needs to be revisited (HA pair, Spock-replicated state, or shared-nothing per-cohort coordinators).
- **Phoenix discipline weakens — peers in scope start failing the cruft test.** Rebuild-from-clean revert mode loses its guarantee; revert to in-place-only.

## References

- [`docs/design/progressive-config-rollout.md`](../design/progressive-config-rollout.md) — design doc operationalizing this ADR (cohort schemes per consumer, activation-gate definitions, coordinator runtime model, revert event schema).
- `docs/project/implementation-plan.md` Block 63A — execution plan (sub-blocks 61A.1 through 61A.8).
- [Cloudflare, "Code Orange: Fail Small, Fail Complete," May 2026](https://blog.cloudflare.com/code-orange-fail-small-complete/) — prior art and motivating example.
- F-039 — internal precedent: configuration change broke fleet-wide DNS without a health gate.
- F-003 — last-writer-wins semantics motivating the structured revert event in §3.5.
- [ADR 0208](0208-phoenix-native-peer-discipline.md) — Phoenix-native peer pattern; cohort derivation and rebuild-from-clean revert depend on it.
- [ADR 0204](0204-multi-site-active-active.md) — Spock active-active; the property this ADR composes with rather than fights.
- [ADR 1010](1010-ory-on-spock-mindful-operation.md) — Ory mindful-operation pattern; the carve-out in §3.7 cross-references its D4.
- See GLOSSARY.md for acronym expansions used in this ADR.
