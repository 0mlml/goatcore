# ADR 0219 — Multi-repo IaC state separation with coordinated compliance

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-05-03 |
| Repo scope | goat core (also informs every other consumer-repo deploying into goat-net cloud accounts) |
| Supersedes | — |
| Superseded by | — |
| Related | `0215-scalability-continuity-scope.md`, [`0216-goat-net-federation-and-site-planes.md`](0216-goat-net-federation-and-site-planes.md), [`0217-shim-repo-containment.md`](0217-shim-repo-containment.md) — all the multi-repo / multi-tree containment work |

## TL;DR (for a layperson)

When multiple repos deploy infrastructure into the same cloud account, they need to share two things (compliance posture + a way to lock state files so two operators don't clobber each other's work) but NOT share a third (the actual state of what each repo deployed). This ADR commits to the standard pattern: **shared compliance + coordination, separate state**. Every consumer-repo brings its own state bucket; everyone uses the same compliance tag schema and registers itself in a shared IaC registry. The result is safe parallel deployment by independent teams, with a single source of truth for "is this resource attributed to a known IaC tree" so the nightly compliance scanner can do its job.

## Context

A production deployment requires standing up production cloud infrastructure (VPS relays, public DNS, S3 backup endpoints) in a new AWS account distinct from the existing dev/test account. The new production account is **shared with a separate platform-org repo** that owns:

- The compliance contract — required-tag schema published to SSM, scanner that runs nightly across the whole account.
- An IaC registry — central declaration of "this resource is owned by this repo's state file at this S3 path."
- A "hunter-killer" job that tears down resources tagged-but-not-registered as click-ops drift.

The goat repo (this one) needs to deploy its prod relays into the production account, participate in the compliance system, and NOT collide with the platform-org's own Terraform state.

Three recurring failure modes when this is done poorly:

1. **Mega-state shared across repos.** A single Terraform state file owned by everyone. State-lock contention on every apply, blast-radius of any state corruption is the entire account, deploy cadence is bottlenecked on whichever repo is slowest.
2. **No coordination at all.** Each repo deploys into the account with no shared tag schema, no central registration. Compliance scanner can't tell repo-owned resources from click-ops; everything looks like drift; trust in the dashboard erodes; the hunter-killer either becomes too aggressive (breaks legitimate work) or gets disabled (defeats the purpose).
3. **Schema drift across repos.** Each repo defines its own copy of the required-tag map. Platform updates the schema (e.g., adds `DataClass`); some consumer-repos pick it up, others don't; compliance findings explode for months.

## Decision

Adopt the **shared compliance, separate state** pattern. Specifically:

### State separation (per-repo)

Each repo that deploys into a goat-net cloud account owns its own Terraform state file in its own S3 bucket. Naming convention: `<account>-<repo-slug>-tf-state`. Within the bucket, state paths follow `<service>/<environment>/<topology>/terraform.tfstate`.

### Lock coordination (shared)

All consumer-repos use the same DynamoDB lock table (one per account, owned by the platform-org). Locks are keyed by the full S3 path of the state file, so two state files at different paths produce two independent locks. No contention possible. Sharing the table avoids per-repo DynamoDB management overhead and keeps the lock-table count = 1 per cloud account.

### Compliance integration (shared, consumed as modules)

Every consumer-repo consumes two Terraform modules from the platform-org's repo:

- `modules/aws/compliance-tags` — produces the canonical `default_tags` map for the consumer-repo's AWS providers, sourced from the live tag contract published in SSM. Single source of truth — when the schema changes, every consumer-repo picks it up on next `apply`.
- `modules/aws/iac-registry` — writes an SSM parameter at `/<project>/<env>/iac-registry/<repo-slug>` declaring "this repo deploys these resources, state is at this S3 path." Compliance scanner cross-references each AWS resource's tags against the registry to confirm ownership.

Both modules are pinned to a Git ref (`?ref=main` for active development; production deployments should pin to immutable tags or commit SHAs once the platform-org cuts releases).

### CSP-portable generalization

The decision is intentionally framed in CSP-agnostic terms so the same shape applies to GCP and Azure when goat-net expands beyond AWS:

