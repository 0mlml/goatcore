# Review-gate field edits — correcting a submission before anything is minted

| Field | Value |
|---|---|
| Status | Design canon for ADR 1137. Slice 1 built and **live on ZAGROS** (first field edit 2026-08-18, `prop-1903570eabebf577`). |
| Scope | What a reviewer may change on a pending onboarding proposal, what they may not, and why the boundary sits where it does. |
| Related | [three-stage-candidate-onboarding.md](three-stage-candidate-onboarding.md) (the gates) · org-hierarchy-and-dedup.md (the registry the org field resolves against) · id-admin-console.md (the surface) · operator-onboard-utilities.md (the manifest the edit writes) · [operator-action-audit.md](operator-action-audit.md) |

## 1. The failure this exists for

A candidate typed their own **name** into "Organization / unit". Intake slugified it into `affiliation.id`; that is not a registered org, so the review page raised its org-match panel — which offers **only fuzzy-suggest candidates**. A person's name has no similarity to the intended org's id, so the right org could not appear at any threshold. The reviewer's two available actions were *attach to a wrong org* or *create a duplicate org named after the person*.

There was also no headless path. Ten proposal routes exist; none writes the manifest. `UpdateProposalManifest` had exactly one caller: the browser-only org-attach handler. So a correction had no audited path.

**The lesson worth keeping:** a capability that exists only behind a browser session gets exercised with `sqlite3` instead. The machine-realm API in §4 is the actual remediation; the form is the convenience.

## 2. Why the existing affordance could not do it

Org-attach was built for **name variants** — an abbreviation vs the full name — where similarity is what makes matching work and where accreting the typed string as an alias is correct learning. A wrong-field entry is a different failure class: the string is a variant of nothing, and aliasing it would put a person's name permanently into the org namespace. Attach is the right tool for its case; it was the only tool.

## 3. The closed set — and the fields deliberately outside it

Editable in slice 1:

| Field | Rule |
|---|---|
| `affiliation.id` | must **resolve** to a registered org id or alias (ADR 1112). Never creates one — creation stays the deliberate `org-new` act that records negative pairs. |
| `affiliation.nation` | ISO 3166-1 alpha-3, or a multinational marker (`NATO`, `EU`). Load-bearing: coalition standing is derived **from** nation, so a blank silently resolves to `partner`. |
| org display name | `vendor.display_name` / `org.display_name` — display only, both keys honoured (ADR 1052 dual-read). |
| `identity.github` | the repo-grant handle; absent means repo grants skip the person. |

**Not editable here, with the reason — none of these is caution:**

| Field | Why not |
|---|---|
| `identity.email` | the person-plane dedup spine (ADR 1048 / F-239); its uniqueness check ran at invite time. |
| `identity.handle` | the manifest filename and a peer-label segment. |
| `zenoh.trial.{org,vendor}` | the per-person **URI-SAN authority**. It must stay distinct per person or the portal 409s every mint after the first. This is the field most likely to *look* like it should follow the org — it must not. |
| `tier`, `netbird.auto_groups` | the sponsorship-capped authorization decision (ADR 1071), not a typo class. |

Unknown field names are refused with a 400 rather than ignored, so a caller cannot believe it set something it did not.

## 3a. Every rendered box must be prefilled — the near-miss that made it a rule

The form promises *"leave a box untouched to leave the field alone."* Nothing in the browser door can keep that promise on its own: it posts **every field it renders**, and an empty value is meaningful — it CLEARS the field (`setNation` deletes `affiliation.nation`, `setGitHub` deletes `identity.github`, deliberately, so a handle entered in error can be removed). "Untouched" and "cleared" arrive identically. The only thing that makes the promise true is that every rendered box starts prefilled with the value currently in the manifest. The org display name is the one box a blank does not clear: the schema requires a name, so a blank is refused (see the draft case below).

Nation was the exception, and it was empty exactly where it mattered. Its prefill was sourced from the **org-match panel**, and that panel suppresses itself the moment the typed org resolves in the registry — correctly, since there is no match left to decide. So the sequence was:

1. Candidate submits `affiliation.nation: USA`. The org is unregistered, the panel renders, the box shows `USA`.
2. Reviewer registers the org via create-new (`onboard_org_new`, carrying `nation: USA` into the registry).
3. The org now resolves → the panel returns zero facts → **the nation box renders empty from that click onward**, on every later view and at every later stage, while the manifest still says `USA`.
4. Any subsequent *Apply correction*, for any unrelated field, posts `affiliation.nation=""` and deletes it.

Observed live 2026-08-19 on `prop-cbfe2e78867c623b` at both the sponsor and admin gates. Step 4 did not happen — the audit carries no `onboard_field_edit` for that proposal and the manifest kept `USA` — so this is a near-miss, not a loss. Had it fired, the consequence is not cosmetic: coalition standing is **derived from the manifest's nation** (`repoSubjectWith`), a blank resolves to `partner`, and repo-access policy keyed on standing would then grant a member's lanes to nobody.

