# ADR 1068 — Anomaly probation: a hold-verify-repair-or-purge lifecycle for things that don't reconcile

**Status:** Accepted — **Phase 1 built in code** — the portal probation list + operator-gated `GET/POST /api/access/probation` + `/{id}/decide` and the `goat access probation` list/hold/decide surface (store + handler + CLI tests). Deploy remains operator-gated. Design: [`anomaly-probation-lifecycle.md`](../design/anomaly-probation-lifecycle.md).
**Related:** [ADR 1026](1026-data-access-common-operating-picture.md) (the COP that surfaces anomalies) · [ADR 1031](1031-credential-revocation-mesh-first-offboarding.md) (the purge path) · [ADR 1061](1061-operator-onboard-intake-system.md) (the repair path) · [ADR 1020](1020-zenoh-federated-peering-trust-model.md) (data-plane quarantine — sibling concept, different layer) · F-207 · F-234.

## Plain-language summary

When the system finds something that doesn't add up — today, a live account nobody wrote a record for ("stray"); later, other odd things — the two obvious answers are both wrong. *Leaving it* means an undeclared live credential lingers (a zero-trust liability). *Deleting it* risks cutting a real partner who was simply half-onboarded. **Probation** is the third answer: hold the thing (don't delete it), record it, and **verify** whether it's real and relevant — then **repair** it into the system if it is, or **remove and cleanse** it if it isn't. A deadline keeps it from sitting in limbo. It's the reversible, graduated posture the platform already uses elsewhere, made a first-class lifecycle for anomalous *entities*.

## Context

The Data-Access COP already *detects* anomalies — the stray class: live identities with an Ory login / mesh peer / cert but no declared manifest. A sweep can surface real partner personnel who were half-onboarded alongside leftover test accounts. What the platform lacks is a governed *disposition*: today each stray is adjudicated by hand every sweep, which does not scale, and both default outcomes (keep / delete) are unsafe. The same hold-verify-decide shape wants to apply to any future "found something weird" — not just identities.

## Decision

**Adopt an anomaly-probation lifecycle: `detected → probation → verify → (repaired | purged)`, with a `cure_by` deadline that flags — never auto-deletes — and the held state carried in a separate probation list, not the declared registry.**

- **Held state = a separate probation list** (portal-owned), one row per entity: `ref`, `kind` (`identity` first; `topic`/`peer`/`inbound` reserved), `reason`, `detected_at`, `held_at`/`held_by`, `cure_by`, `disposition`. It is a read-model + operator workflow, **non-load-bearing** — offline, the four enforcement layers behave unchanged.
- **Repair reuses onboard/intake** (ADR 1061); **purge reuses mesh-first revoke `--purge`** (ADR 1031). Probation adds only the hold + verify + deadline in between — no new enforcement tier.
- **The deadline flags, the operator decides.** Auto-purge on a timer is the hasty-cut failure this exists to prevent.
- **v1 = the identity domain**, operator-initiated entry; `kind` is schema-reserved so other anomaly types reuse the list without a re-model.
- **Attribution via F-207**: each hold/repair/purge verdict emits an `OperatorActionV1`, so the *decisions* are durably audited even though the working list is portal-side.

## Alternatives considered

- **Leave strays as-is / delete on sight.** The two unsafe defaults above — the reason probation exists.
- **A `status: probation` manifest instead of a separate list.** Workable *for identities* (a manifest grants no access — the stray already has its own — so writing one is not itself harmful, contrary to an earlier framing). Rejected on two grounds: the declared registry should hold things *decided to belong*, not undecided maybes distinguished only by a flag; and a manifest describes an identity, whereas probation must hold non-identity anomalies uniformly.
- **Auto-enter + auto-reduce-access from day one.** Deferred to a later phase: auto-quarantining a mis-flagged anomaly and auto-dropping access are the two ways this pattern becomes a footgun; earn them after phase-1 experience.
- **Reuse the data-plane "quarantine" mechanism (ADR 1020).** That is a *namespace* isolation for untrusted inbound data at the fabric layer; probation is an *entity-disposition workflow* at the governance layer. Siblings, composable, not the same thing.

## Consequences

- **+** Undeclared-but-live things stop being a binary keep/delete judgment call; they get a bounded, reversible, audited path. Strays become self-governing as onboarding scales.
- **+** One disposition generalizes across anomaly kinds; the COP gains a probation render and a deadline sweep.
- **−** A new state + list to build and maintain, and a new `goat` surface. Scoped to identity v1 to keep the complexity proportional to the present need.
- **−** Probation is only as good as the *verify* step, which is human — the platform records the verdict + who made it, but cannot decide "real + relevant" for the operator.
- **Non-load-bearing + reversible + no-auto-delete** are invariants: a future phase that adds an access-hold or auto-enter must preserve them.
