# ADR 1176 — A help desk on a goatnet has an inside door and an outside desk, handed out as two QR codes

- **Status:** **Proposed.** Nothing changes in a live goatnet on merge. Design: `help-desk-service-pattern.md`; phases and gates: `mobile-and-help-desk-plan.md`.
- **Date:** 2026-09-10
- **Deciders:** operator
- **Evidence:** a partner hosts HESK (FAQ, triage, tickets) on a peer inside a goatnet and hands out QR codes to reach it; the mesh zone has no public records by design (`security-architecture.md:146`); phones do not receive split DNS (ADR 1175); the relay-health dashboard's mesh-only deployment was retired because a diagnostic surface that fails with the substrate is self-defeating (`relay-health-dashboard.md:206-212`); no help desk, ticket or FAQ surface exists anywhere in the repo.
- **Related:** [ADR 1163](1163-per-visit-attribution-for-fenced-surfaces.md) · [ADR 1072](1072-sponsor-scoped-onboarding-console.md) · [ADR 1085](1085-relay-ingress-dispatch.md) · [ADR 1129](1129-warrant-scoped-expiring-network-authorizations.md) · [ADR 1175](1175-handsets-join-on-the-stock-netbird-app-and-a-handset-key.md) · track `netbird-groups-governance` (PR #991)

## Decision

1. **Two addresses, two QR codes.** The inside door is a mesh name, `help.<site>.<zone>`; the outside desk is a public address off the goatnet's substrate. A printed card carries both with one line saying which to scan. They are never the same application exposed twice.
2. **The inside door is an authenticated `oauth2-proxy-surface` door** with empty allowed-groups, registered in `isitup_services` and `ops/permissions/group-permissions.yml`, deployed by `authenticated-doors.yml`. Identity reaches the application only as `X-Forwarded-User`, `-Email` and `-Groups`; every visit is a `SurfaceVisitV1` record (ADR 1163). Answers link into the goatnet's own tools.
3. **A partner-hosted upstream is a machine on `EXTERNAL-INFRA`**, minted on a machine manifest, never a person's key; its firewall accepts the application port from the management host's mesh IP only, so the door is the only path. Which site scope the peer holds is PR #991's decision and does not change the pattern.
4. **The outside desk takes only "cannot get on" and "cannot log in" tickets**, anonymously, and lives on the public internet: the partner's own public instance for the first instance (recorded as `help_desk_public_url`), the relay-health cloud box for one we host. Never on the relays' ingress dispatch, which terminates on the management host.
5. **The card is generated from the registry** by `ops/scripts/help-desk-qr.py`, which refuses to run without both addresses.
6. **The pattern is the deliverable.** The first instance is HESK hosted by a partner; the second is ours, and the choice of application is made then.

## Alternatives

- **One public name that behaves differently on the mesh** (the intake advisory shape). Needs split DNS the phones do not get, and one QR cannot say which side the reader is on.
- **Expose the mesh instance publicly through relay ingress as the outside desk.** Fails with the management host it would be diagnosing, and puts every inside ticket behind a public front.
- **A mesh-only door with no identity.** No requester, no visit record, no deep links; ADR 1163 records what that produced for an earlier mesh-only surface.
- **Identity from the mesh source IP** (`GET /api/access/peer`, goat-client `WhoIs`). No web surface reads it today, a machine resolves to itself rather than its custodian, and the door already delivers the person.
- **Build our own help desk first and skip the partner instance.** The partner's is ready and ours is not; the pattern is written so the second does not restart from zero.
