# Three-stage delegated candidate onboarding

**Status:** designed — **decisions locked 2026-07-01, ADR [1062](../adr/1062-three-stage-delegated-candidate-onboarding.md) drafted; step 1 (invite mint + one-time token + record store) built.** This is the realized form of the "delegated machine-/person-enrollment" thread from [onboard-tier-role-model.md](onboard-tier-role-model.md) §5. See "Build sequence" for what remains.
> **Engine relocation (in progress):** the onboarding *engine* (the proposal +
> invite store, the mint, review/decide, and the machine/public API realms)
> described below is being **extracted from the data-fabric portal into the
> identity plane (`goat-id-console`)** — see [onboarding-extraction-plan.md](onboarding-extraction-plan.md).
> The three-stage *flow* and trust-plane model here are unchanged; only the
> home of the API/store moves. The portal keeps only its Zenoh cert/slot leg
> (`POST /api/orgs`) + a read-only `GET /api/whoami` for participant-key
> ADR 1071 sponsorship delegation. Endpoint paths below that read
> "the portal" now resolve to the console (candidate-facing URLs unchanged).

**Related:** [ADR 1062](../adr/1062-three-stage-delegated-candidate-onboarding.md) (the decision record) · [operator-onboard-intake.md](operator-onboard-intake.md) ([ADR 1061](../adr/1061-operator-onboard-intake-system.md), the collapsed 2+3 this splits) · [identity-descriptor-standard.md](identity-descriptor-standard.md) ([ADR 1051](../adr/1051-identity-descriptor-standard.md)) · [ADR 1058](../adr/1058-relay-metrics-self-publish-ingest.md) (public-edge→mesh ingest precedent) · [ADR 1024](../adr/1024-goat-cli-internationalization.md) (i18n).

## The path this REPLACED — `operator-onboard.yml`'s enroll leg (retired July 2026)

**Do not hand-author a manifest and run `ops/ansible/operator-onboard.yml` to
enroll somebody. That leg is fenced fail-closed and this section explains why.**

The retired shape: an operator writes `ops/operators/<env>/<handle>.yml` by
hand, runs the play, and the play mints every surface directly and emits an
out-of-band **bundle** — a tarball containing the person's certificate *and
their private key* — for the operator to carry to them.

Two properties make it wrong now, not merely old:

- **An operator ends up holding someone else's private key.** Under the live
  flow the runner mints and the person collects their own credentials; nobody
  hand-carries a key.
- **Nobody vouches.** Stages 1–3 exist to separate authorship (candidate),
  vouching (sponsor) and minting (admin). The play collapses all three into one
  operator and leaves no invite, no sponsor attribution and no candidate record.

### The direction of the manifest is the thing people get backwards

Under the live flow `ops/operators/<env>/<handle>.yml` is an **OUTPUT**. It is
written by `ops/scripts/sync-onboarded-manifests.py` **from** the portal/console
DB, after the runner has already minted, so the declared roster agrees with what
really happened. Every such file says so in its own first line:

```
# zagros — synced from portal proposal prop-<id> (runner-minted; sponsor <id>).
# Written by ops/scripts/sync-onboarded-manifests.py — the portal DB is the
# record of runner mints; this file makes the declared roster agree with it.
```

**If you are copying one of those files as a template, stop.** You are reading a
receipt and treating it as an order form.

### What is still live in that play

**Revoke.** `-e action=revoke` remains the offboarding mechanism (ADR 1031)
until native revoke lands (track `native-revoke`). Only the enroll leg is
fenced; deleting the play outright would break offboarding.

### The correct sequence

```bash
goat admin invite --env <goatnet> …          # console mints the invite
# candidate accepts in goat-id-console
# onboard-runner claims it and mints; the person collects their own credentials
ops/scripts/sync-onboarded-manifests.py --env <goatnet> …   # roster catches up
```

## BLUF

Split onboarding into three stages across three trust planes, so authorship (the candidate), vouching (the sponsor), and minting (the admin) are separate parties:

| Stage | Plane | Who | Auth | What happens |
|---|---|---|---|---|
| **1 — Candidate self-register** | **public internet** | the person being onboarded (external, not yet on the mesh) | a sponsor-minted **one-time presigned link**, dead after submit | fills *their own* identity, in their language; submission forwarded inward |
| **2 — Sponsor review** | private mesh | the sponsor (**sponsorship-authorized**, ADR 1071 — see §"Sponsor eligibility"; scoped to their OWN invitees when non-escalated, ADR 1072 — see §"Sponsor-scoped console access") | oauth2-proxy session or onboard-API key on the id-admin-console (ADMINS full view; non-admin sponsors portal-resolved + own-invitee-scoped) | reviews the candidate's submission, approves (optional: a second sponsor may cross-validate, non-gating) |
| **3 — Admin review** | private mesh | admins only | Ory `ADMINS` / `IAM-ADMIN` | final review post-sponsor-approval, executes the mint (the existing `goat admin onboard` queue) |

What ADR 1061 built so far is a *collapsed* stage 2+3 where the sponsor types everything. This splits the candidate (authors their details) from the sponsor (vouches) from the admin (mints).

## Locked decisions (2026-07-01)

