# ADR 0313 — Silent control plane via outer WireGuard wrap

| Field | Value |
|---|---|
| Status | Proposed — **paused 2026-09-12 by [ADR 1192](1192-wg-cp0-paused-public-dns-entry-point.md)**; kept as the design to re-apply |
| Date | 2026-04-26 |
| Repo scope | goat core |
| Related | [`0202-three-vps-relay-topology.md`](0202-three-vps-relay-topology.md), [`0305-rathole-tunnel.md`](0305-rathole-tunnel.md), [`0308-offline-enrollment-cbor-bundles.md`](0308-offline-enrollment-cbor-bundles.md), [`0314-no-public-dns-for-control-plane.md`](0314-no-public-dns-for-control-plane.md), [`0321-wg-cp0-perimeter-not-acl.md`](0321-wg-cp0-perimeter-not-acl.md), [`../design/offline-enrollment.md`](../design/offline-enrollment.md) |

**Amendment 2026-05-13 ([ADR 0321](0321-wg-cp0-perimeter-not-acl.md)):** The
"Authorization model" of this ADR is now read in conjunction with ADR 0321.
wg-cp0's WireGuard layer enforces **membership only** (valid offline-CA-signed
bundle = full mesh reach at the WireGuard layer); fine-grained per-resource
access control lives one altitude up at netbird-mesh ACL groups, Ory OIDC,
mutual TLS, Spock per-database users, Zenoh key-expressions, and the egress
proxy. The earlier `bundle-create` algorithm that placed the fleet `/24`
catchall on one relay per bundle (producing per-bundle asymmetric AllowedIPs)
is retired; every relay peer block in every bundle now carries
`AllowedIPs = 198.18.0.0/24`. See F-116
for the failure-mode incident chain and ADR 0321 for the durable fix.

## TL;DR (for a layperson)

Today, the part of the mesh that runs the management plane
answers anyone who connects to it on the public internet —
even just to say "no" — which lets a scanner confirm that
something is there and identify what it is. We change this
so the management plane sits behind an outer WireGuard tunnel
whose listener answers nothing without a valid pre-shared
key. To anyone scanning the relay, the management port looks
indistinguishable from a port that is not in use at all. The
keys to reach it travel inside the same offline enrollment
bundle a device gets at provisioning time, so a credentialed
device can still get in — just not anyone else.

## Context

The relay topology ([`0202`](0202-three-vps-relay-topology.md))
exposes the Netbird management plane on TCP 443 of every VPS
relay via a rathole tunnel back to the network host
([`0305`](0305-rathole-tunnel.md)). Today the rathole edge
accepts plaintext TCP, which produces a TLS handshake and a
recognizable Netbird management response to anyone who
resolves the public `netbird.<zone>` name (or scans the relay's
public IP). That gives a passive observer:

1. Confirmation that a Netbird management plane is running here.
2. A long-lived target IP and port to attempt enrollment, brute,
   or DoS against.
3. Fingerprinting material (TLS cert SAN, Netbird response
   structure) that links the relay to the wider mesh.

The user requirement is "no part of the secure net on the
public internet" — meaning the management plane should not be
discoverable, fingerprintable, or addressable from an
unauthenticated source. Detection-evasion tactics (decoys,
traffic shaping, fingerprint-hiding) are explicitly not the
goal of *this* decision and are deferred (see Trigger
conditions).

## Decision

Place the management API behind an **outer control-plane
WireGuard tunnel** terminated on each relay, separate from
the data-plane mesh.

- **Listener.** Each relay runs a second WireGuard interface
  (`wg-cp0`) on a UDP port (default 51821, configurable per
  relay). This interface is for the control plane only, not
  for data-plane peer mesh traffic.
- **Behaviour.** Like all WireGuard listeners, `wg-cp0` is
  silent under unauthenticated handshakes — no TLS banner,
  no Netbird response, no ICMP, no port-scan signature
  beyond "UDP port that may or may not be open."
