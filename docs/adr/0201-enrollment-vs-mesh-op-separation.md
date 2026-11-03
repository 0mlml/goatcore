# ADR 0201 — Enrollment-vs-mesh-operation separation as resilience model

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Supersedes | — |
| Superseded by | — |
| Related | [`0200-identity-based-mesh-overlay.md`](0200-identity-based-mesh-overlay.md), [`0206-island-mode-by-construction.md`](0206-island-mode-by-construction.md), [`0308-offline-enrollment-cbor-bundles.md`](0308-offline-enrollment-cbor-bundles.md) |

## TL;DR (for a layperson)

A device only needs to talk to the central management server *once*
— when it joins the network. After that, the management server is
not in the path of any actual traffic. If the management server
goes down, or the internet goes down, or a partner network
disappears, devices that have already joined keep talking. This is
why our network survives outages that would take down a typical
centralized system.

## Context

Two different connectivity requirements get conflated in many
network designs: the requirement at *enrollment time* (a new device
needs to obtain credentials and policy) and the requirement at
*operation time* (peers need to communicate). Centralized designs
take both dependencies. Mesh designs that handle this well separate
them.

Separating them is what produces the system's resilience to
internet outages, management-plane outages, and contested-
environment partitions. It is the architectural reason island-mode
operation is possible.

## Decision

goat treats enrollment and mesh operation as two
distinct phases with different connectivity requirements.

**Enrollment phase.** A peer connects to `netbird.legacy.example`
(DNS round-robin → VPS relay → rathole → management host) using a
setup key. The management host issues credentials, policy, and
peer registry data. This is the only time the management plane
must be reachable. Enrollment can also happen over an offline-
bundle path ([`0308`](0308-offline-enrollment-cbor-bundles.md)) if
the management plane is not reachable.

**Mesh operation phase.** Once enrolled, peers exchange WireGuard
keys and policy directly with each other (P2P, with single-hop
relay fallback — see [`0306`](0306-p2p-with-relay-fallback.md)).
The management plane is *not* in the data path. Existing tunnels
persist across management-plane outages, internet outages, and
peer reboots. Peers on the same LAN keep talking with no internet
at all.

**The management plane is needed again only for:**

- Enrolling new peers.
- Changing ACL policies.
- Rotating keys.
- Revoking peers.

Each of those events is *occasional* and *latency-tolerant*; none
is in the steady-state data path.

## Alternatives considered

- **Continuous management-plane participation.** Familiar from
  cloud-managed VPN products. Loses island-mode survivability.
  Rejected.
- **Fully decentralized (no management plane).** Loses
  centralized policy authoring, audit, and revocation. Rejected.
- **Relay-mediated traffic by default (no P2P).** Adds latency,
  defeats the point of WireGuard, and re-introduces the central
  bottleneck. Rejected; relay is fallback only.

## Consequences

**Positive.** Steady-state operation survives any single failure
in the management plane, the relay set, or the enrollment path.
Pre-enrollment of devices before deployment becomes a deliberate
operational discipline that pays off at every subsequent partition.

**Negative.** Pre-enrollment is now a logistics step. Devices that
arrive in the field without enrollment must either (a) reach an
online management plane to enroll, or (b) consume an offline
enrollment bundle. Both are documented; neither is automatic.

## Assessor view (SCA-R)

The separation maps onto NIST SP 800-53 SC-5 (denial of service
protection — system continues operating under management-plane
loss), SC-7 (boundary protection — no central choke point), CP-2
(contingency plan — island mode is a normal operating mode, not
a contingency), and AC-17 (remote access — the only remote-access
interaction is enrollment, with a clear scope).

Resilience-relevant ADRs that depend on this separation:
[`0206`](0206-island-mode-by-construction.md),
[`0204`](0204-multi-site-active-active.md),
[`0306`](0306-p2p-with-relay-fallback.md).

## Trigger conditions for revisit

- A specific partner authorization requires continuous
  management-plane participation. Negotiate; do not concede.
- Operational evidence shows pre-enrollment friction is
  unsustainable. Add the offline-bundle path as the primary,
  not the alternative.

---

*See `GLOSSARY.md` for terms and acronyms.*
