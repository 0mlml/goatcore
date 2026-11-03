//! Protocol error types.
//!
//! Spec sections covered: §3 (relay-message framing — parse/serialize errors),
//! §4 (routing logic — unknown-peer / queue-overflow conditions),
//! §5 (healthcheck — timeout conditions).

use thiserror::Error;

/// Top-level protocol error.
#[derive(Debug, Error, PartialEq, Eq)]
#[non_exhaustive]
pub enum ProtocolError {
    /// Buffer is too short to parse the requested structure.
    #[error("short message: need {need} bytes, got {got}")]
    ShortMessage {
        /// Minimum bytes needed to parse the structure.
        need: usize,
        /// Bytes actually present.
        got: usize,
    },

    /// Protocol-version byte (offset 0) does not match [`super::messages::PROTOCOL_VERSION`].
    #[error("unsupported protocol version: 0x{0:02x}")]
    UnsupportedVersion(u8),

    /// Message-type byte (offset 1) does not map to any known
    /// [`super::messages::MsgType`] variant.
    #[error("unknown message type: 0x{0:02x}")]
    UnknownMessageType(u8),

    /// Auth or Hello message body lacks the expected 4-byte STUN magic.
    #[error("invalid magic header in auth/hello body")]
    InvalidMagic,

    /// Message exceeds the protocol-level size cap for its type.
    #[error("message too large: {len} bytes (cap {cap})")]
    MessageTooLarge {
        /// Total bytes the caller attempted to (de)serialize.
        len: usize,
        /// Per-type cap (e.g. 212 for Auth, 8820 for Transport).
        cap: usize,
    },

    /// Peer-state list message has a body length that is not a multiple of
    /// [`super::messages::PEER_ID_SIZE`].
    #[error("invalid peer-list body length: {0} bytes (must be multiple of 36)")]
    InvalidPeerListLength(usize),

    /// Empty peer-state list is rejected by upstream's `marshalPeerIDs`.
    #[error("empty peer list")]
    EmptyPeerList,

    /// Generic parser error with caller-supplied context.
    #[error("parse error: {0}")]
    Parse(String),

    /// Generic serializer error with caller-supplied context.
    #[error("serialize error: {0}")]
    Serialize(String),
}

/// Convenience alias used throughout the crate.
pub type Result<T> = core::result::Result<T, ProtocolError>;