- **Authorisation.** Only peers whose static public keys are
  configured on the relay can complete the handshake. Keys
  are distributed via the offline enrollment bundle
  ([`0308`](0308-offline-enrollment-cbor-bundles.md)); see
  the extension to [`../design/offline-enrollment.md`](../design/offline-enrollment.md)
  §11 added in the same change set.
- **Inside the tunnel.** Management API traffic (gRPC over
  TCP 443) is reachable on a private address inside `wg-cp0`.
  The rathole tunnel from network host to relay is rebound
  to that private address; nothing on the relay's public
  interface accepts mgmt-API traffic any more.
- **Rotation.** The control-plane WG keypair on each relay
  rotates per the credential-hygiene cadence in Block 18D.
  Key rotation is wrapped in a new bundle reissue (the bundle
  carries the relay's WG public key alongside its endpoint).

Enrollment via legacy public-DNS / setup-key remains
retired-by-default (see [`0314`](0314-no-public-dns-for-control-plane.md))
and is brought back only via documented break-glass.

**Sequencing.** This ADR ships *with* ADR 0314 in the same change
set — see ADR 0314 for the bidirectional rationale. Block 22A–22F in
`../project/implementation-plan.md`
order the work: relay-side `wg-cp0` (22A) → bundle pipeline (22B) →
device first-contact (22C) → 14-day dual-stack overlap (22D) →
public-DNS retirement (22E) → break-glass runbook (22F).
Trust-root relocation to the offline CA (Block 22H, ADR
[`0315`](0315-peer-key-attestation-via-offline-ca.md)) sequences
strictly after this block — silent-listener does not by itself
prevent a compromised mgmt-host from minting peers.

## Alternatives considered

- **SPA / fwknop UDP knock in front of the rathole TCP
  listener.** Adds a separate daemon, a separate key
  custody story, and time-sync sensitivity, in exchange for
  silencing a listener WireGuard already silences for free.
  Reserved as a Phase 3+ option if WireGuard-fingerprint
  scanning becomes a credible threat (revisit trigger).
- **mTLS at the rathole edge.** TLS handshake responds before
  client auth; an observer can still see "TLS endpoint here"
  and harvest cert metadata. Does not meet the requirement.
- **Private DNS namespace alone.** Hides the name, not the
  listener — once a relay IP is known, the listener still
  responds. Security through obscurity. Rejected as the
  primary control; adopted as a complementary measure in
  ADR 0314.
- **Move the management plane off public-cloud VPS entirely.**
  Goes against the cloud-for-find-itself / sovereign-for-
  run-itself split (project_sovereignty_split). Rejected for
  this scope.

**Additional alternatives, considered 2026-05-21.** A first-principles
pass triggered by operator concern over the operational cost of running
two WireGuard tunnels fleet-wide. The four 2026-04-26 alternatives above
remain correct; these are the alternatives that were not originally
enumerated, plus a reframing of the real trade.

- **Tor v3 hidden service as the mgmt-API entrypoint.** Strong
  front-side silencing (no inbound IP, rotating address, no cleartext
  fingerprint). Rejected: +200-800ms latency and circuit-build delay
  are hostile to Spock streaming and signal federation, and Tor is
  blocked in some target networks. Even if some non-realtime mgmt-API
  calls could ride it, the latency-sensitive replication piece cannot.
- **Cloudflare Tunnel / ngrok / Tailscale Funnel (vendor reverse
  tunnels).** All move the public listener to a vendor edge that
  terminates clear-text TLS — same rejection class as "mTLS at the
  rathole edge" above, plus a hard vendor dependency that is
  sovereign-deploy-incompatible. Net negative.
- **Reverse-WG rendezvous (self-hosted phone-home).** Mgmt-host phones
  home to a central rendezvous; clients reach mgmt via the rendezvous.
  Just relocates the silent-listener requirement to the rendezvous;
  no net win.
- **Single tunnel — drop wg-cp0; mgmt-API on a netbird mesh-IP gated by
  netbird group-ACL.** The most plausible operational simplification.
  Bundle pipeline gets dumber, no 22D dual-stack ceremony, the F-114 /
  F-116 class of bug disappears, F-109 mobile single-VPN-slot pain
  collapses. Rejected on two grounds. **First**, it shifts the mgmt-API
  trust boundary from a tiny WG-pubkey peer list (wg-cp0 peers only) to
  a fleet-sized ACL enforcement inside the netbird daemon, expanding
  blast radius if netbird's relay sees a 0-day RCE. **Second**, it
  re-introduces the perimeter / ACL conflation ADR 0321 was written to
  clean up — the perimeter (netbird relay) and the ACL (netbird group)
  become the same substrate.
- **Tiered posture — wg-cp0 in production only; single-tunnel
  via netbird mesh-ACL in trial fabrics as a permanent steady-state architecture.** Rejected on
  categorical-complexity grounds. Trial fabrics are the proving ground
  for production-shaped substrates (Blocks 82B / 82S / 83 / etc.);
  bifurcating the security model means substrates get developed against
  the weaker posture and need re-validation when they promote. Uniform
  steady-state posture across fabrics is a feature. A trial fabric may
  stand up before wg-cp0 as a temporary expedient with explicit
  re-validation cost on promotion to production-shape; that is not a
  recommendation that production fabrics ever abandon the silent
  perimeter.
- **AmneziaWG (or equivalent WG-handshake-fingerprint-obfuscating
  variant) as a replacement for mainline WG in wg-cp0.** Not actually
  an alternative — it is incremental hardening of wg-cp0 within the
  existing two-tunnel design, addressing only the residual fingerprint
  risk. Already correctly parked under "Residual" + the tightened
  trigger condition below.

**The real trade, reframed (2026-05-21).** The original "Consequences"
framing described wg-cp0 as "silent-listener for the mgmt plane." On
closer inspection, the netbird data-plane relay is publicly exposed
regardless of wg-cp0, so wg-cp0 does NOT add silencing for the fabric
as a whole — what it adds is an *independent crypto perimeter on the
mgmt-API specifically*, with a tiny WG-pubkey peer list. The value is
twofold: (1) mgmt-API not reachable from data-plane peers, and
(2) defense-in-depth insurance against a netbird relay 0-day. Value (1)
has a single-tunnel equivalent (netbird group-ACL); value (2) does not.
**Two tunnels exist primarily to buy value (2).** This framing is what
makes the operational-cost question well-formed and prevents future
reopen on ops-cost grounds alone — operational pain is being
independently driven down by Block 80 (mobile crutch tier),
goat-client v0.2 (three-mode triad per ADR 0840), ADR 0321
(perimeter / ACL clarification), and bundle-pipeline turnkey-ization,
none of which require giving up the silent perimeter.

**Closed-out status.** "Should we replace wg-cp0 with X?" is foreclosed
by this section for X ∈ {Tor, Cloudflare Tunnel, vendor reverse tunnel,
reverse-WG rendezvous, single-tunnel netbird-ACL, tiered-posture as
permanent steady-state}. Reopen only if a genuinely new technique
appears that the above analysis does not cover, or if a trigger
condition below fires.

## Consequences

**Positive.** Relay public surface no longer answers
mgmt-plane probes. WireGuard's silent-listener property does
the heavy lifting using a stack we already operate and
key-manage. No new daemon, no SPA-key custody, no time-sync
dependency beyond what WG already needs.

**Negative.** Bundle pipeline now also has to carry per-relay
control-plane WG public keys + endpoints, and rotation of a
relay's control-plane keypair forces bundle reissue for any
device that may need to reach that relay. Acceptable: bundle
reissue is already a routine operation under
[`0308`](0308-offline-enrollment-cbor-bundles.md)'s 90-day
expiry. Break-glass enrollment without a bundle is now an
explicit, documented process rather than the casual default.

**Residual.** WireGuard's protocol fingerprint (handshake
size, header bytes) is itself observable to a sophisticated
adversary even though no payload is leaked. Detection-evasion
work (AmneziaWG, traffic shaping, decoy listeners) is
deferred — see the roadmap detection-evasion checkpoint.

