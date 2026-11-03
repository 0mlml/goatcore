# ADR 1112 — Org hierarchy as first-class registry rows, filled in at onboarding, fuzzy-matched before minted

| Field | Value |
|---|---|
| Status | Accepted — **built, partially adopted** — status corrected 2026-08-31 (was "Proposed (2026-08-04, design session)"; the implementation landed the *same day* as the design session and the line was never moved). **Built:** `#2095` 2026-08-04 (`orgstore` — store/match/merge/schema, the `/api/org-registry/{orgs,suggest,export}` routes, `sync-org-catalog.py`, and the `gen-roster --org-source store` overlay), extended by `#2442` 2026-08-11 (update verb + admin-confirmed facts at create). **Enabled:** per goatnet by `id_admin_console_org_match_enabled: true`; OFF elsewhere, where `ORG_MATCH_ENABLED` takes its `false` default — which is why `goat admin org tree|update` answer 404 there. **Adoption gap CLOSED 2026-09-07.** It read: *the catalog holds a handful of orgs against the much larger committed roster, because `gen-roster.py` defaults to `--org-source manifests` and no caller anywhere pins `store` … the org store is authoritative by design and unread in practice.* `--org-source store` is now the DEFAULT, and `apply_org_catalog` folds aliases into the projection — so merge aliases and `name-variant` aliases both reach the roster the portal consumes, which they never did before. Measured at the flip: no org lost. `sync-org-catalog.py` is still hand-run with no CI/Ansible/timer reference — that part of the gap stands. See F-389 and [ADR 1147](1147-one-noun-per-thing-slot-versus-organisation.md). |
| Date | 2026-08-04 |
| Repo scope | goat core — identity plane (org registry, onboarding review surfaces), roster projection, data-access resolvers |
| Supersedes | — |
| Superseded by | — |
| Related | ADR 1043 (affiliation grammar + reserved edge catalog) · [ADR 1051](1051-identity-descriptor-standard.md) (registry = currently-is, effective-dated edges) · [ADR 1065](1065-graph-engine-property-graph-read-model.md) (derived read-models — scoped by D6, not amended) · [ADR 1064](1064-unit-reference-and-verification-service.md) (match engine + provenance grades reused) · [ADR 1048](1048-email-keyed-idempotent-onboarding.md) / F-239 (person-plane dedup sibling) · [ADR 1062](1062-three-stage-delegated-candidate-onboarding.md) (the review gates the match step lands in) · [ADR 1102](1102-organization-owned-data-sharing.md) / [ADR 1105](1105-pubsub-org-admin-co-publish-write-grants.md) (org-keyed authority held non-widened) · design: `org-hierarchy-and-dedup.md` |

## TL;DR (for a layperson)

Today the system knows organizations only as flat, unrelated name tags that spring into existence whenever an onboarding form mentions them — so "Northland Air Force" and "Northland Land Component" can't be related to Northland, and an abbreviation like "NAF" quietly becomes a second, duplicate Air Force. This decision makes organizations real records: created deliberately, related to their parent organization, matched against what's already registered before a new one is minted, and mergeable when duplicates slip through. The structure is built **as people onboard** — nobody sits down to author an alliance org chart in advance — and nothing that already exists (grants, certificates, keys) ever has to be re-issued when a parent is attached.

## Context

Operator-identified gap: enrollees from sub-units of one parent organisation land as unrelated flat orgs because nothing better exists. The consuming machinery is already built and ratified: the manifest schema carries `affiliation.parent`, the roster projection loads it, the data-roles resolver walks it (`expandOrg`, cycle-guarded downward roll-up), and `data.view.org:<parent>`-covers-children is the ratified stance (data-access-roles.md §6.1). What's missing is upstream: no org authority record (orgs are a side effect of manifest aggregation; every live `parent` is null), no match step at onboarding (free-typed names mint silent duplicates — the org-plane F-239), no fill-in affordance, no merge story. Design detail: `org-hierarchy-and-dedup.md`.

## Decision

