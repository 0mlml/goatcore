# ADR 0860 — id-admin-console as a separate admin tenant

| Field | Value |
|---|---|
| Status | Proposed |
| Date | 2026-05-20 |
| Repo scope | goat core |
| Supersedes | — |
| Superseded by | — |
| Related | ADR 0411 (Ory IdP MVP), ADR 0842 (operator IdP), ADR 0857 (patched consent UI / groups claim), ADR 0811 (audit log integrity), ADR 0844 (Zenoh ACL v1), ADR 1009/1010 (Ory on Spock) |

## TL;DR (for a layperson)

We need an admin web page where a platform operator can see who has access to which services, change a user's group, generate a one-time invite link for a new vendor, and read an audit trail of who changed what. We are building this as a small, dedicated Go service at its own URL (`admin.auth.zagros.example`) rather than bolting it onto the existing vendor-facing portal or the network-mesh dashboard. The reason is the same reason a bank does not let tellers and auditors share a screen: an operator's admin powers are a superset of a vendor's reach, and putting both tenants in the same web app blurs the trust boundary in a way that gets people locked out of the wrong system at the wrong time. The cost is one more small service to run; the benefit is that every mutation is auditable, every admin action runs through a deliberately separate fence, and the IdP plane has a single front door for human operators.

## Context

The platform runs two separate Ory Kratos instances on the management host by design ([ADR 0842](0842-dedicated-operator-idp-for-observability.md)):

- **Tenant IdP Kratos** (port 4434) — operators + vendors who log into site services (netbird-dashboard, portal, panoscope, isitup, brief, dataflows, demo apps). Issues JWTs via site Hydra.
- **Operator IdP Kratos** (port 4436) — platform operators only; gates observability surfaces (grafana, future openobserve/minio console). Issues JWTs via operator Hydra.

There is no admin UI shipped with Kratos. Identity mutations today happen via:

- `ops/scripts/goat-user` (YAML manifest editor) → ansible orchestrator → `POST /admin/identities`
- `ops/scripts/operator-idp-enroll.sh` (recovery-link mint for operator IdP)
- `ops/ansible/operator-onboard.yml` (Phase 2 site Kratos identity provisioning)

These work but require shell access + repo checkout + ansible apply. There is no read-side at all: to see a user's groups today, you `ssh` to the management host and `curl 127.0.0.1:4434/admin/identities`. There is no audit log of who changed which user's groups when (mutations are SSH-shell history, not durable).

F-132 is partially closed at the L7 fence layer (the L7 admin-fence implementation) — group→service permissions live in a markdown table that the L7 sidecars enforce, but there is no machine-readable canonical source. The markdown can drift from the sidecar configs.

[`goat-zenoh-portal`](../../ops/goat-zenoh-portal/) was considered as a host for this functionality. It was rejected: the portal's tenant is vendors (external integrators registering Zenoh topics, schemas, and certs). Mixing platform-admin functionality into a vendor surface puts vendor traffic and operator-mutation traffic on the same trust boundary. Operators have a strictly larger blast radius — they can create, demote, and lock out identities including their own peers and other operators. That superset relationship must be visible structurally, not just procedurally.

## Decision

We add a new Go service, `goat-id-console`, at hostname `admin.auth.zagros.example` (mesh-only via Traefik, same DNS pattern as the existing `auth.zagros.*` Kratos selfservice UI). Code at `ops/goat-id-console/`.

**Tenant separation.** The console is a third tenant in the identity plane, distinct from:
- `auth.zagros.*` — end-user self-service (login / registration / recovery / consent) — public-facing, any-authenticated.
- `dashboard.zagros.*` — netbird mesh admin (peers / groups / setup keys) — `ADMINS`-only.
- `portal.zagros.*` — vendor surface (Zenoh topics / schemas / certs) — any-authenticated (vendors).
- `admin.auth.zagros.*` — **new** — IdP-plane admin (users / groups / invites / audit) — `ADMINS`-only with step-up AAL2 on sensitive mutations.

**Two-IdP awareness.** The console manages BOTH Kratos instances. A selector at the top of every page (`Manage: [Tenant IdP ▾]`) picks the upstream. All Kratos and Hydra calls thread the selected IdP through the request context. This is the honest model: operator scope is a superset of tenant scope (an operator-account compromise can lock out tenant access; the reverse is not true), and the two identity DBs are separate stores by design.

**Server-rendered, no browser-to-admin-API.** Go HTTP handlers (`net/http` ServeMux, same shape as `goat-zenoh-portal`). The browser only talks to the console; the console holds the Kratos and Hydra admin credentials. Admin APIs (port 4434/4436) never reach the browser. Pages use Go `html/template`; no SPA.

