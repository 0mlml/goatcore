# Operator onboarding collision-safety

How we stop two operators silently double-issuing identities for the same person on a shared goatnet, and how we safely reconcile a collision that already happened.

- **Finding:** F-239 — the mint path is non-idempotent and dedups only by Ory email, never by NetBird key or Zenoh slot; handle divergence hides the duplication.
- **Decision:** [ADR 1048](../adr/1048-email-keyed-idempotent-onboarding.md).
- **Onboarding mechanics:** `operator-onboard-utilities.md`; manifests at `ops/operators/<env>/<handle>.yml`.


> **Landscape note (2026-07-09, on merge):** written 2026-06-16 against the ansible-only mint path. Since then the onboarding engine was extracted into the identity plane (goat-id-console owns the store + handlers, #1472/#1477/#1480) and runner-minted manifests now sync into the declared roster (#1473 — the split-brain fix, a first step toward layer 3's mint-consulted registry). The problem statement and the email-keyed decision are unchanged; the pre-mint reuse-or-refuse check now lands in the extracted engine (console/runner) *and* the ansible operator-tier path, not ansible alone. Renumbered F-216→F-239 (ID taken concurrently by the undeclared-topics monitor finding).

## The problem in one sentence

Onboarding mutates three shared planes (NetBird keys, Zenoh slots, Ory identities) from multiple operator laptops whose manifests are gitignored and local, with only advisory coordination and dedup on **handle** (operator-chosen, divergent) rather than **email** (the one stable cross-operator identity).

## Prevention — four layers, email as the spine

Ranked by leverage. #1 and #2 are the technical core; #3 gives cross-operator visibility; #4 is the human gate.

### 1. Dedup on email/person, not handle

Email is the only identity stable across operators — Ory already dedups on it; NetBird and the portal do not. Every mint decision (does this person already have a key? a slot?) must resolve on email, never on the operator-chosen handle. This is the spine the other three layers hang on. A stable per-person key (email, or a derived person-id) must travel into the NetBird setup-key name/metadata and the portal slot record so they become dedup-able by the same key Ory already uses.

### 2. Make the mint path idempotent against live state

Before minting, the onboard queries live shared state by email and reuses or refuses instead of blind-creating:

- **NetBird (Phase 1.4):** `GET /api/setup-keys`, find an unrevoked key for this person (matched via the email-derived label from #1). If one exists → reuse it (re-render the bundle from it) or refuse with a clear message naming the existing key. Today Phase 1.4 unconditionally POSTs.
- **Portal slot:** query the portal for an existing slot for this email → reuse or refuse. Today the portal allocates a fresh slot per onboard.
- **Ory (Phase 2.1):** already idempotent on email — keep, and make it the reference pattern.

Idempotency makes re-runs and concurrent runs *converge* instead of duplicating — the property that would have prevented the entire F-239 incident.

> Note: NetBird setup-key secrets are masked after creation, so "reuse" can re-point the bundle at an existing key only if that key's secret is still held (e.g. same operator re-running). Cross-operator reuse therefore means **refuse + hand off**, not silent re-mint — the refusal names who already issued it (via #3).

### 3. Promote `roster.*` to a shared, mint-consulted issuance registry

`docs/operators/roster.{json,md}` already exists but is regenerated from each laptop's gitignored manifests, so it only reflects one operator's view and gates nothing. Make it the **committed, cross-operator source of truth** keyed by email → handle → issued artifacts, that the onboard **reads before minting** and **appends to after**. This is the "declared half of the Data-Access COP" CLAUDE.md already names. Sanitization bar applies (the committed index carries no secrets; PII handling per the roster's existing rules).

### 4. Enforced onboarding claim

`work pickup <env>-onboarding` becomes a **gate**, not advisory: the onboard refuses to mint while another operator holds the claim for that goatnet. The human backstop for the window where #1–#3 haven't converged. Today the work-bus is advisory (ADR 0006) and was bypassed by both operators in the F-239 incident.

## Remediation — safely reconciling a collision that already happened

When a collision is found (duplicate keys/slots, or one person under two handles):

1. **Inventory all three planes keyed by EMAIL, not handle.** List NetBird setup-keys, Ory identities, and portal slots; group by the person's email. This is the step that *requires joining across operators* — a handle-keyed audit on one laptop cannot see another operator's differently-handled artifacts.
2. **Pick the canonical artifact per person.** Rule: the **enrolled** one wins (a key with `used_times>0` / a live peer). If none enrolled, the operators agree, or newest-complete wins.
3. **Revoke (not delete) the non-canonical NetBird keys — `used=0` only.** Assert `used_times==0` before every revoke. Revoking is reversible and audited; deletion is a later sweep. **Never revoke or delete an enrolled (`used=1`) key** — even though revoking a used key does not disconnect its peer, leaving enrolled keys untouched keeps the canonical record unambiguous.
4. **Retire the non-canonical portal slots** after confirming the slot cert is not in active use (the slot cert is the Zenoh write identity; a mesh-first cut applies — see `credential-revocation-and-offboarding.md`).
5. **Ory:** dedups by email automatically. If two identities exist (the person was minted under two *different* emails), disable the extra.
6. **Distribute only the canonical bundle per person.** Move superseded bundles out of the distribution set; a bundle whose key was revoked must never be handed out.

**Invariants:** never touch a used/enrolled key; revoke before delete; verify per-person before acting; identify "mine" by exact bundle key value or the dated key name, never by guessing.