**Trade-off — commercial DDoS / WAF protection (added 2026-05-03).** Cloudflare's main protective products (Proxy /
WAF / Spectrum) all require DNS records pointing at CF edge so CF can
reverse-proxy. With no public DNS for the mgmt entry, those products
don't apply. CF Magic Transit (BGP-anycast L3 protection on raw IPs)
DOES work without DNS but is enterprise-tier (per-Gbps + commit) and
not justified at the small relay-fleet sizes this ADR is targeted at. What replaces CF protection in the silent-control-
plane model:

- **L3/L4 volumetric DDoS** → AWS Shield Standard (auto-on, free,
  default for AWS public IPs). Same protection CF gives at this layer.
- **Targeted floods to the wg-cp0 UDP port** → WireGuard's intrinsic
  cryptographic packet drop in-kernel + VPC security group + nftables
  hashlimit on source-IP rate (codifiable in the wg-cp0-server
  Ansible role).
- **L7 application attacks** → eliminated by construction. There is
  no L7 listener exposed on the public side; mgmt-API is reachable
  ONLY inside the wg-cp0 tunnel after a successful WireGuard
  handshake.
- **Packet analytics + flow visibility** → AWS VPC Flow Logs +
  per-relay node_exporter + a wg-cp0 handshake-attempt logger,
  shipped to our existing Prom + OpenObserve stack. Comparable
  visibility to CF logs at small scale; self-hosted.
