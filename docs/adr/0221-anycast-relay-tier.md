# ADR 0221 — Anycast for the relay endpoint tier

| Field | Value |
|---|---|
| Status | Proposed |
| Date | 2026-05-04 |
| Repo scope | goat core |
| Supersedes | — |
| Superseded by | — |
| Related | [`0202-three-vps-relay-topology.md`](0202-three-vps-relay-topology.md) (DNS round-robin — superseded in part by Anycast for the data path), [`0218-relay-substrate-bare-metal-strategy.md`](0218-relay-substrate-bare-metal-strategy.md), [`0220-multi-cloud-multi-substrate-posture.md`](0220-multi-cloud-multi-substrate-posture.md), `0316-wg-cp0-mixed-cloud-and-local-relay-fleet.md`, [`0709-block-33-relay-rewrite-architecture.md`](0709-block-33-relay-rewrite-architecture.md). Companion (separate, not gating): [`0220-multi-cloud-multi-substrate-posture.md`](0220-multi-cloud-multi-substrate-posture.md). |

## TL;DR (for a layperson)

Today our relays are reachable as three different IP addresses behind one public DNS name. Clients pick one IP and stick with it for the DNS-cache lifetime, even when a closer or healthier relay exists. Anycast is the technique used by Google DNS (`8.8.8.8`), Cloudflare's edge, and the DNS root servers: **one IP, advertised from many physical locations**, with the internet's routing fabric automatically delivering each packet to the nearest healthy location. For our relay tier this gives faster failover (seconds, not DNS-TTL minutes), better single-flow latency (per-packet routing to nearest PoP), and structural DDoS absorption (an attacker pointing 100 Gbps at the IP gets diluted across however many PoPs are announcing the prefix). This is **a separate effort from multi-cloud** — Anycast benefits from multi-substrate diversity but stands on its own merits.

## Context

Three independent forcing functions converge on Anycast as a high-leverage move:

**1. Failover speed.** The current relay endpoint is a public DNS name with three A records (one per relay region). Clients pick one IP at resolution time and stay on it for the DNS TTL window (typically 60-300s). When a relay fails:

- Health-check detection: ~30s.
- DNS record update: ~5-10s.
- Client cache expiry: up to TTL (60-300s).
- Client re-resolves and picks new IP: ~1s.

Total user-visible failover: ~1-5 minutes worst case. WireGuard sessions persist (UDP, connectionless), but new connections + sessions that need rekey block during this window.

Anycast withdrawal happens at BGP speed: ~3-30 seconds between BGP withdrawal at the failed PoP and route convergence everywhere upstream. Per-packet routing means subsequent packets go to the next-nearest PoP. No DNS layer involved. **One order of magnitude faster failover.**

**2. Single-flow latency.** A peer might pick the region-2 A record at DNS resolution time and stay on region 2 for the TTL window — even though region 1 is closer (~50ms vs ~100ms RTT) and even when region 2 is degraded. DNS-based selection has no per-packet awareness.

