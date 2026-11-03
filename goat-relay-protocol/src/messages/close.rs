//! Close + HealthCheck — header-only message types.
//!
//! Spec sections: §3.4 (Close) and §3.5 (HealthCheck) of
//! `docs/design/netbird-relay-wire-protocol.md`.
//!
//! Both messages are header-only (2 bytes total: version + msg_type) — no body.

use super::{MsgType, HEADER_SIZE, PROTOCOL_VERSION};
use crate::error::{ProtocolError, Result};

/// Pre-built byte slice for an outgoing Close message — `[0x01, 0x04]`.
pub const CLOSE_BYTES: [u8; HEADER_SIZE] = [PROTOCOL_VERSION, MsgType::Close as u8];

/// Pre-built byte slice for an outgoing HealthCheck message — `[0x01, 0x05]`.
/// Mirrors upstream's pre-allocated `healthCheckMsg` for hot-path emission
/// without per-call allocation (`shared/relay/messages/message.go:61`).
pub const HEALTH_CHECK_BYTES: [u8; HEADER_SIZE] = [PROTOCOL_VERSION, MsgType::HealthCheck as u8];

/// Build a Close message — returns the static byte sequence.
#[must_use]
pub const fn marshal_close() -> [u8; HEADER_SIZE] {
    CLOSE_BYTES
}

/// Build a HealthCheck message — returns the static byte sequence.
#[must_use]
pub const fn marshal_health_check() -> [u8; HEADER_SIZE] {
    HEALTH_CHECK_BYTES
}

/// Validate that a buffer holds a Close message — header-only, no body.
pub fn parse_close(buf: &[u8]) -> Result<()> {
    if buf.len() < HEADER_SIZE {
        return Err(ProtocolError::ShortMessage {
            need: HEADER_SIZE,
            got: buf.len(),
        });
    }
    if buf[0] != PROTOCOL_VERSION {
        return Err(ProtocolError::UnsupportedVersion(buf[0]));
    }
    if buf[1] != MsgType::Close as u8 {
        return Err(ProtocolError::Parse(format!(
            "expected Close (0x04), got 0x{:02x}",
            buf[1]
        )));
    }
    Ok(())
}

/// Validate that a buffer holds a HealthCheck message — header-only, no body.
pub fn parse_health_check(buf: &[u8]) -> Result<()> {
    if buf.len() < HEADER_SIZE {
        return Err(ProtocolError::ShortMessage {
            need: HEADER_SIZE,
            got: buf.len(),
        });
    }
    if buf[0] != PROTOCOL_VERSION {
        return Err(ProtocolError::UnsupportedVersion(buf[0]));
    }
    if buf[1] != MsgType::HealthCheck as u8 {
        return Err(ProtocolError::Parse(format!(
            "expected HealthCheck (0x05), got 0x{:02x}",
            buf[1]
        )));
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn close_round_trip() {
        let bytes = marshal_close();
        assert_eq!(bytes, [0x01, 0x04]);
        parse_close(&bytes).unwrap();
    }

    #[test]
    fn health_check_round_trip() {
        let bytes = marshal_health_check();
        assert_eq!(bytes, [0x01, 0x05]);
        parse_health_check(&bytes).unwrap();
    }

    #[test]
    fn parse_rejects_short_buffer() {
        assert!(matches!(
            parse_close(&[0x01]),
            Err(ProtocolError::ShortMessage { need: 2, got: 1 })
        ));
    }

    #[test]
    fn parse_rejects_wrong_version() {
        assert!(matches!(
            parse_close(&[0x02, 0x04]),
            Err(ProtocolError::UnsupportedVersion(0x02))
        ));
    }

    #[test]
    fn parse_rejects_wrong_type() {
        // Close parser given a HealthCheck buffer.
        assert!(matches!(
            parse_close(&[0x01, 0x05]),
            Err(ProtocolError::Parse(_))
        ));
    }
}