1. **Public edge is a thin, stateless "front desk," the portal stays mesh-only.** The candidate page is served from an *additive route on the existing enrollment ingress* (the relay/rathole `:443` path) and **forwards the submission inward** — it holds no registry, no secrets, stores nothing long-term. This reuses the ADR 1058 pattern (relays POST to a token-gated mgmt-host ingest). The portal (which holds the registry + operator surfaces) is **never** exposed to the public internet — first-class invariant.
2. **Minimal-burden sponsorship — the sponsor optionally pre-fills; the enrollee fills the rest** (revised 2026-07-01, ADR 1062 Amendment 1 — **built + deployed on ZAGROS across all three surfaces**). Every sponsor-set field (tier / affiliation / site / machine-quota) is **optional**; a sponsor may mint a bare link and let the enrollee fill everything, including their own tier/affiliation — the burden shifts to the person who knows their details. A field the sponsor *did* set is locked; a field left open is the enrollee's. The rule is enforced **once, in the portal API**, so the three sponsorship surfaces inherit it by construction: the **API** (`POST /api/onboard/invite`, all-optional), the **web UI** (`/admin/onboard-invite.html` sponsor page + `/onboard.html` enrollee fields), and the **CLI** (`goat admin invite` — reads a YAML spec via `--file`, `--edit` opens it in `$EDITOR` before firing, or flags). Two human gates (sponsor + admin) still precede any mint, so an enrollee-supplied value is a draft, not a grant. **Exception:** the escalated admin tiers `iam-admin` / `platform-admin` are **sponsor-offered only** (the enrollee can never self-select them; `/api/onboard/submit` refuses an enrollee-set escalated tier) and are **flagged `escalated` for extra scrutiny at both the sponsor and admin review stages.** The consumed invite now assembles a **valid, approvable operator manifest** server-side (a complete effective set) or an admin-completable **draft** (an incomplete one). The enrollee form captures the identity fields the mint needs: name, email, person/system, organization/unit, nation, **unit code** (`affiliation.unit_code` — the enrollee types the code and picks its scheme: national/sovereign / NATO LOGFAS / SAP-ERP, or leaves it to auto-infer from affiliation kind; recorded `source: self-asserted` for the unit-reference service to grade post-mint), role/title, **machines-to-enroll quota** (`netbird.usage_limit`, sponsor-lockable + petitionable like tier/affiliation), and — for a *system* enrollee — an optional **SPIRE/SPIFFE-federation intent** flag (the offline-CA mint can't issue SPIRE federation and external orgs aren't SPIRE-attestable, so it's recorded as an admin follow-up note for the moon-pod federation runbook, not a manifest field). Identity-grammar dimensions still outside the mint schema — `node-class`, `autonomy`, comrel edges — remain admin-completed post-mint until their grammar phases land.
    **Intake outcomes (2026-09-11; claimed organization 2026-09-13, #1086).** A consumed invite ends in exactly one of three states, decided server-side at submit. A complete proposal is then reviewed one of two ways, by who named its organization:

    | Outcome | When | Candidate | Sponsor | Admin |
    | --- | --- | --- | --- | --- |
    | **Refuse** | a required field is missing or invalid | the form's own error; the invite is NOT consumed | nothing | nothing |
    | **Complete proposal, organization bound** | the effective inputs assemble a manifest AND the organization resolves in the org registry (a registered id or an alias) AND the sponsor's prebound org named it; or the registry is disabled / not wired | thank-you page + status stepper | the normal vouch | the normal review |
    | **Complete proposal, organization claimed** | the inputs assemble a manifest and the organization resolves, but its id was derived from the name the candidate typed (the invite pinned no org), registry on | **byte-identical** thank-you + stepper | the normal vouch | the organization panel headed *is claimed by the candidate*, the claimed org listed first with Attach, and no Register new (the org exists); Approve & Provision **disabled with an inline reason** until an admin attaches the org or edits `affiliation.id`, and the server refuses the approval (409) until then |
    | **Draft** | the inputs assemble a manifest but the organization (the sponsor's prebound org, else the typed name) is not in the registry, registry on | **byte-identical** thank-you + stepper — an unregistered organization is never the candidate's problem | the vouch proceeds unchanged, plus one line: *Organization not registered yet. An administrator will register it or match it to an existing one before approval.* | an `org not registered` pill on the queue row; the organization panel headed with the name the candidate typed; Attach / Register new (which complete the draft); Approve & Provision **disabled with an inline reason** when `ONBOARD_REQUIRE_REGISTERED_ORG` is armed, or a warning line above the enabled button when it is not |

    A draft parked for an incomplete input set (the earlier case) is the same record shape; both complete through the admin actions on the review page. The registry check reads the organization-derived id, the same id the assembler writes; a submission with no organization never reaches it, since intake refuses that (the Refuse row) before the invite is consumed. The marker the admin sees is derived from the registry at render time, so it clears the moment the org is registered or attached. The org-before-person rule ("Stage 3 gains an ordering", below) is what this serves: a proposal can no longer carry a fresh slug into the registry at approval without an admin having seen it.

    Who named the organization is recorded on the proposal row as `org_source`, because the assembled manifest keeps no trace of a sponsor's pre-binding and `affiliation` admits no extra keys (`additionalProperties: false`). `sponsor`: the invite pinned the org at mint, or an API or batch intake submitted the manifest whole. `typed`: the id was derived from the organization the candidate wrote. `operator`: an admin attached the org, created it from the panel, or edited `affiliation.id`; that value is what closes the panel and the approve gate. When the console opens its store it backfills rows written before the column existed from the invite that produced them: `onboard_invites.prebound_json.org` set gives `sponsor`, unset gives `typed`. A row with no invite behind it keeps an empty value, which reads as `typed`, so an unknown provenance asks. The claim gate fires only for an organization that resolves; an unregistered one stays under `ONBOARD_REQUIRE_REGISTERED_ORG`. A registry read error refuses the approval (503) instead of reading as "not registered".
2a. **The candidate sees who invited them** (2026-07-08). The intake page's
    preview carries the invite's `sponsor` and `/onboard.html` renders "you've
    been invited by **<sponsor>**" — the candidate always knows who vouches for
    them. Server-derived at mint (`sponsorOf`, never client-supplied); the
    generic shared-operator label is suppressed (a candidate is shown a person,
    or nothing). For CLI mints the person comes from the delegated-attribution
    header `X-Goat-Operator-Actor`, which `goat`'s portal client now sends from
    `$GOAT_OPERATOR_HANDLE` / the profile `operator_handle`.
3. **Single sponsor approval, any tier — no required cross-validation.** One sponsor approving moves a candidate to the admin queue, for *every* tier including the admin tiers. Rationale: the **admin/operator is the effective second pair of eyes** at stage 3, so a mandatory second sponsor would be redundant. Optional cross-validation by another sponsor may exist as a feature but never gates. *(Amended by ADR 1071: the single sponsor must be sponsorship-authorized, and may vouch only up to their annotated tier — see §"Sponsor eligibility".)*
4. **The stored record extends the identity-descriptor standard** ([ADR 1051](../adr/1051-identity-descriptor-standard.md)), not a new one-off schema — so a person carries one consistent, versioned, auditable record from sign-up through minted identity. The review pages *render from it*; it is the programmatic SoT.
5. **Candidate PII protection = in-transit TLS + no edge storage + encrypted-at-rest in the portal** for v1. Browser-side (client) encryption to a portal key before the data leaves the candidate's machine is a **later hardening**, not v1.

## Sponsor eligibility ([ADR 1071](../adr/1071-sponsorship-authorization-at-onboarding.md))

Whether someone may *be* a sponsor — mint invites (stage 1) and vouch (stage 2) — is an explicit, per-identity authorization decided **when that person is onboarded**, not a side effect of holding a portal credential:

- **The annotation:** the manifest's optional `sponsorship: {authorized, max_tier}` block, written onto the identity's portal registry row at mint (both paths — goat-cli native and `operator-onboard.yml` Phase 4.0). The onboarding form/queue is therefore also where the decision is *made*: sponsoring the escalated admin tiers pre-fills it, anyone else gets it deliberately.
- **Defaults:** the `iam-admin` and `platform-admin` tier presets are sponsorship-authorized at their own tier automatically. Operator principals (backend super-user keys) are authorized by definition, uncapped. Everyone else is **not** a sponsor unless annotated case-by-case.
- **Level cap:** a sponsor may offer / vouch **at most their own annotated tier** (tier ranks: browse < developer < iam-admin < platform-admin, declared in the intake form JSON). The cap is stamped server-side onto every invite (`prebound.sponsor_max_tier`), bounding the enrollee's tier menu and submitted tier by the same ceiling; stage-2 vouching re-checks the proposal's tier against the voucher's cap. Fail-closed: unknown tiers never pass; authorized-with-no-max caps at the lowest tier.
- **Case-by-case lever:** operator-gated `POST /api/orgs/{id}/sponsorship` / `goat admin sponsorship <anchor|handle> --max-tier <t>` (`--revoke` to remove), audited per-person. The annotation is visible on `GET /api/orgs`.
- **Who typically holds it (doctrine, not code):** per the identity grammar (ADR 1043) capability is never keyed to affiliation, so this is expressed operationally by who receives the annotation — never as an affiliation-conditioned rule in the gates.

## Sponsor-scoped console access ([ADR 1072](../adr/1072-sponsor-scoped-onboarding-console.md), built in code — ZAGROS deploy pending)

What a sponsor *sees* is scoped to what they sponsor. A non-escalated sponsor (browse/developer cap) gets **only their own invitees** — mint, list, review, vouch — on both realms of the console-owned onboarding engine; admin-tier caps (iam-admin/platform-admin), ADMINS sessions, and operator keys keep the full queue.

- **Login binding:** the sponsorship annotation optionally carries `sponsor_email` — the sponsor's Ory login (`goat admin sponsorship <id> --max-tier <t> --email <login>`, the manifest `sponsorship.email`, the lever's `email` field; the admin-tier intake presets bind it automatically via the `@email` preset token). Cleared on revoke. Without it the console refuses sponsor mode; CLI/API sponsorship with the sponsor's own portal api_key is unaffected.
- **Read-only portal resolves:** participant keys resolve via `GET /api/whoami` (now carrying `alias` + `sponsor_email`); human sessions resolve via the new `GET /api/orgs/sponsorship?email=` (operator bearer or the console↔portal service secret — defaults to the stage-notify token, zero new secrets). The vendors registry stays the sponsorship SoT; both resolves fail closed.
- **Scoping (console store):** a row's recorded `sponsor` is matched against the sponsor's `{anchor prefix, slot alias, login email}`; foreign ids read 404. Stage-2 vouch requires own sponsorship + tier-within-cap; sponsor-session mints stamp the cap onto the invite. Machine-realm admin actions (`approve`/`reject`/`onboarded`/`claim`/`result`) now refuse participant keys outright.
- **Page access:** oauth2-proxy's `id_admin_console_oauth2_proxy_allowed_groups` (default `ADMINS`) is set EMPTY per-goatnet for sponsor access — authenticated-only front door, because partner identities carry no Ory groups (the live vocabulary is PLATFORM-ADMIN/OBS-ADMIN/COMPLIANCE; NetBird auto_groups are a different namespace). The console's `onboardGate` + store scoping carry the real authorization (a non-sponsor who passes the proxy still gets 403), and every non-onboarding console page stays ADMINS-gated. A `SPONSOR` Ory group is the recorded finer-grained follow-up.

## Stage 3 gains an ordering: the organisation exists before the person is approved

[ADR 1171](../adr/1171-person-nationality-and-org-ownership-are-two-facts.md) puts one step in front of the admin decision. The order becomes **organisation registered → organisation's facts confirmed → membership relationship declared → person approved.**

Today `decideProposal` consults no registry, so a candidate can be approved and provisioned carrying an `affiliation.id` that exists nowhere.

Three changes at this gate:

1. **Approval refuses while the candidate's `affiliation.id` does not resolve**, and the refusal names the create action rather than erroring generically. Shipped behind `ONBOARD_REQUIRE_REGISTERED_ORG`, default false, armed per goatnet only after that goatnet's registry backfill.
2. **The org-registration form asks the admin, and prefills nothing from the candidate.** A person's self-description is not a source for an organisation's facts.
3. **The membership relationship is a required choice on approve** — `constituent`, `assigned` or `liaison` — with the person's nationality and the organisation's nation shown side by side. Two different nations is the normal case for a liaison or an internationally recruited member, and the reviewer is the person who knows which.

Sequence, gates and the per-goatnet backfill counts: `org-affiliation-nationality-plan.md`.

## Architecture

```
   PUBLIC INTERNET                         │  PRIVATE MESH
                                           │
 candidate  ──presigned one-time link──▶  [ thin front-desk collector ]
 (browser)     (i18n form, holds nothing)  │        │ forward submission (token-gated)
                                           │        ▼   (ADR 1058 ingest pattern, rathole :443)
                                           │   [ PORTAL  (mesh-only, distroless) ]
                                           │     • mints/validates/CONSUMES the one-time invite token
                                           │     • stores the candidate record (identity-descriptor v-draft)
   sponsor  ──Ory login──────────────────▶│     • Stage 2: sponsor review + approve
   admin    ──Ory login (admins only)─────▶│     • Stage 3: admin queue → goat admin onboard (existing)
```

### Invite token lifecycle (the "presigned one-time link")

Mirrors the existing audit-presign pattern. Sponsor (mesh, Ory) → `POST /api/onboard/invite` with the pre-bound fields (tier/affiliation/site/quota) → portal mints a **signed, single-use, TTL'd** token → returns `https://intake.<public>/?t=<token>` to hand to the candidate. Candidate opens it (public) → the collector shows the i18n form → candidate submits → portal **validates + consumes** the token (single-use) + stores the record in `submitted` state → the link is now dead. States: `invited → submitted → sponsor-approved → admin-approved → onboarded | rejected | expired`.

**One more state, and it is a different KIND of state — `enrolled` (2026-08-05).**
Every transition above is a decision somebody made. `onboarded` therefore means
"credentials were minted and the welcome package was emailed" — it says nothing
about whether that package landed, whether the person could follow it, or
whether the setup key was ever used. Operators kept asking a question the
console could not answer: *did this actually work for them?*

`enrolled` answers it, and is **observed rather than decided**. The console's
enrollment watcher (`ops/goat-id-console/internal/enrollwatch`) polls the
NetBird management API for the person's setup key and flips
`onboarded → enrolled` once `used_times > 0` — proof they ran `netbird up` with
what we sent. Properties, all deliberate:

- **`enrolled` does not mean "can sign in".** Any redeemed device counts,
  including a headless server, while the login link and `/fabric-credential`
  open only on a device connected to the mesh. The Onboarded and Enrolled
  cards therefore list the person's peers with their OS and warn when every
  one is a server; the welcome email says beside the login link that a server
  does not count (#1586, zagros 2026-10-05). Display only — the state machine is
  unchanged.

- **Default-off.** No read-only NetBird token configured
  (`id_admin_console_enroll_watch_nb_token`) ⇒ the watcher never starts and
  every row stays at `onboarded`, exactly as before.
- **Read-only in the code; NOT read-only at the API.** The watcher only ever
  `GET`s `/api/setup-keys` — it never mints, revokes or edits, so the blast
  radius of a *misbehaving watcher* is a wrong badge. But NetBird has **no role
  below `admin` that can list setup keys**, so the token it carries is an admin
  token (verified against the live API, 2026-08-07). Do not read "read-only" as
  an API-enforced scope. The containment that actually exists: the credential
  belongs to a **dedicated service user** (`svc-console-enrollwatch`), never to
  a human, and is revocable on its own by deleting that user. Revisit if
  NetBird grows a narrower role.
- **One-way and idempotent.** The store update matches only rows currently
  `onboarded`, so the watcher cannot resurrect a rejected row, cannot overwrite
  a later state, and re-observing the same key is a no-op.
- **A failed poll is UNMEASURED, never "nobody enrolled."** An unreachable API
  logs at warn and changes nothing — the same honesty rule the fabric-health
  surface follows.
- **Matching is by key NAME**, since the API returns a key's value only at
  creation. The peer-name grammar (`ops/identity/peer-name-grammar.yaml`) puts
  the handle first, so `peer-<handle>-` is an unambiguous per-person prefix;
  the tests fence the near-miss cases (a shorter handle must not inherit a
  longer one's key).
- **Revoked-but-used still counts as enrolled** — the person did join; forgetting
  that would walk the badge backwards.

### The other onboarding path, and why it is in this document (2026-08-18)

This document describes the reviewed path, but it is not the only one. `goat
admin onboard` mints directly, without gates, for an operator who already holds
mint authority — and until 2026-08-18 it left no trace the console could see, so
such identities sat in the roster and in no lifecycle lane at all. It now
registers a row here as the last step of a mint (`POST /api/onboard/register`),
landing at `onboarded` because the mint already happened.

That means the stages in this document describe **everybody**, not only the
people who came through the queue — which is what makes any count read off them
trustworthy. `ops/identity/register-drift.sh` fails if the two registers drift
apart again. Detail: F-357.

### The credential window, and what happens when it closes (2026-08-18)

The mesh setup key in a welcome package defaults to **7 days**, cut from 90 on
2026-08-18. A welcome package is written to be acted on the day it lands, so the
long window bought nothing and left a live mesh credential in a mailbox for
eleven of its twelve weeks; it also made the key the one artifact a reader could
safely defer, which is how a credential ends up unredeemed and forgotten. The
exception is per-person and stays per-person — `netbird.ttl_days` in the
manifest, for kit provisioned ahead of a move — never a change to the role
default. The welcome mail states the window in its time-sensitive block, and
that block now fires on a setup key alone; at 90 days it did not, so a key-only
package printed no deadline anywhere.

When the window closes without the key being redeemed, the ADR 1136 dormancy
sweep revokes the key and the console row is **archived** — cold storage, not
deletion. The row keeps its sponsor, both gate approvals, tier and manifest, so
**Restore is a single admin action**: the row returns to `approved`, the runner
mints a fresh key, and the welcome package goes out again with neither human
gate repeated. It is one button on the console's Archived card (**Restore and
resend key**, goat#1037) or the operator API's `.../restore`; both take the same
guarded store write and write the same audit event. Restore is asynchronous —
the row leaves the Archived tab the moment it is restored and the runner picks
it up later — so the console follows it to the Approved stage, says which
address the fresh package goes to, and marks the row "Restored by X at T" from
there through Onboarded, so a restored person is not read as a fresh approval. Deleting the record instead — which is what ADR 1136 originally
specified — priced re-enabling one person at two fresh vouches for somebody
already vouched for, and the predictable result was that nobody ever ran the
lane. An archived row still answers the duplicate-email guard, so restore is the
only way back and one person keeps one record.

**Arming is per-goatnet, and an unarmed goatnet looks exactly like an empty
queue.** The console's `Enrolled` tab renders as an ordinary empty bucket beside `Onboarded`
— indistinguishable, to anyone reading the page, from "we sent the welcome
packages and not one person joined". It was the same conflation the watcher's
own **unmeasured ≠ zero** rule forbids at poll time, reappearing one layer up at
config time: the rule guards a *failed poll*, and says nothing about a watcher
that was never started. When arming a new goatnet, do both halves — the NetBird
service user AND the `group_vars` token. One without the other is a dark tab.

### Data object (extends the identity-descriptor standard)

`{ schema_version, invite_id, sponsor, sponsor_pre_bound{tier, affiliation, site, machine_quota}, candidate_submission{...}, reviews:[{actor, role: sponsor|cross-validator|admin, decision, at}], status, append_only_audit:[...], assembled_manifest }`. Append-only audit; provenance (which invite, which sponsor). This is the intake-time instance of the ADR 1051 descriptor — build on that doc's spine/dynamic split, don't fork it.

### i18n (stage 1 only)

Reuse the goat-cli translation set — **~30 `.ftl` catalogs already exist** under `crates/goat-cli/i18n/` (pl, lt, lv, et, fr, de, it, es, pt, nl, da, sv, nb, fi, is, cs, sk, sl, hr, hu, ro, bg, el, tr, uk, mk, sq, cnr, lb, + tlh) — the NATO + partners set, per ADR 1024 (Fluent). Stage 1 is candidate-facing → full i18n; stages 2–3 stay English (operator-facing).

## Invite lifetime, and why the three surfaces must agree (F-363)

An invite's TTL is a **chosen** value, not a typed one. The mint form offers
4h / 24h / **72h (default)** / 7d / 30d as a `<select>`; the machine path
(`POST /api/onboard/invite`, `goat admin invite --ttl-hours`) still accepts any
value in 1–720 because automation has reasons a form does not. The form gave up
free-form entry after a blank `<input type="number" min="1">` submitted a `1` —
an empty focused number input writes its `min` on a stray wheel-scroll — and
emailed a candidate a link that died in an hour.

Three surfaces state the lifetime, and all three must say the same thing:

| surface | states |
|---|---|
| mint confirmation | expiry, relative + absolute; a warning banner at ≤12h, before the sponsor sends |
| candidate email | "expires in 3 days — at 2026-08-21 18:51 UTC", never a bare "time-limited" |
| invite list (web **and** `goat admin invites ls`) | the TTL-resolved state |

That last row is the load-bearing one. `expires_at` passing does not rewrite the
stored `status` — expiry is resolved on read — so **every** read path has to
resolve it. When only some did, the web page called a link expired while the CLI
called it open, and an operator checking the CLI got the more comforting answer.
`ListInvites` resolves TTL before filtering for exactly this reason.

**Mint-path parity.** Anything the invite pre-binds must be expressible on both
paths. `collab` was on the web form (checked by default) and absent from the
machine request, so CLI-minted invites silently excluded people from the
collaboration environment. It is now `*bool` / `Option<bool>`: absent inherits
the form's default, and only an explicit `false` excludes. A field that exists on
one mint path and not the other is a divergence, not an omission.

## Acceptance gate: the browser drives the candidate form (built 2026-09-08)

A markup rename once left the intake form's submit handler reading a control that no longer existed. The handler threw after `preventDefault()` and before its `fetch`, so "Submit for review" did nothing on screen, no request reached the collector, and no log line anywhere recorded a failure, while every Go test was green.

That class of defect — the page's own script against the page's own markup — is invisible to anything that does not run the page. Two guards now do, and they are the acceptance gate for this flow:

| Guard | Where | When it runs | What it proves |
| --- | --- | --- | --- |
| Playwright spec, mocked backend | `ops/goat-id-console/web/e2e/tests/intake-form.spec.ts`, workflow `id-console-web-e2e.yml` | every PR touching `ops/goat-id-console/web/**` | the form loads, a person can submit, the request carries the renamed controls (`alliance_standing`, no `coalition`), nationality is required of a person and not of a system, the error path re-enables the button, and **no uncaught page error** occurs |
| Live smoke, real goatnet | `ops/scripts/onboard-intake-smoke.sh --context <ctx> --env <goatnet>` | after every console deploy to a goatnet; the zagros-dev **validation-evidence** the release ladder's dev→preprod hop requires (`ops/release/ladder.yaml`); manifest row `intake-form-browser-submit` in `ops/validation/acceptance-tests.yaml` | mints a one-time invite, opens the public intake page in Chromium, fills and submits, requires the real `201`, finds the proposal in `proposed`, then sponsor-rejects it so nothing is left in any queue; run as the `goat-acceptance` sponsor identity (ADR 1207) |

On a laptop that is on the goatnet's mesh, set `INTAKE_RESOLVE=intake.<public>=<relay ip>` so the browser resolves the public intake name the way a candidate does. The smoke needs a goat context whose key may mint invites (a sponsor suffices) and a one-time `npm ci && npx playwright install chromium` in the e2e directory. For a goatnet the laptop is not meshed to, `--goat-via <user@host>` runs the `goat` legs on a host that holds the context (the goatnet's management host, reached through its door, [env-doors.md](env-doors.md)) while the browser leg stays local against the public intake page, so the operator key never leaves the host. It uses a reserved-TLD candidate address, so the sponsor and rejection mails the flow sends about the synthetic candidate reach no inbox. First live runs 2026-09-08: ZAGROS — PASS; zagros-dev (`--goat-via` its management host) — PASS.

**Coverage boundary.** The gate covers stage 2 (the public form). The sponsor and admin decisions, the runner's mint and the welcome email were proven by hand on ZAGROS on 2026-09-08 (invite → submit → sponsor-approve → approve → onboarded, welcome email sent) and are not yet driven by the browser; the admin pages sit behind Ory login, which the harness does not automate. Extending the spec to the queue pages is the next increment of the suite, and it is what makes the whole of ADR 1062 a release gate rather than one stage of it.

**Nationality is required of a person (2026-09-08, refined 2026-09-11).** Shared data lanes are narrowed by the nationality of the person reading, so a blank one is not "the most restrictive case" — it is a request that cannot be reviewed. The form marks the field required, the script refuses a person's submit without it, and the server refuses it with a single `422 validation_failed` *before* the one-time invite is consumed, naming `nationality` in `error.fields` so the candidate reopens the same link and adds it. A system principal has no citizenship and is exempt. The organisation's nation is unaffected: it is an org fact typed by an admin at registration ([ADR 1171](../adr/1171-person-nationality-and-org-ownership-are-two-facts.md)).

**Multi-field intake validation (2026-09-11).** The public submit door (`POST /api/onboard/submit`) validates the entire submission and refuses any missing or invalid fields with one `422 validation_failed` response instead of a series of single-field errors. The response body is `{"error": {"code": "validation_failed", "message": "…", "fields": {"organization": "…", "tier": "…", "handle": "…", "nationality": "…", "nation": "…"}}}`. Every field name in `error.fields` corresponds to an intake field; messages are the same ISO-code explanations and helper copy as before. A submission with a blank organization, an open tier left blank, and a lowercase nationality therefore receives one refusal naming all three fields, and the invite remains unconsumed.

Organization is required for both principal types, nationality for a person only.

**Affiliation ids are derived from the organisation name, never the handle.** The intake assembler writes `affiliation.id` from the invite's registered org id when pre-bound; otherwise it derives it from the candidate's `organization` answer. ASCII names slug to a stable id (`example-vendor-ltd`), cut to the schema's 64-character ceiling; a name whose slug is too short to be an id (for example a name in a non-Latin script, or a single letter) produces an 8-character SHA-256 hex prefix of the name folded by the org registry's `NormalizeName` (diacritics stripped, lowercased, spaces collapsed), so two spellings the matcher treats as one name land on one id. Every derived id satisfies the manifest schema's `affiliation.id` shape. A name with no letter or digit at all cannot form an id and is refused at intake by name; no path falls back to the candidate's handle.

## The site network is checked before enrolment, not after (built 2026-10-05)

A device on a network that allows only HTTPS registers with NetBird, reports `Connected`, and then reaches nothing. Management and signal use TCP 443. The tunnels need TCP/UDP to the relays (33073 on today's goatnets) or UDP to peers. Nothing the participant had been sent mentioned the requirement, and the guide's firewall page is itself mesh-only.

What a site must allow is now stated before the link is used, from the goatnet's own relay and STUN config (`internal/welcome/netreq.go`):

| Where | What |
|---|---|
| Invite email (`mailer`) | "Before you start: your network": ports, relay hosts, a one-line `nc -zv` test, the blocked symptom, a link to the public page |
| Public intake host, `/network-requirements` | The full table for a network team. Names and ports only, no addresses. Served by the console at `/public/network-requirements`, allowlisted in the intake nginx (`onboard-intake-collector/templates/nginx.conf.j2`) |
| Intake form (`web/static/onboard.html`) | The same short section above the submit button |
| Welcome email | The table again, after the enrolment check |
| Guide troubleshooting | "Connected, but 0 peers — the login link does not open" |

## Archiving dead invites (built 2026-09-04)

A revoked or expired invite stayed on the Invited tab forever. Nothing was
broken by that — a dead link fails closed whatever list it sits on — but the
list an operator scans for live work filled up with links nobody was working
on.

Dead invites are now swept into a folded **Archived** section under the working
list, once they have been dead for at least seven days.

| | |
|---|---|
| What moves | `revoked`, and `expired` — including a row still stored `invited` whose TTL has passed, which `ListInvites` already resolves lazily |
| What never moves | an open invite inside its TTL; a `submitted` one (it became a proposal and has its own lifecycle); one already archived |
| Window | `ONBOARD_INVITE_ARCHIVE_AFTER`, default `168h` (7d). `off` disables the sweep |
| Cadence | hourly, plus once at console boot |
| Where | `Store.ArchiveStaleInvites`, swept from a goroutine in `cmd/console/main.go` |

**Staleness is dated from the DEATH, not the TTL.** For an expired invite those
are the same moment. For a revoked one they are not: a sponsor who revokes a
30-day link on day one killed it on day one, and dating it from the TTL would
hold it on the working list for the rest of the month. `RevokeInvite` therefore
stamps `revoked_at`, and `StaleSince` prefers it. Rows revoked before that
column existed have no stamp and fall back to the TTL — *later* than the truth,
so a legacy row lingers rather than vanishing early.

**Archiving is a display move, not a lifecycle stage.** An archived invite is
exactly as unusable as it was the moment it died, because `ConsumeInvite` only
ever accepts `invited`; there is a test that says so. There is deliberately no
restore: the way back is minting a new invite, not resurrecting a link somebody
may still be holding. That is the opposite of the *proposal* archive, where
Restore is the whole point (it re-mints and re-sends against vouches already
given) — same word, two different jobs, because the underlying objects differ.

**The sweep is not hung off the enrollment watcher**, despite both being
periodic. That watcher needs a NetBird token to start, gives up early when the
API is unreachable, and returns immediately when no proposal is pending — so an
invite sweep riding on it would silently stop in three situations that have
nothing to do with invites. This touches only the console's own SQLite.

## A queued cut that waits: the runner band (built 2026-10-28)

Two stages of the queue are the onboard-runner's, not a person's: Approved
waits for the mint, Cut queued waits for the revoke. A healthy runner claims
either within seconds of its 30 s poll. The console cannot see the runner; it
can see what the runner did and did not do, and since 2026-10-28 it says so
(F-589).

**The band.** When any row has waited more than five minutes since it was
handed to the runner (the admin decision for a mint, the admin click for a
cut), `/onboarding` opens with a warning that lists those rows with their
wait and links, states when any runner last claimed any work (proposals and
follow-up actions alike), and gives one of two verdicts:

| The store shows | Verdict | What the band tells the admin to do |
|---|---|---|
| A claim newer than the oldest waiting row | *Running, not reading this queue.* The runner was alive and polling after the row was queued and left it behind. When every waiting row is a cut, that is a runner built before the offboard leg (2026-09-26) | Deploy a current onboard-runner to this goatnet through the release ladder (`onboard-runner` in `ops/pins/fleet-pins.yaml`); the queued cuts then complete on their own. To unblock one person first: cut from the CLI, then settle the row |
| No claim since the oldest waiting row | *Not running, or not reading this queue* — the store cannot tell which | `docker logs onboard-runner` on the management host: a stopped container or a management-API preflight failure keeps every row waiting; claims logged for other rows but none for these is a build that does not read this queue |

The row's `runner?` badge, which already fired on an approved mint the runner
had not claimed in ten minutes, fires on a queued cut on the same clock. Both
are keyed on the moment the runner was handed the row, never on submission.

**The settle door.** *Mark offboarded* now accepts a never-claimed
`offboard-requested` row. It is for the admin who cut the access by hand
(`goat admin onboard <handle> --env <goatnet> --revoke`) while the runner was
not reading the queue: the note is required and must say how the access was
cut, and the login-plane precheck refuses while the person's login is still
active. A claimed cut (`offboarding`) is the runner's to finish and is not
accepted; a cut the runner wrote `offboard-failed` on re-queues as before.

**What the band does not know.** It infers the runner's shape from its
claims. A runner heartbeat that reports its build and the queues it polls is
the follow-up that would let the band name the version instead; until then
the console and the runner must be deployed as a pair, because the Offboard
button is inert on a goatnet whose runner predates it.

## Enrolled is not the same as reachable: the control-only flag (built 2026-10-05)

NetBird's management API reports a peer as `connected` once it holds a management session over TCP 443, even if it has never formed a tunnel. A participant behind a site firewall that passes only HTTPS therefore reads as enrolled and connected while reaching nothing.

The Onboarded and Enrolled cards flag such a device as "connected to the control server only". The source is the management host's own view of each peer. The `mesh-health` snapshot (`roles/mesh-health`, `mesh_health_peer_summary_enabled`, on for zagros) writes `/var/lib/mesh-peer-summary/peer-handshakes.json`, which holds four fields per peer: IP, FQDN, status and last WireGuard handshake. The console mounts that directory read-only (`PEER_HANDSHAKES_PATH`) and never gets the docker socket, the NetBird socket or the full snapshot. A missing or stale file leaves the flag off rather than raising it. On the participant's side, `goat doctor` (0.5.13) names the same state.

## Relationship to the delegated machine-enrollment thread

This is the *person* half of onboard-tier-role-model §5. The **machine-enrollment quota** the sponsor pre-binds in the invite (decision #2) is the standing authority for that person to bring machines online later, late-bound — the two compose. The machine self-service mint (person → their systems, custody edge) is a *downstream* flow specced separately once this person-flow existed — **now specced and built** in machine-enrollment.md ([ADR 1096](../adr/1096-delegated-machine-enrollment.md)).

## Verify-first outcome (2026-07-01, before the ADR)

Two existing docs were read to avoid duplicating/contradicting a public-surface contract. Neither blocks this; both sharpened it:

- `partner-peering-portal.md` (Block 84) — **no overlap.** That surface peers a partner's own **Zenoh router** (fabric-to-fabric), is mesh-internal only, and authenticates its "Requester" via federated BYOIDC. This flow onboards a **person** who has no identity yet, via a public one-time link. Different problem, different auth, different plane — [ADR 1062](../adr/1062-three-stage-delegated-candidate-onboarding.md) records them as explicitly distinct so they are not conflated.
- `nova-public-posture.md` — **the binding constraint.** Today only the two enrollment routers are public (via the rathole → dedicated `enrollment` Traefik entrypoint, F-153); everything else is mesh-only + `mesh-only-allowlist`. The front desk is therefore the **first intentional public non-enrollment surface** and must ride the enrollment ingress as a token-gated, stateless, additive route — exactly the [ADR 1058](../adr/1058-relay-metrics-self-publish-ingest.md) precedent. This is why step 2 (the public collector) is gated on a security review against this posture, and why step 1 (mint + token + store, all mesh-side) is the safe first increment.

## Build sequence (stage by stage)

1. **Invite mint + token lifecycle** (portal, mesh) — **BUILT** (2026-07-01, [ADR 1062](../adr/1062-three-stage-delegated-candidate-onboarding.md)). `onboard_invites` store (`internal/registry/invites.go` — mint / token-hash lookup / single-use consume→proposal / lazy-expire / revoke; the plaintext token is never persisted, only its SHA-256 hash) + `POST /api/onboard/invite` (sponsorship-authorized principals only since ADR 1071 — see §"Sponsor eligibility"; pre-binds tier/affiliation/site/quota; returns the one-time token once) + operator-gated `GET /api/onboard/invites[/{id}]` + `POST …/{id}/revoke` (`internal/api/onboard_invite.go`) + the `PORTAL_INTAKE_BASE_URL` config. Consume assembles the intake record (pre-bound ⊕ candidate submission + provenance) into an inert ADR 1061 proposal. No public surface; covered by registry unit + api httptest suites (green). The public consume route is step 2.
2. **Stage 1 candidate collector — BUILT, including the public front desk (2026-07-01).** The `/onboard.html` nomination form (self-contained, candidate-facing) + token-gated `GET /api/onboard/preview` + `POST /api/onboard/submit` (consumes the invite → inert proposal), wrapping `registry.ConsumeInvite`. **Public front desk** (decision #1): a thin **nginx allowlist collector** (`ops/ansible/roles/onboard-intake-collector/` + `ops/ansible/onboard-intake-deploy.yml`) on the **enrollment ingress** that serves *only* `/onboard.html` + `/api/onboard/preview` + `/api/onboard/submit`, forwarding them to the mesh-only portal (`goat-zenoh-portal:8090`); **everything else 404s**, so the portal's operator/registry surfaces are never publicly reachable — the portal stays mesh-only, the collector holds no secrets/state. Public DNS `intake.zagros.example` is its own A round-robin directly at the relay EIPs (mesh-safe: never rides the mesh-IP-first apex answer, GH #151); Traefik on the enrollment entrypoint routes `Host(intake…)` to the collector with its **own** LE cert (`tls.domains`, so the intake hostname never lands in the enrollment cert's SAN — the SNI-leak discipline of `nova-public-posture.md`). The portal's `PORTAL_INTAKE_BASE_URL=https://intake.zagros.example` makes the mint return a public `intake_url` (the sponsor UI + `goat admin invite` prefer it over the mesh `nomination_url`). Covered by api httptest (mint→preview→submit→queue round-trip, single-use, validation) + an off-mesh live-fire. A host-local static advisory page on the mesh-facing Traefik websecure entrypoint catches whatever still arrives there — the DNS TTL window on already-cached CNAME answers, and clients holding a stale or pinned answer — and says the link is a public enrollment page that must be reopened off-mesh, instead of a bare `404 page not found` (GH #150). It is a RESIDUAL net, not the fix: GH #151 above removed the cause, so expect to retire it. It covers the intake hostname only, and it does not generalise to future colliding names — a name not already certed on that Traefik fails the TLS handshake before any page can render, so there is no catch-all friendly page over HTTPS.
3. **Stage 2 sponsor review** — a distinct sponsor gate *after* the candidate fills. **BUILT (2026-07-01).** A consumed invite now lands in `proposed` (awaiting the sponsor), not straight in the admin queue. The sponsor reviews the candidate's filled submission at `/admin/onboard-sponsor-review.html` and vouches (`POST /api/onboard/proposals/{id}/sponsor-approve`, sponsor-authed) → `sponsor_approved`, which is where the admin queue now picks it up (its default filter). The admin `approve` refuses anything not `sponsor_approved`, so **two human gates precede any mint** (sponsor + admin). Sponsor-decline (`/sponsor-reject`) and the escalation/petition badges render at this gate too. The same `/sponsor-reject` lets a sponsor withdraw a vouch while the proposal waits in `sponsor_approved`, until the admin decides ([ADR 1207](../adr/1207-acceptance-rows-run-as-a-non-person-sponsor-identity.md)); the console advertises it at `GET /api/onboard/capabilities` (`sponsor_withdraw`). Optional non-gating cross-validation stays a future feature.
4. **Stage 3 admin** — the admin acts on `sponsor_approved` proposals (the queue's default filter). Two surfaces: the **web queue** (`/admin/onboard-queue.html` — Approve/Reject buttons render for `sponsor_approved`, a `proposed` proposal shows "awaiting sponsor review") and the **CLI** (`goat admin queue ls|approve|reject`, the counterpart for operators who live in a terminal — same operator bearer, same two-gate enforcement: approve refuses a proposal the sponsor hasn't vouched for with a clear 409; `queue ls --email <addr>` defaults to all states and filters the exact address at the server; explicit status or environment filters can hide a blocking proposal and print a warning — the follow-up to a duplicate-email 409 at submit, which names the proposal blocking it). Approving records the decision + emits the manifest/command; the **`onboard-runner`** (BUILT + deployed on the ZAGROS mgmt host, separate track) then mints it automatically (~30s → `onboarded`), or the operator runs the emitted `goat admin onboard` command as the manual fallback. The native fast path now provisions browse/web-only tiers too (the earlier `zenoh.enabled=false` bail was fixed in goat-cli 0.4.15), so the runner covers every tier once that release reaches the mgmt host.
5. **Pre-submission visibility — the "Invited" queue stage (2026-08-04).** The console's
   `/onboarding` queue previously started at `proposed` — a minted-but-unopened invite (the
   funnel's first stage, where a whole cohort sits between "emailed" and "submitted") was
   invisible in the human UI, and an invite that lapsed unconsumed expired silently. The queue
   now leads with an **Invited** tab: open invites (tab count = open only), plus expired and
   revoked ones badged rather than vanished (the view computes expiry from `expires_at` —
   `ListInvites` does not lazy-expire, only the Get paths do). The detail pane is the full
   record of the link that went out (2026-08-18) — **who it was addressed to**
   (`candidate_email`, recorded at mint, before any send is attempted, so an address survives
   a failed or out-of-band delivery), **how and when the console released it** (`sent_at` +
   `sent_via`: `email` = the console sent it, `eml` = an .eml the operator downloaded to send
   from their own client, so the console never saw it leave; neither claims receipt), the
   clock as both an absolute deadline and the time left plus the lifetime the sponsor chose,
   **every pre-bound field** (tier / affiliation / site / machine quota / collab / the
   server-set sponsor ceiling, each rendered even when unset so "enrollee's choice" is
   visible as a fact rather than an omission — the machine-quota row said "the intake form's
   default applies" instead, which is a default the invite path does not apply, and an
   operator read it before leaving the field blank, F-365), the note, and the submit that
   ended the invite.
   A row minted before the delivery columns existed reads "no address recorded" rather than
   inventing one. Per-send history stays in the audit trail (`onboard_email_invite` /
   `onboard_email_download`); the row carries the latest hand-over. Those same audit rows
   make most pre-existing invites RECOVERABLE — they carry the invite id and the recipient
   together — so `ops/scripts/backfill-invite-recipients.py` (dry-run default, `--apply`)
   replays them onto the invite table. It never overwrites an address already on a row and
   never invents one for an invite with no recorded send. A blank has TWO causes and the run
   separates them, because they are different claims: an invite minted while the trail was
   running and carrying no send genuinely was never emailed, whereas one minted BEFORE the
   trail begins is unknowable — calling that "never emailed" asserts what the data does not
   say. Run once per goatnet after the console image lands. **Timestamp precision matters here** —
   the audit writes Go's RFC3339Nano (nine fractional digits) and `datetime.fromisoformat`
   only accepts more than six from Python 3.11, so on Ubuntu-22.04 hosts (Python 3.10) a naive
   parse drops every send row and the run reports a confident, wrong "never emailed". The
   script normalises the fraction and shouts about any row it still cannot date. The pane also
   offers **Revoke** on an open invite (`POST /onboarding/invite/{id}/revoke` — the human-web
   leg of the machine realm's revoke; CSRF-gated, audited as `onboard_revoke_invite`).
   Sponsor sessions (ADR 1072) see and revoke only their own invites, exactly like the
   proposal tabs. Submitted invites are excluded — they live on as proposals in later stages.
   Finding one person across all of this — every invite state and every proposal stage — is
   the header find box on the same page (goat#1306; the design note is in
   id-admin-console.md, "Cross-stage candidate find").
6. **i18n** — fold the `.ftl` catalogs into the nomination form.

## Next session (pickup)

Current state to resume from:
- **Steps 1 + 2 built and deployed live on ZAGROS** (2026-07-01, [ADR 1062](../adr/1062-three-stage-delegated-candidate-onboarding.md)): the invite mint + one-time token, and the mesh-side candidate nomination form (`/onboard.html`) + preview/submit → proposal in the admin queue. End-to-end usable today (sponsor invites → candidate fills → admin reviews + `goat admin onboard`).
- **Full chain live-fire proven end-to-end on ZAGROS (2026-07-01)** with a throwaway identity (`user72x`, browse/partner, revoked + purged after): invite → **public** submit (`intake.zagros.example`) → approve → `goat admin onboard --env zagros` (Ansible path, an older local `goat`) minted a NetBird setup key + an Ory identity + a complete bundle that passed the completeness gate. **What this did and did not prove about the grammar:** it proved the *manifest-level* affiliation grammar (Phase C — the onboard validator requires an `affiliation` block) is enforced end-to-end. It did **not** prove Phase D (the Ory token `aff` claim) — that stays deploy-gated: `ory_site_affiliation_trait_enabled` defaults `false` and is not overridden on ZAGROS, and the native fast path (`build_identity_body`) hardcodes `aff` off. So the identity minted **without** an `aff` trait (the current-correct behaviour) — affiliation lives in the manifest + roster registry, not yet in the Ory token. Enabling Phase D later means flipping that flag **and** teaching the native path to emit `aff` (a tracked latent divergence: today both paths omit it, so they agree; the day the flag flips, the Ansible path emits `aff` but the native default still wouldn't). The live fire surfaced + fixed three latent blockers that meant *no* form-nominated proposal had ever completed a mint: the assembler emitted `identity.added: "<set on approval>"` (placeholder, fails `format:date`) and `affiliation.unit_code` as a bare string (schema wants the `{scheme,value,source}` object) — both fixed so the manifest re-lints clean at approve (#1390, portal `sha-4c5d93f`); and both `operator-onboard.yml`'s `_identity_body` and `bundle.md.j2` read `manifest.ory_site.groups` unconditionally, crashing for the non-escalated tiers that legitimately omit it (participant identities with no service-admin claims) — defaulted to `[]` (#1393; the native path was already safe — `#[serde(default)]`). The assembler test had masked the added-date bug by post-hoc stamping; it now lints exactly what the assembler emits.
- This doc + [ADR 1062](../adr/1062-three-stage-delegated-candidate-onboarding.md) + [onboard-tier-role-model.md](onboard-tier-role-model.md) hold the accepted decisions.
- **Runner-minted manifests and the declared roster (split-brain rule, 2026-07-08).** The runner mints from its OWN repo checkout on the mgmt host, so the manifest it writes never reaches the operator's manifest store — and the declared roster then misreports the person. The portal DB is the authoritative record of every runner mint (each onboarded proposal carries its `manifest_json`), and `ops/scripts/sync-onboarded-manifests.py --env <env>` materializes that record into `ops/operators/<env>/` (never clobbers hand-authored files, validates everything it writes). **Run it before every roster regen**; the regen → roster PR → portal deploy then reconciles the COP. Folding this sync into `gen-roster.py` itself is the follow-on that removes the manual step.
- **Remaining in this flow** (all non-blocking — the chain is usable end-to-end today): (a) **notifications** — today both review gates are pull-only (no count, no alert); planned in [`onboarding-notifications.md`](onboarding-notifications.md) (in-portal count/badge + poll, then chat push to Matrix/Zulip on the two hand-off transitions); (b) **i18n** on the nomination form. **Admin action at stage 3 is BUILT** — the queue UI renders Approve/Reject for `sponsor_approved` (a `proposed` one shows "awaiting sponsor review") and `goat admin queue ls|approve|reject` is the CLI counterpart; the deployed **`onboard-runner`** turns an admin approval into an automatic mint. The **public-internet front desk** (`intake.zagros.example`) is BUILT + live (2026-07-01). The **stage-2 sponsor-review gate** is BUILT (2026-07-01). **Phase D native-path parity is now built** — the native `build_identity_body` emits the `aff` trait gated on `GOAT_ORY_AFF_TRAIT_ENABLED` (default off, matching the Ansible `ory_site_affiliation_trait_enabled`), so enabling Phase D is a coordinated flag flip on both paths (plus the Kratos schema deploy), not a code change; ships in the next `goat-cli` release to `get.zagros`.
- **Other named follow-on** (from the tier-role-model doc): the `IAM-ADMIN` oauth2-proxy L7 fence wiring (deploy-side).

The two next candidates are the public front-desk exposure (start from the security review against `nova-public-posture.md`; it forwards to the already-built `/api/onboard/submit`) and the distinct sponsor-review gate.
