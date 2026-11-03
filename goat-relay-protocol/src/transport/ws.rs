//! WebSocket transport (TCP path of `rels://`).
//!
//! Spec section: §1 of `docs/design/netbird-relay-wire-protocol.md`.
//!
//! Constants extracted from upstream + a single client-side helper for the
//! `rels://` → `wss://` URL rewrite. The actual WebSocket I/O loop lives at
//! the consuming binary crate (Block 33B work — runtime decision + WS
//! library decision land there).
//!
//! Key invariants from spec §1:
//!
//! - URL path is fixed at [`URL_PATH`] (`/relay`).
//! - Inbound WS frame opcode MUST be Binary; Text/Control frames at the
//!   relay-message layer are protocol errors.
//! - One relay message = one WS Binary frame.
//! - ALPN is NOT set on this path.
//! - No `Sec-WebSocket-Protocol` subprotocol.

use crate::error::{ProtocolError, Result};

/// HTTP path the WS upgrade request goes to. Constant `WebSocketURLPath` in
/// upstream (`shared/relay/constants.go:5`).
pub const URL_PATH: &str = "/relay";

/// Maximum time the HTTP upgrade phase may take before the listener
/// closes the connection. Mirrors upstream
/// `http.Server.ReadHeaderTimeout` (`relay/server/listener/ws/listener.go:44`).
pub const READ_HEADER_TIMEOUT: core::time::Duration = core::time::Duration::from_secs(5);

/// Maximum time a single `Conn::Write` may block waiting for the WS
/// internal buffer to drain. Mirrors upstream `writeTimeout`
/// (`relay/server/listener/ws/conn.go:16`).
pub const WRITE_TIMEOUT: core::time::Duration = core::time::Duration::from_secs(10);

/// Rewrite a `rel://` or `rels://` URL to its `ws://` / `wss://` form for
/// dialing the WebSocket endpoint. Mirrors upstream's `prepareURL`
/// (`shared/relay/client/dialer/ws/ws.go:59-65`).
///
/// # Errors
///
/// Returns [`ProtocolError::Parse`] if the input scheme is neither `rel:`
/// nor `rels:`.
///
/// # Examples
///
/// ```
/// use goat_relay_protocol::transport::ws::rewrite_dial_url;
/// assert_eq!(
///     rewrite_dial_url("rels://relay-rlb.legacy.example:33073").unwrap(),
///     "wss://relay-rlb.legacy.example:33073"
/// );
/// assert_eq!(
///     rewrite_dial_url("rel://relay-rlb.legacy.example:33073").unwrap(),
///     "ws://relay-rlb.legacy.example:33073"
/// );
/// ```
pub fn rewrite_dial_url(url: &str) -> Result<String> {
    if let Some(rest) = url.strip_prefix("rels:") {
        Ok(format!("wss:{rest}"))
    } else if let Some(rest) = url.strip_prefix("rel:") {
        Ok(format!("ws:{rest}"))
    } else {
        Err(ProtocolError::Parse(format!(
            "unsupported scheme: {url} (expected rel:// or rels://)"
        )))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn url_path_constant_matches_upstream() {
        assert_eq!(URL_PATH, "/relay");
    }

    #[test]
    fn timeouts_match_upstream() {
        assert_eq!(READ_HEADER_TIMEOUT.as_secs(), 5);
        assert_eq!(WRITE_TIMEOUT.as_secs(), 10);
    }

    #[test]
    fn rewrite_rels_to_wss() {
        assert_eq!(
            rewrite_dial_url("rels://relay-rlb.legacy.example:33073").unwrap(),
            "wss://relay-rlb.legacy.example:33073"
        );
    }

    #[test]
    fn rewrite_rel_to_ws() {
        assert_eq!(
            rewrite_dial_url("rel://relay-rlb.legacy.example:33073").unwrap(),
            "ws://relay-rlb.legacy.example:33073"
        );
    }

    #[test]
    fn rewrite_preserves_path_and_query() {
        // The protocol shouldn't strip path / query — the dialer overwrites
        // .Path = WebSocketURLPath after parsing, but the rewrite step itself
        // is just a scheme replacement.
        assert_eq!(
            rewrite_dial_url("rels://example.com:443/some/path?q=1").unwrap(),
            "wss://example.com:443/some/path?q=1"
        );
    }

    #[test]
    fn rewrite_rejects_unsupported_scheme() {
        for bad in [
            "https://example.com",
            "wss://example.com",
            "tcp://example.com",
            "example.com",
        ] {
            assert!(
                matches!(rewrite_dial_url(bad), Err(ProtocolError::Parse(_))),
                "should reject {bad}"
            );
        }
    }

    #[test]
    fn rewrite_does_not_replace_substring_in_host() {
        // Defensive: the upstream Go uses strings.Replace(addr, "rel", "ws", 1)
        // which would buggily replace the FIRST "rel" anywhere. Our implementation
        // uses strip_prefix which only matches the scheme. Verify that.
        assert_eq!(
            rewrite_dial_url("rels://relay-rlb.legacy.example:33073").unwrap(),
            "wss://relay-rlb.legacy.example:33073",
            "rewrite should match scheme prefix only, not 'rel' substring in host"
        );
    }
}