**Auth.** An `oauth2-proxy` sidecar with `--allowed-group=ADMINS` gates every console URL (same pattern as `oauth2-proxy-dashboard` from Block 83C.2-L7). The console additionally re-checks the forwarded `X-Auth-Request-Groups` header in-handler before every action — defense in depth against sidecar mis-config. Sensitive mutations (group escalation to ADMINS, last-admin demotion, session revocation against another admin, delete-user) require step-up AAL2 — the handler redirects the operator through a fresh Kratos AAL2 login flow before completing the action.

**State of record stays in Kratos.** The console does not duplicate identity data. The only console-owned state is:
- `invites` — single-use, TTL-bound, role-preset invite tokens. v1 in-memory; v2 a Postgres schema on the existing per-site Postgres.
- `audit_events` — append-only local mirror of every mutation. v1 in-memory ring buffer. v2 a Postgres table.

**Audit fan-out.** Every mutation publishes a structured event on Zenoh `system/audit/id-console/<event-type>/v1`. The existing Block 82S audit-subscriber routes it into the MinIO `audit-integrity` bucket. No new audit substrate. v1 ships with a stderr emitter and a `TODO` for the Zenoh publisher; the wire shape is locked in this ADR so the upgrade is a swap of the implementation behind the `AuditEmitter` interface.

**Canonical permissions matrix.** Promote the group→service permission table currently in the L7 admin-fence implementation doc into machine-readable YAML at `ops/permissions/group-permissions.yml`. The markdown survey table now cites the YAML as source of truth. The console renders the YAML verbatim into `/permissions`. L7 sidecars continue to be configured via Ansible variables, but those variables and the markdown survey will both reference the YAML; a follow-on can wire the L7 sidecar templates to read it directly.

**Lockout protection.** The console refuses any operation that would leave the selected IdP with zero identities holding the `ADMINS` group. The check is on every group-PATCH, every identity-delete, and every session-revoke that targets the last ADMINS-bearing identity.

**Role-preset invite links.** A small operator-facing flow:
1. Operator clicks "Generate invite for role=VENDOR (TTL 24h)".
2. Console mints a random URL-safe 32-byte token, stores `{token, preset_groups, idp_key, ttl, created_by, created_at, used_at=null, revoked_at=null}` in the `invites` table, and returns the URL `admin.auth.zagros.*/invites/<token>/accept`.
3. New user opens the URL, fills `{name, email}` (single form, no auth required at this step — the token IS the auth).
4. Console creates the Kratos identity with `traits.groups = preset_groups`, mints a Kratos recovery link, displays it once for the operator to deliver out-of-band, and marks the invite `used_at`.

This is a deliberate wedge: Kratos does not natively bind traits to a registration code. The console owns the trait-binding step. The token's TTL plus single-use enforcement plus audit-event-on-use plus operator step-up to mint the token are the four controls that make this acceptable. Documented as such; not papered over.

**Out of scope for v1.** OAuth2 client management (Hydra clients), schema management (Kratos identity-schema), SPIRE workload-id (Block 21 Phase 3+), policy authoring (Block 17A.5). Each is a follow-on; the v1 surface stays narrow.

## Alternatives considered

**Extend `goat-zenoh-portal`** — rejected for tenant-separation reasons (above). Operator capability would inherit the vendor surface's any-authenticated baseline, requiring a parallel admin gate inside the same binary. The trust-boundary muddling is worse than running a second small service.

**Fork or patch `kratos-selfservice-ui-node`** — rejected. That's already a patched Node surface ([ADR 0857](0857-patched-selfservice-ui-for-groups-claim.md)). Adding admin pages there compounds an existing patch-tax and binds the admin UI to Ory's upstream selfservice cadence. Language boundary also points away ([ADR 0112](0112-tech-strategy-workload-shape-language-boundary.md)) — per-request control-plane work is Go.

**Adopt Keycloak Admin Console wholesale** — rejected. Would require running Keycloak in addition to Ory; Keycloak's strong-points (realm tooling, role/permission model) overlap with what we have, and its weak-points (schema migration discipline, JVM ops) cost us. Ory's deliberate position is "we ship admin APIs, you build the console"; we are on that path.

**Adopt Zitadel (Go, bundled console + IdP)** — rejected at this stage. Zitadel is an excellent comparable and we borrow its audit-event-store discipline. Switching identity platforms is a much larger decision than the gap this ADR closes; the gap is "we need an admin UI for the Ory we already run."

