# ADR 1077 — Affiliation coalition standing (framework-general) + institutional sector

| Field | Value |
|---|---|
| Status | Accepted-direction (2026-07-11) — vocabulary + framework registry + derivation resolver built (SoT `ops/identity/grammar.yaml` v1.3.0, registry `ops/identity/coalition-frameworks.yaml`, `generate.py --resolve-coalition`); intake capture + grant-eligibility enforcement build-sequenced |
| Date | 2026-07-11 |
| Repo scope | goat core (identity grammar SoT + manifest schema; onboarding console intake is the sequenced follow-on) |
| Related | ADR 1043 (the five-dimension grammar this amends), [ADR 1067](1067-identity-grammar-generated-sot.md) (the generated-SoT pipeline), [ADR 1051](1051-identity-descriptor-standard.md) (born-as cert vs currently-is registry; effective-dated attributes), [ADR 1074](1074-data-access-role-architecture.md) + [ADR 1075](1075-producer-data-labels-and-access-request-loop.md) (the data-access grammar whose reserved `coalition`/`releaseset` scopes this predicates), [ADR 1027](1027-partner-release-set-governance.md) (release-set governance), `instance-variability-model.md` (per-goatnet config), `docs/rfc/nato-data-policy-rewrite.md` (the FMN-form framing) |

## TL;DR (for a layperson)

Two related identity refinements, both about the org a principal belongs to, both driving what data it may be released.

1. **Coalition standing, region-general.** At coalition boundaries, some orgs are *members* of the operative alliance/coalition (they get the members-only release lane) and others are *partners* (bilateral lanes only). NATO is one such framework — but goat net is built to deploy in many regions, each centered on different players. So the standing is `member` / `partner` **relative to the goatnet's active framework**, and *which* framework (NATO, Five Eyes, a regional coalition, …) is a per-goatnet choice in a registry file — never baked into the code. NATO is simply the first entry.
2. **Sector, orthogonal to nation.** Within a government, a military unit and a national police force have very different legal authorities — and law enforcement often holds excellent sensors and data. So a new `sector` (military / law-enforcement / civil-government / intelligence, plus a demand-gated non-gov cluster — see §4) records that, independent of nationality and of the `kind` of org. It is the primitive the interagency dynamics (border security, gov-mil task forces) will build against.

## Context — the gap, and why framework-general

ADR 1043 deliberately did **not** make NATO a distinct affiliation `kind`; a NATO body was `kind: partner` + `nation: NATO`. Right for *entity type*, but it left the **coalition standing** — the property that governs release-lane eligibility — implicit in `nation`, and it was NATO-shaped. A first cut modeled `affiliation.bloc ∈ {nato, partner}`; that baked NATO into the machine grammar itself, which does not serve a goatnet standing up in another AOR. **This ADR generalizes before that solidifies:** the machine vocabulary carries a framework-neutral standing (`member`/`partner`), and the *framework* is per-goatnet configuration + a registry.

`data-access-roles.md` already **reserved** a `coalition` scope word with no enforcement point (G7 keeps it un-issuable until ratified). This ADR supplies the identity attribute that is that scope's eligibility predicate, and does so region-generally.

## Decision

**1. `affiliation.coalition` ∈ {`member`, `partner`} — framework-neutral standing, a new affiliation sub-attribute** (not a new `kind`, not a sixth dimension; the grammar stays five dimensions). `member` = the org's nation is a member of the goatnet's **active framework** (or a framework body) → eligible for that framework's member release-lane; `partner` = not a member → bilateral/coalition lanes only.

**2. A framework registry + per-goatnet active framework.** `ops/identity/coalition-frameworks.yaml` is a map of named frameworks, each `{display_name, kind (alliance|coalition|interagency), aor, release_lane, member_markers, members[]}` with effective-dated membership. NATO is the first entry (32 members, accession dates). Adding Five Eyes / AUKUS / a regional coalition is an additive edit — no code change; the resolver and validator pick it up. A goatnet declares its **active framework** at standup (instance-variability, like site names or the display lexicon); `default_framework` in the registry covers a NATO-centric deployment. Standing is resolved as `resolve(nation, active_framework, as_of)`.

**3. Registry-authoritative, effective-dated — never a cert stamp, never an Ory claim.** Membership changes over time (FIN `partner`→`member` 2023-04-04, SWE 2024-03-07) *and* the active framework differs per goatnet, so a frozen cert/claim value would be stale or wrong on another goatnet. Standing therefore rides the registry (ADR 1051 `unit_code` reasoning); relying parties resolve current standing from `nation` + the active framework. The manifest MAY seed an explicit value (operator assertion).

