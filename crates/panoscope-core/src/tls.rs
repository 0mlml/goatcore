// SPDX-License-Identifier: Apache-2.0
//
// Client-side mTLS material for `tls/` Zenoh endpoints. When `enabled`
// is true, [`apply_tls_to_zenoh_config`] injects
// `transport/link/tls/{root_ca_certificate,connect_certificate,connect_private_key,enable_mtls,verify_name_on_connect}`
// into the Zenoh config before opening the session.
//
// Required when the router has `enable_mtls: true` on its TLS listener.

use anyhow::{anyhow, ensure, Result};
use serde::{Deserialize, Serialize};
use std::path::PathBuf;
use std::sync::Once;

static PQ_PROVIDER_INIT: Once = Once::new();

/// Key-exchange posture for new Zenoh TLS sessions — the F-259 Tier 1
/// runtime kill-switch (docs/design/pqc-kill-switch.md).
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum PqcMode {
    /// Offer X25519MLKEM768 first (the default posture).
    Pq,
    /// Offer classical groups only (X25519, secp256r1, secp384r1) — for
    /// severely degraded links where the larger PQ handshake flight is the
    /// failure factor.
    Classical,
}

impl PqcMode {
    pub fn label(self) -> &'static str {
        match self {
            PqcMode::Pq => "pq",
            PqcMode::Classical => "classical",
        }
    }
}

/// System-wide pqc-mode state file, written by `goat client pqc on|off` /
/// `goat admin pqc`. Contents: `pq` or `classical` (one line).
pub const PQC_MODE_SYSTEM_FILE: &str = "/var/lib/goat/pqc-mode";

/// Per-user fallback for hosts where the caller can't write the system path
/// (operator laptops). The system file, when present, wins — node daemons
/// run as other users and never see this one.
pub fn pqc_mode_user_file() -> Option<PathBuf> {
    // `$HOME` on Unix; `%USERPROFILE%` on Windows, where `$HOME` is usually
    // unset. This crate can't depend on goat-cli's shared resolver (wrong
    // dependency direction), so it carries its own copy of the same 3-line
    // logic — duplication here is fine as long as the fallback is correct
    // everywhere it's duplicated, which it previously wasn't (HOME-only).
    std::env::var_os("HOME")
        .or_else(|| std::env::var_os("USERPROFILE"))
        .map(|h| PathBuf::from(h).join(".config/goat/pqc-mode"))
}

/// Parse a pqc-mode token. `classical`/`off` force classical; `pq`/`on` keep
/// the PQ default. Anything else is unrecognized (caller keeps the default —
/// a garbled flag must not silently change crypto posture in either direction).
fn parse_pqc_mode(raw: &str) -> Option<PqcMode> {
    match raw.trim().to_ascii_lowercase().as_str() {
        "classical" | "off" => Some(PqcMode::Classical),
        "pq" | "on" => Some(PqcMode::Pq),
        _ => None,
    }
}

/// Resolve the pqc mode a NEW process will latch: `GOAT_PQC` env var first
/// (per-process override), then [`PQC_MODE_SYSTEM_FILE`], then the per-user
/// fallback. Default is [`PqcMode::Pq`]. Returns the mode plus a short
/// human-readable source, e.g. `state file /var/lib/goat/pqc-mode`.
pub fn resolve_pqc_mode() -> (PqcMode, String) {
    if let Ok(v) = std::env::var("GOAT_PQC") {
        if let Some(m) = parse_pqc_mode(&v) {
            return (m, "GOAT_PQC env".to_string());
        }
    }
    let mut paths = vec![PathBuf::from(PQC_MODE_SYSTEM_FILE)];
    paths.extend(pqc_mode_user_file());
    for p in paths {
        if let Ok(raw) = std::fs::read_to_string(&p) {
            if let Some(m) = parse_pqc_mode(&raw) {
                return (m, format!("state file {}", p.display()));
            }
        }
    }
    (PqcMode::Pq, "default".to_string())
}

