# ADR 0300 — Netbird as mesh control plane

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Related | [`0200-identity-based-mesh-overlay.md`](0200-identity-based-mesh-overlay.md), [`0301-patched-netbird-management-image.md`](0301-patched-netbird-management-image.md), [`../reference/mesh-technology-comparison.md`](https://github.com/usareur-af/goatdocs/blob/main/docs/reference/mesh-technology-comparison.md) |

## TL;DR (for a layperson)

NetBird is the open-source software that manages our mesh — it
issues identities to peers, keeps the registry of who's enrolled,
distributes access policies, and runs the signaling that helps
peers find each other. We chose it after evaluating every
comparable option (Headscale, ZeroTier, OpenZiti, iroh, plain
WireGuard). NetBird wins on operational maturity, BSL-license
acceptability, and the integration surface for our specific
needs.

## Context

The mesh control plane has tight requirements: open-source or
acceptable BSL, runs on our infrastructure (not SaaS), supports
NAT traversal, has a programmable management API, has an
established operator community, and has a clear path to the
non-trivial features we need (signal federation, custom
ACL plane, offline enrollment).

The full comparison matrix is at
[`../reference/mesh-technology-comparison.md`](https://github.com/usareur-af/goatdocs/blob/main/docs/reference/mesh-technology-comparison.md).

## Decision

NetBird is the mesh control plane. Specifically:

- **NetBird Management** runs on every site's mgmt-host pair
  (Router-A / Router-B). Active-active multi-site via Spock
  ([`0204`](0204-multi-site-active-active.md)).
- **NetBird Signal** runs on every site, federated via custom
  Forward RPC ([`0302`](0302-custom-federated-signal-server.md)).
- **NetBird Relay** runs on the three VPS relays
  ([`0202`](0202-three-vps-relay-topology.md)).
- **NetBird Client** runs on every peer.
- We run a **patched fork** built from source
  ([`0301`](0301-patched-netbird-management-image.md)).

License: NetBird is BSL with conversion-to-Apache-2.0. Our use
falls under the BSL grant. If license terms shift in a way that
disqualifies our use, we have a documented exit path
(per [`0106`](0106-sovereign-tooling-preference.md)).

## Alternatives considered

| Option | Why rejected |
|---|---|
| **Headscale** (Tailscale control-plane reimpl) | Excellent code quality but smaller ecosystem; ACL surface less flexible than NetBird at the time of selection. |
| **ZeroTier** | Centralized SaaS by default; self-hosted controller path exists but operator burden is high; not BSL/OSS-friendly. |
| **OpenZiti** | Identity-rich, but operationally heavier; closer to a service-mesh than an overlay. Better fit for application-layer auth than for the data plane. |
| **iroh** | Promising but immature for production at our scale. |
| **Plain WireGuard + custom orchestration** | Reinvents the management plane; high engineering cost; loses upstream community contributions. |
| **Tailscale (SaaS)** | Sovereignty issues; SaaS dependency violates Island Mode. |
| **libp2p** | Wrong shape for the mesh data plane: libp2p is application-layer multiplexed streams over QUIC/TCP, not a kernel VPN with a crypto-identity tunnel. Different threat model (no all-traffic transport encryption — peers must opt every flow into libp2p), different performance class (no kernel WireGuard fast path per [`0303`](0303-wireguard-data-plane.md)), and no FIPS path even at the boundary node (per [`0601`](0601-fips-pqc-boundary-node.md)). The DHT-discovery angle, where libp2p is genuinely strong, is a separate pattern parked at [`../design/phoenix-architecture.md`](../design/phoenix-architecture.md) §5.2 Pattern B; it does not replace the NetBird control plane. |

## Consequences

**Positive.** Mature operator community, well-understood
deployment shape, solid NAT traversal, BSL terms acceptable,
upstream continues to ship features we benefit from.

**Negative.** BSL is not Apache/MIT; we monitor license status.
Some features we need (signal federation, certain ACL behaviors)
are not upstream — we patch and contribute back where useful
(see `docs/upstream-prs/`).

## Assessor view (SCA-R)

NetBird Management governs AC-2 (account management — peers as
accounts), AC-3 (access enforcement — ACLs), AC-17 (remote
access — enrollment), IA-2/IA-3 (identification — peer keys).
The patched build ([`0301`](0301-patched-netbird-management-image.md))
goes through our signed build pipeline; supply chain controls
([0700-series](0700-chainguard-first.md)) apply.

The license posture is documented in
[`0106`](0106-sovereign-tooling-preference.md); BSL with
defined change date is acceptable per that ADR.

## Trigger conditions for revisit

- BSL terms shift unfavorably; activate exit path.
- A specific upstream feature path is closed (e.g. Forward RPC
  removed). Patch deeper or fork harder.
- An evaluated alternative materially overtakes NetBird on the
  matrix at
  [`../reference/mesh-technology-comparison.md`](https://github.com/usareur-af/goatdocs/blob/main/docs/reference/mesh-technology-comparison.md).

---

*See `GLOSSARY.md` for terms and acronyms.*
