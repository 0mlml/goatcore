//! Auth-handshake helpers — Validator trait + parse-side request helper.
//!
//! Spec section: §3.1 of `docs/design/netbird-relay-wire-protocol.md`.
//!
//! This module implements the parse-side of the auth handshake. The
//! response side is trivially `AuthResponse::marshal()` /
//! `HelloResponse::marshal()` from [`crate::messages::auth`], typically
//! pre-marshaled at server start per the upstream `preparedMsg` pattern
//! (spec §3.1.5).
//!
//! The async I/O loop that drives this state machine lives at the
//! consuming binary crate (33B work) — this module is sync helpers
//! only.

use crate::error::{ProtocolError, Result};
use crate::messages::{
    auth::{Auth, Hello},
    MsgType, PeerId, HEADER_SIZE, MAX_HANDSHAKE_SIZE,
};

/// Maximum bytes a server should buffer when reading the first message
/// from a newly-accepted connection. Mirrors upstream's `MaxHandshakeSize`
/// (`shared/relay/messages/message.go:10`).
pub const MAX_HANDSHAKE_BUFFER: usize = MAX_HANDSHAKE_SIZE;

/// Default handshake timeout — connections that have not completed the
/// handshake within this duration must be closed. Mirrors upstream's
/// `handshakeTimeout` (`relay/server/handshake.go:21`).
///
/// The protocol crate exposes this as a `Duration` constant; enforcement
/// is the caller's responsibility (typically by setting a connect-or-read
/// deadline in the consuming binary).
pub const HANDSHAKE_TIMEOUT: core::time::Duration = core::time::Duration::from_secs(10);

/// Parse outcome for the first message a client sends on a new connection.
#[derive(Debug, Clone, PartialEq, Eq)]
#[non_exhaustive]
pub enum HandshakeRequest {
    /// Modern Auth message (post-deprecation of Hello).
    Auth(Auth),
    /// Deprecated Hello message — `additions` field is gob-encoded
    /// `auth.Msg{AuthAlgorithm, AdditionalData}` per spec §3.1.3.
    /// Gob decoding is NOT in this crate; consumers needing the
    /// deprecated path implement gob in their consuming binary.
    Hello(Hello),
}

impl HandshakeRequest {
    /// Parse the first message read from a freshly-accepted connection.
    ///
    /// Validates the version + message-type header, dispatches to
    /// [`Auth::parse`] or [`Hello::parse`], and rejects any other
    /// message type with [`ProtocolError::Parse`].
    ///
    /// Caller is responsible for enforcing [`HANDSHAKE_TIMEOUT`] on the
    /// underlying read.
    pub fn parse(buf: &[u8]) -> Result<Self> {
        if buf.len() < HEADER_SIZE {
            return Err(ProtocolError::ShortMessage {
                need: HEADER_SIZE,
                got: buf.len(),
            });
        }
        let mt = MsgType::try_from(buf[1])?;
        match mt {
            MsgType::Auth => Ok(Self::Auth(Auth::parse(buf)?)),
            MsgType::Hello => Ok(Self::Hello(Hello::parse(buf)?)),
            other => Err(ProtocolError::Parse(format!(
                "expected Auth (0x06) or Hello (0x01) as handshake first-message, got {other:?}"
            ))),
        }
    }

    /// The hashed peer ID being authenticated.
    #[must_use]
    pub fn peer_id(&self) -> &PeerId {
        match self {
            Self::Auth(m) => &m.peer_id,
            Self::Hello(m) => &m.peer_id,
        }
    }

    /// Whether this is the modern (Auth) path. `false` means the
    /// deprecated Hello path; consumers without gob support should
    /// reject those connections.
    #[must_use]
    pub const fn is_modern(&self) -> bool {
        matches!(self, Self::Auth(_))
    }

    /// Borrow the auth-credential bytes.
    ///
    /// - For [`Self::Auth`]: returns the raw `auth_payload` field (opaque
    ///   bytes, ≤ 170 bytes). Pass directly to [`Validator::validate`].
    /// - For [`Self::Hello`]: returns the gob-encoded `auth.Msg` body
    ///   from the `additions` field. The consumer must gob-decode it
    ///   before calling [`Validator::validate_hello`] (gob support is
    ///   out-of-tree for this crate).
    #[must_use]
    pub fn credential_bytes(&self) -> &[u8] {
        match self {
            Self::Auth(m) => m.payload.as_ref(),
            Self::Hello(m) => m.additions.as_ref(),
        }
    }
}

/// Host-supplied auth backend interface.
///
/// Mirrors upstream's `Validator` interface (`relay/server/handshake.go:24`).
/// Both methods receive opaque byte slices; the implementation chooses
/// the credential format (HMAC-SHA256, HMAC-SHA512, mgmt-server token, ...).
///
/// The production implementation is `goat-relay/src/auth.rs`
/// `TimedHmacValidator` (netbird timed-HMAC v2 + the ADR 1021 peer-bound
/// extension, wire-compatible with the netbird relay client + the legacy
/// relay tier). The binary fails closed without a configured secret —
/// there is no permissive/accept-all validator in non-test code (CI
/// enforces this; see F-176).
///
/// Return `Ok(())` to accept; return any [`ProtocolError`] (or wrap a
/// custom error in [`ProtocolError::Parse`]) to reject. **On rejection
/// the caller closes the connection silently** — no rejection message
/// is sent on the wire (per spec §3.1).
pub trait Validator {
    /// Validate the auth payload from a modern Auth message.
    ///
    /// `peer_id` is the claimed relay [`PeerId`] from the same Auth frame;
    /// `payload` is the raw `auth_payload` bytes from
    /// [`crate::messages::auth::Auth`]. Implementations that bind the
    /// credential to the peer identity (ADR 1021 peer-bound tokens) verify
    /// the two together; implementations that don't may ignore `peer_id`.
    ///
    /// # Errors
    ///
    /// Implementations return [`ProtocolError::Parse`] (or a more
    /// specific variant) when the credential is invalid, expired, or
    /// otherwise rejected. The relay closes the connection silently on
    /// any error.
    fn validate(&self, peer_id: &PeerId, payload: &[u8]) -> Result<()>;

