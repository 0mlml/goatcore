# ADR 1014 — Codex pattern: ADR-to-enforced-lint

| Field | Value |
|---|---|
| Status | Proposed |
| Date | 2026-05-04 |
| Repo scope | goat core + every consumer-repo where the same ADR series applies |
| Supersedes | — |
| Superseded by | — |
| Related | [`0001-adr-template.md`](0001-adr-template.md), [`0002-adr-lifecycle.md`](0002-adr-lifecycle.md), [`0006-multi-agent-human-concurrency-governance.md`](0006-multi-agent-human-concurrency-governance.md), [`0007-multi-agent-orchestration-hierarchy.md`](0007-multi-agent-orchestration-hierarchy.md), `0009-worker-discipline-mechanisms` (Backlog per [INDEX.md](INDEX.md)), [`0208-phoenix-native-peer-discipline.md`](0208-phoenix-native-peer-discipline.md), [`0224-progressive-config-rollout-with-auto-revert.md`](0224-progressive-config-rollout-with-auto-revert.md), [`0317-per-service-failure-mode-declaration.md`](0317-per-service-failure-mode-declaration.md), [`0413-no-orm-in-our-go-code.md`](0413-no-orm-in-our-go-code.md), [`1008-soak-testing-discipline.md`](1008-soak-testing-discipline.md), [`1012-load-testing-discipline.md`](1012-load-testing-discipline.md). Prior art: [Cloudflare, "Code Orange: Fail Small, Fail Complete," May 2026](https://blog.cloudflare.com/code-orange-fail-small-complete/) §"The Codex: Institutional Memory via Enforced Standards." |

## TL;DR (for a layperson)

Today our ADRs explain *why* we do things a certain way, and our contributor guide explains *how*, but nothing automatically enforces the rules at code-review time. A new contributor reading the ADRs has to remember each rule and apply it; a tired reviewer can miss a violation. Cloudflare adopted a discipline of turning each rule into an automatic check that flags violations the moment a contributor opens a pull request. We adopt the same pattern. Each ADR that is enforceable as a mechanical check declares which check enforces it; CI fails the pull request when the check fails. Phoenix-native peer discipline gives us a quiet additional benefit: once a rule is enforced at PR time, it propagates through the fleet automatically as services rebuild from the supply chain, with no separate "deploy the rule" step.

## §1 Vocabulary

- **Codified rule** — a single mechanical check derived from an ADR's decision section, runnable in CI, that returns pass / fail / warn for a given pull request.
- **Enforcement strength** — one of: `block` (CI fails the PR on violation), `warn` (CI surfaces the violation without failing), or `manual` (no CI check; reviewer responsibility, recorded for visibility).
- **Retrofit** — the act of adding an enforcement column entry to an existing ADR that did not previously have one.
- **Supply-chain rollout** — the process by which a newly-enforced rule reaches running services: it lands in CI at PR time; new builds incorporate the rule; Phoenix-native peer rebuild cadence (ADR 0208) propagates the new build to running services without a separate deployment step.

## §2 Context

Three forces converged.

**1. ADR series has accumulated enforceable rules with no enforcement layer.** Several ADRs already declare rules that are mechanically checkable but checked only by humans: ADR 0413 (no ORM in Go code), ADR 0411 / Ory section "no out-of-band SQL on Ory tables," ADR 0224 §3.7 (Ory carve-out from rollout), Block 50A (http.Server timeouts on every Go HTTP service, `goleak.VerifyTestMain` in every load-bearing Go package), CONTRIBUTING.md sections on signed commits and DCO sign-off. Each has a clear pass/fail definition and could be a CI check. None is.

**2. Cloudflare's post-mortem named "code that assumed inputs would always be valid" as the root of both November and December 2025 outages.** Their remediation was a Codex rule ("services MUST validate that upstream dependencies are in an expected state before processing") plus AI-powered code review enforcement. We have the same shape of risk; Block 50A's "panic-recovery wrapper for every goroutine entry point" is a similar invariant. Without enforcement, future code drifts away from the invariant under deadline pressure.

**3. Phoenix-native discipline gives us a property Cloudflare cannot leverage: rule deployment is free.** When a Codex rule lands in CI as `block` strength, it constrains every future PR. Newly-built service binaries embody the rule. Phoenix-native peer rebuild cadence (ADR 0208) replaces running services with the new builds on its normal schedule. There is no separate "promote the rule from staging to prod" step. The deployment gradient is structural, not procedural.

## §3 Decision

Adopt the **Codex pattern** as the enforcement layer beneath the ADR series.

### 3.1 ADR INDEX gains an "Enforcement" column

Each row in [`docs/adr/INDEX.md`](INDEX.md) gains a new column listing the enforcement mechanism for that ADR's decision. Permitted values per ADR: a CI check name (e.g., `golangci-lint:no-orm`, `clippy:no-unwrap-outside-tests`), a script path (e.g., `ops/ci/check-http-timeouts.sh`), the literal `manual` (reviewer responsibility, no automation), or `n/a` (the ADR is meta or strategic and has no mechanically-checkable decision).

ADRs in the meta band (0000-0010, 0100-0112) are predominantly `n/a` because their decisions are strategic, not mechanical.

### 3.2 Enforcement strength is `block` from day one

A new Codex rule lands at `block` strength on the day its CI check ships. Cloudflare's post implies the same posture; the warn-then-block ladder is unnecessary because Phoenix-native rebuild cadence handles the deployment gradient.

The only exception is when an existing codebase has known violations that cannot be fixed in the same PR that introduces the rule. In that case, the rule lands at `warn` strength with a named cleanup ticket (a Block sub-task in implementation-plan.md) and a deadline; promotion to `block` happens in the PR that closes the cleanup ticket. The matrix carries the deadline; an open `warn` rule past its deadline is a finding.

### 3.3 Rule deployment is structural, not procedural

A Codex rule landing in CI does not require a separate deployment to running services. The path is:

1. Rule lands in CI at `block` strength.
2. Future PRs cannot merge code that violates the rule.
3. The next supply-chain build incorporates the rule's effect (refactored code conforms).
4. Phoenix-native peer rebuild cadence replaces running service instances with the new build on its normal schedule.

No "promote rule from cohort A to cohort B" mechanism is needed. The Phoenix discipline (ADR 0208) is what makes this property hold; non-Phoenix substrate would require a separate rollout mechanism.

### 3.4 Initial retrofit set: five high-leverage rules

The first implementation pass adds enforcement column entries to five ADRs:

1. **ADR 0413 (no ORM in our Go code)** → `golangci-lint` custom rule banning imports of common ORM packages (`gorm.io/gorm`, `github.com/uptrace/bun`, `github.com/go-pg/pg`, etc.).
2. **Block 50A (http.Server timeouts mandatory)** → custom `ops/ci/check-http-timeouts.sh` that ASTs Go files for `http.Server{}` literals and asserts `ReadHeaderTimeout` / `ReadTimeout` / `WriteTimeout` / `IdleTimeout` are all set.
3. **Block 50A (`goleak.VerifyTestMain` mandatory)** → `golangci-lint` custom rule asserting every `_test.go` package has a `TestMain` calling `goleak.VerifyTestMain`.
4. **Block 23 (schema declaration mandatory)** → schema-enforcer interceptor (already running) extended with a CI dry-run mode that flags PRs adding publishes to undeclared topics.
5. **CONTRIBUTING.md "no plaintext secrets in committed files"** → `gitleaks` or equivalent secret-scanning, run on every PR; existing pre-commit hook augmented with the same check.

Subsequent retrofits land per-PR as the rule pattern proves itself.

### 3.4a Per-PR retrofit log

Rules added after the initial five via the per-PR pattern. Each row links the finding(s) the rule guards against, the script that implements it, and the CI gate that runs it.

| # | Rule | Findings guarded | Script | CI gate |
|---|---|---|---|---|
| 6 | Every Ansible task that POSTs/PUTs to Hydra `/admin/clients` MUST include both `skip_consent` and `access_token_strategy` in the body | F-077, F-078 | [`ops/ci/check-hydra-client-invariants.py`](../../ops/ci/check-hydra-client-invariants.py) | Wire into `.github/workflows/ansible-lint.yml` (or equivalent) on PRs touching `ops/ansible/**` — landed alongside Block 21D.1 closeout 2026-05-08 |

### 3.5 Out of scope at v1

- **AI-powered code review.** Cloudflare's Codex uses AI review on top of mechanical checks. Block 48H (AI PR review tooling) is the relevant landing place; this ADR does not commit to the AI layer, only the mechanical-check layer beneath it. The two compose when 48H lands.
- **Vendor / partner code.** Codex rules apply to internal code in goat repos. Vendor-supplied SDKs and partner-supplied WASM modules (Block 28) are out of scope; their conformance is governed by the Vendor Data Services Provider Charter and the Block 28 sandbox respectively.
- **Pre-existing code mass-cleanup as a precondition.** Rules landing as `warn` against pre-existing violations is the explicit mitigation; no big-bang cleanup is required before the first rule ships.

## §4 Alternatives considered

- **Status quo: human reviewer enforcement only.** Rejected — every ADR cited above has had at least one violation slip through review historically; the cost of a missed violation is unbounded (unbounded blast radius on misuse of `.unwrap()` or missing http.Server timeout); the cost of CI enforcement is bounded.
- **Warn-then-block ladder universal.** Rejected as default — Phoenix-native rebuild cadence makes the ladder unnecessary; warn is reserved for the legacy-violations case in §3.2.
- **Separate Codex repo or service.** Rejected — over-engineered for our scale; rules ride existing CI surfaces (golangci-lint, clippy, custom scripts in `ops/ci/`).
- **Tag rules in CONTRIBUTING.md instead of ADR INDEX.** Rejected — the source of truth for rules is the ADR that decided them; CONTRIBUTING.md is the contributor-facing summary; the enforcement column belongs with the decision, not the summary.

## §5 Consequences

**Positive:**
- Rules previously enforced only by reviewer attention become enforced by CI. Drift cost drops.
- The ADR INDEX gains an at-a-glance view of what is enforced where; an assessor or new contributor reads one column to learn the enforcement posture.
- Phoenix-native rebuild cadence (ADR 0208) gains another load-bearing property: it is the deployment vehicle for newly-enforced rules.
- The "validate upstream dependencies are in expected state" Codex rule from Cloudflare's post becomes a candidate for an enforced check in Block 50A's residual-tier work.

**Negative / accepted trade-offs:**
- **Per-rule CI cost.** Each new check adds CI runtime. Mitigated by checking only changed files where possible and parallelizing across the existing CI matrix.
- **False-positive risk.** A poorly-tuned check blocks legitimate PRs. Mitigated by `warn` mode for new rules during a one-week shake-down before promotion to `block`.
- **Discipline cost: every new ADR with a mechanical decision must declare its enforcement.** Mitigated by `n/a` being a permitted value; the discipline is "decide explicitly," not "always enforce."

## §6 Assessor view (SCA-R)

This ADR maps to NIST SP 800-171 rev 2 §3.4.3 (track, review, approve / disapprove, log changes — strengthened by automated enforcement at change time) and §3.4.5 (define, document, approve, enforce physical and logical access restrictions associated with changes — extended to code-level rules). The pattern is a code-side analog to the configuration-management controls already in the ADR series.

The "rules deploy via supply-chain rebuild" property (§3.3) is novel; an assessor reviewing it should confirm Phoenix discipline (ADR 0208) is enforced in practice. Without that, rule deployment becomes a manual restart-each-service step, which is workable but loses the structural guarantee.

The retrofit list (§3.4) is the verifiable v1 commitment. An assessor reviewing during the implementation window should confirm those five rules ship before this ADR moves to Accepted.

## §7 Trigger conditions for revisit

- **Five or more rules accumulate at `warn` strength past their cleanup deadlines.** The warn-mode escape valve is being abused; tighten the discipline.
- **A blocked PR triggers a known false-positive that takes more than two days to fix.** The cost of CI enforcement is exceeding the cost of human-reviewer enforcement for that rule; downgrade the rule to `manual` until the check is more accurate.
- **Phoenix discipline (ADR 0208) weakens — rebuild cadence is paused or skipped.** The structural deployment property in §3.3 no longer holds; the warn-then-block ladder may need to return as a deployment gradient mechanism.
- **An ADR is updated and its enforcement check no longer matches the new decision.** Update the check in the same PR; this is a Codex meta-rule, enforceable by a script that diffs the enforcement column against ADR Decision sections.

## References

- [`docs/adr/INDEX.md`](INDEX.md) — gains the new Enforcement column; canonical surface for the pattern.
- `docs/project/implementation-plan.md` Block 63C — execution plan (CI scaffolding + five named retrofits).
- [Cloudflare, "Code Orange: Fail Small, Fail Complete," May 2026](https://blog.cloudflare.com/code-orange-fail-small-complete/) — prior art and the "Codex" name.
- [ADR 0208](0208-phoenix-native-peer-discipline.md) — supply-chain rollout property in §3.3 depends on this.
- [ADR 0413](0413-no-orm-in-our-go-code.md) — first named retrofit.
- See GLOSSARY.md for acronym expansions used in this ADR.
