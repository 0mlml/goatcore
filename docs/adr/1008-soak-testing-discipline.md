# ADR 1008 — Soak-testing discipline for stateful + replicated components

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Supersedes | — |
| Superseded by | — |
| Related | [`1006-phase-gates.md`](1006-phase-gates.md), [`1007-wave-style-validation.md`](1007-wave-style-validation.md), [`1003-cgnat-aware-validation-default.md`](1003-cgnat-aware-validation-default.md), [`0411-ory-idp-mvp.md`](0411-ory-idp-mvp.md), `../project/idp-decision-review.md` |

## TL;DR (for a layperson)

Anything that holds state on disk and survives restarts — a database, a
replicated identity store, a backup pipeline — has to keep working under
sustained pressure, not just in a happy-path bring-up. Before we bet
operations on such a component, we run it under continuous churn for a
fixed window (a "soak"), record one machine-readable evidence line per
tick, and only then mark the component fit for production. Soak testing
is how we move from "the docs say it works" to "we have evidence it
works on our exact stack at our exact load."

## Context

Several recurring incidents made the absence of formal soak testing
costly:

- ADR 0411 (Ory) was initially almost-accepted on a half-day spike that
  passed first-try; the post-spike decision review (`docs/project/idp-decision-review.md`)
  surfaced 12 assumptions that the spike couldn't possibly have proved.
  Spike-passes are necessary but never sufficient for a stateful
  load-bearing component.
- Wave 3 §3.1 (the canonical 72 h validation-env soak — see ADR 1007) is the only
  artifact we can cite when an SCA-R asks "and how do you know it
  doesn't drift over a long window?"
- The CGNAT validation effort (ADR 1003 + roadmap §7) failed Attempt 1
  precisely because nobody had run a real soak — infrastructure
  validated but no sustained churn evidence.
- The existing soak drivers were ad-hoc bash scripts that recorded
  results inconsistently. Without a shared shape (JSONL evidence,
  duration ceiling, decision-criteria coverage), each soak's output
  couldn't be compared with another's.

Soak testing already happened informally; this ADR codifies the
practice so every stateful component gets one and so the evidence
shape is uniform.

## Decision

**Required:** any new component meeting any of these criteria MUST
ship with a soak driver and at least one soak-evidence file before
its ADR can move to Accepted-for-production:

1. **Stateful** — holds data on disk that must survive restart.
2. **Replicated** — has a consistency or availability guarantee that
   spans more than one node.
3. **Long-running daemon under churn** — admits writes from
   external clients at production-shape rates.

The three criteria are independent — meeting any one triggers the
requirement.

**Soak driver shape.** A soak driver is a bash script under
`ops/<area>/tests/soak/run-<topology>.sh` that:

- Brings up the component(s) plus any dependencies via docker-compose
  or equivalent local fixture (no cloud infra).
- Runs a churn loop with a fixed tick interval (default 30 s) for a
  fixed default duration (default 2 h, override via `$SOAK_DURATION`).
- Exercises the component's load-bearing paths: writes, replication,
  health checks, cross-node verification where applicable.
- Appends one JSON line per tick to
  `docs/reports/<YYYY-MM-DD>-<area>-<topology>-soak-evidence.jsonl`
  (UTC timestamp, iter, elapsed_s, per-operation success ints,
  health codes, replication-lag bytes where applicable).
