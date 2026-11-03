# ADR 0211 — Single management server (no federation between mgmt servers)

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Supersedes | — |
| Superseded by | — |
| Related | [`0204-multi-site-active-active.md`](0204-multi-site-active-active.md), [`0300-netbird-as-mesh-control-plane.md`](0300-netbird-as-mesh-control-plane.md) |

## TL;DR (for a layperson)

Although every site runs a copy of the management plane (so any
one site can keep working in isolation), all those copies share a
single logical management server. There is no federation between
separate, distinct management servers. This keeps peer identity,
ACL policy, and the peer registry globally consistent — a peer
enrolled at one site is a peer at every site, immediately.

## Context

Two related but distinct architectures get confused:

1. **Multi-site active-active management plane.** Every site
   runs a complete copy of *one logical* management server.
   Spock multi-master keeps them in sync. This is what we have
   ([`0204`](0204-multi-site-active-active.md)).
2. **Federated separate management servers.** Each site (or
   each partner) runs *its own logical* management server, with
   peer-identity-and-policy federation between them. This is
   *not* what we have, and we explicitly reject it.

Federated separate management servers are an attractive option
for some scenarios (sovereign per-site IdP control, partner-
specific peer scoping). They impose costs: peer identity is
per-server-pair, ACLs require federation negotiation, key
rotation cascades across federations, and the integration
surface for new partners is much larger.

## Decision

goat operates one logical management server, replicated
across sites for active-active operation. Specifically:

- **One peer registry** — a peer enrolled at any site is
  enrolled across the fleet.
- **One ACL policy plane** — ACLs apply globally; per-site or
  per-partner scoping happens via group membership and ACL
  rules, not via separate authorities.
- **One signal-server federation graph** — every signal-server
  knows about every other (full mesh of Forward RPC
  relationships).
- **One offline CA** — see `0406`
  — one Ed25519 root, one CRL pipeline.

Future-state scope: cross-organization federation (goat
↔ a partner's separate mesh) is supported via the bridge VPSes
([`0203`](0203-bridge-vpses-separate-from-relays.md)) and the
shim pattern ([`0209`](0209-shim-pattern.md)) at the application
level, *not* via management-server federation.

This decision deliberately scopes federation to the *data plane*
and *application layer*; the management plane is not federated.

## Alternatives considered

- **Per-site separate management servers, federated.** Adds
  significant integration surface and duplicates control-plane
  state without proportional benefit. Rejected.
- **Per-partner separate management servers, federated.**
  Reasonable for a future where partners explicitly want
  sovereign mesh ownership. Out of scope today; revisit when
  a partner asks.
- **Hybrid (one goat mgmt server, plus per-partner
  federated peers).** Today's shape — partners federate at the
  data plane via the bridge, not the mgmt plane. Accepted as
  the baseline.

## Consequences

**Positive.** Operational simplicity — one peer registry, one
ACL plane, one identity authority. New peer enrollment is a
single operation. Cross-site policy changes propagate through
Spock without federation negotiation.

**Negative.** Adding a sovereign-per-partner mesh requires
either (a) a separate goat deployment, or (b) ACL-
group scoping within the single mesh. Sufficient for current
needs; revisit if a partner explicitly demands a separate
mgmt server they federate with.

## Assessor view (SCA-R)

The single-mgmt-server decision narrows the authorization
boundary. NIST SP 800-37 system boundary is one logical system
across all sites. NIST SP 800-53 AC-2 (account management) and
IA-2 (identification and authentication) operate against a
single peer registry; CM-2 (baseline configuration) covers one
ACL plane. The active-active replication is an availability
property, not a separate-system property.

If a partner federation requires a separate authorization
boundary (likely for cross-classification federation), the
boundary node ([`0601`](0601-fips-pqc-boundary-node.md)) is
where the boundary lives — *not* in a federated management
server.

## Trigger conditions for revisit

- A partner explicitly requires sovereign management of their
  own peers under their own authorization. Stand up a separate
  goat deployment for them; federate at the bridge
  or boundary node.
- The single peer registry exceeds operational scale (likely
  ceiling ~10k peers). Re-evaluate with sharding within one
  logical mgmt server before considering federation.

---

*See `GLOSSARY.md` for terms and acronyms.*
