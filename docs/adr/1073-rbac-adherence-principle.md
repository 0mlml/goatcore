# ADR 1073 — RBAC adherence: the declared group→service matrix is enforced, and every surface scopes to its viewer

| Field | Value |
|---|---|
| Status | Proposed (code + substrate built this track; production live-fire operator-gated) |
| Date | 2026-07-09 |
| Repo scope | goat core |
| Supersedes | the F-132-era posture "vendor-friendly surfaces intentionally open to any authenticated identity" (scoping note in F-132 §Status) |
| Superseded by | — |
| Related | [ADR 1072](1072-sponsor-scoped-onboarding-console.md) (sponsor-scoped console door — composes via the group_vars superset rule below), ADR 1043, [ADR 1025](1025-zenoh-dynamic-tenant-authz-slot-pool.md), [ADR 1071](1071-sponsorship-authorization-at-onboarding.md), [ADR 1067](1067-identity-grammar-generated-sot.md), [ADR 1028](1028-topic-interface-visibility.md), F-132, F-181, F-207 |

## TL;DR (for a layperson)

We have a real identity system — every person logs in and their account says which groups they belong to — but most of our services only ever ask one question: "are you an admin?" If yes, you can do everything; if no, usually nothing — except our visualization tools, which showed the entire network to anyone on it without even asking who you are. We already wrote down, in one file, exactly which group should be allowed to do what on each service. This decision makes that file the law: services must actually check it, a robot check fails the build if a service's door disagrees with it, and the visualization tools must ask who you are and show you only what your groups allow.

## Context

A 2026-07-09 five-way survey of the codebase (`rbac-adherence.md` §2 is the full review) found:

- The only Ory-group front-door fences are binary: `ADMINS` in-or-403 (dashboard, id-console, relay-health) and `COMPLIANCE` (compliance-proxy). The `IAM-ADMIN` group accepted 2026-06-29 gates nothing — the fences never learned it.
- The goat-built inspection surfaces (panoscope, flashlight, stethoscope) are **unauthenticated on the mesh** — Traefik IP-allowlist only, no identity headers, whole-fabric view to any mesh peer. Two of them provision OIDC clients that were never wired into the route.
- The id-console enforces one literal string (`"ADMINS"`) while already loading the full declared matrix into memory for a display page.
- `ops/permissions/group-permissions.yml` declares rich per-group/per-service RBAC and flags its own unenforcement in three places ("CI does not yet enforce parity", "NOT enforced uniformly at L7", "fences currently allow only ADMINS").
- The identity-grammar role family (ADR 1043) is vocabulary + NetBird labels; nothing consults it at runtime. The genuinely role-differentiated services are all third-party apps (grafana/minio/openobserve) doing their own RBAC off the `groups` claim.
- Exactly one goat-built, per-identity, server-enforced capability exists — ADR 1071 sponsorship — proving the pattern (registry-resolved cap, fail-closed, never trust the request) works in this stack.

The identity plumbing needed for real RBAC exists end-to-end (Kratos `groups` traits → Hydra JWT → oauth2-proxy → headers). What's missing is adherence: services consulting the declared policy, and surfaces knowing their viewer.

## Decision

**RBAC is an enforced engineering principle: authorization derives from `ops/permissions/group-permissions.yml`, every human-facing surface knows its viewer, and viewers see what their groups allow — fail-closed, drift caught in CI.**

1. **The matrix is the PDP input.** Goat-built services gate humans by consulting the matrix (`requireAccess(service, level)`), never a hardcoded group literal. First conversion: the id-console `adminGate`.
2. **L7 fences equal the matrix.** An oauth2-proxy `allowed-groups` role default must equal the set of groups the matrix grants that service; per-goatnet group_vars overrides must be a **superset** (never silently drop a matrix-granted group) and may widen the door with per-goatnet groups — surfaced by CI as a notice, never silent — where an in-app matrix gate bounds what they can do (the ADR 1072 sponsor-door pattern). `IAM-ADMIN` is added to the dashboard + id-console fences accordingly.
3. **CI parity ratchet.** `ops/ci/check-group-permissions-parity.sh` fails the build on matrix↔fence divergence; coverage can only grow (the `check-rollback-coverage.sh` pattern).
4. **Inspection surfaces are viewer-scoped.** Panoscope (first), then flashlight/stethoscope: an oauth2-proxy forwardauth with `--set-xauthrequest` supplies identity headers; the app computes a viewer scope (the ADR 1074 lattice: Full / DataAll / Scoped / floor, design doc §3.3) and filters everything it serves. Enforce mode is a default-off per-goatnet flag; once enforcing, missing identity → minimal scope, never full view.
5. **Mesh presence is not identity.** The mesh-only Traefik middleware remains as defense-in-depth but is no longer an acceptable *sole* fence for surfaces rendering fabric-wide or identity data.
6. **Honesty boundary.** This is surface/API authorization, not wire confidentiality: Zenoh reads stay flat per ADR 1025/F-181, and a cert holder can still subscribe directly. Wire-level read scoping (ADR 1027 release-sets, payload encryption) remains separately sequenced; these tiers become its policy input.
7. **Vocabulary stance.** The Ory service groups remain the enforced web/API axis; the ADR 1043 grammar role family remains identity vocabulary projected onto groups by the onboarding tier presets. Unification is ADR 1067 generated-SoT territory, not this decision.

## Alternatives considered

- **Status quo (network fence + per-app defaults).** Rejected — it is the surveyed defect: the declared matrix enforces nothing, and F-132's "intentionally open" scoping is no longer acceptable for surfaces showing the whole fabric.
- **Central PDP now (Ory Keto / OpenFGA / SpiceDB).** Deferred, as identity-architecture already sequences it (Phase 3+) — a policy engine adds a moving part before a single service even consults the policy we have; the matrix-consult pattern migrates cleanly onto a PDP later.
- **Enforce at L7 only (per-group fences everywhere, no in-app scoping).** Rejected for the inspection surfaces — a front-door 403 can't express "show this viewer *their* slice"; the ask is scoped rendering, not just admission.
- **Wire-level read ACLs per role.** Not available in stock Zenoh and contrary to ADR 1025's decided posture; would reopen the router-build boundary (ADR 0858).

## Consequences

- **+** `IAM-ADMIN` (and future groups) become real by editing one YAML file; drift between declared and deployed authorization becomes a build failure, not a doc footnote.
- **+** Panoscope (then siblings) stops being an unauthenticated whole-fabric window; what a viewer sees is attributable and least-privilege.
- **+** The ADR 1071 capability pattern gets a stated generalization path (portal operator tiering, follow-on).
- **−** More moving parts per inspection surface (a sidecar + headers + a flag) — mitigated by default-off flags, the existing sidecar pattern, and config-tier rollback (ADR 1060).
- **−** Participant-tier scoping is only as good as the viewer→org binding; until onboarding stamps `ORG:` groups, participants degrade to observer (fail-closed, may surprise early viewers).
- **−** Display-layer scoping can be mistaken for confidentiality; §6 of the decision and the design doc's honesty boundary must ride along in any external description.
- Built vs live: code + roles + CI land in this track; **nothing is "resolved" until deployed and re-probed live per goatnet**, which is operator-gated and sequenced in the design doc §5.
