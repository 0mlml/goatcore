# ADR 0309 — Bridge VPS partner-CIDR allowlist + default-deny

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Related | [`0203-bridge-vpses-separate-from-relays.md`](0203-bridge-vpses-separate-from-relays.md), `0819-tak-federation.md`, `../operations/bridge-peering.md` |

## TL;DR (for a layperson)

Our bridge servers (used for partner network integration) start
in default-deny mode — no inbound traffic allowed at all. Each
partner integration explicitly adds the partner's specific IP
ranges and the specific ports needed. This is the opposite of
the default "allow inbound, deny by exception" pattern, and it's
how we prevent accidental partner-network exposure.

## Context

Bridge VPSes ([`0203`](0203-bridge-vpses-separate-from-relays.md))
are the public-internet endpoint for partner traffic (partner command-and-control
systems, partner agencies, partner sensor systems). They run socat TCP forwarders
that take inbound traffic on partner-specific ports and forward
into the mesh. The risk: a misconfiguration or stale forwarder
exposes partner ports to the public internet.

Default-deny prevents this by construction.

## Decision

Bridge VPSes follow a strict default-deny posture:

- **Security group default:** all inbound denied.
- **Per-partner peering** adds explicit rules: partner CIDR(s)
  → specific port(s) only.
- **No wildcard / 0.0.0.0/0 rules** for partner-port access.
- **Outbound:** allowed for socat connect-back and for routine
  egress (DNS, NTP, package repos via egress proxy
  [`0806`](0806-egress-proxy-cascade.md)).
- **SSH:** via mesh IP only; no public SSH (consistent with
  relay posture).
- **Documentation:** per-partner peering captured in
  `../operations/bridge-peering.md`.

Each peering is a Terraform change with a named module call. A
new partner integration is a PR that adds the partner CIDR(s)
to the security group.

## Alternatives considered

- **Default-allow with deny-by-exception.** Inversion of safety;
  one missed rule exposes a port to the world. Rejected.
- **Application-layer auth on every forwarded port.** Useful
  complementary control but does not substitute for network-
  layer default-deny. Layered.
- **Per-partner separate VPS.** Higher cost / management
  surface; default-deny on shared bridge gives equivalent
  network isolation. Rejected.

## Consequences

**Positive.** A misconfiguration cannot accidentally expose
partner ports to the world. Adding a partner is a deliberate,
reviewable PR. Audit posture is mechanical (read the SG rules
to see exactly what's exposed).

**Negative.** Onboarding a partner has a brief delay (Terraform
apply + verification). Acceptable.

## Assessor view (SCA-R)

The bridge default-deny posture serves AC-4 (information flow
enforcement), CM-7 (least functionality), and SC-7 (boundary
protection). The per-partner CIDR allowlist is a reviewable
artifact in Terraform — `terraform/modules/bridge/`. NIST SP
800-41 firewall management guidance is consistent with the
posture.

## Trigger conditions for revisit

- A partner refuses to provide a stable CIDR. Negotiate; do not
  weaken default-deny.
- The per-partner SG-rule count exceeds operational scale
  (>50 rules per bridge). Per-bridge sub-bridges or partner-
  specific bridges; do not collapse to wildcards.

---

*See `GLOSSARY.md` for terms and acronyms.*
