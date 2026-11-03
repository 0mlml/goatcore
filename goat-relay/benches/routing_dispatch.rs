//! Routing-dispatch hot-path microbenchmark.
//!
//! Measures the integrated server-side data-plane operation: take an
//! inbound `Transport` frame buffer, extract the destination peer-id
//! from offset `[2..38]`, in-place rewrite the peer_id field with the
//! source peer-id (zero-copy trick from decisions-log entry 12), and
//! dispatch via [`RoutingTable::forward`] to the destination's
//! writer-task MPSC.
//!
//! Mirrors [`crate::listener::ws::dispatch_transport`] /
//! [`crate::listener::quic::dispatch_transport`] in isolation — no
//! WebSocket framing, no QUIC datagram packetization, no TLS / kTLS.
//! That keeps the signal pure: this measures _our_ dispatch logic, not
//! tokio-tungstenite or quinn.
//!
//! Runs via:
//!
//! ```text
//! cargo bench --bench routing_dispatch
//! ```
//!
//! Companion microbenches at
//! [`goat-relay-protocol/benches/wire_format.rs`](../../../goat-relay-protocol/benches/wire_format.rs)
//! cover [`Transport::parse`] / `marshal` / `rewrite_peer_id` in
//! isolation.

#![allow(missing_docs)] // criterion_group! / criterion_main! generate
                        // undocumented public items; benches don't need
                        // doc coverage.

use bytes::Bytes;
use criterion::{black_box, criterion_group, criterion_main, BenchmarkId, Criterion, Throughput};
use goat_relay::routing::RoutingTable;
use goat_relay_protocol::messages::payload::{
    rewrite_peer_id, Transport, TRANSPORT_PAYLOAD_OFFSET, TRANSPORT_PEER_ID_OFFSET,
};
use goat_relay_protocol::messages::{PeerId, PEER_ID_SIZE};
use tokio::sync::mpsc;

fn peer(b: u8) -> PeerId {
    let mut bytes = [0u8; PEER_ID_SIZE];
    bytes[..4].copy_from_slice(b"sha-");
    bytes[4..].fill(b);
    PeerId::from_bytes(bytes)
}

fn marshaled_transport(dest: PeerId, payload_size: usize) -> Vec<u8> {
    let payload = vec![0xAB; payload_size];
    Transport::new(dest, Bytes::from(payload))
        .expect("payload <= MAX_TRANSPORT_PAYLOAD")
        .marshal()
}

fn bench_parse_rewrite_forward(c: &mut Criterion) {
    let mut group = c.benchmark_group("parse_rewrite_forward");

    // Setup outside the bench loop: routing table + receiver per
    // payload size. The receiver accumulates frames across iters; we
    // drain it in the per-iter setup.
    for &size in &[100usize, 1024, 8192] {
        let routing = RoutingTable::new();
        let dest_id = peer(0xBB);
        let source_id = peer(0xAA);
        let (tx, mut rx) =
            mpsc::channel::<Bytes>(goat_relay::routing::WRITER_QUEUE_CAPACITY);
        routing.insert(dest_id, tx);

        // Pre-build the inbound frame template once per payload size.
        let template = marshaled_transport(dest_id, size);

        group.throughput(Throughput::Bytes(template.len() as u64));
        group.bench_with_input(
            BenchmarkId::from_parameter(size),
            &template,
            |b, template| {
                b.iter_batched(
                    || {
                        // Drain receiver between iters so memory does
                        // not grow unbounded across the bench run, and
                        // build a fresh owned buffer for the rewrite.
                        while rx.try_recv().is_ok() {}
                        template.clone()
                    },
                    |mut buf| {
                        // Hot path mirroring listener/{ws,quic}.rs::
                        // dispatch_transport:
                        // 1. extract dest peer-id from buf[2..38]
                        // 2. in-place rewrite peer_id field with source
                        // 3. forward via routing table
                        let mut dest_id_bytes = [0u8; PEER_ID_SIZE];
                        dest_id_bytes.copy_from_slice(
                            &buf[TRANSPORT_PEER_ID_OFFSET..TRANSPORT_PAYLOAD_OFFSET],
                        );
                        let dest = PeerId::from_bytes(dest_id_bytes);

                        rewrite_peer_id(&mut buf, &source_id).expect("valid frame");

                        routing
                            .forward(black_box(&dest), Bytes::from(buf))
                            .expect("dest registered");
                    },
                    criterion::BatchSize::SmallInput,
                );
            },
        );
    }
    group.finish();
}

/// Isolates the ADR 1055 §2b per-peer counting overhead (F-235 / "what does the
/// inline metric cost the hot path?"). Same dispatch, two ways: `forward`
/// (baseline — delivery + drop accounting only) vs `forward_data` (adds, per
/// frame: 2 peer_stats RwLock read-lock acquisitions + 2 HashMap lookups + 4
/// relaxed atomic fetch_add, for src ingress + dest egress). The
/// `forward_data_counted - forward_baseline` delta IS the per-forwarded-frame
/// cost of the per-peer distribution metric, in isolation (no WS/QUIC/TLS).
fn bench_counting_overhead(c: &mut Criterion) {
    let mut group = c.benchmark_group("per_peer_counting_overhead");
    let routing = RoutingTable::new();
    let dest_id = peer(0xBB);
    let source_id = peer(0xAA);
    let (tx, mut rx) = mpsc::channel::<Bytes>(goat_relay::routing::WRITER_QUEUE_CAPACITY);
    routing.insert(dest_id, tx);
    // forward_data charges the SOURCE's ingress too, so register it — its
    // peer_stats entry must exist for the read-lock lookup to hit (the realistic
    // steady-state case; a miss would be even cheaper).
    let (tx_src, _rx_src) = mpsc::channel::<Bytes>(goat_relay::routing::WRITER_QUEUE_CAPACITY);
    routing.insert(source_id, tx_src);

    let payload = Bytes::from(vec![0xAB; 1024]);
    group.throughput(Throughput::Elements(1));

    // Both cases do the same drain + clone + forward each iter, so the drain
    // (keeps the channel at depth <=1 — no WriterFull) and the refcount clone
    // cancel in the `counted - baseline` delta, leaving just the counting cost.
    group.bench_function("forward_baseline", |b| {
        b.iter(|| {
            while rx.try_recv().is_ok() {}
            routing
                .forward(black_box(&dest_id), payload.clone())
                .expect("dest registered");
        });
    });
    routing.set_peer_counting(true);
    group.bench_function("forward_data_counted", |b| {
        b.iter(|| {
            while rx.try_recv().is_ok() {}
            routing
                .forward_data(black_box(&source_id), black_box(&dest_id), payload.clone())
                .expect("dest registered");
        });
    });
    // Gate OFF: forward_data short-circuits to forward + one bool load. Should
    // land back at ~baseline, proving the runtime knob is effectively free.
    routing.set_peer_counting(false);
    group.bench_function("forward_data_gated_off", |b| {
        b.iter(|| {
            while rx.try_recv().is_ok() {}
            routing
                .forward_data(black_box(&source_id), black_box(&dest_id), payload.clone())
                .expect("dest registered");
        });
    });
    group.finish();
}

criterion_group!(
    routing_dispatch_benches,
    bench_parse_rewrite_forward,
    bench_counting_overhead
);
criterion_main!(routing_dispatch_benches);
