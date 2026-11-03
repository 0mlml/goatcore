# ADR 0411 — Ory (Kratos + Hydra) for human + service-account IdP

| Field | Value |
|---|---|
| Status | **Accepted (final, 2026-05-06)** — post-soak decision review closed PASS on the v7 2-node Spock-replicated nemesis soak (`../project/idp-decision-review.md` §6 v7 addendum). A9 (Kratos kill recovery via watcher) confirmed under load with 2 real kill+revive cycles, both <5s. A14 (Kratos flow A→B replication) closed at 97.56% green. A15 (Hydra token A→B) reaffirmed at 100%. Block 21C-production unblocked. |
| Date | 2026-04-25 (Accepted post-spike); 2026-05-06 (Accepted final post-soak) |
| Repo scope | goat core |
| Supersedes | — |
| Superseded by | — |
| Related | [`0410-go-cli-peer-enrollment.md`](0410-go-cli-peer-enrollment.md), `0412-spiffe-spire-workload-id.md`, [`1008-soak-testing-discipline.md`](1008-soak-testing-discipline.md), [`1009-ory-shared-network-nid-preseed.md`](1009-ory-shared-network-nid-preseed.md), [`0401-per-site-dex-oidc.md`](0401-per-site-dex-oidc.md) (superseded in part), [`0402-kanidm-for-accounts.md`](0402-kanidm-for-accounts.md) (superseded in part), [`0403-fido2-break-glass.md`](0403-fido2-break-glass.md), [`0206-island-mode-by-construction.md`](0206-island-mode-by-construction.md), `../design/identity-architecture.md`, `../project/idp-strategy-spike-2026-04-25.md`, `../project/idp-decision-review.md` |

## Soak verdict (2026-05-06) — final PASS

The post-soak decision-review hook (originally added 2026-04-25) is closed.

Evidence files:

- Single-node 2 h soak — `../reports/2026-04-25-ory-soak-evidence.jsonl` (PASS, 235 ticks all green)
- 2-node Spock nemesis soak v5/v6 — `../reports/2026-04-27-2node-spock-nemesis-evidence.jsonl` + `../reports/2026-04-28-2node-spock-nemesis-v6-evidence.jsonl` (surfaced F-013; resolved 2026-04-30)
- 2-node Spock nemesis soak **v7 (final)** — `../reports/2026-05-06-2node-spock-nemesis-v7-evidence.jsonl` + `../reports/2026-05-06-2node-spock-nemesis-v7-watcher-log.txt` (validates F-013 mitigation under load)

Verdict: **PASS final**. A9 (Kratos kill recovery via in-process `mgmt-host-compose-watch`) confirmed by two real kill+revive cycles during the v7 soak — recovery times 2 seconds and 3 seconds, well under the 15-second acceptance bar. A14 (Kratos flow A→B replication) closed at 97.56% green. A15 (Hydra token A→B) at 100%. A10 (webauthn browser test) deferred to Block 21D per principled scope (no UI shipped in IdP role).

Status flipped Accepted (subject to post-soak review) → **Accepted (final)**. Block 21C-production wire-up is unblocked. Production-side production-readiness items (Block 18D credential rotation, Block 17A.5 policy-engine OIDC consumer) are downstream of 21C-production and not part of this gate.

## Spike-pass addendum — 2026-04-25

Half-day hands-on bring-up spike (per "Hands-on bring-up spike (prereq)"
section below) completed 2026-04-25. Local docker-compose with
`oryd/kratos:v1.3.1` + `oryd/hydra:v2.3.0` + Postgres backend (separate
kratos + hydra logical DBs).

Tests run + verdict:

| Test | Result |
|---|---|
| Postgres schema split (Kratos owns `kratos`, Hydra owns `hydra`) | ✅ Clean separation; init script creates both DBs cleanly |
| `POST /admin/identities` to Kratos (create admin user with email + password) | ✅ Returns identity with verifiable_address + recovery_address auto-populated |
| `POST /admin/clients` to Hydra (create OAuth2 client for client_credentials) | ✅ Returns client_id + client_secret + registration_access_token |
| `POST /oauth2/token` with client_credentials grant | ✅ Issues access token + scope + expires_in |
| `POST /admin/oauth2/introspect` to validate token | ✅ Returns active=true, sub, iss, exp, scope |
| `GET /.well-known/jwks.json` | 🟡 Empty by default — Hydra ships opaque tokens (`ory_at_...`); flip `oauth2.access_token_strategy: jwt` for signed JWTs (known config flag, trivial) |

