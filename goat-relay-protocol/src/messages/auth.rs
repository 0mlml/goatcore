//! Auth handshake messages — Auth + AuthResponse + deprecated Hello/HelloResponse.
//!
//! Spec section: §3.1 of `docs/design/netbird-relay-wire-protocol.md`.
//!
//! Auth wire layout:
//!
//! ```text
//! offset  size  field
//!   0      1    version (0x01)
//!   1      1    msg_type (0x06)
//!   2      4    magic (0x21 0x12 0xA4 0x42)
//!   6     36    peer_id
//!  42      ?    auth_payload (varlen, total ≤212 bytes → ≤170 bytes)
//! ```
//!
//! The `auth_payload` is opaque at this protocol level — upstream documents it
//! as "HMAC, but the protocol does not limit to use HMAC, it can be any other
//! method." Validation logic lives in `relay/server/handshake.go` (AGPLv3,
//! clean-room read pending — spec §3.1 only captures the wire shape here).

use super::{
    MsgType, PeerId, HEADER_SIZE, MAGIC, MAX_HANDSHAKE_RESP_SIZE, MAX_HANDSHAKE_SIZE,
    PEER_ID_SIZE, PROTOCOL_VERSION,
};
use crate::error::{ProtocolError, Result};
use bytes::Bytes;

const AUTH_MAGIC_OFFSET: usize = HEADER_SIZE;
const AUTH_PEER_ID_OFFSET: usize = HEADER_SIZE + MAGIC.len();
const AUTH_PAYLOAD_OFFSET: usize = AUTH_PEER_ID_OFFSET + PEER_ID_SIZE;

/// Maximum auth_payload size after subtracting header (2) + magic (4) +
/// peer_id (36) from [`super::MAX_HANDSHAKE_SIZE`] (212).
pub const MAX_AUTH_PAYLOAD: usize = MAX_HANDSHAKE_SIZE - AUTH_PAYLOAD_OFFSET;

/// Maximum auth-response address size after subtracting header (2) from
/// [`super::MAX_HANDSHAKE_RESP_SIZE`] (8192).
pub const MAX_AUTH_RESPONSE_ADDRESS: usize = MAX_HANDSHAKE_RESP_SIZE - HEADER_SIZE;

/// Auth message — client → server initial handshake.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Auth {
    /// The client's hashed peer ID.
    pub peer_id: PeerId,
    /// Opaque auth credential (typically an HMAC token from the mgmt server).
    pub payload: Bytes,
}

impl Auth {
    /// Construct an Auth message for marshaling. Returns
    /// [`ProtocolError::MessageTooLarge`] if total wire size would exceed
    /// [`super::MAX_HANDSHAKE_SIZE`] (212 bytes).
    pub fn new(peer_id: PeerId, payload: Bytes) -> Result<Self> {
        if payload.len() > MAX_AUTH_PAYLOAD {
            return Err(ProtocolError::MessageTooLarge {
                len: AUTH_PAYLOAD_OFFSET + payload.len(),
                cap: MAX_HANDSHAKE_SIZE,
            });
        }
        Ok(Self { peer_id, payload })
    }

    /// Total wire-format byte length.
    #[must_use]
    pub fn wire_len(&self) -> usize {
        AUTH_PAYLOAD_OFFSET + self.payload.len()
    }

    /// Serialize to bytes.
    #[must_use]
    pub fn marshal(&self) -> Vec<u8> {
        let mut out = Vec::with_capacity(self.wire_len());
        out.push(PROTOCOL_VERSION);
        out.push(MsgType::Auth as u8);
        out.extend_from_slice(&MAGIC);
        out.extend_from_slice(self.peer_id.as_bytes());
        out.extend_from_slice(&self.payload);
        out
    }

