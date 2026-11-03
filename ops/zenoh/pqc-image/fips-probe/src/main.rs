//! FIPS-PQC verification gate — see Cargo.toml header for the two modes.
//! Every assertion here is the empirical form of a zenoh-pqc-tls.md §5 claim;
//! a failure must fail the image build (fail-closed).

use std::io::Write;
use std::net::{TcpListener, TcpStream};
use std::sync::Arc;

use rustls::pki_types::{CertificateDer, PrivateKeyDer, ServerName};
use rustls::{ClientConfig, ClientConnection, RootCertStore, ServerConfig, ServerConnection};

fn kx_names(provider: &rustls::crypto::CryptoProvider) -> Vec<String> {
    provider
        .kx_groups
        .iter()
        .map(|g| format!("{:?}(fips={})", g.name(), g.fips()))
        .collect()
}

/// Drive a rustls client handshake over `sock`; returns the negotiated group.
fn client_handshake(client_cfg: Arc<ClientConfig>, sni: &str, sock: &mut TcpStream) -> String {
    let server_name = ServerName::try_from(sni.to_string()).unwrap();
    let mut conn = ClientConnection::new(client_cfg, server_name).unwrap();
    while conn.is_handshaking() {
        if conn.wants_write() {
            conn.write_tls(sock).unwrap();
        }
        if conn.wants_read() {
            conn.read_tls(sock).unwrap();
            conn.process_new_packets().unwrap();
        }
    }
    while conn.wants_write() {
        conn.write_tls(sock).unwrap();
    }
    sock.flush().unwrap();
    format!("{:?}", conn.negotiated_key_exchange_group().map(|g| g.name()))
}

fn loopback(server_cfg: Arc<ServerConfig>, client_cfg: Arc<ClientConfig>) -> String {
    let listener = TcpListener::bind("127.0.0.1:0").unwrap();
    let addr = listener.local_addr().unwrap();
    let srv = std::thread::spawn(move || {
        let (mut sock, _) = listener.accept().unwrap();
        let mut conn = ServerConnection::new(server_cfg).unwrap();
        while conn.is_handshaking() {
            if conn.wants_read() {
                conn.read_tls(&mut sock).unwrap();
                conn.process_new_packets().unwrap();
            }
            if conn.wants_write() {
                conn.write_tls(&mut sock).unwrap();
            }
        }
    });
    let mut sock = TcpStream::connect(addr).unwrap();
    let group = client_handshake(client_cfg, "localhost", &mut sock);
    srv.join().unwrap();
    group
}

fn fips_client_config(ca_pem: Option<&str>) -> ClientConfig {
    let roots = match ca_pem {
        Some(path) => {
            let pem = std::fs::read(path).expect("read ca pem");
            let mut roots = RootCertStore::empty();
            for cert in rustls_pemfile_certs(&pem) {
                roots.add(cert).unwrap();
            }
            roots
        }
        None => RootCertStore::empty(),
    };
    // Built AFTER install_default() below — uses the process-default (FIPS)
    // provider via the same builder() path zenoh's link crates use.
    ClientConfig::builder()
        .with_root_certificates(roots)
        .with_no_client_auth()
}

/// Minimal PEM cert splitter (avoids a rustls-pemfile dep just for this).
fn rustls_pemfile_certs(pem: &[u8]) -> Vec<CertificateDer<'static>> {
    use rustls::pki_types::pem::PemObject;
    CertificateDer::pem_slice_iter(pem)
        .map(|c| c.expect("parse ca pem"))
        .collect()
}

fn main() {
    let args: Vec<String> = std::env::args().collect();

    // §5 claim: the module really enters FIPS mode (power-on self tests pass).
    aws_lc_rs::try_fips_mode().expect("AWS-LC FIPS module failed to enter FIPS mode");
    println!("OK try_fips_mode");

    let fips = rustls::crypto::default_fips_provider();
    let names = kx_names(&fips);
    println!("fips provider kx_groups: {names:?}");
    assert_eq!(
        names.first().map(String::as_str),
        Some("X25519MLKEM768(fips=true)"),
        "X25519MLKEM768 must be offered FIRST and marked fips=true"
    );
    fips.install_default().expect("install fips provider");

    if args.len() >= 4 && args[1] == "--connect" {
        // Live mode: dial the smoke-booted zenohd TLS listener.
        let (addr, sni, ca) = (&args[2], &args[3], args.get(4).map(String::as_str));
        let cfg = fips_client_config(ca);
        assert!(cfg.fips(), "client config must be fips");
        let mut sock = TcpStream::connect(addr).expect("connect");
        let group = client_handshake(Arc::new(cfg), sni, &mut sock);
        println!("negotiated against {addr}: {group}");
        assert_eq!(group, "Some(X25519MLKEM768)", "live zenohd must negotiate the hybrid group");
        println!("OK live X25519MLKEM768");
        return;
    }

    // Loopback suite. Self-signed P-256 — same curve as the goat CA chain.
    let ck = rcgen::generate_simple_self_signed(vec!["localhost".into()]).unwrap();
    let cert_der = CertificateDer::from(ck.cert.der().to_vec());
    let key_der = PrivateKeyDer::try_from(ck.key_pair.serialize_der()).unwrap();
    let mut roots = RootCertStore::empty();
    roots.add(cert_der.clone()).unwrap();

    let server_cfg = ServerConfig::builder()
        .with_no_client_auth()
        .with_single_cert(vec![cert_der.clone()], key_der.clone_key())
        .unwrap();
    let client_cfg = ClientConfig::builder()
        .with_root_certificates(roots.clone())
        .with_no_client_auth();
    assert!(server_cfg.fips() && client_cfg.fips(), "configs must be fips");

    let g = loopback(Arc::new(server_cfg), Arc::new(client_cfg));
    println!("FIPS<->FIPS negotiated: {g}");
    assert_eq!(g, "Some(X25519MLKEM768)");

    // Mixed-fleet cell: classical ring client (today's fleet posture) against
    // the FIPS server — must still connect, down-negotiated to P-256 (pure
    // X25519 is excluded from the FIPS provider; costs one HRR round trip).
    let ring = rustls::crypto::ring::default_provider();
    let server_cfg2 = ServerConfig::builder()
        .with_no_client_auth()
        .with_single_cert(vec![cert_der], key_der)
        .unwrap();
    let ring_client = ClientConfig::builder_with_provider(Arc::new(ring))
        .with_safe_default_protocol_versions()
        .unwrap()
        .with_root_certificates(roots)
        .with_no_client_auth();
    let g2 = loopback(Arc::new(server_cfg2), Arc::new(ring_client));
    println!("ring-client<->FIPS-server negotiated: {g2}");
    assert_eq!(g2, "Some(secp256r1)", "classical client must down-negotiate to P-256");

    println!("OK loopback suite");
}
