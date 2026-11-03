# ADR 0813 — Service registry pattern — `services.yaml` as SoT

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Related | [`0213-convenience-vs-core-services.md`](0213-convenience-vs-core-services.md), [`0814-traefik-caddy-two-layer-proxy.md`](0814-traefik-caddy-two-layer-proxy.md), `../operations/hosting-services.md`, `../design/service-onboarding-plan.md` |

## TL;DR (for a layperson)

Services hosted on the mesh — dashboards, status pages,
small applications — are listed in a single file called
`services.yaml`. From that one file, generators produce
everything downstream: the Caddy reverse-proxy config, the
Caddy Docker Compose definitions, the health-check targets,
and the DNS entries. One source of truth, many derived
artifacts.

## Context

Without a single source of truth, every new service requires
edits in five places (DNS, reverse proxy, health checks,
Compose, docs). The result: drift, missed updates, and
mysterious 404s.

The service-registry pattern collapses the per-service
authoring to one file, with code generation for everything
else.

## Decision

`ops/serve/services.yaml` is the single source of truth for
hosted services:

- **Format**: YAML; one entry per service.
- **Generators**: `ops/serve/generate.py` produces:
  - Caddyfile entries.
  - Caddy docker-compose definitions.
  - Health-check targets for the status page.
- **Consumed by**:
  - Traefik (TLS termination,
    [`0814`](0814-traefik-caddy-two-layer-proxy.md))
  - Caddy (reverse proxy)
  - isitup (status page)
  - smoketest (CI smoke test)
  - mesh DNS (via `publish-service.sh`).

Adding a new service is one entry + one PR; everything else
is generated.

## Alternatives considered

- **Per-service configuration files.** Drift across files.
  Rejected.
- **Dynamic service discovery only.** No human-readable
  registry; harder to audit. Rejected.
- **Wrap into NetBird's group/peer model only.** NetBird
  ACLs handle authorization but not the
  TLS/proxy/healthcheck layer. Rejected.

## Consequences

**Positive.** One file to edit; generators produce the rest.
Auditable inventory. New services are cheap.

**Negative.** Generator must stay in sync with downstream
formats (Caddy, Traefik). Acceptable; the generator is
small.

## Assessor view (SCA-R)

The service registry serves CM-8 (component inventory),
CM-2 (baseline configuration — services are part of the
baseline), and AC-3 (access enforcement — registry feeds
ACL authoring). NIST SP 800-128 baseline-configuration
guidance is well-served by single-source generators.

## Trigger conditions for revisit

- Service count overflows the single-file pattern.
  Per-domain registry files; do not abandon SoT.

---

*See `GLOSSARY.md` for terms and acronyms.*
