//! Typed relay message bodies + shared protocol primitives.
//!
//! Spec section: §3 of `docs/design/netbird-relay-wire-protocol.md`.
//!
//! This module defines:
//!
//! - Protocol-wide constants ([`PROTOCOL_VERSION`], [`MAGIC`], size limits)
//! - The 36-byte [`PeerId`] type
//! - The single-byte [`MsgType`] enum with explicit discriminants
//!
//! Submodule layout:
//!
//! - [`auth`] — §3.1 auth handshake (Auth + AuthResponse + deprecated Hello/HelloResponse)
//! - [`peer_state`] — §3.2 subscribe / unsubscribe / online / offline
//! - [`payload`] — §3.3 Transport (data-plane hot path)
//! - [`close`] — §3.4 Close + §3.5 HealthCheck (header-only types)

use crate::error::{ProtocolError, Result};

pub mod auth;
pub mod close;
pub mod payload;
pub mod peer_state;

/// Wire-protocol version. Defined in upstream as `CurrentProtocolVersion = 1`
/// (`shared/relay/messages/message.go`).
pub const PROTOCOL_VERSION: u8 = 0x01;

/// 4-byte sentinel used in Auth + Hello message bodies — the STUN magic
/// cookie value (RFC 5389 §6) reused by upstream.
pub const MAGIC: [u8; 4] = [0x21, 0x12, 0xA4, 0x42];

/// Common 2-byte header (version + msg_type) that prefixes every message.
pub const HEADER_SIZE: usize = 2;

/// Fixed 36-byte PeerID size (4-byte "sha-" prefix + 32-byte SHA-256 digest).
pub const PEER_ID_SIZE: usize = 36;

/// Maximum total bytes for an [`auth::Auth`] message (`MaxHandshakeSize`).
pub const MAX_HANDSHAKE_SIZE: usize = 212;

/// Maximum total bytes for an [`auth::AuthResponse`] message
/// (`MaxHandshakeRespSize`).
pub const MAX_HANDSHAKE_RESP_SIZE: usize = 8192;

/// Maximum total bytes for any single message including header
/// (`MaxMessageSize`). [`payload::Transport`] payloads must respect this
/// minus 38 bytes of header+peer_id overhead.
pub const MAX_MESSAGE_SIZE: usize = 8820;

/// 36-byte hashed peer identifier as carried on the wire.
///
/// On-wire layout: 4-byte ASCII prefix `"sha-"` followed by a 32-byte SHA-256
/// digest of the peer's plaintext enrollment ID. The relay never sees
/// plaintext peer IDs; only the hashed form is the routing key.
///
/// `Hash` is hand-implemented (not derived) per Block 33C.1 lever L7:
/// the SHA-256-derived portion of the PeerId is already uniformly random,
/// so the first 8 bytes serve as a perfect-distribution `u64` hash. The
/// derived `Hash` would walk all 36 bytes per lookup; this implementation
/// writes 8 bytes directly to the hasher state. Saves ~28 bytes of hash
/// work per RoutingTable lookup → ~4× faster hash + better L1 cache
/// locality (one cache line vs two). Compatible with all `BuildHasher`s.
#[derive(Clone, Copy, PartialEq, Eq)]
pub struct PeerId(pub [u8; PEER_ID_SIZE]);

impl core::hash::Hash for PeerId {
    fn hash<H: core::hash::Hasher>(&self, state: &mut H) {
        // First 8 bytes of the 32-byte SHA-256 digest (offset 4..12).
        // Skipping the 4-byte "sha-" prefix because it's a constant for
        // all canonical PeerIds — including it would only add to the
        // hash collisions, not eliminate them.
        let mut buf = [0u8; 8];
        buf.copy_from_slice(&self.0[4..12]);
        state.write_u64(u64::from_le_bytes(buf));
    }
}

impl PeerId {
    /// Construct a `PeerId` from a 36-byte array.
    #[must_use]
    pub const fn from_bytes(bytes: [u8; PEER_ID_SIZE]) -> Self {
        Self(bytes)
    }

    /// Borrow the raw 36 bytes.
    #[must_use]
    pub const fn as_bytes(&self) -> &[u8; PEER_ID_SIZE] {
        &self.0
    }

    /// Prefix bytes (`"sha-"` for valid peer IDs).
    #[must_use]
    pub fn prefix(&self) -> &[u8] {
        &self.0[..4]
    }

    /// Hash bytes (the 32-byte SHA-256 digest).
    #[must_use]
    pub fn hash(&self) -> &[u8] {
        &self.0[4..]
    }

