# ADR 0222 — OSS public repo + private-overlay downstream fork relationship

| Field | Value |
|---|---|
| Status | **Superseded by [ADR 1197](1197-public-core-versioned-dependency-private-overlay-repo.md)** (2026-10-27) — the fork model is retained only as a constrained fallback; the default is now a versioned-dependency + private overlay repo. |
| Date | 2026-05-04 |
| Repo scope | goat core (becomes the goat public repo + this becomes the private overlay) |
| Supersedes | — |
| Superseded by | [ADR 1197](1197-public-core-versioned-dependency-private-overlay-repo.md) |
| Related | [`0110-license-posture-agplv3-forked-components.md`](0110-license-posture-agplv3-forked-components.md), [`0111-vendor-friendly-license-posture.md`](0111-vendor-friendly-license-posture.md), [`0217-shim-repo-containment.md`](0217-shim-repo-containment.md), [`0219-multi-repo-iac-state-separation-coordinated-compliance.md`](0219-multi-repo-iac-state-separation-coordinated-compliance.md) |

## TL;DR (for a layperson)

When goat goes open-source, we cut the project into two repos: a **public** one that anyone on the internet can read and contribute to, and a **private overlay** that holds only the deployment-specific layer (which sites we run, our internal team-management apparatus, full-fidelity versions of methodology docs that would help an attacker if published). The public repo is the source of truth; the private one is a downstream fork that pulls from it weekly and adds a thin layer on top. Same shape as Red Hat → Rocky Linux, or any sovereign distro on top of an open base.

## Context

Today the project lives in one private repo that holds substrate code, deployment-specific artifacts, internal program apparatus, and operationally-sensitive methodology. The OSS go-open plan (`docs/project/oss-strategy.md`) commits to opening the substrate. Block 30 sanitizes the public surface across three cleansing categories; Block 29M executes the actual cut.

The decision this ADR records is the **steady-state relationship** between the two repos after the cut, plus the **mechanism** by which the cut happens. Without an explicit decision and operating discipline, drift between public and private accumulates as parallel-fork-fork patches, the public repo stops being the truthful canonical view, and substrate improvements made on the private side stay invisible to adopters.

## Decision

Adopt the **public-upstream + private-downstream-overlay** pattern. Specifically:

### Steady-state shape

- **`goat`** (public, new) — canonical upstream-of-record for the substrate. Substrate code, ADRs (immutable history retained), sanitized design docs, general-purpose runbooks. Eclipse-incubation-eligible.
- **`goat-private`** (private, today's repo renamed) — downstream fork. Pulls from public goat. Adds and only adds: `docs/deployments/<slug>/` (per-deployment artifacts), `docs/internal/` (program apparatus that didn't go via `export-ignore`), full-fidelity versions of methodology docs sanitized in 30F, deployment-specific Terraform / Ansible inventory.

### One-time split mechanism

1. Today's repo retains full history; renamed under Block 29F to a private slug (`goat-private`).
2. A new public repo `goat` is seeded via `git filter-repo` from today's tree, excluding the path list captured in the shared `allowlist.yml` config (`docs/deployments/`, `docs/internal/` and the other overlay-only paths).
3. **Dry-run + tree-diff before push.** The filter-repo invocation is reviewed against an explicit allowlist; the resulting tree is diffed against the allowlist; only after a second-reader signoff does the push to the public remote happen.
4. The private overlay rebases onto public goat as upstream so the public history is the trunk on the private side too.

### Steady-state operating discipline

- **Substrate work flows public-first.** PRs that touch files outside the overlay-only paths open against public goat, are reviewed + merged upstream, and are pulled into the private overlay via `git pull upstream main`.
- **Overlay-only work stays private.** PRs that only touch overlay paths land directly on the private overlay.
- **Conflict-prone work** (sanitized public version + full-fidelity private version of the same doc): authored simultaneously, public version lands upstream first, private version layered as an overlay-only patch.
- **Pull cadence.** Private overlay pulls upstream at minimum weekly + on every public release tag. Drift > 2 weeks triggers a watchlist alert.

### Inverse-guard CI

- **Public repo:** `public-surface-guard.yml` (Block 30D) rejects PRs that introduce deployment-specific tokens outside the exempt paths.
- **Private overlay:** `overlay-content-guard.yml` is the **inverse** — rejects PRs that introduce overlay-only content (`docs/deployments/<slug>/...`, `docs/internal/...`) into a substrate path. Catches drift in the *other* direction (operator authoring a deployment-specific runbook in `docs/operations/` instead of `docs/deployments/<slug>/`).
- Both guards consume the same `allowlist.yml` config under version control, so changes to the boundary land as a deliberate config edit reviewable in PR.

