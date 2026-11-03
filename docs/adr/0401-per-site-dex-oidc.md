# ADR 0401 — Per-site Dex as IdP-abstract OIDC layer

| Field | Value |
|---|---|
| Status | Accepted — **marked for supersession by 0411 (Proposed, pending spike)** — see "Superseded in part" addendum below. Strict ADR lifecycle ([`0002`](0002-adr-lifecycle.md)) requires the superseding ADR to reach Accepted before this one can be marked Superseded. Until then this ADR remains Accepted with the structural decisions still load-bearing and the *product choice* (Dex vs Ory) flagged for change once 0411 lands. |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Superseded by | [`0411-ory-idp-mvp.md`](0411-ory-idp-mvp.md) (in part — IdP product choice) |
| Related | [`0206-island-mode-by-construction.md`](0206-island-mode-by-construction.md), [`0805-policy-engine-active-passive.md`](0805-policy-engine-active-passive.md), [`0410-go-cli-peer-enrollment.md`](0410-go-cli-peer-enrollment.md), [`0411-ory-idp-mvp.md`](0411-ory-idp-mvp.md), `0412-spiffe-spire-workload-id.md`, `../design/identity-architecture.md`, `../project/idp-strategy-spike-2026-04-25.md`, `../project/dex-vs-external-oidc-spike.md` |

## Superseded in part — addendum 2026-04-25

This ADR's **product choice** ("per-site Dex backed by Kanidm") is
superseded by ADR 0411 (per-site Ory Kratos + Hydra, Postgres-backed)
following the 2026-04-25 IdP strategy spike that widened scope from
Dex-vs-external to "all identity domains across all services in the
network." Operator hands-on signal disqualified Zitadel (recalcitrant
in practice); Ory's modular Kratos + Hydra architecture was selected
as the MVP IdP product.

The **structural decisions** of this ADR are preserved:

- ✅ Per-site (not central) — preserved by Ory deployment model.
- ✅ Island-mode-compliant — preserved.
- ✅ IdP-abstract via standard OIDC claims — preserved (Ory speaks
  the same OIDC, services consume `sub` / `email` / `groups` agnostically).
- ✅ Federation-capable upstream — preserved (Ory Kratos has OIDC
  connectors).
- ✅ Trigger conditions for revisit — preserved (admin count > 5,
  partner federation, failed island-mode drill).

**What changed:**

- Product is **Ory** (Kratos + Hydra), not Dex. Postgres-backed (not
  Dex-stateless + Kanidm-backed). See ADR 0411 for the product choice.
- **Peer enrollment is IdP-free.** ADR 0410's Go CLI removes
  netbird's IdP requirement at the API layer, decoupling peer
  enrollment from any IdP product entirely. The IdP is for **humans
  + service accounts** across the broader service portfolio.
- **Workload identity** is a separate domain handled by SPIFFE/SPIRE
  per ADR 0412 (Phase 3+).

The full coherent picture lives in
`../design/identity-architecture.md`.

## TL;DR (for a layperson)

Several services in our system need OAuth/OIDC authentication
(the policy engine UI, internal admin tools, future federations).
Rather than depend on a central external identity provider — which
would break if the WAN went down — every site runs its own
lightweight Dex instance. Dex acts as a local OIDC layer that can
front any underlying identity store (Kanidm today, others later).
Switching identity providers is a one-line config change because
Dex absorbs the difference.

## Context

OIDC adoption in the system has two requirements:

1. **Island Mode.** Authentication must work without WAN
   reachability ([`0206`](0206-island-mode-by-construction.md)).
2. **IdP abstraction.** We will likely change identity stores
   over time (Kanidm now, possibly DoD-federated later).
   Services should not bake in IdP-specific assumptions.

Dex meets both: it is a small Go-based OIDC provider that runs
locally, federates upstream IdPs as backends, and speaks the
standard OIDC protocol with services.

The full evaluation (Dex per-site vs external OIDC) is at
`../project/dex-vs-external-oidc-spike.md`.
The decision stays per-site Dex unless three triggers fire
(>5 admins, partner federation requires central IdP, failed
island-mode drill).

## Decision

Per-site Dex (one instance per site, on the mgmt-host pair) is
the OIDC layer:

- **Backend:** Kanidm ([`0402`](0402-kanidm-for-accounts.md))
  as the user store. Other backends supported via Dex
  connectors.
- **Abstract claims:** services rely on the standard OIDC
  claims (`sub`, `email`, `groups`) only. Switching IdP is
  one config field.
- **DNS:** `auth.<site>.netbird.<zone>` per site.
- **Tokens:** signed with site-local Dex keys; rotated on a
  defined cadence.
- **Federation:** Dex can federate upstream (e.g. to a DoD
  IdP) as a backend; the trigger conditions in
  `../project/dex-vs-external-oidc-spike.md`
  govern when to flip.

## Alternatives considered

- **Single central OIDC (Keycloak / Authentik / Ory).** Single
  point of failure; violates Island Mode. Rejected as primary.
- **No OIDC; rely on FIDO2 + per-service auth.** Service
  integration cost grows with each new service. Rejected.
- **DoD PKI / CAC SAML federation directly into services.**
  Loses partition tolerance; ties services to a specific IdP.
  Rejected.

## Consequences

**Positive.** Each site authenticates locally. IdP abstraction
preserves swap optionality. Standard OIDC claims keep services
portable.

**Negative.** One Dex instance per site is more state to keep
healthy. We accept; the cost is small and the property is
load-bearing.

## Assessor view (SCA-R)

Per-site Dex serves IA-2 (identification and authentication),
AC-2 (account management), and CP-2 (contingency — local
authentication under partition). NIST SP 800-63B (digital
identity guidelines) authenticator-assurance levels apply
unchanged — Dex is a relying-party-side OIDC layer, not the
authenticator.

## Trigger conditions for revisit

- Admin count exceeds 5 across the fleet.
- A specific federation partner requires central IdP that we
  must federate into rather than alongside.
- A failed island-mode drill points to Dex-side failure (rare
  but possible).

---

*See `GLOSSARY.md` for terms and acronyms.*
