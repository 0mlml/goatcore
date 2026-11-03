//! Peer-state list messages — Subscribe / Unsubscribe / PeersOnline /
//! PeersWentOffline.
//!
//! Spec section: §3.2 of `docs/design/netbird-relay-wire-protocol.md`.
//!
//! All four message types share a uniform on-wire shape — a 2-byte header
//! followed by N concatenated 36-byte PeerIDs:
//!
//! ```text
//! offset       size    field
//!   0           1      version (0x01)
//!   1           1      msg_type (0x08 / 0x09 / 0x0A / 0x0B)
//!   2          36      peer_id[0]
//!  38          36      peer_id[1]
//!   …
//! ```
//!
//! Lists with > 244 peer-IDs are chunked across multiple wire messages
//! (see [`MAX_PEERS_PER_MESSAGE`] and [`marshal_peer_list_chunked`]).
//! The empty list is rejected — upstream's `marshalPeerIDs` returns an error
//! when given no IDs.

use super::{MsgType, PeerId, HEADER_SIZE, MAX_MESSAGE_SIZE, PEER_ID_SIZE, PROTOCOL_VERSION};
use crate::error::{ProtocolError, Result};

/// Maximum peer-IDs that fit in one wire message:
/// `(MAX_MESSAGE_SIZE - HEADER_SIZE) / PEER_ID_SIZE = (8820 - 2) / 36 = 244`.
pub const MAX_PEERS_PER_MESSAGE: usize = (MAX_MESSAGE_SIZE - HEADER_SIZE) / PEER_ID_SIZE;

/// Which of the four peer-state message types a buffer represents.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum PeerListKind {
    /// Client → server: subscribe to state notifications for these peers.
    Subscribe,
    /// Client → server: unsubscribe from state notifications for these peers.
    Unsubscribe,
    /// Server → client: subscribed peers came online.
    Online,
    /// Server → client: subscribed peers went offline.
    Offline,
}

impl PeerListKind {
    /// Map to the corresponding [`MsgType`] tag.
    #[must_use]
    pub const fn msg_type(self) -> MsgType {
        match self {
            Self::Subscribe => MsgType::SubscribePeerState,
            Self::Unsubscribe => MsgType::UnsubscribePeerState,
            Self::Online => MsgType::PeersOnline,
            Self::Offline => MsgType::PeersWentOffline,
        }
    }

    /// Construct from a [`MsgType`] tag, returning `None` if the tag is not
    /// one of the four peer-state list types.
    #[must_use]
    pub const fn from_msg_type(t: MsgType) -> Option<Self> {
        match t {
            MsgType::SubscribePeerState => Some(Self::Subscribe),
            MsgType::UnsubscribePeerState => Some(Self::Unsubscribe),
            MsgType::PeersOnline => Some(Self::Online),
            MsgType::PeersWentOffline => Some(Self::Offline),
            _ => None,
        }
    }
}

/// Build a single-message peer-state list. Returns
/// [`ProtocolError::EmptyPeerList`] if `ids` is empty, or
/// [`ProtocolError::MessageTooLarge`] if the list exceeds
/// [`MAX_PEERS_PER_MESSAGE`].
///
/// Use [`marshal_peer_list_chunked`] for arbitrarily-long lists.
pub fn marshal_peer_list(kind: PeerListKind, ids: &[PeerId]) -> Result<Vec<u8>> {
    if ids.is_empty() {
        return Err(ProtocolError::EmptyPeerList);
    }
    if ids.len() > MAX_PEERS_PER_MESSAGE {
        return Err(ProtocolError::MessageTooLarge {
            len: HEADER_SIZE + ids.len() * PEER_ID_SIZE,
            cap: MAX_MESSAGE_SIZE,
        });
    }
    let mut out = Vec::with_capacity(HEADER_SIZE + ids.len() * PEER_ID_SIZE);
    out.push(PROTOCOL_VERSION);
    out.push(kind.msg_type().as_u8());
    for id in ids {
        out.extend_from_slice(id.as_bytes());
    }
    Ok(out)
}

/// Build chunked peer-state messages for arbitrarily-long lists. Mirrors
/// upstream `marshalPeerIDs` (`shared/relay/messages/peer_state.go:48`).
///
/// Each returned `Vec<u8>` is one complete wire message. Empty lists return
/// an error (matches upstream).
pub fn marshal_peer_list_chunked(
    kind: PeerListKind,
    ids: &[PeerId],
) -> Result<Vec<Vec<u8>>> {
    if ids.is_empty() {
        return Err(ProtocolError::EmptyPeerList);
    }
    let mut out = Vec::new();
    for chunk in ids.chunks(MAX_PEERS_PER_MESSAGE) {
        out.push(marshal_peer_list(kind, chunk)?);
    }
    Ok(out)
}

