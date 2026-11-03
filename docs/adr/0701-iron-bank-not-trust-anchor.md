# ADR 0701 — Iron Bank not a standalone trust signal

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | Multi |
| Related | [`0700-chainguard-first.md`](0700-chainguard-first.md), [`0106-sovereign-tooling-preference.md`](0106-sovereign-tooling-preference.md) |

## TL;DR (for a layperson)

Iron Bank is the DoD's catalog of pre-screened container
images. It sounds authoritative, but in practice many Iron
Bank images are insecure — they include outdated software,
unnecessary packages, and known CVEs. Iron Bank presence is
*not* a substitute for our own per-image security review.
We treat Iron Bank as one option among many, not as a trust
signal on its own.

## Context

The instinct in defense IT is to treat any DoD-curated catalog
as a trust anchor. For Iron Bank, this instinct is wrong.
Operators who have actually pulled and audited Iron Bank
images consistently find:

- Outdated upstream versions (Iron Bank rebuild cadence is
  slower than upstream release cadence).
- Heavy base images that include unnecessary packages.
- Images that pass DoD scanner thresholds but still carry
  known CVEs in non-fixable components.
- Per-image quality varies wildly by submitting team.

This is documented in our supply-chain audit
(`../design/os-supply-chain-security.md`)
and in the user-feedback memory
`feedback_ironbank_not_trusted.md`.

## Decision

Iron Bank is treated as an option, not a trust anchor:

- **No automatic acceptance** of an image because it is in
  Iron Bank.
- **Per-image audit required** before adoption: read the
  image's Dockerfile, base image, package list, and CVE
  scan; compare to upstream and to Chainguard equivalent.
- **Chainguard preferred** when both are available
  ([`0700`](0700-chainguard-first.md)).
- **Used when no Chainguard equivalent exists** *and* the
  per-image audit comes back clean.
- **Rejection reasons documented.** When we audit an Iron
  Bank image and reject it, the rejection reason is
  recorded so we don't re-evaluate it cyclically.

## Alternatives considered

- **Treat Iron Bank as trust anchor.** Default in some
  programs; results in shipping insecure images. Rejected.
- **Refuse all Iron Bank images.** Loses some genuinely
  good Iron Bank images. Rejected.
- **Audit-once, trust-forever.** Iron Bank images change;
  our audit must re-run on update. Audit per consumed
  digest.

## Consequences

**Positive.** We do not ship images we have not actually
reviewed. Iron Bank provenance is one input among many; it
does not substitute for our own pipeline.

**Negative.** Per-image audit work. We accept; the audit is
cheap relative to the cost of shipping a known-bad image.

## Assessor view (SCA-R)

This ADR is the explicit answer to "isn't Iron Bank
sufficient?" An assessor familiar with DoD container
practice will recognize the question. The substantive
posture (per-image audit, Chainguard preferred,
build-from-source where justified) is *stronger* than
Iron-Bank-default. NIST SP 800-53 SR-3 / SR-4 are served
by the per-image audit and the SBOM-based attestation
pipeline.

## Trigger conditions for revisit

- Iron Bank materially improves rebuild cadence + CVE
  threshold. Re-evaluate; default may shift.
- A specific authorization mandates Iron Bank for a defined
  image class. Negotiate; do not silently relax the
  per-image audit.

---

*See `GLOSSARY.md` for terms and acronyms.*
