# ADR 1004 — Account migration (dev → prod) discipline — no embedded account IDs

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | Multi |
| Related | [`0004-repo-scope-tagging.md`](0004-repo-scope-tagging.md), [`1005-monorepo-separation-map.md`](1005-monorepo-separation-map.md) |

## TL;DR (for a layperson)

The system runs in a development AWS account today
and will move to production AWS accounts when we're ready. We
make this migration cheap by never embedding specific account
IDs, region IDs, or environment-specific values into ADRs,
design docs, or pattern descriptions. Those values live in
Terraform variables and group_vars files. Migrating accounts is
a config change, not a rewrite.

## Context

Account migration is in our near future:

- Current: a dev account (AWS).
- Future: production accounts (likely separate per-environment
  / per-classification scope).

If account IDs, region IDs, or other environment-specific
values are baked into ADRs / designs / scripts, migration is
a costly rewrite. If they live in config, migration is config
update.

## Decision

ADRs, design docs, and code documentation refer to *patterns*,
not values:

- **No specific AWS account IDs in ADRs / designs.** Refer to
  the account by role ("the production AWS account", "the
  dev account") rather than by ID.
- **Account-specific values live in Terraform variables.**
  `terraform.tfvars` files (gitignored where appropriate)
  carry per-account values.
- **Region IDs**: same treatment — refer by role
  ("<cloud-region-1-notional> hub region") rather than baking the literal
  string into prose.
- **Hostnames**: refer by role ("the alpha site", "the beta
  site") rather than IP / instance ID.
- **Migration runbook**: per-account migration is a documented
  runbook in `docs/operations/`; the new account inherits the
  pattern, the values get filled in.

Applied throughout the ADR series.

## Alternatives considered

- **Embed account IDs / values for clarity.** Migration cost
  becomes prohibitive. Rejected.
- **Use only abstract names; lose all examples.** Reduces
  comprehension. Examples are fine; specific values stay
  in config.

## Consequences

**Positive.** Account migration is a config exercise.
Multi-account deployment (e.g. one per partner) is feasible.

**Negative.** Discipline cost — authors must remember to
abstract. Acceptable; reviewers catch in PR.

## Assessor view (SCA-R)

The discipline serves CM-2 (baseline configuration —
abstracted from specific accounts), SR-3 (supply chain —
no per-account hard-coded paths in code), and reduces the
artifact-modification surface during account-boundary
authorization changes.

## Trigger conditions for revisit

- Account migration happens. Verify discipline held; document
  any places it failed and remediate.
- A new authorization scope demands per-account ADRs (rare;
  usually solved by repo-scope tagging
  [`0004`](0004-repo-scope-tagging.md)).

---

*See `GLOSSARY.md` for terms and acronyms.*
