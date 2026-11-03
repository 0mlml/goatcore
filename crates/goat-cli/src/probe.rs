// Shared Zenoh session probe (issue #1399).
//
// `goat doctor` step 3 and the `goat status` session line both need the
// same answer: does a real mTLS Zenoh session open against the router,
// with this profile's cert, key and CA? A TCP connect to the router port
// cannot answer that (the F-253 admission seizure accepts TCP while every
// new session stalls), so both callers open a session and close it at once.
//
// This module owns the I/O and returns a typed outcome; each caller maps it
// to its own verdict shape and message catalog.

use std::net::SocketAddr;
use std::path::{Path, PathBuf};
use std::time::{Duration, Instant};
use tokio::time::timeout;

/// Session-open budget shared by every caller. One number on purpose: the
/// probe covers TCP + the mTLS handshake + the Zenoh Init/Open exchange
/// (~4 RTT plus cert verification), and a relay-fallback path with a
/// retransmit can take seconds. If `goat status` and `goat doctor` used
/// different budgets a slow-but-healthy peer would read red on one and
/// green on the other for the same router.
pub(crate) const ZENOH_OPEN_TIMEOUT: Duration = Duration::from_secs(5);

/// The profile's mTLS material, as paths on disk.
pub(crate) struct SessionMaterial<'a> {
    pub(crate) cert: &'a Path,
    pub(crate) key: &'a Path,
    pub(crate) ca_roots: &'a Path,
}

/// Why a session probe did not open a session.
#[derive(Debug)]
pub(crate) enum SessionFailure {
    /// An mTLS file is missing or unreadable; zenoh was never called.
    Material(MissingMaterial),
    /// The locator did not fit a zenoh config (the raw config error).
    Config(String),
    /// The TLS half of the zenoh config could not be built.
    TlsConfig(String),
    /// zenoh refused to open; the text is already classified
    /// (reachability vs handshake/ACL) by `classify_zenoh_err_line`.
    Open {
        message: String,
        shape: crate::classify::Shape,
    },
    /// No session within the budget.
    Timeout(Duration),
}

/// One missing or unreadable mTLS file.
#[derive(Debug)]
pub(crate) struct MissingMaterial {
    pub(crate) label: &'static str,
    pub(crate) path: PathBuf,
    pub(crate) denied: bool,
}

impl MissingMaterial {
    /// Operator-facing English cause, naming the file and the fix.
    pub(crate) fn describe(&self) -> String {
        if self.denied {
            format!(
                "{} at {} exists but is not readable (permission denied)",
                self.label,
                self.path.display()
            )
        } else {
            format!(
                "{} is missing at {} — re-run `goat profile init <bundle.cbor>`",
                self.label,
                self.path.display()
            )
        }
    }
}

/// Open one Zenoh client session against `locator` with the profile's mTLS
/// material, close it, and return how long the open took.
///
/// `fallback` is the ADR 1082 snapshot-resolved router address; when set,
/// it replaces the locator host (safe: fabric trust is the mTLS chain, not
/// the name — tls.rs sets `verify_name_on_connect: false`).
///
/// Client mode is forced. The zenoh default is peer mode, which retries a
/// failed connect in the background and returns a session anyway, so a
/// rejected or wedged router would still read as "opened".
pub(crate) async fn zenoh_session(
    locator: &str,
    material: &SessionMaterial<'_>,
    fallback: Option<SocketAddr>,
    budget: Duration,
) -> Result<Duration, SessionFailure> {
    let start = Instant::now();
    check_material(material).map_err(SessionFailure::Material)?;
    let endpoint = match fallback {
        Some(addr) => crate::names::substitute_locator_host(locator, &addr)
            .unwrap_or_else(|| locator.to_string()),
        None => locator.to_string(),
    };
    let config = session_config(&endpoint, material)?;
    match timeout(budget, zenoh::open(config)).await {
        Ok(Ok(session)) => {
            // Opened: the mTLS handshake and the router's ACL match passed.
            let _ = session.close().await;
            Ok(start.elapsed())
        }
        Ok(Err(e)) => {
            let shape = crate::classify::shape_of(&e.to_string());
            Err(SessionFailure::Open {
                message: crate::classify::classify_zenoh_err_line(e, &endpoint, "open a session"),
                shape,
            })
        }
        Err(_) => Err(SessionFailure::Timeout(budget)),
    }
}

/// Name the first missing or unreadable mTLS file before zenoh ever sees
/// it; zenoh's own error for a missing file is raw and multi-line.
fn check_material(m: &SessionMaterial<'_>) -> Result<(), MissingMaterial> {
    for (path, label) in [
        (m.cert, "mtls.cert.pem"),
        (m.key, "mtls.key.pem"),
        (m.ca_roots, "ca-roots.pem"),
    ] {
        if let Err(e) = std::fs::File::open(path) {
            return Err(MissingMaterial {
                label,
                path: path.to_path_buf(),
                denied: e.kind() == std::io::ErrorKind::PermissionDenied,
            });
        }
    }
    Ok(())
}

fn session_config(
    endpoint: &str,
    m: &SessionMaterial<'_>,
) -> Result<zenoh::Config, SessionFailure> {
    let mut config = zenoh::Config::default();
    config
        .insert_json5("mode", "\"client\"")
        .map_err(|e| SessionFailure::Config(e.to_string()))?;
    config
        .insert_json5("connect/endpoints", &format!("[\"{endpoint}\"]"))
        .map_err(|e| SessionFailure::Config(e.to_string()))?;
    let tls = panoscope_core::tls::TlsConfig {
        enabled: true,
        root_ca_certificate: m.ca_roots.display().to_string(),
        connect_certificate: m.cert.display().to_string(),
        connect_private_key: m.key.display().to_string(),
    };
    panoscope_core::tls::apply_tls_to_zenoh_config(&mut config, &tls)
        .map_err(|e| SessionFailure::TlsConfig(e.to_string()))?;
    Ok(config)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn missing_material_names_the_file_and_the_fix() {
        let m = MissingMaterial {
            label: "mtls.cert.pem",
            path: PathBuf::from("/ctx/mtls.cert.pem"),
            denied: false,
        };
        let d = m.describe();
        assert!(
            d.contains("mtls.cert.pem is missing at /ctx/mtls.cert.pem"),
            "{d}"
        );
        assert!(d.contains("goat profile init"), "{d}");
        let denied = MissingMaterial { denied: true, ..m };
        assert!(denied.describe().contains("permission denied"));
    }

    #[tokio::test]
    async fn missing_ca_is_reported_before_zenoh_runs() {
        let dir = tempfile::tempdir().unwrap();
        let (cert, key) = (dir.path().join("c.pem"), dir.path().join("k.pem"));
        std::fs::write(&cert, "x").unwrap();
        std::fs::write(&key, "x").unwrap();
        let ca = dir.path().join("ca-roots.pem");
        let material = SessionMaterial {
            cert: &cert,
            key: &key,
            ca_roots: &ca,
        };
        let out = zenoh_session("tls/127.0.0.1:1", &material, None, Duration::from_secs(1)).await;
        match out {
            Err(SessionFailure::Material(m)) => assert_eq!(m.label, "ca-roots.pem"),
            other => panic!("want Material(ca-roots.pem), got {other:?}"),
        }
    }
}
