//! Transport — the data-plane hot path message type.
//!
//! Spec section: §3.3 of `docs/design/netbird-relay-wire-protocol.md`.
//!
//! Wire layout:
//!
//! ```text
//! offset  size  field
//!   0      1    version (0x01)
//!   1      1    msg_type (0x03)
//!   2     36    peer_id     // destination (c→s) OR source (s→c)
//!  38      ?    payload     // opaque, varlen, ≤8782 bytes
//! ```
//!
//! On client → server: peer_id is the **destination**.
//! On server → client: peer_id is the **source** (rewritten in-place by
//! the relay; see spec §3.3 "dual meaning" note).

use super::{MsgType, PeerId, HEADER_SIZE, MAX_MESSAGE_SIZE, PEER_ID_SIZE, PROTOCOL_VERSION};
use crate::error::{ProtocolError, Result};
use bytes::Bytes;

/// Byte offset of the peer_id field within a Transport message.
pub const TRANSPORT_PEER_ID_OFFSET: usize = HEADER_SIZE;

/// Byte offset of the payload within a Transport message.
pub const TRANSPORT_PAYLOAD_OFFSET: usize = HEADER_SIZE + PEER_ID_SIZE;

/// Maximum payload size for a Transport message after subtracting header
/// (2 bytes) + peer_id (36 bytes) from [`super::MAX_MESSAGE_SIZE`].
pub const MAX_TRANSPORT_PAYLOAD: usize = MAX_MESSAGE_SIZE - TRANSPORT_PAYLOAD_OFFSET;

/// A typed Transport message.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Transport {
    /// Destination peer ID (on c→s) or source peer ID (on s→c).
    pub peer_id: PeerId,
    /// Opaque application payload (typically an inner WireGuard packet).
    pub payload: Bytes,
}

impl Transport {
    /// Construct a Transport for marshaling.
    ///
    /// Returns [`ProtocolError::MessageTooLarge`] if the resulting wire
    /// message would exceed [`super::MAX_MESSAGE_SIZE`].
    pub fn new(peer_id: PeerId, payload: Bytes) -> Result<Self> {
        if payload.len() > MAX_TRANSPORT_PAYLOAD {
            return Err(ProtocolError::MessageTooLarge {
                len: TRANSPORT_PAYLOAD_OFFSET + payload.len(),
                cap: MAX_MESSAGE_SIZE,
            });
        }
        Ok(Self { peer_id, payload })
    }

    /// Total wire-format byte length of this Transport message.
    #[must_use]
    pub fn wire_len(&self) -> usize {
        TRANSPORT_PAYLOAD_OFFSET + self.payload.len()
    }

    /// Serialize to bytes (allocating).
    #[must_use]
    pub fn marshal(&self) -> Vec<u8> {
        let mut out = Vec::with_capacity(self.wire_len());
        out.push(PROTOCOL_VERSION);
        out.push(MsgType::Transport as u8);
        out.extend_from_slice(self.peer_id.as_bytes());
        out.extend_from_slice(&self.payload);
        out
    }

    /// Parse a Transport message from a byte buffer.
    pub fn parse(buf: &[u8]) -> Result<Self> {
        if buf.len() < TRANSPORT_PAYLOAD_OFFSET {
            return Err(ProtocolError::ShortMessage {
                need: TRANSPORT_PAYLOAD_OFFSET,
                got: buf.len(),
            });
        }
        if buf[0] != PROTOCOL_VERSION {
            return Err(ProtocolError::UnsupportedVersion(buf[0]));
        }
        if buf[1] != MsgType::Transport as u8 {
            return Err(ProtocolError::Parse(format!(
                "expected Transport (0x03), got 0x{:02x}",
                buf[1]
            )));
        }
        if buf.len() > MAX_MESSAGE_SIZE {
            return Err(ProtocolError::MessageTooLarge {
                len: buf.len(),
                cap: MAX_MESSAGE_SIZE,
            });
        }
        let mut id_bytes = [0u8; PEER_ID_SIZE];
        id_bytes.copy_from_slice(&buf[TRANSPORT_PEER_ID_OFFSET..TRANSPORT_PAYLOAD_OFFSET]);
        let peer_id = PeerId::from_bytes(id_bytes);
        let payload = Bytes::copy_from_slice(&buf[TRANSPORT_PAYLOAD_OFFSET..]);
        Ok(Self { peer_id, payload })
    }
}