    /// Returns `true` if the peer-ID's prefix is the canonical `"sha-"`.
    #[must_use]
    pub fn has_canonical_prefix(&self) -> bool {
        self.0[..4] == *b"sha-"
    }
}

impl AsRef<[u8]> for PeerId {
    fn as_ref(&self) -> &[u8] {
        &self.0
    }
}

impl core::fmt::Debug for PeerId {
    fn fmt(&self, f: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        write!(f, "PeerId(\"{}{}\")", String::from_utf8_lossy(self.prefix()), hex(self.hash()))
    }
}

impl core::fmt::Display for PeerId {
    /// Formats as `"sha-" + base64(hash)` to match upstream `PeerID.String()`
    /// (`shared/relay/messages/id.go:21`). Without bringing in a base64 dep,
    /// the `Display` impl falls back to hex; tests that match upstream's exact
    /// string form should opt into the base64 helper at the binary crate.
    fn fmt(&self, f: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        write!(f, "{}{}", String::from_utf8_lossy(self.prefix()), hex(self.hash()))
    }
}

fn hex(bytes: &[u8]) -> String {
    let mut s = String::with_capacity(bytes.len() * 2);
    for b in bytes {
        s.push_str(&format!("{b:02x}"));
    }
    s
}

/// Single-byte message-type tag.
///
/// Discriminants and meaning per spec §3.0.3 / upstream
/// `shared/relay/messages/message.go:16-31`.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
#[repr(u8)]
pub enum MsgType {
    /// Reserved sentinel — not a valid on-wire tag.
    Unknown = 0x00,
    /// Deprecated client → server initial message; replaced by [`MsgType::Auth`].
    Hello = 0x01,
    /// Deprecated server → client response; replaced by [`MsgType::AuthResponse`].
    HelloResponse = 0x02,
    /// Bidirectional data-plane payload — the hot path.
    Transport = 0x03,
    /// Bidirectional graceful close.
    Close = 0x04,
    /// Server → client periodic ping (client must respond).
    HealthCheck = 0x05,
    /// Client → server initial auth handshake.
    Auth = 0x06,
    /// Server → client auth-success reply with the server's instance URL.
    AuthResponse = 0x07,
    /// Client → server: subscribe to peer-state updates.
    SubscribePeerState = 0x08,
    /// Client → server: unsubscribe from peer-state updates.
    UnsubscribePeerState = 0x09,
    /// Server → client: subscribed peers came online.
    PeersOnline = 0x0A,
    /// Server → client: subscribed peers went offline.
    PeersWentOffline = 0x0B,
}

impl MsgType {
    /// Returns the on-wire byte for this message type.
    #[must_use]
    pub const fn as_u8(self) -> u8 {
        self as u8
    }
}

impl TryFrom<u8> for MsgType {
    type Error = ProtocolError;

    fn try_from(value: u8) -> Result<Self> {
        Ok(match value {
            0x00 => Self::Unknown,
            0x01 => Self::Hello,
            0x02 => Self::HelloResponse,
            0x03 => Self::Transport,
            0x04 => Self::Close,
            0x05 => Self::HealthCheck,
            0x06 => Self::Auth,
            0x07 => Self::AuthResponse,
            0x08 => Self::SubscribePeerState,
            0x09 => Self::UnsubscribePeerState,
            0x0A => Self::PeersOnline,
            0x0B => Self::PeersWentOffline,
            other => return Err(ProtocolError::UnknownMessageType(other)),
        })
    }
}

/// Validate the protocol-version byte at the start of a buffer.
///
/// Returns `Ok(version)` on success or [`ProtocolError::UnsupportedVersion`] /
/// [`ProtocolError::ShortMessage`] on failure. Mirrors upstream `ValidateVersion`
/// in `message.go:96`.
pub fn validate_version(buf: &[u8]) -> Result<u8> {
    if buf.is_empty() {
        return Err(ProtocolError::ShortMessage {
            need: HEADER_SIZE,
            got: 0,
        });
    }
    let v = buf[0];
    if v != PROTOCOL_VERSION {
        return Err(ProtocolError::UnsupportedVersion(v));
    }
    Ok(v)
}

/// Read the message-type byte from buffer position 1.
///
/// Returns [`ProtocolError::ShortMessage`] if the buffer is shorter than the
/// 2-byte common header; [`ProtocolError::UnknownMessageType`] if the byte
/// does not map to any known [`MsgType`].
pub fn read_msg_type(buf: &[u8]) -> Result<MsgType> {
    if buf.len() < HEADER_SIZE {
        return Err(ProtocolError::ShortMessage {
            need: HEADER_SIZE,
            got: buf.len(),
        });
    }
    MsgType::try_from(buf[1])
}

#[cfg(test)]
mod tests {
    use super::*;

