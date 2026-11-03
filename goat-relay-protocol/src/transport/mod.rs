//! Transport-layer adapters: WebSocket (TCP) and QUIC (UDP).
//!
//! Spec sections: §1 (WebSocket) + §2 (QUIC) of
//! `docs/design/netbird-relay-wire-protocol.md`.
//!
//! This module is **transport-shape only** — pure type definitions for frames
//! at the WebSocket / QUIC layer, with no I/O. The actual async I/O loop lives
//! in the consuming binary crate (Block 33B work — runtime decision between
//! glommio and tokio-uring).
//!
//! Submodule layout:
//!
//! - [`ws`] — §1 WebSocket framing + handshake-shape types.
//! - [`quic`] — §2 QUIC stream framing + ALPN + connection-shape types.

pub mod quic;
pub mod ws;
