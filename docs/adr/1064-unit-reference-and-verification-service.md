# ADR 1064 — Unit reference & verification service: autofill + verified-match for unit names and identity codes

**Status:** Proposed. Designed-not-built. Design: `unit-reference-service.md`.
**Related:** ADR 1043 (affiliation `kind` + `unit_code` — the fields this verifies) · [ADR 1061](1061-operator-onboard-intake-system.md) (the intake form this feeds) · [ADR 1051](1051-identity-descriptor-standard.md) (the effective-dated registry the codes live in) · `actor-relationship-graph.md` (the Postgres reachability read-model the reference set shares).

## Plain-language summary

When an operator onboards a unit, help them fill the unit name + identity code the way an address-correction service helps fill an address: type it, get ranked matches, and either confirm an exact match, collapse to a close known one, or deliberately enter a new value. Each entry is labeled with an honest provenance grade so "verified" means matched against a real authority — not just typed. Because the official code registries (national unit-identification catalogs, alliance ORBAT systems, ERP org codes) are controlled and have no *open* authoritative source, the service is built in two phases: a matching engine + provenance model + open-source **name** seed now, and ingestion of the **authoritative controlled registries** once a deployment's accreditation authorizes holding them. Phase-1 work is not throwaway — the authoritative adapters plug into the same table and upgrade the same entries in place.

## Context

The affiliation grammar (ADR 1043) now carries a codified `unit_code` (`{scheme, value, as_of, source}`) for organic + partner (allied) units. Left as free text, it is error-prone (typos, inconsistent unit naming across a coalition) and unverifiable. Operators asked for autofill + verified-match + fuzzy-collapse, seeded from official registries where an authoritative source exists.

Two facts shape the decision:

1. **No open authoritative source for the codes.** Unit-identification catalogs, ORBAT systems, national schemes, and ERP org codes are all controlled. The code *is* the sensitive artifact. Leaked/unofficial copies must not be ingested.
2. **Accreditation changes the ceiling.** A deployment accredited to hold controlled data *can* hold the official unit registries (the stable catalog; not active-operations comrels). So authoritative ingest is not impossible, it is **gated on accreditation**.

## Decision

Build a **unit reference & verification service** on the portal intake path, backed by the registry Postgres, in two phases.

1. **Names vs codes are split.** Match primarily by unit **name** (fuzzy, human-typed); offer the **code** from the matched record (phase 2) or accept a hand-entered one graded honestly (phase 1). Names have partial open sources; codes do not.
2. **Provenance is a first-class, honest grade** on every entry and on `affiliation.unit_code.source`: `authoritative` (matched an ingested official registry) > `operator-verified` > `open-source` > `self-asserted` (typed, unmatched — shown UNCONFIRMED, never a false green). **Attribution only, never a gate** — onboarding never blocks on a match; a self-asserted code upgrades automatically if a later authoritative ingest matches it.
3. **Match engine = `pg_trgm` on a `unit_reference` table** in the Postgres we already run — no new datastore. Effective-dated + `recorded_at` rows mirror the ADR 1051 descriptor's bitemporal edges, so a code changing on reorg is a new row (as-of-date queryable), not an overwrite.
4. **Intake UX offers, never forces, a correction.** Exact-match confirm / close-match collapse / deliberate-new — a genuinely new or renamed unit must always be enterable.
5. **Phase 2 is a source-adapter pipeline**, one adapter per authority (national catalog / alliance ORBAT / national scheme / ERP), all writing `provenance=authoritative` rows with a `source_id`, resolved by authority precedence. Batch reference ingest, re-runnable, versioned by date — **not** a live query into those systems.
6. **The data-handling gate is hard.** Authoritative ingest happens only after accreditation, with the reference store meeting the controlled-data bar. The populated code set is **not** public-safe (inherits roster sensitivity). Active-operations comrels are out of scope; only the stable unit catalog is ingested; no leaked/unofficial data ever.

## Alternatives considered

- **Free-text only (status quo).** Rejected — the operator ask is exactly to remove the typo/inconsistency/unverifiability of free text across a coalition.
- **Ingest open/leaked code datasets now.** Rejected — the codes are controlled; the only "open" copies are leaks. Security + legal non-starter. Open **names** are fine.
- **Wait for accreditation and build nothing now.** Rejected — the match engine, provenance model, intake UX, and open-source name seed are all buildable now and are precisely the substrate the authoritative adapters plug into. Building them first de-risks the phase-2 ingest to a data-loading exercise.
- **Live query into the registries at onboarding.** Rejected — those systems are not integration surfaces we drive live; batch reference ingest is the right coupling and works offline.
- **A standalone reference microservice.** Rejected — the portal already owns the intake auth, the registry Postgres, and the ADR 1061 form; a separate service duplicates all of it.

## Consequences

- Onboarding gains address-correction-grade autofill for unit names now, and real authoritative code verification once accreditation lands — without a re-architecture between the two.
- "Verified match" has a precise, honest meaning (authority-matched), and unverified values are labeled, not hidden.
- The reference store becomes controlled-data-sensitive once codes are ingested — a deployment obligation, tracked against the deployment's accreditation.
- Cost: a Postgres table + `pg_trgm` + one endpoint + form wiring now; a per-authority adapter set later. A source-survey spike is needed to confirm open ORBAT depth before committing the name seed.

## Rollback

Phase 1 is additive (a table, an endpoint, form fields, the already-landed `unit_code.source` enum) — drop them and onboarding falls back to free-text `unit_code`, unaffected. Phase-2 authoritative ingest is gated behind accreditation and is itself reversible (truncate the `authoritative` rows by `source_id`; entries fall back to their prior provenance). Nothing here is in a wire/ACL/cert enforcement path.