| Concept | AWS | GCP | Azure |
|---|---|---|---|
| Per-repo state | S3 bucket | GCS bucket | Azure Storage Account container |
| Shared lock | DynamoDB | Cloud Spanner / Firestore | Azure Storage blob lease |
| Provider tags | `default_tags` | `default_labels` | `azapi`/`azurerm` tags |
| Tag contract | SSM | Secret Manager OR GCS object | App Configuration OR Key Vault |
| IaC registry | SSM parameter | Secret Manager parameter | App Configuration entry |

Underlying invariants that don't change across CSP: per-repo state-storage object, shared coordination primitive, centrally-published tag contract, centrally-stored IaC registry.

### Operational details

- Bootstrap of a new consumer-repo's state bucket is a one-time, idempotent script (`terraform-prod/bootstrap.sh` for this repo). The shared lock table is created by that bootstrap if absent; otherwise no-op.
- The `compliance-tags` module's `lifecycle_value` defaults to `"persistent"`. Consumer-repos deploying short-lived resources (validation envs, ephemeral test scaffolds) MUST override to `"temporary"` and set `expires_at` to an ISO-8601 timestamp, or the hunter-killer will leave them indefinitely.
- Consumer-repos that bootstrap BEFORE the platform-org has published its compliance modules to GitHub need to either (a) wait for publish, or (b) inline-mirror the schema temporarily and switch to the module on a later commit.

## Consequences

**Positive:**

- Independent deploy cadence — consumer-repos `apply` without coordinating with the platform's deploy windows.
- Smaller blast radius for state corruption — affects only the owning repo.
- Faster `plan` — smaller state files per repo.
- IAM authorization granularity — a repo's CI gets allow-list access to its own state bucket only.
- Tag contract drift is impossible by construction — every consumer pulls the live contract on apply.
- Compliance scanner can attribute every resource to its owning repo via the IaC registry.

**Negative / accepted trade-offs:**

- **Cross-repo dependency on the platform-org's release state.** Consumer-repo `terraform init` fails until the platform-org has published the modules. Mitigated by inline-mirror fallback (with a TODO to switch back to module form once published), and by pinning to immutable tags in production.
- **Operators must understand multi-repo deploy ordering.** Adds cognitive load when bootstrapping a new env (the platform-org's apply must happen first). Mitigated by the design doc's "How a new consumer-repo onboards" sequence.
- **Shared lock-table is a single failure point.** If the lock table is corrupted, every consumer-repo's `apply` blocks. Mitigated by DynamoDB's built-in durability + the table being trivial to recreate (bootstrap.sh handles it).

**Out of scope (explicit non-decisions):**

- This ADR does NOT decide which platform-org features to consume beyond compliance-tags + iac-registry. Future modules from the platform-org's repo (logging, monitoring, IAM baselines) are evaluated case-by-case.
- This ADR does NOT decide on a different VCS or CI for the prod tree. GitHub remains canonical.
- This ADR does NOT mandate per-repo state for ephemeral validation envs; those can use local backends if convenient.

## SCA-R framing (Senior Cloud Architect — Reviewer)

This is the textbook multi-repo Terraform pattern adopted by AWS Well-Architected and most enterprise IaC governance frameworks. The novel piece (vs vanilla AWS WA) is the **explicit IaC registry** — most enterprises rely on tags alone and accept the false-positive rate where tagged-but-untracked resources show up as drift. The IaC-registry-via-SSM-param is a clean refinement: tags say "what compliance class," the registry says "who owns this state, where to look it up." Together they let the scanner produce findings precise enough to feed an automated remediation queue (the hunter-killer).

The decision to inline-mirror the compliance schema as a fallback (when platform-org modules aren't published yet) is pragmatic — without it, the chicken-and-egg of bootstrapping a new platform-org account would block the entire stand-up. The fallback documentation makes the temporary nature explicit and the migration to module form is a one-commit change.

The CSP portability framing is what makes this ADR useful beyond the immediate AWS-prod stand-up. As goat-net expands into GCP / Azure / on-prem, this same shape applies — only the resource types differ.

## References

- `docs/design/cloud-environments.md` — the design doc operationalizing this ADR
- The platform-org repo's multi-repo compliance guide — platform-org consumer-repo onboarding guide
- `terraform-prod/README.md` — operational README for this repo's prod tree
- AWS Well-Architected — Operational Excellence Pillar §"Operate" §"Communications" — the broader pattern reference
- HashiCorp Terraform docs — "Backend Configuration" — the per-repo state separation pattern
- See GLOSSARY.md for acronym expansions used in this ADR (CSP, SSM, IAM, ADR, SCA-R, etc.).
