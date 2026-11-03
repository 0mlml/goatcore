# ADR 0843 — Public mTLS-gated crutch tier for mgmt-API + signal + relay (operator-toggleable; mobile bridging is the first activation rationale)

| Field | Value |
|---|---|
| Status | Proposed |
| Date | 2026-05-10 |
| Repo scope | goat core (terraform-prod for crutch-tier VPSes; ops/ansible for `goat-public-mtls-crutch` role; ops/enrollment for `bundle-create --mobile=true` plumbing) |
| Supersedes | — |
| Superseded by | — |
| Related | [`0313-silent-control-plane-outer-wg-wrap.md`](0313-silent-control-plane-outer-wg-wrap.md), [`0314-no-public-dns-for-control-plane.md`](0314-no-public-dns-for-control-plane.md), [`0407-internal-x509-ca-for-service-tls.md`](0407-internal-x509-ca-for-service-tls.md), `0406-offline-ca-yubikey-piv.md`, [`0840-goat-client-cross-platform-daemon-gui.md`](0840-goat-client-cross-platform-daemon-gui.md) (amendment 2026-05-10b records the v0.2 path-A commitment that reduces the mobile-bridging activation rationale), [`0110-license-posture-agplv3-forked-components.md`](0110-license-posture-agplv3-forked-components.md), F-109 (the constraint that motivates the first activation) |

## TL;DR (for a layperson)

Phones can run only one VPN-style app at a time, which would otherwise lock mobile users out of the mesh entirely. We open a small, deliberate, public web-style entrance with strict client-certificate authentication — the kind embassy gates use; only matched ID gets in — on three new servers, and let phones reach the same management, signaling, and relay services through that gate. Their normal HTTPS stack does the work; no VPN slot is consumed. The substrate is built to be turned **on or off** cleanly by an operator. Mobile is the first reason we are turning it on. Once the combined phone app ships, mobile no longer needs it day-to-day, but the substrate stays in the toolbox for other reasons we know we will want it (partner devices that cannot install our usual client; emergency on-ramp during outages of the silent path; locked-down operator devices; quick external sanity-check of the public-facing services).

## Context

The goat substrate normally puts every peer behind two layers:

1. **wg-cp0 outer tunnel** — silent control plane per [ADR 0313](0313-silent-control-plane-outer-wg-wrap.md). No public DNS ([ADR 0314](0314-no-public-dns-for-control-plane.md)). Bundle-rooted enrollment ([Block 22K](../design/offline-enrollment.md)). Mgmt-API + signal-server federation + Spock replication all ride this.
2. **netbird inner mesh** — peer-to-peer WireGuard data plane. Signal-server brokers connection setup; relay tier carries traffic when peer-to-peer fails.

Both layers are independent kernel-WG interfaces on Linux / macOS / Windows hosts. They coexist trivially on desktops. They do **not** coexist on iOS / iPadOS / Android phones — Apple `NEPacketTunnelProvider` and Android `VpnService` are single-claimant system-VPN slots (F-109). The steady-state architectural answer is **path A** — `goat-client` v0.2 absorbs netbird as a library link and runs both layers inside a single PacketTunnelProvider. Recorded in ADR 0840 amendment 2026-05-10b.

Path A is several weeks of focused engineering plus app-store review cycles away. During that window mobile users would be entirely blocked from the mesh.

Beyond mobile bridging, several other near-and-medium-term scenarios want the same shape — a public-IP, mTLS-gated reach into mgmt + signal + relay that does not require the wg-cp0 client to be installed on the originating device:

- **Partner device on-ramp** for partners who cannot install the wg-cp0 client (locked-down corporate laptops; partner endpoints under MDM constraints they cannot relax). Today such partners are blocked from mesh participation entirely.
- **Emergency on-ramp during mass wg-cp0 unavailability** — if a regional ISP, AWS region, or wg-cp0 forwarder tier degrades simultaneously, operators need a parallel reachability path to the mesh that does not depend on the same substrate that just broke. (This is a deliberately limited "blue path" — does not promise full equivalence to the silent plane, just enough reachability to coordinate recovery.)
- **Non-engineer operators on locked-down devices** — sponsor/program-staff laptops where adding a kernel-WG interface or a privileged daemon is operationally infeasible.
- **Quick external-perspective validation** — "what does our mgmt-API look like to an outside-the-mesh observer?" answered without spinning up a one-shot test VPS. Reuses the existing crutch substrate as the perspective; pairs with snitch / Block 78 synthetic-probes for an outside-in measurement.

