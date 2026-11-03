# ADR 0010 — Boundary classification, in-flight escalation triggers, and escalation medium for agent-to-human handoff

| Field | Value |
|---|---|
| Status | Proposed (2026-05-01) |
| Date | 2026-05-01 |
| Repo scope | Multi |
| Sub-series | `[multi-agent-orchestration]` |
| Supersedes | — |
| Superseded by | — |
| Related | [`0006-multi-agent-human-concurrency-governance.md`](0006-multi-agent-human-concurrency-governance.md), [`0007-multi-agent-orchestration-hierarchy.md`](0007-multi-agent-orchestration-hierarchy.md), `0008-cluster-branching-and-ai-review-tooling.md`, `0009-worker-discipline-mechanisms.md`, [`0812-per-site-matrix.md`](0812-per-site-matrix.md), [`0815-observability-stack.md`](0815-observability-stack.md) |

## TL;DR (for a layperson)

Some work belongs to humans, not agents — strategic calls, sensitive negotiations, hardware operations, hard-to-reverse decisions, deep judgment problems. **We classify every piece of work into one of seven categories at pickup time** so an agent only takes on what it can actually do well, with the rest routed to a named human. **Mid-flight, agents have specific signals to halt and escalate** — not because they're stuck on something hard, but because the work is genuinely outside their effective competence (it touches a security signature, it's the third try without progress, it would change a load-bearing rule with a single line). When an agent escalates, **we want it to reach the right human fast and exchange information richly** — so we use a phone-style notification (Matrix push, ntfy, optionally PagerDuty) to get attention quickly, and a structured GitHub Issue for the actual back-and-forth conversation. Same notification system also serves operational alerts (Block 17 site-reliability paging) — one sovereign per-site notification server, two consumer flows.

## Context

[ADR 0007](0007-multi-agent-orchestration-hierarchy.md) defines the hierarchy (program orchestrator / captain / worker) for 6-10 parallel threads. That hierarchy assumes a question we haven't answered yet: **what work should agents pick up at all?** And the related question: **when, mid-flight, should an agent stop and call for a human?**

Both questions have known failure modes. "Agents do everything; ask for help when stuck" degenerates: agents either (a) never ask (over-confidence), or (b) ask too much (cargo-cult escalation), or (c) ask in ways that don't actually surface what the human needs to know. Recent Claude models (per `../research/agent-team-management-sota-2026-05.md` §1.9) exhibit observable inconsistent-activation, state-loss-on-rejection, no-mid-stream-dialogue, and operator-bottleneck failure modes — these are documented anti-patterns, not theoretical risks. The Stanford April 2026 result also tells us multi-agent fails for *single-task multi-hop reasoning*, which means classification has to distinguish "parallel execution on distinct work" from "single hard reasoning question that should go to one (probably human) decision-maker."

Companion concern: **the medium for agent-to-human handoff matters.** Naive options collapse pager and interactive-medium into one tool (Slack / email / "just spawn a Claude session"); each fails at one job or the other. The right shape is two layers: a fast-attention pager for urgency, a rich persistent thread for the actual conversation. Goat already runs Matrix per-site ([ADR 0812](0812-per-site-matrix.md)); GitHub is core; sovereign-aligned `ntfy` is free and self-hostable. PagerDuty's SMS + voice fallback is the differentiator for genuinely-degraded-connectivity scenarios but is paid SaaS.

The same per-site notification substrate that serves agent-to-human escalation also serves the **site-reliability pager** (`../design/failure-isolation.md` §9 currently wired to PagerDuty only). Adding ntfy as a peer to PagerDuty for SRE alerts (concurrent fan-out, neither is fallback) and reusing the same per-site ntfy instance for both flows is the minimal-surface choice.

## Decision