**4. `affiliation.sector` — two clusters, orthogonal to nation and `kind`.** A nation's army and its police share `nation` + `coalition` standing but differ in legal authorities and in the sensors/data they hold. **Governmental cluster (always live):** `military` = armed forces; `law-enforcement` = police/gendarmerie/border/customs; `civil-government` = a governmental non-military, non-LE, non-intelligence agency (homeland/emergency-management, diplomatic); `intelligence` = a national intelligence/security service — the IC, which spans military and civilian agencies and maps cleanly to none of the other three (added 2026-07-14, see amendment). **Non-governmental cluster (demand-gated, default-OFF):** `ngo` / `media` / `academic` / `commercial` — a positive sector for non-gov participants, replacing the "leave sector blank" default for them; accepted only where the goatnet flips `ONBOARD_SECTORS_NONGOV_ENABLED` (a non-gov value submitted with the gate off is dropped, not stamped). **Distinct from `kind: civil`** (which classifies the *relationship*, not the sector). Declared at onboarding (not derived) — a stable born-as attribution that rides manifest + registry (Ory-claim/cert carriage reserved until sector-scoped authz is enforced). Omitted where sector is unknown.

**5. `affiliation.coalition` is the eligibility predicate for the reserved data-access `coalition`/`releaseset` scopes; `affiliation.sector` is the reserved hook for interagency/authorities scoping.** A framework's member release-lane grant is admissible only to a grantee org whose current standing is `member` of the active framework — the predicate is framework-parameterized, so the same mechanism serves any AOR. Standing bounds *who is eligible*; it never issues a grant (recognition ≠ authorization; the grant stays a deliberate recorded originator act, ADR 1074 §8 / ADR 1075). Command relationship still prompts a grant, never derives one.

**6. Honesty grade.** Membership dates are public records, not an ingested official feed; a derived standing carries `open-source` provenance (ADR 1064) unless operator-verified. The registry is hand-kept; a new member/framework is additive.

## Extension model (thinking ahead, deliberately not built)

The whole point is flexibility to different regional dynamics without re-architecting:
- **New framework** (Five Eyes, AUKUS, a regional coalition): one additive entry in `coalition-frameworks.yaml`; a goatnet activates it. No grammar/code change.
- **New AOR**: a framework declares its `aor`; the AOR list is documentation of breadth, not a closed machine enum.
- **New standing / dynamic** (partners who are also rivals, interagency law-enforcement release tiers, associate/observer): an *additive* grammar value on `affiliation.coalition` or `affiliation.sector`, plus a framework `kind: interagency` — added when the dynamic's release rules are actually built (G7: no value without an enforcement point), never guessed now.

The `sector` attribute is the identity primitive those interagency/gov-mil-law-enforcement constructs codify against; this ADR ships the primitive, not the constructs.

## Living relationships — a data edit, never a rebuild

The external world changes: an ally leaves an alliance, a partner accedes, a partner cools toward adversary. Rigid FMN-style policy hardcodes these relationships and needs a rebuild — the network and services go obsolete the moment the map shifts. Here the map **is data**: membership is an effective-dated range (`member_since`, optional `member_until`) in `coalition-frameworks.yaml`, and policy resolves current standing from it, never from hardcoded rules. So a geopolitical change keeps the network running **in stride** with only a codification edit:
- **accession** → set/add `member_since`; **withdrawal** → set `member_until` (the org flips to `partner` from that date); a partner↔member swing over time is just these two dates; changes can be **pre-staged** (a future date) or backdated.
- **history is queryable** — `--resolve-coalition <NATION> --as-of <DATE>` gives the standing as of any date, so "who could see this in March?" answers from data, not archaeology.
- **adversary/exclusion** (a partner turned hostile): two data-edit levers, no code change — (a) the reserved `restricted` standing (an additive grammar value for "no release lanes," landing when its enforcement point is built, per G7); (b) the immediate lever today, mesh-first credential revocation/offboarding (ADR 1031, reversible) for the org's principals.

This is a deliberate contrast with hardcoded coalition policy, and a first-class design property, not an incidental one.

## Deployment simplicity + open-source neutrality

The code ships **entity-neutral**: no org, nation, or alliance is baked in. Every specific is data/config (the frameworks registry, the per-goatnet active framework, the display lexicon, site names). Because every affiliation sub-attribute beyond `{id, kind}` is **optional**, the default deployment is the smallest one — **organic-only** (single org, everyone `organic`, no coalition/partner/sector, community access), the "just try it out" path an open-source pickup gets with zero identity configuration. Turning on coalition/sector is a config decision (an active framework), not a code fork. See [`goatnet-deployment-profiles.md`](../design/goatnet-deployment-profiles.md).

## What this changes vs. leaves untouched

- **Changes:** `grammar.yaml` → v1.3.0 (adds `affiliation.coalition` + `affiliation.sector`); the generated manifest schema (3 synced copies; `generate.py --check` reproduces byte-for-byte); new `ops/identity/coalition-frameworks.yaml` registry (supersedes the first-cut `nato-membership.yaml`) + `generate.py --resolve-coalition <NATION> [--framework <id>] [--as-of DATE]` + CI validation; `identity-grammar.md` §1.2/§5/§6/§7 and `data-access-roles.md` §6.
- **Untouched (deliberately):** the cert URI-SAN grammar (no new cert attribute — standing is off the cert; sector reserved); the Ory `aff` claim + Kratos identity schema (so the `check-affiliation-kind-enum-sync.sh` parity guard is unaffected); the `affiliation.kind` enum; the Zenoh write anchor; NetBird groups. No re-mint, no ACL change, no wire change.