    /// Block 33C.1 lever L7 — verify the hand-rolled `Hash` impl uses
    /// only the first 8 bytes of the 32-byte SHA-256 digest. Two PeerIds
    /// matching on bytes 4..12 but differing in the tail must hash to the
    /// same `u64`. This is the deliberate trade we accept for ~4× faster
    /// hash + better L1 cache locality on every routing-table lookup.
    /// Production safety: the SHA-256 portion is uniformly random, so 8
    /// uniformly-random bytes give 2^64 distinct hash values — collision
    /// probability per pair = 2^-64 = ~negligible at any realistic peer
    /// fleet size (birthday-paradox 50% collision = ~5 billion peers).
    #[test]
    fn peer_id_hash_uses_first_8_bytes_only() {
        use std::collections::hash_map::DefaultHasher;
        use std::hash::Hasher;
        let mut a = [0u8; PEER_ID_SIZE];
        a[..4].copy_from_slice(b"sha-");
        a[4..12].copy_from_slice(b"shared-8");
        a[12..].fill(0xAA);
        let mut b = [0u8; PEER_ID_SIZE];
        b[..4].copy_from_slice(b"sha-");
        b[4..12].copy_from_slice(b"shared-8");
        b[12..].fill(0xBB); // tail differs
        let pa = PeerId::from_bytes(a);
        let pb = PeerId::from_bytes(b);
        let mut ha = DefaultHasher::new();
        let mut hb = DefaultHasher::new();
        std::hash::Hash::hash(&pa, &mut ha);
        std::hash::Hash::hash(&pb, &mut hb);
        assert_eq!(
            ha.finish(),
            hb.finish(),
            "first-8-bytes-only Hash impl: same first 8 bytes → same hash"
        );
        // Equality still uses all 36 bytes (derived PartialEq), so the
        // collision in the hash bucket is resolved by the PartialEq check.
        assert_ne!(pa, pb, "PartialEq still distinguishes by tail");
    }

    /// L7 verification: PeerIds differing in the first 8 bytes hash differently.
    #[test]
    fn peer_id_hash_differs_on_first_8_bytes() {
        use std::collections::hash_map::DefaultHasher;
        use std::hash::Hasher;
        let mut a = [0u8; PEER_ID_SIZE];
        a[..4].copy_from_slice(b"sha-");
        a[4..].fill(0xAA);
        let mut b = [0u8; PEER_ID_SIZE];
        b[..4].copy_from_slice(b"sha-");
        b[4..].fill(0xBB);
        let mut ha = DefaultHasher::new();
        let mut hb = DefaultHasher::new();
        std::hash::Hash::hash(&PeerId::from_bytes(a), &mut ha);
        std::hash::Hash::hash(&PeerId::from_bytes(b), &mut hb);
        assert_ne!(ha.finish(), hb.finish());
    }

    #[test]
    fn peer_id_prefix_and_hash_split() {
        let bytes = {
            let mut b = [0u8; PEER_ID_SIZE];
            b[..4].copy_from_slice(b"sha-");
            b[4..].copy_from_slice(&[0xAB; 32]);
            b
        };
        let id = PeerId::from_bytes(bytes);
        assert!(id.has_canonical_prefix());
        assert_eq!(id.prefix(), b"sha-");
        assert_eq!(id.hash(), &[0xAB; 32]);
    }

    #[test]
    fn msg_type_round_trip_via_u8_for_all_known_variants() {
        for v in [
            MsgType::Unknown,
            MsgType::Hello,
            MsgType::HelloResponse,
            MsgType::Transport,
            MsgType::Close,
            MsgType::HealthCheck,
            MsgType::Auth,
            MsgType::AuthResponse,
            MsgType::SubscribePeerState,
            MsgType::UnsubscribePeerState,
            MsgType::PeersOnline,
            MsgType::PeersWentOffline,
        ] {
            assert_eq!(MsgType::try_from(v.as_u8()).unwrap(), v);
        }
    }

    #[test]
    fn msg_type_unknown_byte_is_rejected() {
        for byte in [0x0C, 0x10, 0xFF] {
            assert!(matches!(
                MsgType::try_from(byte),
                Err(ProtocolError::UnknownMessageType(_))
            ));
        }
    }

    #[test]
    fn validate_version_rejects_empty_and_wrong() {
        assert!(matches!(
            validate_version(&[]),
            Err(ProtocolError::ShortMessage { .. })
        ));
        assert!(matches!(
            validate_version(&[0x02]),
            Err(ProtocolError::UnsupportedVersion(0x02))
        ));
        assert_eq!(validate_version(&[0x01]).unwrap(), 0x01);
    }
}
