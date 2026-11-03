# ADR 1098 — PLATFORM decomposition: SPIRE-direct workload SVIDs, portal records the composition

| Field | Value |
|---|---|
| Status | Proposed (2026-07-28); **portal/registry/CLI recording + surfacing built; the SPIRE-entry generator + `spire`-role fold-in built** (`ops/identity/gen-platform-spire-entries.py`). Only the deploy-time `selector` fill + the live `entry create` remain — the gated deploy step every Ansible change shares |
| Date | 2026-07-28 |
| Related | machine-enrollment.md (stage 4), [ADR 1096](1096-delegated-machine-enrollment.md) (decision #3), ADR 0412, identity-grammar.md §1.4, ADR 1043 |

## Context

ADR 1096 decision #3 fixed that SPIFFE decomposes only PLATFORMs. Stage 4 of
machine enrollment had to decide the one thing that decision left open: **where a
PLATFORM's sub-workload SVIDs get minted**. A `PLATFORM` (a UAS is sensor *and*
effector *and* processor) is modeled as a **composition** of function-workloads,
each separately SPIFFE-identified (identity-grammar.md §1.4,
ADR 0412). SPIRE Phase A is live on ZAGROS and
already composes with the CN-only Zenoh ACL by CN — the router concatenates the
SPIRE bundle into its trust roots and admits an SVID exactly like a portal-minted
leaf.

## Decision

**Option B — SPIRE-direct workload SVIDs; the portal records only the composition.**

1. **The PLATFORM enrolls as a normal machine** — its own slot / cert / custody
   edge via the ADR 1096 flow, unchanged.
2. **Its declared workload composition** (a list of function classes:
   `SENSOR`/`EFFECTOR`/`PROCESSOR`) is recorded on the machine in the portal
   registry — the born-as declaration (`slots.workloads`, comma-joined). Declared
   at enroll (`goat machine enroll … --workloads sensor,effector`), validated:
   allowed only on a `PLATFORM` node-class, each entry a function-workload class.
3. **The actual workload SVIDs are registered through the existing `spire` Ansible
   role**, composing with the fabric by CN as SPIRE Phase A already does. The portal
   **never proxies SPIRE registration**; the declaration it stores is the input that
   generation reads.
4. **A PLATFORM is 1 quota unit.** Its workloads are internal composition, not
   separate machine enrollments — one physical asset, one slot, one quota draw.
5. **Workload identity model.** A workload SVID's **CN is the platform's slot
   anchor** (its cert CN / Zenoh ACL subject), so the workload composes under the
   platform's write-namespace exactly as the platform does — "compose by CN".
   Workloads are therefore NOT separately wire-distinguishable; they share the
   platform's namespace. Each workload's **`spiffe_id` = `<platform-handle>/<function>`**
   (distinct per function) is the SPIFFE attribution that separates sensor from
   effector (attestation + provenance), not a wire-ACL axis.
6. **The `selector` is deploy-time.** A SPIRE entry needs a selector (a docker
   label) matching the workload's runtime sidecar, which only exists once the
   platform's containers are defined. `ops/identity/gen-platform-spire-entries.py`
   turns the portal's recorded declarations into entry definitions (spiffe_id + CN
   derivable now); it emits the selector null (`NEEDS_DEPLOY_SELECTOR`). The `spire`
   role registers only selector-filled entries and **warns** (never silently drops)
   about the rest.

## Alternatives

- **Option A — portal drives SPIRE's registration API** (one command mints the
  PLATFORM *and* its N workload SVIDs). Rejected: it couples the portal to SPIRE,
  makes the portal a registration proxy for a job SPIRE already owns, and
  duplicates SPIRE as the workload-identity authority — two systems minting the
  same class of identity. Option B keeps a single authority per identity class
  (portal = machine identity + custody; SPIRE = workload SVIDs).
- **Per-workload quota** (a PLATFORM costs N units). Rejected: a platform is one
  physical asset the custodian brought online; charging its internal decomposition
  against quota conflates the asset with its parts.
- **Workloads registered fully out-of-band** (no portal record). Rejected: without
  the declared composition on the machine, nothing ties the workload SVIDs back to
  the platform's custody edge, and no surface can show "this platform decomposes
  into …".

## Consequences

The portal/registry/CLI **record and surface** the PLATFORM composition (built:
`slots.workloads`, `RecordMachineEnrollment` validation, `goat machine enroll
--workloads`, `goat machine ls`/`GET /api/machines` rendering). SPIRE stays the
single source of truth for the actual workload SVIDs. The generator
(`gen-platform-spire-entries.py`) + the `spire`-role fold-in
(`spire_platform_workload_entries`, selector-filled entries only) turn the recorded
declaration into SPIRE registration entries; only the deploy-time `selector` fill +
the live `entry create` remain (the gated deploy step every Ansible change shares).
The custody edge stays at the platform; sub-workloads inherit it as its composition.
No new identity plane, no portal↔SPIRE coupling.
