# Anycast for the relay endpoint tier — design

**Status:** Active (added 2026-05-04, operationalizes [ADR 0221](../adr/0221-anycast-relay-tier.md)).

This document is the operational design for Anycast-based relay endpoint reachability. Where the ADR commits to the *what* and *why*, this document defines the *how* — the two-path decision (own ASN vs Anycast-as-a-service), PoP topology, transport behavior under route flap, BGP + RPKI mechanics, observability hooks, migration sequencing, DDoS-absorption posture, and the spike that decides Path A vs Path B.

This is **a separate effort from multi-cloud** ([ADR 0220](../adr/0220-multi-cloud-multi-substrate-posture.md), [`docs/design/multi-cloud-multi-substrate.md`](multi-cloud-multi-substrate.md)). Anycast benefits from multi-substrate but is not gated on it. Multi-substrate benefits from Anycast but is not gated on it. The two compose multiplicatively when both ship.

---

## 1. What Anycast does for us

Three structural properties of Anycast that DNS round-robin cannot deliver:

### 1.1 Failover speed (~3-30s vs 1-5min)

| Layer | DNS round-robin | Anycast |
|---|---|---|
| Failure detection | health-check, ~30s | BGP withdrawal at failed PoP, ~3-30s |
| Routing convergence | DNS record update + cache expiry, 60-300s | BGP propagation, ~3-30s |
| Client recovery | re-resolve + reconnect, ~1s | per-packet route to next-nearest PoP |
| **Total user-visible** | **1-5 minutes worst case** | **3-30 seconds** |

WireGuard sessions (UDP) persist through both. The window matters for: new connection establishment, QUIC handshakes that need a fresh path, peers that lost their session and are reconnecting.

### 1.2 Per-packet path optimization

DNS round-robin makes one path-selection decision per DNS resolution (per peer per TTL). Anycast makes the decision per packet at every BGP hop on the path.

Concrete: a peer that resolves to a distant PoP's A record stays on it for the TTL window — even when another PoP is closer and even when the first is degraded. With Anycast, the peer's first packet lands at whichever PoP its ISP's BGP table says is nearest. Path changes (transit-provider degradation, partial failure) shift subsequent packets without any client-side awareness.

### 1.3 Structural DDoS absorption

Anycast distributes traffic per-packet to nearest PoP. An attacker pointing 100 Gbps at the Anycast prefix hits PoPs proportional to where the attack sources are (typically: many regions). Per-PoP load = total / PoP-count. Adding PoPs = subtracting per-PoP load, mechanically.

This is the property that lets CDNs absorb terabit-scale floods that would obliterate any single host.

---

## 2. Path A vs Path B — the spike (Block 59A)

The decision deferred in the ADR. Spike resolves it via measurement.

### 2.1 Path A — Own ASN + IP block + multi-provider BGP

**Procurement:**

- IP block: ARIN / RIPE / APNIC. /24 IPv4 + /48 IPv6. ~$100-1000/yr.
- ASN: same regional registries. ~$100-500/yr.
- LOA + RPKI signing infrastructure. Free.
- Multi-provider BGP peering at each PoP. Per-provider $0-200/mo for BGP-capable connectivity.

**Engineering:**

- ~2-4 weeks of focused engineering for first deployment. RPKI, IRR, route announcement supervision, peer relationships at each PoP.
- ~ongoing fractional FTE for LOA refreshes, RPKI key rotations, route announcement supervision, dispute resolution if a provider de-peers.

**Sovereignty:** Maximum. We control the IP block. Migration off any provider is a BGP withdrawal away.

**Trade-off:** Real engineering. The right answer if the project anchors heavily on procurement-resilience ([ADR 0000](../adr/0000-why-adrs-and-procurement-resilience.md)) and intends to operate at sustained scale.

### 2.2 Path B — Anycast-as-a-service

**Options:**

- **Cloudflare Spectrum.** TCP + UDP at L4, Anycast across Cloudflare's 300+ PoP footprint. Most-polished operational surface. Trades sovereignty for simplicity. ~$1k+/mo at our scale.
- **Vultr / Equinix Metal Anycast.** Smaller PoP footprint. More sovereignty-friendly than Cloudflare. ~$100-500/mo. Anycast support is provider-managed but on hardware we control.
- **Vendor-managed BGP at one or two providers.** Hetzner, OVH offer BGP for select customers; you bring your IP block, they announce it. Halfway between A and B.

