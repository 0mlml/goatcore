# ADR 0603 — ML-KEM at the boundary (post-quantum KEM)

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Related | [`0600-two-pq-paths.md`](0600-two-pq-paths.md), [`0601-fips-pqc-boundary-node.md`](0601-fips-pqc-boundary-node.md) |

## TL;DR (for a layperson)

The post-quantum cryptography algorithm we use at the
boundary node is ML-KEM (formerly Kyber, now FIPS 203). It is
the algorithm NIST chose as the primary post-quantum key
encapsulation mechanism. We use it in hybrid mode — classical
ECDHE alongside ML-KEM — so even if one of the two breaks, the
session is still safe.

## Context

NIST FIPS 203 standardizes ML-KEM as the post-quantum KEM. NSA
guidance recommends hybrid (classical + PQ) deployment during
the transition period. wolfSSL and AWS-LC ship validated
ML-KEM implementations.

The choice is mechanical: ML-KEM is the standard; hybrid is
the deployment guidance; a validated module ships it.

## Decision

ML-KEM is the post-quantum KEM at the boundary node:

- **Standard:** NIST FIPS 203.
- **Mode:** hybrid with ECDHE (classical KEM) — both run; the
  shared secret is derived from both.
- **Parameter set:** ML-KEM-768 default; ML-KEM-1024 where
  partner profile requires.
- **Library:** whichever validated module the boundary node
  is running ([`0601`](0601-fips-pqc-boundary-node.md))
  — wolfSSL or AWS-LC, current validated-list status governs.
- **Application:** strongSwan IKEv2 hybrid handshake at the
  boundary.
- **Not used intra-mesh.** Intra-mesh PQ uses Rosenpass
  ([`0602`](0602-rosenpass-hybrid-rekey.md)) — a different
  primitive at a different layer.

## Alternatives considered

- **NTRU / classic-McEliece / SPHINCS+.** NIST selected
  ML-KEM (Kyber) as the primary KEM standard; the others
  are signature schemes or alternates. Rejected.
- **PQ-only (no hybrid).** Premature given the relative
  novelty of ML-KEM; NSA guidance recommends hybrid.
  Rejected.
- **Wait for PQ adoption to mature.** Harvest-now-
  decrypt-later risk. Rejected.

## Consequences

**Positive.** Standards-aligned, validated-module
implementation, hybrid posture insulates against either
classical or PQ break. Partner federation can negotiate
common ML-KEM parameter sets.

**Negative.** Hybrid handshakes are larger / slower than
classical-only. Acceptable at boundary throughput.

## Assessor view (SCA-R)

ML-KEM at the boundary serves SC-12 (key establishment) and
SC-13 (cryptographic protection — FIPS-validated PQ KEM).
Hybrid mode is consistent with NSA's CNSA 2.0 timeline and
NIST PQC migration guidance.

## Trigger conditions for revisit

- NIST sunsets ML-KEM (extremely unlikely given FIPS 203
  status). Switch primitive.
- A specific partner requires a different PQ KEM
  parameter set. Add the parameter; do not change the
  primitive.

---

*See `GLOSSARY.md` for terms and acronyms.*
