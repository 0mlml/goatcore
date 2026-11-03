# ADR 1103 — Data provenance grammar + ephemeral-by-default fabric hygiene

**Status:** Proposed (2026-07-29, design); no code yet.
**Repo scope:** goat core (`panoscope-core` resolver + surfaces, `goat-zenoh-portal` registry, publishing libs, injector runbooks)
**Related:** `data-provenance-and-fabric-hygiene.md` (the design) · ADR 1043 (identity grammar — the orthogonal-axis precedent this mirrors) · [ADR 1041](1041-zenoh-router-zid-pinning-and-source-provenance.md) (SourceInfo — the carrier) · [ADR 1032](1032-zenoh-volatile-non-persisted-namespace-class.md) (`volatile/` — the ephemeral default) · [ADR 1074](1074-data-access-role-architecture.md) (data-access labels — kept orthogonal) · [ADR 1051](1051-identity-descriptor-standard.md) (born-as/currently-is registry model) · [`worktree-hygiene.md`](https://github.com/usareur-af/goatdocs/blob/main/docs/design/worktree-hygiene.md) (the reaper pattern) · `panoscope.md` (SOURCE/INFRA filters generalized)

## Context

The fabric has no vocabulary for *what a datum is* beyond a single `synthetic` boolean, and no convention obliging a data injector to clean up after itself. Both gaps show together when a high-cardinality stream and a load-test run share a fabric. A load-test slot that publishes test topics tagged `synthetic = None` / `node_class = app` renders as **real operational data**, and `SOURCE: REAL` cannot filter it. Untagged defaults to "trusted operational," which is backwards. A rig that finishes can leave publishers live, liveliness declared, storage populated, and its slot registered — no teardown runs, because none is required. Downstream viz folds (panoscope viz-budget fold) manage symptoms; the fabric needs a *provenance vocabulary* + a *cleanup principle* so surfaces can separate mission data from noise and transient runs don't accrete.

## Decision

Adopt a **closed, orthogonal-axis provenance grammar** — the data-side analog of the identity grammar (ADR 1043) — plus an **ephemeral-by-default fabric-hygiene principle.**

**Axes** (a datum carries one value per axis; axes are independent):
- **Purpose** — `operational` · `exercise` · `experiment` · `test` · `demo`
- **Fidelity** — `measured` · `simulated` · `notional`
- **Temporal** — `live` · `replay` · `forecast`
- **Substrate** — `mission` · `infrastructure` · `telemetry`

Orthogonality is the point: an `exercise` carries both `simulated` and `measured` data; a load test is `test`+`notional`. Provenance stays **orthogonal to data-access** (ADR 1074) — *what a datum is* vs *who may read it*.

**Carrier:** the tag rides in `SourceInfo`/attachment (`ProvenanceV1`), with a declared per-publisher/per-namespace default in the registry (born-as/currently-is, ADR 1051). **Never in the topic key** (same rule the identity grammar enforces for affiliation). Resolution: per-sample → registry → **fail-closed default**.

**Fail-closed default:** untagged ⇒ `unclassified`, rendered distinctly and **never assumed operational-measured**. `synthetic == true` ⇒ `notional`; the boolean becomes a derived shim.

**Fabric hygiene — ephemeral-by-default:** *if you inject data, you own its teardown, unless leaving it behind is the stated, tagged purpose.* Transient injectors publish `volatile/` (routed-never-stored, ADR 1032) so nothing persists by construction; **retention is a deliberate act** — stored class + `Purpose=test|experiment`+`retain`(owner/reason) + non-operational namespace, so it is retained *but filtered out of the operational COP by default*. Enforced by a declared lease (`run_id`+owner+TTL), a `fabric-reap` dry-run sweep (worktree-reaper pattern), and a **required** `--destroy`/teardown step in every injector runbook. Composes with "present-but-collapsed for real data": real data is managed in place, test data cleans up after itself.

**Test-data lifecycle convention — intent is in the publish.** The author chooses lifecycle by *how they publish*, no out-of-band config: two orthogonal knobs in the key — **persistence** (`volatile/…` = ephemeral/auto-discarded vs stored = retained, ADR 1032) and **classification** (a `loadtest`/`test` path **segment** = test data → surfaces exclude it from operational views, expose it in a dedicated test-only view). So `volatile/…/loadtest/…` = ephemeral test (never stored, auto-gone), `…/loadtest/…` = retained+visualizable test. The classifier matches the marker as a **segment** (catching `<uuid>/loadtest-wedge/<n>` where a prefix never could). An **operator override** (`PANOSCOPE_TOPIC_BLOCKLIST` segment-match / vendor blocklist) can hard-drop a marker at ingest regardless of what the author declared. **Prepositioned in the harness** so it isn't left to memory: a `data_lifecycle` param on the load/soak rig (default `ephemeral`) prefixes `volatile/` + carries the marker — reference impl in `ops/zenoh/wedge-loadtest/`.

## Consequences

- Surfaces gain honest facets: panoscope's `SOURCE`/`INFRA` filters generalize to per-axis facets (`unclassified` shown-but-flagged, never hidden nor trusted); audit stamps the tuple so "operational or exercise?" is answerable after the fact; QoS may deprioritize `test` traffic.
- The loadtest class of bug is structurally closed: an untagged injector reads as `unclassified`, not operational, and the hygiene principle makes its cleanup a required step, not a courtesy.
- Migration is honest, not a mass-default: `synthetic==true`⇒`notional`; everything else untagged ⇒ `unclassified` (surface the backlog; do NOT default legacy data to `operational`).
- Not router-enforced — brokerless root ([zenoh-brokerless-consequences.md](../design/zenoh-brokerless-consequences.md)) makes this reveal-and-filter at the edges; a publisher can omit/lie, and the fail-closed default + audit make that *visible*, not *prevented*.
- Cost: a new CI-fenced vocab SoT, a `ProvenanceV1` block on the wire, a resolver in `panoscope-core`, and injector-runbook changes. Staged; the `synthetic` shim keeps everything working during migration.

## Alternatives considered (rejected)

- **Extend the `synthetic` boolean / add more booleans.** A flat bit-set can't express orthogonal axes (the exercise-with-real-and-sim case needs a tuple), and boolean sprawl has no closed grammar or fail-closed semantics. Rejected — it's the v0 that just failed.
- **Encode provenance in the topic key** (`test/…`, `sim/…`). Fragments the namespace, breaks subscriptions, and lies when one peer emits mixed streams on structurally identical keys. Violates the identity-grammar anchor rule. Rejected.
- **Fold provenance into the data-access label** (ADR 1074). Conflates *what-it-is* with *who-may-read*; a `simulated` datum can still be `RESTRICTED`. Keep them orthogonal. Rejected.
- **Router-enforced tagging.** Re-introduces the rejected broker (central gate) into a deliberately brokerless fabric. Rejected — reveal-and-filter at the edges instead.
- **Default untagged to `operational`** (convenience). This *is* the bug. Rejected — fail-closed to `unclassified`.
- **Cleanup as advisory only** (no lease/reaper/required teardown). Advisory hygiene is not honored under time pressure. Rejected — make teardown a required, verified, and reap-backstopped step.