**Hosted Ory Console (Ory Network)** — rejected. SaaS; offline-mode and air-gap targets ([ADR 0813](0813-service-registry-pattern.md) family) preclude.

**No console; admins SSH to the management host and run `curl`** — current state. Rejected because it has no audit trail, no lockout protection, no machine-readable permission matrix, and no role-preset invite path for new-user onboarding. Adequate for one operator; breaks down for a second.

## Consequences

**Positive.**
- Single admin surface for the IdP plane; no more SSH-then-curl for read or write.
- Every mutation is auditable (append-only event store) and lockout-protected (last-ADMIN guard).
- F-132 single-source-of-truth: `ops/permissions/group-permissions.yml` is the canonical group→service matrix; the existing L7 fence survey markdown cites it.
- Role-preset invite links remove a friction step in vendor onboarding (today: edit YAML, run ansible, deliver recovery link by chat).
- Composes structurally with the existing oauth2-proxy fence pattern (no new auth substrate) and the Block 82S audit subscriber (no new audit substrate).
- Tenant separation matches the existing ADR 0842 boundary — visible in the URL, not just procedural.

**Negative.**
- One more service to run, monitor, and roll back. Mitigated by reusing the existing oauth2-proxy + Traefik + deploy-history substrate.
- The role-preset invite link is a console-owned wedge for a Kratos primitive that doesn't exist. If Ory ships native trait-bound registration codes, this becomes legacy code to remove.
- Step-up AAL2 redirects the operator through a fresh login flow. Friction is intentional; operators may push back. The threshold (group escalation to ADMINS, last-admin operations, delete-user, revoke-sessions-of-admin) is calibrated to lockout severity, not all mutations.
- v1 audit emitter is stderr-only with TODO for Zenoh. Until that's wired, audit visibility is local-log-only — adequate for trial scope, NOT adequate for production. Tracked as a v1.5 gate.
- The console reads `X-Auth-Request-Groups` from the sidecar header. If oauth2-proxy is misconfigured to allow non-ADMINS through, the console's in-handler check is the last line. Defense in depth, but not a hard cryptographic gate. (Mitigation: the JWT itself is signature-verifiable; a hardening pass should add JWT validation inside the console as a second check, deferred to v1.1.)
- We accept the residual risk that an admin with valid credentials and a compromised browser can perform mutations within the audit window. Step-up AAL2 reduces but does not eliminate this.

## Assessor view (SCA-R)

Maps to NIST SP 800-53 AC-2 (account management), AC-3 (access enforcement), AC-6 (least privilege), AU-2 (event logging), AU-12 (audit record generation), IA-2 (identification and authentication, organizational users), and IA-5 (authenticator management).

**Compensating controls.** The console is mesh-internal only (Traefik + mesh DNS; no public ingress). The oauth2-proxy sidecar gates on `ADMINS` group claim; the console additionally re-checks in-handler. Mutations emit audit events to the Block 82S subscriber (COMPLIANCE 1y retention per ADR 0811 in production; 7d rolling on a trial goatnet). Step-up AAL2 raises authenticator assurance for the highest-blast operations. The last-ADMIN guard prevents accidental account-management lockout (AC-2(11)).

**Residual risk.** v1 stderr audit emitter is not durable. Track as a precondition for moving the console beyond trial scope.

**Accreditation pathway.** Consistent with a cATO posture: every mutation produces a tamper-evident audit record, role-based access is enforced at L7 plus in-handler, and the console adds no new public surface.

## Trigger conditions for revisit

- Ory ships a first-party admin console with feature parity to v1 of this service. (Today: no roadmap commitment from Ory.)
- The console's audit-event store moves to durable storage and the assumption of "stderr is fine for trial" no longer applies.
- A second platform adopts this console and the Two-IdP selector pattern needs to extend to N-IdP — re-examine whether IdP-key threading through context still scales.
- Step-up AAL2 friction is empirically demonstrated to be ignored or worked around by operators; revisit threshold calibration.
- A formal policy-engine (Block 17A.5) lands; revisit whether group→service permissions should move out of `ops/permissions/group-permissions.yml` and into the policy engine.

## Amendment (2026-08-04) — step-up AAL2 completed; the signal is Kratos, not an `acr` claim

**Status:** accepted. Implements issue #174 (which absorbed #175). Implementation:
`ops/goat-id-console/internal/handlers/stepup.go`; design narrative:
`docs/design/id-admin-console.md` §"Auth + step-up".

### What this ADR got wrong