These rationales did not all exist when wg-cp0 was first designed; they accumulated as the substrate has been adopted across a broader cohort of devices and partners. The right response is to build the public-mTLS substrate **once**, make it operator-toggleable cleanly, and let mobile bridging be its first activation.

## Decision

Stand up a **public mTLS-gated crutch tier** — three dedicated VPSes (one per relay region) — that exposes mgmt-API + signal-server + relay over standard TLS on standard ports, with `tls.options.<name>.clientAuth.clientAuthType: RequireAndVerifyClientCert` enforcing per-device X.509 client-cert authentication via the offline CA. Devices reach mgmt + signal + relay over their normal HTTPS stack — no system-VPN slot consumed — and present a per-device client cert minted by [`bundle-create --mobile=true`](../../ops/enrollment/cmd/bundle-create/) and shipped inside the [Block 22K](../design/offline-enrollment.md) bundle.

The decisions of record:

### D1 — Crutch covers mgmt-API + signal-server + relay (NOT mgmt-API only)

Devices using the crutch get full mesh participation: management-plane reachability AND peer-to-peer signaling AND relay-fallback throughput. A narrower mgmt-API-only crutch was considered and rejected — it would leave mobile peers enrolled but unable to actually exchange traffic with other peers, which is no useful improvement on "blocked entirely." The crutch matches the user's expected feature surface: `connect → see other peers → exchange data`. The same property holds for the other activation rationales (partner devices, emergency on-ramp, etc.).

### D2 — Dedicated new VPSes; NOT co-located with the existing prod wg-cp0 relays

The crutch tier is a **deliberately public surface** — public DNS, public TLS, pre-handshake fingerprintable. The existing prod wg-cp0 relays (per `wg-cp0-prod.md`) are deliberately *silent* per ADR 0313. The two postures are incompatible on the same host. Co-locating the crutch on existing relays would erode the silent-listener invariant on the original tier — every TLS handshake on the public IP yields fingerprinting material a passive observer can correlate to the wg-cp0 endpoint, which is exactly the property ADR 0313 closes.

Three new VPSes match the prod relay footprint (one per relay region). **Starting size is a small general-purpose instance**; Block 80H capacity drill validates or upgrades. DNS lives in the production zone (production tree per `cloud-environments.md`). Ongoing cost for the three small instances is low.

The crutch tier carries its own Ansible role + its own terraform-prod modules so its lifecycle is decoupled from the silent-control-plane tier — terraform destroy of the crutch leaves wg-cp0 untouched and vice-versa.

### D3 — Per-device client cert minted by the offline CA via `bundle-create --mobile=true`

Per-device authentication uses X.509 client certs issued by the same offline CA (ADR 0406 custody) that already signs CBOR enrollment bundles ([ADR 0407](0407-internal-x509-ca-for-service-tls.md) extends the CA to X.509). The cert is minted at bundle-create time when the operator passes `--mobile=true`; the cert + private key are embedded in the CBOR bundle as new optional fields `mobile_client_cert` + `mobile_client_key` (under the existing CA signature, so tamper-detected for free). Bundle-create sans the flag does not mint a mobile cert — the crutch is opt-in per bundle, not per fleet.

Per-device certs (not per-user, not per-site, not shared) are required so that:

- Revocation has per-device granularity. Lost phone → revoke its cert, fleet keeps working.
- Audit logs identify the originating device by CN, not just by source-IP-of-the-moment.
- Compromise of one device does not cascade.

CRL distribution piggybacks on the existing offline-enrollment CRL pipeline (per [`offline-enrollment.md`](../design/offline-enrollment.md) §6). The Traefik consumer pulls the CRL on a 5-minute cadence and refuses any client cert listed; the crutch tier filters on a `mobile-revoked` selector so mobile-cert revocations apply at the gate.

Flag naming: `--mobile=true` reads as the dominant first-activation rationale and stays the operator-facing default. A future `--public-mtls=true` synonym (or per-rationale flags like `--partner-ramp=true`) can land if the operator pool grows beyond mobile bridging; the underlying cert issuance is identical.

### D4 — Per-device cert TTL: 90 days (matches bundle TTL, single revocation surface)

