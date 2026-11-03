# ADR 1080 — Generalized placement descriptor for placed assets (relays first)

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-07-14 |
| Repo scope | goat core |
| Supersedes | — |
| Superseded by | — |
| Related | ADR 1058 (relay self-publish ingest), ADR 1061 (relay-health cloud host), ADR 0215 (scope envelope) |

## TL;DR (for a layperson)

The relay dashboard is growing a map view, and a map needs to know where
things are. Rather than hard-coding "AWS region" into the dashboard, we give
every placed asset a small, provider-agnostic *placement descriptor* —
provider, region, availability zone, coordinates, display name — carried as
optional tokens on the same hackable config line that already declares a
relay's capacity. If a relay doesn't declare coordinates it simply appears in
an "unplaced" list instead of at a guessed spot on the map: the dashboard
never invents a location.

## Context

`ops/relay/health/relays.conf` is the relay-fleet source of truth: one
whitespace-column line per relay (`name public_ip region instance_id goatnet`)
plus optional trailing `key=value` tokens, a pattern already proven by the
declared-capacity tokens (`cap_mbit= cap_src=`, with grey-when-undeclared
honesty). The fleet-overview redesign adds a geographic view, which needs
coordinates; multi-provider fleets are on the growth path, which needs the
schema to not assume AWS.

The `region` column predates this ADR and stays what it is: an opaque
provider-region string used by SSM. Placement is the *generalization*, not a
replacement.

## Decision

One placement schema, used by relays now and any placed asset later:

```
placement: { provider, region, az?, site?, lat?, lon?, display_name? }
```

- **SoT = optional `key=value` tokens on the `relays.conf` line**, same
  pattern as the capacity tokens:
  - `provider=<slug>` — `aws`, `azure`, `hetzner`, `onprem`, …
  - `az=<zone>` — provider AZ (enables same-AZ co-location clustering)
  - `geo=<lat>,<lon>` — decimal degrees, operator-declared
  - `place=<display>` — one token; underscores render as spaces
- **Hosted vantages need no extra plumbing.** The `relay-health` role copies
  `relays.conf` verbatim onto every hosted vantage (Thor + cloud), so tokens
  ride the existing deploy path. There is deliberately no group_vars mirror
  of per-relay placement — one SoT, per the conf's header contract.
- **Honesty rule (fail-closed):** no `geo=` → the relay renders in the list
  view and in an **"unplaced" tray** on the map. Coordinates are **never
  inferred from region names at runtime.** A static region→approx-coords
  table ships in the repo (`ops/relay/health/placement-tokens.sh`) strictly
  as *seed data for writing conf lines*; the dashboard and prober never read
  it.
- **Prober contract:** `relay-health-probe.sh` parses the tokens and emits a
  `placement` object per relay in `relay-health.json`
  (`{provider,az,lat,lon,place}`, each `null` when undeclared; `region`
  stays where it is). Malformed `geo=` parses to `null,null` — honest
  unplaced, never a partial coordinate.
- **Naming convention unchanged:** narrative docs keep "region 1/2/3";
  concrete region/az strings live in `ops/` + `terraform/` + the admin-gated
  dashboard, as they already do.

## Alternatives considered

- **Group_vars-templated conf for hosted deploys** — rejected: the role
  already copies the conf verbatim; a second declaration point invites drift
  for zero capability.
- **Runtime region→coords fallback** — rejected: a relay would render at a
  city it may not be in, violating the never-fabricate rule that governs
  every number on this page.
- **Separate placement file** — rejected: two files to keep in sync per
  relay; the token pattern already exists and is proven.

## Consequences

**Positive.** Map view lands with zero new deploy machinery; multi-provider
fleets are just new token values; the descriptor is reusable by any future
placed-asset surface (bridges, standalone goatnet hosts).

**Negative.** Coordinates are hand-declared, so they can be wrong or go
stale if an instance migrates region — the seed helper reduces but does not
remove that risk. Whitespace-token `place=` can't carry spaces (underscore
convention).

## Assessor view (SCA-R)

Configuration-management shaped (CM-8 component inventory attributes): the
descriptor adds inventory metadata to an existing operator-maintained config
under the same review path (PR-gated git). No new network surface, no new
credential. Residual risk is stale/incorrect self-declared location metadata
on an admin-gated observability page; compensating control is the provenance
discipline (declared-only, unplaced-when-absent) plus git history.

## Trigger conditions for revisit

- First non-AWS relay/provider lands (validate the slugs cover it).
- Any asset type outside `relays.conf` needs placement (promote the schema
  to a shared parser instead of copying it).
- Placement ever becomes an input to routing or enforcement (it is
  display-only today; that change would need its own ADR).