Anycast routes per-packet via BGP path cost. The peer's first packet lands at whichever PoP its ISP's BGP table says is nearest. If routes change (e.g., a transit provider's path to region 2 degrades), subsequent packets shift accordingly without any client logic. **Per-packet routing beats per-DNS-resolution routing structurally.**

**3. DDoS absorption.** The current relay endpoint is the most attractive DDoS target in the architecture: named in DNS, public, identity-bearing. A few single-host IPs are easy to saturate; even with a provider's DDoS protection, the per-IP capacity is bounded by the per-IP transit capacity at the upstream peering points.

Anycast is the structural answer. An attacker pointing 100 Gbps at the Anycast prefix doesn't hit one PoP — the traffic is delivered per-packet to whichever PoPs the attacker's traffic sources are nearest to. This is why CDNs absorb terabit-scale floods that would obliterate any single host. For our scale (a regional peer fleet), Anycast across a handful of PoPs absorbs DDoS that would otherwise saturate any single relay.

**4. The transports cooperate.** The Block 33 relay supports two transports (per F-016, the production fleet is now QUIC-by-default):

- **WireGuard data plane:** UDP, connectionless from user-space perspective. Route flaps are non-events.
- **QUIC relay transport:** Connection migration is built into QUIC. Route flaps trigger migration without session loss.
- **WebSocket fallback transport:** TCP. Route flaps mid-stream cause TCP reset → reconnect. This is the one transport where Anycast costs more than it gains, but the cost is bounded (peers reconnect; QUIC peers are the primary path).

Three out of four transports benefit; the fourth degrades gracefully.

**5. Multi-cloud / multi-substrate posture amplifies Anycast.** [ADR 0220](0220-multi-cloud-multi-substrate-posture.md) commits to multiple structurally independent providers. Anycast across multi-substrate PoPs is the strongest possible relay tier: a CSP-level outage withdraws that CSP's BGP announcements; the rest absorb the load. This is **strictly stronger** than Anycast within a single CSP. But Anycast within a single CSP is **still strictly stronger** than DNS round-robin. The two efforts are independent; either one helps without the other.

## Decision

Adopt **Anycast as the relay endpoint tier reachability mechanism.** The relay endpoint becomes a single IP (or single prefix) advertised from every relay PoP in the fleet. Specifically:

### 1. Two paths to Anycast (decision deferred to spike)

**Path A — Own ASN + IP block + multi-provider BGP peering.** Maximum sovereignty. Engineering lift: ~$1-5k/yr for IP block + ASN registration through ARIN/RIPE/APNIC; multi-provider BGP peering at each PoP; route announcement supervision; LOA / RPKI / IRR maintenance. ~2-4 weeks of focused engineering for first deployment, ~ongoing fractional-FTE thereafter.

**Path B — Anycast-as-a-service.** Use a provider that does Anycast for us. Options:

- **Cloudflare Spectrum** (TCP + UDP at L4, Anycast across Cloudflare's full PoP footprint). Trades sovereignty for operational simplicity. Adds a CDN-class dependency on the data path.
- **Vultr / Equinix Metal Anycast offerings.** Smaller PoP footprint than Cloudflare; less polished but more sovereignty-friendly.
- **Vendor-managed BGP** at one or two providers (Hetzner, OVH offer this for select customers).

**Decision:** spike both within Block 59A; pick based on measured behavior + sovereignty fit.

The default expectation is **Path A** (own ASN + IP block) because the project's procurement-resilience anchor ([ADR 0000](0000-why-adrs-and-procurement-resilience.md)) favors sovereign infrastructure controls. Path B remains in scope as a v1 / fallback / capability-driven option (e.g., for absorbing volumetric DDoS that exceeds what a small ASN can defend).

### 2. PoP topology (target end-state)

Minimum 3 PoPs across at least 2 substrate classes per [ADR 0220](0220-multi-cloud-multi-substrate-posture.md). Initial target (subject to substrate selection):

| PoP | Substrate | Geographic class |
|---|---|---|
| region 1 | bare-metal (Equinix Metal / Hetzner / OVH) | region 1 primary |
| region 2 | hyperscaler or bare-metal (TBD) | region 2 primary |
| region 3 | hyperscaler or bare-metal | region 3 primary |
| (Phase 2) Partner-procured | partner-procured infrastructure | Partner sovereignty |

The BGP announcement is the same prefix from every PoP. Withdrawal of a single PoP shifts traffic to the rest within seconds.

### 3. Transports

- **WireGuard (UDP):** unchanged — connectionless, Anycast is a non-event.
- **QUIC:** primary relay transport — connection migration handles route flaps natively.
- **WebSocket (TCP):** fallback only — route flaps cause reconnect; bounded cost since QUIC is primary.
- **Control-plane (`wg-cp0`, ADR 0316):** out of scope of this ADR. The mixed-fleet pattern there has different invariants (local-relay peers want to terminate at *specific* relays, not nearest); Anycast does not apply.

### 4. Coexistence with DNS round-robin during migration

DNS-based round-robin stays in place for the migration period. The Anycast prefix is added as an additional A record (or replaces one of the existing A records on a controlled rollout). Peers that resolve to the Anycast IP get Anycast routing; peers that resolve to a non-Anycast IP get the legacy path. Once Anycast is validated at fleet scale, the legacy A records retire.

This avoids a flag-day cutover. Block 59 sequences the migration explicitly.

### 5. Observability hooks

- Per-PoP traffic counter (Prometheus): `relay_anycast_traffic_bytes{pop="<id>"}` so we can see fleet distribution.
- BGP withdrawal alarm: Alertmanager fires on unexpected BGP state at any PoP.
- Per-peer "served at PoP" attribution in wide events (Block 27) so post-hoc analysis can correlate failure modes by PoP.
- M-NNN measurement entry on first deployment: latency + jitter + failover-time pre/post-Anycast.

### 6. DDoS-absorption posture

Anycast is the **structural** DDoS answer. Per-PoP commercial DDoS protection (Cloudflare Magic Transit, AWS Shield Advanced equivalent at non-AWS providers) is a complement, not a replacement. Phase 2 evaluates whether Magic-Transit-class commercial DDoS protection adds enough on top of structural Anycast to justify the cost.

## Out of scope (explicit non-decisions)

- **Control-plane Anycast.** The `wg-cp0` listener has different invariants (ADR 0316); Anycast is not applied there.
- **Site-plane Anycast.** Site plane is sovereign-edge; outside the addressing scope of this ADR.
- **Cloudflare Spectrum lock-in.** This ADR keeps Cloudflare Spectrum as one of two viable Path-B options without committing to it. The spike (Block 59A) decides.
- **Anycast for `release/<partner>/...` partner-bridge endpoints.** Maybe later. Scope of this ADR is the relay tier reachability for our own peer fleet.
- **IPv6 Anycast.** Same engineering shape as IPv4 once IPv4 Anycast is operational; not separately blocked. Block 59 sub-task picks up IPv6 once v1 is stable.

## Consequences

**Positive:**

- Order-of-magnitude failover-time improvement (~3-30s vs 1-5min).
- Per-packet path optimization — single-flow latency improves.
- Structural DDoS absorption proportional to PoP count.
- Single endpoint identity simplifies bundles + peer config (one IP / prefix instead of three A records).
- Composes additively with [ADR 0220](0220-multi-cloud-multi-substrate-posture.md) multi-substrate posture: multi-substrate Anycast > single-substrate Anycast > DNS round-robin.

**Negative / accepted trade-offs:**

- **Operational complexity.** BGP, RPKI, route announcement supervision is real engineering. Path B mitigates but trades sovereignty.
- **WebSocket fallback degrades on route flap.** Bounded cost — QUIC is primary; WebSocket peers reconnect rather than fail.
- **First-time setup capital cost.** ~$1-5k/yr for IP block + ASN; ~2-4 weeks of focused engineering. Both bounded; both pay for themselves at fleet scale.
- **Module rot risk.** ASN + IP block require ongoing administrative attention (LOAs, RPKI rollovers, IRR updates). Mitigated by including the maintenance in Block 32 carrying-cost metric so the cost stays visible.
- **Diagnostic difficulty.** "Which PoP served this request?" needs explicit instrumentation (§5 above). Wide events make this tractable but the question must be asked.

**Operational:**

- First Anycast prefix is treated as production-class from day one (Block 59 sequencing). No "Anycast for one validation env first" — the validation env itself doesn't benefit; production does.
- Pre-cutover dry run: announce Anycast prefix from one PoP only, observe BGP propagation, validate path measurements, then sequentially add PoPs.
- Peer config update: bundles + peer Ansible roles update to the Anycast IP / prefix as a simple value swap once ready.

## SCA-R framing (Senior Cloud Architect — Reviewer)

Anycast for endpoint reachability is well-trodden territory: every major CDN, every public DNS resolver, all the DNS root servers, much of Cloudflare's product line. The novelty for *this* system is choosing it over DNS round-robin given our small scale — the conventional wisdom is "Anycast is for hyperscale; DNS round-robin is for everyone else." That conventional wisdom is wrong for our shape, because:

1. We have unusually high failover-speed sensitivity — a peer fleet at regional scale with active sessions cannot tolerate 1-5 minute reconvergence windows during a region failure.
2. We have unusually high DDoS exposure — the relay endpoint is the most attractive target in the architecture, named in DNS, identity-bearing.
3. Our transport mix (WireGuard UDP + QUIC) is unusually Anycast-friendly. WebSocket TCP is the only transport that suffers, and it's already a fallback.

The Path A vs Path B decision is the right one to spike rather than pre-decide. Path A (own ASN) preserves the project's sovereignty stance but requires engineering investment; Path B (managed Anycast) buys speed but adds dependency. The spike (Block 59A) gives both a fair shot.

The composition with [ADR 0220](0220-multi-cloud-multi-substrate-posture.md) is worth noting: this ADR stands on its own (Anycast within single-CSP is still strictly better than DNS round-robin within single-CSP), but when both ADRs are operational the result is structurally stronger than either alone. Treating them as separate efforts (per the user direction documented at design time) is the right call — neither blocks the other.

## References

- [`docs/design/anycast-relay-tier.md`](../design/anycast-relay-tier.md) — design doc operationalizing this ADR
- `docs/project/implementation-plan.md` Block 59 — execution plan
- [ADR 0202](0202-three-vps-relay-topology.md) — DNS round-robin (superseded in part for the data path; enrollment / mgmt-API path unchanged for now)
- [ADR 0220](0220-multi-cloud-multi-substrate-posture.md) — multi-cloud / multi-substrate (related, not gating)
- [ADR 0709](0709-block-33-relay-rewrite-architecture.md) — Block 33 relay architecture (transport mix)
- RFC 4786 — BCP 126 — "Operation of Anycast Services"
- RFC 7094 — "Architectural Considerations of IP Anycast"
- F-016 — production fleet QUIC-by-default state
- See GLOSSARY.md for acronym expansions used in this ADR (BGP, RPKI, ASN, LOA, IRR, ADR, SCA-R, etc.).
