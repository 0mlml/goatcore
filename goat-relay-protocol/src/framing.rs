//! Top-level frame envelope — dispatches by msg_type to the right body parser.
//!
//! Spec section: §3 of `docs/design/netbird-relay-wire-protocol.md`.
//!
//! [`Frame::parse`] reads the 2-byte common header, identifies the message
//! type, and delegates to the appropriate body parser in
//! [`crate::messages`]. [`Frame::marshal`] does the inverse — typed message
//! → wire bytes.
//!
//! This is the entry point most callers will use; the per-message structs
//! in [`crate::messages`] are still public for callers that already know
//! the type they are parsing (e.g. the auth handshake explicitly expects
//! [`Auth`] / [`AuthResponse`]).

use crate::error::{ProtocolError, Result};
use crate::messages::{
    auth::{Auth, AuthResponse, Hello, HelloResponse},
    close::{marshal_close, marshal_health_check, parse_close, parse_health_check},
    payload::Transport,
    peer_state::{marshal_peer_list, parse_peer_list, PeerListKind},
    MsgType, PeerId, HEADER_SIZE,
};

/// A parsed relay-protocol message — one variant per active [`MsgType`].
#[derive(Debug, Clone, PartialEq, Eq)]
#[non_exhaustive]
pub enum Frame {
    /// §3.1 — client → server initial auth handshake.
    Auth(Auth),
    /// §3.1.1 — server → client auth-success reply.
    AuthResponse(AuthResponse),
    /// §3.1.2 — DEPRECATED client → server initial message.
    Hello(Hello),
    /// §3.1.2 — DEPRECATED server → client reply.
    HelloResponse(HelloResponse),
    /// §3.3 — bidirectional data-plane payload (the hot path).
    Transport(Transport),
    /// §3.4 — graceful close.
    Close,
    /// §3.5 — health check ping (server) or pong (client).
    HealthCheck,
    /// §3.2 — peer-state subscribe / unsubscribe / online / offline.
    PeerList {
        /// Which of the four list-shaped messages this is.
        kind: PeerListKind,
        /// The peer-IDs carried by this message.
        peers: Vec<PeerId>,
    },
}

impl Frame {
    /// Parse a complete wire-format message from a byte buffer.
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
            MsgType::AuthResponse => Ok(Self::AuthResponse(AuthResponse::parse(buf)?)),
            MsgType::Hello => Ok(Self::Hello(Hello::parse(buf)?)),
            MsgType::HelloResponse => Ok(Self::HelloResponse(HelloResponse::parse(buf)?)),
            MsgType::Transport => Ok(Self::Transport(Transport::parse(buf)?)),
            MsgType::Close => {
                parse_close(buf)?;
                Ok(Self::Close)
            }
            MsgType::HealthCheck => {
                parse_health_check(buf)?;
                Ok(Self::HealthCheck)
            }
            MsgType::SubscribePeerState
            | MsgType::UnsubscribePeerState
            | MsgType::PeersOnline
            | MsgType::PeersWentOffline => {
                let (kind, peers) = parse_peer_list(buf)?;
                Ok(Self::PeerList { kind, peers })
            }
            MsgType::Unknown => Err(ProtocolError::UnknownMessageType(0x00)),
        }
    }

    /// Serialize this frame to wire bytes.
    pub fn marshal(&self) -> Result<Vec<u8>> {
        Ok(match self {
            Self::Auth(m) => m.marshal(),
            Self::AuthResponse(m) => m.marshal(),
            Self::Hello(m) => m.marshal(),
            Self::HelloResponse(m) => m.marshal(),
            Self::Transport(m) => m.marshal(),
            Self::Close => marshal_close().to_vec(),
            Self::HealthCheck => marshal_health_check().to_vec(),
            Self::PeerList { kind, peers } => marshal_peer_list(*kind, peers)?,
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::messages::PEER_ID_SIZE;
    use bytes::Bytes;

    fn peer(byte: u8) -> PeerId {
        let mut b = [0u8; PEER_ID_SIZE];
        b[..4].copy_from_slice(b"sha-");
        b[4..].fill(byte);
        PeerId::from_bytes(b)
    }

    #[test]
    fn frame_dispatches_close() {
        let bytes = [0x01, 0x04];
        assert_eq!(Frame::parse(&bytes).unwrap(), Frame::Close);
    }

    #[test]
    fn frame_dispatches_health_check() {
        let bytes = [0x01, 0x05];
        assert_eq!(Frame::parse(&bytes).unwrap(), Frame::HealthCheck);
    }

    #[test]
    fn frame_dispatches_transport() {
        let p = peer(0x42);
        let msg = Transport::new(p, Bytes::from_static(&[1, 2, 3])).unwrap();
        let bytes = msg.marshal();
        let frame = Frame::parse(&bytes).unwrap();
        assert_eq!(frame, Frame::Transport(msg));
    }

    #[test]
    fn frame_dispatches_auth() {
        let p = peer(0x99);
        let msg = Auth::new(p, Bytes::from_static(b"token")).unwrap();
        let bytes = msg.marshal();
        let frame = Frame::parse(&bytes).unwrap();
        assert_eq!(frame, Frame::Auth(msg));
    }

    #[test]
    fn frame_dispatches_peer_list() {
        let p = peer(0x33);
        let bytes = marshal_peer_list(PeerListKind::Online, &[p]).unwrap();
        let frame = Frame::parse(&bytes).unwrap();
        assert_eq!(
            frame,
            Frame::PeerList {
                kind: PeerListKind::Online,
                peers: vec![p],
            }
        );
    }

    #[test]
    fn frame_marshal_round_trip_for_all_active_variants() {
        let p = peer(0x55);

        let frames = [
            Frame::Close,
            Frame::HealthCheck,
            Frame::Transport(Transport::new(p, Bytes::from_static(&[0xAA])).unwrap()),
            Frame::Auth(Auth::new(p, Bytes::from_static(b"t")).unwrap()),
            Frame::AuthResponse(AuthResponse::new("rels://example:33073".to_owned()).unwrap()),
            Frame::Hello(Hello {
                peer_id: p,
                additions: Bytes::from_static(b"x"),
            }),
            Frame::HelloResponse(HelloResponse {
                additional_data: Bytes::from_static(b"y"),
            }),
            Frame::PeerList {
                kind: PeerListKind::Subscribe,
                peers: vec![p, peer(0x66)],
            },
        ];

        for f in &frames {
            let bytes = f.marshal().unwrap();
            let parsed = Frame::parse(&bytes).unwrap();
            assert_eq!(parsed, *f, "mismatch for {f:?}");
        }
    }

    #[test]
    fn frame_rejects_empty_buffer() {
        assert!(matches!(
            Frame::parse(&[]),
            Err(ProtocolError::ShortMessage { .. })
        ));
    }

    #[test]
    fn frame_rejects_unknown_msg_type() {
        assert!(matches!(
            Frame::parse(&[0x01, 0xFF]),
            Err(ProtocolError::UnknownMessageType(0xFF))
        ));
    }
}