The original decision specified that the operator's assurance level would reach the
console as an **`acr` claim**, minted by Hydra and mapped to a request header by the
oauth2-proxy sidecar. **That mechanism does not exist on this stack**, verified against
the pinned upstreams:

1. **Kratos never sets `acr`.** Kratos is itself the OAuth2 login provider
   (`oauth2_provider.url: http://hydra:4445`), so Kratos — not the selfservice-ui — calls
   Hydra's `acceptOAuth2LoginRequest`. In kratos v1.3.1 `hydra/hydra.go:AcceptLoginRequest`
   populates `Amr` and leaves `Acr` unset. No Hydra configuration can produce a claim
   nothing upstream supplies.
2. **oauth2-proxy cannot map an arbitrary claim to a header.** Its `claim:` lookup resolves
   through `SessionState.GetClaim`, a closed switch in v7.6.0 that has no arm for `acr`,
   `amr`, or `auth_time`; unmatched claims yield an empty value and no header is emitted.

Because the header was never populated, `requireAAL2` was **unsatisfiable in production**:
every one of the gated actions refused every admin, on every goatnet running the
fail-closed default. That defect was tracked as #175 and mitigated — not fixed — by the
`STEP_UP_ENFORCED` policy knob, which made the control enforce nothing rather than break
operations.

### The correction

The AAL is now read **live from the Kratos admin API**: the console resolves the actor's
session `authentication_methods[]` ledger, each entry stamped with the `aal` it satisfied
and its `completed_at`.

This is better than the mechanism this ADR originally specified, on three counts:

- **Unforgeable by construction.** It is a server-to-server admin-API read, so the
  header-trust question this ADR worried about (and F-244 later sharpened) does not arise
  at all — it is sidestepped, not re-litigated.
- **Live, not a login snapshot.** An OIDC claim reflects the moment of login, but the
  oauth2-proxy session lives 8h and refreshes hourly — so a claim-based gate would still
  refuse an operator who had *just* stepped up. A step-up gate fundamentally requires a
  live signal.
- **Genuinely fresh.** `completed_at` gives the per-factor timestamp this ADR's "AAL2
  within the last 5 minutes" requirement always needed but had no way to obtain.

Everything else this ADR decided stands: the threshold list remains in
`ops/permissions/group-permissions.yml`, the friction is still deliberate and still
calibrated to lockout severity rather than applied to all mutations, and the control is
still offered to an assessor as the **IA-2** compensating measure for the highest-blast
operations.

### Two further corrections this amendment records

- **The gate no longer dead-ends.** A sub-AAL2 operator is redirected into an
  explain-then-verify flow (interstitial → Kratos `aal=aal2` login → resume the pinned
  mutation via a one-shot nonce) rather than receiving a 403. Machine callers, which have
  no browser to redirect, keep the refusal. The original "refuse and let the operator
  re-authenticate manually" behaviour was not merely unfriendly — combined with the
  missing signal it was unrecoverable.
- **An audit-honesty defect is fixed.** `step_up_verified` previously recorded whether the
  gate *ran*, not whether it *passed*; under `STEP_UP_ENFORCED=false` an admin
  grant/revoke therefore claimed a second factor had been presented when none had
  been. It now reflects only a genuine, fresh, verified step-up. An
  audit record that overstates assurance is worse for an accreditor than one that
  understates it, so this is treated as a control defect, not a cosmetic one.

### Standing caveat

Kratos scopes AAL to a session, and Hydra does not surface `identity_provider_session_id`
downstream, so the console cannot bind the check to the *specific* session behind the
caller's token; it takes the best active session for the identity. The one-shot nonce
constrains this where it matters — a challenge is satisfied only by an aal2 authentication
that completed *after* the challenge was issued. Binding to the exact session would require
Hydra to propagate that ID, and is a revisit trigger if it ever does.

### Enforcement remains population-gated

`STEP_UP_ENFORCED` is retained as the documented sandbox escape and **its default stays
`true`**. Turning it on for a goatnet whose admins are password-only would brick admin
operations, so enforcement is gated on an admin factor census reporting full enrolment.
The ordered procedure lives in
`docs/operations/id-admin-console-runbook.md`
§"Turning on step-up". Mandatory admin MFA at the login door is configurable via
`ory_admin_mfa_required` (role `ory`, default **on**), which renders
`session.whoami.required_aal` — the only AAL knob Kratos v1.3.1 exposes for login. The
earlier spelling of that variable targeted a key Kratos does not have and prevented
Kratos from booting; corrected 2026-08-17.

### Additional revisit trigger

- Hydra begins propagating the identity-provider session ID to relying parties — revisit
  binding the AAL check to the exact session behind the caller's token.
