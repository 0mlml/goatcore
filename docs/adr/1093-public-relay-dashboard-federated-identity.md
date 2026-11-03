# ADR 1093 — Public relay-health dashboard: federated identity, credentialed onboarding, secondary-identity enrollment

**Status:** Proposed 2026-07-21. Extends ADR 1066 (relay-health cloud break-glass gate) and ADR 1043 / the ICAM federation pattern ([external-icam-federation.md](../design/external-icam-federation.md)). Full design: public-relay-identity-federation.md.

## Context

The public relay-health dashboard (ADR 1066) is off-mesh break-glass, so its identity broker is the **public Dex** (in the platform infra repo), not the mesh Ory. It authenticates via Dex upstream connectors and authorizes via an explicit email allowlist derived from the active Kratos roster (interim, ADR 1066).

Requested: (1) give every new user working dashboard access as part of onboarding, surfaced in the welcome email; (2) let a user bind a secondary login (second email / another SSO / PKI); (3) offer CAC/PIV, login.gov, and Entra as login methods — "like we did for Ory."

Key finding from mapping the mesh ICAM pattern: the mesh CAC login **is already Dex-based** — nginx-mTLS → EDIPI header → a Dex `authproxy` connector → OIDC (`icam-mtls-proxy` role, live-fired with real CAC). So "do it with Dex" is coherent and reuses a proven pattern rather than duplicating or fighting the identity direction.

## Decision

1. **Keep the public relay dashboard on the public Dex plane** (do not route it through mesh Ory) — break-glass requires an off-mesh IdP.
2. **CAC/PKI:** mirror the `icam-mtls-proxy` pattern onto the public Dex — an nginx-mTLS front + a Dex `authproxy` connector, in the platform infra repo, with **production CRL/OCSP** (the public plane is internet-facing) and **SAN-otherName-UPN** cert parsing (not the sandbox CN regex).
3. **Entra:** enable the Dex `microsoft` connector.
4. **login.gov:** integrate via an **intermediate IdP** that speaks `private_key_jwt` (the proven `icam-keycloak-cac` pattern), federated into Dex as an upstream `oidc` connector — because stock Dex cannot do `private_key_jwt`. Gated on an external GSA service-provider registration + an IAL/AAL policy decision.
5. **Authorization:** keep the ADR-1066 email allowlist as the interim boundary; converge to a **`RELAY-HEALTH` group claim** (Kanidm group + Dex `groups` scope) as the durable end-state, which also resolves CAC's EDIPI-vs-email and the multi-identity "which email" ambiguity.
6. **Credentialed onboarding:** make "active goatnet participant ⇒ dashboard access" **structural** — the relay-health role re-derives the allowlist from the active Kratos roster on deploy + a scheduled reconcile (preferred over event-driven writes from the Go service). The welcome email gains a relay-dashboard block (URL + login options; for no-SSO users, a Kanidm username + self-service credential-set link — never a literal password, consistent with F-241).
7. **Secondary-identity enrollment:** build it new (none exists) — Kratos `verifiable_addresses` / OIDC-credential-link methods + a `user_detail` admin surface first, self-service later; each additional email feeds the same allowlist/group auto-sync.

## Alternatives considered

- **Route the dashboard through mesh Ory** (which already has login.gov/Entra/CAC config-built). Rejected: mesh Ory is not reachable off-mesh, which would break the dashboard's break-glass purpose.
- **Add `private_key_jwt` to Dex directly** (for login.gov). Rejected as the default: a Dex fork/upstream dependency the repo avoids; the intermediate-IdP path reuses a proven component instead. Revisit if upstream Dex gains the feature.
- **Event-driven allowlist writes from the console** on each approve/revoke. Deferred in favor of roster re-derivation: structural convergence is simpler to reason about and self-heals drift.

## Consequences

- CAC/login.gov/Entra become additive login methods on the public plane; disabling any one never locks out the others (authZ stays at the gate).
- Most of the build lives in the platform infra repo (Dex, Kanidm, nginx-mTLS front, connectors); this repo owns the allowlist/auto-sync, onboarding welcome-email + Kanidm-provisioning client, secondary-identity UI, and reused claim vocabulary.
- **login.gov cannot go live without the external GSA registration** — it is the one hard external dependency; everything else is buildable in-repo/in-infra. Phasing + blockers are tabled in the design doc.
- The internet-facing nginx-mTLS front adds a new `:443` ingress requiring SG + `node-net-health` baseline updates and mandatory revocation checking.
