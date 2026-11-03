# ADR 1151 — The target affiliation block: one field per question, `kind` derived and retired

- **Status:** Accepted — **built; migration steps 1 through 5 are in code as of 2026-09-02.** What remains is data, not code.
  - **Step 1** — `affiliation.gov_branch` and the target `sector` set (`state` / `commercial` / `civil-society`) in `ops/identity/grammar.yaml` + `ops/operators/schema/operator-manifest.schema.json`. Additive: the ADR 1077 values still validate. Grammar 1.4.0 → 1.5.0.
  - **Step 2** — `Organic`, `Reconcile`, `DeriveKind`, `GovBranch` in `ops/goat-id-console/internal/coalition/derive.go`. `organic` on the identity view; the seeded-standing reconcile on the review page. `OPERATING_SOVEREIGN` wired through console config + the Ansible role, default EMPTY.
  - **Step 3** — dual-read and dual-emit. `assembleManifest` writes `sector` and `gov_branch` (the latter only under `sector: state`); the org-registration handler derives `kind` when it is not given; `gen-roster.py` derives it too, and `ops/operators/test_gen_roster_derive_kind.py` fails if the Go and Python tables drift.
  - **Step 4** — intake now collects the target block: both copies of `onboard-intake-form.json` (portal + console) ask **Sector** and **Branch**, and `Kind` is demoted to *"legacy — derived if you leave it"*. The console's org-registration form asks the same two questions. The mechanical half of the backfill runs in `orgstore.Open`: the kind→sector map is total and lossless (`vendor`→commercial, `civil`→civil-society, `organic`/`partner`→state), so it is the same fact in newer words, not a guess.
  - **Step 5** — `affiliation.kind` is **no longer in the schema's `required`**. It is still accepted, still emitted, and an explicit value still beats the derivation.
  - **STILL NEEDS A HUMAN, and no code will do it:** `gov_branch` for every existing org. The old data does not contain it — `organic` says *ours*, not *army* — so the backfill deliberately leaves it empty rather than assuming `military`.
  - The original status line read *"Design only — the agreed target, not yet built. No schema change lands with this ADR."* That was true when written and was then counted as delivery in a close-out. It is recorded here rather than deleted, because the gap between an ADR and a deployment is what this repo keeps getting wrong.

- **Date:** 2026-09-02
- **Deciders:** operator
- **Amends:** ADR 1043 — corrects and supersedes its `affiliation.kind` value definitions. Everything else in 1043 stands.
- **Related:** [ADR 1077](1077-affiliation-coalition-standing.md) (standing is derived, never stored, never in the ACL) · [ADR 1148](1148-one-name-per-audience-the-circulated-model-is-canonical.md) (alliance vs coalition; the standing field, named `alliance_standing`, superseding the legacy `coalition`) · [ADR 1150](1150-multi-alliance-expansion-plan-only.md) (multi-framework is a query change because standing is derived) · [ADR 1052](1052-vendor-to-org-machine-rename.md) (the additive dual-read migration method this reuses) · `docs/design/identity-grammar.md`

## Why this exists

`affiliation.kind ∈ vendor · organic · partner · civil` asks **two independent questions in one field** — *what legal kind of thing is this* (government / company / non-profit) and *is it ours or foreign*. The two are only crossed for the governmental values, which is why:

- a **foreign commercial vendor** (a defence contractor from an allied nation) has **no correct value** — `vendor` drops the foreignness, `partner` drops the commercialness;
- `partner` the *value* collides with `partner` the *alliance standing*, so the same word means "foreign gov org" on one axis and "not an alliance member" on another;
- `sector` compounds it by packing branch-of-government, the cross-cutting `intelligence`, and non-governmental type (`ngo`/`media`/`academic`) into one flat list, so `academic` never sits cleanly beside `military`.

The fix is not a rename. It is to give each question its own field, derive what can be derived, and let `kind` fall out as a computed, deprecated convenience.

## The target block

