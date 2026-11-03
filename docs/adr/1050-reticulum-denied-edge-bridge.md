# ADR 1050 — Reticulum bridge: a denied-edge bearer for goat (below the IP line)

**Status:** Accepted — the bridge pattern is **proven** (bidirectional, no-hardware, against current `rns` 1.3.5 + `eclipse-zenoh` 1.9.0) in the sibling repo `goat-reticulum`; field RF bearers + goat-identity mapping are designed-not-built.
**Repo scope:** New sibling repo `goat-reticulum` (the bridge + proof, Python — pulls `rns`/`lxmf`/`eclipse-zenoh`). goat core: docs only ([`reticulum-bridge.md`](https://github.com/usareur-af/goatstuff/blob/main/docs/design/reticulum-bridge.md), this ADR, a CLAUDE.md pointer). `ice-goat`: southbound-contract reference (PR).
**Related:** [ADR 0858](0858-zenoh-image-upstream-pivot.md) (the build-from-source cost this avoids) · [ADR 0217 addendum](0217-shim-repo-containment.md) (sibling-repo containment) · [ADR 1032](1032-zenoh-volatile-non-persisted-namespace-class.md) (the `volatile/blob` chunking the large-payload roadmap mirrors) · [ADR 1033](1033-zenoh-qos-prioritization-reserved-mission-slice.md) (rate-shaping onto a scarce bearer) · [`reticulum-bridge.md`](https://github.com/usareur-af/goatstuff/blob/main/docs/design/reticulum-bridge.md) (the design) · ice-goat `docs/interfaces/southbound.md` (the adapter contract).

## Plain-language summary

goat moves data over an IP overlay (WireGuard mesh) and the Zenoh fabric. Below
the IP line — RF, LoRa, packet radio, serial, denied infrastructure — it has no
bearer. Reticulum (RNS) is built for exactly that regime. This ADR adopts a
**bridge** between the two stacks: a Zenoh peer on the goat side, a Reticulum
node on the denied-edge side, translating fabric samples to Reticulum traffic and
back. goat does not reimplement Reticulum; the bridge is the seam. A
self-contained, no-hardware proof shows data crossing the boundary in both
directions on current `rns` and `eclipse-zenoh`.

## Context

- goat's edge-resilience — "the tactical edge runs itself" — is real **through a
  management-plane outage** (tunnels persist, same-LAN peers keep talking) but
  carries an **unstated dependency on IP reachability between peers**. WireGuard
  P2P and relay fallback both need IP. There is no bearer below the IP line.
- Reticulum is coordination-less, cryptographic-identity-addressed (no IP/DNS/
  central authority), with transport interfaces over almost any medium down to a
  few bits per second — the complement to goat, not a competitor.
- ice-goat's southbound adapter contract already defines the plug-point for
  transport/RF/MANET gateways and explicitly flags a "Zenoh over serial/RF"
  tracked dependency that would need the non-default `transport_serial` cargo
  feature and reopen goat's build-from-source cost (ADR 0858).
- A working proof was built first (no design-only commitment): `make proof` in
  the sibling repo passes both directions, three consecutive runs, with the edge's
  Reticulum identity carried end-to-end onto the Zenoh key.

## Decision

1. **Bridge, don't reimplement.** A single bridge process holds both stacks: a
   Zenoh peer (egress subscribe / ingress publish) and a Reticulum `IN`/`SINGLE`
   destination edge peers Link to. `rns`/`lxmf` are runtime dependencies, never
   vendored or forked.
2. **Zenoh stays on IP; Reticulum owns the cross-to-RF hop.** This sidesteps the
   `transport_serial` shape entirely — **no zenohd fork, no non-default Zenoh
   feature, no reopened build-from-source cost.** If a tier only needs *data*
   (not Zenoh's exact wire) to reach a denied edge, this is the cheaper bearer.
3. **The bearer is opaque.** The Reticulum interface (TCP loopback in the proof;
   `RNode`/LoRa, KISS/packet-radio, serial in the field) is config, not bridge
   code — the same bridge runs over real RF unchanged. This mirrors ice-goat's
   southbound boundary exactly.
4. **Identity crosses the seam.** Edge peers prove a Reticulum identity on the
   Link; the bridge labels ingress data by that identity hash, so edge-sourced
   data is attributable on the fabric (`goat/rns/ingress/<edge-id>`), not
   anonymous. Both legs are encrypted end-to-end (RNS Link + goat WireGuard).
5. **Sibling repo, not goat core.** It pulls a Python RF stack neither goat core
   (Rust/Zenoh) nor ice-goat should depend on, and it is a *reference* bearer —
   so it lives beside the contract it implements (ADR 0217 addendum; demand-gated
   radio tier). goat core carries docs + a pointer only.
6. **Positioned as ice-goat's first denied-edge southbound gateway.** It presents
   an IP-reachable endpoint upward and keeps the RF bearer opaque below, per the
   southbound contract; the media-plane obligations (PTT/COR, link-health) are
   the growth path toward ice-goat's voice use case.

## Alternatives considered

- **Carry Zenoh directly over serial/RF (`transport_serial`) — rejected for now.**
  It is the "purest" answer but reopens ADR 0858's build-from-source cost
  (non-default cargo feature → zenohd fork → maintenance/ABI burden). The bridge
  delivers denied-edge reach without any of that. `transport_serial` stays a
  tracked option for a future tier that genuinely needs Zenoh's wire on the RF
  link.
- **Build our own RF/LoRa stack — rejected.** Reticulum already exists, is
  mature, and is the regime's reference implementation. Reimplementing it would
  violate the complexity budget and the "don't rebuild what exists" posture.
- **Put the bridge in goat core or ice-goat — rejected.** Neither should take a
  Python RF dependency; a reference bearer belongs beside the contract, not
  inside it.
- **An accredited cross-domain solution — out of scope (different problem).** A
  separate conversation; the denied-edge bearer is about *transport below IP*,
  not *transfer between security domains*.

## Consequences

- goat gains a path below the IP line: the honest gap in "the tactical edge runs
  itself" is closeable, proven in principle, on current upstreams — at zero cost
  to the goat substrate's build (no Zenoh fork).
- ice-goat gets its first reference denied-edge southbound gateway and the
  lower-cost resolution of its "Zenoh over serial/RF" tracked dependency.
- The capability is contained: a sibling repo, demand-gated, no new dependency in
  goat core or ice-goat.
- **Built since acceptance:** the large-payload path (`RNS.Resource`, chunked) for
  payloads above the ~383-byte single-packet ceiling — auto-selected, transparent
  in both directions, proven bidirectionally over loopback (a 50 KB ≈ 108-part
  transfer). Field-confirmation over RF is part of the gate below.
- **Gate to canon (move out of in-flight):** a real RF bearer (LoRa/packet-radio/
  serial) proven on hardware (the large-payload Resource path confirmed over it),
  multi-edge routing, a Reticulum-identity → goat-slot-anchor mapping for full
  attribution, and rate-shaping onto the scarce bearer (ADR 1033). Until then it
  is a proven bridge pattern, not a fielded bearer.
