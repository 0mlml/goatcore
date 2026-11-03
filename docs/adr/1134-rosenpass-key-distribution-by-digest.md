# ADR 1134 — Rosenpass key distribution: send the digest, resolve the key once

- **Status:** Proposed
- **Date:** 2026-08-15
- **Deciders:** platform
- **Related:** F-338
  (root cause + offline reproduction), ADR 1081 (signal storm resilience — both
  layers miss this), F-105
  (the silent-drop contract that blinds the breaker), ADR 0709 (Rust relay),
  ADR 1085 (relay ingress dispatch — why these bytes are metered),
  rosenpass-intra-mesh-pqc.md

## Context

ZAGROS's mgmt host pulls **~19 Mbit/s inbound across its metered relay-ingress
tunnels to serve a mesh carrying 0.48/1.65 Mbit/s**. Measured at Traefik, after
TLS termination, over 10,251 signal requests in four minutes:

| Request size | % of requests | % of bytes |
|---|---|---|
| < 1 KB | 93.2% | **0.7%** |
| 100–600 KB | 6.8% | **99.3%** |

`max = 524,437`. That is `spkSize = 524160` — the Rosenpass Classic McEliece
460896 static public key — plus 277 bytes of framing. **Every signal OFFER and
ANSWER carries the whole key.**

Three facts constrain any fix, and each one kills an obvious answer:

1. **The key size is protocol-mandated.** `go-rosenpass` pins the suite as
   `Rosenpass v1 mceliece460896 Kyber512 ChaChaPoly1305 BLAKE2s`; `spkSize` is
   524,160 in both v0.4.0 (ours) and v0.5.42 (upstream). It will not shrink.
2. **The full key must still reach every peer.** The initiator needs the
   responder's real `spk` to encapsulate. A digest alone does not complete a
   handshake, so "just send a hash" is not a fix by itself.
3. **This is upstream NetBird**, not a fork regression — upstream `main` is
   identical in every link. Any fork-local fix is a permanent delta.

