# ADR 1067 — Identity grammar as a generated source-of-truth

**Status:** Proposed. Building (increments 1–2). Design: [`docs/design/identity/`](../design/identity/) (synthesis: `grammar-sot-synthesis.html`).
**Related:** ADR 1043 (the dimensions this codifies) · [ADR 1051](1051-identity-descriptor-standard.md) (the descriptor that packages the grammar; a generation target) · [ADR 1065](1065-graph-engine-property-graph-read-model.md) (the graph that projects it) · [ADR 0112](0112-tech-strategy-workload-shape-language-boundary.md) (choose tech by workload shape). Precedent: `ops/permissions/group-permissions.yml` + `ops/ci/check-ory-groups-enum.sh`; `ops/serve/services.yaml` + `generate.py`.

## Plain-language summary

The identity vocabulary — role, affiliation.kind, echelon, node-class, autonomy, unit-code schemes, the edge catalog, statuses, group axes (~13 controlled enums) — is today **duplicated by hand** across the manifest schema (×2), three Ory Kratos schemas, the NetBird taxonomy, the cert URI-SAN grammar, and a dozen doc tables, kept aligned by convention and three CI drift-guards. This ADR makes one machine-readable file the **single source** and **generates** every enforced artifact and the human reference from it, one-way (SoT → outputs, never prose parsed back). Adding or deprecating a value becomes one edit that regenerates everything; drift becomes structurally impossible instead of caught-after-the-fact. It is the foundational vocabulary layer the identity descriptor packages, the actor-relationship graph projects, and the RFC narrates.

## Context

The larger problem this serves: **there is no universal identity, and almost no one attempts one.** Every platform, org, nation, and vendor defines identity its own way, so connecting many systems means bespoke N×M mappings, meaning lost in translation, and trust that doesn't carry across the seam — the ceiling on interconnecting systems at scale. goatnet (a backbone connecting thousands of orgs and millions of systems across many nations and vendors) lives or dies on a single coherent identity that spans commercial/government/allied/civil actors and people/systems/processes. The same fragmentation appears in miniature inside our own stack, which is what this ADR removes.

A repo pass found the vocabulary codified in ~13 places with high duplication (role family: 5 sites; affiliation.kind: 4; ory_site.groups: 3) — held together by guards that *catch* divergence rather than prevent it (a band-aid over fragmentation, not a cure), with one un-guarded gap (`ory_idp.groups`) and a stale third Kratos schema. The `civil`-affiliation drift bug (fixed 2026-07-01: added to the manifest schema but not the Ory schema, would have been silently rejected by Kratos) is the exact failure this removes. The pattern already exists locally: `group-permissions.yml` is a structured SoT with a guard; `services.yaml → generate.py` already generates config. This generalizes that pattern from *guard* to *generate*, across all identity vocabularies. A best-practices review (SCIM RFC 7643, CUE, JSON Schema, SKOS, OpenFGA/SpiceDB, Ory Kratos, protobuf/Avro evolution) confirmed the approach and the format choice.

## Decision

1. **One SoT system, axis-separated.** `ops/identity/grammar.yaml` owns the identity-grammar dimensions; `ops/identity/service-groups.yaml` owns the Ory service/idp group axes (folding in `group-permissions.yml`) — kept separate because the grammar deliberately separates them (mesh-role `ADMIN` ≠ service-group `ADMINS`). One deterministic generator (`ops/identity/generate.py`) over all.

2. **YAML + a deterministic generator — not CUE, not an IDL.** The SoT is a *controlled vocabulary* (closed enums + per-value metadata) = data, not schema-logic; and half the outputs aren't schemas (cert value-sets, NetBird groups). YAML wins on review-ability + determinism; emitted JSON Schema uses `$defs`/`$ref`. (CUE is reserved as a possible later *backward-compat proof* gate, not the authoring surface.)

