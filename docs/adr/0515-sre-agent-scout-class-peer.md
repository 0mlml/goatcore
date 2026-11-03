# ADR 0515 — goat-sre-agent — first agent that issues outbound `cmd/`, as a Scout-class peer with action gating

| Field | Value |
|---|---|
| Status | Proposed |
| Date | 2026-05-06 |
| Repo scope | goat core |
| Supersedes | — |
| Superseded by | — |
| Related | [`0010-boundary-classification-escalation-and-medium.md`](0010-boundary-classification-escalation-and-medium.md), [`0112-tech-strategy-workload-shape-language-boundary.md`](0112-tech-strategy-workload-shape-language-boundary.md), [`0208-phoenix-native-peer-discipline.md`](0208-phoenix-native-peer-discipline.md), [`0411-ory-idp-mvp.md`](0411-ory-idp-mvp.md), [`0413-no-orm-in-our-go-code.md`](0413-no-orm-in-our-go-code.md), [`0500-zenoh-data-fabric.md`](0500-zenoh-data-fabric.md), [`0508-audit-logger-interceptor.md`](0508-audit-logger-interceptor.md), [`0511-publisher-trust-model.md`](0511-publisher-trust-model.md), [`0512-rapid-adaptation-with-ai.md`](0512-rapid-adaptation-with-ai.md), [`0513-wasm-conditioning-sandbox.md`](0513-wasm-conditioning-sandbox.md), [`0514-time-quality-attestation.md`](0514-time-quality-attestation.md), [`0805-policy-engine-active-passive.md`](0805-policy-engine-active-passive.md), [`0807-mesh-microsegmentation.md`](0807-mesh-microsegmentation.md), [`0811-audit-log-integrity.md`](0811-audit-log-integrity.md), [`0822-wide-event-observability-otlp-openobserve.md`](0822-wide-event-observability-otlp-openobserve.md), `0826-per-site-three-orin-topology.md` |

## TL;DR (for a layperson)

We want a local SWE/SRE agent — a process that watches the system, proposes fixes, and (eventually) applies them — without it becoming a god-shell that can wipe a database or lock us out of a host. The decision is to ship it as a **Scout-class peer** ([ADR 0512](0512-rapid-adaptation-with-ai.md)) named `goat-sre-agent`: one agent peer + many narrowly-scoped tool peers (e.g. `cmd/wg/peer-revoke`, `cmd/svc/restart`, `cmd/ansible/play`), each guarded by the existing [policy engine (ADR 0805)](0805-policy-engine-active-passive.md). Reads ride the queryable-by-default surface (Block 23). Writes route through the same dual-path approval the egress proxy uses, with humans answering on Matrix + ntfy. Promotion through three lifecycle modes — Shadow → Assisted → Autonomous — is per-action-class and earned by drill evidence, not by clock time. Inference is local on Orin-C (Ollama / vLLM); model selection is eval-first against past F-NNN incidents, not catalog-driven.

## Context

[ADR 0512 §"Trigger conditions for revisit"](0512-rapid-adaptation-with-ai.md) names the trigger that fires this ADR explicitly:

> **First agent that issues outbound `cmd/`.** Action gating against [`0805`](0805-policy-engine-active-passive.md) requires a per-agent decision-review.

Tipper (proof case 1) is publish-only. Scout (proof case 2) refuses outbound `cmd/` in v1 by design. `goat-sre-agent` is the first agent that will issue `cmd/` to mutate operational state — the per-agent decision-review that 0512 anchored to.

The user-facing prompt: *host a local software/network/site-reliability engineer agent inside the LAN, using local models (Llama / Kimi-Dev-72B / Qwen-Coder-class), able to reach services and fix them via Ansible, SSH, APIs, SQL.*

Two patterns dominate the off-the-shelf shape of this:

