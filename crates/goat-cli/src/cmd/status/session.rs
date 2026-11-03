// `goat status` session line (issue #1399): open one real mTLS Zenoh
// session against the router and close it. The ztol line only proves the
// router port accepts TCP; this line proves the router accepts a session
// for this cert. Probe I/O is shared with doctor step 3 (`crate::probe`).

use super::{verdict, verdict_neutral, Verdict};
use crate::i18n::tr2;
use crate::probe::ZENOH_OPEN_TIMEOUT;
use crate::probe::{SessionFailure, SessionMaterial};
use std::net::SocketAddr;

/// The session verdict. Runs only when the ztol probe passed: off mesh the
/// ztol line already owns the failure, so this line reads neutral instead
/// of adding a second red for the same cause.
pub(super) async fn probe_session(
    router_locator: &str,
    material: &SessionMaterial<'_>,
    fallback: Option<SocketAddr>,
    ztol_ok: bool,
) -> Verdict {
    if !ztol_ok {
        return verdict_neutral(tr2!("status-session-skipped"));
    }
    let result =
        crate::probe::zenoh_session(router_locator, material, fallback, ZENOH_OPEN_TIMEOUT).await;
    match result {
        Ok(took) => verdict(
            true,
            tr2!("status-session-ok", ms = took.as_millis().to_string()),
        ),
        Err(failure) => verdict(false, failure_summary(failure)),
    }
}

fn failure_summary(failure: SessionFailure) -> (String, String) {
    match failure {
        SessionFailure::Material(m) => {
            tr2!("status-session-material-missing", detail = m.describe())
        }
        SessionFailure::Config(err) | SessionFailure::TlsConfig(err) => {
            tr2!("status-session-config-failed", err = err.clone())
        }
        SessionFailure::Open { message, .. } => {
            tr2!("status-session-failed", err = message.clone())
        }
        SessionFailure::Timeout(budget) => tr2!(
            "status-session-timeout",
            secs = budget.as_secs().to_string()
        ),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::path::{Path, PathBuf};

    /// Mint a test CA and a leaf signed by it into `dir`; returns the
    /// (cert, key, ca) paths.
    fn mint_material(dir: &Path) -> (PathBuf, PathBuf, PathBuf) {
        use rcgen::PKCS_ECDSA_P256_SHA256;
        use rcgen::{BasicConstraints, CertificateParams, DnType, IsCa, KeyPair};
        let root_kp = KeyPair::generate_for(&PKCS_ECDSA_P256_SHA256).unwrap();
        let mut root_params = CertificateParams::new(vec![]).unwrap();
        root_params.is_ca = IsCa::Ca(BasicConstraints::Unconstrained);
        let root = root_params.self_signed(&root_kp).unwrap();
        let leaf_kp = KeyPair::generate_for(&PKCS_ECDSA_P256_SHA256).unwrap();
        let mut leaf_params = CertificateParams::new(vec!["localhost".into()]).unwrap();
        leaf_params
            .distinguished_name
            .push(DnType::CommonName, "status-session-test");
        let leaf = leaf_params.signed_by(&leaf_kp, &root, &root_kp).unwrap();
        let (cert, key, ca) = (
            dir.join("mtls.cert.pem"),
            dir.join("mtls.key.pem"),
            dir.join("ca-roots.pem"),
        );
        std::fs::write(&cert, leaf.pem()).unwrap();
        std::fs::write(&key, leaf_kp.serialize_pem()).unwrap();
        std::fs::write(&ca, root.pem()).unwrap();
        (cert, key, ca)
    }

    /// The #1399 regression: a router port that accepts TCP and then drops
    /// the connection used to read all-green. ztol stays ok (the port
    /// answers); the session line must go red.
    #[tokio::test(flavor = "multi_thread")]
    async fn tcp_open_but_no_session_is_not_green() {
        let listener = std::net::TcpListener::bind("127.0.0.1:0").unwrap();
        let port = listener.local_addr().unwrap().port();
        // Accept every connection and close it at once: TCP succeeds, no
        // TLS handshake ever completes. A plain thread, off the runtime
        // (clippy.toml disallows tokio::spawn, ADR 0709 D7).
        std::thread::spawn(move || {
            for sock in listener.incoming() {
                drop(sock);
            }
        });
        let dir = tempfile::tempdir().unwrap();
        let (cert, key, ca) = mint_material(dir.path());
        let material = SessionMaterial {
            cert: &cert,
            key: &key,
            ca_roots: &ca,
        };
        let locator = format!("tls/127.0.0.1:{port}");

        let ztol = super::super::probe_ztol_at(&locator, None).await;
        assert!(
            ztol.ok,
            "ztol must pass on an accepting port: {}",
            ztol.summary
        );

        let session = probe_session(&locator, &material, None, ztol.ok).await;
        assert!(!session.ok, "session must fail: {}", session.summary);
        assert!(!session.neutral);
    }

    #[tokio::test]
    async fn off_mesh_skips_the_session_as_neutral() {
        let p = PathBuf::from("/nonexistent");
        let material = SessionMaterial {
            cert: &p,
            key: &p,
            ca_roots: &p,
        };
        let v = probe_session("tls/127.0.0.1:1", &material, None, false).await;
        assert!(v.ok && v.neutral, "off mesh must not add a second red");
        assert_eq!(v.summary, "session not attempted (off mesh; see ztol)");
    }

    #[tokio::test]
    async fn missing_material_is_named_in_the_verdict() {
        let dir = tempfile::tempdir().unwrap();
        let (cert, key, ca) = (
            dir.path().join("mtls.cert.pem"),
            dir.path().join("mtls.key.pem"),
            dir.path().join("ca-roots.pem"),
        );
        let material = SessionMaterial {
            cert: &cert,
            key: &key,
            ca_roots: &ca,
        };
        let v = probe_session("tls/127.0.0.1:1", &material, None, true).await;
        assert!(!v.ok);
        assert!(
            v.summary.contains("mtls.cert.pem is missing"),
            "{}",
            v.summary
        );
    }
}
