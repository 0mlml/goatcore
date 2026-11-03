# ADR 0321 — wg-cp0 is a perimeter, not an ACL layer

**Status:** Accepted (2026-05-13; substrate landed under track `wg-cp0-bundle-symmetry`).
**Date:** 2026-05-13
**Series:** silent-control-plane (companion to ADR 0313 / 0314 / 0315 / 0316 / 0319 / 0320)
**Supersedes-in-part:** none. **Amends:** ADR 0313 §"Authorization model."
**Repo scope (post-monorepo-split):** core.

**Acceptance trace:** Bundle-create + wg-cp0-bundle-apply code change landed
in the same change-set as this ADR flip; Tier-1 unit-scope invariant test
(`TestRun_BundleSymmetryInvariant` in `ops/enrollment/cmd/bundle-create/`)
gates every wg-cp0 substrate touch on every PR. Fleet re-issue ceremony +
combinatorial mesh-reach bench-env harness queued operator-fired per the
implementation tracking in `docs/project/implementation-plan.md`
Block 22R.0.

---

> **For the technical contributor:** drop the per-peer `/24`-placement
> algorithm in bundle-create. Every relay peer block in every bundle
> carries `AllowedIPs = 198.18.0.0/24`. Membership in wg-cp0 (which
> requires a valid offline-CA-signed bundle) equals full mesh reach.
> Fine-grained per-peer-to-peer access control lives one layer up at
> the netbird inner-mesh ACL + application layers, not at WireGuard's
> AllowedIPs.
>
> **For the layperson sponsor:** wg-cp0 is the iron-clad outer door of
> our secure facility. It checks one thing: do you have the right key?
> If yes, you're inside. The badge readers on individual rooms — who
> can read which file, who can talk to which sensor, who can issue
> which command — are inside the building, not on the outer door.
> We tried briefly to put badge readers on the door itself, which
> hardened nothing (the door's already iron-clad) and broke the
> doorknob (peers couldn't reach each other reliably). This ADR
> removes the inner-door badge readers from the outer door and
> confirms they belong inside.
>
> **For the SCA-R:** the WireGuard outer tunnel (UDP 51821, silent
> listener, no public DNS, drops unauthenticated handshakes per ADR
> 0313) remains the trust boundary. Authentication of a peer's
> membership in the substrate is by offline-CA-signed bundle. Once
> inside the tunnel, peer-to-peer traffic is permitted at the network
> layer; per-resource access control is enforced by separately-audited
> mechanisms above it (netbird-mesh ACL groups, Ory OIDC, mutual
> TLS with offline-CA-issued leaves, Spock per-database users,
> Zenoh key-expression ACLs, egress proxy). No reduction in defense
> in depth; the controls that were doing work all remain at the
> appropriate altitude.

---

## Context

ADR 0313 established wg-cp0 as the "silent control plane" — a
WireGuard outer wrap around all mgmt-plane traffic, with three
properties: (1) silent under unauthenticated handshakes by
WireGuard's own protocol behavior, (2) no public DNS, (3) peer
membership rooted in offline-CA-signed bundles per ADR 0407 +
Block 22K.

ADR 0313 did not specify whether wg-cp0 should additionally
enforce per-peer-to-peer access control at the WireGuard layer.
The `bundle-create` CLI (ops/enrollment/cmd/bundle-create/) was
authored 2026-05-05 and chose to place the fleet-wide
`198.18.0.0/24` catchall on a single relay peer block per bundle,
with other relay peer blocks carrying only the relay's own
`/32`. This was an implicit choice — not motivated by any
documented design intent — that produced a per-peer fine-grained
ACL at the WireGuard layer.

The choice was invisible while the fleet had a few peers
bootstrapped together, all with matching `/24` placement. It
became load-bearing wrong as new peers (further site hosts,
operator laptops) were added
with different per-bundle `/24` placement, producing the
asymmetric-`/24` failure mode described below.

## Decision

**wg-cp0's WireGuard layer enforces membership only.** Concretely:

1. `bundle-create` shall produce bundles where **every relay peer
   block carries `AllowedIPs = 198.18.0.0/24`** (the fleet wg-cp0
   subnet, per ADR 0319). The per-bundle relay-choice algorithm
   for `/24` placement is removed.

2. Existing fine-grained access controls remain at their
   architecturally-correct altitudes:
   - Mesh-level peer-to-peer policy → netbird-mesh ACL groups
     (docs/design/mesh-acl.md, Block 18A).
   - Service-level human + service-account policy → Ory OIDC +
     Hydra (Block 21).
   - Service-to-service authentication → mutual TLS with leaves
     issued by the offline-CA internal X.509 CA (ADR 0407).
   - Per-database authority → per-database PG users + Spock
     replication-set membership (Blocks 17D / 21C).
   - Per-topic data-fabric policy → Zenoh key-expression ACLs
     (Block 18A §"mission slicing at app layer").
   - Outbound network policy → egress proxy 5-tier
     ([docs/design/egress-proxy.md](../design/egress-proxy.md), Block
     17) + policy engine (Block 17A.5).

3. The WireGuard-layer source-IP allowlist semantic of
   `AllowedIPs` is treated as a side-effect we accept (not a
   security control we rely on) when set to `198.18.0.0/24`. The
   security claim of wg-cp0 reduces to: "a packet inside wg-cp0
   came from some peer with a valid offline-CA-signed bundle"
   — which is exactly what ADR 0313 originally claimed.

4. **Rotation as a fleet-wide routine ceremony is dropped.**
   WireGuard pubkeys are forever in the protocol; "rotation" in
   our current ops is rotating the bundle envelope (CA signature +
   expiry), not the pubkey. Bundle re-issuance happens on real
   events: compromise, lost device, peer revocation. Bundle TTL
   stays at 90 days enforced validator-side, not by mass re-issue.

## Cost of NOT doing this (first-hand evidence)

The asymmetric-`/24` choreography produces two failure modes:

- **Drift on relay restart.** A peer allowlist push
  triggers the relay-side `restart wg-cp0` handler, clearing
  per-peer endpoint state. Peers whose on-disk wg-cp0.conf has
  drifted across rotation generations cannot re-handshake.
- **Asymmetric reach on join.** A peer handshakes every relay but
  cannot be reached from other peers over wg-cp0, because the
  bundle issued to each peer placed the `/24` catchall on a
  *different* relay. Handshakes to all relays succeed; peer-to-peer
  reach fails because the catchall placement on one bundle does not
  match the placement on the target's bundle. Per-peer workarounds
  (hand-editing `/etc/wireguard/wg-cp0.conf` to add `/24` on
  multiple relay peer blocks) confirm the diagnosis but do not
  scale.

**Generalizing the cost.** Every future peer addition or rotation
risks producing the same drift class. Every new operator laptop
risks producing the same mesh-reach failure. Every relay
restart risks F-114-class disruption of unrelated peers. The
cost is monotonically rising with fleet size, has no upper
bound, and is recurring rather than one-time.

## Prior art (why this is a known pattern, not a novel debate)

WireGuard's `AllowedIPs` field has dual semantics — it is **both** the
outbound destination-routing key (which peer to encrypt to for a given
inner destination) **and** the inbound source-IP allowlist (which
source IPs are acceptable from packets decrypted via this peer block).
The two semantics are inseparable in the WireGuard protocol; the
field cannot be configured to serve only one role.

When WireGuard is used in its **dominant pattern** (point-to-point
tunnels — VPN client to gateway, or two hosts paired directly), the
dual semantics collapse harmlessly: each side has exactly one peer,
so the routing-vs-ACL distinction does not surface. This is the
pattern WireGuard's own documentation and dominant deployment
guides assume.

When WireGuard is used in a **hub-and-spoke forwarding pattern** (a
hub relay decrypts traffic from spoke X, looks at the inner
destination, re-encrypts to spoke Y), the dual semantics diverge:

- Outbound on the spoke: AllowedIPs picks which hub to encrypt to.
- Inbound on the receiving spoke: AllowedIPs filters source IPs.

Receiving spoke Y, decrypting a packet that came via the hub it
shares with spoke X, must have *spoke X's IP* in the AllowedIPs of
the hub peer block it's decrypting with — not just the hub's own
IP. If the hub-peer-block's AllowedIPs is hub-only (`hub-ip/32`),
packets from any other spoke source IP are silently dropped at the
inbound check.

This is **well-documented in the community** as the dominant cause
of mesh-forwarding failures in operator-rolled WireGuard meshes:

- **wg-meshconf** ([github.com/k4yt3x/wg-meshconf](https://github.com/k4yt3x/wg-meshconf))
  — the project's central feature is that it generates *symmetric*
  configs across all peers because the author hit this exact bug
  rolling their own mesh. Project README explicitly identifies the
  asymmetric-AllowedIPs failure mode as the motivation.
- **Tailscale's design doc** ([tailscale.com/blog/how-tailscale-works](https://tailscale.com/blog/how-tailscale-works))
  — explicitly states that AllowedIPs alone does not give mesh
  reach, and that Tailscale's coordination server distributes
  ephemeral per-pair keys with NAT-punched direct connections
  precisely so that the AllowedIPs problem is never seen at
  operator-facing altitude.
- **NetBird itself** — the management server we operate is a
  fork of NetBird's mgmt-stack. NetBird's *entire reason for
  existing* is to solve mesh-routing-over-WireGuard at a higher
  altitude than raw AllowedIPs. The irony of our position: we run
  NetBird for the inner mesh (which correctly solves
  hub-and-spoke at the management layer) and then *re-invented
  the broken half* at the wg-cp0 layer beneath it.
- **ZeroTier**, **Headscale**, **Nebula**, **Innernet** — every
  serious mesh-over-WireGuard or mesh-over-Noise project either
  (a) generates symmetric configs by construction, or (b) adds
  a coordination layer that distributes config dynamically. None
  of them rely on operator-rolled per-peer asymmetric AllowedIPs
  as a security control.

**The pattern in plain English:** WireGuard is a point-to-point
crypto primitive. Building a mesh on top of it requires symmetric
configuration across all participants. Every project that has done
this and survived has either (a) generated configs symmetrically
or (b) layered a coordination service above to manage the
symmetry. The asymmetric pattern is a known-broken altitude error.

## Consequences

### Positive

- **The mesh actually works** for all current peers and any future
  additions. A peer added next month with a fresh bundle reaches
  every other peer immediately.
- **Substrate complexity drops.** The per-bundle `/24`-placement
  algorithm is removed. The 4-tool ceremony (bundle-create,
  wg-cp0-bootstrap, wg-cp0-rotate-bundles, surgical-update)
  collapses toward one — Block 22R's declarative source-of-truth
  becomes additive hardening rather than a prerequisite for a
  working mesh.
- **The acceptance criterion is simple and runnable.** A
  combinatorial mesh-reach test (`for src in peers; for dst in
  peers; ping src→dst`) on a 2-node bench env is the
  invariant gate. Codified as ADR 1017 — substrate-invariant
  test class. Catches any future regression of the pattern.
- **Operator cognitive load drops.** "Did the bundle put the
  catchall on the right relay?" is no longer a question.
  "Membership = full mesh reach" is the only mental model
  needed at this layer.
- **Recovery from operator error is bounded.** New-peer
  additions and bundle re-issuances no longer risk silently
  drifting the fleet into an asymmetric state.

### Neutral

- The WireGuard-layer source-IP ACL is no longer doing work as a
  security control. This is **not a reduction** in defense in depth
  because (a) it was not providing the security an operator might
  intuit it provided — the relay is sovereign infrastructure
  already trusted to forward, and a malicious relay can forge
  source IPs regardless of the ACL — and (b) the fine-grained
  access controls that *were* doing real work are all at
  application altitude and unchanged. Net security posture:
  identical to the day before this ADR.
- Block 22R's remaining work (declarative source-of-truth,
  reconcile-not-rewrite, `wg syncconf` migration) becomes
  hardening on top of a working mesh. Its independent value
  (eliminating the 4-tool drift class, eliminating F-114-style
  disruption-on-restart) is preserved. The sequencing
  changes: 22R.0 ships open-perimeter first; 22R.1-4 ships
  hardening second.

### Negative

- The WireGuard-layer mental model becomes "membership = full
  mesh reach," which a reviewer accustomed to per-peer ACLs at
  every layer may initially read as a regression. This ADR is
  the durable artifact explaining that the read is incorrect:
  fine-grained controls live at the layers above, where they
  always belonged.
- The acceptance criterion for "the mesh works" tightens — any
  regression of the `/24`-everywhere invariant must trip the
  combinatorial-reach test before merge. This is a process
  cost (test must run on every wg-cp0-touching commit) traded
  for the elimination of an entire class of latent bugs.

## Alternatives considered

### A. Status quo — keep per-bundle `/24` placement, drive symmetry by operator discipline

Rejected. Cost rises with fleet size; no upper bound on
incident rate. Operator discipline cannot
substitute for fleet-level invariants that the substrate does not
itself enforce.

### B. Adopt Tailscale

Rejected. Coordination server is hosted by a third party (loss of
sovereignty); even self-hosted via Headscale, the coordination
server becomes a single point of trust whose compromise gives an
attacker every peer's ephemeral key material. Our threat model
explicitly does not permit a centralized coordination service in
the trust boundary.

### C. Adopt Headscale (self-hosted Tailscale)

Rejected. Eliminates the third-party-host concern of (B) but
preserves the centralized-coordinator-as-attack-surface concern.
Sovereignty is preserved at the cost of operating a substantial
additional component whose security review, key custody, and
operational continuity story is not significantly cheaper than
the current wg-cp0 stack once made symmetric.

### D. Adopt ZeroTier / Nebula / Innernet

Rejected. Same trade-off shape as (B)/(C) — coordination layer
required, sovereignty cost. Additionally, each introduces a
distinct crypto primitive that does not compose with the existing
mTLS / offline-CA / OIDC stack.

### E. Per-peer `/32` enumeration on every relay peer block (no `/24` at all)

Considered. Achieves the same mesh-reach property as the chosen
decision — every relay peer block lists every other peer's `/32`,
so any source IP is accepted from any direction. Trade-off vs.
the `/24` catchall: more explicit (the wg-cp0.conf names every
peer's IP) but requires every peer's wg-cp0.conf to be updated
on every new-peer addition. This re-introduces the "must touch
every peer on every change" problem that motivated the
asymmetric-`/24` algorithm in the first place. The `/24`
catchall solves the same security claim without the
N-peer-update tax on additions.

Variant chosen by Block 22R for the **relay-side** allowlist
(per-peer `/32`) because relays are the natural choke point and
their config can be regenerated atomically; rejected for the
**peer-side** configuration because peers are heterogeneous,
intermittently online, and operator-managed.

### F. Application-layer source authentication only (drop wg-cp0 ACL semantic entirely)

Effectively what this ADR adopts, framed differently. We do not
"drop" the AllowedIPs ACL semantic — the WireGuard protocol does
not let us — but we set it to the widest valid value at the
WireGuard layer and rely on application-layer auth for
per-resource access control. This is the standard pattern in
every mesh-over-WireGuard project; we are not introducing
novelty by adopting it.

## Implementation

Tracked at `docs/project/implementation-plan.md`
Block 22R.0 (pull-forward from Block 22R authored under track
`wg-cp0-substrate-redesign-rca`).

- **22R.0.1** — bundle-create algorithm change. One-line in the
  bundle generator: emit `AllowedIPs = 198.18.0.0/24` on every
  relay peer block in every bundle.
- **22R.0.2** — combinatorial mesh-reach test on bench env.
  Codified as the first instance of ADR 1017 (substrate-invariant
  test class).
- **22R.0.3** — fleet-wide bundle re-issue ceremony. One-time
  operator action: re-mint every active peer's bundle (no key
  rotation; same pubkey, new envelope with corrected AllowedIPs);
  push relay-side allowlist no-op (pubkeys unchanged); apply
  new bundle on each peer.

Acceptance: combinatorial test green; every peer can ping every
other peer, operator laptops included.

## Related

- [ADR 0313 — silent control plane outer WG wrap](0313-silent-control-plane-outer-wg-wrap.md) — amended by this ADR (the "Authorization model" section).
- [ADR 0314 — no public DNS for mgmt endpoint](0314-no-public-dns-for-control-plane.md).
- [ADR 0315 — peer-key attestation via offline CA](0315-peer-key-attestation-via-offline-ca.md).
- ADR 0319 — wg-cp0 mesh subnet non-VPC conventional.
- [ADR 0322 — wg-cp0 declarative substrate](0322-wg-cp0-declarative-substrate.md) — Block 22R parent (supersedes the abandoned ADR 0320 draft per F-117); this ADR adds 22R.0 sub-block.
- [ADR 0407 — offline-CA bundle signing](0407-internal-x509-ca-for-service-tls.md).
- [ADR 1011 — findings register](1011-findings-register.md).
- [ADR 1017 — substrate-invariant test class (Proposed)](1017-substrate-invariant-test-class.md).
- F-114 — wg-cp0 rotation serialization gap.
- F-115 — substrate-update propagation operator-fired only.
- F-116 — wg-cp0 asymmetric AllowedIPs misapplied zero-trust at the wrong altitude (pending in same change-set).
- docs/design/wg-cp0-prod.md — design doc; amend in same change-set.
- docs/design/mesh-acl.md — the architecturally-correct altitude for fine-grained peer-to-peer policy.
- Mental model — wg-cp0 outer door, layered controls inside.

---

*See `GLOSSARY.md` for terms and acronyms.*
