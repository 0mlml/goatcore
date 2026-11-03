# isitup — the goatnet status page

**Status:** built (role `ops/ansible/roles/isitup/`, page `ops/status/index.html`). The #1518 changes (row list, lifecycle, `/external`, collapsible sections, registry-filtered probes) are written and tested in CI, and not yet deployed to any goatnet.

Each goatnet serves one status page at `https://isitup.<site>.<zone>/`, behind the goatnet's own sign-in. It has two jobs. It is the **home page**, where a newcomer finds what they can open. It is also the **health board**, where an operator sees what is down. This document describes how the page decides which rows exist, how it lays them out for those two readers, and how a row's state reaches alerting.

## Who reads it

| Reader | What they need from the page |
|---|---|
| Newcomer or candidate (OBSERVER) | The few things they can open now: sign-in, the guide, docs, the CLI download |
| Everyday mesh user (DEVELOPER) | Whether the tools they use are up, so they know whether a problem is theirs |
| Partner | Their own service and the shared surfaces, clearly apart from goat's own, with an owner to contact |
| Operator or on-call (MONITOR, PLATFORM-ADMIN) | A banner that goes red only when a live, committed service is down |
| External-service owner | Control over which of their hostnames are listed, under their own name |

## Where rows come from

Every row on the page comes from one list, built once per deploy, so the rows that are probed and the rows that are described are always the same set.

`templates/isitup-rows.json.j2` builds the list into the `isitup_rows` fact (`tasks/main.yml`, "Build the status-page row list"). It merges three inputs in precedence order, and a row whose id is already on the list is skipped:

1. `isitup_services`, the goatnet's own services (`group_vars/<site>_hosts/vars.yml`). Source `core`, or `offmesh` for a row with `category: external`.
2. `isitup_external_services`, the shared off-mesh catalog (`isitup_external_services_catalog` in `group_vars/all.yml`, opted into per site). Source `offmesh`.
3. External-service declarations, `external_service_*` in `group_vars/<site>_hosts/external-service-<name>.yml` (ADR 1178). There is one row per listed hostname, and a hostname that an earlier row already names is skipped. Source `external`.

A hidden row (`listed: false` or past `retire_after`) still claims its id and hostname. The catalog's copy of a hidden inline row, or a declaration for the same hostname, therefore stays hidden too. zagros and zagros-prod carry the seven catalog rows inline as well (`group_vars/zagros_hosts/vars.yml`, "Off-mesh rows recovered"); the inline copy wins, and each renders once.

Two files are rendered from `isitup_rows`:

- `services.env` is the pipe-delimited list `files/check.sh` probes every five minutes. Its column format (`APP_SERVICES_FORMAT=3`) is unchanged.
- `data/meta.json` records, per row id, the facts the probe does not know: `lifecycle`, `source`, `owner` and `retire_after`. It sits in the `data/` directory bind mount, so a deploy updates it without recreating the container. nginx serves it `no-store`, like `current.json`.

The page joins `current.json` (probe results) with `meta.json` by row id. A goatnet deployed before `meta.json` existed has none, and every row then reads as a live core service, which is how the page behaved before this change.

## Row state

Each row can say whether it should be counted, shown and alerted on, independently of whether its probe passes.

| Field | Where | Effect |
|---|---|---|
| `lifecycle: live` | default | Shown, counted in the banner, probed by the obs host |
| `lifecycle: planned` | `isitup_services` row, declaration `status.lifecycle`, or `status.hostnames.<h>.lifecycle` | Shown with a *Planned* pill and a muted row. Not counted in the banner or section summaries. Not a blackbox target |
| `lifecycle: retiring` | same | Shown muted with *Retiring* and its date. Not counted. Not a blackbox target |
| `listed: false` | `isitup_services` row; `status.list: false` or `status.hostnames.<h>.list: false` on a declaration | Not on the page, not probed by isitup, not a blackbox target. The row's mesh DNS record, certificate name and gym link are untouched, because those read `isitup_services` and `hostnames:` directly |
| `retire_after: YYYY-MM-DD` | row or declaration | Dropped from `isitup_rows` once the date has passed, and hidden by the page from `meta.json`, so it disappears even if nobody re-runs the play |

The role asserts that every `lifecycle` is one of the three values ("Require every row's lifecycle to be one the page understands"). A typo would otherwise be counted as live on the page and ignored by the obs filter.

## Layout

**Home page (`/`).** This shows the goatnet's own rows: sources `core` and `offmesh`. Sections follow `GROUPS` in `index.html`, in newcomer order: account, learn, tools, live, sandbox, utilities, and so on. Each section is a `<details>` element:

- *Sign-in & onboarding*, *Docs & briefings* and *Tools & downloads* start open. Every other section starts closed.
- A closed section's heading carries its row count and any fault, for example `15 · 1 down · 2 not live`.
- A section with a down or degraded live row is always open.
- A reader's own open or close choice is remembered in `localStorage`, keyed per page. Only the reader's clicks are recorded, not a section the page opened for a fault, so that section closes again once the fault clears. This is a convenience only: every call is wrapped in try/catch, and the page renders the same without it.
- A planned or retiring row's dot is the neutral grey ring whatever its probe says; the detail line still shows the measurement.

**External services (`/external`).** This page is the same SPA and reads the same data. nginx routes `/external` (with or without a trailing slash) to `index.html` (`templates/nginx.conf.j2`), and the page picks its view from the path. It shows only `source: external` rows, grouped under their declaration's `owner`, with a *Hosted by …* line on each row and the `retire_after` date when there is one. A note at the top says these services are run by their owners, not the goat core team. The home page carries a one-line summary linking here, for example *External services — 9 hosted by members, not the core team · 1 with a fault*.

The *This goatnet's services / External services* switch appears only when external rows exist, because the same `index.html` also serves the legacy relay-tier board (`ops/status/`), which has no `/external` route.

**Banner.** On each page the banner counts only that page's `live` rows. Planned, retiring and past-date rows never reach it.

## Alerting

Paging comes from the obs hosts' blackbox exporter, not from isitup. A probe failure there fires the TLS and surface alerts routed by Alertmanager to ntfy (ADR 1168). The status page itself sends nothing. Taking a row out of alerting is one edit to its registry row on both obs stacks:

- **goat-obs stack** (`ops/compose/goat-obs/prometheus/prometheus.yml`, zagros). Blackbox targets are rendered from `isitup_services`. The `blackbox-https` and `blackbox-surface-app` loops skip any row that is not `lifecycle: live`, is `listed: false`, or is past its `retire_after` date.
- **obs-host role** (`ops/ansible/roles/obs-host/`, zagros-prod). Targets are the hand-kept `obs_blackbox_targets` in `group_vars/<site>_obs_hosts/vars.yml`. The role reads the mgmt host's vars through `hostvars`, from the group named by `obs_blackbox_registry_group` (for zagros-prod, `zagros_prod_hosts`), and renders `templates/registry-rows.json.j2`. That gives one `{fqdn, lifecycle, listed}` per `isitup_services` row and per external-service hostname, with a declaration's `status` settings, `enabled: false` and a past `retire_after` folded into `listed`. `templates/prometheus.yml.j2` and `templates/docker-compose.yml.j2` then drop every target whose host is on a non-live or unlisted row. A named group that is absent from the inventory fails the play.

Two CI checks hold this in place:

- `ops/ci/check-blackbox-covers-registry.sh` requires every live, listed `probe_gate` row to have a target, and requires `obs_blackbox_registry_group` to name the site's own mgmt group on every goatnet with blackbox enabled.
- `ops/ci/check-prometheus-template-renders.sh` renders the goat-obs template with planned, retiring, unlisted (including a quoted `"false"`) and past-retire fixture rows plus a live control row, and fails if any of them is a target or the control row is dropped. It renders the obs-host templates with the real zagros-prod registry and declarations, and again with the first target's row marked planned. It also checks `registry-rows.json.j2` directly: a declaration's lifecycle, a per-hostname `list: false`, `enabled: false` and a past `retire_after` must each reach the obs host.

## Tests

`ops/ansible/roles/isitup/tests/syntax-check.sh` is the role's CI entry point (`.github/workflows/ansible-syntax-check.yml`). It runs:

- `row-list.py`: which rows reach the page. Covers dedup by id, `listed`, `retire_after`, lifecycle, source, owner, per-hostname `list` and `health_path`, and checks that `services.env` and `meta.json` describe the same set.
- `free-text-columns.sh`: `name` and `desc` survive quoting into `services.env`.
- `front-door-probe.sh`: the check.sh front-door grading.

## Related

- [external-service-golden-path](external-service-golden-path.md) §"On the status page": the declaration fields an owner sets.
- dns-service-discovery: why the off-mesh catalog is a separate list from `isitup_services`.
- Gym module `read-the-status-page` (`ops/gym/gym.yaml`, `ops/gym/content/read-the-status-page.md`).
- Issue #1518.
