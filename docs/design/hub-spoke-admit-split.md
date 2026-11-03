# Hub-and-Spoke Admit/Revoke Split — Deferred Alternative

**Status:** **DEFERRED.** Documented as an alternative architecture to `ha-multi-site.md`'s multi-master active-active design. Not in the execution plan. Captured here so a future spike can start from the insight rather than rediscover it.

**When to revisit:**
- Need arises to run **more than 5 sites in a single cluster**. The architectural cap on approach B is 5 sites per cluster; scaling paths within that cap are (a) vertical per site — peers served by one mgmt-plane host can grow unbounded — and (b) per-region clusters with a bridging tier. Approach C is only relevant if we need to exceed 5 sites in one cluster, not if we add another cluster.
- Multi-master Spock surfaces operational pain meeting the bar in `ha-multi-site.md` §17 (>2 conflict incidents/quarter or >4h/upgrade).
- Threat profile re-evaluation concludes that defense-in-depth layers in [`mgmt-plane-threat-mitigations.md`](mgmt-plane-threat-mitigations.md) are insufficient against demonstrated coincident-infiltration attacks.
- Netbird upstream lands something that eases the admit/revoke API split (reduces implementation cost).

**Status as of 2026-04-20:** approach B (multi-master active-active) is the committed plan for 5 sites. This doc exists so option C is not lost.

---

## Table of contents