**Operational signal:** Configs matched runtime behavior. Error messages
were specific and pointed at line numbers. No "docs say X but runtime
does Y" surprises. No event-sourced model debug tax. The spike-disqualifier
risk that Zitadel had did not materialize for Ory.

**Status flipped Proposed → Accepted.** The remaining "intentionally
deferred" components (Keto for Zanzibar authz, Oathkeeper for policy-
aware proxy) stay deferred per the original Phase 1 / Phase 3+ split.

**Decision artifacts from spike:**

- Local compose stack lives at `/tmp/ory-spike/` for re-creation
  (one shot — not committed to repo, used as proving ground only).
- Production deployment will use the same image versions as the
  spike (Kratos v1.3.1, Hydra v2.3.0) until a deliberate version
  bump.
- Token strategy: start with default opaque tokens + introspect
  endpoint; switch to JWT + JWKS once a service needs offline
  validation (likely Block 17A.5 policy engine for performance).

## TL;DR (for a layperson)

Several services in our system need a way for people (and automated
processes acting on their behalf) to log in. Rather than rely on a
single central provider that could go down or be controlled by a
third party, every site runs its own copy of an open-source identity
product called Ory. Ory is two small Go programs — Kratos handles
who-you-are (login, password reset, multi-factor) and Hydra hands out
the standard tokens services accept (OIDC). Both store their data in
the same Postgres database we already operate. Picking Ory keeps
our identity layer sovereign, modular, and open-source.

## Context

Identity-strategy spike on 2026-04-25
(`../project/idp-strategy-spike-2026-04-25.md`)
evaluated Ory, Zitadel, Keycloak, Authentik, Pocketid, standalone
Dex+Kanidm, and Kanidm-alone as candidate IdP products for **humans
and service accounts** (machine identity stays WireGuard per ADR
0410's separation of concerns; workload identity goes SPIFFE in
Phase 3+ per ADR 0412).

Constraints:

- Sovereign, per-site, island-mode-compliant (ADR 0206).
- Postgres-backed where possible (fits existing Spock + pgBackRest
  storage discipline; avoids per-product backend forks).
- Engineer-friendly, FIPS-buildable, scales to ~12 services growing
  to ~50.
- Phase 3+ growth path through Zanzibar authz (Keto/OpenFGA) and
  upstream federation (DoD IdP) without rebuild.

Operator hands-on signal: Zitadel has been recalcitrant in practice
for the team (event-sourced model debug tax,
docs-vs-reality gaps, recalcitrant customization). Ory's modular
architecture is more conventional, easier to operate, and matches
our Go-first toolchain.

## Decision

For human and service-account identity, we deploy **Ory Kratos + Ory
Hydra** per-site on the mgmt-host pair, Postgres-backed, behind
keepalived VIP active-passive.

**Components in MVP (Phase 1):**

- **Ory Kratos** — identity management. Sign-up, login, password
  reset, MFA (FIDO2/passkeys), session management. Stores
  identities + sessions in Postgres (own logical DB).
- **Ory Hydra** — OAuth2 / OIDC server. Issues JWTs to services.
  Stores clients + grants in Postgres (own logical DB).

**Components deferred (Phase 3+):**

- **Ory Keto** — Zanzibar-style relationship-based authz. Add when
  RBAC role-spaghetti hits the trigger condition documented in the
  spike doc.
- **Ory Oathkeeper** — policy-aware reverse proxy. Add when service
  count makes self-validation noisy.

**Per-site deployment:**

- One Kratos + one Hydra per site, in compose stack on the mgmt-host
  pair, active-passive behind keepalived VIP (same pattern as
  postgres-spock + signal-server).
- Postgres backend in the existing postgres-spock cluster (own
  logical databases). Spock-replicated within the site.
- No cross-site identity replication — each site's admin set may
  differ. Per-site sovereign per ADR 0401's island-mode property
  (which this ADR preserves).

**FIPS posture:** Ory Go binaries built with FIPS-validated Go
toolchain (BoringCrypto). Container image source: Chainguard where
available, build-from-source pinned-digest where not.