1. **Orgs become first-class registry rows** in the identity plane (goat-id-console store + generated committed catalog, the ADR 1067 pattern): `orgs` + effective-dated `org_parent_edges` + `org_aliases`. Created, parented, aliased, merged as deliberate audited acts. `gen-roster` reads org facts from this store; the portal resolvers consume the roster unchanged.
2. **`parent` is a registry currently-is fact** (ADR 1051): single current parent (vertical containment, the grammar's v1 edge), effective-dated, as-of-date queryable. Roots are normal; chains have whatever depth reality has; no level dispatch. Lateral edge types stay reserved, not built here.
3. **Attach, never re-key.** Org ids are immutable and absent from every wire anchor (ADR 1043/1025), so existing flat orgs migrate by gaining parent edges with **zero** credential, grant, or key churn. Re-parenting is an audited, justification-recorded governance act whose surface shows subtree-grant impact before commit.
4. **Match-or-create at onboarding, fuzzy-matched, human-confirmed.** The sponsor/admin review surfaces gain a ranked-candidate picker (normalized trigram over ids + names + aliases, in-process over the small catalog) with a create-chain affordance for missing ancestors, minimal fields, ADR 1064 provenance grades. Never auto-merge, never refuse (org names have no ground-truth key — unlike the person plane's email); confirmed matches write alias rows, explicit overrides write negative pairs.
5. **Merge = alias absorption, reversible.** A duplicate's id becomes a permanent alias of the winner (deprecate-never-delete); every consumer resolves through aliases so nothing keyed to the old id breaks; un-merge is the reversal. A merge joining two orgs with active governance/grants/pools requires explicit acknowledgment of the combined authority set.
6. **Hierarchy never widens org-keyed authority** beyond the one ratified channel (the `data.view.org` vertical roll-up). `data.govern.org` (ADR 1102), co-publish at-most-own-org (ADR 1105), and sponsorship caps read the literal org node — asserted by tests with populated-chain fixtures. **ADR 1065 is scoped, not amended**: parent edges are registry SoT rows (what the derived read-models are rebuilt *from*); no graph engine enters an enforcement path.

## Alternatives considered

- **Pre-built ontology / ingest an ORBAT first.** Rejected — explicitly counter to the operator requirement; authoritative catalogs are access-controlled anyway (ADR 1064 phase 2); incremental fill-in accretes exactly the structure the population needs.
- **Auto-merge above a similarity threshold.** Rejected — no ground-truth key on the org plane; near-identical names can be genuinely distinct orgs; a wrong merge is a governance union.
- **Refuse on match (the ADR 1048 pattern).** Rejected — right for emails (ground truth), wrong for names; confirm-or-create keeps the human in the loop without blocking a genuinely new org.
- **Encode hierarchy in ids/keys** (`northland/land/…`). Rejected — the standing ADR 1043/1051 invariant: relations are mutable, keys are frozen; would make every reorg a re-key event.
- **Org store in the portal (data plane).** Rejected — the review surfaces where match-or-create runs live in the console, and the extraction plan already moves identity stores there; the portal keeps consuming the roster projection.
- **Amend ADR 1065 to allow an authoritative graph.** Not needed — the registry was always the SoT; only registry `parent` rows are authoritative, every graph engine stays a derived read-model.

## Consequences

- Nation → service → command becomes representable, queryable (current + as-of-date), and grantable at any level (`data.view.org:<ancestor>` covers the subtree via the already-built roll-up) — with the flat-org migration a no-op on credentials by construction.
- Name-variant duplicate minting stops being silent; the catalog's recall improves with every confirmed match (alias accretion).
- New obligations: the reviewer does the match/attach work at onboarding (deliberately — the enrollee is never blocked); merge carries a governance-union acknowledgment; the non-widening invariants must land as tests, not prose.
- Related fix riding the same code: sponsor attribution on operator-key invite mints honors `X-Goat-Operator-Actor` so invites record a person, not `"operator"`.
- Build: ~2–4 impl sessions (registry + roster wiring; match-or-create surfaces; merge + CLI + invariant tests; backfill sweep over the existing catalog). Design doc §12.

## Rollback

Nothing is deployed by this ADR (design only). At build time each increment is independently revertible: the org store is additive (roster generation can fall back to manifest aggregation); the match step is a review-surface affordance (removing it restores free-typed intake); merges are reversible by un-merge; parent edges are effective-dated rows that close without deleting history.

## Amendment 1 — bulk backfill dropped (2026-08-09)

The build map's increment 4 (bulk import of the legacy roster catalog into the
org registry) is **dropped by operator decision**. It was a build-map inference,
never an operator requirement — the stated requirement is "the ability to
appropriately deal with orgs as they come in" — and its own mandatory dry-run
refuted it: the roster's "orgs" mix genuine affiliations with seed-model
per-person producer rows, so a bulk import would re-introduce the producer/org
conflation ADR 1043 separated. The registry accretes **incrementally at
onboarding** (proven in use soon after deploy), with deliberate one-line
`register` for any org worth pre-seeding. The dry-run tool is re-scoped to a
legacy-duplicate awareness report (roster/manifest hygiene). The §13.5 capstone
is re-scoped to real-use validation (design doc §13.5).