/// Parse a peer-state list message. Returns the list kind + the parsed peer
/// IDs (zero or more — though by upstream convention messages always carry
/// at least one).
pub fn parse_peer_list(buf: &[u8]) -> Result<(PeerListKind, Vec<PeerId>)> {
    if buf.len() < HEADER_SIZE {
        return Err(ProtocolError::ShortMessage {
            need: HEADER_SIZE,
            got: buf.len(),
        });
    }
    if buf[0] != PROTOCOL_VERSION {
        return Err(ProtocolError::UnsupportedVersion(buf[0]));
    }
    let mt = MsgType::try_from(buf[1])?;
    let kind = PeerListKind::from_msg_type(mt).ok_or_else(|| {
        ProtocolError::Parse(format!(
            "expected peer-state list (0x08/0x09/0x0A/0x0B), got 0x{:02x}",
            buf[1]
        ))
    })?;
    let body = &buf[HEADER_SIZE..];
    // `% == 0` matches MSRV 1.75; `usize::is_multiple_of` is 1.87+.
    if body.len() % PEER_ID_SIZE != 0 {
        return Err(ProtocolError::InvalidPeerListLength(body.len()));
    }
    let n = body.len() / PEER_ID_SIZE;
    let mut ids = Vec::with_capacity(n);
    for i in 0..n {
        let start = i * PEER_ID_SIZE;
        let mut id_bytes = [0u8; PEER_ID_SIZE];
        id_bytes.copy_from_slice(&body[start..start + PEER_ID_SIZE]);
        ids.push(PeerId::from_bytes(id_bytes));
    }
    Ok((kind, ids))
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
    fn round_trip_subscribe_one_peer() {
        let p = peer(0x10);
        let bytes = marshal_peer_list(PeerListKind::Subscribe, &[p]).unwrap();
        assert_eq!(bytes[0], 0x01);
        assert_eq!(bytes[1], 0x08);
        assert_eq!(bytes.len(), HEADER_SIZE + PEER_ID_SIZE);
        let (kind, ids) = parse_peer_list(&bytes).unwrap();
        assert_eq!(kind, PeerListKind::Subscribe);
        assert_eq!(ids, vec![p]);
    }

    #[test]
    fn round_trip_each_kind() {
        for kind in [
            PeerListKind::Subscribe,
            PeerListKind::Unsubscribe,
            PeerListKind::Online,
            PeerListKind::Offline,
        ] {
            let p = peer(0xCC);
            let bytes = marshal_peer_list(kind, &[p]).unwrap();
            let (parsed_kind, parsed_ids) = parse_peer_list(&bytes).unwrap();
            assert_eq!(parsed_kind, kind);
            assert_eq!(parsed_ids, vec![p]);
        }
    }

    #[test]
    fn rejects_empty_list() {
        assert!(matches!(
            marshal_peer_list(PeerListKind::Subscribe, &[]),
            Err(ProtocolError::EmptyPeerList)
        ));
    }

    #[test]
    fn rejects_oversize_list_in_single_message() {
        let ids: Vec<PeerId> = (0..=MAX_PEERS_PER_MESSAGE).map(|i| peer(i as u8)).collect();
        assert!(matches!(
            marshal_peer_list(PeerListKind::Online, &ids),
            Err(ProtocolError::MessageTooLarge { .. })
        ));
    }

    #[test]
    fn chunked_handles_overflow_list() {
        // 500 peers — should chunk to 3 messages: 244 + 244 + 12.
        let ids: Vec<PeerId> = (0..500).map(|i| peer((i % 256) as u8)).collect();
        let chunks = marshal_peer_list_chunked(PeerListKind::Online, &ids).unwrap();
        assert_eq!(chunks.len(), 3);
        // Sum of parsed peers across chunks equals input.
        let mut total = Vec::new();
        for c in &chunks {
            let (k, ids) = parse_peer_list(c).unwrap();
            assert_eq!(k, PeerListKind::Online);
            total.extend(ids);
        }
        assert_eq!(total, ids);
    }

    #[test]
    fn parse_rejects_invalid_body_length() {
        let mut bytes = vec![PROTOCOL_VERSION, MsgType::PeersOnline as u8];
        bytes.extend_from_slice(&[0u8; 35]); // not a multiple of 36
        assert!(matches!(
            parse_peer_list(&bytes),
            Err(ProtocolError::InvalidPeerListLength(35))
        ));
    }

    use proptest::prelude::*;

    proptest! {
        #[test]
        fn proptest_round_trip_peer_list(
            kind_idx in 0usize..4,
            len in 1usize..=MAX_PEERS_PER_MESSAGE,
        ) {
            let kind = [
                PeerListKind::Subscribe,
                PeerListKind::Unsubscribe,
                PeerListKind::Online,
                PeerListKind::Offline,
            ][kind_idx];
            let ids: Vec<PeerId> = (0..len).map(|i| peer((i % 256) as u8)).collect();
            let bytes = marshal_peer_list(kind, &ids).unwrap();
            let (parsed_kind, parsed_ids) = parse_peer_list(&bytes).unwrap();
            prop_assert_eq!(parsed_kind, kind);
            prop_assert_eq!(parsed_ids, ids);
        }
    }
}
