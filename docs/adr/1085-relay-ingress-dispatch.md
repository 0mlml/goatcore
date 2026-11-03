# ADR 1085 — Relay ingress dispatch: haproxy TLS-passthrough over N per-backend rathole tunnels

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-07-15 |
| Repo scope | goat core (relay ansible + templates + goatnet inventory) |
| Related | `../design/relay-ingress-dispatch.md` (canonical detail) · ADR 1084 (two-node federation — the enabler) · [ADR 1081](1081-control-plane-storm-resilience.md) (weak-uplink context) · [ADR 1060](1060-universal-deploy-rollback-ledger.md) · F-213 |

## TL;DR (for a layperson)

A goatnet with two management hosts still funnels all public traffic
through its relay boxes into the *first* host's weak internet link. The
obvious fix — point some relays at host A and some at host B — has no
failover and no control over how load splits. Instead, every relay now keeps
a private tunnel to *every* management host, and a tiny traffic director on
each relay picks a healthy tunnel per connection: sticky (the same user
always lands on the same host, because login sessions don't transfer),
weighted (most load goes to the stronger link), and self-healing (a dead or
hung tunnel is detected in seconds and routed around). It generalizes to any
number of hosts, not just two.

## Decision

1. **Rathole carries one service per mgmt-host backend, localhost-bound.**
   `server.toml` renders `[server.services.<name>]` → `127.0.0.1:<tunnel_port>`
   per backend when `rathole_ingress_dispatch_enabled`; backend #1 keeps the
   historical `netbird-mgmt` name so incumbent clients need no change. The
   legacy single-service `0.0.0.0:443` rendering is untouched when the flag is
   off (fleet default).
2. **haproxy owns the relay's public :443 in TCP mode, TLS passthrough**
   (relays never hold keys — L4-passthrough posture preserved). Routing
   policy, in order: **SNI pins** send single-writer hostnames (e.g. intake)
   to their home backend only, fail-closed; the **default pool balances by
   consistent source-hash** (identical config on every relay ⇒ a client maps
   to the same backend fleet-wide, protecting the deliberately-non-replicated
   Ory session/flow state, ADR 1010 D8) with **per-backend weights** to shed
   the weak uplink; **check-ssl health checks** (full TLS handshake, 3s
   interval) evict a wedged tunnel in ~9s.
3. **The watchdog restarts; haproxy routes.** The F-213 watchdog probes each
   tunnel port (not :443 — haproxy would mask a single wedge) and restarts
   rathole-server only on the wedge signature (connect-OK, TLS dead/slow).
   Connection-refused is exempt: it means "backend offline / not yet wired,"
   which haproxy already routes around, and restarting for it would cut
   healthy tunnels repeatedly.
4. **Generalized N, config-only.** Backends are a per-goatnet list
   (`rathole_ingress_backends`: name / service / tunnel_port / weight /
   sni_pin) — adding a mgmt host is one list entry + one client-side
   group_var (`rathole_client_service_name`), no code. Practical envelope
   ~10 backends.
5. **Rollback = flag flip** (ADR 0859 discipline): disabling stops haproxy
   *before* rathole rebinds legacy `0.0.0.0:443`; deploys write the per-relay
   rollback ledger (`relay-ingress-dispatch`, `config`, ADR 1060).

## Consequences

- The second host serves real production ingress continuously (exercised,
  not a cold standby); the first host's uplink sheds the majority of mgmt-sync/signal/enrollment
  load by weight.
- Either mgmt host can die and every relay keeps serving within seconds —
  the F-213 wedge class stops being user-visible.
- A converted relay is valid with only one live backend (others show DOWN),
  so the relay fleet converts before the second host's tunnels exist.
- Precondition for giving a backend weight: its Traefik must serve the apex
  end-to-end.
- New relay component (haproxy) inside the complexity budget: it *replaces*
  routing hacks (static partitions, watchdog-as-failover) rather than adding
  a coordination plane; config is one template, no state.

## Alternatives (brief)

- **Static relay partition** — no failover, no load-aware split, breaks
  cross-relay session stickiness. Rejected.
- **DNS steering** — relays *are* the DNS targets; cannot choose a backend
  behind them; TTL failover is minutes. Rejected.
- **Balancer on a mgmt host** — dispatcher behind the very uplink being shed.
  Rejected.
- **nginx-stream instead of haproxy** — no consistent hashing on source in
  stream mode without lua; haproxy's chash + check-ssl are exactly the two
  load-bearing features. Rejected.

## Amendment 1 (2026-07-16) — signal is not any-node; F-256

The original decision treated signal as any-node ("signal federates").
**Wrong in one load-bearing way:** signaling is connection-state-ful — a
peer is present on exactly one signal server, so balancing signal makes
cross-shard forwarding (federation) load-bearing, and its transport was the
other host's **mesh IP**. A routine host⇄host P2P flap therefore cannot
heal (the healer's transport dies with the patient) and the mesh shards
(F-256).

Amended decision:

1. **Invariant:** signal may take weight on more than one backend only when
   inter-signal forwarding rides a **partition-independent transport**
   (public path / relay-tunneled — both hosts keep public egress through
   a mesh partition). Until that lands, the second backend runs MAINT'd
   hot-standby at the dispatchers (`set server balanced/<b> state ready` =
   instant re-enable; the pinned intake backend and check-based failover
   are unaffected).
2. **Durable fix** (BUILT 2026-07-17, mesh-control-path Phase 3; **partition
   drill PASSED → second backend UN-MAINT'd, weight 0→1, active-active** —
   F-264: cross-shard federation survives a host⇄host mesh partition via the
   relay-backup leg, ~240ms, ~30s breaker transition gap): dual-path federation
   transport —
   bearer-token auth on the federation surface (both paths), SNI-pinned
   per-host public federation hostnames through the relay dispatchers
   (Traefik-terminated TLS on the enrollment entrypoint), and a host-local
   `federation-proxy` haproxy (peer mesh IP primary, relay-tunneled public
   backup) that `--federation-peers` dials. Detail:
   `../design/relay-ingress-dispatch.md`
   §"Dual-path federation transport". Alternative considered: split signal
   onto its own public hostname with a pin-with-failover dispatcher policy
   (keeps mgmt sync dual-homed — the bulk load — while signal stays
   single-homed); viable, but fixing the federation transport is strictly
   stronger since failover itself crosses shards.
3. **ADR 1081 L2 interaction:** the signal negative cache must not record
   recipients offline while federation transport is unhealthy — "absent
   across federation" is unknowable then. (Landed with the Phase 3 build,
   F-256: the cache records only when the federation broadcast was
   *complete* — every peer gave a definitive answer for that lookup —
   evaluated per-call in signal-server's relay path.)

mgmt sync / enrollment / Ory login remain genuinely any-node (DB-backed,
replicated) — the two-node hosting model stands; only signal needed the
stronger transport guarantee.
