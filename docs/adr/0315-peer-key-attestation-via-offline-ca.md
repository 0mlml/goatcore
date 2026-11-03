# ADR 0315 — Peer-key attestation via offline CA (Tailnet-Lock-style verification)

| Field | Value |
|---|---|
| Status | Proposed; amended 2026-05-13 — spike-bias added toward allowlist elimination |
| Date | 2026-04-27 (amended 2026-05-13) |
| Repo scope | goat core |
| Supersedes | — |
| Superseded by | — |
| Related | [`0313-silent-control-plane-outer-wg-wrap.md`](0313-silent-control-plane-outer-wg-wrap.md), [`0314-no-public-dns-for-control-plane.md`](0314-no-public-dns-for-control-plane.md), [`0208-phoenix-native-peer-discipline.md`](0208-phoenix-native-peer-discipline.md), [`0308-offline-enrollment-cbor-bundles.md`](0308-offline-enrollment-cbor-bundles.md), `0406-offline-ca-yubikey-piv.md`, Finding `F-011` |

## TL;DR (for a layperson)

Today, when a new device joins our mesh, every other device on the
mesh trusts that device's WireGuard "address" because the management
server told them to. If the management server were ever compromised
— by an insider with admin access, by a kernel exploit, or by a
supply-chain attack on the management software — the attacker could
quietly add their own device to the mesh and every other device
would build encrypted tunnels to it. We close this gap by requiring
every device's WireGuard address to also be signed by an offline,
hardware-held trust key (the same YubiKey custody we already use for
offline enrollment bundles), and by having every device verify that
signature before it accepts a new peer. Even a fully-compromised
management server then cannot mint a peer that the rest of the mesh
will accept. This is the same shape as Tailscale's "Tailnet Lock"
feature, adapted to our offline-CA pattern.

This ADR is **Phase 3 deferred** — design locked, implementation
sequenced after the Block 22 silent-control-plane work has shipped
in Phase 2D. We are *not* reshuffling Phase 2 to add this; we *are*
naming it as a Phase 3 commitment so the trigger fires on schedule.

## Context

Surfaced in Finding F-011:
netbird-mgmt is the sole authority distributing peer WireGuard
pubkeys. Existing peers accept whatever pubkey set the mgmt server
sync delivers. There is no peer-side cryptographic check against an
external trust root. A compromised mgmt-host can therefore inject a
rogue peer; existing peers will build tunnels to it.

Our threat model
(`../compliance/comparative-posture.md`)
explicitly contemplates advanced adversaries — exactly the class
that motivated Tailscale's Tailnet Lock
(`https://tailscale.com/blog/tailnet-lock`). Today our defenses
against mgmt-host compromise are entirely *preventive*: hardened
host (LUKS, STIG, FIPS), supply chain (Chainguard, signed images,
SBOM), runtime detection (Falco, osquery, auditd). All real
controls; none of them change what an adversary can do *once
inside*. An architectural complement is needed.

We have most of the building blocks already:

- **Trust anchor exists.** Offline-CA YubiKeys per
  ADR 0406 (Ed25519 PIV).
- **Bundle pipeline already signs device material.** Per
  [ADR 0308](0308-offline-enrollment-cbor-bundles.md), CBOR
  bundles already carry offline-CA signatures.
- **Block 22 already extends this for the control plane.**
  [ADR 0313](0313-silent-control-plane-outer-wg-wrap.md) +
  [ADR 0314](0314-no-public-dns-for-control-plane.md) introduce a
  per-device `cp_device_pubkey` registered into each relay's
  `wg-cp0` allowlist via the offline-CA-signed bundle pipeline.
  The shape of "device pubkey signed by offline-CA goes into
  allowlist" is therefore already running for the outer control
  plane.
- **Phoenix-native peer discipline already names the principle.**
  [ADR 0208](0208-phoenix-native-peer-discipline.md) §"Self-attest
  who it is" via persistent cryptographic identity. Today this
  describes the peer's *own* attestation; this ADR extends it to
  peer-side verification of *other* peers.

The gap is at the data plane — the mesh peer-to-peer WireGuard
tunnels. Receiving peers do not currently verify that an incoming
peer's WG pubkey is signed by our offline CA before allowing the
tunnel handshake.

## Decision