/// In-place peer_id rewrite — upstream's zero-copy forwarding trick.
///
/// Mirrors `UpdateTransportMsg` (`shared/relay/messages/message.go:324`).
/// The relay reuses the buffer it received from the source peer, only
/// overwriting the 36-byte peer_id field (so on server→client the field
/// holds the SOURCE peer ID, not the destination), and forwards the
/// resulting bytes directly to the destination peer's connection.
pub fn rewrite_peer_id(buf: &mut [u8], new_peer_id: &PeerId) -> Result<()> {
    if buf.len() < TRANSPORT_PAYLOAD_OFFSET {
        return Err(ProtocolError::ShortMessage {
            need: TRANSPORT_PAYLOAD_OFFSET,
            got: buf.len(),
        });
    }
    buf[TRANSPORT_PEER_ID_OFFSET..TRANSPORT_PAYLOAD_OFFSET]
        .copy_from_slice(new_peer_id.as_bytes());
    Ok(())
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
    fn transport_round_trip() {
        let p = peer(0x42);
        let msg = Transport::new(p, Bytes::from_static(&[0xAA, 0xBB, 0xCC])).unwrap();
        let bytes = msg.marshal();
        assert_eq!(bytes[0], 0x01);
        assert_eq!(bytes[1], 0x03);
        assert_eq!(&bytes[2..38], p.as_bytes());
        assert_eq!(&bytes[38..], &[0xAA, 0xBB, 0xCC]);
        assert_eq!(Transport::parse(&bytes).unwrap(), msg);
    }

    #[test]
    fn transport_empty_payload_round_trip() {
        let p = peer(0xFF);
        let msg = Transport::new(p, Bytes::new()).unwrap();
        let bytes = msg.marshal();
        assert_eq!(bytes.len(), TRANSPORT_PAYLOAD_OFFSET);
        assert_eq!(Transport::parse(&bytes).unwrap(), msg);
    }

    #[test]
    fn transport_max_payload_round_trip() {
        let p = peer(0x01);
        let payload = Bytes::from(vec![0x55; MAX_TRANSPORT_PAYLOAD]);
        let msg = Transport::new(p, payload).unwrap();
        let bytes = msg.marshal();
        assert_eq!(bytes.len(), MAX_MESSAGE_SIZE);
        assert_eq!(Transport::parse(&bytes).unwrap(), msg);
    }

    #[test]
    fn transport_rejects_oversize_payload_at_construction() {
        let p = peer(0x00);
        let oversize = Bytes::from(vec![0; MAX_TRANSPORT_PAYLOAD + 1]);
        assert!(matches!(
            Transport::new(p, oversize),
            Err(ProtocolError::MessageTooLarge { .. })
        ));
    }

    #[test]
    fn parse_rejects_short_buffer() {
        assert!(matches!(
            Transport::parse(&[0x01, 0x03, 0x00]),
            Err(ProtocolError::ShortMessage { .. })
        ));
    }

    #[test]
    fn parse_rejects_oversize_buffer() {
        let oversize = vec![0u8; MAX_MESSAGE_SIZE + 1];
        let mut buf = oversize;
        buf[0] = PROTOCOL_VERSION;
        buf[1] = MsgType::Transport as u8;
        assert!(matches!(
            Transport::parse(&buf),
            Err(ProtocolError::MessageTooLarge { .. })
        ));
    }

    #[test]
    fn rewrite_peer_id_in_place() {
        let p1 = peer(0x11);
        let p2 = peer(0x22);
        let msg = Transport::new(p1, Bytes::from_static(&[0xDE, 0xAD])).unwrap();
        let mut bytes = msg.marshal();
        rewrite_peer_id(&mut bytes, &p2).unwrap();
        let parsed = Transport::parse(&bytes).unwrap();
        assert_eq!(parsed.peer_id, p2);
        assert_eq!(parsed.payload.as_ref(), &[0xDE, 0xAD]);
    }

    use proptest::prelude::*;

    proptest! {
        #[test]
        fn proptest_transport_round_trip(
            id_byte in 0u8..=255u8,
            payload in proptest::collection::vec(any::<u8>(), 0..=MAX_TRANSPORT_PAYLOAD),
        ) {
            let p = peer(id_byte);
            let msg = Transport::new(p, Bytes::from(payload.clone())).unwrap();
            let bytes = msg.marshal();
            let parsed = Transport::parse(&bytes).unwrap();
            prop_assert_eq!(parsed.peer_id, p);
            prop_assert_eq!(parsed.payload.as_ref(), payload.as_slice());
        }

        #[test]
        fn proptest_rewrite_preserves_payload(
            id1 in 0u8..=255u8,
            id2 in 0u8..=255u8,
            payload in proptest::collection::vec(any::<u8>(), 0..=1500),
        ) {
            let msg = Transport::new(peer(id1), Bytes::from(payload.clone())).unwrap();
            let mut bytes = msg.marshal();
            rewrite_peer_id(&mut bytes, &peer(id2)).unwrap();
            let parsed = Transport::parse(&bytes).unwrap();
            prop_assert_eq!(parsed.peer_id, peer(id2));
            prop_assert_eq!(parsed.payload.as_ref(), payload.as_slice());
        }
    }
}
