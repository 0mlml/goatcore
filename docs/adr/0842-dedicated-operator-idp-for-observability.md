---
adr: 0842
title: Dedicated operator IdP for observability + utility services on Orin-C
status: Proposed 2026-05-09
audience: Technical contributor; sponsor; SCA-R
deciders: project maintainers
repo_scope: goat core (operations + design)
date: 2026-05-09
supersedes: none
superseded_by: none
related: 0411 (Ory MVP), 1009 (Ory shared NID — does not apply to operator IdP), 1010 (Ory single-active pattern — does not apply to operator IdP), 0826 (per-site three-Orin topology), 0822 (wide-event observability), 0407 (internal X.509 CA for service TLS)
---

# ADR 0842 — Dedicated operator IdP for observability + utility services

## Context

Block 21D is sweeping non-mgmt-API services onto OIDC. The first consumer was netbird-dashboard, fronted by the site Hydra on Orin-A. The next batch is the obs-host trio (Grafana + Prometheus + Alertmanager + OpenObserve + MinIO) plus the utility stack (Forgejo, Ollama UI).

**The naïve placement** — point all of these at the site Hydra on Orin-A — has a structural anti-pattern for the observability subset:

- Per [ADR 1010 D1](1010-ory-on-spock-mindful-operation.md), Ory at a three-Orin site is single-active on Orin-A; Orin-B is passive. If Orin-A goes down, the site Hydra is down for the site until B is manually promoted.
- The interval during which obs dashboards are most useful (post-Orin-A-failure incident response) is exactly the interval during which they would be unreachable.
- Principle: **the IdP that gates observability must not depend on the system being observed.**

Mgmt-API consumers (netbird-dashboard, future tuwunel) do *not* violate the principle — when mgmt-API is down, the dashboard cannot do anything anyway, so coupling their auth lifecycle to mgmt-API health is harmless. They also benefit from cross-site identity replication via Spock + PITR-grade backup via pgBackRest — properties that justify keeping their auth in the production-grade substrate. See [ADR 1009](1009-ory-shared-network-nid-preseed.md), [`pgbackrest.md`](../design/pgbackrest.md) §1.