**Engineering:** Days, not weeks. Provider-managed.

**Sovereignty:** Diminished. Provider can withdraw the announcement; provider's PoP footprint determines reach.

**Trade-off:** Speed-to-deploy + DDoS-absorption capacity (Cloudflare's footprint dwarfs anything we'd achieve on Path A in v1). The right answer if the immediate priority is volumetric DDoS resistance or if the engineering bandwidth for Path A isn't available.

### 2.3 Spike scope (Block 59A)

Within ~1 week:

1. Stand up a Path-B test: Cloudflare Spectrum or Vultr Anycast in front of a single relay PoP. Validate latency, jitter, failover behavior with a controlled withdrawal of that PoP.
2. Stand up a Path-A test: borrow / rent a /24 + ASN (RIPE allows temporary assignments for testing), announce from two PoPs, validate same. (Actual ARIN/RIPE/APNIC procurement deferred to Block 59B if Path A wins.)
3. Compare measurements per [ADR 1012](../adr/1012-load-testing-discipline.md) discipline. Capture as M-NNN.
4. Evaluate operational complexity, sovereignty fit, cost projection.
5. Pick A or B. Document. Proceed to Block 59B with the picked path.

**Default expectation:** Path A wins on sovereignty but loses on speed-to-deploy. If Block 59 is a Phase-3-go-live gating item, Path B is acceptable as v1; Path A is the pursued end-state.

---

## 3. PoP topology — target end-state

Minimum 3 PoPs across at least 2 substrate classes per [ADR 0220](../adr/0220-multi-cloud-multi-substrate-posture.md). This is the **structural lower bound** — Anycast with 1 PoP is just regular routing; Anycast with 2 PoPs handles failover but not load balancing; 3+ PoPs absorb DDoS proportionally.

| PoP | Substrate class | Geographic class | Sourcing rationale |
|---|---|---|---|
| PoP 1 | bare-metal (Equinix Metal / Hetzner / OVH) | region 1 primary | Existing relay deployment + ADR 0218 Phase 2 bare-metal candidate |
| PoP 2 | hyperscaler (AWS) or bare-metal (TBD) | region 2 primary | Existing relay deployment |
| PoP 3 | hyperscaler or bare-metal | region 3 primary | Existing relay deployment; substrate selection per [ADR 0220](../adr/0220-multi-cloud-multi-substrate-posture.md) sovereignty fit |
| (Phase 2) Partner-procured | partner-procured | Partner sovereignty | Block 58.5 first partner pilot — Anycast-aware from day one if feasible |
| (Phase 3) Additional regions | mix | (per growth) | As fleet expands |

The same prefix is announced from every PoP. Withdrawing one PoP shifts traffic to the rest within seconds.

---

## 4. Transport behavior under route flap

Three transports the relay supports; route-flap behavior differs per transport.

### 4.1 WireGuard (UDP) — primary data plane

- Connectionless. Route flap is a non-event. Packets land at next-nearest PoP. Session continues.
- Net: Anycast is pure win for WireGuard.

### 4.2 QUIC — primary relay transport (per F-016)

- Connection migration is built into QUIC (RFC 9000).
- If a packet lands at a different PoP after a route flap, the QUIC server at that PoP doesn't know the connection state. QUIC handles via path validation + connection ID continuity, but **only if the server-side state is shared across PoPs**.
- **Two operational options:**
  - **(a)** Keep QUIC server state per-PoP. Route flap = client treats as a new connection, reconnects. Cost: a connection-establishment round-trip on each flap. Bounded.
  - **(b)** Share QUIC server state across PoPs (e.g., shared keying material + connection-ID-to-PoP routing table). Operationally complex; possibly worth it post-v1.
- **Default for v1:** Option (a). Cheap and simple. Route flaps are rare in steady state; reconnect cost is bounded.

### 4.3 WebSocket (TCP) — fallback only

- TCP. Mid-stream route flap = packets to wrong PoP = TCP reset = client reconnect.
- Bounded cost: WebSocket is the fallback transport; QUIC is primary. Affected sessions reconnect (sub-second) and continue.

### 4.4 Control plane (`wg-cp0`) — out of scope

The wg-cp0 listener has different invariants (ADR 0316): clients want to terminate at a *specific* relay (mixed-fleet pattern with deterministic per-relay endpoints), not the nearest. Anycast is not appropriate here.

