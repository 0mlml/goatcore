# ADR 1072 — Sponsor-scoped onboarding console access

**Status:** Proposed (built in code on the extracted console-owned onboarding engine; ZAGROS deploy pending — portal + console image bumps + oauth2-proxy group broadening).
**Related:** [ADR 1071](1071-sponsorship-authorization-at-onboarding.md) (who may sponsor — this ADR decides what a sponsor *sees*) · [ADR 1062](1062-three-stage-delegated-candidate-onboarding.md) (the three-stage flow) · [ADR 0860](0860-id-admin-console-separate-tenant.md) (the console shell) · [`onboarding-extraction-plan.md`](../design/onboarding-extraction-plan.md) (the console owns the onboarding engine; the portal registry stays the sponsorship SoT) · F-207 (per-person attribution).

## Plain-language summary

ADR 1071 made "who may bring people in" an explicit per-identity grant. But the onboarding surfaces were still all-or-nothing: the console's `/onboarding` page was ADMINS-only, and on the machine realm any sponsorship-authorized participant key could read the whole queue — and even reach the admin-stage actions. This ADR gives non-admin sponsors a real seat with honest walls: a developer- or browse-capped sponsor can mint invites and see **only their own invitees**, vouch only for them within their tier cap, and never touch the admin decisions. Admin-tier-capped sponsors (iam-admin / platform-admin), ADMINS sessions, and operator keys keep the full view.

## Decision

**Non-escalated sponsors get an own-invitee-scoped onboarding surface on both realms of the console-owned engine; the portal registry stays the sponsorship SoT, read via two read-only resolves.**

- **The login binding is part of the grant.** The ADR 1071 annotation gains an optional `sponsor_email` — the sponsor's Ory login (`goat admin sponsorship <id> --max-tier <t> --email <login>`, the manifest `sponsorship.email`, the lever's `email` field; the admin-tier intake presets bind it automatically via a new `@email` preset token since the candidate flow onboards a person). Cleared on revoke. No binding → no console sponsor mode; CLI/API sponsorship with the sponsor's own portal key is unaffected.
- **Two read-only portal resolves, one trust pair.** The console already resolves participant keys via the portal's `GET /api/whoami`; that response now also carries the identity's `alias` and bound `sponsor_email`. For human sessions the console asks the new `GET /api/orgs/sponsorship?email=` — gated by an operator bearer or the console↔portal service secret (`PORTAL_CONSOLE_LOOKUP_TOKEN`, defaulting to the stage-notify shared token: same pair, same blast radius, zero new secrets). Both resolves are cached briefly (60 s) and fail closed — portal-down means no sponsor mode, never a silently widened one.
- **Scoping runs in the console against its own store.** A row's recorded `sponsor` is matched against the sponsor's `{anchor prefix, slot alias, login email}` — the three forms mints record across realms. Non-escalated sponsors: invite list/get/revoke and proposal list/get filter to own rows, with foreign ids reading 404 (existence-hiding); stage-2 vouch additionally requires the proposal to carry their sponsorship *and* sit within their tier cap; human mints stamp their cap onto the invite (`prebound.sponsor_max_tier`) instead of the ADMINS-unrestricted `""`.
- **Admin-stage actions are operator/ADMINS-only — now enforced.** The machine realm's `approve`/`reject`/`onboarded`/`claim`/`result` gain an operator-principal guard (previously any sponsorship-authorized participant key could reach them); the human realm refuses admin actions from sponsor sessions server-side, and the UI hides them.
- **Escalated caps lift the visibility wall.** A sponsor capped at `iam-admin`/`platform-admin` sees the full queue (their tier caps still bind); the own-invitee restriction is for the non-escalated tiers.
- **Page access broadens by config, not code — to authenticated-only, not a group list.** oauth2-proxy keeps `id_admin_console_oauth2_proxy_allowed_groups` (default `ADMINS`, fail-closed). A goatnet enabling sponsor access sets it EMPTY (any authenticated Ory login passes the front door) because group-list broadening cannot work today: the live Ory `groups` vocabulary is `ADMINS`/`ALL-ADMIN`/`COMPLIANCE` and partner identities carry no Ory groups at all — NetBird `auto_groups` names (`DEVELOPER`/`OBSERVER`) are a different namespace (live-fire lesson, ZAGROS 2026-07-09). The console's `onboardGate` (portal-resolved sponsorship, fail-closed) carries the real authorization, and every non-onboarding console page stays behind `adminGate` regardless. A first-class `SPONSOR` Ory group assigned with the grant is the recorded finer-grained follow-up.

## Alternatives considered

- **Scope portal-side.** The onboarding engine moved to the console (extraction plan); the portal's onboarding handlers are inert-after-cutover and slated for the strip — scoping them would be dead code on arrival.
- **An Ory `SPONSORS` group as the authorization.** Re-opens the ADR 1071 rejection: the group would drift from the registry annotation that actually gates mint/vouch; groups here only open the front door.
- **A console-local copy of the sponsorship annotation.** Forks the SoT the extraction deliberately kept in the portal; the read-only resolve is the recorded residual coupling.
- **A per-sponsor console credential for human sessions.** A second credential to store/rotate/revoke; the Ory session plus the email binding achieves the same identity with none of the lifecycle.

## Consequences

- **+** Delegation is now real end-to-end: a partner sponsor runs their own cohort's intake from a browser or their own portal key, seeing exactly their slice, attributed per-person (human mints record their login; machine mints record their anchor) — and the machine realm's participant keys can no longer read the full queue or reach admin actions.
- **+** Fail-closed everywhere new: unbound login, ambiguous binding (two annotations on one email → 409, never a guess), portal-down, unknown tier, foreign row — all refuse.
- **−** Sponsorship revocation takes up to 60 s to bite on an open console session (the resolve cache); the next store-mutating action still re-checks scope inside the request, so the lingering window is the page shell, not authority.
- **−** A sponsor whose Ory login differs from the bound email resolves to nothing — operator fixes by re-granting with the right email.
- **−** Sponsor mode requires the console↔portal pairing (`PORTAL_INTERNAL_URL` + the service secret) to be configured; without it non-admin sessions are simply refused — the pre-1072 posture, not an outage.
