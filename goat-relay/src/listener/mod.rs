//! Connection listeners for the goat-relay binary.
//!
//! Per ADR 0709 D7, D9, D10:
//!
//! - [`ws`] — WS-over-TLS listener (TCP + rustls + tokio-tungstenite).
//! - [`quic`] — QUIC listener with RFC 9221 datagrams (UDP + quinn).
//!
//! Both listeners share the per-shard [`crate::routing::RoutingTable`]
//! so a peer connected via either transport is reachable from peers
//! connected via the other.
//!
//! All accept-loop spawning uses `tokio::task::spawn_local` (the LocalSet-
//! aware spawn API) — `tokio::spawn` and `tokio::task::spawn` are
//! forbidden by clippy lint per D7 layer 3.

pub mod quic;
pub mod ws;
pub mod ws_ktls;

/// tokio-uring compatibility shim for AsyncRead+AsyncWrite over an
/// io_uring-backed TcpStream. Linux-only per ADR 0711 (Block 33C.1
/// lever L5).
#[cfg(target_os = "linux")]
pub mod uring_compat;

/// SO_REUSEPORT_CBPF source-IP affinity steering. Linux-only.
/// Block 33C.1 lever L8.b — eliminates cross-shard MPSC sync for
/// steady-state peer traffic by pinning each peer's connection to a
/// single shard via `src_ip % num_sockets`.
#[cfg(target_os = "linux")]
pub mod cbpf;
