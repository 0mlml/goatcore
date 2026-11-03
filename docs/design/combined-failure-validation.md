# Combined-Failure & Adversarial Validation

**Purpose.** Real operational failure modes are multi-axis: partition + clock skew + admin compromise + supply-chain push + GNSS denial co-occur. The existing test classes cover single axes well — soak ([ADR 1008](../adr/1008-soak-testing-discipline.md)) is duration; load ([ADR 1012](../adr/1012-load-testing-discipline.md)) is throughput; continuous architectural chaos (Block 38) probes invariants. None of them test the *combinations* that are the actual production failure mode. This document defines the combined-failure validation framework and the adversarial Red Team program.

**Scope.** Test methodology for combined faults + adversarial validation. Not a test runner implementation — that is Block 55.

**Companion docs.** `docs/design/threat-model.md` defines the adversary tiers; this document validates the controls in §3 of that doc actually hold under combination. [`docs/design/continuous-architectural-chaos.md`](continuous-architectural-chaos.md) is single-invariant probing in production; this document is multi-axis testing in a contained env.

---

## 1. Two distinct programs

Combined-failure validation has two complementary programs, each with its own cadence and audience:

### 1a. Combined-fault test matrix

**Premise.** Most production-relevant failure modes are not first-time-novel — they are predictable combinations of axes we already understand. We can enumerate the combinations and write deterministic tests against them.

**Output.** A growing matrix of combined-fault scenarios, each with a deterministic driver and a verdict criterion. Lives in `ops/combined-fault/` (path placeholder; created in Block 55.1).

**Audience.** Engineering team. Run pre-merge for changes that touch the load-bearing failure-isolation paths; run weekly on a cron against a dedicated env.

### 1b. Adversarial Red Team

**Premise.** Tests we *can enumerate* exhaust the adversary axis we *thought of*. Real adversaries find paths we didn't think of. The complement is opening the substrate to a creative adversary inside a contained environment.

**Output.** Quarterly Red Team engagement against a clone of the production substrate. Findings flow into the F-NNN findings register and the threat model §1 capability tiers.

**Audience.** Engineering team + Sponsor Liaison + Sponsor.

The two programs are sequenced: 1a establishes a baseline of known-defended combinations; 1b probes for unknown ones. 1b findings become 1a tests. The cycle compounds.

---

## 2. Combined-fault axes

The fault axes the test matrix covers, drawn from the threat model + the failure-isolation design + operational experience:

| Axis | Examples |
|---|---|
| **Network partition** | One site cut from federation; relay fleet partial outage; bridge VPS unreachable |
| **Clock skew** | Forward / backward NTP drift; deliberate spoof; GNSS denial |
| **Identity compromise** | Peer credential leaked; admin credential leaked; service-account stolen |
| **Supply chain** | Tainted image pushed; CI pipeline compromised; upstream-dep advisory |
| **Resource exhaustion** | OOM / disk-full / connection pool exhausted; runaway peer; relay saturation |
| **Crypto / time-quality degradation** | Key rotation in flight; chrony source loss; cert expiration |
| **Operator action** | Bad policy push; ACL mistake; mid-flight migration |
| **Hardware** | NVMe failure; chassis intrusion; rack power loss |

Combinations are written as *N-axis*: 2-axis is the base, 3-axis is where most production-novel failures live, 4+-axis is the long tail. The matrix should cover **every 2-axis pair where both are individually defended**, plus the **eight named 3-axis combinations** in §4.

---

## 3. Combined-fault driver shape

Each scenario in the matrix is a deterministic driver (bash + docker-compose + Ansible, mirroring the [soak driver pattern](../adr/1008-soak-testing-discipline.md)) with the following structure:

```
ops/combined-fault/<scenario_id>/
├── README.md            # what the scenario probes, named axes, expected verdict
├── setup.sh             # provision the env (typically a CGNAT-aware mini-fleet)
├── run.sh               # apply faults in order, observe, compute verdict
├── faults/              # per-axis fault injectors (network partition, clock skew, etc.)
└── teardown.sh          # destroy the env
```

The `run.sh` records evidence as JSONL into `docs/reports/combined-fault/<scenario_id>-<date>.jsonl`. A summarizer (analogous to [`ops/ansible/roles/ory/tests/soak/summarize.py`](../../ops/ansible/roles/ory/tests/soak/summarize.py)) emits a §6-criterion-check rollup ready to paste into `validation-state.md`.

**Verdict criteria.** Each scenario specifies:
- **Pass** — the substrate held the named invariant despite the combined fault.
- **Degraded-by-design** — the substrate entered a documented degraded mode that's expected under this combination.
- **Fail** — the substrate violated an invariant that should have held; emits an F-NNN finding.

