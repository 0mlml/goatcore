# ADR 0607 — WireGuard FIPS waiver — pointer to compliance waiver

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goatCompliance |
| Related | [`0303-wireguard-data-plane.md`](0303-wireguard-data-plane.md), [`0103-crypto-boundary-discipline.md`](0103-crypto-boundary-discipline.md), `0854-wireguard-fips-waiver.md`, `../compliance/waivers/wireguard-fips-validation.md` |

## TL;DR (for a layperson)

WireGuard is not on the official FIPS-validated cryptographic-
module list. We use it inside the mesh anyway, because the
crypto-boundary discipline ([`0103`](0103-crypto-boundary-discipline.md))
keeps FIPS validation at the boundary — not in the mesh
interior. This ADR is the index entry for the formal waiver
that documents this deviation; the waiver narrative is in
the compliance waivers directory and is mirrored by the
accreditation-side ADR (`0854`).

## Context

The 0600-series crypto ADRs reference a WireGuard FIPS
waiver in multiple places. This index ADR exists so a future
contributor or assessor following links can reach the waiver
narrative directly without having to know the exact filename.
The accreditation-side ADR (`0854`)
carries the SCA-R-facing structure; this ADR cross-references.

## Decision

The waiver narrative lives at
`../compliance/waivers/wireguard-fips-validation.md`.
It is structured around:

- Deviation: WireGuard primitives are not FIPS 140-3
  validated.
- Reason: WireGuard is the right intra-mesh primitive on
  performance and simplicity grounds; FIPS validation at
  every component would defeat the boundary discipline.
- Compensating controls:
  - The boundary node ([`0601`](0601-fips-pqc-boundary-node.md))
    carries FIPS-validated cryptography for cross-boundary
    flows.
  - Rosenpass ([`0602`](0602-rosenpass-hybrid-rekey.md))
    provides hybrid post-quantum protection intra-mesh.
  - Identity-based mesh ([`0200`](0200-identity-based-mesh-overlay.md))
    plus per-pair encryption means an intra-mesh
    cryptographic break still requires per-pair key
    compromise.
- Residual risk: intra-mesh sessions rely on non-validated
  primitives.
- Expiry / re-evaluation cadence.

This ADR's Status changes (e.g. to Superseded) when WireGuard
or its primitives receive FIPS validation.

## Alternatives considered

- **Don't use WireGuard.** Rejected via [`0303`](0303-wireguard-data-plane.md).
- **FIPS-validate WireGuard ourselves.** Out of scope for our
  organization; the upstream WireGuard project is the right
  driver if/when this happens.
- **No formal waiver (rely on assessor-by-assessor
  conversation).** Loses the reciprocity benefit; loses the
  written trail. Rejected.

## Consequences

**Positive.** A specific, citable artifact for the deviation.
Reciprocity-friendly. Auditable.

**Negative.** Waiver maintenance discipline (re-affirm
periodically); we accept.

## Assessor view (SCA-R)

The waiver is the load-bearing document. Read it directly:
`../compliance/waivers/wireguard-fips-validation.md`.
The accreditation-pathway alignment is in
`0854` with mapping to
NIST SP 800-171 / 800-53 control families and to the relevant
compensating controls.

## Trigger conditions for revisit

- WireGuard primitives receive FIPS 140-3 validation.
  Retire the waiver; this ADR transitions to Superseded.
- A specific authorization explicitly forbids non-validated
  modules within the boundary. Negotiate the boundary
  position; do not silently include non-validated modules.

---

*See `GLOSSARY.md` for terms and acronyms.*
