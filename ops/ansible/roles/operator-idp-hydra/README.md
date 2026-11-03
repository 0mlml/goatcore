# operator-idp-hydra

Per-site Ory Hydra serving OAuth2 / OIDC for the operator IdP per [ADR 0842](../../../../docs/adr/0842-dedicated-operator-idp-for-observability.md).

## Posture

- **Issuer URL:** `https://oauth2.ops.<site>.netbird-prod.goat.example/`
- **Login + consent + logout URIs** point at `auth.ops.<site>...` — the operator-idp-kratos UI surface (same Orin-C, separate compose project).
- **Default access-token strategy = JWT.** F-078 (Hydra defaulting to opaque tokens) is mitigated at the issuer level here; every operator-IdP consumer JWKS-validates JWTs offline.
- **Per-client `skip_consent: true`** is the F-077 mitigation, applied at OAuth2 client provisioning time (not in this base config).

## Required env vars

| Var | Purpose |
|---|---|
| `operator_idp_hydra_secrets_system` | 64-hex Hydra system secret. FRESH per site. |
| `operator_idp_hydra_secrets_cookie` | 64-hex Hydra cookie secret. FRESH per site. |
| `operator_idp_pg_app_password` | DSN password — same as the `operator_idp` Postgres app role. |

For vega specifically: `VEGA_C_OPERATOR_IDP_HYDRA_SECRETS_*`.

## Sequencing

This role MUST run AFTER `operator-idp-postgres` (DB) AND `operator-idp-kratos` (login UI surface). The orchestrator (`vega-obs-bootstrap.yml` Phase 8 added in 21H.1d) enforces order via play sequence.

## What the role does

1. Pre-flight assertion that secrets + DSN password + `site_id` are set.
2. Renders compose project (mode 0640), hydra.yaml (mode 0644 — non-root container needs world-readable), and `.env` (mode 0600 — secrets).
3. Pulls image, runs Hydra migrations (idempotent), brings up the container.
4. Waits for `/health/ready` (loopback admin port) AND `/.well-known/openid-configuration` (public). Public listener flake-budget (20 × 3s) covers the same first-start delay observed at vega-A .

## OAuth2 client provisioning (NOT in this role)

Per-consumer OAuth2 clients (Grafana, MinIO, OpenObserve, Forgejo, etc.) are provisioned by their respective consumer roles — same idempotent GET-then-POST-or-PUT pattern as the netbird-dashboard role's 21D.1.c. Each new client gets `skip_consent: true` baked in.

## Tests

`tests/syntax-check.sh` — Ansible parse-only smoke + register-uniqueness lint. Behavioral coverage rides on the operator-fired 21H.1 bringup smoke.
