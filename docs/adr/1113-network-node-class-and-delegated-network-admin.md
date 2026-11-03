# ADR 1113 — `NETWORK` node-class + group tier, and delegated network-admin enrollment

**Status:** accepted
**Date:** 2026-08-06
**Amended by:** [ADR 1216](1216-net-engineer-group-and-edge-server-class.md) — `EDGE-SERVER` joins `NETWORK` in the delegable set (§3); `BREAKGLASS` and `NETWORK-JUMP` stay operator-gated
**Amends:** ADR 1043 §1.4 (node-class enum),
[ADR 1096](1096-delegated-machine-enrollment.md) §5 (the infrastructure gate)
**Related:** identity-grammar.md,
machine-enrollment.md,
mesh-acl.md, `ops/ansible/netbird_acl_taxonomy.yml`

## Context

Enrolling network devices such as a switch or a router surfaces that the identity grammar and the mesh ACL taxonomy have **no
vocabulary for network transport gear**, and that nothing about the flow is
delegable to the person whose job it actually is.

Three concrete gaps:

1. **No node-class fits.** identity-grammar.md
   §1.4 closes the enum at `SENSOR` / `EFFECTOR` / `PROCESSOR` (function),
   `PLATFORM` (composite), and `MGMT-HOST` / `ZENOH` / `SNITCH` / `SERVICES`
   (infrastructure). A router or switch is none of these. It runs no goat
   software; it carries the IP underlay the mesh rides on.

2. **No group tier fits, so `INFRA` gets used and over-grants.** `INFRA` is the
   only group in either the `ZAGROS-` or `ALPHA-` set that a platform admin
   reaches on any port — which is what makes a device manageable. But `INFRA`
   membership is bidirectional in effect: `INFRA → *` is any-port to every peer
   in the set. Putting a remote switch in `INFRA` therefore grants that switch
   unrestricted initiate to every host in the set. A managed switch needs NTP, syslog,
   DNS and TACACS *outbound*; nothing more.

3. **Nothing is delegable.** `goat machine enroll` gates every infrastructure
   node-class to the operator ([ADR 1096](1096-delegated-machine-enrollment.md)
   decision #1, `IsInfraNodeClass` in
   `ops/goat-zenoh-portal/internal/registry/machines.go`). So even once a class
   exists, the site's network admin cannot use it — the goatnet operator remains
   in the loop for every switch, which is precisely the coupling the request
   asked to remove.

## Decision

### 1. Add `NETWORK` to the node-class enum, as an infrastructure class

> **`NETWORK`** — network transport infrastructure: routers, switches, radios,
> and other bearer devices carrying the IP underlay the mesh rides on. Runs no
> goat software beyond a mesh client; enrolled so it is manageable over the mesh
> and so its health and custody are attributable.

It is an *infrastructure* class, not a function class: it shapes the substrate,
and a compromised bearer is a substrate-wide event. That classification is what
makes decision #3 necessary rather than optional.

We deliberately do **not** add `ROUTER` / `SWITCH` / `RADIO` as separate values.
The grammar classifies by what an asset *is to the goatnet*, not by product
category; the device model belongs in the registry's display attributes.

### 2. Add a `NETWORK` NetBird group tier with deliberately narrow rules

A new suffix in `netbird_acl_taxonomy.yml`, present in both the canonical site
template and the activity/site-set template, with an asymmetric rule shape:

- **Reachable** by `PLATFORM-ADMIN` on any port (management: SSH, NETCONF, HTTPS).
- **Reachable** by `INFRA` on any port (the substrate polls it).
- **Initiates** only toward `INFRA`, and only on the management-plane service
  ports it genuinely needs — `udp 123` (NTP), `udp 514` + `tcp 6514` (syslog),
  `udp/tcp 53` + `udp 5353` (DNS), `tcp 49` (TACACS+), `udp 161/162` (SNMP).
- **No lateral reach.** A `NETWORK` peer cannot initiate to `OPSYSTEM`,
  `OBSERVER`, `DEVELOPER`, `POD`, or another `NETWORK` peer. Bearer devices talk
  to the management plane, not to mission systems.
- **Never in `P2P`.** P2P graduation grants `p2p → all`; that is the opposite of
  what a bearer device should hold.

This is strictly less authority than `INFRA` while preserving the one property
that made `INFRA` attractive: platform admins can manage it on any port.

### 3. Extend the enrollment gate from operator-only to entitlement-scoped

[ADR 1096](1096-delegated-machine-enrollment.md) §5 gates infrastructure classes
behind "operator-only". This ADR narrows that to a per-class entitlement:

| Node-class | Who may enroll |
|---|---|
| `SENSOR` / `EFFECTOR` / `PROCESSOR` / `PLATFORM` | custodian, self-service against quota (unchanged) |
| `MGMT-HOST` / `ZENOH` / `SNITCH` / `SERVICES` | operator only (unchanged) |
| **`NETWORK`** | **operator, or a principal holding the network-admin entitlement, scoped to its own site** |

> Note (2026-09-23): the self-service row predates ADR 1116, which replaced `SENSOR` / `EFFECTOR` / `PROCESSOR` with the gear classes; see `identity-grammar.md` for the current mint table.

The network-admin entitlement is **not a new role**. It is the existing
`IAM-ADMIN` Ory tier, which a goatnet's group_vars can document as the
network/IAM admin, combined with the site-scoped platform-admin group
(`ALPHA-PLATFORM-ADMIN`) that bounds
*which* site's gear they may enroll. Adding a role would have spent a dimension
of the grammar on something two existing dimensions already express — role plus
site, exactly as identity-grammar.md §1.5
intends.

Fail-closed: absent the entitlement, a `NETWORK` enrollment is refused with the
same error shape as any other infrastructure class.

## Consequences

- A site network admin can enroll bearer gear for their own site without the
  goatnet operator. Cross-site enrollment still requires the operator.
- `NETWORK` peers hold materially less mesh authority than the `INFRA` peers
  they would otherwise have become.
- The node-class enum is closed and consumed by
  `ops/goat-zenoh-portal/internal/registry/machines.go`,
  `crates/goat-cli/src/cmd/machine.rs`, `ops/identity/grammar.yaml` and
  `ops/identity/generate.py`. All are updated in the same change; a value added
  to one and not the others is the failure mode the generated-SoT work
  ([ADR 1067](1067-identity-grammar-generated-sot.md)) exists to prevent.
- A `NETWORK` device that genuinely must publish to the fabric (a switch
  exporting telemetry) needs a fabric cert like any other principal. The class
  does not grant or deny fabric access; it is orthogonal, as node-class always is.

## Alternatives considered

- **Reuse `SERVICES`.** Wrong meaning — `SERVICES` is hosted goat services, and
  it carries the same `INFRA`-tier reach that motivated this ADR.
- **No new class; keep using `INFRA` and accept the over-grant.** Rejected: the
  over-grant is on the remote, physically-exposed device, which is the worst
  place to concentrate authority.
- **A dedicated `NETWORK-ADMIN` role in the role family.** Rejected per
  ADR 1043 §1.1 — roles are
  named by capability and the capability here is "administer this site's
  substrate", which `PLATFORM-ADMIN` already names. Site scoping is the group
  prefix's job.
