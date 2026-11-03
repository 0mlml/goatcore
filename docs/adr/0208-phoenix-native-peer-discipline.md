# ADR 0208 — Phoenix-native peer discipline

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | Multi |
| Supersedes | — |
| Superseded by | — |
| Related | [`0209-shim-pattern.md`](0209-shim-pattern.md), [`0210-zenoh-universal-substrate.md`](0210-zenoh-universal-substrate.md), [`../design/phoenix-architecture.md`](../design/phoenix-architecture.md). Forward-composes-with: [`0224-progressive-config-rollout-with-auto-revert.md`](0224-progressive-config-rollout-with-auto-revert.md) (cohort taxonomy derived from peer Zenoh-key declarations; rebuild-from-clean is a Phoenix-only revert mode), [`0317-per-service-failure-mode-declaration.md`](0317-per-service-failure-mode-declaration.md) (fail-rebuild is a fourth failure mode available only to Phoenix-native peers), [`1014-codex-pattern-adr-to-enforced-lint.md`](1014-codex-pattern-adr-to-enforced-lint.md) (Codex rule deployment rides supply-chain rebuild rather than a separate promotion mechanism). |

## TL;DR (for a layperson)

Every component on the network must be able to be torn down and
rebuilt at any time, with no special steps. We achieve this by
requiring four things of every component: it identifies itself
cryptographically, it announces its role over a standard channel,
it discovers what it needs to talk to via that same channel, and
its code comes from our verified supply chain. Components built
this way are operationally tougher even if we never actually do
the rebuild — and they make accumulated cruft (hand-tuned configs,
hardcoded addresses, undocumented integration scripts) impossible
by construction.

## Context

The full design is at [`../design/phoenix-architecture.md`](../design/phoenix-architecture.md).
The driving observation: every long-lived system accumulates
cruft — bespoke auth setup, hardcoded discovery, per-peer
integration modules, mediating config files — that prevents
full-system rebuild. The cruft is not noticed until rebuild is
required, at which point it is too expensive to recover.

Phoenix-native peer discipline eliminates the cruft *by
construction*. The discipline itself produces the operational
toughness; whether or not periodic teardown-rebuild is actually
practiced is an operational choice (see "operational practice"
below).

## Decision

Every peer class on the network conforms to four requirements.

| Requirement | Concrete shape |
|---|---|
| **Cryptographic self-attestation** | Persistent identity (Ed25519 keypair generated on-device, private key never leaves), proven on every connection. |
| **Role declaration via Zenoh key expressions** | The peer publishes its role to Zenoh ([`0210`](0210-zenoh-universal-substrate.md)) under a standard key prefix. No hand-written service catalog. |
| **Capability-based discovery** | The peer discovers collaborators via Zenoh queries on capability key prefixes — not via hardcoded hostnames or service IPs. |
| **Supply-chain-delivered code** | The peer's binary comes from the signed, scanned, pinned-digest supply chain ([0700-series](0700-chainguard-first.md)) — no manual build, no operator-installed drift. |

The discipline is **strictly stronger than the operational
practice of scheduled rebuild.** A phoenix-native peer is
operationally tougher than a non-phoenix peer regardless of
whether we ever schedule a teardown-rebuild.

The **scheduled-rebuild operational practice** (deliberate
teardown + rebuild on a cadence) is *optional, per-site,
operator-choice.* The discipline lets that practice be cheap.

The **cruft test** for any new component: "Could this be torn
down and rebuilt in one Ansible run, end-to-end, with no manual
configuration steps?" If the answer is no, the component is not
phoenix-native and either (a) gets refactored, (b) gets absorbed
into a shim ([`0209`](0209-shim-pattern.md)) that *is*
phoenix-native, or (c) gets carved out as a deliberate exception.

## Alternatives considered

- **Phoenix-native discipline as an aspirational goal but not a
  hard requirement.** Components drift back into cruft.
  Rejected.
- **Scheduled rebuild as the primary mechanism (without the
  discipline).** Rebuilds become operationally painful and stop
  happening. Rejected.
- **Phoenix-native only for new components, legacy left as-is.**
  Acceptable transition path; the goal is full coverage. Block
  20 in `../project/implementation-plan.md`
  tracks per-component conformance.

## Consequences

**Positive.** Cruft cannot accumulate on phoenix-native peers.
Disaster recovery is bounded. New-site bring-up is a software
exercise, not a tribal-knowledge exercise. The integration
surface is small and uniform. Capability-based discovery means
adding a new collaborator class does not require a global
registry update.

**Negative.** Some legacy systems (vendor radios, partner
proprietary protocols) cannot be made phoenix-native directly.
We absorb them via the shim pattern ([`0209`](0209-shim-pattern.md)),
which puts the phoenix-native discipline on the *shim*, not on
the legacy system itself.

The discipline depends on Zenoh as the universal peer-
interaction substrate ([`0210`](0210-zenoh-universal-substrate.md)).
Phase 2 gate ([`0509`](0509-phase-2-gate-criteria.md))
determines whether Zenoh is committed to that role at scale.

## Assessor view (SCA-R)

Phoenix-native discipline serves CM-2 (baseline configuration —
the baseline is the supply-chain-delivered binary plus IaC),
CM-3 (change control — drift cannot accumulate), CM-7 (least
functionality — capability-based discovery prevents stale
service exposure), CM-8 (component inventory — Zenoh role
declarations *are* the inventory), CP-10 (system recovery —
rebuild is a known-cost operation), and SR-3 (supply chain
controls — every peer's code is supply-chain-traceable).

The discipline is novel in defense-network terms; the
[`../design/phoenix-architecture.md`](../design/phoenix-architecture.md)
design doc explains the cruft test and the per-class
attestation requirements in detail.

## Trigger conditions for revisit

- A category of legacy system genuinely cannot be made phoenix-
  native and cannot be absorbed into a shim. Document the
  exception explicitly; do not loosen the discipline globally.
- The Phase 2 Zenoh gate produces a no-go decision. Re-examine
  the role-declaration / discovery mechanism (NATS JetStream
  fallback per [`0500`](0500-zenoh-data-fabric.md)); the
  discipline survives the substrate change.

---

*See `GLOSSARY.md` for terms and acronyms.*