## Built vs. build-next (honest map)

- **Built now:** the vocabulary (grammar SoT + manifest schema), the framework registry (NATO populated), the framework-parameterized derivation resolver (`--resolve-coalition`; effective-dating + framework-selection verified: FIN/SWE pre-membership → `partner`, post → `member`; unknown nation/framework → `partner`; the `NATO` marker → `member`), and CI validation of the registry.
- **Build-next:** (a) onboarding-console capture — the intake form captures/derives `coalition` and captures `sector`, writes them to the manifest, and surfaces the release-lane consequence on the review card; the registry resolver returns current standing against the goatnet's active framework; the per-goatnet active framework wired into the console config (instance-variability). (b) Grant-eligibility enforcement — when the `coalition`/`releaseset` scopes are ratified (they consume release-sets, ADR 1027, designed-not-built), the grant surface checks grantee standing (and, later, sector). Until then those scopes stay reserved/un-issuable per G7.

## Alternatives considered

- **`affiliation.bloc ∈ {nato, partner}` (the first cut)** — rejected: bakes NATO into the machine grammar; does not serve other AORs; a rename to framework-neutral `member/partner` + a framework registry is the generalization, done before it solidified.
- **A new `kind: nato` / per-framework kinds** — rejected: conflates entity-type with coalition-standing; explodes with every framework.
- **Store standing per-org, denormalized** — rejected: drifts from `nation`; a nation's accession or a change of active framework would touch every org row. Derive from nation + active framework keeps one source of truth.
- **Stamp standing on the cert / forward in the `aff` claim** — rejected: mutable *and* goatnet-relative ⇒ stale/wrong-on-another-goatnet (the ADR 1051 lesson, doubled).
- **Fold sector into `kind`** — rejected: sector is orthogonal (a partner-nation org is `kind: partner` whether it is that nation's army or its police); overloading `kind` loses the cross-product.

## Amendment — 2026-09-07: standing derives from the ORG's controlling nation, and from nothing else

[ADR 1171](1171-person-nationality-and-org-ownership-are-two-facts.md) separates a person's own citizenship (`identity.nationality`) from the controlling nation of the organisation they belong to (`affiliation.nation`). That raises a question this ADR never had to answer while there was only one nation in the model: once an organisation can be staffed by several nationalities, which of them decides its standing?

**Only `affiliation.nation`.** A contributor's nation, a liaison officer's nation and an `assigned` member's nation are attribution and are never inputs to the derivation.

The rule is symmetric and both directions matter. An organisation owned by a non-alliance nation does not become an alliance `member` because one allied officer is attached to it — that would be an access widening produced by staffing, and it would let a posting decision grant a release lane. An alliance-owned organisation does not drop to `partner` because one non-alliance partner officer is attached to it, which would withdraw a lane from everyone in the organisation on the same evidence.

Nothing else in this ADR changes. Standing stays derived rather than stored, stays out of the cert and the Ory claim, and stays out of the ACL (§5). The derivation's input is now named precisely rather than being the only nation available.

## Amendment — 2026-07-14: `intelligence` (live) + demand-gated non-gov sector cluster

Two sector-enum additions, for participants that map cleanly to none of the original three governmental values:

- **`intelligence` (live).** A national intelligence / security service (the IC) spans military and civilian agencies and has authorities distinct from both `military` and `civil-government`; folding it into `civil-government` erased exactly the interagency distinction §4 exists to carry. Added to the governmental cluster and live fleet-wide, same carriage as the other three (manifest + registry; cert/claim reserved). `civil-government` is now explicitly the governmental non-mil, non-LE, **non-intelligence** agency.
- **Non-governmental cluster (`ngo` / `media` / `academic` / `commercial`) — designed, demand-gated, default-OFF.** Non-gov participants (non-profits, journalists, universities, companies) previously had no positive sector (blank). These give them one, without asserting non-gov classification is wanted everywhere. Gated behind the per-goatnet console flag `ONBOARD_SECTORS_NONGOV_ENABLED` (default off, mirrors the `NOTIFY_ROSENPASS` staged-rollout pattern): the enum values ship fleet-wide (schema + grammar SoT `gated_values`), but the console only offers them at intake and only stamps them when a goatnet opts in — a non-gov value submitted with the gate off is silently dropped, never stamped. No goatnet is live on the non-gov cluster yet; flip on demand. Per G7 the values exist because the intake enforcement point (accept/drop) exists; the downstream authorities/authz constructs remain designed-not-built as before.

Unchanged: cert URI-SAN grammar, the `aff` claim / Kratos schema, `affiliation.kind`, the Zenoh write anchor, NetBird groups. Enum lives in the grammar SoT (`ops/identity/grammar.yaml`: `intelligence` under the sector enum, non-gov under `gated_values`) + the two synced manifest-schema copies (schema-sync CI guard).
