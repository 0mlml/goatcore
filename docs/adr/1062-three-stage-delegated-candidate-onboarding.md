# ADR 1062 — Three-stage delegated candidate onboarding (candidate self-registers · sponsor vouches · admin mints)

**Status:** Proposed 2026-07-01.
**Anchor design:** [`docs/design/three-stage-candidate-onboarding.md`](../design/three-stage-candidate-onboarding.md).
**Builds on / extends:** [ADR 1061 — operator onboarding intake system](1061-operator-onboard-intake-system.md) (the *collapsed* stage 2+3 this splits apart) · [ADR 1051 — identity descriptor standard](1051-identity-descriptor-standard.md) (the record extends its spine/dynamic split) · [ADR 1058 — relay metrics self-publish → mesh ingest](1058-relay-metrics-self-publish-ingest.md) (the public-edge→mesh token-gated ingest precedent) · ADR 1043 — identity grammar (the pre-bound tier/affiliation vocabulary) · [ADR 1024 — goat-cli internationalization](1024-goat-cli-internationalization.md) (the stage-1 `.ftl` catalog reuse).

## Context

ADR 1061 built an onboarding intake system where a **sponsor types everything** about the person being onboarded — their name, email, and identity details — then an operator approves and mints. That is a *collapsed* stage 2+3: the person who vouches and the person who authors the candidate's details are the same party, and the candidate being onboarded never touches the system.

Three problems follow from the collapse:

1. **The candidate's own details are authored by someone else.** The sponsor guesses at the candidate's name spelling, email, and personal particulars. For external partners (people from partner organizations and nations), the sponsor often does not have this right, and the candidate cannot self-correct their own record in their own language.
2. **There is no way for an external, not-yet-on-the-mesh person to contribute to their own onboarding.** Everything in ADR 1061 lives behind portal auth on the private mesh. A partner who is being invited has, by definition, no mesh identity yet.
3. **Vouching and authorship are conflated.** "I know who this person is" (authorship) and "I vouch that they should be onboarded" (sponsorship) are different acts by potentially different people, and the collapsed flow cannot separate them.

The three-stage flow separates **authorship** (the candidate), **vouching** (the sponsor), and **minting** (the admin) into three parties across three trust planes.

This is distinct from the **partner peering portal** (partner-peering-portal.md, Block 84): that surface peers a partner's own **Zenoh router** (fabric-to-fabric) and authenticates its requester via federated BYOIDC on the private mesh. This ADR onboards a **person** (and, downstream, their machines) who has no identity yet, via a public one-time link. The two do not overlap and must not be conflated — one is router peering, the other is person enrollment.

## Decision

Split onboarding into three stages across three trust planes:

| Stage | Plane | Who | Auth | What happens |
|---|---|---|---|---|
| **1 — Candidate self-register** | **public internet** | the candidate (external, not yet on the mesh) | a sponsor-minted **one-time presigned link**, dead after submit | fills *their own* identity, in their language; submission forwarded inward |
| **2 — Sponsor review** | private mesh | the sponsor | Ory login | reviews the candidate's submission, approves |
| **3 — Admin review + mint** | private mesh | admins only | Ory `ADMINS` / `IAM-ADMIN` | final review, executes the mint (the existing ADR 1061 admin queue → `goat admin onboard`) |

Five decisions are locked:

1. **The public edge is a thin, stateless "front desk"; the portal stays mesh-only.** The candidate page is served by an **additive route on the existing enrollment ingress** (the relay/rathole `:443` path), and it **forwards the submission inward** — it holds no registry, no secrets, and stores nothing long-term. This reuses the ADR 1058 pattern (a token-gated mesh ingest reached over the relay's rathole path). The portal — which holds the registry and every operator surface — is **never** exposed to the public internet. This is a first-class invariant and the first *intentional* public non-enrollment surface on a goatnet; it conforms to the goatnet public posture (only enrollment-shaped, token-gated routes ride the public path; everything else is mesh-only + `mesh-only-allowlist`).

2. **The invite pre-binds the access level; the candidate fills identity only.** When a sponsor mints an invite they set tier / affiliation / site / machine-enrollment-quota. The public form asks the candidate only *who they are* (name, email, personal particulars). **A candidate can never self-select a tier or affiliation** — those ride the server-held invite, never the public form. **→ Superseded by Amendment 1 (2026-07-01): the sponsor's pre-fill becomes *optional* and the enrollee may fill any left-open field, with a hard carve-out for escalated admin roles. See "Amendment 1" below.**

3. **Single sponsor approval, any tier — no required cross-validation.** One sponsor approving moves a candidate to the admin queue, for *every* tier including the admin tiers. The **admin/operator is the effective second pair of eyes** at stage 3, so a mandatory second sponsor would be redundant. Optional cross-validation by another sponsor may exist as a feature but never gates.

4. **The stored record extends the identity-descriptor standard** ([ADR 1051](1051-identity-descriptor-standard.md)), not a new one-off schema — so a person carries one consistent, versioned, auditable record from sign-up through minted identity. The review pages *render from it*; it is the programmatic source of truth. The intake-time record is a `v-draft` instance of the descriptor: the pre-bound fields populate the dynamic-attributes half (affiliation/site/role-tier), the candidate submission populates the person particulars, and the eventual mint fills the persistent spine (UUID anchor + cert binding).

5. **Candidate PII protection (v1) = in-transit TLS + no edge storage + encrypted-at-rest in the portal.** Browser-side (client) encryption to a portal key before the data leaves the candidate's machine is a **later hardening**, not v1.

### Invite token lifecycle (the "presigned one-time link")

Mirrors the portal's existing path-token patterns (the `vendor-bundle` one-time download token + the audit-presign short-TTL signer). A sponsor (mesh, Ory-authenticated) calls `POST /api/onboard/invite` with the pre-bound fields → the portal mints a **single-use, TTL'd** invite and returns `https://intake.<public>/?t=<token>` to hand to the candidate. The candidate opens it (public) → the front desk shows the i18n form → the candidate submits → the portal **validates + consumes** the token (single-use) and stores the record as a `submitted` proposal → the link is now dead. States: `invited → submitted → sponsor-approved → admin-approved → onboarded | rejected | expired`.

The token is an opaque high-entropy secret; the portal stores only its hash (the same posture as the vendor api-key hash), so a database read never yields a usable link. Single-use is enforced by durable state (`consumed_at`), not by signature alone, so a replay after submit fails closed.

## Alternatives considered

- **Keep the ADR 1061 collapsed flow (sponsor types everything).** The status quo. Cannot let the candidate author their own details, cannot reach an off-mesh partner, and conflates vouching with authorship. This ADR is precisely the un-collapse.
- **Expose the portal itself to the public internet for stage 1.** Rejected outright — it would put the registry, operator surfaces, and mint machinery one Traefik rule away from the open internet, violating the "no secure-plane component on the public internet" invariant. The thin stateless front desk exists so the portal need never be public.
- **A public account the candidate creates (username/password / self-service Kratos registration).** Heavier, introduces a public credential store and its whole attack surface, and still would not pre-bind tier/affiliation safely. The one-time link carries exactly the authority needed and dies on use.
- **Let the candidate self-select tier/affiliation on the form (operator corrects later).** Rejected — capability must never originate from the untrusted public party. Pre-binding on the server-held invite makes over-selection structurally impossible.
- **Mandatory two-sponsor cross-validation for admin tiers.** Rejected as redundant: the admin at stage 3 is already the second (and higher-authority) reviewer. Optional cross-validation stays available but non-gating (decision 3).
- **A new bespoke intake record schema.** Rejected — it would fork identity representation and lose the through-line from sign-up to minted identity. The record extends the ADR 1051 descriptor instead (decision 4).
- **Browser-side client encryption of PII in v1.** Deferred, not rejected: worthwhile hardening, but v1's in-transit-TLS + no-edge-storage + encrypted-at-rest is the proportionate first bar; client-side encryption is a follow-on (decision 5).
- **Conflate with the partner peering portal (Block 84).** Rejected — different problem (router peering vs person enrollment), different auth (federated BYOIDC vs one-time public link), different plane posture. Kept distinct.

## Consequences

**Positive:**
- The candidate authors their own identity, in their own language, correcting the class of errors a sponsor guesses wrong — especially for external partner-nation onboarding.
- An off-mesh, not-yet-enrolled person can contribute to their own onboarding without any mesh identity, via a link that dies on use.
- Authorship / vouching / minting are three separated acts with three distinct actors and audit trails.
- Capability (tier/affiliation) can never originate from the untrusted public party — it rides the server-held invite.
- One consistent identity record (ADR 1051 descriptor) carries from sign-up to mint; the review pages render from it.

**Negative / cost:**
- The first intentional public non-enrollment surface on a goatnet — it must be built and reviewed against the public-posture contract (stage 2 in the build sequence pairs with a security review). It is stateless and token-gated by construction, but it is new attack surface.
- New persisted state (the invite store) and a new public front-desk component to deploy on the enrollment ingress.
- More moving parts than the collapsed flow — justified by the three-party separation, but a real complexity add against the budget.

**Neutral:**
- The mint path is unchanged — stage 3 is the existing ADR 1061 admin queue → `goat admin onboard`. No new mint credential surface (the fully-automated `onboard-runner` remains the separate ADR 1061 fast-follow).
- The one-time invite is inert authority to *submit a draft*, not to mint; a consumed invite yields a `submitted` proposal, which is as inert as any ADR 1061 proposal.

## Build sequence

1. **Invite mint + token lifecycle** (portal, mesh) — `POST /api/onboard/invite` (Ory/operator-gated) + single-use TTL token + consume-on-submit + the record store (extend the descriptor). Testable without any public surface. **← this ADR lands with step 1.**
2. **Stage 1 public collector** — the thin front-desk route on the enrollment ingress + the i18n candidate form + token-gated inbound POST → portal ingest. The load-bearing security piece; pair with a security review against the goatnet public-posture contract.
3. **Stage 2 sponsor review** — mesh page (Ory), sponsor sees candidates they invited, approves; optional non-gating cross-validation.
4. **Stage 3 admin** — wire sponsor-approved records into the existing ADR 1061 admin queue → `goat admin onboard`.
5. **i18n** — fold the ~30 existing `.ftl` catalogs into stage 1.

## Relationship to delegated machine-enrollment

This is the *person* half of [onboard-tier-role-model.md](../design/onboard-tier-role-model.md) §5. The **machine-enrollment quota** the sponsor pre-binds in the invite (decision 2) is the standing authority for that person to bring machines online later, late-bound. The machine self-service mint (person → their systems, a custody edge) is a downstream flow to spec separately once this person-flow exists.

## Amendment 1 (2026-07-01) — minimal-burden sponsorship + escalated-role exception

Decision 2 originally required the sponsor to pre-bind the access level and forbade the candidate from touching tier/affiliation. Operating it surfaced the opposite pull: **the sponsor should carry as little form-filling burden as possible.** So decision 2 is revised:

1. **Every sponsor-set field is optional.** A sponsor may mint a fully-blank invite (`{}`) — a bare "come onboard" link — or pre-fill any subset of tier / affiliation / site / machine-quota. Nothing is required of the sponsor beyond issuing the link.
2. **The enrollee may fill anything the invite left open** — tier, affiliation, site, and their identity — so the burden shifts to the person who actually knows their own details. A field the sponsor *did* set is **locked** (the enrollee cannot change it); a field left open is the enrollee's to fill.
3. **This principle is identical across all three surfaces — API, CLI, and UI** — because it is enforced in one place: the portal API (`/api/onboard/invite` accepts any-subset; `/api/onboard/preview` reports locked-vs-open; `/api/onboard/submit` accepts only the open fields). The CLI and the web UI are thin clients of that contract and inherit the rule by construction, not by re-implementation.
4. **The safety model is unchanged and still sound:** nothing is minted from an enrollee-supplied value without **two** human gates (sponsor review + admin review). An enrollee proposing "developer" for themselves is a *draft* two people must approve — not a grant.

**The escalated-role exception (the one hard carve-out).** The two backend-admin tiers — **`iam-admin`** and **`platform-admin`** — are *escalated*. For them:

- **Sponsor-offered only.** An escalated tier can enter a flow **only** because a sponsor set it in the invite. The enrollee's form never offers `iam-admin`/`platform-admin` as self-selectable, and `/api/onboard/submit` **refuses** an enrollee-supplied escalated tier (fail closed). Capability this powerful never originates from the untrusted party — the original decision-2 invariant, kept exactly where it matters.
- **Flagged for scrutiny at both gates.** An invite/proposal carrying an escalated tier is marked `escalated: true`. That flag is surfaced prominently when the **sponsor** offers it (a deliberate "you are granting admin" confirmation) and again on the **admin** review queue (a badge that says *review this one harder*). Both the sponsor-acceptance and admin-acceptance stages see the escalation; neither can miss it.

**Approvability correction (built with this amendment).** For the flow to complete, a consumed invite must land as a **valid, lintable operator manifest**, not the abstract `v-draft` record of decision 4. `/api/onboard/submit` now assembles the effective manifest server-side — applying the chosen tier's preset expansion (sourced from the same `onboard-intake-form.json` hints the ADR 1061 form uses, so no second mapping) over the effective (sponsor-locked ⊕ enrollee-filled) fields plus identity — so the admin queue's re-lint passes and `goat admin onboard` accepts it. Provenance (which party set each field, the `invite_id`, the sponsor) is retained alongside.

**Prefilled fields are shown, not hidden — the enrollee may petition (2026-07-01 refinement).** A field the sponsor locked is **shown to the enrollee prefilled + read-only**, not omitted. The enrollee can take an explicit extra action ("request a change") to propose a different value — recorded as a **petition** (`{field, sponsor_value, requested_value}` on the proposal), **never auto-applied**: the effective manifest keeps the sponsor's value. Petitions are **badged for scrutiny at both review gates** (the admin queue flags "change request" + shows the diff; the sponsor-review stage will too). An enrollee can never petition *into* an escalated tier — the escalation refusal covers petitions as well as open values. The enrollee form also carries the **meaningful identity fields** (name, email, principal-type, organization, affiliation kind + nation, tier) rather than only name/email, so the enrollee can complete their own record; operator-plumbing fields (NetBird groups, Zenoh cert toggles, setup-key reuse) stay off the candidate surface. The redundant `site` field is dropped from the sponsor form (site = the goatnet = `env` for onboarding; the assembler derives it), and the shared image no longer hardcodes a goatnet — the form derives `env` from the portal hostname.
