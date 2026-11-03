# Site node naming — recommended convention

**Status: recommended guidance, UNENFORCED.** No CI gate checks this; it is a
convention to keep node names legible across a site so nobody reads an offline
*backup* box as an offline *core* node.

## The pattern

```
<site>-<role>-<ordinal>
```

- **`site`** — short site code: `alpha`, `beta`, … (not the goatnet; a site inside it).
- **`role`** — what the box IS: `core` (mgmt stack), `obs` (observability),
  `relay`, `breakglass`, `backup`-of-a-role (see below).
- **`ordinal`**:
  - `01`, `02`, … for **peers in an HA/pair set** — nodes that both SERVE and can
    take over for each other (the ADR 1141 core-pair / obs-pair).
  - **`bkp`** (optionally `bkp-01`) for a **dedicated backup receiver** — a box
    that only HOLDS copies and does not serve. Kept distinct from `-02` on purpose:
    a `-02` implies a live standby that fails over; a `bkp` does not.

## Worked example — site alpha

| Node | Canonical name | Serves? | Backs up? |
|---|---|---|---|
| mgmt stack | `alpha-core-01` | yes (the core) | — |
| Prometheus/Grafana | `alpha-obs-01` | yes (obs) | — |
| pgBackRest repo copy | `alpha-core-bkp` | no | the core's Postgres |
| obs data copy | `alpha-obs-bkp` | no | the obs node's TSDB/Grafana |

If a backup is ever promoted to a true hot standby, rename it to the next serving
ordinal (`alpha-core-bkp` → `alpha-core-02`) — the name should track the role.

## What a name touches (so a rename is deliberate, not casual)

Most identities are decoupled from the hostname and ride through a rename:
`site_id`-derived things — the Spock node name, the pgBackRest stanza, service
cert CNs. What a rename DOES touch: the OS hostname, the NetBird peer name/FQDN
(rename via the API to keep the mesh IP), the Zenoh router ZID (seed is
`inventory_hostname` — **pin `goat_zenoh_router_zid` before renaming** to avoid a
federation re-link), the Ansible `inventory_hostname` and every `hostvars[...]`
reference, and any cross-site allowlist that names the peer FQDN
(`host_net_allowed_names`, `mesh-health` peer classes on the FOUNDER). Do it early,
on fresh nodes; it is far cheaper before a site is in heavy use.