/// Install the rustls CryptoProvider for this process, exactly once, choosing
/// the key-exchange groups from [`resolve_pqc_mode`]: aws-lc-rs with
/// X25519MLKEM768 preferred (default), or the same provider restricted to
/// classical groups when the runtime kill-switch says `classical`.
///
/// This is the panoscope-core PQC chokepoint (ADR 1086 §4 Rust recipe): every
/// surface that opens a `tls/` Zenoh session goes through
/// [`apply_tls_to_zenoh_config`], so installing the provider here brings
/// panoscope + stethoscope + flashlight into post-quantum key exchange in one
/// place — no per-binary change. Against a PQ-capable router the session
/// negotiates X25519MLKEM768; against a classical router it down-negotiates
/// (TLS 1.3 group negotiation), so this is always safe.
///
/// Latch semantics (verified against zenoh-link-tls 1.9.0): zenoh builds its
/// `ClientConfig` from the PROCESS-DEFAULT provider (`utils.rs` uses
/// `ClientConfig::builder*`, no per-session provider injection), so the mode
/// is read once, at the first TLS session build, and holds for the process
/// lifetime. Flipping the state file takes effect for every new process
/// (each goat-cli invocation is one); a long-running surface picks it up on
/// restart. Not per-session — see docs/design/pqc-kill-switch.md Tier 1.
///
/// Idempotent + first-installer-wins: the underlying `install_default()` is a
/// no-op (returns Err, ignored) if a provider is already installed — the same
/// F-247 seam zenoh's own `ring` install tolerates. Must run BEFORE the first
/// `zenoh::open`; calling it while building the session Config guarantees that.
fn ensure_pq_crypto_provider() {
    PQ_PROVIDER_INIT.call_once(|| {
        let mut provider = rustls::crypto::aws_lc_rs::default_provider();
        if resolve_pqc_mode().0 == PqcMode::Classical {
            use rustls::crypto::aws_lc_rs::kx_group;
            provider.kx_groups =
                vec![kx_group::X25519, kx_group::SECP256R1, kx_group::SECP384R1];
        }
        let _ = provider.install_default();
    });
}

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct TlsConfig {
    pub enabled: bool,
    pub root_ca_certificate: String,
    pub connect_certificate: String,
    pub connect_private_key: String,
}

impl TlsConfig {
    /// Verify the three cert paths are non-empty when TLS is enabled.
    /// Returns Ok(()) when disabled (mTLS is optional).
    pub fn validate(&self) -> Result<()> {
        if self.enabled {
            ensure!(
                !self.root_ca_certificate.is_empty()
                    && !self.connect_certificate.is_empty()
                    && !self.connect_private_key.is_empty(),
                "TLS enabled requires root_ca_certificate + connect_certificate + connect_private_key"
            );
        }
        Ok(())
    }
}

