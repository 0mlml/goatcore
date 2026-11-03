# Progressive configuration rollout — design

This design doc operationalizes [ADR 0224](../adr/0224-progressive-config-rollout-with-auto-revert.md). It specifies the coordinator runtime, per-class cohort schemes for the four named v1 consumers, activation-gate definitions, the revert event schema, partition-mode interaction with Block 52, and the placeholder for the Block 63A.8 compound change-set abstraction.

Vocabulary used here (replication, activation, coordinator, cohort, activation gate, in-place revert, rebuild-from-clean) is defined in [ADR 0224 §1](../adr/0224-progressive-config-rollout-with-auto-revert.md#1-vocabulary). Read that section first.

We gate at the consumer layer, not the replication layer, because Spock's flat active-active replication (ADR 0204) is load-bearing for our resilience model.

## §1 The coordinator

A single Phoenix-native peer named `goat-rollout-coordinator` per fleet. Written in Rust per ADR 0112 (workload shape: per-message fan-out on the data plane is Rust territory).

**Identity and discovery:** the coordinator publishes its presence at the well-known Zenoh key `attest/queryable/rollout-coordinator`. Consumers subscribe to `cmd/rollout-coordinator/<change_class>/<change_id>/<cohort>` for activation and revert signals scoped to their cohort.

**State:** the coordinator's only durable state is the activation-state log (which cohort received which signal at what time) — replicated via Spock to all sites so any coordinator instance can pick up where another left off. Phoenix-native; rebuild-from-clean is the failure mode for the coordinator's own state corruption.

**Concurrency:** at v1 the coordinator is a singleton per fleet, started by Ansible on the active mgmt-host A and failed over to mgmt-host B by keepalived. HA pair, not multi-instance. Multi-instance coordination is reserved for a future revisit when fleet size makes singleton throughput a bottleneck.

**Health-signal ingest:** the coordinator subscribes to Block 27 wide-event streams (via OpenObserve query API) and Prometheus alert webhooks. It does not poll services directly; it consumes the same observability substrate operators use. This guarantees that "what the coordinator sees" matches "what an operator sees during incident review."

**Pluggable activation-gate definitions:** the coordinator does not encode per-change-class gate logic in its core. Each class defines its gate as a YAML rule shipped alongside the coordinator binary at `ops/rollout-coordinator/gates/<class>.yaml`. Adding a new change class is a config drop, not a coordinator code change.

## §2 Per-class cohort schemes (v1)

Four named consumers in v1. Each declares the configuration class it consumes and the cohort scheme to apply.

### 2.1 netbird-mgmt — ACL changes

- **Class:** `acl`
- **Source row:** Spock-replicated row in `netbird_mgmt.policy_rule` and related tables.
- **Consumers in scope:** every netbird-mgmt instance (one per mgmt-host pair, active-passive behind keepalived). At v1, `acl` consumers form one cohort per site.
- **Cohort scheme:** **per-site, sequential.** Cohort 0 = vega. Cohort 1 = first-spoke-after-vega. Future cohorts as additional sites land. Sequential because ACL changes are operationally critical (per §3.4 partition-mode `activate-on-arrival`) and one-site-at-a-time progression catches a bad ACL before fleet-wide effect.
- **Cohort window:** 90 seconds between activation signals. Long enough for the gate to read steady-state metrics; short enough that a healthy change reaches all sites within a minute or two.
- **Activation gate:** `acl-health.yaml` — see §3.1.
- **Default revert mode:** in-place. ACL changes are pure-config rollback by re-reading the previous policy_rule row.

### 2.2 schema-enforcer — schema declarations

- **Class:** `schema-declaration`
- **Source row:** Spock-replicated row in `attest.schema_declaration` (Block 23).
- **Consumers in scope:** every schema-enforcer interceptor (one per Zenoh router, deployed as a sidecar in the router's container).
- **Cohort scheme:** **canary-then-fleet.** Cohort 0 = one designated canary site (vega at v1). Cohort 1 = remaining sites. Two-cohort scheme because schema declarations are non-urgent (per §3.4 partition-mode `hold-until-rejoin`); the broader cohort can wait.
- **Cohort window:** 10 minutes between cohorts. Schema declarations affect downstream consumers; 10 minutes is long enough for downstream effects to surface.
- **Activation gate:** `schema-declaration-health.yaml` — see §3.2.
- **Default revert mode:** in-place. Schema declarations are versioned per-topic; revert means marking the previous version current.

### 2.3 db-policy-engine — egress + microsegmentation policy

- **Class:** `policy`
- **Source row:** Spock-replicated rows in `policy.egress_policy` and `policy.microseg_rule` (Block 17A.5).
- **Consumers in scope:** every db-policy-engine instance (one per mgmt-host pair, active-passive).
- **Cohort scheme:** **per-site, sequential** (same as netbird-mgmt ACL).
- **Cohort window:** 90 seconds.
- **Activation gate:** `policy-health.yaml` — see §3.3.
- **Default revert mode:** in-place. Policy rules are pure-config.

### 2.4 WASM module loader — Block 28

- **Class:** `wasm-module`
- **Source row:** Spock-replicated row in `release.wasm_module_pin` (Block 28; per-partner WASM module signed hash).
- **Consumers in scope:** every release-bridge instance (one per mgmt-host pair).
- **Cohort scheme:** **canary-then-fleet** (same as schema-enforcer).
- **Cohort window:** 5 minutes.
- **Activation gate:** `wasm-module-health.yaml` — see §3.4.
- **Default revert mode:** *rebuild-from-clean*. WASM modules carry partner-supplied state-handling logic; in-place revert risks leaving partial state. Rebuild-from-clean uses the Phoenix discipline — release-bridge tears down, rebuilds with the previously-pinned module hash, rejoins. This is the v1 example of fail-rebuild as the default revert mode.

## §3 Activation-gate definitions

Each class's gate is a YAML rule file. Format is intentionally simple; complexity lives in the underlying observability queries, not in a gate-DSL.

### 3.1 `acl-health.yaml`

```yaml
class: acl
required_signals:
  - name: netbird-mgmt-error-rate
    source: prometheus
    query: 'rate(netbird_mgmt_request_errors_total[2m]) / rate(netbird_mgmt_requests_total[2m])'
    threshold_lt: 0.01  # less than 1% error rate
  - name: peer-handshake-success
    source: prometheus
    query: 'rate(netbird_peer_handshake_success_total[2m]) / rate(netbird_peer_handshake_attempts_total[2m])'
    threshold_gt: 0.95  # at least 95% handshake success
  - name: signal-server-forward-deadline
    source: prometheus
    query: 'histogram_quantile(0.95, rate(signal_forward_duration_seconds_bucket[2m]))'
    threshold_lt: 0.2  # p95 forward < 200ms (Block 52 alignment)
window_seconds: 60  # gate checks signals over the last 60 seconds
```

### 3.2 `schema-declaration-health.yaml`

```yaml
class: schema-declaration
required_signals:
  - name: schema-enforcer-reject-rate
    source: prometheus
    query: 'rate(schema_enforcer_publish_rejected_total[2m]) / rate(schema_enforcer_publish_attempted_total[2m])'
    threshold_lt: 0.005  # rejection rate must not spike above 0.5% (catches schemas that break legitimate publishers)
  - name: downstream-consumer-error-rate
    source: openobserve
    query: 'fabric.consumer_errors{schema_class="$change_id"}'
    threshold_lt: 0.01
window_seconds: 300  # 5 minutes; downstream effects take longer to surface
```

### 3.3 `policy-health.yaml`

```yaml
class: policy
required_signals:
  - name: policy-engine-decision-error-rate
    source: prometheus
    query: 'rate(db_policy_engine_decision_errors_total[2m]) / rate(db_policy_engine_decisions_total[2m])'
    threshold_lt: 0.01
  - name: egress-proxy-deny-spike
    source: prometheus
    query: 'rate(squid_proxy_request_denied_total[2m])'
    threshold_lt_relative: 2.0  # deny rate must not exceed 2x the pre-activation baseline
window_seconds: 60
```

### 3.4 `wasm-module-health.yaml`

```yaml
class: wasm-module
required_signals:
  - name: wasm-module-fault-rate
    source: prometheus
    query: 'rate(release_bridge_wasm_fault_total[2m]) / rate(release_bridge_wasm_invocations_total[2m])'
    threshold_lt: 0.001  # < 0.1% fault rate (very tight; partner WASM is supposed to be correct-by-contract)
  - name: wasm-module-fuel-exhaustion-rate
    source: prometheus
    query: 'rate(release_bridge_wasm_fuel_exhausted_total[2m])'
    threshold_lt: 1  # at most one fuel-exhaustion event per minute (anything more = the module is broken)
window_seconds: 120
```

## §4 Revert event schema

Per ADR 0224 §3.5, the coordinator emits a structured event into Block 27 wide events and Block 17D tamper-evident audit at the moment of revert. Schema:

```yaml
event_type: rollout.revert
correlation_id: <uuid>
timestamp: <RFC3339Nano>
change_id: <uuid>
change_class: acl | schema-declaration | policy | wasm-module
revert_mode: in-place | rebuild-from-clean
activation_started_at: <RFC3339Nano>
revert_started_at: <RFC3339Nano>
triggering_gate: <gate-rule-name>
triggering_signal: <signal-name>
signal_value_observed: <number>
signal_threshold: <number>
cohorts_affected:
  - cohort_id: <int>
    consumers: [list of peer identities]
previous_version: <change_id of pre-activation state>
emitted_to:
  - block-27-wide-event-substrate (OpenObserve)
  - block-17D-audit-integrity-substrate (MinIO Object Lock)
```

The event is durably observable independent of subsequent data-layer state. A later legitimate change overwriting the configuration row under Spock LWW does not erase the revert event.

## §5 Partition-mode interaction with Block 52

When a site is partitioned from the coordinator at the time of an activation:

- **`activate-on-arrival` classes** (acl, policy): the consumer activates at the moment Spock delivers the row, without waiting for the activation signal. The site behaves as if the coordinator's signal had arrived. Risk accepted: a site may activate a change that the coordinator would have caught.
- **`hold-until-rejoin` classes** (schema-declaration, wasm-module): the consumer stores the row but defers activation. On partition heal, the coordinator either publishes the activation signal (if the change passed its gate at other sites) or publishes the revert signal (if it failed). The consumer follows whichever arrives.

Convergence on partition heal follows [Block 52's post-partition convergence runbook](../operations/post-partition-convergence-runbook.md). This design adds two specific events to Block 52's runbook §3 ("activation backlog flush") and §4 ("revert backlog flush") which the runbook now lists as dedicated steps when the coordinator's state catches up post-partition.

## §6 Compound change-set placeholder (Block 63A.8)

A configuration change requiring two or more rows to activate atomically is not supported in v1. Block 63A.8 is reserved to land that design when the first compound-change need arises in production.

Until then, two mitigations:

1. **Schema-enforcer interceptor flag (Block 23 extension, lands in 61A.7):** any PR adding two or more rows to configuration classes within a single change_set tag is flagged. The flag warns; it does not block. The flag is the trigger for promoting Block 63A.8 from Backlog to active.
2. **Documentation in change-author guidance (CONTRIBUTING.md amendment, lands in 61A.7):** authors needing a compound change must split it into a sequence of single-row changes that maintain validity at each intermediate state. If splitting is impossible, the author files an escalation per ADR 0010 to defer the change until 61A.8 lands.

## §7 Implementation plan reference

This design is operationalized in Block 63A of the implementation plan, which decomposes into:

- 61A.1 — coordinator binary scaffolding (Rust, Phoenix-native, Spock-replicated state)
- 61A.2 — consumer-integration framework (Rust + Go libraries)
- 61A.3 — netbird-mgmt ACL integration (named first integration; exercises the pattern end-to-end)
- 61A.4 — schema-enforcer schema-declaration integration
- 61A.5 — db-policy-engine policy integration
- 61A.6 — WASM module loader integration (also exercises rebuild-from-clean default)
- 61A.7 — activation-gate YAML definitions + schema-enforcer compound-change flag
- 61A.8 — compound change-set abstraction (deferred; gated on first production need)

## §8 Open questions for human review

These questions need lead-engineer review before 61A.1 begins. Items are sequenced into the human/external actions register in implementation-plan.md.

1. **Coordinator HA pattern.** v1 spec is "active-passive behind keepalived, Spock-replicated state." Is keepalived sufficient, or does the coordinator need a Spock-aware leader-election layer? Trade-off: keepalived is simple but slower to fail over; leader election is fast but adds complexity.
2. **Cohort window tuning per class.** Numbers in §2 are first-cut estimates. Empirical tuning needs production-traffic data; first revisit at the 30-day mark after each consumer integration ships.
3. **`activate-on-arrival` risk acceptance per class.** §5 risk language is engineer-grade. A formal risk acceptance for ACL and policy classes specifically (recorded as a finding-or-waiver, not just an ADR clause) is the kind of artifact an SCA-R asks for.
4. **Ory carve-out completeness.** ADR 0224 §3.7 excludes `kratos.*` and `hydra.*` schemas. A cross-check against the actual Ory schema list at v1 is needed; new Ory tables added in future Ory releases should automatically inherit the carve-out.
5. **`wasm-module` rebuild-from-clean as default.** §2.4 commits this; it is the v1 example of fail-rebuild as a default revert mode. Rebuild-from-clean for partner WASM is operationally novel and may have surprising effects (mid-flight partner-data rejection, audit gap). Pilot validation needed before the default is locked.

## §9 References

- [ADR 0224](../adr/0224-progressive-config-rollout-with-auto-revert.md) — the parent ADR.
- [Block 52 design](replication-consistency-observability.md) — partition-heal substrate this design composes with.
- [Block 27 design](observability-wide-events.md) — wide-event substrate the revert event lands on.
- [Block 17D / ADR 0811](../adr/0811-audit-log-integrity.md) — audit integrity substrate.
- [Block 38 / ADR 1013](../adr/1013-continuous-architectural-chaos.md) — chaos probes will exercise activation gates as part of the rollout-coordinator's invariant set.
- F-039 — internal precedent for the failure class this design addresses.
- F-003 — LWW semantics motivating §4 revert event.