The choice is between 90 days (matching the bundle's `not_after` per [`offline-enrollment.md`](../design/offline-enrollment.md) §3) and 1 year (matching the [ADR 0407](0407-internal-x509-ca-for-service-tls.md) leaf-cert convention for service-side TLS). 90 days wins on three grounds:

1. **Single revocation surface.** Bundle expiry already gates device validity; matching the cert TTL means one operator action retires both. Mismatched TTLs (cert valid past bundle expiry) creates a window where a compromised device could reach mgmt + signal + relay even though its bundle has expired — defeats the bundle TTL.
2. **Fleet-wide rotation cadence.** Bundle re-issue every 90 days is already an operator routine. Mobile-cert renewal slots into the same cadence, no new operator workflow.
3. **Cert lifetime stays short relative to substrate-activation cadence.** Even when the crutch is active continuously, no cert outlives the typical operator review-and-prune cadence.

### D5 — Operator-toggleable lifecycle (NOT a one-shot calendar-bounded sunset)

The crutch substrate is built to be **enabled, disabled, and re-enabled** by operators as the use case justifies. It is not a one-shot bridging artifact that retires permanently when path A ships.

Lifecycle states:

- **Activated.** terraform-prod `crutch_tier` modules applied; three VPSes running; Ansible `goat-public-mtls-crutch` role deployed; DNS records published; per-device `--mobile=true` bundles being minted as needed; CRL pipeline live. This is the state the substrate enters at first stand-up.
- **Deactivated (substrate kept).** Traefik routes pulled on the crutch tier (refuses new connections; existing connections drain); DNS records retired (or pointed to a maintenance page); no new mobile certs minted (`bundle-create --mobile=true` returns an informational error pointing operators at the v0.2 install path); per-device certs revoked en masse via CRL; VPSes either kept up at minimum size for fast re-activation OR destroyed. terraform state, Ansible role, and bundle-create plumbing all stay; deactivation is reversible in operator hours, not days.
- **Re-activated.** Re-apply terraform-prod, re-deploy Ansible role (gated on activation rationale review per the runbook), republish DNS, re-mint per-device certs as needed.

The expected lifecycle is:

1. **First activation: mobile bridging.** Today (v0.1.x desktop only). Substrate stands up.
2. **Mobile-bridging activation deactivated when path A is operationally available.** Once goat-client v0.2 ships and operators confirm the upgrade path is working for the mobile user pool, the mobile-bridging *rationale* for keeping the crutch on no longer applies.
3. **Substrate stays present.** Operators can re-activate for any of the other documented rationales (partner device on-ramp, emergency reachability, non-engineer locked-down devices, external-perspective validation) without re-doing the substrate work.
4. **Re-deactivation cycles** match the activation rationale's lifetime (e.g., emergency on-ramp activates for the duration of the wg-cp0 outage, then deactivates).

The substrate is not "always on by default" — operators activate it for a stated rationale and deactivate it when the rationale lapses. Activation is an operator-fired action with explicit recorded rationale; the runbook tracks the active rationale and review cadence.

A naive read might expect a calendar-bounded sunset; this ADR explicitly rejects that read. The cost-of-having-the-substrate is small (three small instances when activated; near-zero when deactivated-with-VPSes-destroyed), the cost-of-not-having-the-substrate at the moment a future rationale fires can be high (mobile users blocked again; emergency reachability gone when needed; partner on-ramp blocked). Net: keep the substrate; toggle the activation.

### D6 — `--mobile=true` flag gates issuance; crutch is mobile-by-intent at first activation

Bundle-create defaults to no mobile-cert issuance. Operators opt in per-bundle when issuing a bundle for a device that will use the crutch path. Desktop and site-host peers do NOT receive mobile certs by default — this keeps the crutch population to exactly the user class that needs it, minimizing the public-cert-on-the-wire fingerprint surface.

The flag is not a security boundary (an operator could trivially issue mobile certs for desktop devices); it is an intent-and-housekeeping declaration. Audit-log review of bundle-create runs surfaces any mismatch between the issued certs and the deployed device class.

### D7 — Signal-server forwarders, NOT signal-cluster-mesh participants

Two shapes were considered for how the crutch-tier signal terminates:

- **Pure forwarder.** Crutch-tier `signal-server-forwarder` is a thin pass-through that mTLS-terminates inbound from the device and dials the inner-mesh signal cluster over wg-cp0 (the standard cross-site federation path). The signal-server cluster itself stays on the wg-cp0 silent-control-plane tier, unchanged in topology.
- **Federated cluster member.** Crutch-tier signal-server is a peer of the existing multi-node signal cluster. Federation participants exchange Forward RPC peer-to-peer.

The pure-forwarder shape wins. Reasons:

- Federation invariant per `wg-cp0-prod.md` stays unchanged on the original tier (the existing signal mesh is untouched). The crutch participates as a *client* of that mesh, not as a peer.
- Signal-server quorum / consensus / failure-domain assumptions stay constant. Adding three crutch-tier signal nodes to the federation mesh would be a change to the existing topology that needs its own separate decision; the crutch should not co-decide that.
- Simpler operational story for the toggleable lifecycle (D5). A pure forwarder being deactivated cleanly disconnects its devices and nothing else; a federated cluster member being deactivated is a topology event the existing signal mesh has to absorb.
- Latency is a concession (device → crutch-tier → wg-cp0-back-to-inner-mesh-signal vs. device → federated-cluster-member-direct), but acceptable given the activation rationales.

### D8 — Relay-tier coupling: expose the EXISTING goat-relay-rewrite tier through the crutch (NOT a second relay tier)

The existing 3-VPS goat-relay-rewrite tier (per `relay-paths.md`, Phase 3 advertised 2026-05-06) already carries production peer-to-peer relay traffic for the prod fleet. Standing up a **second** relay tier behind the crutch would double the relay capacity cost (three more relay-class instances) and split the fleet into two cohorts (cert-bearing-mobile-cohort routes through tier B; everyone else routes through tier A). The two cohorts would have to either heal (two tiers concurrently advertised in `management.json` `Relay.Addresses` — operationally fine, no per-cohort routing decision) OR diverge (operationally complicated).

Cleaner: the crutch-tier Traefik reverse-proxies the relay endpoint over the wg-cp0 path back to the existing goat-relay-rewrite tier. Devices on the crutch dial `relay.<crutch-host>:33073` over mTLS-gated TLS; crutch Traefik forwards to the inner relay tier; the inner tier handles the peer-to-peer relay forwarding as it does today. M-040 / M-041 capacity envelope already covers this — the crutch does not add net relay throughput requirements, only a new entry point shape.

Risk: Traefik must support the netbird-relay wire protocol (WS / QUIC). WS support is uncontroversial. QUIC reverse-proxying via Traefik is less mature; if Traefik cannot pass QUIC cleanly the crutch falls back to WS-only on the relay leg. Block 80 sub-task 80B.5 tests this and decides; explicit "open question" in the design doc until measured.

## Trade-offs

**Cost:**

- Engineering: ~5-7 days for substrate (terraform + Ansible role + bundle-create flag + CRL filter + Traefik mTLS config + per-route map). ~2-3 days mobile shell wiring (Block 80F). ~2 days capacity validation drill (Block 80H). Total Block 80 spend: ~9-12 engineering days.
- Procurement: three small crutch-tier VPSes when activated; ~$0/mo when deactivated and VPSes destroyed (substrate-as-code stays free).
- Maintenance when activated: bounded; CRL drift and Traefik mTLS config are the two ongoing concerns. Both have mechanical alarms (Block 80B).

**Benefit:**

- Mobile users participate in the mesh during the first-activation window. Without the crutch they are blocked entirely until v0.2 ships.
- Substrate reusable for partner device on-ramp, emergency reachability, non-engineer locked-down devices, external-perspective validation. Each of those rationales would otherwise be a separate one-off engineering effort.
- Forces an early, real exercise of `RequireAndVerifyClientCert` + per-device CRL filtering at the Traefik layer. That experience is reusable in any future mTLS-gated public surface (partner gateways, for example).
- Per-device cert issuance via `bundle-create` is reusable; the same primitive is what a future per-device-mTLS partner-gateway pattern would want.

**Risk:**

- **Public TLS surface exists where ADR 0313 said no public surface should exist.** Honest: the crutch tier yields pre-handshake info disclosure (SNI, mTLS-required signal in the TLS handshake response, public IPs in DNS). Reasoning: the crutch is on **dedicated** VPSes with **dedicated** DNS, so the silent-control-plane invariant on the original tier is preserved. The crutch is its own posture, not a relaxation of the original tier's posture.
- **mTLS strict-required is a hard gate, not a defense-in-depth.** A misconfigured Traefik (`clientAuthType` accidentally degraded to `VerifyClientCertIfGiven`) drops the gate entirely. Mitigation: Ansible role asserts the strict-required setting on every deploy + healthcheck probe verifies a no-client-cert connection is rejected with TLS 4xx, not allowed through.
- **CRL pipeline staleness.** If the CRL pull is broken, revoked mobile certs continue to authenticate. Mitigation: stale-CRL alarm fires when the local CRL on the crutch tier is older than 1h; Traefik fails closed if the CRL file is older than 6h (configurable).
- **Substrate becoming silently load-bearing past its activation rationale.** Risk that "mobile-bridging activation deactivated when path A ships" never quite happens because mobile users + operator routine quietly normalize on the crutch. Mitigation: monthly engineering-health review of the active rationale + the device cohort using each crutch-active mode; if mobile-bridging is still active 60 days after path A is generally available to the mobile user pool, the review explicitly re-justifies or deactivates.
- **Cost disclosure.** The crutch has a (small) public surface that an adversary can fingerprint when activated. Crutch-tier hostnames, IPs, and mTLS-required-handshake response signal "goat-net public-mTLS endpoint here." We accept this cost during activation in exchange for the activation-rationale benefit. ADR 0313's silent posture remains the correct choice for the steady-state secure plane; the crutch is the bounded, deliberate exception that the operator chooses to enable.

## Alternatives considered

### Bring up goat-client v0.2 first, no crutch substrate at all

**Rejected.** Path A is the right steady state but mobile users are blocked for ~2-3 weeks of focused engineering work plus app-store review cycles. Operationally that's a long stretch of mobile-mesh-unavailable. And even after path A ships, the other activation rationales (partner device on-ramp, emergency reachability, locked-down operator devices, external-perspective validation) still want the same substrate; building it once and toggling activations is cheaper than building it per-rationale.

### Pure path C — relax wg-cp0 silent posture and put mgmt + signal + relay on the public DNS surface (no mTLS gate)

**Rejected.** Violates ADR 0313 silent-control-plane invariant with no compensating control. mgmt-API on the public internet, even behind TLS, is exactly the property Block 22 was designed to remove (TLS handshakes pre-auth yield fingerprinting material). The crutch's mTLS strict-required is the compensating control that distinguishes this from path C.

### Narrower crutch — mgmt-API only, leave signal + relay unreachable

**Rejected** per D1. Devices using the crutch enrolled but unable to exchange traffic is not a meaningful improvement; it stops at "you can see the mesh exists" without any way to use it. The user-visible feature is "I can connect" — which means signal + relay must be reachable.

### App-scoped userspace WireGuard, no system-VPN slot claim

**Rejected.** WireGuard-go can be linked into a mobile app without claiming the system tunnel slot, but the resulting tunnel only captures traffic the app process explicitly proxies. The mesh-peer use case (the user's other apps reaching mesh services) requires system-tunnel scope. Without it the user gets a single-app overlay, not mesh participation.

### Wait for Apple / Google to ship multi-VPN-slot support

**Rejected.** Apple has had the single-claimant constraint since the NetworkExtension framework launched (iOS 8, 2014). No public roadmap signal of relaxation. Waiting is indefinite. Even if Apple shipped multi-slot tomorrow, the other crutch activation rationales are independent of mobile.

### Time-bounded sunset on path A ship (the original framing)

**Considered and reframed.** The original prompt for this work treated the crutch as a one-shot bridging artifact that retires permanently when goat-client v0.2 ships. After review, that framing leaves three concrete activation rationales (partner device on-ramp; emergency reachability; locked-down operator devices) and one diagnostic rationale (external-perspective validation) without a substrate, which would mean rebuilding equivalent mTLS-gated public surfaces ad-hoc later. Cheaper to design the substrate for clean operator-toggleable activation in the first place; mobile-bridging is its first activation, not its only one.

## Implementation tracking

Block 80 sub-tasks (80A terraform → 80B Ansible role → 80C bundle-create plumbing → 80D CRL filter → 80E snitch probe extension → 80F mobile-side wiring → 80G activate / deactivate / re-activate runbook → 80H capacity validation drill). Backlog entry at `docs/project/backlog.md`.

Acceptance: substrate landed on the three dedicated VPSes; one mobile-cert bundle minted via `bundle-create --mobile=true`; iOS / Android shells in the goat-client repository reach mgmt + signal + relay through the crutch with the cert active; one operator-run capacity validation drill PASSES against synthetic mobile-client load; one operator-fired enable / disable / re-enable cycle completed end-to-end against the runbook.

## Glossary

See `GLOSSARY.md`.