Degraded-by-design is a first-class verdict; it acknowledges that not every fault combination has a clean-recovery answer, but the *behavior* under the combination must be *known and documented*.

---

## 4. The eight named 3-axis combinations

Drawn from threat-model + production experience + Wave-3 + Phase-2 incidents. These are the combined faults the matrix prioritizes first.

| ID | Combination | Why it matters |
|---|---|---|
| **C3-01** | Site partition + clock skew + admin policy push | The Spock LWW correctness scenario from [`mgmt-plane-threat-mitigations.md`](mgmt-plane-threat-mitigations.md). Skewed-clock site wins conflicts during partition; admin push during skew compounds the wrong outcome. |
| **C3-02** | Peer compromise + relay saturation + island-mode admit | Tier-2 attacker exploits the island-mode admit path during a relay-saturation event. Combines F-014 with [`mgmt-plane-threat-mitigations.md`](mgmt-plane-threat-mitigations.md) §2. |
| **C3-03** | Tainted image + audit-log MinIO pressure + admin compromise | Tier-3+ scenario — Block 17D Object-Lock is supposed to be tamper-evident under admin compromise, but a tainted image *plus* MinIO pressure could drop tamper-evidence at the moment we need it most. |
| **C3-04** | GNSS denial + NTS source loss + signal-server failover | EW-class scenario — multi-axis time-quality degradation co-occurs with mgmt-plane failover. Validates Block 36 + the §1 NTP defenses + ha-failover under combination. |
| **C3-05** | Cert expiration + Spock peer rejoin + cross-site-replay | Cert in the cross-site replication path expires *during* a Spock peer rejoin from prior partition; tests whether the rejoin path enforces freshness on rejoin or accepts a stale-cert peer that was previously valid. |
| **C3-06** | Egress-proxy outage + DNS egress failover + audit ingestion lag | Layered egress controls (Block 17 Tier 0/1/2) failing in a specific order — does the system fail-closed or fail-open, and is the failure visible in audit on time? |
| **C3-07** | Partner-bridge WASM module crash + replay attempt + identity rotation | Block 28 WASM sandbox handles single-module-crash, but combined with an in-flight identity rotation + a replay attempt against the same module, does the boundary still default-deny? |
| **C3-08** | Mass peer rejoin + audit-tier write surge + monitoring outage | The "every peer comes back online at once after a regional outage" scenario; tests every backpressure path simultaneously. |

Each scenario is a Block 55 sub-task; ordering is risk-times-tractability.

---

## 5. Non-axis-injection scenarios — adversarial Red Team

The Red Team program is structurally different from the matrix. It does not pre-specify what gets attacked — only the rules of engagement and the contained environment.

### 5a. Cadence

- **Quarterly** — one engagement per quarter.
- **Duration** — 5 working days (one week of Red Team activity, with the engineering team in defender mode but not blocked from other work).
- **Scope** — a clone of the production substrate (the "redteam" env, see §6) with synthetic data.

### 5b. Rules of engagement

- The Red Team has **Tier-2 starting position** by default (one peer credential issued for the engagement). They may attempt to escalate to Tier-3 or Tier-4 within the engagement env.
- Out of scope: the offline CA's *physical* custody (the clone env runs a development-mode CA per `accepted-risks.md` AR-002), social engineering of out-of-engagement personnel, denial-of-service against goat-team workflow tools.
- In scope: every load-bearing component, every documented control, every advertised invariant.
- The Red Team commits to **no pre-coordination with the engineering team** on attack surface; the engineering team commits to **no pre-fortification** of specific controls during the week.

### 5c. Engagement

- **Day 0** — kickoff: Red Team gets engagement scope + Tier-2 starting credentials.
- **Days 1-4** — Red Team works; engineering team operates the substrate normally; both sides keep a structured log.
- **Day 5** — debrief: Red Team walks through every attack attempted, what worked, what didn't, what was unexpected.

### 5d. Outputs

- Every successful attack → F-NNN finding entry, control-traceability matrix update, threat-model §1 tier review.
- Every *failed* attack against an asserted control → reinforces confidence in that control (logged in the engagement report, also valuable evidence per the feedback discipline on confirmations).
- Every *novel* attack vector that didn't work-or-fail-cleanly → file as an open question for the next engagement.

### 5e. Sourcing

