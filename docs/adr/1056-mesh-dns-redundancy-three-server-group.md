# ADR 1056 — Mesh DNS redundancy via a 3-server nameserver group

**Status:** Proposed
**Date:** 2026-06-20
**Driver:** a single mesh CoreDNS that goes unreachable when its relay path flaps takes down all service-subdomain resolution (`Nameservers 0/1`).
**Design:** [`mesh-dns-redundancy.md`](../design/mesh-dns-redundancy.md). **Companion:** `dns-service-discovery.md`.

## Context

Mesh-internal DNS is a single point of failure: one CoreDNS on the goatnet mgmt host, advertised as a **1-server** netbird nameserver group. Lose that host or its (relayed) path and every service name stops resolving goatnet-wide — peer-named DNS survives, but nameserver-group services (`isitup`, portal, …) die.

Two facts make redundancy cheap: the mesh zone is **deterministically generated** (so N replicas serve identical answers with no transfer/consensus), and NetBird nameserver groups **support 1–3 servers** with resolver-side failover (`validateNSList` rejects only 0 and >3 — see the 2026-07-29 correction under Alternatives).

## Decision

Adopt a **3-server netbird nameserver group**: three CoreDNS replicas, each on a distinct **mesh-resident** host, all serving the identical generated `ops/dns/<site>/mesh-hosts`. The `mesh-coredns` role + `goat-coredns.yml` gain a per-site `mesh_dns_servers` list (1–3 entries; default = today's single host, fully backward-compatible) that drives both the multi-host CoreDNS deploy and the group registration. Resolver failover across the 3 means DNS survives losing any one (or two) replica paths.

**Eligibility:** replicas must be mesh peers (a nameserver is queried by mesh IP over the overlay). **Relays are excluded** — they aren't mesh peers, and relay public-IP DNS would leak mesh service names onto the internet (violates the no-public-secure-plane invariant). Eligible: mgmt host(s), per-site standby nodes (ha-failover), mesh-joined service nodes.

## Alternatives considered

- **Status quo (1 server).** Rejected — the single-point failure is the whole motivation.
- **Relay-hosted DNS (public IP).** Rejected — relays aren't on the mesh, and public-internet DNS leaks the service namespace. The relays' existing local dnsmasq stays local-only.
- **Anycast / a DNS load-balancer in front.** Rejected — new component + new failure mode; netbird's built-in group failover already does the job with zero new infra.
- **Primary/secondary zone transfer (AXFR).** Rejected — pointless when the zone is byte-deterministically generated and CI-gated; shipping the same file *is* the replication.
- **2-server group.** Available and redundant (survives losing one). 3 was chosen to also survive losing *two*, but 2 is a valid redundant config; the F-254 drift guard therefore requires **≥ 2**, not exactly 3.

> **Correction (2026-07-29, while building the F-254 guard):** netbird's `validateNSList` (`management/server/nameserver.go`) rejects only `len == 0 || len > 3` — so it accepts **1, 2, or 3**. Its `InvalidArgument` message *says* "should be 1 or 3," and the original "exactly 1 or 3, never 2" claim above was read from that message rather than the code. 2-server groups are valid and redundant.

## Consequences

- DNS tolerates losing 1–2 of 3 replica paths; only 0 reachable = outage. A relay flap would have to take down all three simultaneously.
- Backward-compatible: a 1-entry `mesh_dns_servers` is byte-identical to today; existing goatnets are unchanged until they opt into 3.
- **Single-host gap:** a goatnet with one mesh host needs two more mesh-resident DNS hosts for a live 3-server group (lightweight mesh cloud hosts, or standby nodes later). The role/playbook support 3 immediately; each live deploy is gated on standing up the replica hosts.
- The generated-zone + `dns-registry-sync` pipeline is unchanged; redundancy is purely deploy-topology + group-registration.