/// Inject TLS keys into a Zenoh `Config`. No-op when `tls.enabled` is false.
///
/// Field names match the zenoh 1.9.0 schema at
/// `zenoh-config-1.9.0/src/lib.rs`: `root_ca_certificate` +
/// `connect_certificate` + `connect_private_key`. The router's
/// `enable_mtls=true` gate forces every connecting peer to present a
/// chain-verifiable client cert.
///
/// `verify_name_on_connect: false` — clients (panoscope, flashlight)
/// connect to the router by Docker container name (e.g.
/// `goat-zenoh-router`), which won't appear in the router cert's
/// SAN list (the cert is signed for the FQDN + LAN IP). Chain-to-root
/// validation still applies — only certs signed by the configured
/// `root_ca_certificate` are accepted — so the auth gate is not weakened.
///
/// `enable_mtls: true` — required on the CLIENT side too. Zenoh's
/// TLS layer gates client-cert loading on this flag; without it the
/// `connect_certificate` + `connect_private_key` paths are ignored and
/// the server rejects the handshake with `CertificateRequired`.
/// (Verified live .)
pub fn apply_tls_to_zenoh_config(
    config: &mut zenoh::Config,
    tls: &TlsConfig,
) -> Result<()> {
    if !tls.enabled {
        return Ok(());
    }
    // PQC chokepoint (ADR 1086): install the aws-lc-rs provider before the
    // session opens so panoscope/stethoscope/flashlight offer X25519MLKEM768.
    ensure_pq_crypto_provider();
    for (key, value) in [
        (
            "transport/link/tls/root_ca_certificate",
            tls.root_ca_certificate.as_str(),
        ),
        (
            "transport/link/tls/connect_certificate",
            tls.connect_certificate.as_str(),
        ),
        (
            "transport/link/tls/connect_private_key",
            tls.connect_private_key.as_str(),
        ),
    ] {
        // Quote through serde_json, never a bare `format!`: JSON5 reads `\U`,
        // `\d`, `\.` as escapes and drops the backslash, so every Windows
        // path arrived as `C:Usersname.goatcontexts...` and the session
        // failed "file not found", reported as E_ROUTER_UNREACHABLE.
        // Serializing a `&str` cannot fail.
        let quoted = serde_json::to_string(value).expect("str serializes");
        config
            .insert_json5(key, &quoted)
            .map_err(|e| anyhow!("invalid zenoh TLS config for {key}: {e}"))?;
    }
    config
        .insert_json5("transport/link/tls/verify_name_on_connect", "false")
        .map_err(|e| anyhow!("invalid zenoh TLS verify_name_on_connect: {e}"))?;
    config
        .insert_json5("transport/link/tls/enable_mtls", "true")
        .map_err(|e| anyhow!("invalid zenoh TLS enable_mtls: {e}"))?;
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn validate_passes_when_disabled() {
        let t = TlsConfig::default();
        assert!(t.validate().is_ok());
    }

    #[test]
    fn validate_fails_when_enabled_with_empty_fields() {
        let t = TlsConfig {
            enabled: true,
            ..TlsConfig::default()
        };
        assert!(t.validate().is_err());
    }

    #[test]
    fn validate_passes_when_enabled_with_paths() {
        let t = TlsConfig {
            enabled: true,
            root_ca_certificate: "/root.pem".into(),
            connect_certificate: "/cert.pem".into(),
            connect_private_key: "/key.pem".into(),
        };
        assert!(t.validate().is_ok());
    }

    #[test]
    fn apply_to_zenoh_config_noops_when_disabled() {
        let mut cfg = zenoh::Config::default();
        let tls = TlsConfig::default();
        assert!(apply_tls_to_zenoh_config(&mut cfg, &tls).is_ok());
    }

    #[test]
    fn apply_to_zenoh_config_keeps_windows_backslashes() {
        let root = r"C:\Users\operator\.goat\contexts\default\ca-roots.pem";
        let cert = r"C:\Users\operator\.goat\contexts\default\mtls.cert.pem";
        let key = r"C:\Users\operator\.goat\contexts\default\mtls.key.pem";
        let mut cfg = zenoh::Config::default();
        let tls = TlsConfig {
            enabled: true,
            root_ca_certificate: root.into(),
            connect_certificate: cert.into(),
            connect_private_key: key.into(),
        };
        apply_tls_to_zenoh_config(&mut cfg, &tls).unwrap();
        for (field, want) in [
            ("root_ca_certificate", root),
            ("connect_certificate", cert),
            ("connect_private_key", key),
        ] {
            let got = cfg
                .get_json(&format!("transport/link/tls/{field}"))
                .unwrap();
            assert_eq!(
                serde_json::from_str::<String>(&got).unwrap(),
                want,
                "{field}"
            );
        }
    }

    #[test]
    fn pqc_mode_parses_classical_and_pq_tokens() {
        assert_eq!(parse_pqc_mode("classical"), Some(PqcMode::Classical));
        assert_eq!(parse_pqc_mode("off"), Some(PqcMode::Classical));
        assert_eq!(parse_pqc_mode(" Classical\n"), Some(PqcMode::Classical));
        assert_eq!(parse_pqc_mode("pq"), Some(PqcMode::Pq));
        assert_eq!(parse_pqc_mode("on"), Some(PqcMode::Pq));
    }

    #[test]
    fn pqc_mode_rejects_garbled_tokens() {
        // A garbled flag must not flip crypto posture in either direction.
        assert_eq!(parse_pqc_mode(""), None);
        assert_eq!(parse_pqc_mode("clasical"), None);
        assert_eq!(parse_pqc_mode("true"), None);
    }
}