- **Per-IP threat intel / blocklists** → not automatic; can be added
  via crowdsec / abuseipdb feeding nftables if recon traffic warrants.

The honest read: this ADR trades away CF's commercial L7 protections
in exchange for a much smaller exposed surface that AWS Shield + the
WireGuard kernel-level drop already protect well. The trade is
correct at our scale and threat-model fit; if we ever needed
multi-Gbps DDoS protection, Magic Transit is the documented
upgrade path.

Comprehensive layperson-readable description of the resulting
security posture: [`../design/control-plane-hiding-explainer.md`](../design/control-plane-hiding-explainer.md).

## Assessor view (SCA-R)

This decision serves SC-7 (boundary protection — the
authorized-only listener removes a public attack surface),
AC-3 (access enforcement — pre-shared keys gate every
session), SC-8 (transmission confidentiality — mgmt API
traffic is wrapped in WG even on the relay-to-public path),
and CP-2 (contingency — the silent-by-default posture
survives loss of public DNS without operator action).

The compensating control for the residual WireGuard-
fingerprint risk is the offline enrollment bundle's bounded
distribution and 90-day expiry: even an observer who can
identify a WireGuard listener cannot synthesize a valid
peer key.

## Trigger conditions for revisit

- WireGuard-fingerprint scanning becomes a real adversary
  capability we expect to face — operationalized (tightened
  2026-05-21) as: the wg-cp0 handshake-attempt logger shipped
  to Prom + OpenObserve shows targeted recurring scans against
  the wg-cp0 UDP port with WG-handshake-shaped initiator
  packets from a small distinct IP set. Until that signal
  fires, AmneziaWG promotion is churn against a threat with
  no measured incidence. When it fires, revisit and add
  either SPA in front of `wg-cp0`, AmneziaWG-style
  obfuscation, or both.
- Netbird upstream adds a native silent-listener mode for
  the management plane — re-evaluate whether the outer-WG
  wrap is still the right shape.
- Any documented incident where the silent-listener was
  compromised (e.g. a stolen relay private key).
- Relay topology changes (e.g. moving from VPS rathole to
  on-prem direct) — the wrap design assumes a publicly
  addressable relay edge.

---

*See `GLOSSARY.md` for terms and acronyms.*