Add **peer-side cryptographic verification of every WireGuard peer's
pubkey against an offline-CA-rooted signature**, making the offline
CA the trust anchor for mesh membership rather than the management
server. The verification step is performed at the receiving peer's
daemon (or a sidecar in front of it), not at the management server.

### D1 — Trust root distribution

The offline-CA root pubkey ships in every enrollment bundle (already
true for the offline path; extended to the online setup-key path as
part of Block 22 Phase 2A YubiKey promotion). Every peer persists
the trust-root set locally; mgmt-host cannot modify it post-
provisioning. Updates to the trust-root set (e.g., key rotation)
flow only through new bundles signed by the *outgoing* root — the
same chained-signature pattern Tailnet Lock uses.

### D2 — Per-peer key signing at enrollment

At enrollment, every peer's WireGuard pubkey is signed by the
offline CA. This is already done for the offline-PACE path
(ADR 0308); for the online setup-key path it requires the
[`mgmt-host-bootstrap`](../../ops/mgmt-host-bootstrap/) Go CLI
(ADR 0410) to call into the offline-CA signing flow. The signature
is published alongside the pubkey through the normal mgmt-API sync
channel.

### D3 — Peer-side verification step

Before building a tunnel to a peer-pubkey delivered by mgmt sync,
the receiving peer's daemon (or sidecar) verifies the signature
against the local trust-root set. Three implementation options,
ranked by integration cost:

- **Option A — Zenoh-mediated signed-fact channel.** Mgmt-host
  publishes peer-pubkey-signature facts to Zenoh under
  `identity/peer/<peer-id>/wg-pubkey-signed`; peers subscribe and
  cache. Verification gates the tunnel-config write. Best fit
  with the phoenix-native pattern and the Phase 3+ Zenoh
  universal-substrate commitment ([ADR 0210](0210-zenoh-universal-substrate.md)).
  Decision deferred to D7 spike.
- **Option B — Interceptor sidecar.** A small Go service runs on
  every peer, sits between netbird-mgmt sync output and the
  WireGuard tunnel-config writer; verifies signatures, drops
  unsigned/invalid peers, emits a Prometheus metric. Avoids
  forking netbird upstream. Decision deferred to D7 spike.
- **Option C — Netbird upstream patch.** Add the verification
  step inside the netbird client. Best long-term home for the
  logic, but increases divergence from upstream and conflicts
  with the patch-surgery discipline ([ADR 0110](0110-license-posture-agplv3-forked-components.md)
  D2). Likely rejected unless the patch is small and upstream
  accepts it.

### D4 — Disablement secrets (Tailnet-Lock-style escape valve)

Two operator-held disablement secrets, plus optionally a third in
escrow, can sign a "lock disabled" attestation that drops every
peer back to mgmt-only trust. Custody mirrors the offline-CA
YubiKey custody pattern. Use case: catastrophic offline-CA loss
or compromise. Documented in the sovereign bringup runbook's
pre-flight checklist when this work lands.

### D5 — Failure modes and graceful degradation

- **Trust-root set empty / corrupted on a peer.** Peer fails closed
  — refuses to build any tunnel until trust set is restored from
  bundle. Documented break-glass in D4 path.
- **Signature on an enrolled peer's pubkey expires / revoked.**
  Tunnel is torn down; peer requires re-enrollment.
- **Mgmt-host issues a pubkey we can't verify.** Peer logs +
  Prometheus alert + drops the peer entry. Operator alerted via
  Block 17A.5 policy engine notification adapter.
- **Offline-CA YubiKey unavailable during enrollment.** Falls back
  to existing PACE Contingency offline bundle path (ADR 0308) —
  already designed to operate without online CA.

### D6 — Sequencing and out-of-scope

- **Sequenced as Block 22H** — strictly after Block 22A-22F
  (silent control plane main work) lands. Reuses the Block 22B
  bundle pipeline as the trust-root + signature distribution
  channel; that pipeline must be production-shipped first.
- **Phase 3 deliverable.** Calendar trigger: Phase 2 close-out +
  the Block 22G detection-evasion-checkpoint review (which is
  already scheduled at Phase 3 close-out per ADR 0313).
- **Out of scope:** revoking authorized-but-compromised peers
  (still ACLs + Block 18A microsegmentation); offline-CA
  custody mechanics (already covered by ADR 0406 / ADR 0606);
  detection-evasion of the verification channel itself
  (deferred per Block 22G).

### D7 — Two spikes to fire at Phase 3 entry

