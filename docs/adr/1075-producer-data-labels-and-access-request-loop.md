# ADR 1075 — Producer data labels (fidelity / context / access lanes) + the discover→request→approve grant loop

| Field | Value |
|---|---|
| Status | Accepted-direction (identity-owner decision 2026-07-09); **built-in-code (2026-07-09)** — the portal grant store + label lanes + request→approve loop + preemptive grants + resolver evaluation + drift telemetry are built and unit/integration-tested in `ops/goat-zenoh-portal`; **deploy still pending** (the portal image redeploy is the rollbackable unit). SoT rows remain the law; nothing claims *live-fired* enforcement until the ZAGROS redeploy lands (follow-ons §1). |
| Date | 2026-07-09 |
| Repo scope | goat core |
| Supersedes | — (extends ADR 1074; subsumes the `synthetic: true` flag into the fidelity lane, additively) |
| Related | [ADR 1074](1074-data-access-role-architecture.md), [ADR 1028](1028-topic-interface-visibility.md) (visibility advisory ≠ access — the discoverability half was already decided), [ADR 1027](1027-partner-release-set-governance.md), [ADR 1051](1051-identity-descriptor-standard.md) (effective-dated grant rows), [ADR 1071](1071-sponsorship-authorization-at-onboarding.md)/[1072](1072-sponsor-scoped-onboarding-console.md) (the request→approve workflow template), F-207 (grant auditing) |

> **Amended by [ADR 1208](1208-sovereign-environments-release-namespaces-and-grant-encryption.md) (2026-10-30):** the `access` label is not an access control and means nothing to Zenoh or goat. Grant rows stay, as the audience for grant-keyed encryption.

## TL;DR (for a layperson)

Producers stamp their data streams with three small labels: how real it is (practice / simulated / real), what it's for (development / exercise / operations), and who may see it (everyone on this network / my organization / by-permission-only). Unlabeled data on the sandbox network defaults to everyone-may-see. Every stream is *listed* in the catalog for everybody — seeing that something exists is free; seeing its contents is not — and anyone can click "request access," which the data's producer approves for a whole organization or a single person. Restricting is deliberate (you must say why); opening is effortless. This is the opposite of how the government usually does labels, on purpose: audits of the government's own system show most labels end up wrong and the errors that hurt are the *over-restrictive* ones.

## Decision

1. **Three label lanes, closed enums, topic-namespace grain** (never per-message): `fidelity ∈ {notional, simulated, real}` (subsumes `synthetic: true`, additive migration), `context ∈ {experiment, exercise, operational}`, `access ∈ {community, org, controlled}`. Applied by the producer at portal registration, echoed by the attest schema-decl; fidelity/context are advisory + display; **access is the authz-relevant lane** and maps onto the ADR 1074 modes (community = no grant needed; org = the existing Scoped mode; controlled = explicit grant required).
2. **Permissive default, per-goatnet:** unlabeled data gets `access` = the goatnet's `data_label_access_default` — **community on ZAGROS/sandbox** (the ratified full-access default tag); operational goatnets choose org/controlled. Unlabeled fidelity/context render honestly as *unlabeled*, never fabricated.
3. **`context: operational` exists everywhere for parity** but operational data belongs on operational goatnets — the value on any other goatnet surfaces a loud wrong-fabric flag, never silence.
4. **Discover → request → approve:** every topic's catalog metadata (existence, labels, producer, org) is visible to every authenticated viewer (ADR 1028); any viewer may request access; **the producer approves** (its custodian humans/org — the data-authority principle, data-access-roles.md §8), minting a **grant row** in the registry: `{grantee: org|principal, producer, granted_by, justification, effective [from,to), status}` — F-207-audited, expiry-able (the comrel-window pattern), org-scope preferred at the coalition boundary, principal-scope for exceptions.
5. **Grants are enumerable relations, never rule formulas; no central PDP.** The data-roles resolver evaluates grant rows; surfaces cache registry snapshots with a declared staleness/refresh discipline (mesh revocation stays the instant cut). "Who can see X" and "what can Y see" are answered by *reading rows*.
6. **Friction is on restriction, not openness:** applying `access: org|controlled` requires a recorded justification (the NAVADMIN 060/23 pattern); the non-community label share per goatnet is watched telemetry (ratchet-drift counter). Openness needs no ceremony.

