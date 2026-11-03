# ADR 1090 — microscope: an admin-gated per-entity behavior lens (RCA + watch)

**Status:** Proposed 2026-07-17 — Phase A (design + plan); no code yet.
**Repo scope:** goat core (new service `ops/microscope/` in later phases; `goat-cli` lens; Ansible role; this phase is docs-only)
**Related:** [ADR 1047](1047-goat-who-identity-cop.md) (the roster COP microscope drills down from) · [ADR 1065](1065-graph-engine-property-graph-read-model.md) (read-only projection doctrine) · [ADR 1073](1073-rbac-adherence-principle.md) (matrix-enforced gating) · [ADR 1022](1022-zenoh-denial-observability.md) (denial observability gate) · [ADR 1089](1089-probation-isolation-lifecycle.md) + [ADR 1068](1068-anomaly-probation-lifecycle.md) + [ADR 1031](1031-credential-revocation-mesh-first-offboarding.md) (the watch-instead-of-expel levers microscope observes for; ADR 1088's transfer engine moves entities into/out of them) · [ADR 1019](1019-goat-cli-unified-operator-binary.md) (CLI delegation guardrail) · `microscope.md` (the design)

## Plain-language summary

microscope answers, for identity admins: *"what is this specific entity doing — across every
layer, in as much detail as we hold — and does that match what they may do?"* One entity per
view: capability pane (what they may do, per layer), merged timeline (what they did — sessions,
admin actions, mesh presence, fabric writes/reads), and a denial pane that either shows a
recorded refusal or states honestly that the layer refuses silently. Three missions: RCA on
"I should be able to but can't," standing watch on a concern, and full-detail observation of a
known bad actor deliberately retained instead of expelled. Standalone admin-fenced web service
(stethoscope deployment template) + a `goat microscope` CLI lens rendering the same view-model.

## Context

- An active admin can look un-onboarded because the evidence of their activity lives in three
  stores on two hosts with
  no composing surface. `goat who`'s roadmap covers parts (Ory last-login, operator-action-audit
  fold) but not the console web-audit stream, and a roster row cannot carry a timeline.
- The activity evidence already exists per layer (console `audit.jsonl`, operator-action audit,
  Zenoh ops/** audit tiers, Kratos/Hydra sessions, NetBird `last_seen`, pub/sub census) — this is
  composition, not new capture. Denial evidence is the partial exception (F-177 silent denial;
  ADR 1022 gates write enforcement on making it observable).
- Anomaly probation (ADR 1068) and reversible revocation (ADR 1031) already commit us to
  watch-instead-of-expel outcomes; nothing today gives the watcher a working view.

## Decision

1. **A per-entity lens, not another roster.** `goat who` stays the picker; microscope is the
   drill-down: one entity, all layers, timeline + capability + denials.
2. **Read-only, reveal-only, rebuildable** (ADR 1065 doctrine). Derived read-model over the
   per-layer stores; registry/manifests stay SoT; if microscope dies nothing about enforcement or
   the data path changes. No central audit funnel (brokerless consequences hold).
3. **Compose, don't re-capture.** v1 = source adapters over existing stores, stethoscope-plugin
   idiom, each degrading to `skipped (unavailable)`. Capture gaps are named in the design doc and
   built in their OWNING layer, never as microscope-side collection.
4. **Identity-admin gate, orthogonal to data access.** `MICROSCOPE_RBAC_WHO_GROUPS` (default
   `ADMINS,PLATFORM-ADMIN,IAM-ADMIN`), oauth2-proxy admin fence, `X-Forwarded-Groups` only
   , default-off enforce flag, matrix parity ratchet (ADR 1073). `data.view.all` does not
   admit it.
5. **Watching is audited at the F-207 chokepoint.** Every view/export emits an operator-action
   event (who examined whom, which lens). Missions 2–3 are acceptable only with this counterweight.
6. **CLI twin under the ADR 1019 guardrail.** `goat microscope <selector>` renders the service's
   JSON/SSE — one view-model, TUI + `--once` plain-text renderers; never re-computes.
7. **Honesty discipline inherited unchanged** (`goat top` lineage): no false green, absence of a
   denial record is never evidence of success, convention-only spine joins are tagged as such.

## Alternatives considered

- **Extend `goat who`'s detail pane instead.** Rejected: a roster row cannot carry a multi-stream
  timeline; who is portal-key-gated CLI-first, while missions 2–3 need a fenced web surface with
  watch-audit; and who's charter is breadth, not depth.
- **A new panoscope page.** Rejected: panoscope's spine is the live fabric; most microscope sources
  (Kratos, console audit, operator audit, NetBird) are off-fabric reads. Deployment isolation
  (own image/ledger/rollback) fits the standalone template.
- **Central audit aggregation store.** Rejected: re-introduces the funnel the brokerless root
  rejects; retention stays per-store, microscope reads in place.

## Consequences

- Phase-gated build (design → spine+adapters+CLI → web+fence+deep-links → denial adapters →
  watch mode); each later phase carries its own live-fire gate.
- The design doc's §Sources becomes the maintained census of actor-attributable streams; its
  §Gaps list drives per-layer observability builds (console-audit fold, Ory last-login surfacing,
  denial records) that benefit `goat who` too.
- One more standalone service per goatnet (image, role, ledger, isitup row) — accepted against the
  complexity budget because the alternative is the status quo: multi-host manual forensics under
  time pressure.