Decisions deferred to spike rather than locked here:

- **D7-S1 — Implementation option choice (A vs B vs C).** ~1
  week. Decision goes in `docs/project/peer-key-attestation-spike.md`.
  Spike triggers when Block 22 main work hits §"Done" (estimated
  Phase 2D close-out).
- **D7-S2 — Performance impact on a 600-peer mesh.** ~3 days.
  Quantify added handshake latency + crypto cost on Orin and x86
  hardware. Trigger: D7-S1 lands a chosen option.

The two spikes are the only branches not decided here. Everything
else (Decision sections D1-D6) is locked.

## Alternatives considered

- **Status quo + lean harder on host hardening.** All preventive,
  no architectural complement. Doesn't change what a compromised
  mgmt-host can do. Rejected as a complete answer; the host
  hardening still happens — this ADR is additive.
- **Per-peer manual key approval (Tailscale-pre-Tailnet-Lock
  pattern).** Operator must approve each new peer's pubkey by
  hand. Doesn't scale to thousands of peers across several sites. Rejected.
- **Sign every peer's pubkey at the mgmt-host (online CA).**
  Moves the trust root onto the same machine as the authority
  that mints peers — defeats the purpose. Rejected.
- **Use SPIFFE/SPIRE workload-identity attestation as the trust
  root.** SPIFFE is Phase 3+ per ADR 0412 — same timeframe but
  designed for *workload* identity, not for *machine/peer*
  identity at the WireGuard layer. Worth considering at
  D7-S1; possible re-use of SPIRE infrastructure as the
  signing-channel implementation. Not blocking.

## Consequences

**Positive.**

- Mgmt-host compromise no longer translates directly into peer
  injection. Defense-in-depth alongside ADRs 0313 / 0314 /
  0703 / 0705 / 0800.
- Phase 4+ federation with mission partners benefits: even if a
  partner mgmt server in our trust path is compromised, our
  peers won't accept rogue peers from it without offline-CA
  signature.
- Aligns with [ADR 0208](0208-phoenix-native-peer-discipline.md)
  §2A.3 — peer self-attestation extended to mutual attestation.
- Comparative-posture artifact (`../compliance/comparative-posture.md`)
  gains "control plane integrity under compromise" as a clearly-
  ahead dimension.

**Negative.**

- Adds a verification step to every peer's tunnel handshake
  hot path. D7-S2 quantifies the cost; if non-trivial,
  caching + TTL strategy in the chosen implementation.
- Adds a real engineering surface that has to be maintained and
  tested (interceptor sidecar OR netbird patch OR Zenoh signed-
  fact channel — pick one at D7-S1 and own it).
- Disablement-secrets custody is one more operator-burden item
  for a sovereign bringup.

## Assessor view (SCA-R)

NIST SP 800-53 IA-3 (Device Identification and Authentication) is
served at a stronger level than mgmt-server-only authority. SC-12
(Cryptographic Key Establishment) gains an external trust anchor
beyond the protected channel. SC-23 (Session Authenticity) gains
an end-to-end identity check rather than relying on TLS to the
mgmt server. The pattern is well-precedented (Tailnet Lock,
SPIFFE workload attestation, code-signing) and standard in
zero-trust frameworks.

## Amendment 2026-05-13 — Spike-bias toward allowlist elimination

D7-S1's framing was previously "choose A vs B vs C on evidence." That
is incomplete: any of the three can be implemented as either
(i) *additive* verification alongside today's static relay-side
allowlist (closes the F-011 trust-concentration gap but leaves the
per-new-peer "push allowlist to every relay" ceremony intact), or
(ii) *substitutive* verification that lets us delete the static
allowlist entirely (closes F-011 *and* eliminates the operator
friction that today's per-new-peer ceremony causes across the wg-cp0
relay tier).

The two outcomes look identical from a trust-model standpoint and
very different from an operator-workflow standpoint. The dominant
operator friction in the perimeter substrate today is *not* the
existence of an authentication layer (wg-cp0 is silent and
hardware-rooted, which is the property we want) — it is the
*manual maintenance of the allowlist that gates that layer*. The
[Block 22K bundle-rooted enrollment substrate](../design/offline-enrollment.md)
already delivers a per-device CA-signed credential at issuance time;
22H is the runtime verification layer that lets the relay stop
trusting the static allowlist and start trusting the credential
presented at handshake.

