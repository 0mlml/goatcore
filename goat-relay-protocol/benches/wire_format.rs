//! Wire-format microbenchmarks for the relay's data-plane hot path.
//!
//! Mirrors upstream's `relay/test/benchmark_test.go` shape but at the
//! microbench layer (per-call latency / throughput in isolation, no
//! network). Runs via:
//!
//! ```text
//! cargo bench --bench wire_format
//! ```
//!
//! # What this measures
//!
//! - [`Transport::parse`] — incoming Transport-frame deserialization.
//!   Allocates a [`bytes::Bytes`] for the payload (copy-from-slice).
//! - [`Transport::marshal`] — outgoing Transport-frame serialization
//!   (allocating a fresh `Vec<u8>`).
//! - [`payload::rewrite_peer_id`] — the load-bearing zero-copy
//!   in-place rewrite from decisions-log entry 12 (upstream's
//!   `UpdateTransportMsg` trick: relay reuses the inbound buffer,
//!   overwrites the 36-byte peer_id field with the source peer-id,
//!   forwards). The microbench gives a separate signal for this
//!   per-frame operation.
//!
//! # What this does NOT measure
//!
//! - Network throughput (that's iperf3 / step 8 / 33D).
//! - TLS / WS / QUIC framing (those benchmark tokio-rustls /
//!   tokio-tungstenite / quinn, not our code).
//! - Routing-table dispatch (that's `goat-relay/benches/routing_dispatch.rs`).
//!
//! # Payload size sweep
//!
//! Three sizes — small (100 B, ~typical control message), medium
//! (1 KiB, ~typical WireGuard packet), and large (8 KiB, near the
//! Transport upper bound). Captures the size-dependent constant
//! factors (parse cost is dominated by the `Bytes::copy_from_slice`
//! at large sizes; rewrite cost is constant 36 B regardless).

#![allow(missing_docs)] // criterion_group! / criterion_main! generate
                        // undocumented public items; benches don't need
                        // doc coverage.

use bytes::Bytes;
use criterion::{black_box, criterion_group, criterion_main, BenchmarkId, Criterion, Throughput};
use goat_relay_protocol::messages::payload::{rewrite_peer_id, Transport};
use goat_relay_protocol::messages::{PeerId, PEER_ID_SIZE};

/// Construct a sha-prefixed PeerId with byte `b` filling the suffix.
fn peer(b: u8) -> PeerId {
    let mut bytes = [0u8; PEER_ID_SIZE];
    bytes[..4].copy_from_slice(b"sha-");
    bytes[4..].fill(b);
    PeerId::from_bytes(bytes)
}

/// Pre-marshaled Transport-frame bytes for `parse` benchmarking.
fn marshaled_transport(payload_size: usize) -> Vec<u8> {
    let payload = vec![0xAB; payload_size];
    Transport::new(peer(0xCC), Bytes::from(payload))
        .expect("payload <= MAX_TRANSPORT_PAYLOAD")
        .marshal()
}

fn bench_transport_parse(c: &mut Criterion) {
    let mut group = c.benchmark_group("transport_parse");
    for &size in &[100usize, 1024, 8192] {
        let bytes = marshaled_transport(size);
        group.throughput(Throughput::Bytes(bytes.len() as u64));
        group.bench_with_input(BenchmarkId::from_parameter(size), &bytes, |b, bytes| {
            b.iter(|| {
                let parsed = Transport::parse(black_box(bytes)).expect("valid Transport");
                black_box(parsed);
            });
        });
    }
    group.finish();
}

fn bench_transport_marshal(c: &mut Criterion) {
    let mut group = c.benchmark_group("transport_marshal");
    for &size in &[100usize, 1024, 8192] {
        let payload = Bytes::from(vec![0xAB; size]);
        let msg = Transport::new(peer(0xCC), payload).expect("payload OK");
        group.throughput(Throughput::Bytes(msg.wire_len() as u64));
        group.bench_with_input(BenchmarkId::from_parameter(size), &msg, |b, msg| {
            b.iter(|| {
                let bytes = msg.marshal();
                black_box(bytes);
            });
        });
    }
    group.finish();
}

fn bench_rewrite_peer_id_inplace(c: &mut Criterion) {
    let mut group = c.benchmark_group("rewrite_peer_id_inplace");
    let new_id = peer(0xDD);
    // The rewrite cost should be near-constant at 36 bytes regardless
    // of total buffer size — this bench confirms that property AND
    // gives us a single-call latency number to track for regression.
    for &size in &[100usize, 1024, 8192] {
        let buf = marshaled_transport(size);
        group.throughput(Throughput::Elements(1));
        group.bench_with_input(BenchmarkId::from_parameter(size), &buf, |b, buf| {
            b.iter_batched(
                || buf.clone(),
                |mut owned| {
                    rewrite_peer_id(black_box(&mut owned), &new_id).expect("valid frame");
                    black_box(owned);
                },
                criterion::BatchSize::SmallInput,
            );
        });
    }
    group.finish();
}

criterion_group!(
    wire_format_benches,
    bench_transport_parse,
    bench_transport_marshal,
    bench_rewrite_peer_id_inplace
);
criterion_main!(wire_format_benches);