## Alternatives considered

The chosen shape is a known good pattern; the obvious alternatives have a single disqualifying property each:

| Alternative | Disqualifying property |
|---|---|
| Single repo + `.gitattributes` `export-ignore` for everything | Works for the apparatus category; fails for deployment-specifics + sensitive methodology because git history (and commit messages) of the same repo still leak the content. |
| Two completely separate repos with no fork relationship | Substrate improvements made on either side become parallel-fork-fork drift. The public repo stops reflecting what we run. |
| Hybrid (public substrate + private repo consuming it as a submodule) | Adds submodule operational tax. A fork+pull relationship gets the same separation with less ceremony and a more familiar contributor model. |

The remaining shape — public upstream + private downstream fork with `filter-repo` cut + push-up discipline — is the standard distro-on-distro pattern (Red Hat → Rocky Linux; Debian → Ubuntu; OpenWrt → vendor builds). Adopters recognize it; the operational habits are documented in the wider OSS ecosystem.

## Consequences

**Positive:**

- Public goat is the truthful canonical view of the substrate; adopters see what we actually run.
- Deployment-specific and operationally-sensitive material never enters public history.
- Standard contributor model — outside contributors interact with the public repo exactly as they would any other OSS project; no special access path.
- Inverse-guard CI catches drift in *both* directions automatically.
- Eclipse-incubation-eligible public surface (Block 29G).

**Negative / accepted trade-offs:**

- **One-time `filter-repo` is load-bearing.** Mistakes leak content into public history. Mitigation: dry-run + tree-diff + second-reader signoff before push. Reversibility: private overlay retains full history, so the public repo can be re-cut if filter-repo missed something — at the cost of a force-push visible to early adopters.
- **Weekly pull cadence is operator discipline, not enforced by tooling at v1.** Drift > 2 weeks triggers a watchlist alert; if drift becomes habitual, automate the pull as a scheduled CI job on the private overlay.
- **Two CI workflows to maintain** (`public-surface-guard.yml` + `overlay-content-guard.yml`). Mitigated by shared `allowlist.yml` config — both workflows consume the same source.

**Out of scope (explicit non-decisions):**

- This ADR does NOT decide which Eclipse / CNCF / other foundation governance shape goat eventually adopts; that is Block 29G.
- This ADR does NOT decide the per-deployment-slug structure inside `docs/deployments/`; that lives in Block 30C.
- This ADR does NOT mandate that all out-of-tree shim repos follow the same pattern; per-shim repo decisions ride [ADR 0217](0217-shim-repo-containment.md).

## SCA-R framing (Senior Cloud Architect — Reviewer)

This is the standard upstream-distro / downstream-fork pattern. The novel piece (vs. a vanilla downstream-distro shape) is the **inverse-guard CI on the private overlay** — most downstream distros don't formalize a CI guard rejecting overlay content from substrate paths because their downstream layer is mostly packaging and build configs, not documentation drift. Goat's overlay carries operationally-sensitive *documentation* in addition to deployment configs, so the inverse guard prevents an operator from accidentally writing a deployment-specific runbook in a substrate path where the next public push would leak it.

The pull cadence + drift-watch alert pair is what keeps the public repo truthful. Without it, the private overlay would accumulate substrate-shaped changes that should have been upstreamed, and a stranger reading the public repo would see a stale view of the substrate.

The chosen `filter-repo` mechanism over BFG or `git filter-branch` is intentional — `filter-repo` is the upstream-recommended tool for this class of cut and gives the cleanest history. The dry-run + tree-diff + second-reader review before push is the load-bearing safety mechanism.

## References

- `docs/project/oss-strategy.md` — go-open strategy, the three-categories framing, the upstream/private-overlay relationship section.
- `docs/project/implementation-plan.md` Block 29M — execution of the split.
- `docs/project/implementation-plan.md` Block 30 — sanitization sub-blocks (30A-30F) prerequisite to the split.
- [`0219-multi-repo-iac-state-separation-coordinated-compliance.md`](0219-multi-repo-iac-state-separation-coordinated-compliance.md) — sister ADR for the multi-repo IaC shape; informs the per-repo separation discipline applied here.
- [`0217-shim-repo-containment.md`](0217-shim-repo-containment.md) — sister ADR for per-shim repo separation; this ADR is the equivalent decision for the substrate / deployment split.
- `git filter-repo` — upstream tool: <https://github.com/newren/git-filter-repo>
- See GLOSSARY.md for acronym expansions used in this ADR.
