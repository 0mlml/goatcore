# ADR 0405 — Bridge access split — Authelia for an external web viewer, TAK federation direct

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Related | [`0203-bridge-vpses-separate-from-relays.md`](0203-bridge-vpses-separate-from-relays.md), `0404-authelia-in-pocket.md`, `0819-tak-federation.md` |

## TL;DR (for a layperson)

Different external connections need different security models.
A web service exposed to external viewers needs interactive
authentication (a login screen). A machine-to-machine
federation between TAK servers needs mutual TLS authentication
with no human in the loop. We use different access patterns at
the bridge for each — Authelia for the web case (when active),
direct mutual-TLS for the federation case.

## Context

Two distinct integration patterns at the bridge:

1. **Human-driven web access** (e.g. an external map viewer
   for a specific user community). Interactive login via
   Authelia (`0404`).
2. **Machine-to-machine federation** (e.g. TAK server
   federation with a partner's federation hub). Mutual TLS authentication;
   no human in the loop.

Conflating these would either (a) force humans through cert-
based auth (bad UX), or (b) force machine federation through
web SSO (impossible / fragile).

## Decision

Bridge access is split by integration pattern:

| Pattern | Auth | Active service |
|---|---|---|
| **Human web access** | Authelia → Traefik ForwardAuth → mesh-internal service | Currently in pocket (`0404`) |
| **Machine federation** | Mutual TLS at the bridge socat / native handler | TAK federation TCP 9001 to a partner federation hub (`0819`) |

Properties:

- **Each pattern has its own bridge configuration.** No
  conflation.
- **Each has its own per-partner CIDR allowlist** at the
  bridge security group ([`0309`](0309-bridge-cidr-allowlist.md)).
- **Each has its own audit trail** (Authelia logs auth events,
  TAK federation logs at the application layer).
- **TAK federation is unaffected by Authelia state** — runs
  continuously regardless.

## Alternatives considered

- **Single auth pattern (Authelia for everything).**
  Forces machines through web SSO. Rejected.
- **Single auth pattern (mutual TLS for everything).** Forces
  humans through cert auth. Rejected.
- **Per-partner custom bridge.** Higher cost, no operational
  benefit over the per-pattern split. Rejected.

## Consequences

**Positive.** Each integration class uses the right pattern.
Authelia status (in pocket vs active) does not affect TAK
federation. Adding a new integration in either pattern is a
known operation.

**Negative.** Two bridge patterns to operate. Acceptable; the
TAK side is essentially zero-touch once configured.

## Assessor view (SCA-R)

Pattern-specific authentication serves IA-2 (identification
and authentication) at the right AAL for each pattern,
AC-3 (access enforcement), and AC-4 (information flow
enforcement — partner-CIDR allowlist + protocol-specific
auth). The mutual-TLS path uses the sovereign PKI
(`0400`) where federation
allows; partner-issued certificates are accepted where the
partner mandates.

## Trigger conditions for revisit

- A new integration pattern emerges (e.g. SAML-based partner
  federation, OAuth2 device flow). Add a third pattern
  rather than overload existing ones.
- A partner refuses mutual TLS and requires Web SSO for a
  machine federation. Negotiate; do not bend the categories.

---

*See `GLOSSARY.md` for terms and acronyms.*
