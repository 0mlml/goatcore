# icam-keycloak-cac — Keycloak CAC front-end (toggle-switch path #3)

**Status: real + proven, default-off.** A single flag
(`icam_keycloak_cac_enabled: true`) stands the whole path up. Not a stub.

## What it does

Keycloak as the CAC front-end, using its native **X.509 client-certificate
authenticator**: a CAC cert presented at the TLS layer is validated against the DoD
PKI trust anchor, the **EDIPI is extracted from the cert Subject DN** (regex), matched
to a user, and Keycloak issues an OIDC token carrying `edipi`. Kratos federates to
Keycloak over OIDC (the `keycloak_frontend` mode of `icam_federation`), reusing the
path-#1-proven broker + `ops/identity/claim-maps/eams-a.jsonnet`.

## Proven

The full-chain kit for a physical card is `ops/identity/cac-live-test/`. Headless-proven with a
software cert: `ops/identity/keycloak-cac-proof/run.sh` — a test CAC cert → Keycloak x509
direct-grant → token with `edipi=1234567890` + `preferred_username`. The role
templatizes that exact realm/config.

## Enable

Set in `group_vars/<site>/`:
```yaml
icam_keycloak_cac_enabled: true
icam_keycloak_cac_ca_bundle: /opt/mgmt-host/pki/dod-roots.pem   # the DoD PKI trust anchor
icam_keycloak_cac_crl_enabled: true                             # production revocation
```
plus the `icam_federation` provider in `keycloak_frontend` mode pointing at
`icam_keycloak_cac_issuer_url`. Secrets via env (`ICAM_KC_ADMIN_PASSWORD`,
`ICAM_KC_KRATOS_CLIENT_SECRET`). Mesh-only bind (`icam_keycloak_cac_bind_ip`).

## Deployment notes

- **User provisioning** is deployment-specific: the x509 authenticator maps the cert
  to an *existing* Keycloak user (LDAP/directory federation or JIT). The proof realm
  pre-creates the user; a real deploy federates the directory.
- Swap the self-signed server TLS cert for the mesh CA-issued cert in production.
- `deploy-history.yml` + rollback-ledger meta-dep should be added when first deployed
  to a goatnet (ADR 1060).
