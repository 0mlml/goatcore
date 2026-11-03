# operator-idp-kratos

Per-site Ory Kratos serving the operator IdP per [ADR 0842](../../../../docs/adr/0842-dedicated-operator-idp-for-observability.md).

## Posture

- **WebAuthn-only credential method.** No password, no TOTP. Operator population is small + already YubiKey-equipped per the dev-CA multi-operator distribution; password adds attack surface without usability gain.
- **Self-service registration is OFF.** Operator identities are admin-managed via the 21H.2 enrollment script. Anyone able to hit the registration UI must NOT be able to mint themselves an `obs-admin` role.
- **Identity schema with groups[].** Three traits: `email`, `name`, `groups` (array of strings). Per-service authorization via group membership — special `obs-admin` / `obs-readonly` groups for superuser-style access, plus per-consumer groups (`grafana-admin`, `minio-readonly`, `forgejo-user`, etc.) for fine-grained scoping. Each operator carries an array of memberships; consumer-side authz checks for either a global group OR a service-specific group. See [ADR 0842](../../../../docs/adr/0842-dedicated-operator-idp-for-observability.md) §"Decision/Identity schema" for the enum.
- **Distinct WebAuthn RP ID** from site Kratos. A credential issued for site Hydra is NOT replayable against operator IdP and vice versa per ADR 0842.

## Required env vars

Per-site, resolved at runtime via `group_vars/<site>_obs_hosts/vars.yml` from `.envrc.local.d/30-site-services.envrc`:

| Var | Purpose |
|---|---|
| `operator_idp_kratos_cookie_secret` | 64-hex Kratos cookie secret. FRESH per site (NOT cluster-shared — per-site DB). |
| `operator_idp_kratos_cipher_secret` | 32-hex (16-byte) Kratos cipher secret. FRESH per site. |
| `operator_idp_pg_app_password` | DSN password — same as the `operator_idp` Postgres app role; defined by the `operator-idp-postgres` role's pre-flight. |

For vega specifically: `VEGA_C_OPERATOR_IDP_KRATOS_COOKIE_SECRET` etc.

## Sequencing

This role MUST run after `operator-idp-postgres` (the kratos DB must exist on the per-Orin-C Postgres). The orchestrator (`vega-obs-bootstrap.yml` Phase 8 added in 21H.1d) enforces order via play sequence.

## What the role does

1. Pre-flight assertion that all three secrets + `site_id` are set.
2. Renders compose project (mode 0640), kratos.yaml + identity-schema.json (mode 0644 — non-root container needs world-readable), and `.env` (mode 0600 — secrets).
3. Pulls image, runs Kratos migrations (idempotent), brings up the container, waits for `/admin/health/ready` over loopback.

## Tests

`tests/syntax-check.sh` — Ansible parse-only smoke + register-uniqueness lint. Behavioral coverage (real OIDC flow, real WebAuthn enrollment) lives in the operator-fired 21H.1 bringup smoke + 21H.2 enrollment runbook.
