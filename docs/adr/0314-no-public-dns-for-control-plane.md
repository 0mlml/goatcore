# ADR 0314 — No public DNS for the control plane

| Field | Value |
|---|---|
| Status | Proposed — **paused 2026-09-12 by [ADR 1192](1192-wg-cp0-paused-public-dns-entry-point.md)**: no goatnet runs it; kept as the design to re-apply |
| Date | 2026-04-26 |
| Repo scope | goat core |
| Related | [`0312-dns-two-tier.md`](0312-dns-two-tier.md), [`0313-silent-control-plane-outer-wg-wrap.md`](0313-silent-control-plane-outer-wg-wrap.md), [`0308-offline-enrollment-cbor-bundles.md`](0308-offline-enrollment-cbor-bundles.md), [`0211-single-management-server.md`](0211-single-management-server.md), [`../design/offline-enrollment.md`](../design/offline-enrollment.md) |

## TL;DR (for a layperson)

Today, anyone can look up the public DNS name of our
management plane (`netbird.legacy.example`) and see exactly
which servers run it. We retire that public lookup. The
endpoints that a credentialed device needs to reach the
management plane travel inside the same offline enrollment
bundle that already carries its identity. A device with a
bundle still gets in; an outsider can't even find out where
to try.

## Context

ADR 0312 established a two-tier DNS split — public Cloudflare
for *enrollment* names, mesh-internal CoreDNS / dnsmasq for
mesh-internal services. At the time, the enrollment name
`netbird.legacy.example` was deliberately public so any
authorised peer could enroll from any internet-connected
network with a setup key.

Two facts changed the calculus:

1. **Phase 2A offline enrollment bundles**
   ([`0308`](0308-offline-enrollment-cbor-bundles.md)) carry
   `known_endpoints` per-bundle. A device with a bundle
   already does not need to resolve a public name to find
   the management plane.
2. **ADR 0313 silences the control-plane listener.** Even if
   the public DNS name still resolved, the listener on the
   other end no longer answers without a control-plane WG
   key. The public name is now solely a target list for
   adversaries, with no benefit to legitimate enrolment.

The user requirement is "no part of the secure net on the
public internet." The public DNS name is the last
discoverable artefact; this ADR retires it.

## Decision

Retire `netbird.legacy.example` (and the per-region
round-robin members) from public
Cloudflare DNS once the bundle pipeline carries control-
plane endpoints end-to-end.

- **Endpoint distribution.** Each enrollment bundle carries,
  per relay, the relay's public IP literal + UDP port for
  `wg-cp0` + the relay's control-plane WG public key. Bundle
  schema extension lives in
  [`../design/offline-enrollment.md`](../design/offline-enrollment.md)
  §11.
- **Bundle reissue on relay change.** Adding, removing, or
  re-IPing a relay triggers a bundle reissue cycle. Cadence
  is bounded by the existing 90-day bundle expiry plus an
  ad-hoc reissue path documented in the runbook.
- **Mesh-internal DNS unchanged.** ADR 0312's tier-2 mesh
  resolver continues to serve `*.netbird.legacy.example`
  for *internal* service discovery. Only the *enrollment*
  public record is retired.
- **Bridge / partner-federation public records unchanged.**
  `bridge.legacy.example` and the partner-facing socat
  forwarders are a separate trust boundary and stay public
  per ADR 0203 / 0309 / 0405.
- **Break-glass.** A documented exception process re-publishes
  a temporary A record for a single relay (≤24h, audited)
  for the no-bundle-available emergency. Default is
  off; bring-up requires Block 17A.5 policy-engine
  approval.
- **Sequencing.** This ADR ships *with* ADR 0313, not
  before it — removing public DNS while the listener still
  answers plaintext TCP would be a regression in
  discoverability without a corresponding gain in access
  control.

Detection-evasion measures (relay IP rotation, decoy A
records, geographically dispersed front-door names) are
deliberately out of scope for *this* decision. The roadmap
carries an explicit checkpoint to revisit (see
`../project/roadmap.md` priority
risk call-out 12).

## Alternatives considered

- **Keep public DNS; rely on ADR 0313 silence alone.** The
  silent listener prevents access but the DNS name still
  attributes the relay to the project and gives an
  adversary a target list. Half-measure; rejected.
- **Move the control plane to a private DNS namespace
  resolved only by enrolled peers.** Requires a bootstrap
  channel for the private resolver — same problem as the
  bundle solves, with extra moving parts. Rejected.
- **Rotate the public DNS name on a schedule.** Doesn't
  remove the discoverable artefact, just makes it harder to
  catalogue. Properly belongs in detection-evasion (deferred).
- **Cloudflare Access / mTLS at the public edge instead of
  removing DNS.** Adds a mandatory third-party dependency in
  the enrollment path; violates the sovereignty split
  (project_sovereignty_split). Rejected.

## Consequences

**Positive.** No public DNS record for the management plane.
Combined with ADR 0313's silent listener, an outside observer
cannot enumerate or fingerprint the mgmt plane from internet
scanning + DNS reconnaissance. Sovereignty-aligned (no
third-party DNS becomes a hard dependency for enrolment).

**Negative.** "Type one command from any network with a
setup key" is no longer possible. Every enrolment requires a
bundle delivered through the offline pipeline. Acceptable:
that was already the Phase 2A operating model. Break-glass
re-publication of a public record is now an audited,
policy-engine-gated event.

**Operational.** Adding or replacing a relay forces a bundle
reissue for affected device classes. The reissue runbook
already exists; this just makes "relay topology change"
another routine reissue trigger alongside expiry and key
rotation.

**Residual.** The control-plane WG endpoints inside bundles
become a higher-value target — anyone who exfiltrates a
bundle obtains both a credential *and* the endpoint to use
it against. Bundle in-flight protections (activation
deadline, registry, no-show auto-revoke per
`offline-enrollment.md` §3.1) are the compensating control.

## Assessor view (SCA-R)

This decision serves SC-7 (boundary protection — public DNS
exposure is reduced to enrollment-irrelevant names), SC-20
(secure DNS — what's on public DNS is correspondingly
narrower), CP-2 (contingency — bundles carry the discovery
data, so loss of public DNS does not block credentialed
enrollment), and CM-7 (least functionality — the public DNS
surface is restricted to what is genuinely needed).

The compensating control for the larger blast radius of a
stolen bundle is the existing
[`../design/offline-enrollment.md`](../design/offline-enrollment.md)
§3.1 bundle-in-flight mitigations (14-day activation
deadline, delivery state tracking, daily auto-revoke for
no-shows).

## Trigger conditions for revisit

- A new high-volume enrollment use case appears that cannot
  feasibly be served via bundles (e.g. third-party
  partner devices we do not control). Either extend the
  bundle pipeline or reintroduce a scoped public endpoint
  for that use case only.
- A documented incident where the bundle pipeline failed to
  reach a forward site in time and break-glass was used —
  pattern of repeated break-glass events would force
  reconsidering the default.
- Detection-evasion work (deferred) lands and changes the
  shape of the public-edge story (e.g. moving to randomised
  decoy front doors).
- Public DNS provider changes (Cloudflare exit / pricing
  shift) — the absence of a public mgmt record means the
  exit story is much smaller, which is a positive
  consequence rather than a revisit.

---

*See `GLOSSARY.md` for terms and acronyms.*