---

## 5. BGP + RPKI mechanics (Path A specifics)

If Path A wins the spike, the operational components:

### 5.1 IP block + ASN

- /24 IPv4 minimum (smaller prefixes don't propagate across the global BGP table).
- /48 IPv6 minimum (similar reason; some providers filter smaller).
- ASN registered to the project's umbrella organization.

### 5.2 RPKI (Resource Public Key Infrastructure)

- ROAs (Route Origin Authorizations) signed for the IP block declaring "ASN N is authorized to originate this prefix."
- Modern transit providers filter on RPKI invalid. Without ROAs, announcements can be silently dropped.
- ROA signing infrastructure: each regional registry (ARIN / RIPE / APNIC) provides a hosted signing UI; project owns the keys.

### 5.3 IRR (Internet Routing Registry)

- Route objects published in IRR for each provider relationship. Some providers still consult IRR in addition to RPKI.
- Less critical than RPKI but still worth maintaining.

### 5.4 LOA (Letter of Authorization)

- Per-provider letter authorizing them to announce the prefix on our behalf.
- Renewable, typically annual.

### 5.5 Route announcement supervision

- Per-PoP BGP session monitored continuously. Alertmanager alarms on:
  - Unexpected withdrawal at any PoP.
  - Unexpected announcement (route leak, route hijack).
  - Path-length anomalies (longer-than-expected AS paths).
- BGPmon / RouteViews or similar third-party route observation provides external corroboration.

### 5.6 Dispute resolution

- If a provider de-peers or filters incorrectly, the project's escalation path: provider's NOC → regional registry → public mailing lists. The provider replacement option (announce from a different provider) is the fail-safe.

---

## 6. Migration sequencing — coexistence with DNS round-robin

Block 59 sub-tasks:

1. **59A — Path A vs Path B spike.** ~1 week. Picks the path.
2. **59B — Procurement (Path A) or contract (Path B).** ~2-4 weeks Path A; ~3 days Path B.
3. **59C — Single-PoP Anycast deploy.** Announce from one PoP only. Validate BGP propagation (BGPmon / RouteViews / RIPE Atlas). Validate latency + jitter from a globally-distributed measurement set.
4. **59D — Two-PoP Anycast deploy.** Add a second PoP. Validate path selection per measurement-source location. Validate failover behavior with controlled withdrawal.
5. **59E — Three-PoP Anycast deploy.** Add a third PoP. Validate full fleet behavior.
6. **59F — DNS migration.** Add Anycast IP to `netbird.legacy.example` A records (alongside existing). Monitor peer distribution between Anycast and per-region IPs. Once Anycast is validated at fleet scale, remove per-region A records. Anycast IP becomes sole record.
7. **59G — Bundle + peer-config update.** Offline-enrollment bundles + peer Ansible roles update to Anycast IP.
8. **59H — Observability rollup.** Per-PoP wide-event attribution operational; failover-time SLO baseline captured.
9. **59I — Phase 2 partner-procured Anycast PoP.** Composes with Block 58.5 partner pilot.

**Coexistence period:** Steps 6 explicitly retains DNS round-robin during migration. Peers that resolve to the Anycast IP get Anycast routing; peers that resolve to per-region IPs get the legacy path. No flag-day cutover.

---

## 7. DDoS-absorption posture

### 7.1 Structural absorption (free with Anycast)

- DDoS traffic distributes across PoPs proportional to attacker-source distribution.
- Per-PoP load = total attack volume / PoP count.
- Each PoP absorbs whatever its substrate's transit capacity supports.

### 7.2 Per-PoP commercial DDoS protection (additive)

- Cloudflare Magic Transit or equivalent at each PoP adds upstream filtering.
- Bare-metal providers (Equinix Metal, Hetzner, OVH) typically offer per-PoP DDoS scrubbing as managed service.
- AWS provides Shield Advanced.
- **Phase 2 evaluation:** does adding commercial DDoS protection on top of structural Anycast meaningfully change resilience? Block 59J cost/benefit analysis.

### 7.3 Volumetric defense vs L7 defense

- Anycast handles **volumetric** (L3/L4) attacks structurally.
- L7 attacks (application-layer) are not addressed by Anycast.
- For our shape, volumetric is the dominant concern (relay endpoint is L3/L4 by design; no L7 surface on the relay itself).

---

## 8. Observability hooks

Each PoP exports the following:

| Metric | Type | Purpose |
|---|---|---|
| `relay_anycast_traffic_bytes{pop="<id>"}` | Counter | Per-PoP traffic volume; fleet distribution visibility |
| `relay_anycast_active_sessions{pop="<id>"}` | Gauge | Per-PoP session density |
| `relay_anycast_bgp_peer_state{provider="<p>",peer="<asn>"}` | Gauge | BGP session health per peering |
| `relay_anycast_bgp_announcement{prefix="<p>",pop="<id>"}` | Gauge | Whether the prefix is currently announced from this PoP |
| `relay_anycast_route_flap_count{pop="<id>"}` | Counter | BGP flap rate per PoP (excessive flapping = de-peer warning) |

Wide-events (Block 27) carry per-emission `served_at_pop` field so post-hoc analysis can correlate failure modes by PoP.

Alertmanager rules:

- Unexpected BGP withdrawal at any PoP.
- Unexpected announcement (route hijack, route leak).
- Route flap rate above threshold (de-peer warning).
- PoP traffic distribution anomaly (skew > 3× expected).

M-NNN measurement entry on first deployment + annual baseline captures: latency p50/p95/p99/p99.9, jitter, failover-time pre/post-Anycast, DDoS test (controlled flood at 10 Gbps from synthetic source against bench env).

---

## 9. Engineering-health signals

Adopted into [Block 32](https://github.com/usareur-af/goatdocs/blob/main/docs/design/engineering-health-assessment.md) carrying-cost metric:

- **BGP / RPKI maintenance cost.** Per-month FTE-hours on LOA refresh, RPKI rollover, peer relationship maintenance, dispute resolution. If > 0.05 FTE sustained, the diversity premium is worth re-evaluating.
- **Failover SLO.** Target: ≤ 30s p95 from BGP withdrawal to next-nearest PoP serving traffic. Monitor against synthetic withdrawals quarterly.
- **DDoS-absorption headroom.** Synthetic load capacity vs current peak traffic, per PoP. Operational margin = headroom / peak.
- **Per-PoP availability.** Independent measurement, not provider-claimed.

---

## 10. Hard prohibitions

- **No Anycast for the control plane (`wg-cp0`).** Different invariants.
- **No Anycast for site-plane endpoints.** Site plane is sovereign-edge addressing.
- **No flag-day cutover.** Coexistence with DNS round-robin during migration is mandatory.
- **No "Anycast for one validation env first" cargo-culting.** Validation envs don't benefit; production does. Block 59 sequences direct-to-production with PoP-by-PoP staging.

---

## 11. Open questions

- **Path A vs Path B.** Resolved by Block 59A spike.
- **QUIC state-sharing across PoPs.** Default v1 is per-PoP state + reconnect on flap. Phase 2: evaluate shared state once flap-rate measurements are in.
- **Phase 2 commercial DDoS protection on top of Anycast.** Cost/benefit analysis at Block 59J.
- **IPv6 Anycast.** Same engineering shape as IPv4; Block 59 includes IPv6 in target end-state but v1 may ship IPv4-only.
- **Anycast for partner-bridge endpoints.** Out of scope of this design; revisit if partner-bridges become a structural DDoS target.

---

## 12. References

- [ADR 0221](../adr/0221-anycast-relay-tier.md) — the ADR this document operationalizes
- [ADR 0220](../adr/0220-multi-cloud-multi-substrate-posture.md) + [`docs/design/multi-cloud-multi-substrate.md`](multi-cloud-multi-substrate.md) — the related (separate) multi-substrate effort
- [ADR 0218](../adr/0218-relay-substrate-bare-metal-strategy.md) — bare-metal substrate strategy
- [ADR 0709](../adr/0709-block-33-relay-rewrite-architecture.md) — Block 33 relay architecture
- F-016 — production fleet QUIC-by-default state
- `docs/project/implementation-plan.md` Block 59 — execution plan
- RFC 4786 — BCP 126 — "Operation of Anycast Services"
- RFC 7094 — "Architectural Considerations of IP Anycast"
- RFC 6480 — "An Infrastructure to Support Secure Internet Routing" (RPKI)
- RFC 9000 — "QUIC: A UDP-Based Multiplexed and Secure Transport"
