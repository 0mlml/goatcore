# ADR 0305 — Rathole tunnel (TCP 443 mgmt forwarding)

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Related | [`0202-three-vps-relay-topology.md`](0202-three-vps-relay-topology.md), [`0301-patched-netbird-management-image.md`](0301-patched-netbird-management-image.md) |

## TL;DR (for a layperson)

Our network host runs the management plane behind a residential-
class internet connection without port forwarding. We need
peers anywhere on the internet to reach the management plane.
Rathole is a small open-source tool that creates a persistent
outbound connection from the management host to each VPS relay
and forwards inbound TCP 443 traffic through it. That gives the
management plane a public-internet on-ramp without needing port
forwarding at the network edge.

## Context

The upstream link does not provide port forwarding. The
network host is behind that link with no inbound public
reachability.

The conventional solution is a reverse-tunnel (the host opens
an outbound connection to a public-IP server, and inbound
traffic gets forwarded back through that connection). Several
implementations exist; we evaluated frp, rathole, ngrok-self-
hosted, and Cloudflare Tunnel.

## Decision

Rathole is the reverse-tunnel layer. Configuration:

- **Server side**: rathole runs on each of three VPS relays
  ([`0202`](0202-three-vps-relay-topology.md)).
- **Client side**: rathole runs on the network host (mgmt-host).
- **Forward**: TCP 443 from the public side of the relay to
  netbird-management on the network host.
- **Authentication**: shared token, rotated on relay rebuild.
- **TLS**: terminated by Traefik on the network host (rathole
  forwards opaque TCP).

Rathole was chosen over alternatives for: small attack surface,
Rust implementation (memory safety), no kernel-module dependency,
clear configuration, active upstream.

## Alternatives considered

- **frp.** Larger feature surface; older codebase. Rejected on
  attack-surface grounds.
- **ngrok self-hosted.** Service-oriented design; more
  operational overhead. Rejected.
- **Cloudflare Tunnel.** SaaS dependency; sovereignty concern.
  Originally evaluated then dropped (see CLAUDE.md).
- **Custom WireGuard tunnel between mgmt-host and relays.**
  Reinvents what rathole provides. Rejected.

## Consequences

**Positive.** Small, auditable, single-purpose. Survives
host reboot via systemd. Token rotation is a one-line operation.

**Negative.** Rathole upstream is small; if it stalls, we may
need to fork. Acceptable risk; substitution is straightforward
(replace one binary).

## Assessor view (SCA-R)

Rathole serves SC-7 (boundary protection — controlled inbound
TCP via a known forwarding policy) and AC-17 (remote access —
the public-internet on-ramp is rathole-mediated). The Rust
implementation is a positive trust signal. The token-based
authentication is rotated on relay rebuild
(see `terraform/modules/relay/`). No mission data passes
through rathole un-TLS'd.

## Trigger conditions for revisit

- Rathole upstream stalls (no commits in 12 months). Fork or
  substitute.
- The upstream link gains port forwarding. Reverse-tunnel
  no longer needed.
- A specific authorization disallows third-party reverse-tunnel
  software. Substitute or build in-house.

---

*See `GLOSSARY.md` for terms and acronyms.*