The Red Team is **not** the engineering team. Initial sourcing options, in preference order:
1. **External Red Team** — formal engagement with a commercial firm or sponsor-side cell. Best independence; highest cost.
2. **Cross-team within the broader org** — engineers from a sister project / FFRDC / service unit with no goat day-to-day. Moderate independence.
3. **Internal pair-with-rotation** — two goat-team engineers explicitly designated as Red Team for the engagement; they switch out for defender role next quarter. Low independence but realistic for the small team in early phases.

Phase-3 entry: source 2 or 3. Phase-4: source 1.

---

## 6. The "redteam" env

Both 1a and 1b need a contained env that mirrors production posture but has no production data. Defined here:

- **Topology** — clone of the production-target topology: several sites with multiple mgmt-hosts each, full federation plane, partner-bridge to a stand-in partner network. Smaller-scale variant (2 sites) for fast 1a iteration.
- **Data** — synthetic only. No partner-real data. No personnel-identifiable material. Synthetic data generators per fabric topic class.
- **Identity** — separate offline CA (development-mode), separate IdP tenants, separate certificate roots from production.
- **Bootstrap** — Terraform + Ansible drivers parameterized to spin up + tear down. Cost target: ≤ $50/quarter for the sustained env (small) + ≤ $500 for the quarterly Red Team week (full topology).
- **Lifecycle** — ephemeral by default; persistent only during a Red Team week.

Concrete provisioning lives at `ops/redteam-env/` (placeholder — created in Block 55.2).

---

## 7. Ordering against existing test classes

| Test class | Anchor | Axis | Depth | Cadence |
|---|---|---|---|---|
| Soak ([ADR 1008](../adr/1008-soak-testing-discipline.md)) | Driver pattern | Time | Single-axis | Per-component, per-release |
| Load ([ADR 1012](../adr/1012-load-testing-discipline.md)) | Measurement register | Throughput | Single-axis | Per-component, per-release |
| Continuous chaos (Block 38) | Architectural invariants | Many; production env | Single-axis per probe | Continuous |
| **Combined-fault matrix (this doc)** | Scenarios | Many; contained env | **Multi-axis** | Per-merge for affected paths; weekly cron |
| **Red Team (this doc)** | Engagement | All | **Adversarial creative** | Quarterly |

Combined-fault tests are *not* a substitute for soak or load; they presume both have established baselines. Red Team is *not* a substitute for combined-fault; combined-fault enumerates the known, Red Team finds the unknown.

---

## 8. Sequencing

Implementation tracked in Block 55. Sequencing:

- **55.0 — env stand-up** (the small redteam env first; full topology later).
- **55.1 — driver framework** (the `ops/combined-fault/` shell mirroring soak drivers).
- **55.2 — first three named scenarios** (C3-01, C3-04, C3-08 — picked for highest production-relevance + lowest implementation cost).
- **55.3 — remaining five 3-axis scenarios**.
- **55.4 — Red Team rules of engagement + sourcing** (depends on 55.0 + 55.1; can run in parallel with 55.2/3).
- **55.5 — first quarterly Red Team week**.

Block 55 should not gate anything — the combined-fault matrix accumulates over many quarters. The first scenarios should be in place before Phase 3 production cutover; the full matrix is a multi-quarter program.

---

## 9. Open questions

- **Do we need a chaos-engineering tool (Chaos Mesh, Pumba, toxiproxy)** or are bash + tc + iptables + sigkill enough? First-cut answer: shell tools are enough for the named scenarios; consider Chaos Mesh if the matrix grows to 50+ scenarios where reuse-of-fault-injectors becomes the bottleneck.
- **Sponsor-side Red Team participation?** The threat-model §1 Tier-4 capability includes nation-state SIGINT — only a sponsor-side cell has realistic capability to emulate this. Coordination path TBD.
- **CI integration of combined-fault subset?** Most scenarios are too long for per-PR; a subset (C3-04, C3-08) could fit in ≤ 15 min and run on PRs touching ha-multi-site or signal-server. Pilot post-55.2.

---

## 10. References

- `docs/design/threat-model.md` — adversary tiers being validated
- `docs/design/failure-isolation.md` — single-axis failure modes
- [`docs/design/continuous-architectural-chaos.md`](continuous-architectural-chaos.md) — single-invariant continuous probing
- [`docs/adr/1008-soak-testing-discipline.md`](../adr/1008-soak-testing-discipline.md) — soak pattern (driver shape mirrored)
- [`docs/adr/1012-load-testing-discipline.md`](../adr/1012-load-testing-discipline.md) — load pattern
- `docs/findings/INDEX.md` — F-NNN findings
- `docs/measurements/INDEX.md` — M-NNN measurements
- `docs/project/validation-state.md` — verdict tracker
- Block 55 — combined-failure & Red Team — execution tracker