3. **SKOS-shaped per-value envelope.** Each value carries `definition` (migrated verbatim-faithfully from the vetted tables — identity-grammar.md, GLOSSARY, ADR 1043 — never re-glossed), `authority`, `mappings`, `scope`, `added`, `deprecated`+`superseded_by`. One envelope feeds both the schema `description` and the doc glossary.

4. **Three-source `authority` axis** (`doctrinal | native | industry`) — the reconciliation ledger for the three non-aligned identity worlds: existing mil canon (UIC/LOGFAS/echelon doctrine), novel goat-native vocabulary, and adopted industry patterns. **goat's native UUID anchor is the identity spine**; doctrinal codes are *mapped, provenance-graded attributes* — often latent, non-unique, coarse, or nation-scoped — attached to the anchor, never the key. goat conforms where doctrine is correct and applicable and records the delta where it isn't; it never renames or inherits the error. Formalizes the per-field source/authority provenance the descriptor already reserved (ADR 1051 §7).

5. **Generation targets:** the manifest-schema enum/`$defs` blocks (×2), the 2 canonical Kratos identity-schemas, the NetBird taxonomy value-lists, the cert URI-SAN value-sets, **the identity-descriptor schema (JSON Schema + protobuf) + its cross-tech map** (ADR 1051 §6/§11 — so the portable artifact can't drift from the vocabulary), a **versioned external vocabulary export** (the machine "actor ontology" external partners anchor to), and the human spec tables + GLOSSARY. A `git diff` regeneration gate fails CI on any hand-edit of generated content.

6. **Evolution rules (hard):** additive-only; deprecate-never-delete; never reuse a retired token; **any value ever minted into a cert or Ory identity stays valid forever** (the enum only grows; the registry handles "currently-is"); closed enums fail-closed on machine paths with a reserved `experimental` block; semver the vocabulary + stamp the version into every generated header — the same additive-minor / fail-closed-on-unknown discipline as the descriptor's `?v=<major>` negotiation, one layer down.

7. **Governance is federated:** additive-value PRs fast-path (CI proves additive-only); removals/renames require heavyweight ratification — the SoT is never a serialized-approval chokepoint.

## What it subsumes

Retires the redundant per-enum guards (`check-affiliation-kind-enum-sync.sh`, `check-ory-groups-enum.sh`) once their targets are generated; closes the un-guarded `ory_idp.groups` gap for free; audits and retires the stale third Kratos schema.

## Alternatives considered

- **Keep prose spec + drift-guards (status quo+).** Rejected — guards catch after the fact; generation prevents. The `civil` bug is the proof.
- **CUE / Protobuf / Smithy as the SoT.** Rejected as the authoring surface — heavy schema languages earn their cost only for constraint systems; ours is a vocabulary, and half its outputs aren't schemas. CUE reserved as an optional later backward-compat gate.
- **Prose doc as the source, parsed to structure.** Rejected — non-deterministic; derivation is strictly SoT → docs, one-way. Consensus across SCIM/OpenFGA/Kratos/CUE.
- **One merged vocabulary file.** Rejected — re-conflates the axes the grammar separated.

## Consequences

- Adding/deprecating a value is one SoT edit; every schema, taxonomy, cert value-set, and doc regenerates in lockstep — drift impossible.
- The portable descriptor + the external ontology export derive from the same vocabulary, so the "universal, cross-tech" promise is served by construction, not by hand.
- Cost: authoring the SoT (migrating today's vocabulary + definitions) + the generator + the regeneration gate; a one-time byte-for-byte reproduction proof before flipping any target.
- The definitions become the one carefully-human-authored part; everything downstream is mechanical.

## Rollback

Purely additive until a target is flipped to generated. If the generator can't reproduce a current artifact byte-for-byte, that target simply isn't flipped — no artifact is at risk. Flipped targets revert by restoring the hand-authored file + the guard. The SoT + generator are inert config until wired.