1. [Core insight](#1-core-insight)
2. [Write taxonomy framework](#2-write-taxonomy-framework)
3. [Signal topology options at scale](#3-signal-topology-options-at-scale)
4. [What breaks — legitimate local-admit use cases](#4-what-breaks--legitimate-local-admit-use-cases)
5. [New risks introduced](#5-new-risks-introduced)
6. [Hub HA](#6-hub-ha)
7. [Weaknesses retired vs. preserved vs. new](#7-weaknesses-retired-vs-preserved-vs-new)
8. [Implementation scope estimate](#8-implementation-scope-estimate)
9. [Open questions for the spike](#9-open-questions-for-the-spike)
10. [Comparison to approach B](#10-comparison-to-approach-b)
11. [References](#11-references)

---

## 1. Core insight

**Writes are not symmetric in security value.**

Admit-writes (enroll peer, grant policy, expand ACL) are what adversaries want during a partition — they want to inject identity with no one watching. Revoke-writes (disable peer, shrink ACL, ban identity) are what defenders want during a partition — they want to expel a compromised identity without waiting for reconnect.

The current multi-master active-active design treats both the same under LWW. That's a gift to the adversary and an unnecessary complication for the defender.

An asymmetric-writes model centralizes admits (stripped of island-mode admit capability, which aligns with the security posture) and keeps revokes local (which aligns with defender agility).

This insight also solves a scaling problem: admit-centralization is what Tailscale, ZeroTier, Nebula, Headscale, and iroh all do to scale to tens of thousands of nodes. The difference from their model is keeping revoke local — defense can't wait for the coordinator to be reachable.

---

## 2. Write taxonomy framework

Every Netbird management-plane write falls into one of three buckets. Before committing to the design, a spike's first deliverable should be walking every API endpoint through this classification.

| Bucket | Location | Semantics | Examples |
|---|---|---|---|
| **Admit** — permissive, grants access | Hub only | Fails during partition (or requires pre-issued capability envelope) | Enroll peer, create setup key, create group membership, add ACL rule, grant policy, rotate user key, create user |
| **Revoke** — restrictive, removes access | Any site | Monotonic merge: most restrictive wins. Replicates up. | Disable peer, delete peer, remove group membership, remove ACL rule, delete user, ban identity |
| **Read / telemetry** — observational | Every site | Replicated from hub + per-site monotonic telemetry append | Peer list, ACL state, policy catalog, last-seen (site-local, monotonic timestamp) |

### Why this is mechanically cleaner than LWW

- Monotonic merge has no clock dependency — NTP weakness [§1.3 of ha-multi-site-validation.md #5] largely evaporates for revokes.
- Revokes are idempotent and commutative; conflict resolution is a set-union operation, not a timestamp comparison.
- DDL coordination becomes one-directional (hub migrates first, spokes replay) — simpler than current bidirectional discipline.
- Spock conflict-storm risk (issue #349) retires for the admit-heavy tables.
- Hub = single primary Postgres → **Patroni** is the right tool, which we originally ruled out only because of multi-master.

### Questions the taxonomy walk must answer

- Which Netbird REST endpoints admit vs revoke vs both? Some endpoints are both (POST on a group can add OR remove members depending on body).
- Do we split the API surface or classify per-operation at request time?
- How does the admit/revoke split interact with Netbird's existing RBAC model?
- `last-seen` updates are high-volume and site-local — clearly revoke-bucket mechanically but not semantically. Does the bucket-shape need a fourth type or is "monotonic per-site append" a sub-type of revoke?

---

## 3. Signal topology options at scale

Under asymmetric writes, cross-site signal becomes cheaper architecturally. Three shapes to choose from:

### Option C-A: No cross-site P2P. Hub-mediated relay only.
- Signal is purely site-local; federation deleted entirely.
- Cross-site traffic uses Netbird VPS relay (single-hop through a VPS — infrastructure already deployed).
- Trivially scales. Simplest code. **Kills the federation attack surface outright.**
- Cost: bandwidth through VPS for cross-site traffic. Acceptable if cross-site volume is low (typical for management-plane-adjacent ops).

### Option C-B: Hub signal federation only — star, not mesh.
- Each site's signal has ONE federation peer: the hub signal.
- Hub has N peers.
- Cross-site signal path: site A → hub → site B. Two hops.
- Hub signal HA required.
- Scales to ~hundreds of sites with hub handling O(N) forwards per pair.

### Option C-C: Peer-location hint cache (incremental, not full rework).
- Each signal server caches recently-seen peer locations.
- Broadcast only on cold-miss; steady-state is O(1) forwards per cross-site Send.
- Fine to ~20–30 sites; past that cache pressure and staleness are real.
- Preserves existing architecture as evolution.

At ≤15 sites, C-C is a reasonable evolution. At 50+, C-A or C-B. Spike should resolve this based on site-count projections at decision time.

---

## 4. What breaks — legitimate local-admit use cases

These are real operational needs that hub-only admit blocks. The spike must produce an answer for each before committing.

1. **Replacement hardware during partition.** Asset breaks, swap is on-hand locally, setup-key activation needs hub.
   - *Mitigation:* pre-issued sealed setup keys. Hub issues short-lived, single-use, scope-limited keys in advance; distributed to each site during connectivity windows. Activation is local but the key was hub-authorized. Keys are treated like pre-positioned spares.

2. **Mission-scoped temporary ACL expansion.** "Give asset X access to resources Y for the next 6 hours."
   - *Mitigation:* capability envelopes. Hub pre-grants each site a bounded local-admit capability ("up to N identities of class X, expiring in M hours"). Site consumes from its envelope. Capability-based, not unconstrained. Envelopes expire if unused.

3. **Bulk telemetry writes.** Last-seen, peer-online states, stream statistics.
   - *Not an admit problem.* High-volume, legitimately site-local, monotonic. Fits the revoke bucket semantically even though not a revoke. Category of write, not category of permission.

4. **Local mesh-internal DNS updates.** Currently multi-master.
   - *Probably keep local-writable.* Mesh DNS is a site-local fact, not cross-site identity. Not an admit in the security sense. Spike should classify.

5. **Emergency policy rollback during active incident.** Site observes attack in progress, must shrink ACL fast.
   - *Fits revoke bucket.* ACL shrink is monotonic-restrictive. Local write, replicates up. Exactly the case asymmetric-writes is designed for.

---

## 5. New risks introduced

### Revoke abuse by a compromised site

An adversary who compromises site A can use local revokes to kick legitimate peers off the mesh. That replicates to all sites.

Three mitigations, ranked:

1. **Scope revokes to local peers only.** Site A can revoke peers primarily homed at A; not at B. Removes most of the attack surface. Cost: cross-site "this peer is misbehaving at my site" workflow needs a propose-to-hub path.
2. **Quorum on cross-site revokes.** Revokes to peers not homed at the issuing site require M-of-N site signatures. Local enforcement is immediate at the issuing site; other sites wait for quorum before honoring.
3. **Hub-confirmed revokes.** Local revoke enforced immediately at the issuing site but requires hub confirmation to propagate. During partition, revokes are site-local only. When hub returns, hub confirms or rejects.

Option 1 is probably sufficient. Option 3 is safest against a fully-compromised site. None are free.

### Hub becomes critical path

Under C, hub failure stops new admits everywhere. Mitigation is standard Postgres HA (see §6). Trade: exotic multi-master failure modes replaced with well-understood single-primary failure modes.

### Split-brain during hub failover

If hub primary fails and standby promotes, admits made at the old primary during the failover window may or may not have replicated. Standard Patroni fencing + etcd quorum handles this — mature problem.

---

## 6. Hub HA

With admits centralized, hub is the critical admit path. The good news: **standard Postgres HA is exactly the right tool.** All the reasons we ruled out Patroni for multi-master evaporate — hub is single-primary with streaming replicas.

Proposed shape:

- **Hub primary** in one well-connected region.
- **Warm streaming replica** in another region for DR.
- **Patroni + etcd** for automated failover.
- **Admit API** is read-write against primary; global load balancer in front (DNS-based or anycast).
- Worst-case RTO on hub failure: minutes.

This is a problem with decades of production use at scale. Patroni has clear failure modes, documented recovery, third-party tooling, large community.

---

## 7. Weaknesses retired vs. preserved vs. new

Mapped against the `ha-multi-site-validation.md` §2 weakness enumeration (indexed as "#N" below):

### Retired by adopting C
- **#5 Clock-skew LWW** — monotonic merge has no clock dependency for revokes. Admit time-correctness still matters but only at the hub.
- **#7 Spock conflict-storm noise** — no concurrent same-row updates on admits.
- **#13 pgEdge single upstream** — hub uses standard Postgres; spoke replication from hub can use native logical replication or Spock. Hedge becomes cheaper.
- **#17, #18 Federation scaling** — federation shrinks to star (C-B) or disappears (C-A).
- **#20 Spock DML-only** — spokes replay hub DDL; no bidirectional DDL coordination.

### Partially retired
- **#6 Per-site Dex identity fragmentation** — if admit is centralized, Dex returns to single-instance at hub. Per-site Dex no longer needed for island mode.
- **#29 Federation security** — C-A eliminates it entirely; C-B shrinks it to star topology with one high-value target (hub signal).
- **Coincident-infiltration attack** — adversary at compromised isolated site cannot admit without pre-issued capability; damage bounded to capability envelope.

### Preserved from B
- **#1 No prior art** — different architecture, same first-public-user problem (now for hub-spoke Spock or native logical replication + custom revoke-merge).
- **#22 Multi-week island has no adequate test** — unchanged.
- **#25 LUKS + OP-TEE / TPM key-custody hardware lock-in** — unchanged (key-custody mechanism repointed from Kcrypt/Tegra-SE to OP-TEE on Orin / TPM on x86; see [`orin-os-decision.md`](orin-os-decision.md)).

### New risks introduced by C
- **Revoke abuse by compromised site** — see §5.
- **Hub SPOF** — mitigated by Patroni + multi-region, but a single concentrated target for adversary reconnaissance.
- **Capability envelope management** — new primitive to design, implement, and operate.
- **Spike investment cost** — see §8.

---

## 8. Implementation scope estimate

Rough order-of-magnitude for a committed C build:

| Item | Effort | Notes |
|---|---|---|
| Write taxonomy walk of Netbird REST API | 1 week | Every endpoint classified; edge cases documented |
| Admit/revoke split in management server (upstream fork OR sidecar proxy with two-admit-paths) | 4–8 weeks | Sidecar is cheaper but less clean; upstream fork is cleaner but coupling risk |
| Capability envelope primitive (schema, issuance, consumption, expiry) | 2–3 weeks | New concept; affects setup-key lifecycle |
| Revoke-scope enforcement (local peers only OR quorum) | 2–4 weeks | Depends on mitigation option chosen |
| Hub HA deployment (Patroni + etcd + multi-region replica) | 2–3 weeks | Well-documented, but still needs Ansible work + drills |
| Spoke replication from hub (native logical or Spock, read-mostly) | 1–2 weeks | Simpler than multi-master Spock |
| Monotonic revoke-merge at hub | 1–2 weeks | Application-level; shouldn't touch Spock |
| Signal topology decision + implementation (C-A/B/C) | 1–3 weeks | C-A is cheapest |
| Migration from B if already deployed | 3–6 weeks | Schema conversion, peer re-enrollment, cutover plan |

**Rough total:** 4–6 engineer-months for a greenfield C build. Add ~1–2 more months if migrating from an operating B deployment.

**Spike scope (decision-quality, not production):** 2–3 weeks — the first three rows (taxonomy, admit/revoke split proof-of-concept against sidecar, capability envelope schema). Enough material to commit or reject.

---

## 9. Open questions for the spike

A future spike should answer these before anyone commits to building C:

1. **Write taxonomy boundaries.** Are there Netbird operations that don't classify cleanly? How many? What do we do with them?
2. **Sidecar vs fork decision.** Can an admit/revoke split be done cleanly as a sidecar proxy in front of Netbird management, or does it require upstream code changes? Sidecar approach likely covers 80% but may leak.
3. **Capability envelope lifecycle.** How are envelopes issued (hub → site at connectivity windows)? How are they revoked mid-flight if compromised? What happens at envelope expiry during partition?
4. **Revoke-abuse mitigation choice.** Local-scope only, quorum, or hub-confirm? Each has operator UX and trust implications.
5. **Hub failure tolerance window.** How long can the system operate with no admits (hub failover + heal)? Shapes the Patroni topology.
6. **Dex reunification.** Single Dex at hub or external OIDC? If single Dex, what's the authentication latency impact at distant spokes?
7. **Migration plan from B to C.** If B ships first, what's the cutover? Does B's data translate cleanly to C's admit-log + revoke-log model?
8. **Upstream Netbird cooperation.** Is there any prospect of getting admit/revoke split merged upstream, or are we indefinitely forked?

---

## 10. Comparison to approach B

| Dimension | B (committed) | C (deferred) |
|---|---|---|
| Engineering success (short-term) | ✓ — partly built | ✗ — 4–6 engineer-months greenfield |
| Engineering success (long-term) | ◐ — 30 structural weaknesses, most mitigated | ✓ — fewer weaknesses, mature tools for hub HA |
| Mission fit — remote manageability | ✓ | ✓ |
| Mission fit — island function | ✓ (admits + revokes) | ✓ (revokes always; admits via capability) |
| Mission fit — security posture | ◐ — relies on [`mgmt-plane-threat-mitigations.md`](mgmt-plane-threat-mitigations.md) layers | ✓ — architectural enforcement |
| Scale ceiling | ~15 sites before rework | 100+ sites as mainline |
| Upstream coupling | 2 sanctioned modifications + ongoing Netbird tracking | Larger ongoing modification OR sidecar with its own maintenance cost |
| Netbird-source-code modifications | Bounded (per `tech-strategy.md`) | Expanded — admit/revoke split doesn't fit the existing two-exception framing |

**Why B won for now:** the target is a handful of sites, not 100; most of B is already built; threat-specific defense layers in [`mgmt-plane-threat-mitigations.md`](mgmt-plane-threat-mitigations.md) address the coincident-infiltration and NTP weaknesses without requiring Netbird modifications at the scale C needs. B's scale weaknesses don't trigger at 5 sites.

**What would flip the choice to C:**
- Need to exceed 5 sites in a **single** cluster (per-region multi-cluster + bridging is approach B's preferred scaling path; C is only relevant if that pattern fails).
- Multi-master operational pain crosses the threshold in `ha-multi-site.md` §17.
- A demonstrated coincident-infiltration attack that bypasses the mgmt-plane-threat-mitigations layers.
- Upstream Netbird changes that reduce the admit/revoke split cost.

---

## 11. References

- `docs/design/ha-multi-site.md` — committed multi-master design (approach B)
- `docs/design/ha-multi-site-validation.md` — validation plan + tech-debt register
- [`docs/design/mgmt-plane-threat-mitigations.md`](mgmt-plane-threat-mitigations.md) — threat-specific defense layers that make B defensible at 5 sites (NTP correctness + coincident-infiltration defense + universal NTP hygiene requirement)
- `docs/design/federation-security.md` — federation attack surface + roadmap
- `docs/design/tech-strategy.md` — Decision Framework for rewrite-vs-fork; justifies why B's modifications fit and C's would need re-evaluation
