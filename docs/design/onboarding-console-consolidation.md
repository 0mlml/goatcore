# Consolidating onboarding into goat-id-console

**Status:** in progress (started 2026-07-08). Decision + slice plan below; first slice building now.

## Why

The three-stage delegated onboarding (ADR 1062) review UI was built as standalone static
pages bolted onto the **vendor portal** (`goat-zenoh-portal` `/admin/*.html`). That's the
wrong home: the vendor portal is the *vendor/data* surface, `/admin/` is a bare file
listing with no shell/nav, and there's no role-adaptive single pane. Meanwhile
**`goat-id-console`** (ADR 0860) already exists as the **identity-plane admin console** — a
server-rendered Go app with a nav shell, an `admin()` oauth2-proxy (`ADMINS`) gate, users /
invites / permissions / audit pages, and **its own isitup service** at
`admin.auth.<site>.<zone>`. That is the role-adaptive single pane onboarding belongs in.

## Decision

Move the onboarding **review + admin actions** into `goat-id-console`, rendered in its shell
with the identity+authorities card. The onboarding **store + mint stay in
`goat-zenoh-portal`** (the `onboard_proposals` table, `/api/onboard/*`, the runner) — the
console is the **UI**, calling the portal API **server-side** (it already holds credentials
and sits behind ADMINS; no CORS, no bearer in the browser). Two backends, one pane.

- The portal keeps being the onboarding *backend* (proposals, invite mint, the runner).
- The console gains onboarding *views* that call `/api/onboard/*` with a portal operator key.
- The standalone portal `/admin/onboard-*.html` pages are retired once the console covers them.

## Invite-flow reconciliation (the overlap)

`goat-id-console` has its own simpler `/invites` (Kratos-identity-only, preset groups). The
three-stage flow is the richer, multi-surface (mesh + login + fabric) mint. **Resolution:**
the three-stage flow is the onboarding mechanism; the console's onboarding pages drive it
(sponsor a candidate → review → admin approve). The legacy Kratos-only `/invites` becomes a
narrow "IdP-only quick add" or is retired — decided when the console onboarding views land.

## Slices

1. **Portal API client in the console** (`internal/portalapi/`): `ListProposals(status)`,
   `GetProposal(id)`, `Decide(id, action, note)` over the portal operator bearer. Config:
   `PORTAL_BASE_URL` + `PORTAL_OPERATOR_API_KEY`.
2. **Admin queue view** (`GET /onboarding` — the cross-stage find box carries its term in the
   URL fragment, which never reaches the server, goat#1306 — `GET /onboarding/{id}`, `POST …/approve|reject`):
   the sponsor_approved queue + detail with the identity card + Approve/Reject. **← first slice.**
3. **Sponsor-review** — folded into the one `/onboarding` page as the "Awaiting sponsor"
   stage tab (proposed proposals + sponsor-approve/reject). **← done (slice 1).**
7. **Sponsor a candidate (invite mint)** — `GET/POST /onboarding/invite` + `portalapi.MintInvite`:
   the sponsor mints a one-time link in the console (all-optional pre-bind), gets the public
   intake URL to hand over. **← done (slice 2).**
4. **Identity+authorities card as a shared asset** — reuse `identity-card.js` (embed the
   proposal JSON in the page, render client-side) so the console and portal render identically.
5. **Nav** entry ("Onboarding") in `layout.html`. **← done.** The legacy Kratos-only
   `/invites` nav entry is replaced by Onboarding when the portal is wired (one mechanism);
   the `/invites` route stays for IdP-only quick-add.
6. **Retire the portal `/admin/onboard-invite|sponsor-review|queue` pages** — they are now
   redirect stubs to the console (host derived `portal.*` → `admin.auth.*`); the `/admin`
   landing points at the console. **← done.** Direct-**intake** + **batch** stay on the portal
   for now (not yet ported); the public `/onboard.html` front desk stays by design.

## Not built

- **Email delivery of invites.** There is no SMTP/email service. An invite is a link the
  sponsor copies + hands over (or, once the notifications plan lands, a Matrix/Zulip ping —
  see `onboarding-notifications.md`). Emailing invites would be net-new: a small SMTP sender
  wired to the mint. Not started.

## Notes

- Attribution: sponsor = invite-mint actor, admin = approve actor — both real handles when
  minted/approved with per-person `PORTAL_OPERATOR_API_KEYS` keys (not the shared key).
- Runner: on admin-approve the deployed `onboard-runner` mints automatically.
- Deploy: rebuild `goat-id-console` image + the `id-admin-console` role.
