# ADR 1171 — A person's nationality and an organisation's ownership are two facts, and neither derives the other

- **Status:** Accepted — **decisions 1-3 and 6-7 are BUILT in code as of 2026-09-07; not yet deployed.** Decision 4 (standing from the controlling nation only) is written into ADR 1077 and unbuilt, because nothing reads a contributor nation yet. Decision 5 (lateral edges) is unbuilt. The phase order, its gates and the measurements are in `docs/project/org-affiliation-nationality-plan.md`.
  - **Built:** `identity.nationality` + `affiliation.relationship` + `org.composition` in the grammar and both manifest schemas; the `composition` column and its vocabulary in `orgstore`; the intake form asking the person about the person and no longer asking them for the organisation's nation; the person's nationality **required** at submit (2026-09-08; a system is exempt), refused before the invite is consumed; `alliance_standing` and `gov_branch` no longer dropped at the submit boundary; the org-create form inheriting nothing from a candidate's manifest; the relationship declared at the approval gate; and the registered-org requirement behind `ONBOARD_REQUIRE_REGISTERED_ORG`, default off and armed per goatnet.
  - **Also built, same day:** `roster.go` no longer gives a member their organisation's nation as their own. `gen-roster.py` projects `identity.nationality` onto each member row and `access.RosterEntry` carries it as `Nationality` beside the org's `Nation`; an undeclared nationality stays empty rather than borrowing the org's. The field is empty for everyone today because it post-dates every manifest in the store — the carriage is built, the data is not.
  - **Built 2026-09-07, later the same day:** `gen-roster.py`'s first-member-wins org facts are gone — `--org-source store` is the default, so the catalog supplies kind/nation/parent and the manifests supply membership. The claim that this needed an operator with the gitignored manifest store was wrong in one direction: CI cannot re-derive the roster and a worktree carries no gitignored files, but the operator's own checkout holds all of them. Measured at the flip: no org lost, member-less chain ancestors recovered, several facts corrected. A second defect surfaced with it — every alias the registry had ever recorded was inert in the projection, so per-person slugs kept masquerading as organisations; folding them shrank the roster with every member intact.
  - **Not built:** the typed lateral edges of decision 5.
  - The original status line read *"design only. Nothing in this ADR is built."* That was true when written and is recorded here rather than replaced, because an ADR whose status quietly tracks reality is one nobody can date.
