# ADR 1061 — Operator onboarding intake system (webform · linter-as-a-service · admin approval queue)

**Status:** Proposed 2026-06-24.
**Anchor design:** [`docs/design/operator-onboard-intake.md`](../design/operator-onboard-intake.md).
**Builds on:** ADR 1043 — identity grammar (the form's vocabulary) · the manifest schema + `validate.py` linter · the `goat admin onboard` orchestrator · [ADR 1052 — vendor→org machine rename](1052-vendor-to-org-machine-rename.md) (noun discipline) · [F-207 / operator-action-audit](../design/operator-action-audit.md) (sponsor + approval attribution).

## Context

Onboarding a participant starts from a hand-authored YAML manifest (`ops/operators/<env>/<handle>.yml`). The manifest schema is rich and correct, but only *machine*-readable: the human copies a sibling manifest, edits by guesswork, and learns it is wrong only when `goat admin onboard` rejects it — one error per re-run.

A manifest copied from an older template fails validation as soon as a gate begins to *require* a new block, such as the affiliation block after the grammar cutover. The information needed to author it correctly was already in the schema; nothing surfaced it to the operator. Onboarding also has no concept of a **sponsor** (who vouched for this person) and no way for a non-operator to *propose* an onboarding without holding mint credentials.

## Decision

Build an **onboarding intake system** hosted in the goatnet portal, with five parts:

1. **Schema-generated intake webform.** The form is rendered from the JSON Schema ⊕ a thin UI-hints sidecar — every field shows its type (enum dropdown / free text), required-vs-optional, and a plain-English description. It cannot drift from the schema because it is generated from it. Vocabulary and enums are the ADR 1043 grammar dimensions (role · affiliation · echelon · node-class · site), with tier presets (dev / browse / operator) for the common cases. **Future-grammar-native, not the legacy vendor shape.**

2. **Sponsor = the authenticated submitter, bound server-side.** The intake handler stamps `sponsor = actorOf(r)` (the per-person principal, F-207) onto the proposal; it is not a form field and cannot be forged client-side.

3. **Linter-as-a-service — Go-native against the shared schema, CI-proven equal to the onboard gate.** `POST /api/onboard/lint` (mirroring the `peering/lint` workbench) validates in Go because the portal runtime is a Chainguard **distroless** single-binary image (ADR 0700) with no Python/shell — baking a Python runtime in to run `validate.py` would multiply the very attack surface distroless removes. The JSON Schema (`operator-manifest.schema.json`) is the bulk of validation and is consumed **unchanged** by a Go Draft-7 validator (one shared file = no drift on the structural rules); only the two grammar-regression rules + the display `classify_shape` are hand-ported (~40 lines). Drift is held off by **two fail-closed CI guards**: a byte-identical schema-sync check on the portal's embedded copy, and a **parity test** that runs the Go linter and `validate.py --json` over a fixture corpus and asserts identical verdicts. `validate.py` keeps its additive `--json` mode (Phase A) as the canonical CLI gate and the parity reference. Net: the webform, batch upload, and Phase-0 gate run verifiably-equal validation.

4. **Proposals are inert; an operator approves to run.** Submissions persist as `proposed` records (no credential minted). An operator-gated admin view reviews the queue and approves; approve re-lints, writes the validated `<handle>.yml`, and runs `goat admin onboard`. **v1 is semi-automated** — the portal emits the manifest and the exact onboard command for the operator to run; the orchestrator's credentials (NetBird token, SSH to mgmt, portal operator key) stay with the operator, not the portal.

5. **Batch upload.** `POST /api/onboard/intake/batch` lints each record of an uploaded file independently; valid records become proposals, invalid records report per-record findings (same posture as topic bulk-import).

## Alternatives considered

- **Shell the portal out to `validate.py`.** Initially chosen, then rejected once the runtime was checked: the portal is a Chainguard distroless single-binary image with no Python/shell, and adding them to run a linter multiplies the attack surface distroless exists to remove. Go-native against the *shared* schema file (no structural drift) + the two CI parity/sync guards gets the single-source-of-truth guarantee without the Python runtime.
- **Reimplement validation freehand in Go (no shared schema, no parity gate).** *That* is the drift trap — the form could go green on a manifest the onboard rejects. Avoided by consuming the same schema JSON unchanged and CI-asserting verdict-parity with `validate.py`, so the duplication is provably equal, not hopefully equal.
- **Fully portal-triggered onboarding in v1** (portal holds the NetBird/SSH/operator credentials and runs ansible itself). Materially larger credential surface; deferred to a fast-follow phase behind its own security review. v1 keeps mint credentials with the operator.
- **Open (anonymous) submission.** A proposal carries pre-mint identity data and must be attributable to a sponsor. Submission is authenticated; only approval mints. Rejected.
- **A standalone intake service.** Would duplicate the portal's auth, admin frontend, registry persistence, and lint-workbench precedent. Rejected — home is the portal.
- **Hand-maintained form (not schema-generated).** Drifts from the schema the moment either changes — the same machine-only-readability problem one layer up. Rejected.

## Consequences

**Positive:**
- The schema becomes self-serve: no more one-error-at-a-time guesswork; the affiliation-block class of failure is caught in the form, before any mint attempt.
- Sponsorship is first-class and attributable; non-operators can propose without holding mint credentials.
- One linter, provably shared with the onboard gate.
- Batch onboarding becomes a file drop with per-record validation.
- Grammar-native intake nudges the whole onboarding surface toward the ADR 1043 model and away from the legacy vendor shape.

**Negative / cost:**
- A Go port of the two grammar rules + `classify_shape`, kept honest by the schema-sync + parity CI guards (the structural schema itself is shared unchanged, so the duplicated surface is small).
- New persisted state (the proposal queue) and a new admin page to maintain.
- v1's semi-automation still requires an operator to run the emitted command — the approval button does not itself mint (deliberate, until the credential-surface review for full automation).

**Neutral:**
- No new secret crosses the intake surface; manifests stay gitignored identity material written to the existing `ops/operators/<env>/` location on approve.
- The approve action is an operator action and emits an `OperatorActionV1` audit event.

## Build phases

A: `validate.py --json` + this ADR/design (current). B: `/api/onboard/lint`. C: schema field-metadata + generated single-record form. D: proposal store + `/api/onboard/intake` (sponsor binding). E: admin approval view + approve→emit. F: batch upload. Detail in the design doc.
