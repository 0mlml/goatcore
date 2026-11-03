# ADR 0306 — P2P preferred, single-hop relay fallback

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Related | [`0202-three-vps-relay-topology.md`](0202-three-vps-relay-topology.md), [`0303-wireguard-data-plane.md`](0303-wireguard-data-plane.md), [`0304-coturn-stun.md`](0304-coturn-stun.md) |

## TL;DR (for a layperson)

When two peers can talk directly, they do — that's the fast,
private, reliable path. When they can't (hard NAT, asymmetric
routing, partner network restrictions), they fall back through
one of our three VPS relays, which forwards the encrypted
traffic. Even on the relay path, the encryption is end-to-end:
the relay sees ciphertext only.

## Diagram



Source: [`diagrams/0306-p2p-with-relay-fallback.mmd`](diagrams/0306-p2p-with-relay-fallback.mmd).

## Context

Mesh peers ideally communicate directly via WireGuard P2P. But
some NAT types (hard symmetric NAT, double-NAT under partner
infrastructure) defeat hole-punching. Without a fallback, those
peers cannot reach each other.

The fallback options are:

1. **Multi-hop overlay** (mesh-routed). Adds complexity, latency,
   and operational state. Not justified at our scale.
2. **Single-hop relay**. A pre-deployed relay forwards encrypted
   traffic between two peers that cannot connect directly.

We pick single-hop relay; it preserves end-to-end encryption,
adds bounded latency, and uses infrastructure we already run.

## Decision

Mesh-data routing is P2P-preferred with single-hop relay
fallback:

| Path | Conditions |
|---|---|
| Direct P2P | NAT traversal succeeds via STUN ([`0304`](0304-coturn-stun.md)) + UPnP / hole-punching. |
| Local-LAN P2P | Peers on the same LAN; the ICE host-candidate pair is selected. Preferred by priority, not guaranteed (see Path selection); whether same-LAN pairs actually hold this path is measured by M-110 (issue #240). |
| Single-hop relay | NAT traversal fails; one of the three NetBird relays at TCP 33073 forwards traffic. |

Properties:

- **Encryption is end-to-end** in all cases (WireGuard).
- **Relay sees ciphertext only.** No plaintext at the relay.
- **Path selection** is per-pair ICE in the NetBird client
  (0.75.1's pinned pion fork; mechanism from the source reading
  in issue #240): each side gathers host (LAN), srflx (public,
  via STUN) and relay candidates; all pairings are probed and
  the controlling side takes the highest-priority validated
  pair. Host pairs rank highest and are eligible immediately;
  srflx pairs wait 500 ms (pion's default, which netbird leaves
  in place: `SrflxAcceptanceMinWait` is settable in `AgentConfig`
  but not overridden, and no netbird switch reaches it); relay
  pairs 2 s (pion's default as well, and the one wait netbird
  exposes to an operator, `NB_ICE_RELAY_ACCEPTANCE_MIN_WAIT_SEC`
  in the client's ICE env).
  The selected pair is then KEPT for the life of
  the connection — the pinned fork predates pion's
  renomination, so there is NO periodic re-evaluation. A same-
  LAN pair therefore lands on the LAN unless the LAN probe is
  blocked (firewall, AP client isolation — the F-206 class),
  the srflx pair wins the race, or a transient at boot picked
  WAN first; each of those keeps the WAN path until the
  connection dies. No NetBird knob forces the LAN candidate;
  only relay-forcing switches exist.
- **Same-LAN scope**: established tunnels survive a WAN outage
  (WireGuard is serverless once up), but a NEW or broken same-
  LAN session cannot re-form while the central signal server is
  unreachable — ICE candidate exchange transits it even for two
  peers on one switch, and signal is not federated
  (relay-architecture
  §Signal server limitations). On goatnets whose management is
  cloud-resident, "same-LAN peers keep working with no
  internet" therefore holds for existing sessions only; sites
  running on-prem management (ADR 0826) keep signaling local.
- **Relay capacity**: relays are sized for ~10% of the fleet's
  data on the relay path. P2P-success-rate alerting fires if the
  relay-path fraction climbs. That metric cannot see a same-LAN
  pair hairpinning over WAN — a srflx-direct connection IS a
  P2P success — so LAN-adjacency is watched separately by the
  mesh-health `goat_mesh_lan_peers_hairpinning` series against
  the declared `mesh_health_lan_peers` roster (issue #240).
- **Latency budget**: relay path adds 10-100ms depending on
  geography; acceptable for mesh control and most data flows.

## Alternatives considered

- **No fallback (P2P-only).** Some peer pairs simply cannot
  connect. Unacceptable.
- **Relay-mediated by default.** Defeats the WireGuard
  performance benefit; expands relay attack surface. Rejected.
- **Multi-hop mesh routing.** Heavier, more state, more failure
  modes. Rejected at our scale.

## Consequences

**Positive.** Most flows are direct (cheap, fast, private).
Hard-NAT pairs still work via the relay. End-to-end encryption
preserved. Relay capacity is bounded.

**Negative.** Relay-path latency is higher; some flows feel
the difference. Operationally we monitor P2P-success-rate as
a health metric.

## Assessor view (SCA-R)

The fallback pattern serves SC-5 (DoS — single-pair
unreachability is absorbed), SC-8 (transmission confidentiality
— end-to-end encryption preserved on the relay path), and AU-2
(event logging — relay-path usage is recorded).

The relays are inside the authorization boundary
([`0202`](0202-three-vps-relay-topology.md)); they handle only
ciphertext, never plaintext mission data.

## Trigger conditions for revisit

- P2P-success-rate trends down. Investigate (NAT-type
  evolution, partner-network restrictions); the fallback is
  doing its job, but high relay-path fraction is operationally
  expensive.
- `goat_mesh_lan_peers_hairpinning` sustained above 0 on any
  site (issue #240). P2P-success-rate is blind to this failure:
  a declared same-LAN pair transiting srflx or relay burns the
  site's WAN uplink while counting as a P2P success. Follow the
  M-110 decision gate — LAN probe blocked (fix reachability),
  race/stickiness (bounce heals it; consider the watchdog), or
  upstream (the srflx wait and no-renomination live in the
  pinned pion fork).
- Relay capacity becomes a bottleneck. Add a fourth relay
  region rather than redesigning the model.

---

*See `GLOSSARY.md` for terms and acronyms.*