1. **Three-tier "safe-loop"** (Inference / Control / Execution) parallel to the existing fabric, with a generic SSH/SQL/Ansible execution surface guarded by regex blacklist + a "kill-switch webhook." Adds substrate next to the substrate we already have, and bets safety on a regex layer that grows new holes with every new fix.
2. **Generic agent platform** (Bedrock-Agents, AgentSpace, n8n) that hosts the agent + tools + audit + identity in one vendor stack. Sovereignty- and island-mode-incompatible per [ADR 0512](0512-rapid-adaptation-with-ai.md) §"Posture" and the no-public-internet invariant in `CLAUDE.md`.

What we already have in this repo that the SRE agent should reuse, not duplicate:

- **Agent-class peer pattern + queryable-by-default + citation discipline + ephemeral lifecycle** — [ADR 0512](0512-rapid-adaptation-with-ai.md), Block 23, design at [`../design/ai-rapid-adaptation.md`](https://github.com/usareur-af/goatdocs/blob/main/docs/design/ai-rapid-adaptation.md) + [`scout.md`](https://github.com/usareur-af/goatdocs/blob/main/docs/design/scout.md).
- **Per-site policy engine with dual-path approval** — [ADR 0805](0805-policy-engine-active-passive.md) + [Block 17A.5 design at egress-proxy.md §6](../design/egress-proxy.md). Spock-replicated, OIDC-abstract, Island-Mode-correct. Fast path (two-of-N peer ≠ requestor) and normal path (single role-holder + 24 h SLA, auto-escalates to fast).
- **Audit-by-construction** — [ADR 0508](0508-audit-logger-interceptor.md) + [ADR 0811](0811-audit-log-integrity.md) integrity tier. Citation discipline rejects zero-source emissions at the schema-enforcer interceptor.
- **Wide-event correlation** — [ADR 0822](0822-wide-event-observability-otlp-openobserve.md) `correlation_id` cross-plane joins.
- **Time-quality attestation** — [ADR 0514](0514-time-quality-attestation.md). Every emission carries a quality envelope; consumers filter / weight by quality.
- **WASM sandbox for externally-authored code** — [ADR 0513](0513-wasm-conditioning-sandbox.md). The *only* mechanism by which externally-authored code lands inside a host process we own.
- **Identity** — Ory MVP for service-account identity ([ADR 0411](0411-ory-idp-mvp.md)); request-scoped sub-identities per Scout's pattern.
- **Escalation medium** — Matrix Element + ntfy peer-with-PagerDuty per [ADR 0010](0010-boundary-classification-escalation-and-medium.md). Same fanout as alertmanager and the policy-engine notifier.
- **Inference host** — Orin-C utility host runs Ollama per ADR 0826 amendment 2026-05-06.

What is **missing**:

1. A per-agent decision for the first `cmd/`-issuing agent, naming the action surface it gets and the lifecycle by which write-authority is earned.
2. A typed-tool-peer pattern that decomposes generic SSH / SQL / Ansible into operations narrow enough to reason about per-class.
3. A lifecycle promotion contract — Shadow / Assisted / Autonomous — that pairs evidence with authority growth.

## Decision

Ship the SRE agent as **`goat-sre-agent`**: a Scout-class peer with an extended action surface gated structurally, not regex-style. Six load-bearing decisions:

### D1. The agent IS a Scout-class peer, not a parallel "control plane"

`goat-sre-agent` inherits the full Scout-class contract from [ADR 0512](0512-rapid-adaptation-with-ai.md) §"Bones": agent-class peer pattern, ephemeral request-scoped identity, citation discipline, queryable-by-default for read, audit-by-construction. Emits exclusively under `agents/sre/<incident-id>/...`. Cross-namespace publishing is rejected by the schema-enforcer interceptor against the announce manifest. The "incident" is the request scope — typically an alert page, a human-issued question, or an upstream-agent escalation; lifetime budget is a configurable wall-clock (default 60 min, ceiling 4 h) after which an incident escalates to human pickup rather than continuing autonomously.

The proposed three-tier ("Inference Zone / Control Plane / Execution Sandbox") parallel stack is rejected explicitly — see "Alternatives considered." We get inference (Orin-C Ollama), control (policy engine), and execution sandboxing (typed tool peers + WASM where externally-authored) from primitives already in the substrate.

### D2. Decompose the action surface into typed tool peers, not a generic shell

Generic SSH / SQL / Ansible gives the agent a god-shell with a flat blast radius. The action surface is instead a finite, growable set of **typed tool peers**, each its own peer in the fabric, each with:

- A single named `cmd/<peer>/<verb>` queryable / command (e.g. `cmd/wg/peer-revoke`, `cmd/pg/role-create`, `cmd/svc/restart-named`, `cmd/ansible/play`).
- An action-class identifier declared in `attest/action-class/<peer>/<verb>` carrying a plain-language description, blast radius, idempotency property, and SLA tier.
- Its own per-tool authorization in the policy engine — adding a new capability is a new tool peer + a new action-class registration + a new policy entry, **not** an addition to a regex blacklist.
- Its own implementation in normal Go or Rust per [ADR 0112](0112-tech-strategy-workload-shape-language-boundary.md). Tool peers are request-shaped → Go.

For the v1 charter, the typed tool peer set is:

| Peer | Verbs | Class | First-cut lifecycle |
|---|---|---|---|
| `pg-tools` | `query` (read), `role-create`, `role-revoke` | Database admin | Read-only first; writes Assisted only |
| `wg-tools` | `peer-list` (read), `peer-add`, `peer-revoke` | Mesh membership | Read-only first; writes Assisted only |
| `svc-tools` | `status` (read), `restart`, `stop`, `start` | Service lifecycle | `restart` Autonomous on non-load-bearing, others Assisted |
| `ansible-tools` | `play` (signed playbook ref + dry-run + lint pre-flight) | Playbook execution | Assisted only at v1 |
| `cert-tools` | `expiry-check` (read), `renew` (rotation) | TLS rotation | `expiry-check` Autonomous; `renew` Assisted |
| `log-tools` | `tail`, `grep` (both read) | Diagnostics | Autonomous |

A **generic SSH peer** is explicitly out of scope. Free-form remote shell breaks the typed-action discipline. Operations that don't fit any tool peer escalate to a human via the same Matrix / ntfy channel; the right outcome is "operator does it once, then we add a tool peer if it recurs," not "let the agent type bash."

### D3. Reads ride queryable-by-default; writes route through the policy engine

**Read actions** (everything under `cmd/<peer>/query` and the read verbs above) are unguarded at the policy-engine level — the queryable contract from [ADR 0512](0512-rapid-adaptation-with-ai.md) §1.2 already enforces identity-bound + audit-by-construction + citation discipline. The agent's parent identity gates topic visibility; it cannot read what its identity isn't authorized to read.

**Write actions** (every verb that mutates) route through the [Block 17A.5 policy engine](../design/egress-proxy.md) using the existing dual-path approval contract:

- **Normal path** — single role-holder approval + 24 h SLA from request submission timestamp. Auto-escalates to fast path on SLA breach.
- **Fast path** — two-of-N peer approval (≠ requestor), immediate activation; for urgency or SLA-breach release valve.
- **Per-action-class** policy: each `(peer, verb)` pair has its own policy row in the `action_policy` table. Approval-required-by default; explicit allow-list opt-in for autonomous classes (per D5 lifecycle).

The agent **cannot bypass**. There is no "kill-switch webhook" because there is no separate kill switch — the policy engine is the gate. Disabling autonomy on a class is a policy-row update, Spock-replicated to every site.

### D4. Approval surface uses existing Matrix + ntfy fanout, not a new webhook

Approval requests are emitted to the per-site policy engine's notification adapters per [ADR 0805](0805-policy-engine-active-passive.md) (already three-way: Matrix room `#agent-approvals:<site>.netbird.legacy.example` + email + isitup widget). Block 17A.5 pattern adds ntfy push parallel to alertmanager fan-out. Operator clicks Approve in Matrix or via ntfy push → posts to `cmd/policy-engine/approve` → the engine releases the held action.

Reusing the same notification surface that handles egress + microsegmentation + tamper-response approvals means:

- One human-facing surface, not three.
- One audit shape (Block 17A.5 `egress_approval` table semantics extend to `agent_action_approval`).
- Island-mode-correct by default (Matrix + ntfy are per-site sovereign substrates).

### D5. Lifecycle: Shadow → Assisted → Autonomous, per-action-class, drill-gated

Three modes exist per `(peer, verb)` pair, not per agent. Promotion is by drill evidence, not by clock or seniority:

| Mode | Authority | Gate to next mode |
|---|---|---|
| **Shadow** | Agent reads + plans + emits proposed action diff to `agents/sre/<incident-id>/proposed/...`. **No write authority on this class.** Operator reviews proposals daily. F-NNN finding draft on every false-positive proposal. | 10 successful Shadow proposals on a class (operator confirms "I would have approved this") + 0 unsafe proposals OR FAIL → root-cause finding before retry. |
| **Assisted** | Write authority gated on synchronous operator approval per D3/D4. Every action is an approved diff. | 30 successful Assisted approvals on a class + 0 production-impact regressions OR FAIL → root-cause + 30-count reset. |
| **Autonomous** | Write authority without per-action approval, scoped to the class. Audit + citation + wide-event emission unchanged. | Reverted to Assisted on first production-impact regression. Reverted to Shadow on second. Reverted permanently on third within rolling 90 d. |

`pg.role.create`, `wg.peer.add`, `wg.peer.revoke`, `cert.renew`, `ansible.play` for any role tagged `load-bearing: true` are **never autonomous** in v1 — Tier-3 SPoT-class per `threat-model.md` §3a. Promotion to Autonomous on these classes requires a separate ADR with operator + lead-engineer named consent. The default-deny here is structural.

### D6. Inference is local, eval-first, Orin-C-resident

Per [ADR 0112](0112-tech-strategy-workload-shape-language-boundary.md), the inference workload is **request-shaped** (LLM serves agent planner queries), not data-plane hot-path — Go-shaped at the serving boundary, with the actual model running under Ollama / vLLM. Orin-C is the inference host; Ollama is the v1 serving runtime per the goat-moon-pod precedent and the Orin-C utility host buildout.

**Model selection is eval-first**, not catalog-driven:

- An eval harness at `ops/sre-agent/eval/` replays past F-NNN findings as "incidents" against candidate models. Pass criterion: would-have-proposed-the-correct-fix at ≥ 70 % rate, with zero unsafe proposals.
- Candidates we *expect* to evaluate (not commitments): Qwen-Coder-32B (Q4 ~20 GB, cheap), Kimi-Dev-72B (Q4 ~40 GB, slower), Llama-3.3-70B-Instruct, GLM-4.5-Air. Pick the cheapest model that hits the bar.
- Two-model "action / reviewer" stacking is **not adopted by default** — the bar is set by measured net-positive on the eval harness, not by intuition. If a reviewer pass measurably lifts pass-rate, fine; if not, it adds latency without value.

The planner peer's external contract (input: question + catalog snapshot; output: structured plan) is unchanged from Scout — this preserves the planner-peer-boundary swappability from [ADR 0512](0512-rapid-adaptation-with-ai.md) §"The planner — swappable at the boundary."

### D7. Externally-authored tool plugins land via WASM only

If — at any future point — a third-party publishes an SRE tool plugin (vendor-supplied playbook generator, partner-supplied monitor adapter), the *only* mechanism by which it loads into a host process we own is the [WASM conditioning sandbox (ADR 0513)](0513-wasm-conditioning-sandbox.md): signed `.wasm`, fuel-bounded, no network / FS / env imports, instance-per-call. Internal tool peers stay native Go / Rust per [ADR 0112](0112-tech-strategy-workload-shape-language-boundary.md). The boundary is "externally-authored code" — internal team writes Go.

This forecloses the "drop a vendor plugin DLL into the agent" failure mode before it can be proposed.

## Alternatives considered

- **Three-tier "safe-loop" (Inference / Control / Execution) parallel stack with regex guardrail + kill-switch webhook.** Doubles the substrate next to the existing one. Bets safety on a regex blacklist that grows holes with every new capability. Doesn't compose with policy engine, audit, citation, or wide-event correlation. Rejected.
- **Generic vendor agent platform (Bedrock-Agents, AgentSpace, LangGraph-as-a-service).** Sovereignty + island-mode incompatible. Pulls model + tools + audit + identity into a vendor stack outside the accreditation boundary. Rejected for the secure plane; revisitable for unclassified partner-side work under [ADR 0821](0821-mission-partner-sharing-pattern.md).
- **Generic SSH peer with regex command blacklist + session recording.** A god-shell with a regex moat. Every new fix is a new regex hole. Audit is post-hoc rather than structural. Rejected — typed tool peers are the right primitive (D2).
- **One mode for all action classes (single Shadow→Assisted→Autonomous gate per agent).** Conflates blast radius — `log-tools.tail` and `pg-tools.role-create` cannot share a promotion gate. Per-class gating is the right granularity. Rejected.
- **Skip Shadow mode; start in Assisted directly.** Assisted requires synchronous human approval for every action; Shadow is the cheaper way to surface false-positive proposals before any human time is on the line. Rejected — Shadow's marginal cost is small and the failure-mode-discovery value is large.
- **Pick a specific model now (Kimi-Dev-72B + Llama-3.3-70B-reviewer + Llama-Guard 3).** Premature lock-in without an eval harness. The bar should be set by measured success on past incidents, not by what's catalog-popular. Rejected — D6 eval-first instead.
- **Two-model action+reviewer stack as default.** Adds latency unconditionally; lift varies by model. Rejected as default; allowed if eval harness measures net-positive.
- **Persistent (non-ephemeral) long-running agent peer with JIT-elevated identity (Block 18B PAM shape).** Simpler to operate but bounds blast radius worse and conflicts with the request-scoped audit pattern Scout established. Rejected at v1; revisitable when a class of work is structurally long-running (e.g. continuous auto-remediation) and the cost / benefit measurement supports it.
- **Add a separate "agent-only" approval path bypassing the policy engine.** Convenient, structurally wrong — every action surface that exists outside the policy engine is an exfil channel for the next compromise. Rejected. The policy engine IS the surface.
- **Build the agent before Block 23 ships.** The bones (queryable-by-default, citation, schema-enforcer interceptor) are the safety architecture; building before they land trades structural safety for ad-hoc safety. Rejected — sequencing locks Block 23 before Block 69.

## Consequences

**Positive.**

- First `cmd/`-issuing agent ships with a per-agent decision-review on the record (closes the Block 23 trigger).
- The SRE agent reuses ~80 % of the substrate (queryable, citation, audit, policy, identity, escalation, time-quality, wide-event) — marginal cost of the agent is the agent + tool peers + lifecycle drills, not a new substrate.
- Typed tool peers cap blast radius by design; new capabilities require new ADR-shaped surface decisions, not regex amendments.
- Lifecycle gating (Shadow → Assisted → Autonomous) earns authority from drill evidence, not narrative; per-class granularity matches actual blast-radius distribution.
- Inference choice deferred to eval-harness measurement; no model lock-in. Planner-peer boundary preserved; future model swaps are config flips.
- Externally-authored tool plugins are structurally constrained to the WASM sandbox before any partner / vendor can propose DLL-shaped delivery.
- Approval UX is the same one operators already know (Matrix + ntfy + isitup widget). One human-facing surface; one audit shape.

**Negative.**

- Tool-peer count grows with capability scope. Mitigation: thin tool peers (~few hundred LoC each); shared helper crate at `ops/sre-tools/lib/`.
- Action-class registry adds publishing friction (every new `(peer, verb)` declares its action class). Mitigation: friction is the feature — the friction is what makes the surface inspectable.
- Drill discipline (Shadow → Assisted → Autonomous) is real engineering investment; without it the lifecycle promotion is theater. Mitigation: pair with [ADR 1008 soak discipline](1008-soak-testing-discipline.md) shape; the eval harness IS the drill substrate.
- Eval-first model selection delays "we have a model picked" by the time it takes to assemble an eval set. Mitigation: 30 past F-NNN incidents is sufficient for a v1 bar; harness is reusable per future model bump.
- Inference on Orin-C contends with other utility-host workloads (Forgejo, apt-cache, registry mirror). Mitigation: Ollama supports CPU/GPU split + queueing; if contention emerges, queue depth is the metric; second Orin-C class instance is a compose-level decision.
- Autonomous-on-non-load-bearing creates a class line that future capability creep will press on. Mitigation: the class line is named explicitly in `threat-model.md §3a`; promotion past the line requires an explicit ADR.

**Out of scope for this ADR.**

- Specific model pick (deferred to D6 eval harness output).
- Specific tool-peer wire schemas (design doc + per-peer ADRs as they land).
- Multi-agent SRE coordination (one agent, multiple tool peers — yes; multiple SRE agents on one incident — deferred).
- Cross-site agent state (per-site SRE agent today; federation pattern deferred to Phase 4).
- Adversarial-prompt robustness on the planner LLM (shared concern with Scout per [ADR 0512](0512-rapid-adaptation-with-ai.md) §"Out of scope"; addressed at the planner-peer-boundary, not in this ADR).
- Long-running auto-remediation pattern (covered by D7 alternative; revisitable per trigger).

## Trigger conditions for revisit

- **First Autonomous-class promotion on a `load-bearing: true` action.** Requires its own ADR with named operator + lead-engineer consent.
- **First production-impact regression in Assisted mode.** Action-class reverts to Shadow per D5; root-cause finding documents the gap.
- **First action that doesn't fit any tool peer and recurs.** Spawns a new tool-peer ADR + addition to the action-class registry; no shoehorning into an existing peer.
- **Eval-harness pass-rate falls below 70 % on the deployed model after a model update.** Roll back; re-evaluate.
- **First externally-authored tool plugin proposed.** Confirms WASM sandbox path per D7 and [ADR 0513](0513-wasm-conditioning-sandbox.md).
- **Long-running auto-remediation pattern earns its case.** Revisits the request-scoped-vs-persistent decision (D2 alternative).
- **Multi-agent SRE coordination becomes load-bearing.** Formalize agent-to-agent semantics on top of the Scout-class contract.
- **Approval volume saturates Matrix + ntfy fanout.** Revisit batching / per-class digest patterns.

## Assessor view (SCA-R)

This decision serves AC-3 (access enforcement), AC-6 (least privilege), AC-2 (account management for the agent's per-incident sub-identity), AU-2 / AU-3 / AU-12 (audit content + records + generation), AU-6 (audit review and analysis — citation discipline + wide-event correlation), CM-2 (baseline config — action-class declarations are queryable), CM-7 (least functionality — typed tool peers + per-class policy), IR-4 (incident handling — agent-issued actions are auditable by construction), and SI-4 (system monitoring — Shadow-mode proposals are themselves emissions).

The lifecycle gating (Shadow → Assisted → Autonomous, per-action-class, drill-gated) operationalizes AC-6 — least privilege is a measured property, not an asserted one. The default-deny posture for `load-bearing: true` actions never reaching Autonomous v1 is itself an SCA-R-relevant property: the most consequential surface area is structurally human-in-the-loop.

The model + serving stack choice is deliberately deferred to the eval harness in D6; when concrete, it returns through its own ADR. The narrow platform-shaped commitment at the inference layer (Ollama as v1 serving runtime, swappable at the planner-peer boundary) inherits the same posture analysis from [ADR 0512](0512-rapid-adaptation-with-ai.md) §"Posture."

WASM-only externally-authored tool plugin policy (D7) inherits the [ADR 0513](0513-wasm-conditioning-sandbox.md) sandbox-property argument: SI-3 (malicious code protection), SC-39 (process isolation), and the structural property that "loading external code without the sandbox" is not an alternative the agent's code can reach.

---

*See `GLOSSARY.md` for terms and acronyms.*
