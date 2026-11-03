//! goat-relay — Netbird-relay-compatible relay server (Block 33B+).
//!
//! Library crate exposing the modules consumed by the binary entry-
//! point ([`src/main.rs`](../main/index.html)) and by the benches in
//! `benches/` (Rust binary crates cannot be imported from sibling
//! targets without a lib target; the lib target is essentially a
//! re-export).
//!
//! See [`docs/design/netbird-relay-wire-protocol.md`](../../docs/design/netbird-relay-wire-protocol.md)
//! for the wire-protocol spec, [`docs/adr/0709-block-33-relay-rewrite-architecture.md`](../../docs/adr/0709-block-33-relay-rewrite-architecture.md)
//! for the architectural decisions, and
//! [`docs/project/block-33-progress.md`](../../docs/project/block-33-progress.md)
//! for cross-session working state.

pub mod auth;
pub mod keepalive;
pub mod listener;
pub mod routing;
pub mod runtime;
pub mod shard;
pub mod stats;
