# ADR 1000 — Cloud env lifecycle tracker — explicit teardown criteria per env

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Related | [`1003-cgnat-aware-validation-default.md`](1003-cgnat-aware-validation-default.md), `../operations/cloud-env-lifecycle.md` |

## TL;DR (for a layperson)

We stand up cloud environments (hadev, integdev, a site
PoC, pgbackrest-wave18, cgnat-validation) for specific time-
bounded validation purposes. Each one has explicit teardown
criteria and destroy commands tracked centrally. New cloud
envs must add a row to that tracker in the same PR that
provisions them. This prevents "perpetual cloud env" drift.

## Context

Cloud envs are easy to spin up and easy to forget. Forgotten
envs cost money, accumulate stale configurations, and become
attack surface. The fix: every env is born with a teardown
plan.

## Decision

Every ephemeral cloud env follows this lifecycle:

- **Provisioning PR** must:
  - Add a row to
    `../operations/cloud-env-lifecycle.md`
    with: env name, purpose, owner, teardown criteria,
    destroy command.
  - Pass CI (Terraform validation, smoke tests).
- **TeardownBy tag**: every Terraform-managed resource in
  an ephemeral env has a `TeardownBy=<criterion>` tag.
- **Status**: tracker shows live / pending-teardown /
  destroyed.
- **Auto-destroy**: where teardown criteria are mechanical
  (e.g. "smoke test passed"), CI / cron destroys
  automatically.
- **Destroy verification**: `terraform destroy` plus
  `aws-vault exec ... aws ec2 describe-instances` confirms
  zero remaining resources.

Examples of teardown criteria in the tracker today:

- hadev: §3.1 + §3.9 PASS → retire (done 2026-04-24).
- pgbackrest-wave18: smoke test PASS → destroy.
- cgnat-validation: 24h soak PASS → destroy.

## Alternatives considered

- **No tracker.** Cloud envs accumulate. Rejected.
- **Tracker but no destroy criteria.** Loses the lifecycle
  property. Rejected.
- **Auto-destroy on a fixed timer regardless of criteria.**
  Useful as backstop; not as primary because validation may
  legitimately need extension.

## Consequences

**Positive.** Cloud envs have bounded lifetime. Cost is
predictable. Stale-env risk eliminated.

**Negative.** Tracker discipline. Acceptable.

## Assessor view (SCA-R)

The tracker serves CM-2 (baseline configuration — ephemeral
envs are not part of the baseline), CM-8 (component inventory
— what is currently provisioned), and PE-x (physical / cloud
asset management).

## Trigger conditions for revisit

- A class of long-lived "almost-permanent" ephemeral env
  emerges. Either promote to permanent (with explicit
  baseline addition) or sunset.

---

*See `GLOSSARY.md` for terms and acronyms.*
