# Role: `netbird-dashboard`

Block 21D.1 — first 21D OIDC consumer. Deploys per-site `netbirdio/dashboard`
behind Ory Hydra. Mirrors the `ory` role shape:
operator-fired via `ops/ansible/dashboard-deploy.yml`, not part of
`site-bootstrap.yml`.

## What it does

1. Renders `dashboard.env` (mode 0600) into `{{ ory_compose_dir }}` with
   the OIDC issuer URL + audience + client_id + client_secret + scope
   list resolved from per-site group_vars.
2. Drops the `docker-compose.dashboard.yml` overlay alongside the existing
   mgmt-host `docker-compose.yml` + `docker-compose.ory.yml`.
3. Provisions (or upserts) the OIDC client in Hydra via the loopback
   admin API (`127.0.0.1:4445`). Idempotent — the role checks for the
   client first, POSTs on first run, PUTs on subsequent runs to keep
   redirect_uris + scopes in sync with `defaults/main.yml`.
4. Pulls + brings up the dashboard service.
5. Smokes the loopback HTTP path (`http://127.0.0.1/` with `Host:
   dashboard.<site>...` header) — confirms the React bundle answers.
   Hydra OIDC roundtrip validation lives in the operator runbook.

## Required inputs

| Var | Source | Note |
|---|---|---|
| `site_id` | `group_vars/<site>_hosts/vars.yml` | e.g. `vega` |
| `site_dns_zone` | per-site group_vars (set to `netbird-prod.goat.example` for prod sites) | matches the Ory role |
| `netbird_dashboard_oidc_client_secret` | `<SITE>_DASHBOARD_OIDC_CLIENT_SECRET` env var, 1Password-backed | ≥16 chars; same value on Router-A |

The secret is rendered into `dashboard.env` at mode 0600. It is also
written into Hydra's `hydra_client.secret` row (bcrypt-hashed) when the
role POSTs the OIDC client. Both copies must match — the role drives
both writes from the same input, so they cannot drift unless an operator
edits Hydra out-of-band (don't).

## Per-site rollout

Lands on Router-A only at `full`-profile sites. Router-B is passive per
[ADR 1010 D1](../../../../docs/adr/1010-ory-on-spock-mindful-operation.md);
on A→B failover, the dashboard goes down with the rest of the mgmt-stack
until A is restored or B is operator-promoted.

See `docs/operations/dashboard-bringup.md`
for the operator-fired sequence — cert ceremony, env-var seeding, Ansible
commands, OIDC-client smoke, dogfood validation.

## Idempotence

- Re-renders `dashboard.env` only if the env-var values differ; restart
  handler fires on change.
- OIDC-client upsert: GET → 200 means PUT (refresh config); 404 means
  POST (first create). Either path leaves Hydra in the same desired
  state. Client_secret is part of both POST + PUT bodies.
- `docker compose pull` + `up -d` is no-op if image digest unchanged.
- Smoke-test is read-only.
