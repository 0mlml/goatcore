# ADR 0602 — Rosenpass hybrid rekey for WireGuard session keys

> ## ⛔ DISABLED FLEET-WIDE 2026-08-15 — DO NOT RE-ENABLE WITHOUT READING THIS
>
> Rosenpass is **off** by default (`rosenpass_enabled: false`). It is not
> off for cost or policy — **it breaks the mesh it is meant to secure.**
>
> Rosenpass v1 mandates Classic McEliece 460896, whose static public key is
> **524,160 bytes**, and NetBird puts the **whole key in every signal OFFER and
> ANSWER** (upstream behaviour, identical on `upstream/main` — not a fork bug).
> Signal traffic comes to dwarf the mesh's own data traffic, and each newly
> enabled peer multiplies the large requests and loses connections, recovering
> the moment it is disabled. With `--rosenpass-permissive` set this is not a
> negotiation mismatch: the 512 KB offers saturate the client's uplink and starve ICE of the small
> CANDIDATE messages that actually establish links.
>
> **Everything below is still correct about how to enable Rosenpass. It is
> currently the wrong thing to do.** Read
> F-338
> and [ADR 1134](1134-rosenpass-key-distribution-by-digest.md) first.
> ADR 1134 D2/D3 is the way back: ship `blake2s(spk)` and resolve the full key
> once per peer-pair.



| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Related | [`0303-wireguard-data-plane.md`](0303-wireguard-data-plane.md), [`0600-two-pq-paths.md`](0600-two-pq-paths.md) |

## TL;DR (for a layperson)

Inside our mesh, we add post-quantum protection to WireGuard
sessions using a small companion daemon called Rosenpass. It
sits next to WireGuard, periodically negotiates a fresh
post-quantum-strong key, and feeds it into WireGuard as a
"pre-shared key" — the same hook WireGuard uses for any
external entropy. This gives us PQ protection intra-mesh
without changing WireGuard or losing its speed.

## Context

WireGuard's classical key-exchange (Curve25519) is fast and
audited but not post-quantum. Adding PQ to WireGuard directly
would require a protocol change and FIPS-style bureaucracy.
Rosenpass is the lightweight pattern: a companion daemon that
negotiates a hybrid (classical + PQ) shared secret and injects
it into WireGuard via the standard PSK mechanism.

This is intra-mesh protection only and makes no FIPS claim.
Cross-boundary FIPS
PQC is the boundary node's job
([`0601`](0601-fips-pqc-boundary-node.md)).

## Decision

Rosenpass runs alongside WireGuard on every mgmt-host and
peer:

- **Pattern:** Rosenpass hybrid (classical + PQ) per-pair
  negotiation; the PQ secret is injected as the WireGuard
  PSK on the same pair.
- **Cadence:** per Rosenpass upstream defaults (the design doc
  does not pin a specific Rosenpass rekey interval; Rosenpass
  and WireGuard rekey on independent schedules — Rosenpass
  re-derives the PSK periodically and WireGuard's standard
  session-key rotation continues underneath). Pin the actual
  cadence in the Block 19 implementation tracker once the
  shipped configuration is finalized.
- **Implementation:** the NetBird client **embeds** Rosenpass
  in-process via the `cunicu.li/go-rosenpass` Go library
  (`client/internal/rosenpass/manager.go`) — there is **no
  separate Rosenpass binary** to ship, sign, or pin. Enablement
  is the `--enable-rosenpass` client flag (persisted by
  `netbird up`); the NetBird image pin is the only artifact. See
  the correction note below and
  `../design/rosenpass-intra-mesh-pqc.md`.
- **Tracked as Block 19** in
  `../project/implementation-plan.md`.
- **Not a FIPS claim.** Rosenpass is not a FIPS-validated
  module.

> **Correction note (2026-04-25).** The original draft of this
> ADR pinned the cadence as "every 2 minutes / 60 GiB", which is
> WireGuard's default session-rekey cadence — not Rosenpass's
> hybrid-PQ rekey cadence. The two protocols rekey
> independently. The canonical design doc
> (`fips-pqc-path.md`) does not pin a specific Rosenpass
> cadence; that's an implementation detail to be locked in
> Block 19. Original draft authored without opening the design
> doc. Pinning the Rosenpass cadence is a forward task.

> **Correction note (2026-07-12).** The original decision line
> said "upstream Rosenpass **binary**, signed, pinned by digest,
> deployed via Ansible." That is wrong against the code: NetBird
> embeds the `cunicu.li/go-rosenpass` **library** in-process
> (`client/internal/rosenpass/manager.go` imports `rp
> "cunicu.li/go-rosenpass"`; keypair + server run in the client
> process, not a sidecar). There is no separate binary. Enablement
> is the `--enable-rosenpass` / `--rosenpass-permissive` client
> flags — already compiled into our fork (unmodified by our four
> patches) and already supported by our self-hosted management
> server (`management/server/metrics/selfhosted.go` counts
> `RosenpassEnabled`; the network map carries the RP flags). The
> feature is **off in our deployment** (`grep -rn rosenpass ops/`
> is empty). Block 19 is therefore *enable + validate + roll out
> an existing feature*, not *build + package a binary*. Full plan:
> `../design/rosenpass-intra-mesh-pqc.md`.

## Alternatives considered

- **Modify WireGuard to do hybrid PQ in-protocol.** Heavy;
  would diverge from upstream. Rejected.
- **No intra-mesh PQ, accept harvest-now-decrypt-later
  risk.** Acceptable today, increasingly not later.
  Rejected as the long-term posture.
- **OpenSSH / strongSwan tunnels intra-mesh.** Loses
  WireGuard. Rejected.

## Consequences

**Positive.** Intra-mesh sessions get PQ protection without
changing WireGuard or losing its speed. The PSK injection
hook is a stable WireGuard interface — Rosenpass can be
swapped for another hybrid daemon if needed.

**Negative.** Rosenpass is a small upstream; we monitor for
governance changes. Not a FIPS claim — clearly documented
to avoid misrepresentation.

## Assessor view (SCA-R)

Rosenpass intra-mesh PQ serves SC-12 (key establishment —
hybrid classical + PQ secret) and is consistent with NSA
commercial-grade hybrid guidance. It makes no FIPS claim;
the boundary node ([`0601`](0601-fips-pqc-boundary-node.md))
provides FIPS-validated PQ at the cross-boundary
interconnect.

## Trigger conditions for revisit

- Rosenpass receives FIPS validation. Revisit the FIPS position.
- A different intra-mesh hybrid daemon emerges with stronger
  upstream backing. Evaluate; the PSK-injection pattern
  survives the swap.

---

*See `GLOSSARY.md` for terms and acronyms.*
