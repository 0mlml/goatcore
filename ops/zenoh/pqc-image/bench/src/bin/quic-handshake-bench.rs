// QUIC counterpart to the TCP/TLS handshake microbench (../main.rs).
//
// Same idea, same measured delta: a full QUIC (TLS 1.3) mutual-auth handshake
// against a zenohd `quic/` listener, provider-selectable between aws-lc-rs
// (X25519MLKEM768 preferred) and ring (classical). QUIC rides the SAME rustls
// 0.23 provider as the TCP path via `QuicClientConfig::try_from`, so the PQ-vs-
// classical seam is identical — the only thing that changes versus main.rs is
// the transport, which is exactly the cell this binary adds to the matrix.
//
// Bytes-on-wire come from quinn's own UDP counters (`Connection::stats()`),
// read immediately after the handshake resolves and before close — the QUIC
// analogue of main.rs's CountingStream. quinn 0.11 does not surface the
// negotiated KX group through its handshake-data API, so the group is confirmed
// out-of-band (router-side pcap, or by construction from the shared provider
// that the TCP bench already wire-proves); this binary reports latency + bytes.
//
// QUIC mandates ALPN. zenoh's `quic/` listener ALPN is passed as the optional
// last arg so it can be pinned once confirmed against the live router.

use std::net::{SocketAddr, ToSocketAddrs};
use std::sync::Arc;
use std::time::Instant;

use rustls::pki_types::pem::PemObject;
use rustls::pki_types::{CertificateDer, PrivateKeyDer};
use rustls::{ClientConfig, RootCertStore};

fn percentile(sorted: &[u128], p: f64) -> u128 {
    if sorted.is_empty() {
        return 0;
    }
    let idx = ((sorted.len() as f64 - 1.0) * p).round() as usize;
    sorted[idx]
}

#[tokio::main(flavor = "current_thread")]
async fn main() {
    let args: Vec<String> = std::env::args().collect();
    if args.len() < 7 {
        eprintln!(
            "usage: {} <host:port> <sni> <ca.pem> <client_cert.pem> <client_key.pem> \
             <pq|classical> [iters] [alpn]",
            args[0]
        );
        std::process::exit(2);
    }
    let addr_arg = &args[1];
    let sni = args[2].clone();
    let ca_path = &args[3];
    let cert_path = &args[4];
    let key_path = &args[5];
    let mode = &args[6];
    let iters: usize = args.get(7).map(|s| s.parse().unwrap()).unwrap_or(200);
    // QUIC requires a non-empty ALPN; default "zenoh" is a placeholder to be
    // pinned once confirmed against the live router's quic/ listener.
    let alpn = args.get(8).cloned().unwrap_or_else(|| "zenoh".to_string());

    let provider = match mode.as_str() {
        // prefer-post-quantum puts X25519MLKEM768 first in DEFAULT_KX_GROUPS.
        "pq" => rustls::crypto::aws_lc_rs::default_provider(),
        "classical" => rustls::crypto::ring::default_provider(),
        other => panic!("unknown mode {other}"),
    };

    let mut roots = RootCertStore::empty();
    for cert in CertificateDer::pem_file_iter(ca_path).expect("read ca") {
        roots.add(cert.expect("parse ca cert")).expect("add ca");
    }
    let client_certs: Vec<CertificateDer> = CertificateDer::pem_file_iter(cert_path)
        .expect("read client cert")
        .map(|c| c.expect("parse client cert"))
        .collect();
    let client_key = PrivateKeyDer::from_pem_file(key_path).expect("read client key");

    let mut config = ClientConfig::builder_with_provider(Arc::new(provider))
        .with_protocol_versions(&[&rustls::version::TLS13])
        .expect("tls13")
        .with_root_certificates(roots)
        .with_client_auth_cert(client_certs, client_key)
        .expect("client auth");
    // Full handshake every iteration — no session resumption / 0-RTT, matching
    // the TCP bench so latency and flight sizes are not PSK-diluted.
    config.resumption = rustls::client::Resumption::disabled();
    config.alpn_protocols = vec![alpn.clone().into_bytes()];

    let quic_crypto = quinn::crypto::rustls::QuicClientConfig::try_from(config)
        .expect("rustls->quic client config");
    let client_cfg = quinn::ClientConfig::new(Arc::new(quic_crypto));

    let addr: SocketAddr = addr_arg
        .to_socket_addrs()
        .expect("resolve addr")
        .next()
        .expect("no addr");

    // One client endpoint (ephemeral UDP socket); fresh Connection per iter with
    // resumption disabled = a full handshake each time.
    let mut endpoint =
        quinn::Endpoint::client("0.0.0.0:0".parse().unwrap()).expect("client endpoint");
    endpoint.set_default_client_config(client_cfg);

    let mut durations_us: Vec<u128> = Vec::with_capacity(iters);
    let mut tx_total = 0u64;
    let mut rx_total = 0u64;
    let mut failures = 0usize;

    for i in 0..iters {
        let start = Instant::now();
        let connecting = match endpoint.connect(addr, &sni) {
            Ok(c) => c,
            Err(e) => {
                eprintln!("iter {i}: connect setup failed: {e}");
                failures += 1;
                continue;
            }
        };
        let conn = match connecting.await {
            Ok(c) => c,
            Err(e) => {
                eprintln!("iter {i}: handshake failed: {e}");
                failures += 1;
                continue;
            }
        };
        let elapsed = start.elapsed().as_micros();
        // Read UDP byte counters before close so they reflect the handshake only.
        let stats = conn.stats();
        let tx = stats.udp_tx.bytes;
        let rx = stats.udp_rx.bytes;
        durations_us.push(elapsed);
        tx_total += tx;
        rx_total += rx;
        println!(
            "{{\"iter\":{i},\"mode\":\"{mode}\",\"transport\":\"quic\",\"dur_us\":{elapsed},\"tx\":{tx},\"rx\":{rx}}}"
        );
        conn.close(0u32.into(), b"bench-done");
    }
    endpoint.wait_idle().await;

    durations_us.sort_unstable();
    let n = durations_us.len();
    let sum: u128 = durations_us.iter().sum();
    eprintln!(
        "SUMMARY transport=quic mode={mode} alpn={alpn} group=out-of-band ok={n} fail={failures} \
         mean_us={} p50_us={} p95_us={} p99_us={} min_us={} max_us={} \
         avg_tx_bytes={} avg_rx_bytes={}",
        if n > 0 { sum / n as u128 } else { 0 },
        percentile(&durations_us, 0.50),
        percentile(&durations_us, 0.95),
        percentile(&durations_us, 0.99),
        durations_us.first().copied().unwrap_or(0),
        durations_us.last().copied().unwrap_or(0),
        if n > 0 { tx_total / n as u64 } else { 0 },
        if n > 0 { rx_total / n as u64 } else { 0 },
    );
}