    /// Parse an Auth message.
    pub fn parse(buf: &[u8]) -> Result<Self> {
        if buf.len() < AUTH_PAYLOAD_OFFSET {
            return Err(ProtocolError::ShortMessage {
                need: AUTH_PAYLOAD_OFFSET,
                got: buf.len(),
            });
        }
        if buf[0] != PROTOCOL_VERSION {
            return Err(ProtocolError::UnsupportedVersion(buf[0]));
        }
        if buf[1] != MsgType::Auth as u8 {
            return Err(ProtocolError::Parse(format!(
                "expected Auth (0x06), got 0x{:02x}",
                buf[1]
            )));
        }
        if buf[AUTH_MAGIC_OFFSET..AUTH_PEER_ID_OFFSET] != MAGIC {
            return Err(ProtocolError::InvalidMagic);
        }
        if buf.len() > MAX_HANDSHAKE_SIZE {
            return Err(ProtocolError::MessageTooLarge {
                len: buf.len(),
                cap: MAX_HANDSHAKE_SIZE,
            });
        }
        let mut id = [0u8; PEER_ID_SIZE];
        id.copy_from_slice(&buf[AUTH_PEER_ID_OFFSET..AUTH_PAYLOAD_OFFSET]);
        let payload = Bytes::copy_from_slice(&buf[AUTH_PAYLOAD_OFFSET..]);
        Ok(Self {
            peer_id: PeerId::from_bytes(id),
            payload,
        })
    }
}

/// AuthResponse — server → client reply with the server's instance URL.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AuthResponse {
    /// Server's instance URL (e.g. `rels://relay-rlb.legacy.example:33073`)
    /// — UTF-8 string, total message ≤ 8192 bytes.
    pub address: String,
}

impl AuthResponse {
    /// Construct an AuthResponse for marshaling.
    pub fn new(address: String) -> Result<Self> {
        if HEADER_SIZE + address.len() > MAX_HANDSHAKE_RESP_SIZE {
            return Err(ProtocolError::MessageTooLarge {
                len: HEADER_SIZE + address.len(),
                cap: MAX_HANDSHAKE_RESP_SIZE,
            });
        }
        Ok(Self { address })
    }

    /// Total wire-format byte length.
    #[must_use]
    pub fn wire_len(&self) -> usize {
        HEADER_SIZE + self.address.len()
    }

    /// Serialize to bytes.
    #[must_use]
    pub fn marshal(&self) -> Vec<u8> {
        let mut out = Vec::with_capacity(self.wire_len());
        out.push(PROTOCOL_VERSION);
        out.push(MsgType::AuthResponse as u8);
        out.extend_from_slice(self.address.as_bytes());
        out
    }

    /// Parse an AuthResponse.
    pub fn parse(buf: &[u8]) -> Result<Self> {
        if buf.len() < HEADER_SIZE {
            return Err(ProtocolError::ShortMessage {
                need: HEADER_SIZE,
                got: buf.len(),
            });
        }
        if buf[0] != PROTOCOL_VERSION {
            return Err(ProtocolError::UnsupportedVersion(buf[0]));
        }
        if buf[1] != MsgType::AuthResponse as u8 {
            return Err(ProtocolError::Parse(format!(
                "expected AuthResponse (0x07), got 0x{:02x}",
                buf[1]
            )));
        }
        if buf.len() > MAX_HANDSHAKE_RESP_SIZE {
            return Err(ProtocolError::MessageTooLarge {
                len: buf.len(),
                cap: MAX_HANDSHAKE_RESP_SIZE,
            });
        }
        let address = core::str::from_utf8(&buf[HEADER_SIZE..])
            .map_err(|e| ProtocolError::Parse(format!("address not valid UTF-8: {e}")))?
            .to_owned();
        Ok(Self { address })
    }
}

/// Hello — DEPRECATED client → server initial message. Same body shape as Auth.
///
/// Implemented for parity with older daemons that may still send Hello.
/// New code emits [`Auth`].
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Hello {
    /// The client's hashed peer ID.
    pub peer_id: PeerId,
    /// Opaque auth credential / additions (upstream calls this `additions`).
    pub additions: Bytes,
}

