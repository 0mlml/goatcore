# ADR 0301 — Patched netbird-management image (custom build, signed)

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Related | [`0300-netbird-as-mesh-control-plane.md`](0300-netbird-as-mesh-control-plane.md), [`0702-build-from-source-mgmt-plane.md`](0702-build-from-source-mgmt-plane.md), [`0706-custom-netbird-image-build-ci.md`](0706-custom-netbird-image-build-ci.md) |

## TL;DR (for a layperson)

We build our own version of the NetBird management server from
source — not because the upstream is broken, but because we need
specific behavior (skip schema migrations on rollback, use our
patched signal-server, FIPS-mode build) that upstream does not
provide. Building it ourselves means we control the supply chain
end-to-end and can ship security patches on our own cadence.

## Context

Stock netbird-management does not satisfy three requirements:

1. **Skip-migrations toggle.** For rollback during a multi-site
   coordinated upgrade, we need to start the management server
   *without* automatic schema migrations.
2. **Signal-server federation.** Stock signal-server does not
   federate; we patch netbird-management to use our federated
   signal-server ([`0302`](0302-custom-federated-signal-server.md)).
3. **Supply chain control.** Pre-built upstream images come from
   GHCR with provenance we cannot fully attest to in our build
   pipeline.

## Decision

We build `netbird-management:patched-<sha>` from source via our
own CI:

- **Source.** Fork of upstream NetBird, branch `goat`.
  Patches documented and individually reviewable; upstream PRs
  filed where appropriate (`docs/upstream-prs/`).
- **Build.** GitHub Actions native per-arch build (no QEMU);
  CGO=1; Ubuntu runtime base.
- **Tag.** `patched-<short-sha>` immutable.
- **Distribution.** Pushed to GHCR with cosign signature; pinned
  by digest in compose.
- **Toggle.** `NETBIRD_MGMT_IMAGE` env var lets us swap to
  upstream for testing without code change.

The build pipeline is `ops/netbird/`. Coordinated multi-site
upgrade flow uses `ops/ansible/coordinated-upgrade.yml`.

## Alternatives considered

- **Use upstream images.** Loses the three required behaviors;
  loses supply-chain end-to-end attestation. Rejected.
- **Maintain a long-lived hard fork.** Upstream-divergence cost
  grows; we want to land changes upstream. Rejected; fork is
  branch-only with rebases.
- **Apply patches at deploy time (overlay tarball).** Hides the
  modifications from supply-chain review; harder to audit.
  Rejected.

## Consequences

**Positive.** End-to-end supply-chain attestation. Custom
behavior available. Upstream-PR pathway preserves
maintainability.

**Negative.** We carry the build and CI overhead. Each upstream
release requires rebase. Accepted.

## Assessor view (SCA-R)

The build pipeline serves CM-7 (least functionality —
explicit features only), SI-7 (software, firmware, integrity
— signed images, pinned digests), and SR-3/SR-4 (supply
chain controls / provenance).

## Trigger conditions for revisit

- All three required behaviors land upstream. Drop the patch
  set; consume upstream images directly.
- Build cost becomes operationally unmanageable. Investigate
  binary patching or sidecar approaches; do not silently
  consume upstream.

---

*See `GLOSSARY.md` for terms and acronyms.*
