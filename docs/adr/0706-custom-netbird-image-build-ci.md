# ADR 0706 — Custom Netbird image build/CI

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Related | [`0301-patched-netbird-management-image.md`](0301-patched-netbird-management-image.md), [`0702-build-from-source-mgmt-plane.md`](0702-build-from-source-mgmt-plane.md), [`0703-pinned-digests-and-pin-regression-ci.md`](0703-pinned-digests-and-pin-regression-ci.md) |

## TL;DR (for a layperson)

The custom NetBird image we ship is built by a dedicated CI
pipeline. The pipeline takes a specific NetBird source commit
or tag, applies our patches, builds for both x86 and ARM
natively (no emulation), runs our tests, and pushes the
result to our image registry. This pipeline is itself part of
the cATO factory — its outputs are the trusted artifacts.

## Context

The decision *to* build NetBird from source is in
[`0301`](0301-patched-netbird-management-image.md). This ADR
covers the build *pipeline* — the mechanism that produces
trustworthy `netbird-management:patched-<sha>` images.

CI for a multi-arch container with CGO requirements has
specific failure modes (QEMU emulation is slow and flaky for
CGO; multi-arch manifest generation has gotchas; Go
toolchain version must match upstream's go.mod). The
pipeline is documented because the gotchas are real.

## Decision

The custom NetBird build pipeline lives at `ops/netbird/` and
runs in GitHub Actions:

- **Source:** `NETBIRD_REF` env var (SHA or tag) — defaults
  to our patched-`<sha>` reference, can be set to a stock
  upstream tag for testing.
- **Per-arch native runners:** `ubuntu-latest` for amd64,
  `ubuntu-24.04-arm` for arm64 (no QEMU).
- **Toolchain:** the builder uses a fixed `golang:1.25-bookworm`
  base image (not `GOTOOLCHAIN=auto`); the Go version tracks our
  pin, not upstream's `go.mod` automatically. Bumps to the Go
  base image are deliberate PRs.
- **CGO=1:** required for upstream's SQLite usage.
- **Runtime base:** Ubuntu (not distroless, because of CGO);
  hardened via standard distro hardening.
- **Tests:** unit tests + a two-instance Spock-replication
  smoke test before push.
- **Output:** GHCR multi-arch manifest tagged
  `patched-<short-sha>` (immutable).
- **Signing + SBOM** (`0705`)
  — currently a roadmap commitment, not yet wired (see 0705
  Implementation status).

> **Correction note (2026-04-25).** The original draft of this
> ADR claimed `GOTOOLCHAIN=auto`. The actual `ops/netbird/Dockerfile`
> uses `golang:1.25-bookworm` directly. Distinction matters: with
> `GOTOOLCHAIN=auto` the builder honors upstream's `go.mod` Go
> version automatically; with a fixed base image we own the Go
> version explicitly. Original draft authored without reading the
> Dockerfile.

The pipeline is itself reviewable in `.github/workflows/`.

## Alternatives considered

- **QEMU emulation for arm64.** Slower, flakier; rejected.
- **Cross-compile via Go's native cross-compile.** Doesn't
  work cleanly with CGO. Rejected.
- **Outsource the build to Chainguard / similar.** They
  don't ship our patches. Rejected.

## Consequences

**Positive.** Reproducible per-arch builds. Tests run before
push. Smoke test catches Spock-replication regressions.

**Negative.** Two arch runners cost CI minutes; we accept.

## Assessor view (SCA-R)

The custom build pipeline is the cATO factory for the
mesh-management component. It serves SR-3 (supply chain
controls), SR-4 (provenance), SI-7 (integrity), CM-3
(change control — every build is a tracked CI run), and
CA-7 (continuous monitoring — pipeline outputs feed
continuous attestation).

## Trigger conditions for revisit

- Upstream NetBird changes build requirements (e.g. drops
  CGO). Update pipeline; preserve the build-from-source
  posture.
- arm64 runner availability changes. Re-evaluate; QEMU is
  the fallback if native runners disappear.

---

*See `GLOSSARY.md` for terms and acronyms.*