    /// Validate the `AdditionalData` field of a deprecated Hello
    /// message's gob-decoded `auth.Msg`. Default implementation rejects.
    /// Overrides only needed by consumers shipping gob support.
    ///
    /// # Errors
    ///
    /// Default returns [`ProtocolError::Parse`]; overrides may reject
    /// with any error.
    fn validate_hello(&self, _additional_data: &[u8]) -> Result<()> {
        Err(ProtocolError::Parse(
            "Hello (deprecated) handshake path not supported by this validator".into(),
        ))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use bytes::Bytes;

    fn peer(byte: u8) -> PeerId {
        let mut b = [0u8; crate::messages::PEER_ID_SIZE];
        b[..4].copy_from_slice(b"sha-");
        b[4..].fill(byte);
        PeerId::from_bytes(b)
    }

    struct AcceptValidator;
    impl Validator for AcceptValidator {
        fn validate(&self, _peer_id: &PeerId, _payload: &[u8]) -> Result<()> {
            Ok(())
        }
    }

    struct RejectValidator;
    impl Validator for RejectValidator {
        fn validate(&self, _peer_id: &PeerId, _payload: &[u8]) -> Result<()> {
            Err(ProtocolError::Parse("denied".into()))
        }
    }

    #[test]
    fn handshake_request_dispatches_auth() {
        let p = peer(0x42);
        let msg = Auth::new(p, Bytes::from_static(b"token")).unwrap();
        let bytes = msg.marshal();
        let req = HandshakeRequest::parse(&bytes).unwrap();
        assert!(matches!(req, HandshakeRequest::Auth(_)));
        assert!(req.is_modern());
        assert_eq!(req.peer_id(), &p);
        assert_eq!(req.credential_bytes(), b"token");
    }

    #[test]
    fn handshake_request_dispatches_hello() {
        let p = peer(0x99);
        let msg = Hello {
            peer_id: p,
            additions: Bytes::from_static(b"gob-bytes-opaque-here"),
        };
        let bytes = msg.marshal();
        let req = HandshakeRequest::parse(&bytes).unwrap();
        assert!(matches!(req, HandshakeRequest::Hello(_)));
        assert!(!req.is_modern());
        assert_eq!(req.peer_id(), &p);
        assert_eq!(req.credential_bytes(), b"gob-bytes-opaque-here");
    }

    #[test]
    fn handshake_request_rejects_other_message_types() {
        // Transport (0x03) is valid mid-conn but not as the first message.
        let mut bytes = vec![0x01, 0x03];
        bytes.extend_from_slice(&[0u8; 36]);
        assert!(matches!(
            HandshakeRequest::parse(&bytes),
            Err(ProtocolError::Parse(_))
        ));
    }

    #[test]
    fn handshake_request_rejects_close_as_first_message() {
        // Close (0x04) is also not a valid handshake opener.
        assert!(matches!(
            HandshakeRequest::parse(&[0x01, 0x04]),
            Err(ProtocolError::Parse(_))
        ));
    }

    #[test]
    fn handshake_request_rejects_unknown_msg_type() {
        assert!(matches!(
            HandshakeRequest::parse(&[0x01, 0xFF]),
            Err(ProtocolError::UnknownMessageType(0xFF))
        ));
    }

    #[test]
    fn validator_accept_path() {
        let v = AcceptValidator;
        assert!(v.validate(&peer(0x01), b"anything").is_ok());
    }

    #[test]
    fn validator_reject_path() {
        let v = RejectValidator;
        assert!(v.validate(&peer(0x01), b"anything").is_err());
    }

    #[test]
    fn validator_default_rejects_hello() {
        let v = AcceptValidator;
        // Default impl of validate_hello rejects even though validate accepts.
        assert!(v.validate_hello(b"anything").is_err());
    }

    #[test]
    fn handshake_timeout_constant() {
        assert_eq!(HANDSHAKE_TIMEOUT.as_secs(), 10);
    }

    #[test]
    fn end_to_end_handshake_accept() {
        let p = peer(0x33);
        // Client → server first message
        let auth = Auth::new(p, Bytes::from_static(b"valid-token")).unwrap();
        let bytes = auth.marshal();

        // Server side: parse + validate
        let req = HandshakeRequest::parse(&bytes).unwrap();
        let validator = AcceptValidator;
        validator.validate(req.peer_id(), req.credential_bytes()).unwrap();

        // Server response: pre-marshaled AuthResponse
        let response = crate::messages::auth::AuthResponse::new(
            "rels://relay-rlb.legacy.example:33073".to_owned(),
        )
        .unwrap();
        let response_bytes = response.marshal();

        // Round-trip the response client-side
        let parsed_response =
            crate::messages::auth::AuthResponse::parse(&response_bytes).unwrap();
        assert_eq!(parsed_response.address, "rels://relay-rlb.legacy.example:33073");
    }
}