**Three tightly-coupled pieces.** Codified in [`../design/multi-agent-team-management.md`](https://github.com/usareur-af/goatdocs/blob/main/docs/design/multi-agent-team-management.md) §6.5a + §6.5b + §6.5c + §6.5d.

### Piece 1 — Pre-pickup classification (seven-class rubric)

Every track is classified at pickup into one of three eligibility tiers — `agent-eligible`, `captain-eligible`, `human-only` — plus a class label `none` or `C1..C7`:

- **C1** — Strategy / values / policy judgment (decision belongs to the human, not the analysis)
- **C2** — Cross-organizational / interpersonal (trust, relationship, accountability)
- **C3** — Hardware / physical-world / one-shot ops (cannot be delegated)
- **C4** — Novel / unprecedented architectural calls (no prior art; multiple valid approaches; hard-to-reverse)
- **C5** — High-blast-radius / hard-to-reverse (production push; force-push; credential rotation)
- **C6** — Senior-engineer-judgment problems (depth + calibrated intuition)
- **C7** — Trust / security / compliance signature (the *signature* needs to be human even when the *answer* is clear)

Default when classification is unclear: **`human-only`** — bias toward asking, not toward acting. Classification fields land in `../project/active-work.yaml` cluster + track rows. Per-cluster routing-to-right-human table per the kickoff memo (`../project/cluster-kickoffs/TEMPLATE.md` §6).

### Piece 2 — In-flight escalation triggers

Worker mid-flight escalates when ANY of:

- Three consecutive halts under the progress-shape spin-detection mechanism (ADR 0009 §6.4.3) — strong signal it's a class-C4 or C6 problem the worker mis-classified.
- A single-line fix would change a load-bearing invariant (config-line change is healthy laziness; config-line change to an ADR-named decision is class-C1).
- Natural diff exceeds threshold of cross-cluster touch — class-C4.
- Worker is about to make a decision that names a value/policy choice — class-C1.
- Work hits a class-C7 surface (offline CA, secrets, accreditation posture) — automatic halt, no override.
- **Cost-budget tripwire** (carefully scoped to avoid the wrong-metric trap): if estimated cost exceeded by ≥3× AND progress-shape detection has fired ≥2 times, escalate by *cost-of-continuing*, not by time elapsed. Time-budget tripwires are explicitly DROPPED per ADR 0009 net-positive analysis — wrong metric.

**Pushback affordance.** Agents push back on humans handing them work that's miscategorized: *"I think this is human-only because X; before I proceed, can you confirm or reclassify?"* Distinct from gatekeeping (per `feedback_no_gatekeeping.md`); pushback on miscategorization is praised, not penalized.

### Piece 3 — Escalation medium (two layers, do not collapse)

**Pager layer (asynchronous attention-getting):**

| Severity | Triggers | Recommended pager |
|---|---|---|
| **sev-1** (≤1 min response) | C5 about-to-be-destructive; C7 trust/security; production-incident-class | **Matrix priority-room with Element mobile push** (sovereign, [ADR 0812](0812-per-site-matrix.md)). **Optional PagerDuty escalation** for after-hours / degraded-connectivity coverage. **Sovereign sev-1 alternative:** ntfy (free, self-hosted, FCM/APNS push) layered with Matrix. |
| **sev-2** (≤30 min, in-hours) | C4 architectural; C6 senior-judgment; C1 strategy in-hours | **Matrix room @-mention** in per-cluster room. |
| **sev-3** (hours-acceptable) | C2 cross-org; C1 batch | **GitHub @-mention** on escalation issue + Matrix daily-batch summary. |

**Interactive medium — GitHub Issue with structured "Escalation" template at `.github/ISSUE_TEMPLATE/escalation.md`.** Title `[escalation/<class>] <track-name> — <one-line question>`; body the structured packet (Question / What I know / What I've ruled out / What I'd lean toward / What I need from you); assignee the routed-to human; linked from `active-work.yaml` track row.

**Pager → interactive flow:** agent hits trigger → opens GitHub Issue → posts to Matrix at right severity tier with link → human's mobile push → reads one-liner → taps through to issue → responds in thread → resolution links to durable artifact (commit / ADR / PR / re-classification).

### Same ntfy substrate, two flows

Per-site self-hosted ntfy server (single Go binary; mesh-internal at `ntfy.<site>.netbird.legacy.example`). Topics:

- `goat-sre-<site>-critical` / `goat-sre-<site>-warning` — site-reliability alertmanager paging per `../design/failure-isolation.md` §9 update 2026-05-01. **ntfy adopted as peer to PagerDuty, not fallback** — concurrent fan-out, both fire when both configured.
- `goat-escalations-<cluster>-<sev>` — Block 48J agent escalations.

Same server, distinct topics, distinct subscribers. Sovereign-aligned; zero marginal cost.

## Alternatives considered

- **"Agents handle everything; humans review PRs."** Doesn't work for class-C5 (high-blast-radius) where human authority must precede action, or class-C7 (signature must be human). Naive flat permission model.
- **Time-budget escalation tripwires.** Considered + DROPPED per ADR 0009 net-positive analysis: time penalizes legitimate slow progress on hard problems and misses spinning-fast-without-progress. Replaced by progress-shape spin-detection.
- **Slack threads for interactive medium.** Rejected on Block-30 sanitization grounds (third-party diff exposure same concern as CodeRabbit per ADR 0008).
- **Custom web UI for agent↔human conversation.** Rejected on minimal-surface preference; we have GitHub Issues + Matrix already.
- **PagerDuty alone for both flows.** SaaS-only; per-seat cost; no sovereign alternative.
- **ntfy alone, no PagerDuty.** Loses SMS + voice fallback for genuinely-degraded connectivity scenarios. Operating both as peers (per `../design/failure-isolation.md` §9) gives both sovereign-aligned IP-push and SaaS-strength fallback.
- **Email-only escalation.** No urgency discrimination; thread fragmentation; not designed for back-and-forth; rejected.
- **Direct Claude Code session as primary interactive medium** (human spawns + joins agent context). High-fidelity for class-C4/C6 deep diagnostic; doesn't scale to 6-10 escalations. Reserved as last-resort for the hardest cases.

## Consequences

**Positive.** Routes the right urgency to the right human via the right medium with rich context preserved. Prevents agent-induced harm in the 7 named classes. Surfaces work humans *should* do without forcing every escalation through one bottleneck. Sovereign-aligned (Matrix per-site already deployed; GitHub already core; ntfy is single Go binary). Same substrate serves two flows (SRE incidents + agent escalations), so per-site ops cost is amortized. Backward-compatible: existing PagerDuty-only deployments unaffected until operator configures `ntfy_alertmanager_webhook_url`.

**Negative.** Classification overhead at pickup (~30 sec/track captain time). Risk of cargo-cult "ask the human about everything" anti-pattern; mitigated by named seven-class rubric (specific tells, not vibes-based) + reviewer-role gate catching scope outside agent-eligibility at PR time. Risk of mis-classification (work classified human-only that an agent could have handled, or agent-eligible that should have been human-only); mitigated by §6.5d pilot validation tracking false-positive + false-negative rates with weekly captain rubric refinement. Pushback dynamic adds occasional friction with humans handing miscategorized work; mitigated by praise-not-penalty norm.

**Residual risk.** The **rubric is tactical/observational** (per `../research/agent-team-management-sota-2026-05.md` §0): failure modes shift as Claude model behavior shifts. **6-month freshness budget** — re-survey on or before 2026-11-01. Carried in Block 32 quarterly engineering-health scorecard. ntfy push fidelity is IP-only; for genuinely-degraded-connectivity sev-1 we rely on optional PagerDuty (or Twilio SMS if PagerDuty cost is too high — see human-action H12).

## Assessor view (SCA-R)

This ADR establishes the human-in-the-loop discipline for an AI-augmented development workflow. **Control-family relevance:**

- **AC-3 / AC-4 (Access Enforcement / Information Flow Enforcement).** The seven-class rubric + escalation triggers are the operational analog of access-control — work hits class-C7 surface (offline CA, secrets, accreditation) → automatic halt, no override; class-C5 (high-blast-radius) → mandatory human authority before action.
- **CA-7 (Continuous Monitoring).** Escalation outcomes feed Block 27 wide-event observability + Block 17D audit-integrity ledger; class-C7 escalations resolve with audit-log entries.
- **IR-4 (Incident Handling).** SRE-pager (PagerDuty + ntfy peer fan-out per `../design/failure-isolation.md` §9) is the production-incident path; class-C5/C7 agent escalations route through the same severity-tier framework with separate topics.
- **AT-3 (Role-based training).** Routing-to-right-human table named per cluster (per the kickoff memo §6) is the operational analog of role-based-training-record for agent participants.

No waiver dependencies. Accreditation pathway: consistent with cATO (ADR 0100) — every escalation produces a durable artifact (GitHub Issue + audit-integrity ledger entry for class-C7). Residual risk: rubric drift (tactical claim, 6-month freshness) is the named watch; weekly captain-led false-positive/false-negative tracking is the compensating control.

## Trigger conditions for revisit

- Block 48D pilot reveals the seven-class rubric mis-classifies common cases (FP + FN rates fail to converge below ~5% within first 4 weeks of operation).
- 6-month freshness budget expires (on or before 2026-11-01) without scheduled re-survey of the rubric against current Claude model behavior.
- Escalation latency in production exceeds 30 sec from trigger to human-mobile-push (named verdict criterion in Block 48J) — points at infrastructure issue, not rubric.
- Human-orchestrator review-time-per-day exceeds 60 min sustained over a week — Devin-pattern manager-of-Devins ceiling has been hit; either rubric needs tightening (more `agent-eligible`) OR cluster count needs reducing.
- A class that is not in the seven categories surfaces repeatedly in production work — add C8 (or revise an existing class) with full rubric refinement pass.
- ntfy mobile push fidelity degrades materially (FCM/APNS deprecation; UnifiedPush adoption shifts).
- Block 30 public-surface sanitization stalls and external AI review tools cannot activate — interactive-medium-only-via-GitHub becomes a bottleneck instead of just a default.

## Glossary

See `GLOSSARY.md` for project-specific terms (`Cluster kickoff memo`; `multi-agent-orchestration` sub-series tag; `ntfy`; `Mainline tenacity`; `Fast/slow inchworm`).

## Status flip criterion

Proposed → Accepted on three signals:

1. Block 48D pilot completes with green verdict including a deliberate-classification test (a class-C5 PR submitted as `agent-eligible` auto-halts at the reviewer-role gate) AND a deliberate-escalation test (worker hits 3 consecutive progress-shape halts → opens GitHub Issue + Matrix posts at right severity + ntfy fires within 30-second budget).
2. First 4 weeks of production use post-48F yield rubric false-positive + false-negative rates that converge below 5% (per §6.5d weekly captain review).
3. At least one in-production class-C7 escalation resolves correctly (audit-integrity ledger entry created; security/compliance reviewer signature applied; cluster work resumes).