## Research grounding (deep-research round 2026-07-09; 23 claims verified 3-0 against primary sources, 2 refuted, coverage gaps named)

- **Tiny closed vocabulary:** NIST SP 800-162 names the finite, well-known marking set as the tractable labeling pattern, and warns ABAC's attribute/policy management cost can exceed its benefit — the ambitious end of this design space has a two-decade adoption-failure record (NCCoE SP 1800-3a; the Gartner 70%-by-2020 ABAC forecast never materialized).
- **Expect defective labels; default open; label only restriction:** DoD's own audits of the most mature labeling regime in existence found 70% (DODIG-2013-142, n=220) and 84% (GAO-06-706, n=111) marking-defect rates; labelers resolve doubt by ratcheting UP; and the sharing damage came specifically from **erroneous restrictive dissemination markings** (NOFORN misuse — CDSE doctrine; Navy's response was a justification gate). SP 800-178/NGAC's worked example shows universal mandatory marking is the burden, labeling-only-the-restricted the economy.
- **Grants as relations:** SP 800-178: formula/rule policy (XACML-class) cannot answer per-user/per-object review efficiently (NP-complete in general) and its delegation model cannot govern federated administration; relation-graph representations review efficiently and delegate per-element — the producer-administers-own-grants model.
- **No central PDP; cached snapshots:** SP 800-162 §3.3.1 prescribes local attribute caching for disconnected/tactical environments with staleness as the accepted, declared risk. XACML's centralized combining pipeline degrades with total policy count.
- **Coalition:** RAND RR-302 (Afghan Mission Network): interoperability required *relaxing* restrictive releasability regimes, and fragmenting data across many security domains reduces interoperability — favor few domains, org-scope grants for unanticipated partners (SP 800-162's one proven ABAC benefit).
- **Honesty — refuted and unknown:** the claim "permissive defaults *outperformed* fine-grained restriction on AMN" was refuted (0-3) — the evidence supports only "restrictive regimes had to be relaxed"; and the research produced **no surviving verified claims** for cloud tag-explosion, data-mesh approval-workflow bottlenecks, DDS Security field lessons, or IDS/Gaia-X deployments — those lessons rest on analogy, recorded as open questions (design doc §9), and the design compensates structurally (closed lanes can't explode; enumerable expiry-able grants counter stale entitlements).

## Alternatives considered

- **Per-message STANAG 4774/4778-style bound labels.** Rejected for v1: the marking-burden evidence above, at message rates; the stream is the unit of production here. Reserved as the `releaseset` arc's concern if per-object binding is ever demanded.
- **Free-form tags.** Rejected — tag explosion is the known disease; G1 applied to labels.
- **A policy rule language (XACML/OPA-class) for label×grant decisions.** Rejected — un-auditable-by-enumeration, federated-delegation hole, PDP centralization; decisions here are a row lookup plus a lane check.
- **Mandatory labeling (refuse unlabeled publishes).** Rejected — universal marking burden with documented defect rates; the default label carries unlabeled data safely instead.
- **Admin-approved (not producer-approved) grants.** Rejected — the producer is the data-authority (§8); central approval is the documented bottleneck shape and re-creates the operator super-user pool this track is dismantling.

## Consequences

- **+** Every stream is discoverable without being readable; access requests become self-service; approvals are producer-owned, audited, expiring, enumerable.
- **+** The notional default-on experience generalizes: labels make fidelity/context first-class instead of one synthetic flag.
- **+** Restriction leaves a written why; openness stays frictionless — the ratchet bias is countered structurally.
- **−** Built-in-code, deploy-pending: the portal label fields + justification gate, the catalog/request/approve UI, the grant store, resolver grant-evaluation, the preemptive-grant path, and the per-goatnet `data_label_access_default` knob are all built and tested (follow-ons §0c.1–0c.5). Panoscope label badges (§0c.6) are a sibling surface. Until the portal image is redeployed to ZAGROS, these are enforced in code but not yet live-fired; the SoT stays honest per lane (G7).
- **−** Cached-snapshot staleness is an accepted, declared revocation-latency risk (bounded; mesh revocation remains instant).
- **−** Areas the research could not verify (catalog-workflow bottlenecks, tag-scale lessons) may surprise us; open questions are recorded and the v1 surface is deliberately small enough to correct cheaply.
