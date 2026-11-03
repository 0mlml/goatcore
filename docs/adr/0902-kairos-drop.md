# ADR 0902 — Kairos drop — could not deliver FIPS 140-3 + DISA STIG

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Related | [`0900-ubuntu-lts-luks-x86.md`](0900-ubuntu-lts-luks-x86.md), [`0901-ubuntu-tegra-orin.md`](0901-ubuntu-tegra-orin.md), [`../design/orin-os-decision.md`](../design/orin-os-decision.md) |

## TL;DR (for a layperson)

Earlier in the project we evaluated and prototyped Kairos —
an immutable-OS distribution. Immutability is operationally
attractive (the OS can't drift; boot is reproducible). We
dropped it because Kairos cannot deliver the FIPS 140-3 + DISA
STIG compliance the project requires. The
operational benefits we wanted from Kairos are now achieved
through other means in our Ubuntu+LUKS stack.

## Context

Kairos was evaluated for both x86 mgmt-hosts and Orin nodes.
The properties we wanted: immutable root, OTA discipline,
phoenix-native operational pattern.

Two obstacles forced the drop:

1. **FIPS 140-3 module pathway** — Kairos's kernel does not
   ship a FIPS-validated cryptographic module on a path we
   can rely on.
2. **DISA STIG hardening** — Kairos lacks the Ubuntu Pro
   USG-STIG ansible role equivalent. We would have to build
   and maintain STIG hardening ourselves on top of an
   immutable OS, which is operationally harder, not easier.

Phoenix-native operational properties
([`0208`](0208-phoenix-native-peer-discipline.md)) are
achieved through capability-based discovery, supply-chain-
delivered binaries, and the cruft test — not through OS
immutability.

## Decision

Kairos is dropped from the system entirely:

- **x86**: Ubuntu 24.04 LTS + Ubuntu Pro
  ([`0900`](0900-ubuntu-lts-luks-x86.md)).
- **Orin**: Ubuntu-for-Tegra 22.04 + Ubuntu Pro
  ([`0901`](0901-ubuntu-tegra-orin.md)).
- **`ops/goat/` deleted** from the repo (commit history
  retains it for reference).
- **Block 6B split** to reflect the Ubuntu-Pro path.

The decision and replacement map are documented in
[`../design/orin-os-decision.md`](../design/orin-os-decision.md).

## Alternatives considered

- **Continue Kairos with FIPS / STIG built ourselves.**
  Operationally untenable. Rejected.
- **Wait for Kairos FIPS support.** Indefinite timeline.
  Rejected.

## Consequences

**Positive.** Compliance channel is well-supported. Operator
team can lean on Ubuntu Pro tooling.

**Negative.** Loses Kairos's immutable-OS property. We
substitute with phoenix-native peer discipline, dm-verity
on read-only partitions, and supply-chain attestation.

## Assessor view (SCA-R)

This ADR is the explicit answer to "did you consider an
immutable OS?" — yes, we evaluated Kairos and dropped it for
named compliance reasons. Reviewers familiar with Kairos
will recognize the question; the answer is in this ADR plus
[`../design/orin-os-decision.md`](../design/orin-os-decision.md).

The replacement controls (phoenix-native discipline + Ubuntu
Pro FIPS + USG STIG + boot-chain integrity) cover the same
risk surface Kairos was load-bearing for.

## Trigger conditions for revisit

- Kairos gains FIPS 140-3 + DISA STIG support upstream.
  Re-evaluate.
- A specific authorization mandates immutable-OS posture.
  Negotiate.

---

*See `GLOSSARY.md` for terms and acronyms.*
