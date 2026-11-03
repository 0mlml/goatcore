# ADR 0703 — Pinned digests + pin-regression CI guard

| Field | Value |
|---|---|
| Status | Accepted (target); see Implementation status — digest guard in CI since 2026-09-22 as a shrink-only ratchet over the references that still floated then |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Related | [`0700-chainguard-first.md`](0700-chainguard-first.md), [`0702-build-from-source-mgmt-plane.md`](0702-build-from-source-mgmt-plane.md), `0705-signed-images-sbom-attestation.md` |

## TL;DR (for a layperson)

Every container image we deploy is referenced by its
cryptographic digest (a SHA-256 hash), not by a moving tag
like `:latest` or `:v1.2`. This means we deploy *exactly* the
image we tested. A CI guard prevents anyone from accidentally
loosening this — if a PR replaces a digest with a tag, CI
fails. This is one of the smallest and highest-leverage
supply-chain controls available.

## Context

Tag-based references (`postgres:16`, `caddy:2`, etc.) are
mutable — the image behind the tag can change at any time
without our knowledge. Digest-based references (`@sha256:...`)
are immutable: the same digest always refers to the same bits.

The discipline is simple but easy to backslide on. A tired
engineer changes a digest to a tag because "tags are easier."
A pin-regression CI guard makes the backslide cost-prohibitive.

## Decision

All container image references in compose / Ansible templates
are digest-pinned:

- **Format:** `image:tag@sha256:digest` (the tag is
  human-readable; the digest is authoritative).
- **Source:** the digest is taken from the actual published
  image after our build pipeline finishes.
- **CI guard:** `compose-lint` + a custom check enforces
  that every image reference in compose has a digest. PRs
  without digests fail CI.
- **Update flow:** updating a pin is a deliberate PR with
  the new digest, the rationale, and the artifacts (build
  log, scan results) that justify the bump.
- **Pin regression on bake-in:** `postgres-spock` images
  bake in pgBackRest at a specific version; CI checks the
  baked-in version against the spec.

This applies to all images: Chainguard, Iron Bank,
build-from-source, upstream — no exceptions.

## Implementation status (2026-04-25)

**What's actually in CI today:**

- `compose-lint.yml` runs a `pin-compat` job invoking
  `ops/ci/check-pin-compat.sh`. That script validates **CLI
  flag compatibility** between the image's pinned version and
  the Compose template — it does not check for digest
  presence.
- `postgres-spock.yml` bakes a specific pgBackRest version
  into the postgres-spock image (a different kind of
  pinning).

**What's NOT in CI yet:**

- A digest-enforcement check that fails CI when any
  `docker-compose*.yml` or Ansible-templated Compose file
  contains an image reference without `@sha256:...`.
- Pin-regression alerting that compares previous and current
  pinned digests on PR.

**Forward task:** add a CI step that greps every Compose
template for image references missing the `@sha256` suffix
and fails the build. Tracked as a Block 13B follow-up.

> **Correction note (2026-04-25).** The original draft of this
> ADR claimed `compose-lint` + a custom check enforces digest
> presence today. That overstates `check-pin-compat.sh`,
> which checks CLI flag compatibility only. Statement above
> is what's actually shipped vs. what the ADR commits to as
> the target.

## Implementation status (2026-09-22)

The digest guard now exists, as a ratchet rather than a clean
sweep. `ops/ci/check-no-floating-image-pins.sh` (CI job
`coupled-fact-guards`) checks:

| Where | Since | Rule |
|---|---|---|
| hand-authored `group_vars` | 2026-09-05 | every `*_image` pin carries `@sha256:` |
| role `defaults/*.yml` `*_image` | 2026-09-22 | same; with no override this is what deploys |
| every compose file's `image:` literals | 2026-09-22 | same |
| every Dockerfile `FROM` | 2026-09-22 | same, except build-stage names, `scratch`, `${ARG}` |
| `.github/dependabot.yml` | 2026-09-22 | a Dockerfile that pins a digest has its directory listed, so the pin gets a refresh PR |

The 214 references that floated on 2026-09-22 are listed in
`ops/ci/floating-image-pins.allow`, and that list only shrinks:
a new floating reference fails CI, and so does a line whose
reference is already pinned.
Pin each to the digest the host is **running**, not to what
the tag resolves to today; a fresh resolve is an upgrade.

Rollback of the fabric's own images was never affected: those
pins come from `ops/pins/fleet-pins.yaml` and carry digests on
every listed goatnet. A floating Dockerfile `FROM` does not
affect rollback either, since a rollback redeploys an image
that was already built. It makes a rebuild of the same commit
produce a different image.

## Alternatives considered

- **Tag-pinned only.** Mutable; defeats the supply-chain
  story. Rejected.
- **Digest-pinned but no CI guard.** Drift happens. Rejected.
- **Floating tags with daily rebuild.** Acceptable for
  Chainguard's *upstream* daily rebuild but not for our
  consumption — we still pin to a specific daily digest.

## Consequences

**Positive.** Deployments are exactly reproducible. Image
content cannot change underneath us. CI catches backsliding
mechanically.

**Negative.** Pin updates are deliberate — a small overhead.
We accept; the trade-off is correct.

## Assessor view (SCA-R)

Digest pinning serves SI-7 (software, firmware, integrity —
deployed image bits are the bits that were tested), CM-2
(baseline configuration — the digest is the baseline), CM-3
(change control — pin update is a tracked PR), and SR-4
(provenance). The CI guard provides mechanical enforcement,
not advisory.

## Trigger conditions for revisit

- A specific image source uses a content-addressable system
  that requires a different reference shape. Adapt; do not
  drop pinning.

---

*See `GLOSSARY.md` for terms and acronyms.*
