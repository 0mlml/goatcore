# ADR 1086 — Generalized per-endpoint PQC adoption pattern

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-07-17 |
| Supersedes/relates | Generalizes [ADR 1079](1079-zenoh-pqc-hybrid-tls.md) (the fabric-router instance) into a language-agnostic pattern |
| Design | [`../design/pqc-adoption-pattern.md`](../design/pqc-adoption-pattern.md) |

## Context

PQ hybrid key exchange (X25519MLKEM768) is live on the fabric routers (ADR
1079), and we are now extending it to the client fleet and additional
transports. Done naively, that is a pile of per-service custom work — one
bespoke change per binary, per language — with the general requirements buried
in each diff. That does not scale and it hides the pattern, so the next
endpoint starts from scratch.

The observation that makes a general pattern possible: **PQ on a TLS 1.3 link
is entirely a property of the crypto library at each endpoint**, and TLS
negotiation makes adoption per-endpoint, order-free, and mixed-safe. So every
"bring X into PQC" task is the same recipe parameterized by X's TLS stack.

## Decision

1. **Adopt a single generalized pattern** for bringing any endpoint into PQC,
   documented as the design of record in `pqc-adoption-pattern.md`: identify
   the TLS stack → apply the per-stack recipe (or wrap in a PQ-capable bridge)
   → rebuild → **verify on the wire** → measure coverage. No bespoke
   per-service crypto.
2. **Per-stack recipes are canonical and centralized** (Rust/rustls, Go,
   Python/OpenSSL, C, browser, bridge). A new endpoint uses a recipe; if a
   recipe is missing, add it to the doc rather than solving it privately.
3. **Chokepoint over copies:** when binaries share a TLS-init crate, the
   provider install happens once in that crate (e.g. `panoscope-core`).
4. **Evidence, not assertion:** an endpoint is PQ-adopted only when its
   negotiated group is observed X25519MLKEM768 (via `crypto-posture` or a wire
   capture). Coverage is the measured fraction on the crypto-posture surface,
   not a checklist of merged PRs.
5. **FIPS stays orthogonal and router-side.** Clients use the stock PQ
   provider; the validated-module FIPS claim remains a property of
   router-terminated sessions only (ADR 1079 / claim package).
6. **Transport parity:** QUIC inherits PQ from the same provider seam as TLS;
   enabling QUIC is a transport-availability decision, not additional PQC work.
7. **Maintain the adoption ledger** (design doc §9) as the single view of
   which endpoints are on the pattern and observed-PQ.

## Alternatives considered

- **Per-service ad-hoc adoption.** Rejected — buries the general requirement
  in each diff, doesn't scale, re-solves the same problem per binary. This ADR
  exists specifically to prevent that.
- **A central PQ-terminating proxy for everything (mandatory bridge).**
  Rejected as the default — it re-introduces a chokepoint component and adds a
  hop for endpoints that can do PQ natively. The bridge stays the *escape
  hatch* for endpoints that genuinely can't, not the standard path.
- **Wait for every stack to offer PQ by default.** Rejected — most already do
  (Go 1.24, OpenSSL 3.5, current browsers); the pattern lets us adopt now and
  absorb defaults as they land, per-endpoint, with no coordination.

## Consequences

**Positive.** New PQC adoptions are a lookup-and-apply, not a design exercise;
the requirements are in one place; coverage is measurable; mixed-state fleets
are always functional; QUIC and future transports come along for free.

**Negative.** The pattern depends on the crypto-posture evidence surface being
deployed where coverage matters (already on ZAGROS; a new goatnet must stand it
up to measure its own coverage). The per-stack recipes carry version floors
that must be re-verified as toolchains move — mitigated by the "verify the
actual version at adoption" discipline rather than trusting the floor.

## Assessor view (SCA-R)

Supports SC-12/SC-13 by making the post-quantum posture of every endpoint
individually verifiable on the wire and centrally measurable, rather than
asserted from configuration. No FIPS claim is created or changed by this ADR;
it standardizes *how* endpoints reach PQ, and keeps the validated-module claim
(ADR 1079) scoped to the router as before.