The traffic persists rather than settling because the ADR 1081 client circuit
breaker is fed `breaker.OnResult(err == nil)`, and the signal server returns
**OK with an empty payload for an unknown recipient** on purpose (F-105 — a
`NotFound` breaks the vanilla daemon's `IsHealthy()` probe). So `err` is nil for
every offer to a dead peer, and `breaker.go:88` treats success as
`state = breakerClosed; failures = 0`. Each dropped offer resets the breaker
built to suppress it. Reproduced: **1000 offers, 0 suppressed.**

## Decision

### D1 — The cost must become O(peers) once, never O(peer-pairs × time)

This is the invariant, and it is what disqualifies the alternatives. Tens of peers ×
512 KB is tens of MB of key material. Holding that as state is trivial.
Re-transmitting it every 2.4 seconds per peer-pair is the defect.

### D2 — Offers carry a digest; the full key is resolved on a miss

`OFFER`/`ANSWER` carry `blake2s(spk)` (32 bytes) instead of `spk` (524,160).
A receiver that does not recognise the digest requests the full key once, and
caches it keyed by digest. A Rosenpass static key is long-lived, so the miss path
runs about once per peer-pair per key-lifetime.

Reproduced effect on the real marshalling path: **524,293 → 161 bytes, 99.97%
reduction**; at the live 2.93 offers/sec, **12.29 Mbit/s → 0.0038 Mbit/s**.

`hashRosenpassKey` already exists at `client/internal/rosenpass/manager.go:52`
and is currently used only for a log line.

### D3 — Negotiate with `featuresSupported`, never a version check

`Body.featuresSupported` (`signalexchange.proto:60`) is the existing capability
mechanism; `DirectCheck uint32 = 1` is its only current member
(`shared/signal/client/client.go:25`). Add one constant. A peer sends the digest
form **only** when the far end advertises support, and the full key otherwise, so
a mixed-version fleet keeps working with no flag day. Version sniffing is
rejected: our fleet already runs at least two client lineages and F-306 is a
standing example of version pins that do not match reality.

### D4 — The signal server does not change

`EncryptedMessage.body` is an opaque NaCl box (`signalexchange.proto:26`); the
server never parses it. This is entirely client↔client. No signal-server
release, no fleet-wide server coordination, and the change cannot regress the
F-105 health-probe contract because it does not touch the server's response
shape.

### D5 — Fix the breaker's input, not the breaker

The breaker is correct. Given a truthful signal it does its job exactly:
**1000 offers → 5 sent, 995 suppressed** in reproduction. The defect is that
`err == nil` cannot distinguish delivered from silently dropped.

The delivered/dropped signal must ride somewhere `IsHealthy()` ignores — a
response field or a gRPC trailer on the existing `Send` reply, not a status code.
`IsHealthy()` sends `Send(RemoteKey="dummy")` and treats **any error** as
"signal disconnected"; that is the whole reason the silent-drop contract exists,
and it must survive.

This is independent of D2 and multiplies with it: D2 cuts the size, D5 cuts the
count.

### D6 — Fix in the fork first, offer upstream

The defect is upstream's and affects every NetBird deployment running Rosenpass.
We carry it as a fork delta because our relay ingress is metered and we are
paying for it now, and we offer the change upstream so the delta can retire.
Fork deltas are patches, not a permanent divergence.

### D7 — Do NOT enable `SIGNAL_SENDER_RATE_LIMIT` as mitigation

`GrpcClient.Send` retries up to 4 times (`shared/signal/client/grpc.go:389`) and
never reads `errdetails.RetryInfo`. Throttling a 512 KB offer makes the client
re-send it: the token bucket **multiplies** the bytes it is meant to reduce. It
is also keyed on the sender, and the live shape is a flat many-sender ×
many-offline-recipient fan, which a per-sender bucket cannot separate from healthy
traffic. Recorded here because it is the intuitive lever and it is backwards.

## Alternatives rejected

- **Distribute `spk` in the management network map**, as `sshConfig.sshPubKey`
  already travels (`management.proto:409-423`). Architecturally tidy and fails
  D1 in a different direction: tens of peers × 512 KB is tens of MB *per map*, pushed to
  every peer on every change. Trades a per-offer cost for a per-map-push cost.
  Viable only with delta encoding, which is a larger change than D2.
- **Compress the key.** McEliece public keys are near-incompressible
  high-entropy material. Measured effort not spent; the structure of the artefact
  makes the outcome predictable.
- **Disable Rosenpass.** Removes the traffic and the day-one PQC property with
  it. Valid as an A/B experiment, never as a fix.
- **Send the key only on the first offer per peer-pair, with no negotiation.**
  Cheaper than D2 and unsafe: no way to recover when the receiver restarts and
  loses the cache, and no way to detect that it has.
- **Suppress at the server by pushing an "offline" hint** down the sender's open
  ConnectStream. Attractive — it stops the bytes before they are sent — but it
  makes the server authoritative about peer liveness on a path that is currently
  advisory, and it needs the server change D4 avoids. Reconsider if D5 proves
  hard to land without breaking `IsHealthy()`.

## Consequences

- Signal bytes on ZAGROS should fall by ~99.3% from D2 alone; the remaining
  ~0.7% is genuine ICE candidate traffic.
- D2 and D5 together are ~99.97% in reproduction, but that figure is arithmetic
  from two measured inputs, not an end-to-end observation. **It is not validated
  until a patched client is measured on a live goatnet.**
- A digest miss adds one round trip per peer-pair per key-lifetime. On a
  denied-edge link that round trip happens when the peer is already reachable
  enough to handshake, so it does not add a new failure mode — but it must be
  covered by a test that exercises the miss path with the far end restarting.
- The reproduction tests in `client/internal/peer/f338_signal_bloat_repro_test.go`
  are written to **fail once the defect is fixed**. Whoever lands the fix must
  delete or invert them deliberately; they cannot rot silently.
- zagros-prod runs the same `rosenpass_enabled: true` and is unmeasured. nova
  has Rosenpass unset and runs the same signal-server image, making it the
  natural control — it has no ADR 1117 door, which is now a concrete cost.
