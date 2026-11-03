# ADR 0205 — Intra-site VRRP failover (Router-A / Router-B pair)

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Supersedes | — |
| Superseded by | — |
| Related | [`0204-multi-site-active-active.md`](0204-multi-site-active-active.md), [`0805-policy-engine-active-passive.md`](0805-policy-engine-active-passive.md) |

## TL;DR (for a layperson)

At each site, we run two identical management hosts side-by-side
called Router-A and Router-B. They share a virtual IP address
that always points to whichever one is currently active. If the
active one fails, the other takes over within a couple of
seconds. This sub-second failover is what keeps the local site
operational during a single-host outage.

## Diagram



Source: [`diagrams/0205-intra-site-vrrp-failover.mmd`](diagrams/0205-intra-site-vrrp-failover.mmd).

## Context

Within a site, sub-second failover for the management plane is a
practical requirement. Typical hardware failures (kernel panic,
disk failure, network interface drop) should not produce a
several-minute outage of mgmt-plane services. VRRP (the standard
keepalived-based active-passive failover) is the canonical
pattern; we adopt it intra-site.

This is *not* in tension with cross-site active-active. Each
site has its own A/B pair; the cross-site relationship between
sites is multi-master via Spock
([`0204`](0204-multi-site-active-active.md)).

## Decision

Every production site runs a Router-A + Router-B pair on
identical hardware. They share a virtual IP via keepalived
(VRRP). Services that need a stable single point of contact
(the policy engine, certain PostgreSQL connection paths) bind
to the VIP; only one router holds the VIP at a time.

| Property | Value |
|---|---|
| Failover time | < 3 seconds for VIP move; ~60s for full recovery (pg_promote + service rebind) |
| Failure detection | keepalived health check + service-specific check scripts |
| Intra-pair replication | Native Postgres streaming replication, synchronous `remote_write` from Router-A primary → Router-B warm-standby replica (NOT Spock) |
| Spock peering active on | Router-A only (the current PRIMARY); Router-B's Spock node activates on promotion |
| Each node has its own | pgBackRest stanza on local NVMe (`<site>-a-netbird` and `<site>-b-netbird`; see ADR 0802) |
| VIP-bound services | Policy engine ([`0805`](0805-policy-engine-active-passive.md)), externally-facing reverse proxy on some sites |
| Failback policy | Manual; no automatic failback to original active to avoid flap |

This is *not* in tension with cross-site active-active. Each site
runs *one* active Spock node at a time (the current primary on
whichever Router holds the VIP); cross-site Spock multi-master
peers across the active primaries — see [`0204`](0204-multi-site-active-active.md)
and [`0801`](0801-postgresql-spock-multi-master.md).

Hosts are named consistently: `<site>-router-a`, `<site>-router-b`.
Each runs the full mgmt-host compose stack
(`../design/ha-multi-site.md`).
Per-site paired pgBackRest stanzas
([`0802`](0802-pgbackrest-paired-stanzas.md)) — one per
router — give site-local PITR coverage that survives single-host
loss within the site.

## Alternatives considered

- **Single management host per site.** Single point of
  failure. Rejected.
- **Three-host quorum per site.** Adds a third host without
  meaningful additional reliability for the failure modes we
  see; Spock multi-master and cross-site replication already
  carry the durability load. Rejected.
- **VRRP across sites instead of intra-site.** Cross-site
  VRRP requires reliable cross-site connectivity, which
  contradicts the partition-tolerance requirement. Rejected.
- **Container orchestrator (Swarm/K8s) handling failover.**
  Adds a control plane; consensus dependencies break under
  partition. Rejected per
  [`0207`](0207-docker-compose-orchestration.md).

## Consequences

**Positive.** Single-host outages are absorbed. The policy
engine and other VIP-bound services have a stable address.
Each router can be patched / restarted independently.

**Negative.** Two hosts per site doubles the per-site
hardware cost. We accept; the failover semantics are
load-bearing for site availability.

VRRP failover is a known operational gotcha — split-brain on
asymmetric WAN failures, ARP cache staleness on the LAN
switch. We mitigate via service-specific health checks and
a LAN switch configuration that handles
gratuitous-ARP correctly.

## Assessor view (SCA-R)

Intra-site failover serves CP-2 (contingency plan), CP-9
(backup), and SC-5 (DoS protection — single-host failures
are absorbed). The two-host pattern also makes CM-3 (change
control — patch one, validate, patch the other) and CM-2
(baseline configuration — A and B are identical) easier to
demonstrate.

The Router-A / Router-B naming convention is a reviewable
artifact; an assessor can verify pair-completeness across
sites at a glance.

## Trigger conditions for revisit

- Sub-second failover is no longer required (e.g. all VIP-
  bound services move to round-robin client-side discovery).
  Could simplify; revisit then.
- A specific service requires three-host quorum within the
  site (unlikely; Spock multi-master and federation already
  cover that role).

---

*See `GLOSSARY.md` for terms and acronyms.*
