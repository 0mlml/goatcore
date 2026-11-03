use std::io::{Read, Write};
use std::net::TcpStream;
use std::sync::Arc;
use std::time::{Duration, Instant};

use rustls::pki_types::pem::PemObject;
use rustls::pki_types::{CertificateDer, PrivateKeyDer, ServerName};
use rustls::{ClientConfig, ClientConnection, RootCertStore};

/// Counts bytes crossing the socket so we can report handshake flight sizes.
struct CountingStream {
    inner: TcpStream,
    tx: u64,
    rx: u64,
}

impl Read for CountingStream {
    fn read(&mut self, buf: &mut [u8]) -> std::io::Result<usize> {
        let n = self.inner.read(buf)?;
        self.rx += n as u64;
        Ok(n)
    }
}

impl Write for CountingStream {
    fn write(&mut self, buf: &[u8]) -> std::io::Result<usize> {
        let n = self.inner.write(buf)?;
        self.tx += n as u64;
        Ok(n)
    }
    fn flush(&mut self) -> std::io::Result<()> {
        self.inner.flush()
    }
}

fn percentile(sorted: &[u128], p: f64) -> u128 {
    if sorted.is_empty() {
        return 0;
    }
    let idx = ((sorted.len() as f64 - 1.0) * p).round() as usize;
    sorted[idx]
}

fn main() {
    let args: Vec<String> = std::env::args().collect();
    if args.len() < 7 {
        eprintln!(
            "usage: {} <host:port> <sni> <ca.pem> <client_cert.pem> <client_key.pem> <pq|classical> [iters]",
            args[0]
        );
        std::process::exit(2);
    }
    let addr = &args[1];
    let sni = args[2].clone();
    let ca_path = &args[3];
    let cert_path = &args[4];
    let key_path = &args[5];
    let mode = &args[6];
    let iters: usize = args.get(7).map(|s| s.parse().unwrap()).unwrap_or(200);

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
    // Every iteration must be a FULL handshake — with the default in-memory
    // resumption, iterations 2..N would PSK-resume (no cert exchange, no full
    // KEM) and silently dilute both latency and flight-size averages.
    config.resumption = rustls::client::Resumption::disabled();
    let config = Arc::new(config);

    let server_name = ServerName::try_from(sni).expect("sni");

    let mut durations_us: Vec<u128> = Vec::with_capacity(iters);
    let mut group_seen = String::new();
    let mut tx_total = 0u64;
    let mut rx_total = 0u64;
    let mut failures = 0usize;

    for i in 0..iters {
        let tcp = match TcpStream::connect(addr) {
            Ok(s) => s,
            Err(e) => {
                eprintln!("iter {i}: tcp connect failed: {e}");
                failures += 1;
                continue;
            }
        };
        tcp.set_nodelay(true).ok();
        tcp.set_read_timeout(Some(Duration::from_secs(30))).ok();
        let mut stream = CountingStream { inner: tcp, tx: 0, rx: 0 };
        let mut conn =
            ClientConnection::new(config.clone(), server_name.clone()).expect("conn");
        let start = Instant::now();
        let mut ok = true;
        while conn.is_handshaking() {
            match conn.complete_io(&mut stream) {
                Ok(_) => {}
                Err(e) => {
                    eprintln!("iter {i}: handshake failed: {e}");
                    failures += 1;
                    ok = false;
                    break;
                }
            }
        }
        if !ok {
            continue;
        }
        let elapsed = start.elapsed().as_micros();
        durations_us.push(elapsed);
        tx_total += stream.tx;
        rx_total += stream.rx;
        if group_seen.is_empty() {
            if let Some(g) = conn.negotiated_key_exchange_group() {
                let name: rustls::NamedGroup = g.name();
                group_seen = format!("{:?}", name);
            }
        }
        // JSONL per-iteration record for the evidence file.
        println!(
            "{{\"iter\":{i},\"mode\":\"{mode}\",\"dur_us\":{elapsed},\"tx\":{},\"rx\":{}}}",
            stream.tx, stream.rx
        );
    }

    durations_us.sort_unstable();
    let n = durations_us.len();
    let sum: u128 = durations_us.iter().sum();
    eprintln!(
        "SUMMARY mode={mode} group={group_seen} ok={n} fail={failures} \
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
