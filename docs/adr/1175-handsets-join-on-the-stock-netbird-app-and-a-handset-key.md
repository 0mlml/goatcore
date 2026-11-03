# ADR 1175 — Phones and tablets join a goatnet on the stock NetBird app with a per-person handset key

- **Status:** **Proposed.** Nothing changes in a live goatnet on merge. Design: [`mobile-device-onboarding.md`](../design/mobile-device-onboarding.md); phases and gates: `mobile-and-help-desk-plan.md`.
- **Date:** 2026-09-10
- **Deciders:** operator
- **Evidence:** the ZAGROS membership table in `docs/project/netbird-groups-governance-plan.md` (PR #991): handsets on the goatnet joined on keys minted by hand in the dashboard; the live welcome guide (`ops/goat-id-console/internal/welcome/welcome.go:690-724`) has no phone section; the only written phone procedure sits in the retired enroll leg (`ops/ansible/templates/operator-onboard/bundle.md.j2:40-50`); the pushed nameserver group is match-domain only (`ops/ansible/goat-coredns.yml:250-255`), which NetBird documents as unsupported on iOS and Android.
- **Related:** [ADR 1062](1062-three-stage-delegated-candidate-onboarding.md) · [ADR 1096](1096-delegated-machine-enrollment.md) · [ADR 1083](1083-goat-client-single-client-and-distribution.md) · [ADR 1163](1163-per-visit-attribution-for-fenced-surfaces.md) · [ADR 1176](1176-help-desk-inside-door-outside-desk-two-qr-codes.md) · F-109 · F-338 · track `netbird-groups-governance`

## Decision

1. **A phone or tablet is a person device on a person role.** It is another peer under the person's identity, OBSERVER unless the sponsor raises it to DEVELOPER at approval. It is never a machine principal, never holds an infra tier, a node class or P2P.
2. **The client is the stock NetBird app** from the App Store or Google Play, pointed at the goatnet's public management name with a setup key. goat-client mobile replaces it when it ships and changes only the install step.
3. **Each person gets a handset key beside the device key.** The runner mints it with `<GOATNET>-OBSERVER` only, reuse count `netbird.handset_limit` (default 1, 0–5, asked on the intake form as capacity, not authority), the same TTL, and the same `auto_peer_name` prefix, so the phone is named `peer-<handle>-<site>-<date>-<device>` and resolves to the person like any other peer. Both keys revoke together.
4. **The welcome guide gains a phone section** generated from the manifest, with the handset key, the management URL, the one-VPN-at-a-time and Android Private DNS notes, and a plain list of what a phone can and cannot do. The guide's browser e2e asserts it.
5. **Mesh-name resolution on handsets is measured before anything is built for it.** If a phone on ZAGROS cannot open `guide.zagros.netbird.zagros.example`, each goatnet gets a second nameserver group, `<site>-handset-dns`, primary with no match domains, distributed to a `<GOATNET>-HANDSET` group that appears in no ACL rule and is declared in the taxonomy with the `netbird-groups-governance` track. Desktops keep the match-domain group.

## Alternatives

- **goat-client mobile now.** Source-complete, never released, blocked on store signing (`goat-client.md:11,258`); cannot coexist with the stock app on one device. It stays the successor, not the path.
- **One key per person, phones included.** Puts a handset in whatever substrate groups the person holds; that is the shape panoscope's D5 check alarms on and PR #991 is walking back by hand.
- **The public-mTLS crutch tier for phones** (ADR 0843). Reaches services without a tunnel through a per-device certificate ceremony; code-only, zero live resources; solves reach, not membership.
- **Make the existing nameserver group primary for all peers.** Every laptop's DNS would cross the mesh and vanish with it; the name-fallback resolver exists to prevent exactly that.
- **QR or deep-link enrolment on the phone.** A convenience over copying a key from the email opened on the phone; deferred to goat-client's bundle QR, where it is already designed.
