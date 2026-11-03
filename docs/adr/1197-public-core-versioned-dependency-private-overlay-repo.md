# ADR 1197 — Public core consumed as a versioned dependency by a private config/overlay repo

| Field | Value |
|---|---|
| Status | **Proposed** |
| Date | 2026-10-27 |
| Deciders | operator |
| Supersedes | [ADR 0222](0222-oss-public-private-overlay-fork-relationship.md) (public-upstream + private-downstream **fork**) |
| Superseded by | — |
| Related | `open-source-split-public-core-private-overlay.md` (full design + sources), [ADR 1196](1196-equipment-and-site-identifiers-are-near-secrets.md), [ADR 1186](1186-credentials-at-rest-are-references-only.md) |

## TL;DR (for a layperson)

When goat goes open-source we keep two repositories. The **public** one holds the generic technology that anyone can read and use. The **private** one holds the details of our actual deployments — which machines, which addresses, which secrets — and it *uses* the public one the way any project uses a library: by pointing at a released version, not by copying its code. Because the private repo only references the public one, our deployment secrets can never slip into the public repo by accident.

## Context

ADR 0222 (Proposed, May 2026) chose a **fork** relationship: a public upstream and a private downstream fork that rebases from it weekly. Two things have changed. First, the repo moved to `USAREUR-AF/goatcore` (ADR 1195), so 0222's `goat-maintainer/goat` + `goat-maintainer/goat-private` slugs are stale. Second, a 2026 review of current practice (design doc above) finds the fork model is the weakest safe option for *genuinely secret* downstream content: a fork shares git history with upstream, so private commits are one mis-push from the public remote, and the merge/rebase tax grows with drift and multiplies per site. The stronger, now-standard pattern for a public core with secret downstream is a **versioned dependency + GitOps config/overlay repo**, developed in public.

## Decision

Adopt **public core consumed as a versioned dependency by a private config/overlay repo**, developed in public. Specifically:

1. **`USAREUR-AF/goatcore` is a cleansed, Apache-2.0, develop-in-public core.** It publishes tagged, versioned, signed artifacts (git tags `vX.Y.Z`, Go module versions, signed OCI images) and carries no site inventory, deployment addresses, logins or secret values.
2. **A separate private operations repo consumes the core by pinned version** (module/tag/digest), bumped deliberately. It holds only per-site overlays (`group_vars/<site>_hosts/` / Kustomize bases + overlays; N sites = N overlay directories) and references to secrets (`op://` / External Secrets Operator → 1Password), plus the gitignored site inventories (ADR 1196). The core never sees it.
3. **The coupling is a version string, not shared history**, so site inventory and secret values physically cannot cross into the public repo. Local `replace` / local module paths serve tight inner-loop development against unreleased core.
4. **Leak prevention is at the public repo's door:** GitHub push protection + secret scanning, `check-no-identity-spillage.sh` + `check-no-live-secret.sh` as required checks, and `gitleaks`/`trufflehog` in CI.
5. **Contributing back is a normal open-source PR** from a clean branch cut off public `main` — never a private branch pushed to the public remote.

A **fork/pinned-sibling** relationship (ADR 0222's model, Chromium-style) is retained only as a fallback for the case where the private side must patch core internals with no clean extension seam; it is not the default.

## Alternatives considered

| Alternative | Disqualifying property |
|---|---|
| Open-core in one tree (GitLab `ee/`) | Private code in the same repo — one mistake from the public remote. Works for *source-available* paid tiers, not *secret* inventory. |
| Public upstream + private **fork** (ADR 0222) | Shared history → private commits one mis-push from public; merge/rebase tax grows with drift, multiplies per site. Kept only as a constrained fallback. |
| git submodules | Fragile, entangle checkout lifecycle, no benefit over a versioned dependency. |
| Develop-in-private, publish cleansed | Forces history rewrites + full-history scans + credential rotation on every publish; one missed secret is public forever. |

## Consequences

- The private repo and the public repo have **no shared git history**; the go-open "split" becomes *seeding a clean public repo + standing up a private consumer*, not forking.
- Faster, decoupled iteration: a site change is a one-line overlay edit; a core change is a normal PR; they meet at a pin bump.
- Scales as N private overlays against one pinned core.
- The one-time go-open seeding mechanics (which paths seed the public repo, the history-cleanliness gate) are the go-open execution plan, owed separately; ADR 0222's "One-time split mechanism" section still informs it. This ADR sets the steady-state relationship only.
- Does not decide foundation/governance shape, or the private repo's name — those remain in the migration plan.
