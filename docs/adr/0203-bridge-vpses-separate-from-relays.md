# ADR 0203 — Network bridge VPSes separate from relay VPSes

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Supersedes | — |
| Superseded by | — |
| Related | [`0202-three-vps-relay-topology.md`](0202-three-vps-relay-topology.md), [`0309-bridge-cidr-allowlist.md`](0309-bridge-cidr-allowlist.md), `0819-tak-federation.md` |

## TL;DR (for a layperson)

We run a second set of three cloud servers — separate from the
relay servers — whose only job is to bridge our network to
external partner networks (partner command-and-control systems,
partner sensor systems, TAK federation). Keeping these "bridges" separate from the "relays"
means each kind of server has one clear purpose, one clear set of
firewall rules, and one clear failure mode. Mixing them would
mean every partner integration affects mesh enrollment.

## Context

Two different external-facing concerns share an instinct to live
on the same VPSes: enrollment ingress (the public-internet
on-ramp for new peers) and partner egress (the network-bridge
endpoint for partner integrations). Combining them creates two
problems:

1. **Different security postures.** Relays should be locked down
   to mesh-management traffic only. Bridges have to allow
   per-partner CIDR ranges on specific ports. Mixing means the
   relay's tight default-deny posture has to relax for partner
   ranges, expanding blast radius.
2. **Different failure semantics.** A partner integration outage
   should not affect mesh enrollment, and vice versa. Combined
   VPSes couple them.

## Decision

Three bridge VPSes, geographically distributed across notional cloud
regions, run partner network bridging — separate from the three
relay VPSes. Concrete cloud-region selection is operational and
lives in `terraform/`; bridge identifiers are opaque short codes.
Each bridge has its own DNS name, and all three sit round-robin
behind one shared bridge name.

Each bridge runs:

- **NetBird client** — bridge VPSes are mesh peers (relays are
  not).
- **socat TCP forwarders** — partner-specific port forwards from
  public Elastic IP to mesh-internal services.
- **Default-deny security groups** — partner-specific CIDRs
  are added explicitly per peering.

Elastic IPs (one per bridge) give partners a stable allowlist
target. SSH access is via mesh IP through WireGuard
(unlike relays, which are SSM-only).

See `../operations/bridge-peering.md`
for the per-partner workflow and `../design/relay-architecture.md`
for the relay-vs-bridge architectural distinction.

## Alternatives considered

- **Combined relay + bridge VPSes.** Couples enrollment and
  partner failure modes; expands relay blast radius. Rejected.
- **Bridge per partner (no shared bridge VPS).** Higher cost,
  more management surface, marginal isolation gain. Rejected;
  per-bridge default-deny + partner-specific socat config gives
  us most of the isolation benefit at a fraction of the cost.
- **Bridge inside the management host.** Brings partner traffic
  into the same node as the management plane; expands attack
  surface in the wrong direction. Rejected.

## Consequences

**Positive.** Relay and bridge security postures evolve
independently. A partner integration change is isolated to
bridge config. Bridge VPSes can be peers (and use mesh-internal
DNS) while relays remain externally addressable only.

**Negative.** Six VPSes instead of three. Cost ~$60/mo extra,
plus management surface. Accepted; the isolation benefits
exceed the cost.

## Assessor view (SCA-R)

Bridge VPSes carry SC-7 (boundary protection — the partner-
federation boundary) and AC-4 (information flow enforcement —
allowed flows are explicit per-partner per-port). Default-deny
posture serves CM-7 (least functionality). Partner-CIDR
allowlists are documented per peering; see
[`0309`](0309-bridge-cidr-allowlist.md) for the discipline.

The bridge boundary is *complementary* to the FIPS-PQC boundary
([`0601`](0601-fips-pqc-boundary-node.md)) — bridges handle
TCP forwarding for partner systems that do not need post-
quantum or FIPS-validated transport; the boundary node handles
the cryptographic boundary for those that do.

## Trigger conditions for revisit

- A new partner integration class emerges that requires
  bridge-side application logic (not just TCP forwarding).
  Build a separate service; do not co-locate on bridge VPSes.
- Partner-CIDR allowlists become unmanageable at scale.
  Investigate; consider per-partner sub-bridges; do not
  collapse into shared.

---

*See `GLOSSARY.md` for terms and acronyms.*
