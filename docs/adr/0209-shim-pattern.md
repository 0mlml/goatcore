# ADR 0209 — Shim pattern for vendor cruft absorption

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | Multi |
| Supersedes | — |
| Superseded by | — |
| Related | [`0208-phoenix-native-peer-discipline.md`](0208-phoenix-native-peer-discipline.md), [`0104-no-prime-capture.md`](0104-no-prime-capture.md), `../design/shim-pattern.md` |

## TL;DR (for a layperson)

We integrate with vendor systems (TAK, sensor-net, partner radios,
example-vendor) by writing one small program per vendor — a "shim" — that
translates between the vendor's protocol and our standard internal
protocol. The shim absorbs all the vendor-specific quirks. The rest
of the system never sees the vendor's protocol directly. This keeps
the rest of the system clean even when vendors do strange things.

## Context

Every external vendor we integrate with brings their own protocol,
their own data shape, their own auth model, their own update
cadence, and their own bugs. A naïve integration approach (let each
component speak each vendor's protocol directly) produces the
N²-bilateral-integration explosion described in
`../design/tech-strategy.md`.

The shim pattern is the architectural counter-move: route every
vendor protocol through one bounded translation layer that maps
vendor-specific cruft into our standard internal substrate (Zenoh,
[`0210`](0210-zenoh-universal-substrate.md)).

## Decision

Every vendor protocol is integrated via a shim. The shim is:

- **One per vendor protocol class** (one TAK shim, one sensor-net
  shim, one example-vendor shim — *not* one per device or per feed).
- **Stateless or near-stateless.** State that needs to persist
  lives in the substrate, not in the shim.
- **Phoenix-native** ([`0208`](0208-phoenix-native-peer-discipline.md))
  — supply-chain-delivered, role-declared, capability-discovered,
  cryptographically self-attesting.
- **Bidirectional where the protocol is bidirectional.**
- **Versioned independently of the vendor side.** A vendor
  upgrade is absorbed by a shim revision; the substrate is
  unchanged.

The five shim conventions, codified in
`../design/shim-pattern.md` §2:

1. **Identity.** Ed25519 keypair issued by the offline CA at
   first deployment; stored in encrypted config volume; one
   identity per shim instance (not per vendor session); shim
   signs messages it publishes for high-trust topics. Shim's
   mesh identity is orthogonal to its vendor-side credentials.
2. **Declarative role announcement.** At startup the shim
   publishes to
   `services/shims/<vendor-class>/<instance-id>/announce`
   with class, version, instance, site, capabilities,
   `translates_from`, `translates_to`, `config_digest`. The
   announcement IS the shim's configuration record — the
   management plane holds no per-shim records.
3. **Key-expression namespace.** Predictable namespaces:
   `<domain>/<vendor>/<data-type>/**` for translated data,
   `commands/<vendor>/<instance>/**` for inbound commands,
   `health/shims/<vendor>/<instance>` for 30s heartbeat,
   `errors/shims/<vendor>/<instance>` for structured errors,
   `config/shims/<vendor>/<instance>/**` for declarative config.
   No shim publishes outside its declared namespace.
4. **Restart and failure semantics.** Vendor disconnect:
   shim becomes idle, reports health, retries; does NOT exit.
   Zenoh disconnect: shim exits; orchestrator restarts.
   Config changes affecting steady-state behavior require
   restart (no in-place reload). Ride-through is a consumer
   concern, not a shim concern.
5. **Supply chain and repo layout.** Every shim lives at
   `ops/shims/<vendor-class>/` with Chainguard-based
   Dockerfile, SBOM-attested image, README naming the topics
   + vendor-version compatibility + runbook link.

The TAK-to-Zenoh shim (Block 20D in
`../project/implementation-plan.md`)
is the reference implementation.

> **Correction note (2026-04-25).** The original draft of this
> ADR listed five different "conventions" (single-direction
> translation / no vendor-data leakage past the shim / vendor-
> side faults isolated / per-shim audit trail / per-shim review
> checklist) that are *behavioral properties* the canonical
> conventions enforce, not the conventions themselves. The five
> above are the canonical conventions per `shim-pattern.md` §2.
> Original draft authored without opening the design doc.

## Behavioral checklist — landed 2026-04-25

The five behavioral properties from the original draft are now
appended to the shim review checklist in
`../design/shim-pattern.md` §4.5
under the heading "Behavioral (added 2026-04-25 per ADR 0209
enhancement)." They sit *under* the structural conventions as
properties a reviewer verifies *after* confirming the five
canonical conventions are met.

## Alternatives considered

- **Direct vendor-protocol integration in every consumer.**
  Produces N² bilateral integrations. Rejected.
- **Single mega-bridge that handles all vendors in one process.**
  Couples failure modes; one vendor's bug crashes integrations
  with all others. Rejected.
- **Buy a commercial integration platform.** Adds another
  proprietary component to the system; produces prime capture
  (see [`0104`](0104-no-prime-capture.md)). Rejected.

## Consequences

**Positive.** Vendor cruft is bounded to one component per
vendor. The substrate stays clean. Vendor updates are absorbed
in one place. New consumers join the substrate without
per-vendor knowledge. The shim is the single place to add
vendor-specific audit / telemetry / sanitization.

**Negative.** Each shim is a piece of code that must be
maintained. We accept; the cost scales linearly with vendor
count, not quadratically.

A shim that grows beyond "stateless translation" is a warning
sign — investigate; either re-decompose, or document the state
deliberately.

## Assessor view (SCA-R)

Shims serve AC-4 (information flow enforcement — every
vendor-side data flow is mediated), CM-7 (least functionality —
the shim is the only thing in the system that speaks the
vendor protocol), AU-2 (event logging — per-shim audit trail),
and SI-7 (software, firmware, integrity — shims are
phoenix-native, supply-chain-attested).

The shim pattern is the operational expression of MOSA's
"government-defined interface" requirement
([`0101`](0101-mosa-as-policy-compliance.md)). The substrate
defines our interface; shims adapt vendor protocols into it.

## Trigger conditions for revisit

- A vendor protocol is genuinely simple enough that a shim is
  overkill. Rare; document the exception explicitly.
- A shim grows stateful in ways that look load-bearing.
  Refactor; either fold into the substrate, or split into
  sub-components.
- The substrate substrate changes (e.g. Phase 2 Zenoh gate
  produces NATS fallback). Shims rebind to the new substrate;
  the pattern is unchanged.

---

*See `GLOSSARY.md` for terms and acronyms.*