**Observability and utility services are different.** They are site-local (each Grafana / Forgejo / Ollama serves only its own site's data). Their auth chain does not benefit from cross-site identity replication, and the operator-identity data behind it does not need PITR-grade recovery — it is small (a handful of operators, OAuth2 client configs reproducible from Ansible role variables), and the unreproducible portion (FIDO2 enrollments) is recoverable via 30-second operator self-action.

Two prior placement options were considered and rejected:

- **Move site Hydra to Orin-C.** Even with Hydra service on Orin-C connecting to its DB on the Orin-A/B postgres-spock cluster over wg-cp0, this entangles the obs-tier failure domain with the carefully-established Spock + pgBackRest substrate on Orin-A/B. Operator preference is to leave that substrate untouched.
- **Sync identities from site Hydra to a separate operator IdP.** Convenience win is modest (single FIDO2 enrollment) and conditional on shared WebAuthn RP ID (a security trade-off). Sync code is new failure surface. Defeats blast-radius isolation by re-introducing dependency. Worth revisiting only if the operator population grows by an order of magnitude with documented friction.

## Decision

**Per-site dedicated operator IdP on Orin-C, fully independent of site Hydra.**

Components per three-Orin site:

- Dedicated `kratos serve` + `hydra serve` processes on Orin-C
- Dedicated **single-instance** Postgres on Orin-C — NOT in the postgres-spock cluster, NOT in the pgBackRest stanza
- Simple `pg_dump` cron + age-encrypt to the existing off-site path; no PITR, no streaming standby, no Spock replication
- Distinct Hydra issuer URL: `oauth2.ops.<site>.<mesh zone>`
- Distinct WebAuthn RP ID: `ops.<site>.<mesh zone>`
- Operator identities + FIDO2 credentials live here, separate from end-user identities in site Hydra

**Single-Orin sites**: same logical pattern, same processes, on the single Orin. Physical isolation impossible (one host) but logical isolation preserved — site Hydra failure does not take operator-IdP login down with it; operator-IdP backup path remains independent of pgBackRest. Single-Orin failure mode (host dies → everything dies) is unchanged.

**Consumer split:**

| Service | IdP | Reason |
|---|---|---|
| Grafana | operator IdP (Orin-C) | site-local; obs |
| Prometheus | operator IdP | site-local; obs |
| Alertmanager | operator IdP | site-local; obs |
| OpenObserve | operator IdP | site-local; obs |
| MinIO console | operator IdP | site-local; storage admin |
| Forgejo | operator IdP | site-local; operator git |
| Ollama UI | operator IdP | site-local; operator inference |
| netbird-dashboard | site Hydra (Orin-A) | fleet admin; cross-site identity replication wanted; PITR-grade backup justified |
| (future) tuwunel | site Hydra | end-user chat; same reasoning |

**No sync between operator IdP and site Hydra.** Operators register a FIDO2 token at each operator IdP independently. Roles managed at each operator IdP independently. Population is small enough that double-enrollment cost is operational noise.

**Identity schema (operator-idp-kratos):** three traits — `email`, `name`, and `groups[]` (array of strings). Per-service authorization rides on the `groups` claim in the issued JWT. Group strings:

| Group | Effect |
|---|---|
| `all-admin` | Admin on every consumer (superuser) |
| `all-readonly` | Read-only on every consumer |
| `<service>-admin` | Admin scoped to one consumer (e.g., `grafana-admin`) |
| `<service>-readonly` | Read-only scoped to one consumer |
| `forgejo-user` / `ollama-user` | Service-specific user-tier (no admin) |

Each consumer's role config maps the group strings it cares about to its internal authorization (~3–5 lines per service). An operator with `["all-admin"]` has admin on everything; an operator with `["grafana-admin", "minio-readonly"]` has Grafana admin + MinIO read; an operator with `["all-readonly"]` has read-only across the board. Adding a new consumer means extending the schema enum + adding a one-line role-mapping in that consumer's role. Concrete enum lives in [`operator-idp-kratos/templates/identity-schema.json.j2`](../../ops/ansible/roles/operator-idp-kratos/templates/identity-schema.json.j2).

**Local break-glass preserved.** Each obs/utility service keeps its local-admin path (Grafana `admin` user, MinIO `MINIO_ROOT_USER`, OpenObserve `ZO_ROOT_USER_EMAIL`). These are the partition-tolerance fall-through when operator IdP is down. Credentials live in the operator's credential store per the existing convention.

## Failure-mode table

| Scenario | Site mgmt-stack | Operator IdP | Obs stack | Operator can use obs? |
|---|---|---|---|---|
| Steady state | up | up | up | ✅ |
| Orin-A down (mgmt-pair partial) | A→B promote | up | up | ✅ — independent failure domains |
| Orin-B down | up (A active) | up | up | ✅ |
| Orin-C down | up (A active) | down | down | ⚠️ obs gone anyway; coupling is correct |
| Mgmt-pair (A+B) down | down | up | up | ✅ — operator IdP keeps working; obs stack serves cached telemetry |
| Site fully down | down | down | down | site recovery scenario; peer-site obs unaffected |
| Operator IdP DB corrupt | up | down | up but locked | local break-glass admin creds; restore from `pg_dump` |

The asymmetry vs the naïve "site Hydra fronts everything" placement is in rows 2 and 5: under the naïve placement, both rows lock operators out of obs. Under this ADR, operators retain obs access in both.

## Why not these alternatives

- **Site Hydra fronting obs (status quo).** Documented anti-pattern: lockout when Orin-A is down, exactly when obs is most needed.
- **Relocate site Hydra to Orin-C.** Either (a) DB stays on A/B and Hydra service traverses wg-cp0 to its DB, adding cross-host failure mode and operationally entangling obs-tier with mgmt-pair Spock+pgBackRest substrate, or (b) DB also moves, requiring new Spock peering + new pgBackRest stanza on Orin-C. Operator rejected on grounds of preserving the carefully-established Postgres substrate on Orin-A.
- **Ring topology — peer-site Hydra as primary.** Adds cross-site RTT to every login. Wasteful when Orin-C is in-site and idle. Useful as a deferred enhancement (multi-site fall-through) but not as the primary path.
- **One-way passive sync from site Hydra to operator IdP.** Convenience win (single FIDO2 enrollment) requires shared WebAuthn RP ID, a real security trade-off. Sync code is new failure surface (~100-200 LOC, load-bearing for revocations). Re-introduces dependency on the substrate operator IdP is meant to be insulated from. Operator population at today's scale does not justify the complexity. Revisit only if the population grows by an order of magnitude OR on a documented friction case.
- **Shared identity (single Kratos for both site + operator).** Forces obs and mgmt-API auth to share failure domain — defeats the whole point.

## Implications

### Hardware / load

Two new processes (kratos + hydra) and one new Postgres on Orin-C. Total memory budget ~256-384 MiB. Negligible against Orin-C's 64 GiB unified memory and current obs+utility footprint.

### Backup grade

Per [`pgbackrest.md`](../design/pgbackrest.md) §1, pgBackRest exists for properties that operator IdP DB does not need:

| Property | Site Hydra DB | Operator IdP DB |
|---|---|---|
| PITR / WAL replay | ✅ critical (corruption recovery) | ❌ none — 24h RPO is fine |
| Restoring from corruption | ✅ critical (Spock replicates corruption) | ❌ low — DB <1 MB, rebuild from Ansible role + operator self-recovery |
| On-disk corruption detection | ✅ valuable | ⚠️ marginal — page checksums cover this |
| Restore speed at scale | ✅ valuable | ❌ none — DB <1 MB |
| Incr/diff | ✅ valuable | ❌ none — full pg_dump trivial |

`pg_dump` cron + age-encrypt + retention via `find -mtime +N -delete` is the correct grade.

### Network exposure

New mesh DNS records `oauth2.ops.<site>.<mesh zone>` + `auth.ops.<site>.<mesh zone>` → Orin-C wg-cp0 IP. Internal X.509 CA leaf cert per [ADR 0407](0407-internal-x509-ca-for-service-tls.md), issued via the Block 22J ceremony covering both subdomains under one multi-SAN cert. Operator-IdP serves only on the wg-cp0 mesh; no public DNS.

### Block 21 sub-task split

This ADR creates Block 21H:

- **21H.1** — Operator-IdP infrastructure on Orin-C (Postgres + Kratos + Hydra Ansible roles; pg_dump cron; cert ceremony for new SAN)
- **21H.2** — Operator-IdP identity bootstrap (initial operator enrollment, FIDO2-only credential method, role schema: `obs-admin` / `obs-readonly`)
- **21H.3** — Wire obs-host trio (Grafana + OpenObserve + MinIO) against operator IdP; preserve local break-glass admin
- **21H.4** — Wire utility stack (Forgejo, Ollama UI) against operator IdP

Sequenced before the 21D obs-trio wire-up since the IdP must exist before consumers wire to it.

### Cross-site federation impact

None. Operator IdP is per-site, not federated. Operators at multiple sites carry one FIDO2 token registered separately at each site's operator IdP RP. Site Hydra's existing federation properties unchanged.

## Open questions

- ~~**Issuer URL hostname.**~~ Resolved: `oauth2.ops.<site>.<mesh zone>`. The `ops` subdomain matches the "operator IdP" framing semantically (operator-only auth surface) and reserves `obs.<site>...` for observability *services* rather than an *auth surface*.
- ~~**Hydra DB choice — Postgres vs SQLite.**~~ Resolved: Postgres. Consistency with the Ansible role pattern (existing Postgres role; new role `operator-idp-postgres` clones the shape). SQLite would be smaller for ~5 OAuth2 clients but breaks operational mental-model parity.
- **Total-token-loss recovery.** If every operator loses their FIDO2 token simultaneously (theft, fire), the local-admin break-glass on each obs service is the path back. Operator-IdP itself has no FIDO2-token recovery — operator IdP is rebuilt from Ansible + initial enrollment ceremony repeats. Documented in operator runbook in 21H.2.
- **Multi-site operator continuity.** The same operator at several sites carries the same FIDO2 token registered separately at each site. Future enhancement: HQ IdP layer (per ADR 0215) that federates upstream of all per-site operator IdPs. Out of scope here.
- **What about Prometheus / Alertmanager?** Prometheus and Alertmanager UIs do not natively speak OIDC. We either (a) front them with Traefik-ForwardAuth → operator IdP (pattern matches the existing dev-tier Authelia setup; works), or (b) keep them on local creds + IP allowlist via wg-cp0 mesh membership. Decided in 21H.3.

## Cross-refs

- [ADR 0411](0411-ory-idp-mvp.md) — Ory product selection (Kratos + Hydra). Same product family at operator IdP.
- [ADR 1009](1009-ory-shared-network-nid-preseed.md) — shared NID is a property of site Hydra (Spock-replicated). **Does not apply** to operator IdP — each per-site operator IdP has its own NID.
- [ADR 1010](1010-ory-on-spock-mindful-operation.md) — single-active operating pattern is for site Hydra. **Does not apply** to operator IdP — single-instance, no replication, no failover.
- ADR 0826 — Orin-C role expansion. Amended in same PR to add operator IdP.
- [ADR 0407](0407-internal-x509-ca-for-service-tls.md) — TLS leaf cert source for operator IdP issuer URL.
- [ADR 0822](0822-wide-event-observability-otlp-openobserve.md) — observability stack design; consumer set under §4 wired to operator IdP per this ADR.
- [`docs/design/pgbackrest.md`](../design/pgbackrest.md) §1 — backup grade rationale referenced under §"Implications/Backup grade."
- `docs/design/identity-architecture.md` — amended in same PR to add operator-identity facet.
- `docs/project/implementation-plan.md` Block 21H sub-tasks (added in same PR).

## Glossary

See `GLOSSARY.md`.
