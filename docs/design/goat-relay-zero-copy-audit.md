# goat-relay zero-copy + efficiency audit (2026-04-30)

Audit of the data-plane forwarding path looking for allocations, copies, and per-message overhead beyond what the wire protocol requires. Per the user reframe: the win isn't raw Mbps, it's **efficiency** — Mbps per CPU-second, syscalls per MB, allocations per message. That's where kTLS + tokio-uring + zero-copy actually pay vs the Go relay and the hyper-derp publication.

## Hot path summary

The forwarding path through goat-relay for a single Transport frame:

```
peer-A WS write
  → ws_reader (run_reader, ws.rs:339)            [Vec<u8> from tokio-tungstenite 0.24]
  → dispatch_transport (ws.rs:451)               [in-place rewrite_peer_id]
  → Bytes::from(buf) (ws.rs:473)                 [zero-copy — Bytes::from(Vec) reuses alloc]
  → routing_table.forward (routing.rs:89)        [HashMap lookup + mpsc send; Bytes is moved]
  → run_writer (ws.rs:492)                       [drains mpsc]
  → Message::Binary(frame.to_vec()) (ws.rs:507)  ⚠️ ALLOCATES + COPIES
  → ws_writer.send                               [tokio-tungstenite serializes Vec<u8>]
peer-B WS read
```

## Findings

### F-A1 — `frame.to_vec()` in WS writer is the dominant per-message allocation

`goat-relay/src/listener/ws.rs:506-509`:

```rust
if ws_writer
    .send(Message::Binary(frame.to_vec()))
    .await
```

`frame: Bytes`, `to_vec()` allocates a new `Vec<u8>` and `memcpy`s the entire payload. At 100 Mbps single-flow ≈ 14k packets/sec ≈ **14k unnecessary heap allocations/sec on the WS path** that the QUIC path doesn't have.

**QUIC writer comparison** (quic.rs:408-414):

```rust
async fn run_writer(connection: quinn::Connection, mut rx: mpsc::UnboundedReceiver<Bytes>) {
    while let Some(frame) = rx.recv().await {
        if let Err(e) = connection.send_datagram(frame) {  // takes Bytes — zero-copy
```

`send_datagram(frame: Bytes)` queues the `Bytes` directly into the QUIC datagram send queue. No allocation, no copy. This is the asymmetry.

**Fix**: upgrade `tokio-tungstenite = "0.26"` (or newer) which exposes `Message::Binary(Bytes)` instead of `Message::Binary(Vec<u8>)`. Removes the `.to_vec()` copy entirely.

**Expected impact**: at 100 Mbps single-flow, eliminating 14k×~8KB allocations/sec ≈ ~110 MB/sec less memory bandwidth + fewer allocator-lock contentions + fewer cache evictions. Should compound favorably with kTLS (kernel does the actual TX from the same buffer the userspace side handed off). Multi-flow gain probably larger because the allocator becomes the contention point at higher rates.

### F-A2 — `Bytes::copy_from_slice` in `Transport::parse`

`goat-relay-protocol/src/messages/payload.rs:100`:

```rust
let payload = Bytes::copy_from_slice(&buf[TRANSPORT_PAYLOAD_OFFSET..]);
```

The parse path is **not** the forwarding hot path (the forwarder uses `dispatch_transport` which does in-place `rewrite_peer_id` + `Bytes::from(buf)` — zero-copy). `Transport::parse` is used by the conformance fixture binary + tests, and would be used if the relay ever needed to *fully deserialize* a Transport message (we currently never do).

**Verdict**: **Not on the hot path.** Leave as-is unless future code paths need it; benchmark would show no production impact.

### F-A3 — `Vec::from(buf)` in WS reader

`goat-relay/src/listener/ws.rs:364`:

```rust
let bytes = match msg {
    Ok(Message::Binary(b)) => b,  // tokio-tungstenite 0.24 returns Vec<u8>
    ...
};
```

