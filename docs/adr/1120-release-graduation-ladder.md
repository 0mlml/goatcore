# ADR 1120 — Release graduation ladder: staged promotion across goatnets

**Status:** Accepted (2026-08-09, ops) · **Design:** [release-graduation.md](../design/release-graduation.md) · **Track:** `release-graduation`

## Context

The consistency foundation (ADR 1110) made "is each goatnet running what it is
supposed to?" answerable — typed intent, deploy-ledger convergence records, one
comparison. But *how "what it's supposed to" changes* was undecided: which envs
get a change, in what order, after what proof, captured where. With two live
goatnets (zagros = preprod, zagros_prod = prod) and a dev rung coming, per-deploy
operator judgment does not scale and leaves no reviewable trace. Two failure
classes are already in the record: an out-of-band branch deploying stale state
to a live surface, and cross-env skew with no way to distinguish "a
release in flight" from "drift."

## Decision

1. **The ladder is committed data** — `ops/release/ladder.yaml`: rungs in
   graduation order (dev *planned* → preprod=zagros → prod=zagros_prod), each hop's
   gates, per-component graduation classes, a shrink-only legacy-skew
   allowlist. The dev rung is modeled now so its standup changes a `status:`
   field, not the model.
2. **The unit of graduation is the fleet-pins value.** A release candidate is
   a **declared deviation on the preprod goatnet**; graduation **folds** the
   candidate into `fleet:` and deletes the deviation. No new fact kind, no pin
   schema change; candidacy renders on every existing consistency surface as
   reasoned skew. Build once — the digest that soaked is the digest that
   graduates.
3. **Every promotion writes a record** (`ops/release/records/<component>/…`):
   ref, reason, evidence (validation + observed preprod convergence), state,
   operator ack. Records are governance metadata; the ADR 1060 ledger remains
   the only truth about what ran.
4. **Gates are data, checked by the tool**: candidate-record, soak (min 1
   day), convergence-evidence, validation-evidence (ADR 1115 suite when live),
   operator-ack for the prod hop. Evidence gates check presence; truth is the
   named operator's signature.
5. **`ops/release/graduate.py` guides and observes, deliberately not
   actuates**: `open`/`graduate`/`hotfix` write records and *print* the exact
   pin edit (fleet-pins comments are load-bearing; the edit belongs in the PR
   diff); `status` grades cross-rung state; `check` (in the unfiltered
   fleet-pins CI job) enforces C1–C6 — most importantly: **undeclared
   cross-rung skew fails CI**.
6. **Hotfix is a recorded path, not an exemption**: expedited-to-prod as a
   declared prod deviation + record with named ack, graded
   `hotfix-backfill-owed` until the ref graduates normally. Rollback needs no
   record (ADR 1060; reverting *to* the value of record).
7. **Observation stays a projection** (environment-parity §5): graduation
   status reads committed facts only; convergence truth stays with the
   consistency surfaces. `status --live` (join via env-doors + CF-7 collect)
   is named designed-not-built.

## Alternatives rejected

- **Per-env branches or repos** — violates one-fact-one-home; env skew becomes
  merge state, invisible to the pin table and every consistency surface.
- **Rebuild images per env** — soak evidence would describe a different
  artifact than prod runs; contradicts the digest-pinning discipline.
- **A promotion service/controller** — environment-parity §8 forbids a central
  consistency authority; goatnets must not depend on rear-echelon
  infrastructure to deploy.
- **Tool rewrites fleet-pins.yaml directly** — YAML round-tripping destroys
  the table's load-bearing comments; printed-edit + `check` closes the same
  loop with the diff visible in review.
- **Git tags/releases as the graduation record** — invisible to the CI gates
  that guard the pin table, and not reviewable next to the change.

## Consequences

- Cross-env deployment order and proof become reviewable artifacts; "why is
  preprod ahead" always has a one-file answer.
- One more fast unfiltered CI step; PR authors bumping prod pins must carry a
  record (the tool writes it in one command).
- Until dev exists, preprod absorbs validation — declared in the ladder.
- First real graduation will exercise the printed-edit workflow; §9 of the
  design doc names the trigger for adopting comment-preserving YAML editing
  instead.