**The invariant, now pinned by `TestFillEditable_everyRenderedFieldIsPrefilled`:** `fillEditable` reads every prefill from the manifest, never from a panel that may or may not render. A prefill sourced from an adjacent feature inherits that feature's suppression rules, and suppression rules are written for the panel's own correctness, not for this form's.

The same sourcing mistake had a second, louder consequence in the org box, which was composed as *typed value, falling back to the resolved org* — `index $p.OrgResolved 0`. `index` on an empty slice is a template **execution error**, so on every path where both panels are quiet the whole onboarding page failed to render. Those paths are the ones `orgMatchesForProposal` and `orgChainForProposal` explicitly document as degrading rather than 500-ing: org matching disabled on a goatnet, a matcher hiccup, an org-store read failure. A deliberate quiet degradation was taking the page down. Pinned by `TestTemplates_editorWithNoOrgPanel`.

**A registered org never overwrites the proposal, and a candidate's answer never feeds the org.** Since ADR 1171 the form no longer asks a candidate for their organisation's nation at all — it asks for `identity.nationality`, the person's own, which is a different fact and theirs to state. The org's nation is typed by an admin at registration. Where a manifest's nation and the registered org's nation differ, the surface says so beside the box rather than reconciling them: the manifest value is the one standing and repo grants read, so the divergence has to stay visible.

