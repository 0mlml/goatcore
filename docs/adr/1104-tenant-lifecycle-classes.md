# ADR 1104 — Tenant lifecycle class: the slot-plane binding of the provenance grammar, with per-class retention

**Status:** Accepted (2026-08-01) — first enforcement hooks building on the sandbox goatnet; composes with ADR 1103 (Proposed) without depending on its wire carrier.
**Related:** [ADR 1103](1103-data-provenance-grammar-and-fabric-hygiene.md) (provenance grammar + ephemeral-by-default hygiene — the vocabulary this binds; per-datum carrier, designed-not-built) · [ADR 1025](1025-zenoh-dynamic-tenant-authz-slot-pool.md) (slot pool) · ADR 1043/[1051](1051-identity-descriptor-standard.md) (mutable registry attributes; never a cert stamp) · [ADR 1101](1101-zenoh-lazy-per-slot-storage.md) (per-slot storage — the TTL render point) · `zenoh-cardinality-budget-and-lazy-crawler.md` (topic budget — the notifier + census surfaces this labels)

## Context

The first live grant-relative topic-budget cycle resurfaced, at the
*tenant* plane, the gap ADR 1103 diagnosed at the *data* plane: test
identities, curated demonstrations, and real partner tenants are
indistinguishable to the registry, the storage tier, and the budget/census
surfaces. Concretely:

- A tenant's **flood-test debris** (keys such as `sensor/flood` and
  timestamp-minted `sensor/precut/<ts>`) sits in storage indefinitely. Nobody returns to clean test-flow outputs; default retention
  treats junk and mission data identically. ADR 1103's hygiene principle
  (ephemeral-by-default, lease + reap) governs *future, well-behaved*
  injectors — it has no lever over data already stored by tenants who never
  declared anything.
- The **synthetic fleet** and demonstration identities blend into the same slot
  pool as partner tenants, muddying every census, COP, and budget view.
- The budget remediation needs a home for **kept exemplars** — "here is the
  same data shaped correctly" — that notices can link to and that never ages
  out or reads as a real tenant.

ADR 1103 defines the vocabulary (Purpose · Fidelity · … axes) and the
per-datum carrier (`ProvenanceV1` in SourceInfo, registry default,
fail-closed `unclassified`), but is not yet built, and several enforcement
points cannot act on per-datum tags at all: storage TTL is rendered
**pre-cut per slot**, and courtesy notifications address a
*tenant*, not a sample.

## Decision

1. **A mutable registry attribute `lifecycle_class` on the slot record**
   (portal `slots` table), values **`test | notional | real`** — defined as
   the coarse slot-plane projection of the ADR 1103 grammar, not a new
   vocabulary:
   - `test` ⇔ the tenant's declared-default Purpose is `test`/`experiment`
     (transient rigs, load/flood identities);
   - `notional` ⇔ declared-default Purpose `demo` and/or Fidelity
     `simulated`/`notional` **with retention as the stated purpose** (the
     synthetic fleet; curated best-practice exemplars);
   - `real` ⇔ declared-default `operational` (partners, cohort humans).
   When ADR 1103's registry defaults land, `lifecycle_class` becomes (and is
   CI-fenced as) derivable from them — one grammar, two altitudes.
   Per ADR 1043/1051 it is registry **currently-is** state: never a cert
   stamp, never in the write anchor; reclassification is an UPDATE, not a
   re-mint.
2. **Fail-safe default:** absent/NULL ⇒ treated as `real` — the class that
   deletes nothing and notifies normally. (This inverts ADR 1103's
   fail-closed-to-`unclassified` deliberately: at the data plane the safe
   failure is *distrust*; at the retention plane the safe failure is
   *don't destroy*.)
3. **Per-class enforcement hooks** (each reads the class from the registry
   projection; none inspects certs or per-datum tags):
   - **Storage retention:** the router's per-slot storage render gains
     per-class `time_to_live` — `test` short (default 72 h), `notional` and
     `real` the site default. Pre-cut like everything else; takes effect at
     the F-156 render + restart.
   - **Budget/census labeling:** the budget projection carries the class per
     owner; the Prometheus bridge emits it as a label. Alerts keep firing for
     every class (a test identity over its allowance is still sprawl) but the
     annotation names the class, and dashboards can facet test noise away —
     the tenant-plane analog of panoscope's ADR 1103 facets.
   - **Notifier tone:** the budget notifier (design §"Budget notifications"
     in the cardinality doc) emails `real` tenants; `test`/`notional` events
     route to the operator bus only.
   - **Reaper eligibility (named follow-up):** `test` slots idle past N days
     become candidates for revoke + storage drop — operator-fired
     reconciliation, the ADR 1103 `fabric-reap` pattern applied to slots.
4. **Backfill:** existing sandbox slots classified in one operator pass —
   synthetic fleet + exemplar slots → `notional`, load/flood identities →
   `test`, partners + cohort humans → `real` (the default, so only the
   exceptions need rows).

## Alternatives considered (brief)

- **Wait for ADR 1103 and use per-datum tags alone.** Retention is rendered
  per-slot pre-cut and notifications address tenants — per-datum tags cannot
  reach either; and 1103 has no lever over already-stored untagged debris.
  Rejected as the sole mechanism; 1104 binds to 1103's vocabulary instead.
- **A fresh tenant vocabulary disjoint from 1103.** Two overlapping
  provenance grammars is exactly the drift ADR 1043 exists to prevent.
  Rejected — 1104's classes are defined as projections of 1103's axes.
- **Class in the cert (URI-SAN/CN).** Violates ADR 1051 born-as/currently-is;
  reclassification would force a re-mint. Rejected.
- **A separate test goatnet.** The sandbox goatnet *is* the sandbox; the needed distinction
  is intra-sandbox lifecycle, not another environment. Rejected.

## Consequences

- Test flows stop accumulating: their stored outputs age out in days with no
  one remembering to clean them; the census/COP can facet them out today,
  before the full 1103 build lands.
- The notional class gives remediation demonstrations and future exemplars a
  first-class, kept home that notices link to.
- One more onboarding attribute, defaulted to `real` so the simple path stays
  simple; one column on the slot record; consumers treat unknown values as
  `real`.
- A CI fence keeps the class ↔ grammar mapping in lock-step once ADR 1103's
  registry defaults exist (drift between the two would resurrect the split
  this ADR exists to prevent).