| Field | Target | Change from today |
|---|---|---|
| `affiliation.id` | the containing organisation, kebab slug | unchanged |
| `affiliation.nation` | controlling nation — ISO 3166-1 alpha-3, alpha-2 accepted and normalised (US→USA) | unchanged (normaliser landed 2026-09-01) |
| `affiliation.parent` | edge to the parent org | unchanged |
| `affiliation.unit_code` | `{scheme,value,as_of}` codified unit id | unchanged |
| **`affiliation.alliance_standing`** | `member` \| `partner` — **derived** from `nation` + the goatnet's active framework; never stored, never in the cert, never in the ACL (ADR 1077 §5) | already present; a manifest may still seed it, and a seeded value must be **reconciled against** the derived one rather than trusted (open gap today) |
| **`organic`** | **derived**, not a field: `nation` == the operating sovereign's nation. Relative to the operator, never one fixed nation | new — replaces the "ours/foreign" axis currently smuggled inside `kind` |
| **`affiliation.sector`** | **`state` \| `commercial` \| `civil-society`** — the UN SNA institutional-sector split, decided by two tests: *market? (charges economically significant fees)* and *state-controlled?* | **new value set**, replacing the flat 8-value enum |
| **`affiliation.gov_branch`** | `military` \| `law-enforcement` \| `intelligence` \| `civil` — **only when `sector: state`** | new — the within-government branch, lifted out of the flat `sector` |
| `affiliation.kind` | **deprecated** — fully derivable (`vendor`≈commercial, `civil`≈civil-society, `organic`/`partner` = the derived organic test × a governmental sector); accepted for back-compat, never required | was required; becomes derived |
| `affiliation.sovereignty` | reserved (data-residency), unchanged — note its value format must be reconciled with `nation`'s before either is compared | unchanged |

The allied defence contractor now has a correct, complete answer: `sector: commercial`, `nation: <its nation>` → `organic` false, `alliance_standing: member`. No value is forced to carry a fact it doesn't hold.

## The sector homework

Level 1 (`sector`) is not invented here — it is the UN System of National Accounts institutional-sector split (general government / corporations / NPISH), which supplies a **decision rule** rather than taste: is the unit *market* (economically significant fees) and is it *state-controlled*. A state-owned arms manufacturer is market + government → `commercial`; a government lab is non-market + government → `state`; an independent NGO is non-market + not-government → `civil-society`. "Academic" stops being a category and becomes a question the two tests answer — which is why it never sat still in the flat list.

Level 2 (`gov_branch`) has no good public prior art — economics has no reason to separate an army from a foreign ministry — so it is ours. The four values are the sensible existing ones, relocated under `state` rather than beside `commercial`.

## Migration — additive, dual-read, no cert re-mint (ADR 1052 method)

1. **Add** `gov_branch` and the new `sector` value set to the schema and `grammar.yaml`, alongside the current fields. Nothing required yet.
2. **Derive** `alliance_standing` and `organic` at read time (both already computable from `nation` + framework); reconcile any seeded `alliance_standing` against the derived value and warn on conflict.
3. **Dual-read** `kind`: accept it where present, and where the new fields are present, derive `kind` from them. Emit both during the window.
4. **Stop requiring** `kind`; flip intake to collect `sector` + `gov_branch`; backfill the new fields for existing orgs (data work, needs a human — see ADR 1112's dedup/merge, which this composes with).
5. **Retire** `kind` as a required field last, telemetry-gated. **The cert is untouched throughout** — `aff-kind` in the URI-SAN is attribution only, never a write prefix, so an already-issued cert stays valid and nothing re-mints (the same accommodation ADR 1052 and 1147 made).

## What this ADR does and does not commit

- It **commits** the target block and the sector model as the destination, so the identity fixes now have one agreed end-state to move toward rather than each being argued separately.
- It **does not** build anything, and it does not set a date. The migration above is the plan; each step is its own change.
- The one decision it still leaves open, deliberately, is **which axis `partner`-containment binds to at mint** — `kind`-derived-foreign or `alliance_standing`-derived-non-member — which is an access decision tracked with the mint-time binding work, not a grammar decision.

## Consequences

- One field per question. `partner` means exactly one thing on the standing axis; `organic`/foreign is a derived property, not a value competing for the same slot.
- `kind` survives as a derived convenience for anything still reading it, then retires without a flag day and without touching a certificate.
- `sector` gains a decision rule, so classification stops being a matter of taste and the awkward values (`academic`, `media`) resolve by the two tests.
- The block composes with ADR 1112 (the org registry / dedup) and ADR 1150 (multi-framework): standing stays derived, so none of this creates a stored field that would foreclose serving more than one alliance.