**Bias:** the D7-S1 spike SHALL evaluate each option (A/B/C) against
a fourth decision dimension — *"does this option enable full
deletion of the relay-side static allowlist, or does it only add a
verification step alongside it?"* — and SHALL prefer options that
enable deletion. Concretely:

- **Option C (Netbird upstream patch)** maps most naturally to
  deletion if the patch verifies handshake-time-presented
  attestation against the offline-CA trust root and admits peers
  dynamically. Patch-surgery discipline ([ADR 0110](0110-license-posture-agplv3-forked-components.md) D2)
  still applies — the bias raises Option C's *value* relative to
  the cost of maintaining a divergent fork, but does not override
  the cost. **A WireGuard fork itself is explicitly *not* on the
  table** (changing the WG handshake protocol is a much larger
  scope than the goal warrants); Option C operates at the netbird
  client layer that sits above stock WG. The 22H value is most
  fully realized in wg-cp0 (the relay-tier perimeter); the same
  pattern applies, secondarily, to mesh-tier peer admission.
- **Option B (interceptor sidecar)** also maps to deletion if the
  sidecar writes the peer list dynamically based on incoming
  CA-signed attestations rather than reading a pre-staged
  allowlist. This is likely the lowest-engineering-cost route to
  the goal — no upstream divergence, all moving parts in our own
  Go codebase.
- **Option A (Zenoh-mediated signed-fact channel)** is the
  furthest from deletion: it adds a verification step *before* the
  netbird-mgmt sync delivers peer-pubkey-signature facts, but
  does not by itself remove the static allowlist on the
  relay-side wg-cp0 servers. Net positive over status quo, but
  does not deliver the workflow win.

**Success criterion (post-22H steady state):** "issue bundle" is
the only ceremony per new peer; the relay-side wg-cp0
`peer_allowlist` accumulator at [`ops/ansible/wg-cp0-bootstrap.yml`](../../ops/ansible/wg-cp0-bootstrap.yml)
Pass 2A is retired (or reduced to a degenerate one-entry-per-relay
list for the relay-relay handshake which has different trust
properties); every relay admits new peers on the basis of
handshake-time verification against the offline-CA trust root, the
same root that signs the bundle delivered to the peer at issuance.

**Composition with Block 22K.9 bundle-renew daemon:**
the success criterion above pairs directly with bundle auto-renew —
once admission is credential-validated at handshake and renewal is
automated, "rotation without keyboard time" becomes a measurable
property of the substrate rather than an aspiration. The two changes
together complete the credential-validating-perimeter shape; either
alone leaves the operator workflow short of the goal.

**ECDSA P-256 root note (Block 79, 2026-05-10):** ADR 0315 was
written when the offline-CA root was Ed25519. Block 79 rotated the
root to ECDSA P-256 to close macOS-Keychain import compatibility
(F-090).
The signature-verification step in this ADR is algorithm-agnostic
(verifies against whatever the `ca.Signer` produces); the bias
recorded here applies unchanged.

**Sequencing forward.** Phase 3 entry remains the named trigger,
but D7-S1 + D7-S2 are now strongly biased to land *early* in Phase
3 (not late) because the workflow value is now better understood
and is operationally felt today. The spike artifact at
`docs/project/peer-key-attestation-spike.md` records the chosen
option *and* the deletion-vs-additive analysis for posterity.

## Trigger conditions for revisit

- **D7 spikes complete.** Move ADR from Proposed → Accepted with
  the chosen implementation option locked.
- **A documented incident demonstrating a real mgmt-host
  compromise (any cluster, any partner).** Move work forward
  in the schedule; revisit whether Phase-3 deferral is still
  appropriate.
- **Tailscale Tailnet Lock graduates from "alpha" and adds
  features (e.g., signing for shared nodes) we should match.**
  Re-read at that point and revisit D1-D5.
- **Netbird upstream adds a peer-key-attestation primitive of
  its own.** Likely lets us collapse D3 Option B/C into a
  smaller patch. Revisit D7-S1 with the upstream option in
  scope.
- **Operator-friction telemetry from the wg-cp0 allowlist
  ceremony exceeds a threshold (e.g., > 1 hr of operator
  keyboard time per week across the fleet for allowlist
  pushes).** Promote 22H from Phase-3-deferred to Phase-3-entry
  even if the calendar trigger has not fired.

---

*See `GLOSSARY.md` for terms and acronyms.*
