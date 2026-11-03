# ADR 1048 — Email-keyed idempotent onboarding: make the mint path converge instead of double-issue

**Status:** Proposed — surfaced by F-239 (concurrent-operator collision). Design + remediation runbook in [`../design/operator-onboarding-collision-safety.md`](../design/operator-onboarding-collision-safety.md). Not yet built.
**Repo scope:** goat (`ops/ansible/operator-onboard.yml` Phase 1 NetBird + portal slot mint; `docs/operators/roster.*` registry; `ops/work` claim gate)
**Related:** F-239 (the finding) · [ADR 0006](0006-multi-agent-human-concurrency-governance.md) (the advisory work-bus this gates) · F-207 / [`../design/operator-action-audit.md`](../design/operator-action-audit.md) (sibling "operator tier mutates shared infra" theme) · [`../design/data-access-cop.md`](../design/data-access-cop.md) (the declared issuance registry) · [ADR 1031](1031-credential-revocation-mesh-first-offboarding.md) (mesh-first cut, reused by remediation)


> **Landscape note (2026-07-09, on merge):** written 2026-06-16 against the ansible-only mint path. Since then the onboarding engine was extracted into the identity plane (goat-id-console owns the store + handlers, #1472/#1477/#1480) and runner-minted manifests now sync into the declared roster (#1473 — the split-brain fix, a first step toward layer 3's mint-consulted registry). The problem statement and the email-keyed decision are unchanged; the pre-mint reuse-or-refuse check now lands in the extracted engine (console/runner) *and* the ansible operator-tier path, not ansible alone. Renumbered F-216→F-239 (ID taken concurrently by the undeclared-topics monitor finding).

## Plain-language summary

Two operators onboarding the same person to the same goatnet today produces duplicate NetBird keys, duplicate Zenoh slots, and — if they pick different handles — two whole identities, with no warning. The fix: the onboard must **check live shared state, keyed on the person's email, before it mints**, and reuse-or-refuse instead of blindly creating. Email is the spine because it is the only identity stable across operators (Ory already dedups on it; NetBird and the portal key on the operator-chosen, divergent handle).

## Context

`operator-onboard.yml` mutates three shared planes per run. Two of them are non-idempotent: NetBird Phase 1.4 unconditionally `POST`s a setup-key, and the portal allocates a fresh slot. Only Ory dedups, and only by email. Handles are free text and diverge between operators (`alice-mainline` vs `alice-smith`), so a handle-keyed audit on one laptop can't even see the other operator's artifacts. Manifests are gitignored per-laptop; `roster.*` is a committed index but is regenerated locally and gates nothing. See F-239 for the full mechanism.

## Decision

Make onboarding **idempotent against live shared state, keyed on email/person**, in four layers:

1. **Email/person is the dedup key everywhere.** Carry an email-derived stable label into the NetBird setup-key name/metadata and the portal slot record so all three planes dedup on the same key Ory already uses.
2. **Pre-mint reuse-or-refuse.** Before minting, GET existing NetBird keys + portal slots for this email. Reuse when the same operator still holds the secret; otherwise **refuse with a message naming who already issued it** (NetBird secrets are masked post-creation, so cross-operator silent re-mint is impossible by design — refuse + hand off).
3. **`roster.*` becomes the committed, mint-consulted issuance registry** (email → handle → artifacts), read before minting and appended after — not a local regenerate.
4. **Enforced `work pickup <env>-onboarding` claim** — the onboard refuses to mint while another operator holds the goatnet's onboarding claim.

Layers 1–2 are the core (they alone prevent the F-239 collision); 3 gives cross-operator visibility; 4 is the human gate.

## Alternatives considered

- **Keep advisory coordination only (status quo).** Rejected — an advisory bus is easy to bypass; advisory ≠ protection for a path that mutates shared infra.
- **Dedup on handle / enforce unique handles.** Rejected — handles are operator-chosen and the divergence *is* the failure mode; a unique-handle rule doesn't stop two operators picking two handles for one human.
- **Lock onboarding to a single operator/host.** Rejected — partner onboarding is deliberately multi-operator; the fix is convergence, not serialization. (The enforced claim in layer 4 serializes only the brief mint window, not the role.)
- **Central onboarding service/broker.** Rejected for now — reintroduces a central component; the email-keyed idempotent check achieves convergence without one. Revisit only if pre-mint checks prove insufficient.

## Consequences

- **Convergence, not duplication.** Re-runs and concurrent runs resolve to one identity per person; the F-239 collision class closes.
- **Refusals replace silent dupes.** Operators see "already issued by X" instead of a silent second key — a behavior change that needs a clear message and an override for the legitimate re-issue case.
- **Roster gains weight.** Promoting `roster.*` to a gating registry means it must stay correct (CI sync-check) and sanitized; a stale registry would wrongly refuse.
- **Remediation stays manual + email-keyed** until built (the runbook); the tooling change is forward-looking, existing collisions are reconciled by hand.
