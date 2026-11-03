# Upper-Band Scale Validation Plan

**Purpose.** ADR 0215 commits to a 600 / 3000 / 6000 fleet envelope (floor / target / stretch) across 6 sites. Validated evidence today caps near 100 peers (CGNAT attempt-2), single-relay throughput (M-001, M-003), and 2-node Spock soaks (inter-site Spock multi-master shape — see [`run-2node-spock.sh`](../../ops/ansible/roles/ory/tests/soak/run-2node-spock.sh) header). Block 33 fixes one known data-plane bottleneck. There is no integrated test plan that exercises the *full* substrate at the *target* band before production hits it. This document defines that plan.

**Envelope revision (2026-08-11).** [ADR 1122](../adr/1122-scale-envelope-systems-vs-peers-pod-tier.md) supersedes the 0215 envelope in part: fleet counts are now split **systems vs mesh peers**, with a per-goatnet 30k/300k/1M-systems ladder carried on 10k/30k/100k peers via the pod aggregation tier. The bands below stand unchanged as the first rungs — nothing above them is validated — and each 10× step past Stretch re-runs this harness shape at the new band.

**Scope.** Methodology + harness + named measurements for substrate-level scale validation. Sub-component scale work continues to live in its own Block (e.g. Block 33 relay).

**Companion docs.** `goat-net-scaling.md` — federation-plane / site-plane scaling architecture (this is *what* scales). [`engineering-health-assessment.md`](https://github.com/usareur-af/goatdocs/blob/main/docs/design/engineering-health-assessment.md) — post-deployment metrics (this is what you measure once *running*). This plan covers the *pre-deployment* validation that the substrate can sustain the target band.

**Maturity caveat.** Per ADR 0215 and the project-level "POC stage" framing in CLAUDE.md, sovereign edge data backbones at this shape and scope have no established operational precedent at this scale. Specific scaling, performance, and operational characteristics will be discovered. This plan defines how we discover them deliberately rather than under production load.

---

## 1. Three bands, three test plans

The harness covers three bands, each with its own purpose:

| Band | Per-site peers × sites | Total peers | Purpose |
|---|---|---|---|
| **Floor** | 100 × 6 = **600** | 600 | Phase-3 go-live target. Must validate before Phase-3 cutover. |
| **Target** | 500 × 6 = **3,000** | 3,000 | Steady-state operational envelope. Must validate before any per-site peer count > 200 in production. |
| **Stretch** | 1,000 × 6 = **6,000** | 6,000 | Capacity headroom. Validates before any decision to onboard a second wave of sites or expand peer count. |

Each band has the same harness shape with different parameters. We do not skip bands — passing Stretch does not imply Target was tested at production-realistic conditions, since cost / failure-mode / observation-quality differ.

---

## 2. Dimensions to measure

For each band, the harness measures:

| Dimension | Metric | Pass criterion (placeholder — refined per band) |
|---|---|---|
| **Mesh peer admit time** | Time from `nb up` to `peer connected` for the Nth peer in a fleet of N peers, p50 / p95 / p99 | p99 ≤ 60s at floor; refined at target |
| **Mesh control-plane latency** | Time for ACL update to propagate to all peers, p50 / p95 / p99 | p99 ≤ 5s at floor |
| **Signal-server federation Forward RPC** | p50 / p95 / p99 latency under steady load + concurrency at target rate | p99 ≤ 200ms (per Block 52A.4 tightening) |
| **Spock replication lag** | Time from write at site A to visibility at site F, p50 / p95 / p99 | p99 ≤ 5s at floor |
| **Postgres connection density** | Active connections at mgmt-host postgres-spock during peak peer activity | < 80% of `max_connections` budget |
| **Audit-tier ingest rate** | Wide-event + integrity-tier events/sec sustained at target peer activity | No drop / no backpressure to producers |
| **OpenObserve query latency** | Cross-site wide-event query p99 under steady ingest | p99 ≤ 2s for 24h window |
| **Mesh DNS QPS** | Resolver query rate at saturation | Resolver scales linearly; no cliff |
| **Relay fleet throughput** | Aggregate Mbps across 3 relays under peer mix | Floor: per-peer share ≥ 10 Mbps when relayed |
| **Memory + CPU at every load-bearing host** | Steady-state under target activity | Headroom ≥ 30% on every load-bearing component |
| **Failure-mode under saturation** | Behavior at 1.2× target rate — does the substrate degrade gracefully? | Documented degradation; no cascading failure |

Each dimension's `pass criterion` is refined as we learn — placeholders here are starting points based on adjacent measurements + reasonable expectations.

---

## 3. Harness shape

The harness extends the [CGNAT validation env pattern](cgnat-validation-env.md) (cloud-init self-enrollment, terraform-managed, ~$16/run for 100 peers). Scaling considerations:

| Concern | Approach |
|---|---|
| **Cost** | Floor band ≈ 600 t3.nano × $0.0052/h × 6h ≈ $19/run. Target band ≈ $95/run. Stretch ≈ $190/run. Costs are tractable — we run each band quarterly during Phase 3, less frequently after. |
| **Topology** | Mirror production: 6 sites, full federation plane, CGNAT-aware (per ADR 0316 production shape). Each "site" is a 2-node mgmt-host pair (active-passive Spock) on EC2. |
| **Peer simulation** | Stock netbird daemon as systemd service per peer (the validated pattern from CGNAT attempt-2, post-Block 21 identity work). Synthetic load via [`ops/zenoh/loadgen/`](../../ops/zenoh/loadgen/) extension. |
| **Realism** | Peer behavior = ~10% admit/release churn rate, ~40% steady-state mesh chatter, ~50% partner-release flow at target ingestion rate. Calibrated to expected production mix; refined as production data accumulates. |
| **Observation** | All measurements collected via Block 27 wide events to a *separate* OpenObserve instance from the substrate-under-test. Don't measure observability with itself. |

Concrete provisioning lives at `ops/scale-validation/` (placeholder — created in Block 54.1).

---

## 4. Sequencing — Floor band before Phase 3 cutover

The Floor band (600 peers) is a **gating measurement for Phase-3 go-live**. Concretely:

- **Before the first site moves from a cloud proof-of-concept to operational use**, the Floor band must have completed at least one full pass (all dimensions in §2 measured, all pass criteria met or documented as accepted-risk).
- Re-pass quarterly during Phase 3.

Target + Stretch bands are *not* gates — they are *ceiling claims*. We have no production envelope at those scales today; the bands establish what's possible before we get there.

---

## 5. Per-band run shape

A run takes ~6-8 hours wall-clock and produces:

1. **Pre-run** — env spun up via Terraform, peers self-enrolled via cloud-init (mass-rollout pattern from CGNAT env).
2. **Steady-state hold** — substrate under target peer activity for at least 1 hour. All §2 dimensions sampled.
3. **Saturation push** — ramp to 1.2× target. Observe degradation mode + recovery on ramp-down.
4. **Failure injection** — single-axis faults during steady state (lose one mgmt-host, lose one relay, partition one site). Captures ha-failover under load. Adjacent to but not duplicating the [combined-failure framework](combined-failure-validation.md), which is multi-axis in a smaller env.
5. **Tear-down** — env destroyed.
6. **Report** — JSONL evidence + summary table → `docs/measurements/M-NNN-scale-validation-<band>-<date>.md` per [ADR 1012](../adr/1012-load-testing-discipline.md) measurement-register pattern. Updates to `validation-state.md` §"Scale validation" in the same change.

---

## 6. Expected first-failure modes (predictions)

Hypothesis-first per the [worker-discipline mechanism](https://github.com/usareur-af/goatdocs/blob/main/docs/design/multi-agent-team-management.md). What we expect to break first, in rough order:

1. **Mesh DNS QPS** — CoreDNS hosts plugin re-reads file every cycle; at high churn this becomes the bottleneck.
2. **Postgres connection pool** — every netbird-mgmt instance + signal-server + IdP + policy-engine + observability holds connections; at 6 sites × 1k peers we may saturate `max_connections` without a bouncer.
3. **Signal-server federation Forward RPC** — Block 52A.4 tightened the deadline; under target concurrency the budget may be insufficient.
4. **Audit-tier MinIO ingest** — wide events + integrity tier + video-fabric metadata ride the same per-site MinIO; under combined load, we may hit IOPS caps.
5. **Spock replication lag** — under burst write rate (mass peer rejoin), Spock LWW may fall behind enough to cause user-visible lag.

Each prediction maps to a §2 dimension; pass/fail at the predicted dimension gives evidence on whether the prediction was right. Wrong predictions are valuable too — they refine the model.

---

## 7. Out of scope

- **Production data exfiltration / partner-real flows** — the harness uses synthetic traffic; partner-real flows are validated via the partner-charter framework, not this harness.
- **Adversarial behavior** — single-axis failure injection is in scope (§5 step 4); multi-axis adversarial is the [combined-failure framework](combined-failure-validation.md).
- **Component-specific micro-benchmarks** — those live in their own Block (Block 33 relay, Block 34 load-testing back-fill). This document is *substrate-level* — what happens when all the components run together at scale.

---

## 8. Vendor / platform-org handoff candidacy

Per `vendor-handoff-pickup-plan.md` framework: parts of this work are candidate for vendor / platform-org handoff once the harness is shipped. Specifically:
- Quarterly run cadence + report production is operationally simple and high-value-per-hour but low-novelty after the first runs.
- Specific bottleneck remediation (e.g. postgres connection-pool work, MinIO IOPS work) may be vendor-handoff candidates if we can write the artifact bundle cleanly.

The harness itself is not handoff candidate — it is project-defining infrastructure.

---

## 9. References

- ADR 0215 — scalability continuity scope
- `goat-net-scaling.md` — federation-plane / site-plane scaling architecture
- [`cgnat-validation-env.md`](cgnat-validation-env.md) — env-bootstrap pattern this harness extends
- [`engineering-health-assessment.md`](https://github.com/usareur-af/goatdocs/blob/main/docs/design/engineering-health-assessment.md) — post-deployment metrics
- [`combined-failure-validation.md`](combined-failure-validation.md) — multi-axis adversarial validation (companion)
- `docs/measurements/INDEX.md` — measurement register where reports land
- `docs/project/validation-state.md` — verdict tracker
- Block 54 — upper-band scale validation harness — execution tracker
