# ADR 1083 — goat-client is the single netbird client; public pre-trust distribution

| Field | Value |
|---|---|
| Status | Accepted (direction); implementation sequenced + gated per track |
| Date | 2026-07-15 |
| Repo scope | core + `goat-client` + the netbird fork |
| Related | [`../design/goat-client-distribution.md`](../design/goat-client-distribution.md) (canonical detail) · [`goat-client.md`](../design/goat-client.md) · [`control-plane-storm-resilience.md`](../design/control-plane-storm-resilience.md) (ADR 1081, L1) · `goat-cli.md` (ADR 1019) · [three-stage-candidate-onboarding.md](../design/three-stage-candidate-onboarding.md) (ADR 1062, invite channel) · `os-supply-chain-security.md` |

## TL;DR (for a layperson)

Everyone on the mesh should run **one** networking app — **goat-client** — which
already contains the netbird VPN (built from our own version). Fixes like the
storm-resilience backoff (ADR 1081 L1) then reach people as a normal app update,
not a "download the source and rebuild it yourself" chore. To get goat-client to
someone who hasn't joined yet (the chicken-and-egg), we host signed binaries on a
public page reachable through a **one-time link** delivered in their onboarding
invite — so it isn't an open target for abuse, and a tampered download can't slip
through.

## Decision

1. **goat-client is the single netbird client for the fleet.** It embeds the
   netbird client from our fork (`client/embed`, `go.mod replace =>`
   the fork); `goat-cli` (Rust) is the control surface over it. Stock
   netbird (`pkgs.netbird.io`) and the hand-built patched-`netbird` binary are
   **not** the target path.
2. **Fork-level client fixes ship via goat-client updates.** ADR 1081 L1
   (backoff + circuit breaker) reaches users by bumping goat-client's netbird
   fork pin and rebuilding — no repo clone, no `go build ./client`, no
   overwriting `/usr/local/bin/netbird`.
3. **Retire the netbird-build path.** The operator-laptop-onboarding Phase 3.6
   step (`go build ./client` + repoint `/usr/local/bin/netbird`) is legacy from
   before goat-client embedded netbird. It is removed; stock-netbird users
   **migrate to goat-client**, they do not hand-build a binary.
4. **Public pre-trust distribution of goat-client, done safely.** Binaries for
   all OSes are cosign-signed; download links are **one-time, short-TTL, minted
   as part of the three-stage onboarding invite** (ADR 1062) and delivered
   through that existing channel — solving both chicken-and-egg and the
   DDoS/scraping exposure. Hosted off-mesh (by definition — it must be reachable
   before joining), behind rate-limiting/WAF. The signature-verification key is
   delivered out-of-band in the invite, never from the same page.
5. **Peer-set reduction protects the transition.** Until every peer is on a
   goat-client carrying L1, the storm is bounded on stock clients by shrinking
   how many pairs each client attempts: **lazy connections** (item 5, ADR 1081)
   + **ACL scoping** (separate track). Fewer pairs = smaller storm. These are
   server/config-side and need no client change.

## Why (grounded in verified facts)

- goat-client compiles netbird in from our fork — verified in the repo
  (`internal/innermesh/netbird.go` imports `netbirdio/netbird/client/embed`;
  `go.mod` replaces it with the fork). So it is a clean delivery vector.
- Stock netbird cannot be made to self-throttle from the server: its signal
  client ignores the gRPC error code on send (retries 4× then drops) and treats
  stream errors as disconnects (`shared/signal/client/grpc.go`) — so a
  "gentle retry-after" is a dead end ([control-plane-storm-resilience.md](../design/control-plane-storm-resilience.md)
  §"Delivery reality"). The only client-behavior fix is a client build we
  control = goat-client.
- The onboarding invite already exists as an out-of-band trust channel (ADR
  1062), which is exactly what a one-time signed-download link needs.

## Alternatives considered

- **Keep the hand-built patched-`netbird` path.** Rejected: it asks each operator
  to clone + build + overwrite an existing install — high-ceremony, error-prone,
  and moot now that goat-client embeds netbird.
- **Server-side retry-after to fix stock clients without a client change.**
  Rejected: code-proven dead end (stock netbird ignores the hint).
- **Public open (non-one-time) download page.** Rejected for the pre-trust
  surface: unnecessary DDoS/scraping exposure and weaker abuse control than an
  invite-minted one-time link.

## Consequences

New public, pre-trust surface (the highest-stakes supply-chain point in the
system) — gated behind signing + one-time links + WAF, designed in
[goat-client-distribution.md](../design/goat-client-distribution.md) before any
infra stands up. One legacy onboarding step (Phase 3.6) is removed. goat-client's
maturity must rise from "operator-class dogfood" to non-engineer distribution.
Cross-repo coordination: the netbird fork (fork commit), `goat-client`
(`go.mod` bump + release), core (onboarding, hosting infra).
