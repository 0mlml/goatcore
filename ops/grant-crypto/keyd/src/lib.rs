//! grant-keyd — the E1 key-distribution service (design §3, M2.1).
//!
//! The pure state machine and assembly pipeline live in `grant_crypto::service`,
//! with the two integration points left as traits so the logic is unit-tested in
//! isolation. This crate supplies the concrete impls that carry the heavy deps:
//!
//! - [`publisher::ZenohBundlePublisher`] — the native-Zenoh `BundlePublisher`: it
//!   opens a real session and PUTs the signed bundle on `keys/<rs>/<gen>`. Proven
//!   end-to-end over a live loopback router in `tests/transport.rs` (the Rust
//!   analogue of the Go+Python E0 proof, now on the production transport path).
//! - [`directory::DataRolesClient`] — the portal role/member resolver client
//!   (`GET /api/access/data-roles`). The audience's *identity* half; it returns
//!   ids/producers, deliberately no key material.
//!
//! - [`datapath`] — the producer + consumer libraries (Piece A): a producer that is a
//!   member of its own release-set seals a live stream under the DEK it unwraps from the
//!   bundle, and a consumer that admits bundles and opens the stream — an outsider is
//!   denied by the crypto. The visible capability a data owner and a grantee actually use.
//!   As of the Phase-1 SDK hardening (ADR 1106) this module lives in the SDK core
//!   (`grant_crypto::datapath`) — with the typed `Context`, held-DEK zeroization, and the
//!   enforced seal cap — and is re-exported here so keyd's call sites and examples are
//!   unchanged in path.
//!
//! Still to come (next increments): release-set member enumeration from grant rows (the
//! grant-row `AudienceResolver` swapping in for `resolver::ConfiguredAudience`), and the
//! fabric distribution of the member-key directory (the F-267 durable half).

pub mod config;
pub mod daemon;
/// The producer + consumer data-path, hoisted into the SDK core (ADR 1106); re-exported so
/// `grant_keyd::datapath::…` paths keep resolving.
pub use grant_crypto::datapath;
pub mod directory;
/// The Prometheus `/metrics` + `/healthz` surface (dependency-free, std TCP).
pub mod metrics;
pub mod portal;
#[cfg(feature = "publisher")]
pub mod publisher;
pub mod recipient;
pub mod resolver;
pub mod signer;
pub mod svid;
