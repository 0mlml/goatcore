# ADR 0202 — Three-VPS relay topology with DNS round-robin enrollment

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Supersedes | — |
| Superseded by | — |
| Related | [`0201-enrollment-vs-mesh-op-separation.md`](0201-enrollment-vs-mesh-op-separation.md), [`0203-bridge-vpses-separate-from-relays.md`](0203-bridge-vpses-separate-from-relays.md), [`0306-p2p-with-relay-fallback.md`](0306-p2p-with-relay-fallback.md) |

## TL;DR (for a layperson)

Devices joining the network and devices that cannot reach each
other directly need a public on-ramp. We run three small cloud
servers, geographically distributed, that act as that on-ramp.
Three is enough for redundancy; geographic distribution gives us
a working server within reasonable latency from any forward
location; DNS round-robin handles the rest. None of these servers
holds mission data — they only forward traffic that is already
encrypted end-to-end.

## Diagram



Source: [`diagrams/0202-three-vps-relay-topology.mmd`](diagrams/0202-three-vps-relay-topology.mmd).

## Context

Satellite terminals and mobile devices typically sit
behind CGNAT, with no public ingress. The mesh management plane
needs to be reachable from anywhere on the internet for new-peer
enrollment, and the WireGuard data plane needs a fallback path
when peer-to-peer fails. Both functions need a public-internet
presence we own.

A single relay introduces a single point of failure. Two
introduces split-brain risks. Three is the minimum for a quorum
that does not require quorum logic — DNS round-robin gives us
load distribution and failure tolerance without adding control-
plane complexity.

## Decision

Three VPS relays, geographically distributed across notional cloud
regions, run the public-internet on-ramp. Concrete cloud-region
selection is operational and lives in `terraform/`; the relay
identifiers are opaque short codes — this ADR makes no region commitment beyond "three
geographically distributed cloud regions."

| Relay (opaque code) | DNS |
|---|---|
| relay 1 | round-robin entry on `netbird.example` |
| relay 2 | round-robin entry on `netbird.example` |
| relay 3 | round-robin entry on `netbird.example` |

Each relay runs:

- **rathole** on TCP 443 — forwards mgmt-plane HTTPS to the network host.
- **Coturn** on UDP 3478 — STUN for NAT discovery / hole-punching.
- **Netbird relay** on TCP 33073 — single-hop data-relay fallback.
- **Native dnsmasq** on the relay itself — local mesh DNS resolution.

Relays are managed via AWS SSM (no SSH); credentials are held in
AWS Secrets Manager per region. Terraform is the source of truth;
Ansible carries config. See `../design/relay-architecture.md`.

DNS round-robin handles distribution and one-region failure
tolerance. Two-region simultaneous failure is an accepted residual
risk — peers are typically already enrolled, and the data plane
keeps working without management-plane reachability per
[`0201`](0201-enrollment-vs-mesh-op-separation.md).

## Alternatives considered

- **Single relay.** Single point of failure. Rejected.
- **Two relays.** Split-brain risk; round-robin is awkward.
  Rejected.
- **Five+ relays.** Marginal additional reliability for
  meaningful additional cost; AWS region redundancy already
  handles the residual. Revisit if global coverage gaps emerge.
- **Self-hosted on partner infrastructure.** Loses the
  geographic-distribution and quick-replacement properties of
  cloud VPSes. Rejected as primary; partner-hosted relays
  could supplement.

## Consequences

**Positive.** A peer anywhere on the internet can reach the
management plane within tolerable latency. One region failing is
absorbed by DNS round-robin without operator intervention.
Self-managed (we own the IaC) so we can replace a relay without
vendor cooperation.

**Negative.** Three VPSes are three more attack surfaces. The
relay nodes are deliberately minimal (no SSH, SSM-only access,
TLS-terminated only at the management host, all data already
encrypted end-to-end) to minimize blast radius. Cost is ~$30/mo
per relay — accepted.

## Assessor view (SCA-R)

SC-7
(boundary protection — at the public-ingress edge), AU-2 (event
logging — relay logs ship to the central observability stack),
SI-4 (system monitoring), and CP-2 (contingency — single-region
failure is absorbed automatically). The relay nodes process *no*
mission data — they forward already-encrypted WireGuard or TLS-
terminated mgmt traffic; the data classification at the relay
nodes is "transport metadata only."

Note: relay VPSes are *not* mesh peers. Bridge VPSes
([`0203`](0203-bridge-vpses-separate-from-relays.md)) are mesh
peers and have a different role.

## Trigger conditions for revisit

- A region develops sustained capacity issues. Replace the
  affected relay; do not change the count.
- Coverage gaps emerge that three regions cannot serve.
  Add a fourth; do not collapse to a smaller set.
- A specific partner requires sovereign-host relays in their
  jurisdiction. Add per-partner; do not move global relays.

---

*See `GLOSSARY.md` for terms and acronyms.*
