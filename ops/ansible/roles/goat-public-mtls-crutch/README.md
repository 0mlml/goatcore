# ops/ansible/roles/goat-public-mtls-crutch

Block 80 / ADR 0843 — Public mTLS-gated crutch tier.

Installs Traefik with `RequireAndVerifyClientCert` strict-required client-cert
verification against the offline CA, per-route map for mgmt-API + signal-server
+ relay (forwarded over wg-cp0 back to the inner mesh), CRL refresh adapter,
and a post-deploy self-test that asserts the gate rejects no-cert handshakes
at the TLS layer.

## When applied

- Host has `crutch_tier_enabled: true` set (group_vars or `-e`).
- The `wg-cp0-client` role has already run on the same host (the role
  dialing back to the inner mesh assumes `wg-cp0` is up).
- A server cert + key pair has been minted offline by `cert-sign` covering
  the three per-route hostnames (`mgmt.<fqdn>`, `signal.<fqdn>`,
  `relay.<fqdn>`) and staged at `crutch_server_cert_path` +
  `crutch_server_key_path`.

When `crutch_tier_enabled = false` (the default), the role is a no-op — the
substrate-as-code state per ADR 0843 §D5.

## What it does

| Task | Notes |
|---|---|
| Stages the offline-CA root cert | mounted into the Traefik container at `crutch_offline_ca_root_path_host` |
| Renders Traefik static + dynamic config | dynamic.yml carries the `crutch-mtls` TLS options + per-route map + `extract-client-cn` + `strip-inbound-goat-device-id` middlewares |
| Renders the docker-compose stanza | host-network mode, Traefik 3.x, single container |
| Renders the CRL refresh script + systemd timer | 5-min cadence; emits a Prometheus textfile metric |
| Brings up Traefik via `docker_compose_v2` | restart-unless-stopped |
| **Post-deploy self-test** | no-client-cert handshake on TCP 443 MUST fail with a TLS alert; play fails-fast if the gate drifts |

## Test-mode

Set `crutch_test_mode: true` to skip docker/systemd actions (renders templates
only) — for role-level fixtures or pre-flight reviews. Mirrors the
`wg-cp0-client` role's `wg_cp0_test_mode` convention.

## Sibling roles

- `traefik` — the standard mesh-internal Traefik role (server-TLS only; no
  mTLS). This role is its own substrate because the mTLS terminator's shape
  and deploy lifecycle differ enough to be cleaner side-by-side.
- `wg-cp0-client` — must run BEFORE this role on the same host. Provisions
  the silent control-plane tunnel back to the inner mgmt-plane that the
  crutch's per-route backends dial.

## Cross-references

- Design: `docs/design/public-mtls-crutch-tier.md`
- ADR: `docs/adr/0843-public-mgmt-signal-relay-mobile-crutch.md`
- Runbook: `docs/operations/block-80-crutch-activate.md`
- Implementation plan: Block 80 in `docs/project/implementation-plan.md`