`b` is `Vec<u8>` here. Once we upgrade to tokio-tungstenite 0.26+ this becomes `Bytes`, and we lose nothing (we'd have wrapped Vec→Bytes anyway via `Bytes::from(buf)` at line 473).

**Coupled with F-A1 fix.**

### F-A4 — `mpsc::unbounded_channel` for routing-table writer queues

`goat-relay/src/listener/ws.rs:243`, `goat-relay/src/listener/quic.rs:202`:

```rust
let (tx, rx) = mpsc::unbounded_channel::<Bytes>();
```

`unbounded_channel` allocates per-message linked-list nodes. Under sustained throughput this is millions of small allocs.

**Alternative**: `tokio::sync::mpsc::channel::<Bytes>(N)` (bounded) uses a ring buffer, no per-message allocation. Sizing N is the tradeoff — too small back-pressures sources, too large defeats memory-bound flow control.

**Probable win**: ~similar order of magnitude as F-A1 (thousands of allocations/sec eliminated). Needs sizing experiments — propose N=256 as starting point (matches typical QUIC datagram queue depth).

**Risk**: if the writer task stalls (slow peer-B), bounded channel back-pressures the reader → reader blocks → source-peer's WS slow-reads → TCP back-pressure. That's the *correct* behavior for relay, but it changes the failure-mode semantics. Worth a soak.

### F-A5 — `eprintln!` per dropped frame

`goat-relay/src/listener/ws.rs:478, 482`:

```rust
eprintln!("ws: forward dropped: dest peer not registered");
eprintln!("ws: forward dropped: dest writer closed (auto-deregistered)");
```

Per-drop logging. In normal operation drops are rare (peer offline, peer just disconnected). Under attack or race conditions could fire per packet.

**Hot-path mitigation**: rate-limit or batch via tracing's `info_span!` + counter. Production: structured logging with a per-source rate-limiter. Out of scope for tier 4b but should land before Block 33C (multi-shard).

### F-A6 — `routing_table` uses `HashMap<PeerId, ConnHandle>`

`goat-relay/src/routing.rs` (inferred from `inner.borrow_mut().insert` etc).

`HashMap` lookup is O(1) avg but allocates buckets on grow. At fleet scale (1k+ peers per relay) the table is small + rarely grows post-warmup. Lookup cost is dominated by hashing 36-byte PeerId.

**Alternative**: pre-hash on Auth + use a smaller integer key (`u64`-by-`xxhash`) → faster lookups. Or `BTreeMap` for cache-locality at small sizes.

**Verdict**: probably **not** a meaningful win at the targeted scales. Skip unless profiling shows hash overhead.

## Recommended sequencing

1. **F-A1 (tokio-tungstenite 0.26+ upgrade)** — biggest single win on WS path; closes the QUIC asymmetry. Roll first.
2. **F-A4 (bounded mpsc)** — needs sizing experiments + soak. Land second.
3. **F-A5 (rate-limited drop logging)** — block 33C prereq.
4. **F-A2, F-A3, F-A6** — defer until profiling shows them on the flame graph.

Combined F-A1 + F-A4 should be the difference between "matches the Go relay's tuned ceiling" and "handily beats it." kTLS already on board; eliminating per-message userspace allocations is the next compounding factor.

## What this audit does NOT cover

- Container-init overhead (per-connection setup; tier 4b's main bottleneck is steady-state throughput).
- TLS handshake performance (rustls + ktls handshake is one-shot at connection setup, not in the per-message path).
- QUIC flow control + congestion (quinn's defaults are good; revisit if QUIC measures *worse* than WS).
- I/O-uring vs epoll (tokio-uring is wired but our shape may not surface its advantage at single-shard; revisit at Block 33C multi-shard).

## Cross-refs

- Hot path tested via `goat-relay/benches/routing_dispatch.rs` (criterion microbench landed during Block 33B step 7+).
- Hyper-derp comparison + HASWELL-style profiling reports tracked separately (TODO post-tier-4b verdict).
- Block 33C multi-shard depends on F-A4 sizing being settled.
