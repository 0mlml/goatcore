# ADR 0806 — Egress proxy 5-tier cascade

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goatPolicy |
| Related | [`0805-policy-engine-active-passive.md`](0805-policy-engine-active-passive.md), [`0811-audit-log-integrity.md`](0811-audit-log-integrity.md), [`../design/egress-proxy.md`](../design/egress-proxy.md), `../project/egress-proxy-implementation.md`, [`../operations/egress-runbook.md`](../operations/egress-runbook.md) |

## TL;DR (for a layperson)

Without controls, an enrolled mesh peer could freely access
the public web — which is a back-door we close before Phase 3
production. Our egress proxy works in five layers: block
encrypted-DNS so peers must use our resolver, default-deny
network rules, an HTTP/HTTPS forward proxy that allow-lists
specific destinations, a SOCKS5 proxy for narrow non-HTTP
flows, and time-bounded direct allowlists for exceptions.
Every approval flows through the per-site policy engine.

## Diagram



Source: [`diagrams/0806-egress-proxy-cascade.mmd`](diagrams/0806-egress-proxy-cascade.mmd).

## Context

An enrolled peer with a working WG tunnel has IP
connectivity. Without egress controls, that peer can browse
the web, exfiltrate data, or pull untrusted code. The
egress-proxy design (Block 17) closes the gap.

The five-tier pattern lets us start strict (default-deny) and
selectively open paths through documented mechanisms.

## Decision

Egress is governed by a 5-tier cascade per site:

| Tier | Layer | Mechanism |
|---|---|---|
| **0. DNS egress control** | Network | Block DoH/DoT on the egress; force mesh resolver. |
| **1. Default-deny nftables** | Router-A/B | All egress denied except explicit allowlist. |
| **2. Squid HTTP/HTTPS forward proxy** | Application (HTTP) | SNI-allowlist; no TLS MITM. |
| **3. dante SOCKS5** | Application (other) | Narrow non-HTTP flows (NTP, package mirrors, partner protocols). |
| **4. Direct CIDR allowlist** | Network | Time-bounded; auto-expiry; per-flow. |

Audit-log integrity sub-tier (§5.6 in
[`../design/egress-proxy.md`](../design/egress-proxy.md)):
per-site MinIO with Object Lock, cross-mounted peer-mgmt-host
storage, integrity ledger, daily tamper-detection CI. See
[`0811`](0811-audit-log-integrity.md).

Approval workflow runs on the per-site policy engine
([`0805`](0805-policy-engine-active-passive.md)).

## Alternatives considered

- **Default-allow with deny-by-exception.** Inversion of
  safety. Rejected.
- **Single tier (e.g. just default-deny + direct
  allowlists).** Operationally awkward — every web access
  needs a CIDR. Rejected.
- **TLS MITM forward proxy.** Adds CA distribution to peers
  + breaks pinned-cert apps. Rejected.

## Consequences

**Positive.** Closed the back-door for enrolled peers.
Each tier handles a different egress class cleanly.

**Negative.** Initial allowlist tuning is real work. We
accept; tuning happens once and stabilizes.

## Assessor view (SCA-R)

Egress proxy serves AC-4 (information flow enforcement),
SC-7 (boundary protection), AU-2 (every flow audited), and
SI-4 (system monitoring). The 5-tier cascade is reviewable
in [`../design/egress-proxy.md`](../design/egress-proxy.md);
each tier's policy is in code.

The comparative-posture artifact identifies "egress control"
as a dimension where goat is meaningfully ahead.

## Trigger conditions for revisit

- A peer class genuinely needs broader egress than the
  cascade allows. Add a Tier-4 entry with time bound.
- A new egress class emerges (e.g. WireGuard-over-WireGuard).
  Add a tier rather than retrofit existing ones.

---

*See `GLOSSARY.md` for terms and acronyms.*
