# ADR 0814 — Two-layer proxy — Traefik TLS + Caddy reverse

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Related | [`0813-service-registry-pattern.md`](0813-service-registry-pattern.md), [`0312-dns-two-tier.md`](0312-dns-two-tier.md) |

## TL;DR (for a layperson)

We use two reverse-proxy layers in front of hosted services.
Traefik handles TLS termination via Let's Encrypt; Caddy
handles routing to specific services. Two layers because
Traefik is excellent at TLS-DNS-01 challenge automation and
Caddy is excellent at simple service-routing config; combining
them gives us the strengths of both.

## Context

A single proxy layer either does TLS well (Traefik, Envoy) or
configuration-as-code well (Caddy, NGINX). The combination
gives us:

- **Traefik** at the edge: Let's Encrypt DNS-01 via
  Cloudflare API for `*.netbird.example.net` wildcard
  cert, automatic renewal.
- **Caddy** behind: simple per-service reverse proxy from
  `services.yaml` ([`0813`](0813-service-registry-pattern.md));
  static-file serving for HTML-only services; mesh-peer
  forwarding for backend-on-peer services.

## Decision

Two-layer proxy:

| Layer | Tool | Role |
|---|---|---|
| **Edge** | Traefik | TLS termination via Let's Encrypt DNS-01 (Cloudflare API). |
| **Reverse** | Caddy | Per-service routing, static-file serving, mesh-peer forwarding. |

Properties:

- **TLS-only at edge**: HTTPS only on `*.netbird.example.net`.
- **No public DNS for service hostnames**
  ([`0312`](0312-dns-two-tier.md)).
- **Caddyfile generated** from `services.yaml`
  ([`0813`](0813-service-registry-pattern.md)).
- **Health checks** at Traefik for upstream Caddy and at
  Caddy for upstream service.

## Alternatives considered

- **Single-layer (Traefik only).** Per-service Traefik
  config is heavier than Caddy's. Rejected.
- **Single-layer (Caddy only).** Caddy's Let's Encrypt
  DNS-01 ergonomics are weaker than Traefik's at our
  scale. Rejected.
- **NGINX + cert-manager.** Heavier operationally.
  Rejected.

## Consequences

**Positive.** TLS automation is reliable. Service routing
is cheap. Generators produce both layers' configs from
`services.yaml`.

**Negative.** Two layers to operate. Acceptable;
each is minimal.

## Assessor view (SCA-R)

Two-layer proxy serves SC-8 (transmission confidentiality
— TLS at edge), SC-13 (cryptographic protection — TLS),
and AC-4 (information flow enforcement — per-service
routing). DNS-01 challenge means certificates can be
issued without exposing services to the public internet.

## Trigger conditions for revisit

- A specific service requires features (mTLS, OIDC) that
  the current two-layer stack doesn't handle ergonomically.
  Add per-service exception; do not collapse.

---

*See `GLOSSARY.md` for terms and acronyms.*
