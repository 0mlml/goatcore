# goat-id-console

Admin console for the Ory Kratos + Hydra identity plane. One front door
for platform operators to manage identities + groups + invites across
both Kratos instances on nova-a (tenant IdP on port 4434 + operator IdP on
port 4436).

Design: `docs/design/id-admin-console.md`.
ADR: [`docs/adr/0860-id-admin-console-separate-tenant.md`](../../docs/adr/0860-id-admin-console-separate-tenant.md).
Runbook: `docs/operations/id-admin-console-runbook.md`.
Permissions YAML: [`ops/permissions/group-permissions.yml`](../permissions/group-permissions.yml).

## Layout

```
cmd/console/      entry point
internal/
  config/         env loading + IdP map
  kratos/         Kratos admin HTTP client
  hydra/          Hydra admin HTTP client (consent sessions)
  audit/          Audit emitter interface + stderr impl + ring buffer
  store/          Invite token store (in-memory v1)
  permissions/    YAML loader for the permission matrix
  handlers/       HTTP handlers + templates wiring
web/
  templates/      html/template page templates
  static/         CSS + favicon
```

## Build

```
go build ./...
go test ./...
```

Vendored deps: `gopkg.in/yaml.v3` only.

## Run locally (against a real Kratos)

```
LISTEN_ADDR=:8080 \
IDP_TENANT_KRATOS_ADMIN_URL=http://127.0.0.1:4434 \
IDP_TENANT_HYDRA_ADMIN_URL=http://127.0.0.1:4445 \
IDP_OPERATOR_KRATOS_ADMIN_URL=http://127.0.0.1:4436 \
IDP_OPERATOR_HYDRA_ADMIN_URL=http://127.0.0.1:4447 \
PERMISSIONS_YAML_PATH=../permissions/group-permissions.yml \
DEV_BYPASS_AUTH=1 \
go run ./cmd/console
```

`DEV_BYPASS_AUTH=1` short-circuits the `X-Auth-Request-Groups` check for
local development against a Kratos without an oauth2-proxy in front. Never
set this in any deployed environment.

## Status

v1 scaffold: listing (with MFA column + client-side filter) + group editing
+ recovery-link mint + second-factor (2FA) reset + invite-link generation +
permissions render + in-memory audit. Postgres-backed invite + audit store
and Zenoh audit fan-out are TODO behind interfaces.