- **Date:** 2026-09-07
- **Deciders:** operator
- **Amends:** ADR 1043 — the affiliation dimension gains a per-membership relationship, the org gains a composition class, and `affiliation.nation` is stated to be the organisation's and only the organisation's. Everything else in 1043 stands. Also reverses one decision recorded in `org-hierarchy-and-dedup.md` §"Create-new renders kind + nation as prefilled" — see [Decision 6](#decision-6--org-facts-are-typed-not-inherited).
- **Confirms and narrows:** [ADR 1077](1077-affiliation-coalition-standing.md) §5 — standing stays derived and stays out of the ACL, and this ADR fixes *which* nation it derives from.
- **Related:** [ADR 1112](1112-org-hierarchy-and-dedup.md) (the org registry this rests on) · [ADR 1148](1148-one-name-per-audience-the-circulated-model-is-canonical.md) (`alliance_standing`) · [ADR 1151](1151-the-target-affiliation-block.md) (`sector` / `gov_branch`; this ADR reuses its additive migration method) · [ADR 1051](1051-identity-descriptor-standard.md) (effective-dated edges) · [ADR 1027](1027-partner-release-set-governance.md) (release lanes) · `identity-grammar.md`

## Why this exists

Take a Canadian officer who works for a United States command. The model has one nationality field, `affiliation.nation`, the schema defines it as the **organisation's** controlling nation, and the intake form puts it in front of the **person** with the label *Nation*. They answer `CAN`.

Both readings of that record are now wrong at once. It is not the command's nation, and there is nowhere else for the person's own to go.

The single field is not the only problem. Two pieces of code infer each fact from the other, in opposite directions:

- `ops/operators/gen-roster.py` took an org's nation from the first member manifest that named one. A later member who disagreed produced `conflicting nation ... — using first` as a *warning*, and was discarded. One Canadian onboarding first would make a US command a Canadian organisation. **Fixed 2026-09-07** by defaulting to `--org-source store`: the catalog answers, and a disagreeing manifest is the thing that warns.
- `ops/goat-zenoh-portal/internal/access/roster.go:104-117` gives every member its org's nation back, because *"the member rows carry no affiliation of their own"*.

Neither inference is safe in general. A liaison officer sits at an organisation without being of it, and organisations recruit internationally without changing hands. An alliance headquarters registered under one nation's code is the same single-nation constraint applied to a body that belongs to dozens of nations.

The aggregation leaves a fingerprint: orgs with no nation at all, and the same nation spelled two ways (`USA` and `US`) as two distinct values.

## Decisions

### Decision 1 — nationality belongs to the thing it is written on

A person's nationality is their citizenship and lives on the person: **`identity.nationality`**, new. An organisation's national ownership is its controlling sovereign and lives on the organisation: **`affiliation.nation`**, which exists and whose meaning this ADR states explicitly rather than leaving to a schema description.

Neither is derived from the other in either direction, and the two are permitted to disagree. Disagreement is the normal state for a liaison officer and for an internationally recruited member; it is information, not an error condition.

### Decision 2 — an organisation declares a composition class

**`composition`**, new on the org registry, set once at creation:

| Value | Meaning | `nation` holds |
|---|---|---|
| `multinational` | Constituted by a body of nations. Being staffed by many nations is what the organisation *is*. | the body marker — `NATO`, `EU` |
| `national` | Owned by one sovereign regardless of who staffs it. | that sovereign's alpha-3 code |

An alliance headquarters is `multinational`. A national army command is `national`, and each lower echelon beneath it is more strictly so.

### Decision 3 — a membership declares its relationship

**`affiliation.relationship`**, new, declared per person by the admin at approval:

| Value | Meaning | Example |
|---|---|---|
| `constituent` | This member's nation is part of the organisation's national character. | a Belgian officer at an alliance headquarters |
| `assigned` | A full member working for the organisation. Their nation is their own and does not transfer to it. | the Canadian officer at the US command — and equally a US officer there |
| `liaison` | Attached from outside, representing another organisation or nation. | a Canadian liaison officer sitting at the US command but working for Canada |

The value describes the relationship, not the nationality, which is why a US officer and a Canadian officer at the same US command are both `assigned`. What separates them is `identity.nationality`, a separate field doing separate work.

This is the switch that governs both inferences, per membership:

| | member's nation contributes to the org's | org's access is inherited by the member |
|---|---|---|
| `constituent` | yes | yes |
| `assigned` | no | yes, intersected with the member's own nation |
| `liaison` | no | **no** — explicit grant only |

The `liaison` row is the one that changes behaviour beyond bookkeeping. An attached officer today inherits their host organisation's read lanes wholesale, because membership is membership.

### Decision 4 — alliance standing reads the controlling nation only

Standing (`member` / `partner`) and the derived organic test read `affiliation.nation` and nothing else. A contributor nation, a liaison's nation and an `assigned` member's nation are attribution and are never inputs.

An organisation owned by a non-alliance nation does not become an alliance member because one allied officer is attached to it — that would be an access widening produced by staffing. An alliance-owned organisation does not lose member standing because one partner officer is attached to it. One rule covers both directions.

### Decision 5 — containment stays singular; dual-hatting is a lateral edge

At most one open parent edge per organisation, unchanged (`org_parent_edges_one_open` in `ops/goat-id-console/internal/orgstore/schema.sql:72`). That constraint is what makes the downward roll-up in `expandOrg` terminate and be unambiguous, and a dual-hatted organisation is not an organisation with two parents.

Dual-hatting is expressed with the typed lateral edges already catalogued in `ops/identity/grammar.yaml` under `edge_types`. A dual-hatted national command: `parent → <national-hq>` for administrative containment, `c2-authority → <alliance-hq>` for the alliance hat, `supports ↔ <alliance-land-hq>`. `org_parent_edges` is already effective-dated and bitemporal, so it generalises with a `type` column rather than a second store.

Nothing about this makes an edge an authorization input. The relationship graph stays a derived read-only projection.

### Decision 6 — org facts are typed, not inherited

Org facts are entered by the admin from what they know about the organisation. The org-registration form stops prefilling `nation`, `sector`, `gov_branch` and `kind` from whichever candidate's manifest triggered the create (`ops/goat-id-console/internal/handlers/org_match_review.go:230, 245, 249, 264`).

This reverses a decision recorded in `org-hierarchy-and-dedup.md` line 90, which introduced the prefill to fix invisible inheritance after the 2026-08-11 `origin` mislabel. Making the inherited value visible and editable was an improvement on inheriting it silently; the answer this ADR reaches is that a person's self-description is not a source for an organisation's facts at all, visible or otherwise.

### Decision 7 — the organisation exists before the person is approved

Approval order: organisation registered → organisation's facts confirmed → membership relationship declared → person approved.

Approval refuses while the candidate's `affiliation.id` does not resolve in the org registry. `decideProposal` consults no registry today, so a candidate can be approved and provisioned carrying an organisation that exists nowhere.

The refusal ships behind `ONBOARD_REQUIRE_REGISTERED_ORG`, default false, armed per goatnet only after that goatnet's registry backfill is complete. Armed against a nearly empty registry it would refuse essentially every onboarding.

### Decision 8 — each onboarding input states which fact it captures

The candidate's intake form asks **Nationality — your own citizenship**, and that is the only nation question a person is ever asked, because it is the only nation fact they are the authority on. The organisation's nation leaves that page entirely.

The org-registration form, at admin review, asks the organisation's composition and controlling nation.

The approval step asks for the membership relationship, with both nations shown side by side.

## What does not change

- Organisation ids stay immutable and stay absent from every wire anchor. Nothing here re-keys anything.
- No certificate re-mints. Standing is derived and is deliberately absent from the cert and the Ory `aff` claim, so re-pointing its input costs one code path.
- No NetBird group changes and no Zenoh write-anchor changes. Affiliation remains identity-only.
- `ProducersUnderOrg` stays a pure downward containment roll-up with no notion of nation.
- Person→organisation membership is already a union at the enforcement layer (`dataroles.go:244`). This ADR does not add multi-org membership; it removes the reason the single manifest field was being made to carry two meanings.

## Consequences

Two existing behaviours are deleted rather than extended: the org-nation aggregation in `gen-roster.py` and the member-nation inheritance in `roster.go`. Deleting the second changes the inputs to release-lane derivation, so it soaks rather than fastlanes, and it is blocked on roster generation flipping to `--org-source store` — until that flip the org registry's facts do not reach the portal at all.

Everything else is additive. Two new columns with empty defaults, two new optional manifest fields, and forms that ask a clearer question. An older console binary ignores the new columns.

The registry backfill is the critical path and is human work: every organisation on every goatnet, each needing a composition, a controlling nation and a provenance grade decided by someone who knows the organisation. That is the point — these facts stop being inferred.

## Open

- The names `constituent` / `assigned` / `liaison`, and `composition`. They land in the grammar, both schemas and a CI gate.
- Whether `composition: multinational` participates in standing derivation. Recommendation: no, per Decision 4. Not ratified.
- Whether an `assigned` member is inside a strict org-and-owning-nationality encryption audience by default, or only by a deliberate widening. Recorded in `backlog.md` `grant-audience-nationality-intersection`; under the strict reading `assigned` and `liaison` become identical at the crypto layer, which they are nowhere else in this model.
- Whether every goatnet is in scope for the backfill or some are explicitly deferred.