**An empty box is seeded from the registry, never from the candidate (#1122).** A candidate who typed an org that resolves carries no `affiliation.nation`, because intake asks only for their nationality and the seed at `onboard_effective.go` covers a sponsor-pinned org only. The box rendered empty and the reviewer read the registered nation off the hint and typed it back in; a blank left there resolves standing to `partner`. `seedNation` (`onboard_review_nation.go`) now fills an EMPTY box from the resolved org's registered nation and the card names the source org, because Apply writes the value to the manifest. It is the pinned-org rule with a reviewer's click in between, and it is a registry fact, so ADR 1171 Decision 6 holds. Three consequences of a box that posts a value it used to post empty are handled server-side. The card posts a hidden `affiliation.nation.seed` naming the org; an untouched seed (posted value still equals that org's registered nation) is written with `affiliation.nation.source: registry:<org>` on the audit row, so an unrelated fix on the card never reads as the admin correcting the nation, and a value the admin typed over the seed carries no tag. When the admin changes the org in the same Apply, the seed belongs to the org they left; because edits apply in field order (`affiliation.id` first), `reconcileSeededNation` re-reads it from the org they chose, or drops it from the Apply when that org has no usable nation, so the old org's nation never stands beside the new one. The registry does not shape-check what it stores while `setNation` refuses anything that is not a 2–4 letter code, so a registered value that would fail Apply (`NATO-ACT`, a country name) is not seeded: the box stays empty and the divergence hint still shows it. For an org the registry does not know, the create-new Nation box stays empty and the candidate's nationality shows beside it as a labelled reference. A one-click "use the candidate's nationality" fill would amend Decision 6 and is not built. Pinned by `TestSeedNation` and the `TestReviewNation_*` cases, which drive the browser form door.

**A draft has no manifest to prefill from, so the editor prefills from what completion would write.** An incomplete draft stores the intake record, not a manifest, and reading the boxes off that record's shape rendered every one of them empty. Observed live on zagros, 2026-09-23 (`prop-8edb2e9725f403a7`): the first Apply completed the draft, then wrote the empty display-name box over the name completion had just filled, and the admin gate refused the row with `proposal no longer passes the onboard lint gate` and no field named. `editableManifest` (`field_edits_prefill.go`) now prefills a draft from `assembleDraft`, the same assembly `completeDraft` persists, without persisting it; a draft that cannot assemble prefills nothing, and Apply on it answers 409 as before. Two server-side rules stand behind the prefill, because the browser door cannot keep the promise alone: `org.display_name` refuses a blank outright (the schema requires a name, so a blank is never a clear), and `lintRegression` (`field_edits_lint.go`) refuses any edit that would take a gate-clean manifest to a gate-failing one, before the write, naming the field. A manifest that already failed is not held hostage to the edit, which may be the correction. The approve gate's own refusal names the findings too. With the boxes prefilled, Apply with nothing changed is a natural click on a draft, and it used to complete the draft and write its audit row before answering `nothing to change`. `precheckDraftEdit` (`field_edits_diff.go`) now runs the edits against the same assembly first, so a no-op or refused edit leaves the draft incomplete with no `onboard_draft_complete` row. Pinned by `TestFillEditable_draftPrefillsWhatCompletionWouldWrite`, `TestHandleFieldEdits_draftApplyKeepsTheDisplayName`, `TestHandleFieldEdits_draftNoOpApplyWritesNothing`, `TestApplyFieldEdits_blankDisplayNameRefused`, `TestLintRegression_refusesTheEditThatWedgedTheApproveGate` and `TestDecide_lintGateRefusalNamesTheFinding`.

## 3b. Org facts are not a field edit, and the org box stops being editable here

[ADR 1171](../adr/1171-person-nationality-and-org-ownership-are-two-facts.md) draws the line this surface was blurring. **Correcting what a candidate got wrong about themselves is a field edit. Deciding what an organisation IS is not** — it is an org-registry act with its own audit trail, its own provenance grade, and a different blast radius, because an org fact is shared by every current and future member.

Two consequences here:

- The `Organization` box on this surface stays a *pointer* — it resolves the person to an already-registered org and refuses an unregistered one, which is what it already does. It never carries the org's nation, sector, branch or composition, and the create-new panel beside it stops prefilling those from the triggering candidate's manifest.
- **The person's own `nationality` is inside the closed set** and correctable here like any other self-description. **The organisation's `nation` is not**, and a reviewer who needs to change it uses `goat admin org update` — an audited org act, before → after recorded.

§3a's rule that every rendered box must be prefilled still holds for the person's fields. It is precisely what must NOT happen to the org's.

## 4. Two doors, one engine

```
  browser form  ──┐
  (session+CSRF,  │
   admin gate)    ├──▶ applyFieldEdits()  ──▶ validate (closed set, org resolve, nation shape)
                  │         │                 ──▶ stage gate (proposed | sponsor_approved)
  machine API  ───┘         │                 ──▶ UpdateProposalManifest (409 past review)
  POST /api/onboard/        └──────────────── ──▶ audit `onboard_field_edit` + per-field before/after
    proposals/{id}/fields
  (operator key)
```

One validation path, one audit path. The API takes `{"fields": {"affiliation.id": "org-1", …}}` and returns the applied diff, so a caller sees what actually changed rather than assuming.

## 5. The stage boundary

Edits are admissible while the proposal is `proposed` or `sponsor_approved`. After that the identity, the peer label and the cert URI-SAN exist, and an org change stops being a row edit and becomes the ADR 1051 born-as-vs-currently-is problem — owned by `goat admin org` plus a re-mint, deliberately. The store already refuses a late write; the handler renders that as a 409 naming the remedy instead of a bare failure.

## 6. Audit shape

`onboard_field_edit`, on the same F-207 emitter as every other console mutation, carrying actor, proposal id, and a per-field `{before, after}` map restricted to the closed set. Before/after rather than a bare "edited" for two reasons: the wrong value is evidence about the **intake form** (a field corrected repeatedly is a form defect, not a user defect), and the next reviewer of the same org needs to see what was corrected and by whom.

## 7. Built vs next

**Built (slice 1, this track):** the closed set, both doors, the org typeahead on the edit form, the stage gate, the audit diff, and tests covering the refusals (unknown field, unresolvable org, malformed nation, post-review 409, sponsor refused) as well as the accepts.

**Not built, and named rather than absorbed:**
1. **Sponsor-scoped editing.** Slice 1 is admin-only, matching the standing "sponsors vouch for people, not org structure" posture. Whether a sponsor may fix their own invitee's nation or display name is a real question; the gate is one line once it is answered.
2. **Candidate self-correction.** The better fix for the candidate's own typos; needs its own token and notification design.
3. **A `goat` CLI verb.** The API is usable today with an operator key; a first-class verb needs a CLI release to reach operators, so it rides the next one.
4. **Post-mint org moves.** Still delete-and-re-mint by design.

## 8. The upstream half — say what a field decides

Correcting a field at review is the safety net. The cheaper fix is the candidate getting it right, and the form was not helping: **three of the four consequential fields are labelled "optional"**, which reads as "unimportant". The live cohort produced a candidate's own name in Organization and two blank GitHub usernames.

Both the invite email and the intake form now name the consequence rather than the format:

| Field | What the candidate is now told |
|---|---|
| Organization | your employer or unit, **not your own name**; it is your organization of record, and teammates should type the same name |
| GitHub username | repo access is granted to this account at approval — **blank means no repositories** |
| Nation | your organization's controlling nation; releasability follows from it, so blank is treated as the most restrictive case |
| Machines / devices | it sets the allowance on your enrollment key — count the reinstall you will do later |

The email carries the same four, because a candidate reads it before they see any form. Both are pinned by tests (`TestIntakeForm_explainsWhatFieldsDecide`, `TestComposeInvite_namesTheConsequentialFields`) — this is copy, which is the first thing edited away by someone tidying, and nothing else would notice.

The machine-quota note also closes a real trap: the same number sets the setup-key device allowance, and a candidate who leaves it at zero on a goatnet running a pre-#2629 minter gets a **single-use** key whose second use fails with "couldn't add peer: setup key is invalid".

## 9. Deploy note

Nothing changes on a goatnet until the console pin moves. ZAGROS carries slice 1; other goatnets fall back to reject-and-re-invite until their pin moves. The §3a prefill fix is a console change and needs the same pin bump to reach a goatnet.
