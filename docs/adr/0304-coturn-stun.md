# ADR 0304 — Coturn STUN on each relay

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Related | [`0202-three-vps-relay-topology.md`](0202-three-vps-relay-topology.md), [`0306-p2p-with-relay-fallback.md`](0306-p2p-with-relay-fallback.md) |

## TL;DR (for a layperson)

For two devices to talk directly when they're both behind
internet routers (NAT), they need to learn each other's public
addresses. STUN is the standard protocol that helps with this.
We run an open-source STUN server called Coturn on each of our
three relay servers.

## Context

Most peers sit behind NAT. WireGuard P2P requires that each
peer know the other's public address-and-port mapping. STUN is
the standard discovery protocol. Coturn is the dominant
open-source STUN/TURN implementation.

We want STUN-only (not TURN). TURN would relay user data
through the server; we have a separate relay path
([`0306`](0306-p2p-with-relay-fallback.md)) and do not want STUN
servers double-booked as TURN servers.

## Decision

Each of the three VPS relays runs Coturn as STUN-only on UDP
3478. Configuration:

- **Listen UDP 3478** on the relay's public IP.
- **No TURN credentials** issued; Coturn is bound to STUN
  responses only.
- **No long-term credential database**; STUN is unauthenticated
  (consistent with the protocol).
- **No TLS-STUN.** STUN responses contain mapped addresses,
  not user data; TLS would add cost without security benefit.

DNS round-robin on `netbird.legacy.example` includes the STUN
servers.

## Alternatives considered

- **Public STUN servers (Google, Cloudflare).** Reliability and
  privacy concerns; accept relay-side telemetry inadvertently.
  Rejected.
- **Self-built STUN.** Reinvents Coturn. Rejected.
- **Run Coturn as TURN as well.** Doubles attack surface;
  conflates roles with the data-relay path. Rejected.

## Consequences

**Positive.** Sovereign STUN. P2P NAT traversal works for the
common cases. No dependency on third-party STUN.

**Negative.** Coturn is one more piece of software to keep
patched. We use a Chainguard-hardened image
([`0700`](0700-chainguard-first.md)) and pin by digest.

## Assessor view (SCA-R)

STUN serves SC-7 (boundary protection — peer-discovery boundary)
and AU-2 (event logging — STUN access logs ship to the central
observability stack). STUN responses contain mapped addresses
only; no mission data crosses Coturn. Data classification at
Coturn: peer mapped-address metadata.

## Trigger conditions for revisit

- A NAT type emerges that STUN cannot handle. Engage at the
  relay-fallback path ([`0306`](0306-p2p-with-relay-fallback.md)).
- Coturn upstream stalls. Evaluate alternatives.

---

*See `GLOSSARY.md` for terms and acronyms.*
