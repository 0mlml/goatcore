# ADR 1115: Goatnet e2e acceptance suite — manifest-driven live tests, deploy-ledger triggered

**Status:** Proposed (2026-08-04, design)

## Context

A goatnet standup can leave silent defects — services reporting healthy while
functionally broken (empty OIDC auth config, router storages dead behind a green
healthcheck, health data published under another goatnet's site key, a declared service
tier never deployed). `docker ps`, isitup, and self-reported env-health caught none of them. The
turnkey standup's phase-14 validation gate was designed-not-built; Gate 1's
`fresh-peer-probe.sh` covers four checks of hundreds of aspects. Operator commission:
every provisioned aspect gets an e2e live test proving it online AND functional,
triggered as its dependencies complete deployment.

## Decision

1. **Tests are manifest rows, not scripts.** One SoT manifest
   (`ops/validation/acceptance-tests.yaml`); each row = {id, aspect, probe type, params,
   `requires` (deploy-ledger component names), `applies_when` (capability/pins
   expression), severity, `caught_would_have` citation}. A small probe-type library
   (http-ok, tls-identity, tls-alpn, dns-resolve, bus-liveliness, bus-roundtrip,
   container-health, cmd-assert, oidc-flow, enroll-probe) is parameterized by rows —
   that is how "hundreds of tests" stays maintainable.
2. **The ADR 1060 deploy ledger is the trigger.** A row is eligible when `requires` ⊆
   deployed components. v1: `goat validate <env>` fired per phase/by the orchestrator;
   v2: a ledger watcher fires newly-eligible rows automatically.
3. **Honesty grammar:** PASS | FAIL | UNKNOWN | SKIPPED(dep) | N/A. Nothing-heard never
   green; gates count only PASS. Every probe type is proven red against a known-broken
   fixture before its green is trusted.
4. **Probes run from the mesh-connected operator controller** (outside-in — the path
   real peers traverse), delegating host-local asserts via ansible; least-privilege
   acceptance credentials (own leaf cert row), never admin credentials.
5. **Applicability is derived** from capability flags + fleet-pins + cloud-parity — the
   existing parity joins — never hand-listed per environment.
6. **Standup phase 14 = all applicable rows green**; the suite remains re-runnable
   post-standup as the regression surface. CI later enforces the aspect↔row join
   (declared aspect with no row fails).

## Alternatives rejected

- **One end-of-standup mega-gate:** loses the dependency-triggered early signal; a
  defect discovered hours after its deploy costs a phase of archaeology.
- **Extending isitup:** monitoring up/down ≠ proving function; conflating them weakens
  both (and isitup is intentionally simple).
- **Self-reported health as the gate:** self-report was exactly what lied in the silent
  failures; acceptance must probe outside-in.
- **Per-test scripts:** unmaintainable at hundreds; the row/probe-library split follows
  the repo's manifest-as-data pattern (services.yaml, fleet-pins, service-classes,
  cloud-parity).

## Consequences

- **Positive:** silent-failure classes get a mechanical detector; the no-AI operator's
  judgment surface shrinks to "which row is red and what does its citation say"; the
  cold-read drill gains an objective pass criterion.
- **Negative:** the manifest is a new SoT to keep honest — mitigated by the CI join
  check and the red-first rule.
- **Neutral:** nothing here is built; design + seed manifest only. Build order in
  [goatnet-acceptance-suite.md](../design/goatnet-acceptance-suite.md).