impl Hello {
    /// Serialize to bytes.
    #[must_use]
    pub fn marshal(&self) -> Vec<u8> {
        let mut out = Vec::with_capacity(AUTH_PAYLOAD_OFFSET + self.additions.len());
        out.push(PROTOCOL_VERSION);
        out.push(MsgType::Hello as u8);
        out.extend_from_slice(&MAGIC);
        out.extend_from_slice(self.peer_id.as_bytes());
        out.extend_from_slice(&self.additions);
        out
    }

    /// Parse a Hello message.
    pub fn parse(buf: &[u8]) -> Result<Self> {
        if buf.len() < AUTH_PAYLOAD_OFFSET {
            return Err(ProtocolError::ShortMessage {
                need: AUTH_PAYLOAD_OFFSET,
                got: buf.len(),
            });
        }
        if buf[0] != PROTOCOL_VERSION {
            return Err(ProtocolError::UnsupportedVersion(buf[0]));
        }
        if buf[1] != MsgType::Hello as u8 {
            return Err(ProtocolError::Parse(format!(
                "expected Hello (0x01), got 0x{:02x}",
                buf[1]
            )));
        }
        if buf[AUTH_MAGIC_OFFSET..AUTH_PEER_ID_OFFSET] != MAGIC {
            return Err(ProtocolError::InvalidMagic);
        }
        let mut id = [0u8; PEER_ID_SIZE];
        id.copy_from_slice(&buf[AUTH_PEER_ID_OFFSET..AUTH_PAYLOAD_OFFSET]);
        Ok(Self {
            peer_id: PeerId::from_bytes(id),
            additions: Bytes::copy_from_slice(&buf[AUTH_PAYLOAD_OFFSET..]),
        })
    }
}

/// HelloResponse — DEPRECATED server → client reply. Same body shape as
/// AuthResponse but no UTF-8 enforcement (upstream returns raw bytes).
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct HelloResponse {
    /// Additional data — opaque bytes (upstream does not constrain encoding).
    pub additional_data: Bytes,
}

impl HelloResponse {
    /// Serialize to bytes.
    #[must_use]
    pub fn marshal(&self) -> Vec<u8> {
        let mut out = Vec::with_capacity(HEADER_SIZE + self.additional_data.len());
        out.push(PROTOCOL_VERSION);
        out.push(MsgType::HelloResponse as u8);
        out.extend_from_slice(&self.additional_data);
        out
    }

