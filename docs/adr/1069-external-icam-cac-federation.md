# ADR 1069 — External ICAM / CAC federation into the Humans domain

**Status:** Proposed. Toggle switch + both sovereign CAC roles built default-off; **#2 (mtls-proxy) + #3 (Keycloak x509) live-fired with a real CAC + real DoD PKI** (2026-07-02, 2026-07-07); #1 (upstream OIDC) config-built pending RP registration. Design: [`../design/external-icam-federation.md`](../design/external-icam-federation.md). Runbook: [`../operations/dod-pki-cac-federation-setup.md`](../operations/dod-pki-cac-federation-setup.md).
**Related:** `identity-architecture.md` §9 (federation strategy — this realizes it) · [ADR 0857](0857-patched-selfservice-ui-for-groups-claim.md) (groups stay local) · ADR 1043 + [ADR 1067](1067-identity-grammar-generated-sot.md) (the grammar the assurance/source-scheme dims extend) · [ADR 1051](1051-identity-descriptor-standard.md) (the descriptor the binding rides) · ADR 0412/[federated-trust-model.md](../design/federated-trust-model.md) (the *workload* federation analog).

## Plain-language summary

Let a person whom an authoritative external identity provider already authenticated — a DoD **CAC/PIV**, **login.gov**, or **DoD Entra/Azure Gov** — become a first-class goat identity, *without goat pretending to be their identity source*. goat's native UUID anchor stays the key; the external subject (EDIPI / login.gov sub / Entra oid) rides as a mapped attribute. One selector picks how the CAC is validated; all paths converge on the same Kratos broker + claim-map.

## Context

The four-domain model (ADR-referenced) already decouples the human IdP from machines/workloads/ACL/audit, and `identity-architecture.md` §9 names Kratos-as-OIDC-broker as the federation mechanism — but it was unconfigured. DoD reality (enterprise ICAM programmes, Azure-Gov cross-tenant consent) is that CAC validation is done *upstream* (an enterprise IdP / Azure Gov) and relying parties integrate over OIDC/SAML. Empirically confirmed: **Ory Kratos has no native x509/smartcard method** (Keycloak does); so goat must federate CAC, not terminate it in Kratos.

## Decision

**One CAC-ingress toggle switch, three interchangeable backends, one converged downstream contract.** `icam_federation.providers[].mode` selects:

1. **`upstream_oidc` (#1, default)** — Kratos federates to an external enterprise IdP that already does CAC (an enterprise IdP / Azure Gov / login.gov). Zero smartcard code; enterprise-reachability dependency treated as opportunistic.
2. **`mtls_proxy` (#2, sovereign)** — a local nginx terminates x509 mTLS against the DoD PKI, extracts the EDIPI, and bridges via Dex authproxy to Kratos. The only CAC path with no reachable enterprise IdP.
3. **`keycloak_frontend` (#3)** — a local Keycloak runs its native x509 authenticator; Kratos federates to it. Platform-One-style flows without hand-building #2.

All three present to Kratos as an upstream OIDC provider → a per-IdP declarative claim-map (jsonnet) → a native anchor whose `metadata_public.external_subjects` carries `(issuer, client_id, subject)` + a per-event assurance snapshot. Load-bearing sub-decisions:

- **Native anchor is the key; external subjects are mapped attributes** — forced by every provider (login.gov `sub` is pairwise, Entra `sub` per-app, EDIPI reissued/non-unique). Identity *linking*, not replacement.
- **The binding rides `metadata_public`, not a `traits` field** — Kratos coerces a nested array trait to `{}` during oidc-registration (proven); `metadata_public` set by the same mapper passes through, and it is the correct home (Kratos already tracks `(provider, subject)` natively for login-matching; `external_subjects` is goat's descriptor projection).
- **Claim-maps read `claims.raw_claims`** (Kratos nests non-standard claims there) and map external groups to goat capability **only through an explicit per-IdP allow-map** — never silent trust (ADR 0857 discipline; RBAC stays local).
- **Federation attaches to the site IdP; the operator IdP stays native** (sovereign break-glass).
- **Two new grammar SoT dimensions** (ADR 1067): `identity.source_scheme` + NIST 800-63 `assurance.{ial,aal,fal}`, additive, fail-closed.
- **#2/#3 ship as real default-off ansible roles** (`icam_mtls_proxy_enabled` / `icam_keycloak_cac_enabled`) — proven, feature-flagged, activated on demand.

## Alternatives considered

- **Terminate CAC in Kratos directly.** Rejected — Kratos has no x509 method; would mean a heavy fork.
- **Adopt Keycloak as the sole human IdP (replace Ory).** Rejected — throws away the Ory groups-claim/consent work (ADR 0857) and the sovereign posture; Keycloak is used *only* as the optional CAC edge (#3).
- **Store `external_subjects` as a Kratos trait.** Rejected empirically — array-trait coercion breaks the mint; metadata is also the architecturally-correct home.
- **Use the external subject as the identity key.** Rejected — pairwise/reissued external subjects can't be stable cross-system keys; FICAM itself prescribes link-external-keep-local-governance.
- **One path only.** Rejected — sovereign/denied-edge needs #2/#3; enterprise-connected needs #1; the toggle keeps all three one flag apart.

## Consequences

- CAC/PIV/login.gov/Entra humans become native goat identities with local RBAC and audit; the external assertion is attribution, the anchor is authority.
- **#2 + #3 proven with a real CAC + real DoD PKI**; ready to activate per-goatnet by flipping one flag (+ staging the DoD PKI bundle, enabling CRL/OCSP).
- #1 is a config + an RP/app-registration away (organizational, not technical).
- New surfaces to own when activated: DoD PKI trust store + revocation, the mTLS proxy / Keycloak realm, and (for SAML-only upstreams / SCIM provisioning) the shim + adapter — deferred until demanded.
- Assurance now travels as first-class per-event data; downstream authz can gate on it (fail-closed) — the enforcement side is designed-not-built.

## Rollback

Additive and default-off end to end: set `icam_federation.enabled: false` (the Kratos oidc block stops rendering) and leave `icam_*_enabled: false` (the roles no-op). The grammar dims are additive (deprecate-never-delete). No live goatnet consumes it yet, so rollback is config-only.