**Hands-on bring-up spike (prereq):** Half-day local docker-compose
bring-up of Kratos + Hydra. Create a user, get a JWT, validate
against a fake protected endpoint. Check docs vs reality. **Must
complete before week-1 production deployment commits.** Tracked in
roadmap.

**netbird's relationship to Ory:** netbird's IdP requirement was an
artifact of dashboard-mediated admin flow. With ADR 0410's Go CLI
absorbing peer enrollment, netbird-mgmt configures
`IdpManagerConfig.ManagerType = "none"` and is IdP-free at the API
layer. The netbird **dashboard** can later be wired to Ory in Phase
2 if we want the UI; dashboard-less ops is also viable for our scale.

## Alternatives considered

- **Zitadel.** Single binary, Postgres-backed, multi-tenant, modern.
  Rejected based on operator hands-on experience: event-sourced
  model debug tax, docs-vs-reality gaps. Re-open if Ory hands-on
  bring-up reveals analogous problems.
- **Keycloak.** Most battle-tested. Rejected for JVM operational
  weight (500+ MB minimum, Java ops surface) — not aligned with
  Go-first stack.
- **Authentik.** Python/Django. Rejected for operational toolchain
  mismatch (we don't otherwise carry Python ops surface in the
  mgmt-plane).
- **Pocketid.** Intentionally minimal. Rejected as outgrows our
  scope quickly (a dozen services growing to fifty, m2m flows,
  future Zanzibar).
- **Standalone Dex + Kanidm.** ADR 0401's original direction.
  Rejected because Dex is a thin OIDC layer; at this scope you
  end up rebuilding the Ory stack piecewise (Dex + Keto + an
  IdP-abstract user store). Better to start with Ory.
- **Kanidm alone.** Excellent for sovereign user identity + FIDO2.
  Rejected as the public-facing IdP for a dozen services: its OIDC
  story is intentionally scoped, not built for that role.

## Consequences

**Positive.** Identity for humans + service accounts is sovereign,
per-site, island-mode-compliant, Postgres-backed (in our existing
storage discipline). Modular: Keto and Oathkeeper add later without
rebuild. Two Go binaries with strong API surfaces, clean docs, and
engineer-friendly config. Phase 3+ Zanzibar migration is well-trodden.
Federation upstream (a partner IdP) is an OIDC connector config when the
trigger fires.

**Negative.** Two services to deploy in week 1 instead of one (Kratos
+ Hydra, vs single-binary Zitadel). Half-day spike risk: the hands-on
bring-up may surface unanticipated problems that re-open the IdP-
product question. We pin to Ory's release cadence + their breaking-
change policy. Service-account discipline (rotation, scoping, audit)
is real ongoing operational work.

## Assessor view (SCA-R)

Ory satisfies IA-2 (identification and authentication of users), AC-2
(account management), AU-2/AU-3 (audit events for identity operations,
piped to Block 17D integrity tier per
`../design/identity-architecture.md` §7).
Per-site deployment satisfies CP-2 (contingency: local authentication
under partition). The decision is consistent with NIST SP 800-63B
authenticator-assurance levels (Ory supports AAL2 with FIDO2; AAL3
when paired with hardware-backed authenticators). FIPS 140-3 posture:
Ory Go binaries built with BoringCrypto are CMVP-validated for the
crypto operations performed; assessor-visible via container image
attestation. The Phase 1 RBAC model is a known constraint, with the
Phase 3+ Keto migration documented as a controlled evolution path.
Service-account rotation discipline is carried by Block 18D;
shorter default token TTLs bound exposure between rotations.

## Trigger conditions for revisit

- Ory hands-on bring-up spike surfaces docs-vs-reality gaps similar
  to those that disqualified Zitadel — re-open product evaluation
  with Authentik, Keycloak, or Kanidm-alone as candidates.
- A new IdP enters the open-source landscape with materially better
  posture for our scope (Go-native, Postgres-backed, modular,
  sovereign-friendly).
- DoD partner federation requires a specific upstream IdP that Ory
  cannot federate to — verify connector availability; pivot only
  if Ory genuinely cannot federate.
- Service count exceeds 50 OR admin count exceeds 50 — re-examine
  whether Ory's modular architecture still scales or whether
  Keycloak / commercial Ory Cloud becomes more right.

---

*See `GLOSSARY.md` for terms and acronyms.*