    /// Parse a HelloResponse.
    pub fn parse(buf: &[u8]) -> Result<Self> {
        if buf.len() < HEADER_SIZE {
            return Err(ProtocolError::ShortMessage {
                need: HEADER_SIZE,
                got: buf.len(),
            });
        }
        if buf[0] != PROTOCOL_VERSION {
            return Err(ProtocolError::UnsupportedVersion(buf[0]));
        }
        if buf[1] != MsgType::HelloResponse as u8 {
            return Err(ProtocolError::Parse(format!(
                "expected HelloResponse (0x02), got 0x{:02x}",
                buf[1]
            )));
        }
        Ok(Self {
            additional_data: Bytes::copy_from_slice(&buf[HEADER_SIZE..]),
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn peer(byte: u8) -> PeerId {
        let mut b = [0u8; PEER_ID_SIZE];
        b[..4].copy_from_slice(b"sha-");
        b[4..].fill(byte);
        PeerId::from_bytes(b)
    }

    #[test]
    fn auth_round_trip_with_payload() {
        let p = peer(0x42);
        let msg = Auth::new(p, Bytes::from_static(b"hmac-token-bytes")).unwrap();
        let bytes = msg.marshal();
        assert_eq!(bytes[0], 0x01);
        assert_eq!(bytes[1], 0x06);
        assert_eq!(&bytes[2..6], &MAGIC);
        assert_eq!(&bytes[6..42], p.as_bytes());
        assert_eq!(&bytes[42..], b"hmac-token-bytes");
        assert_eq!(Auth::parse(&bytes).unwrap(), msg);
    }

    #[test]
    fn auth_max_payload_round_trip() {
        let p = peer(0x33);
        let msg = Auth::new(p, Bytes::from(vec![0x77; MAX_AUTH_PAYLOAD])).unwrap();
        let bytes = msg.marshal();
        assert_eq!(bytes.len(), MAX_HANDSHAKE_SIZE);
        assert_eq!(Auth::parse(&bytes).unwrap(), msg);
    }

    #[test]
    fn auth_rejects_oversize_payload() {
        let p = peer(0);
        assert!(matches!(
            Auth::new(p, Bytes::from(vec![0; MAX_AUTH_PAYLOAD + 1])),
            Err(ProtocolError::MessageTooLarge { .. })
        ));
    }

    #[test]
    fn auth_rejects_bad_magic() {
        let p = peer(0x11);
        let mut bytes = Auth::new(p, Bytes::new()).unwrap().marshal();
        bytes[2] = 0x00; // corrupt magic
        assert!(matches!(Auth::parse(&bytes), Err(ProtocolError::InvalidMagic)));
    }

    #[test]
    fn auth_response_round_trip() {
        let url = "rels://relay-rlb.legacy.example:33073".to_owned();
        let msg = AuthResponse::new(url.clone()).unwrap();
        let bytes = msg.marshal();
        assert_eq!(bytes[0], 0x01);
        assert_eq!(bytes[1], 0x07);
        assert_eq!(&bytes[2..], url.as_bytes());
        assert_eq!(AuthResponse::parse(&bytes).unwrap(), msg);
    }

    #[test]
    fn auth_response_rejects_oversize_address() {
        let huge = "x".repeat(MAX_AUTH_RESPONSE_ADDRESS + 1);
        assert!(matches!(
            AuthResponse::new(huge),
            Err(ProtocolError::MessageTooLarge { .. })
        ));
    }

    #[test]
    fn auth_response_rejects_invalid_utf8() {
        let mut bytes = vec![PROTOCOL_VERSION, MsgType::AuthResponse as u8];
        bytes.extend_from_slice(&[0xFF, 0xFE, 0xFD]); // invalid UTF-8
        assert!(matches!(AuthResponse::parse(&bytes), Err(ProtocolError::Parse(_))));
    }

    #[test]
    fn hello_round_trip() {
        let p = peer(0x99);
        let msg = Hello {
            peer_id: p,
            additions: Bytes::from_static(b"legacy-additions"),
        };
        let bytes = msg.marshal();
        assert_eq!(bytes[0], 0x01);
        assert_eq!(bytes[1], 0x01);
        assert_eq!(Hello::parse(&bytes).unwrap(), msg);
    }

    #[test]
    fn hello_response_round_trip() {
        let msg = HelloResponse {
            additional_data: Bytes::from_static(b"server-data"),
        };
        let bytes = msg.marshal();
        assert_eq!(bytes[0], 0x01);
        assert_eq!(bytes[1], 0x02);
        assert_eq!(HelloResponse::parse(&bytes).unwrap(), msg);
    }

    use proptest::prelude::*;

    proptest! {
        #[test]
        fn proptest_auth_round_trip(
            id_byte in 0u8..=255u8,
            payload in proptest::collection::vec(any::<u8>(), 0..=MAX_AUTH_PAYLOAD),
        ) {
            let msg = Auth::new(peer(id_byte), Bytes::from(payload.clone())).unwrap();
            let bytes = msg.marshal();
            let parsed = Auth::parse(&bytes).unwrap();
            prop_assert_eq!(parsed.peer_id, peer(id_byte));
            prop_assert_eq!(parsed.payload.as_ref(), payload.as_slice());
        }

        #[test]
        fn proptest_auth_response_round_trip(
            address in "[a-zA-Z0-9://._\\-]{0,256}",
        ) {
            let msg = AuthResponse::new(address.clone()).unwrap();
            let bytes = msg.marshal();
            let parsed = AuthResponse::parse(&bytes).unwrap();
            prop_assert_eq!(parsed.address, address);
        }
    }
}
