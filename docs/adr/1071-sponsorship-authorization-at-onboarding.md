# ADR 1071 — Sponsorship authorization decided at onboarding

**Status:** **Accepted (2026-07-08).** The portal serves the gates; sponsors are granted via the org-only lever, verified on `GET /api/orgs`, and cohort manifests carry the `sponsorship:` block. Endpoint behavior live-verified (fresh restart + max_tier validation); the gate logic itself is httptest-verified. `goat admin sponsorship` ships with goat-cli (the API lever works directly). Design: [`three-stage-candidate-onboarding.md`](../design/three-stage-candidate-onboarding.md) §"Sponsor eligibility".
**Related:** [ADR 1062](1062-three-stage-delegated-candidate-onboarding.md) (the three-stage flow this gates) · [ADR 1061](1061-operator-onboard-intake-system.md) (the admin queue) · ADR 1043 (capability is never named after affiliation) · F-207 (per-person attribution) · [ADR 1072](1072-sponsor-scoped-onboarding-console.md) (what a sponsor *sees*: own-invitee scoping for non-escalated sponsors on the console-owned onboarding engine).

## Plain-language summary

Until now, anyone with any portal credential — every onboarded org and person — could mint a candidate-onboarding invite and vouch for a submission, because ADR 1062 deliberately kept "proposing" open and relied on the operator-gated stage-3 mint as the real gate. This ADR makes sponsorship an explicit decision made **when a person is onboarded**: are they allowed to bring others in, and up to what role level? Admin-tier people (IAM Admin, Platform Admin) get it by default. Everyone else gets it case-by-case, annotated on their identity, and never above their own level.

## Context

ADR 1062's three-stage flow authenticates sponsors but does not authorize them: `POST /api/onboard/invite` and the stage-2 `sponsor-approve` accepted any authenticated principal. On a partner-onboarding sandbox goatnet that means any onboarded partner org could invite and vouch for new candidates at any non-escalated tier. The two human gates still preceded any mint, so this was contained, but the *vouching* stage carried no notion of who is trusted to vouch. Operationally, the sponsoring role on such a goatnet is typically performed by the host organization's personnel, with deliberate extension to others — a policy the system had no way to express.

## Decision

**Sponsorship is a per-identity authorization decided at onboarding: `sponsorship: {authorized, max_tier}`, default-on for the escalated admin tiers at their own level, case-by-case for everyone else, and never above the sponsor's own tier.**

- **The annotation lives on the identity's portal registry row** (`vendors.sponsor_authorized` + `sponsor_max_tier`), written at mint from the manifest's optional `sponsorship:` block (both mint paths: goat-cli native and `operator-onboard.yml` Phase 4.0). The manifest schema gains the block; the intake tier presets set it by default for `iam-admin` (max `iam-admin`) and `platform-admin` (max `platform-admin`).
- **Operator principals are sponsorship-authorized by definition, uncapped.** The backend super-user keys are the platform-admin tier's out-of-band credential; gating them on a registry row would gate the gatekeeper.
- **Tier cap = at most your own level.** Tiers carry an explicit privilege `rank` (browse 1 < developer 2 < iam-admin 3 < platform-admin 4) in the intake form JSON (the single tier-preset source). A sponsor's cap is stamped server-side onto every invite they mint (`prebound.sponsor_max_tier`, never client-set), so the enrollee's tier menu and submitted tier are bounded by the same ceiling; the stage-2 vouch compares the proposal's recorded tier against the voucher's cap.
- **Fail closed everywhere:** an unannotated principal cannot mint or vouch; an unknown tier never passes a cap check; an authorized identity with no recorded max is capped at the least-privileged tier; granting via the lever *requires* a valid max_tier (the level cap is part of the decision, never implicit).
- **Case-by-case lever:** operator-gated `POST /api/orgs/{id}/sponsorship` (`goat admin sponsorship <id> --max-tier <t>` / `--revoke`), audited per-person via `actorOf`/F-207.
- **Who typically holds it is doctrine, not code.** On a sandbox goatnet the sponsoring role is normally the host organization's personnel, deliberately extensible to others. Per the identity grammar (ADR 1043), capability is never keyed to affiliation — so this is recorded as guidance in the design doc and expressed operationally by *who gets the annotation*, never as an affiliation-conditioned rule in the gates.
- **Escalated-tier scrutiny (ADR 1062 Amendment 1) is unchanged and composes:** a platform-admin-capped sponsor may offer `platform-admin`, and the invite/proposal is still flagged `escalated` for extra scrutiny at both review gates.

## Alternatives considered

- **Keep vouching open, rely on stage-3 + audit (status quo).** Rejected by policy: sponsorship should be a deliberate grant, not a side effect of having any credential.
- **Gate on an Ory group instead of a registry annotation.** The invite/vouch surfaces authenticate with portal bearer tokens, not Ory sessions; an Ory-group gate would bolt a second auth path onto endpoints that must also serve the CLI/API. The registry row is where the principal already resolves.
- **Gate on affiliation (e.g. `host-org` may sponsor).** Violates the identity grammar's core rule — capability named after affiliation — and breaks the moment sponsorship is extended.

## Consequences

- **+** "Who may bring people in, and to what level" is now explicit, queryable (`GET /api/orgs` exposes the annotation), auditable, and revocable — decided at the same moment the identity itself is decided.
- **+** Delegation stays real: a developer-tier partner annotated as a sponsor can still run their own cohort's intake up to developer, without an admin in the loop until stage 3.
- **−** Existing org/person principals lose invite-mint/vouch until annotated — deliberate (fail-closed), and the operator surfaces keep working since operator keys are default-authorized. Grant on demand via `goat admin sponsorship`.
- **−** The id-admin-console's onboarding pages call the portal with an operator key, so console-minted invites are uncapped regardless of the logged-in admin's own tier. Acceptable today (the console is ADMINS-gated = the top tier); folding the per-person Ory identity through is the already-tracked F-207 web half.
- Sponsored-tier history rides the proposal (`tier` is now denormalized onto the queue row), so the admin queue can render what level a vouch would grant at a glance.
