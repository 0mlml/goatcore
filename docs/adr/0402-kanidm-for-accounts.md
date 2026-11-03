# ADR 0402 — Kanidm for accounts, sudo, PAM

| Field | Value |
|---|---|
| Status | Accepted — **marked for supersession by 0411 (Proposed, pending spike)** — see addendum below. Strict ADR lifecycle ([`0002`](0002-adr-lifecycle.md)) requires the superseding ADR to reach Accepted before this one can be marked Superseded. Until then this ADR remains Accepted with the user-store role flagged for change to Ory once 0411 lands. |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Superseded by | [`0411-ory-idp-mvp.md`](0411-ory-idp-mvp.md) (in part — Ory absorbs the user-store role for OIDC-fronted services) |
| Related | [`0401-per-site-dex-oidc.md`](0401-per-site-dex-oidc.md), [`0410-go-cli-peer-enrollment.md`](0410-go-cli-peer-enrollment.md), [`0411-ory-idp-mvp.md`](0411-ory-idp-mvp.md), [`0808-pam.md`](0808-pam.md), [`0403-fido2-break-glass.md`](0403-fido2-break-glass.md), `../design/identity-architecture.md`, `../project/idp-strategy-spike-2026-04-25.md` |

## Superseded in part — addendum 2026-04-25

ADR 0411 selects Ory Kratos + Hydra as the MVP IdP product, with
Postgres as the backing store. Ory Kratos absorbs **the OIDC user-store
role** that this ADR previously assigned to Kanidm.

**What this ADR's Kanidm role becomes:**

- ❌ **Not** the OIDC user-store for human + service-account auth
  (that's now Ory Kratos in Postgres, per ADR 0411).
- 🟡 **Possibly** still relevant for FIDO2 break-glass per ADR 0403
  if the break-glass authenticator backend benefits from Kanidm's
  FIDO2-native design — open question, may resolve to "Ory handles
  FIDO2 directly via WebAuthn connector and Kanidm is not needed."
- 🟡 **Possibly** still relevant for sudo + PAM (ADR 0808) if those
  use a separate authenticator path from web-based OIDC. PAM is a
  different protocol stack; integration with Ory vs Kanidm vs SSSD
  is a Phase 2 sub-decision.

The full coherent picture lives in
`../design/identity-architecture.md`.
ADR 0411 may further-supersede this one when the FIDO2 break-glass
+ PAM integration pattern is locked.

## TL;DR (for a layperson)

Kanidm is the open-source identity store that holds operator
accounts, group memberships, and the credentials sudo and SSH
need. It is Rust-based, designed with strong security defaults,
and supports modern authentication (FIDO2, WebAuthn). We use it
across the fleet so that a single sudo policy or group-membership
change applies everywhere.

## Context

Operator accounts and credentials need a sovereign, modern,
auditable home:

- **Linux PAM integration** for SSH and sudo on every mgmt-host.
- **OIDC / SCIM** for service integration via Dex
  ([`0401`](0401-per-site-dex-oidc.md)).
- **Modern authenticators** (FIDO2, WebAuthn) without
  workarounds.
- **Sovereignty** (no SaaS dependency).

LDAP-based stores (FreeIPA, OpenLDAP+Samba) are operationally
expensive and have older security defaults. Kanidm is the
modern open-source identity-store option that meets the bar.

## Decision

Kanidm is the identity store:

- **Per-site instance** with multi-master replication across
  Router-A / Router-B pair.
- **PAM integration** on every mgmt-host (sudo, SSH).
- **OIDC backend** for Dex.
- **FIDO2 / WebAuthn** as primary authenticator.
- **Group-based authorization** integrated with sudo policy
  and Kubernetes RBAC (when relevant).
- **Audit hooks** ship to the central audit pipeline.

Implementation is part of Block 3 in
`../project/implementation-plan.md`.
PAM rollout is Block 18B / 3D
([`0808`](0808-pam.md)).

## Alternatives considered

- **FreeIPA.** Mature but operationally heavy; LDAP-based;
  Java/Python heavy. Rejected.
- **OpenLDAP + Samba 4.** DIY assembly; weaker security
  defaults. Rejected.
- **Keycloak / Authentik.** OIDC-focused; less complete on
  PAM/Linux integration. Rejected as primary identity store.
- **DoD-federated identity store.** Loses sovereignty;
  partition-broken. Rejected.

## Consequences

**Positive.** Modern, auditable, sovereign identity store.
PAM integration covers SSH and sudo on every mgmt-host. FIDO2
support out of the box.

**Negative.** Kanidm is a younger ecosystem than LDAP-based
alternatives. We accept; the security defaults more than
compensate, and the Rust implementation is a positive trust
signal.

## Assessor view (SCA-R)

Kanidm serves IA-2 (identification and authentication), AC-2
(account management), AC-6 (least privilege — group-based
sudo), and AU-2 (audit). The Rust implementation and modern
defaults align with NIST SP 800-63B AAL2/AAL3 expectations
when paired with FIDO2 ([`0403`](0403-fido2-break-glass.md)).

## Trigger conditions for revisit

- Kanidm upstream stalls or governance changes adversely.
- A federation partner requires LDAP integration we cannot
  achieve via Dex / SCIM. Add a connector; do not switch
  identity stores.

---

*See `GLOSSARY.md` for terms and acronyms.*
