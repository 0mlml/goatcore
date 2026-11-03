# lan-coredns — closed-LAN authoritative DNS for a single mgmt-host instance

Per-site CoreDNS role purpose-built for the **closed-LAN posture** of `docs/design/nova-lan-instance.md`. Sibling-shape to [`mesh-coredns`](../mesh-coredns/README.md) but with the inverse load-bearing invariants:

| Invariant | `mesh-coredns` | `lan-coredns` |
|---|---|---|
| Bind interface | Netbird mesh IP + optional wg-cp0 IP (NEVER LAN) | **LAN IP only** (no mesh, no wg-cp0) |
| Forwarder | `forward . 1.1.1.1 8.8.8.8` | **none** — return NXDOMAIN for unknown queries |
| Posture | Per-site overlay for hostnames on top of public DNS | **Authoritative** for the entire `nova.lan` zone |
| Trust boundary | Silent control plane (ADR 0313/0314) | The closed LAN itself (`nova-lan-instance.md` § Internal CA + DNS) |

The role is deliberately separate from `mesh-coredns` rather than parameterizing the existing one — `mesh-coredns` aggressively rejects LAN binding as a silent-control-plane invariant, and weakening that assertion would create a footgun for every other site that uses the existing role. Keep the postures structurally distinct.

## When to use this role

- Stand up the canonical name server on nova-lan-A in a closed-network deployment.
- Future closed-LAN sites that mirror the `nova.lan` shape (different zone name, same posture).

Do **not** use this role for:
- Internet-connected sites — those run `mesh-coredns` per their respective designs.
- Any deployment where the host has a public uplink and needs to forward queries it doesn't own.

## Required variables

| Variable | Description |
|---|---|
| `lan_coredns_bind_ip` | The host's LAN IP (NOT mesh, NOT wg-cp0). Bound on UDP port `lan_coredns_port` (default 53; the closed network has no other consumer of port 53 on the host). |
| `lan_coredns_zone_name` | The authoritative zone (e.g. `nova.lan`). Used in synthetic SOA records and zone-file path resolution. |
| `lan_coredns_zone_dir` | Path to the directory containing the zone-data file (e.g. `{{ playbook_dir }}/../dns/nova-lan`). The role reads `<zone_dir>/<zone_name>.hosts`. |
| `lan_coredns_smoke_test_name` | A hostname expected to resolve under the zone (e.g. `portal.nova.lan`). |
| `lan_coredns_smoke_test_expect` | The IP that `lan_coredns_smoke_test_name` should resolve to. |

## Optional variables

See `defaults/main.yml`.

## Apply

Via the canonical wrapper playbook `ops/ansible/nova-lan-coredns.yml`. The wrapper sets the bind IP from the per-site inventory and points at `ops/dns/nova-lan/` for the zone data.

## Smoke test

Same shape as `mesh-coredns` — `dig @<bind-ip> -p <port> <smoke-name> +short` must return a non-empty answer.

## Composes with

- Phase B.3 bootstrap-bundle wire-up — distributes the `nova-lan-ca` root cert and seeds `/etc/resolv.conf` on Tier-T hosts to point at this resolver.
- Phase C Ansible role tree — Traefik on nova-lan-A consumes service-cert files keyed by hostname; those hostnames must resolve via this role.
- Phase D translator framework — translators on Tier-L hosts do not use this resolver (Tier-L has no DNS by design); the resolver serves Tier-T only.
