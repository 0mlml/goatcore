# ADR 0600 — Two PQ paths — Rosenpass intra-mesh + strongSwan boundary

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | Multi |
| Related | [`0103-crypto-boundary-discipline.md`](0103-crypto-boundary-discipline.md), [`0601-fips-pqc-boundary-node.md`](0601-fips-pqc-boundary-node.md), [`0602-rosenpass-hybrid-rekey.md`](0602-rosenpass-hybrid-rekey.md), `../design/fips-pqc-path.md` |

## TL;DR (for a layperson)

We have two distinct strategies for protecting the system against
future quantum computers, and they are *not* interchangeable. Path
one is post-quantum protection between any two peers in our mesh
(via Rosenpass) — fast, modern, but not FIPS-validated. Path two is
post-quantum protection at the cross-boundary (via strongSwan +
FIPS-validated module + ML-KEM) for traffic that has to cross into
or out of the authorization boundary. Both paths run; each serves a
different purpose.

## Diagram



Source: [`diagrams/0600-two-pq-paths.mmd`](diagrams/0600-two-pq-paths.mmd).

## Context

Two requirements that look similar but are in fact distinct:

1. **Intra-mesh PQ.** All peer-to-peer WireGuard sessions should
   gain post-quantum protection without sacrificing the speed and
   simplicity of WireGuard. The traffic stays inside the
   authorization boundary; FIPS validation is *not* a hard
   requirement (covered by waiver
   `0854`).
2. **Cross-boundary PQ.** Traffic crossing the authorization
   boundary into a partner network has FIPS validation as a
   hard requirement; PQ protection there must come from a
   FIPS-validated module.

Conflating the two leads to bad outcomes — either Rosenpass is
oversold as a FIPS solution (it isn't), or the boundary node is
loaded with non-FIPS PQ that fails accreditation.

## Decision

Two paths, named explicitly, neither substituting for the other.

| Path | Where | What | FIPS posture |
|---|---|---|---|
| **Intra-mesh real-PQ** | Every peer-to-peer WG session | Rosenpass hybrid rekey of WireGuard session keys ([`0602`](0602-rosenpass-hybrid-rekey.md)) | Not FIPS-validated; covered by waiver `0854`. Tracked as Block 19. |
| **Cross-boundary FIPS PQC** | Boundary node | strongSwan + wolfSSL/AWS-LC FIPS + ML-KEM ([`0601`](0601-fips-pqc-boundary-node.md)) | FIPS 140-3 validated module. |

This was reconciled on 2026-04-24 — prior framing risked treating
the two as alternatives. They are complementary.

Architecture details:
`../design/fips-pqc-path.md`.
Compliance posture, waivers, SBU-E plan in
`../compliance/`.

## Alternatives considered

- **Single PQ path (Rosenpass everywhere).** Loses FIPS posture
  at the boundary. Rejected.
- **Single PQ path (strongSwan everywhere).** Loses WireGuard
  speed and simplicity intra-mesh. Rejected.
- **No intra-mesh PQ; only boundary PQ.** Leaves intra-mesh
  traffic vulnerable to harvest-now-decrypt-later. Rejected.
- **Wait for WireGuard FIPS validation; use one path.** No
  certain timeline. Rejected.

## Consequences

**Positive.** Each path serves its purpose. Intra-mesh stays
fast and modern; boundary stays FIPS-validated. The two-path
framing is reviewable: an SCA-R can verify each path against
its own bar.

**Negative.** Two paths to maintain. Two distinct sets of
upstream dependencies (Rosenpass + WireGuard for path 1;
strongSwan + wolfSSL/AWS-LC for path 2). Acceptable.

## Assessor view (SCA-R)

Two-path PQ aligns with NIST SP 800-208 (stateful-hash and
post-quantum considerations) and recent NSA commercial-grade
hybrid PQC guidance. Path 1 is covered by waiver
`0854`; path 2 satisfies
SC-13 (cryptographic protection) at FIPS 140-3 validation
inside the authorization boundary's interconnect controls
(SC-7, CA-3).

The waiver narrative explicitly distinguishes "PQ protection"
from "FIPS validation" — both important, neither implies the
other.

## Trigger conditions for revisit

- WireGuard / Rosenpass receive FIPS validation; collapse path
  1 into a single FIPS-validated path.
- A specific authorization rejects the two-path framing.
  Negotiate; do not abandon either path silently.
- ML-KEM standard evolves (NIST FIPS 203 is recent).
  Update the boundary node's parameter set.

---

*See `GLOSSARY.md` for terms and acronyms.*
