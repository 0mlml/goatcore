# ADR 0503 — DDS bridge for legacy / partner systems (Phase 3+)

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Related | [`0500-zenoh-data-fabric.md`](0500-zenoh-data-fabric.md), [`0209-shim-pattern.md`](0209-shim-pattern.md), `../design/zenoh-data-fabric.md` |

## TL;DR (for a layperson)

Many existing tactical data systems use a real-time data-
distribution protocol called DDS (Data Distribution Service).
For us to integrate with those systems without forcing them to
change, we build a bridge between our Zenoh fabric and DDS.
The bridge is a Phase 3+ deliverable; it is not in the Phase 2
pilot.

## Context

DDS (OMG specification) is the dominant pub/sub fabric in
real-time tactical and aerospace systems. Many partners and
many existing programs use DDS. We do not adopt DDS as our
internal substrate (Zenoh is the internal substrate per
[`0500`](0500-zenoh-data-fabric.md)) but we must federate
with DDS-speaking systems.

The fix is a bridge — a single component that maps Zenoh
key-expression-addressed pub/sub to DDS topic-based pub/sub
and back. The bridge follows the shim pattern
([`0209`](0209-shim-pattern.md)): one component per protocol
class, stateless, phoenix-native.

## Decision

Phase 3+ deliverable: DDS bridge.

- **Direction:** bidirectional Zenoh ↔ DDS.
- **Mapping:** Zenoh key prefix ↔ DDS topic name (configurable
  per integration).
- **Implementation:** likely RTI `zenoh-dds-bridge` upstream
  fork, evaluated during Phase 3 implementation; alternative
  is custom Rust.
- **Boundary deployment:** runs on the boundary node
  ([`0601`](0601-fips-pqc-boundary-node.md)) for partners
  outside the authorization boundary; runs adjacent to the
  Zenoh router for inside-boundary integrations.
- **Schema:** DDS IDL types map to Zenoh-side schemas via the
  schema-evolution mechanism ([`0506`](0506-fabric-schema-evolution.md)).

This is **not** in the Phase 2D pilot. Phase 2D establishes
Zenoh as the substrate first; the bridge work happens after
the gate ([`0509`](0509-phase-2-gate-criteria.md)) closes.

## Alternatives considered

- **Adopt DDS internally.** Heavier; less interception-
  friendly; does not extend to non-DDS partners. Rejected as
  internal substrate.
- **Per-DDS-system custom adapter.** Reinvents the bridge per
  partner. Rejected.
- **No DDS support.** Loses meaningful federation surface.
  Rejected.

## Consequences

**Positive.** Existing DDS-speaking partners federate without
changing their side. The bridge isolates DDS-side cruft from
the Zenoh substrate. Schema evolution at the bridge.

**Negative.** Build cost in Phase 3+. We accept; partner
integration value is high.

## Assessor view (SCA-R)

The DDS bridge serves SC-7 (boundary protection — DDS
federation crosses the authorization boundary), AC-4
(information flow enforcement), AU-2 (per-bridge audit), and
SI-7 (integrity). Where the bridge crosses the authorization
boundary, it sits on the FIPS-PQC boundary node
([`0601`](0601-fips-pqc-boundary-node.md)).

## Trigger conditions for revisit

- A specific DDS partner integration rejects the bridge
  pattern (e.g. demands native DDS internally). Negotiate;
  do not adopt DDS internally.
- Zenoh upstream provides a stable DDS bridge. Adopt it
  rather than building custom.

---

*See `GLOSSARY.md` for terms and acronyms.*