- Cleans up via a `trap` on exit (no orphaned containers).
- Refuses to start if a known-bad precondition holds (e.g. the
  daemon never reaches `/health/ready` within 5 minutes — fail fast,
  don't soak a broken stack).

**Soak duration tiers.**

| Tier | Duration | When |
|---|---|---|
| Smoke | 5 min | CI gate; smoke regression of the soak driver itself |
| Validation | 2 h | Default — single-node and replicated soaks for ADR-flip evidence |
| Canonical | 72 h | One-time per major component (Wave 3 §3.1 shape); cited in the assessment package |
| Long-haul | 30 d | Production canary on a real site; passive monitor, not a separate driver |

The validation tier is the default for a new ADR's evidence. The
canonical tier is the artifact an SCA-R will ask for. Long-haul comes
free from production telemetry once the component is deployed.

**Decision-review template.** A soak feeds a decision review
(template: `docs/project/idp-decision-review.md`). The review:

- Lists the assumptions the soak is supposed to validate
  (numbered A1..AN, mapped to operational risk).
- Names the alternatives still on the table if the soak fails review
  (so the path back to Proposed is pre-walked, not panic-walked).
- Defines Pass / Conditional pass / Fail outcomes with explicit
  evidence references.
- Has a reviewer signoff line (date + name).

The decision review is the artifact that moves an ADR from
Accepted-pending-soak to Accepted (or back to Proposed). It is NOT
optional — even a green soak needs the assumption table walked.

**Evidence retention.** JSONL evidence files commit to `docs/reports/`
(public, non-sensitive). Trim noise (per-tick logs > a few hundred
lines: keep the JSONL; archive verbose logs to `/tmp/` and don't
commit). The decision review cites the JSONL line ranges and any
notable cryptic-error excerpts.

**Pin-regression hook.** A soak driver that touches a pinned third-party
image (postgres-spock, oryd/kratos, etc.) gets a CI workflow that
re-runs the smoke-tier soak when the pin changes (same pattern as
`.github/workflows/mgmt-host-bootstrap.yml`).

## Alternatives considered

- **No formal soak — rely on production canaries.** Production canaries
  are useful but expensive to set up before the component is even
  deployed and surface failures slowly. Pre-deployment soaks are
  cheap (local docker-compose) and surface failures immediately.
  Rejected.
- **Soak everything regardless of statefulness.** Soaking a stateless
  HTTP service produces no signal beyond what the unit test suite
  already produces. Targeted at the three trigger criteria above.
  Rejected as written; soaking pure-stateless is not required.
- **Make soak a CI gate (every PR runs a 2 h soak).** 2 h is too long
  for the PR-review loop. Resolution: smoke-tier (5 min) on PR; full
  validation tier on the soak driver's pin-regression cron + on
  decision-review demand.
- **Adopt an off-the-shelf chaos / soak harness (jepsen, k6, locust).**
  Jepsen is the gold standard for distributed-systems soak but its
  per-target test surface is heavyweight, and most of what it would
  prove for our stack (Spock is eventually consistent, LWW per row)
  is already documented in ADR 0801 + ADR 1010 by design — so a
  Jepsen run would mostly confirm known properties rather than find
  bugs. The interesting properties are app-layer (lifespan budgets
  hold, retry catches the gap, app-layer uniqueness wins over LWW),
  which our existing JSONL probe shape already captures. k6 is
  HTTP-only. locust is HTTP-only. None of them produce evidence in
  our JSONL shape without glue code. The bash + python3 + curl
  pattern is uniform across all current soak drivers and well-
  understood. Rejected for now; re-open if soak driver count exceeds
  8 and duplication becomes costly.

  **Failure-injection lives natively in soak drivers** (gated by an
  env var, e.g. `SOAK_NEMESIS=1`) rather than as a separate framework.
  The 2-node Spock-replicated Ory soak driver is the reference
  implementation: alternates `docker pause postgres-b` (partition
  shape) and `docker kill kratos-a` (process-crash shape), tags each
  event in JSONL, and the summarizer reports a recovery green-rate
  on post-event ticks separately from the steady-state rate. The
  pattern is portable to any future driver that needs it.

## Consequences

**Positive.**

- New ADRs that touch state get a uniform evidence shape; reviewers
  read JSONL the same way every time.
- Wrong-resistance is a property of the process — half-day spikes can
  no longer slip into Accepted on first-try optimism.
- The decision-review template forces the alternatives back into
  scope before a soak even runs, so a Fail outcome doesn't trigger
  panic candidate-set re-evaluation.
- Pin-regression hooks catch upstream-image breakage before it lands
  in production.

**Negative.**

- Each new stateful component costs a half-day of soak-driver authoring
  on top of the implementation. Acceptable; pays back the first time
  upstream changes break behavior under sustained load.
- Soak drivers add to CI burn (smoke tier) and cron burn (validation
  tier on pin change). Acceptable; we already pay for the
  cgnat-validation cron + the metrics-sample cron.
- Bash + docker-compose soaks won't catch every failure mode a real
  Jepsen-style nemesis would. We accept the residual; ADRs that
  warrant Jepsen-class testing (e.g. multi-region Spock under network
  partition) get bespoke validation envs (Wave 3, CGNAT validation,
  future).

**Residual risk + compensating control.** A soak proves the absence of
some failure modes, never all of them. The compensating control is
the layered evidence trail: spike → soak → wave-style validation →
phase gate → production canary. No single layer is sufficient; the
combination is.

## Assessor view (SCA-R)

Soak testing maps to:

- **CA-2** (security assessment): named soaks with structured evidence
  files that an assessor can re-derive.
- **CA-7** (continuous monitoring): the long-haul tier is the
  continuous-monitoring evidence for any stateful component.
- **SI-6** (security function verification): each soak verifies that
  the component continues to function under sustained load, not just
  at start.
- **SA-11** (developer security testing and evaluation): the canonical
  72 h soak is the developer-side evaluation artifact.

The decision-review template (`docs/project/idp-decision-review.md`)
is the structured artifact an SCA-R cites; the JSONL files are the
underlying evidence.

## Trigger conditions for revisit

- Soak driver count exceeds 8 and duplication becomes costly →
  consider an off-the-shelf harness or an internal harness factored
  out of the existing drivers.
- A class of failure repeatedly slips through 2 h validation but
  surfaces in production → raise the validation default to 6 h or
  add explicit chaos injection (network partition, restart) to the
  default tier.
- Compliance regime mandates a specific soak shape we don't currently
  produce → adapt the JSONL schema to include the required fields.

---

*See `GLOSSARY.md` for terms and acronyms.*
