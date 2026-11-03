// SPDX-License-Identifier: Apache-2.0
//
// Presence — the liveliness key convention + construction helpers for the
// goat fabric (ADR 1040 gap 3, docs/design/zenoh-capability-adoption.md).
//
// Zenoh liveliness is the native push-based presence primitive: a peer
// declares a token ("I exist"), watchers are told the instant it drops
// (session close, crash, or connectivity loss), and late-joiners query or
// `history(true)` the current set. All of it is STABLE zenoh 1.9.0 API —
// no `unstable` feature — so goat-cli (which deliberately omits
// `unstable`) can consume this module too.
//
// Key convention:
//
//     <write_root>/presence/<label>
//
// where `write_root` is the peer's ACL write root — the UUID slot anchor
// on slot-pool goatnets (ZAGROS), the vendor prefix on per-vendor goatnets
// (nova-a), or `ops` for operator/infra services (`ops/presence/<service>`).
// Keeping the token inside the peer's own write namespace means the
// existing per-identity ACL rule covers it (with `liveliness_token` added
// to the rule's messages) — no new authz surface, mirroring how the
// volatile/ class rode the same identity boundary (ADR 1032).
//
// Tokens are NOT puts: they hit no rocksdb storage and the audit
// subscriber never sees them, so presence adds zero retention load.
//
// The roll-up view (`ops/presence/health/<site>`, EnvHealthV1
// {domain:"presence"}) is published by the standalone goat-presence-health
// meter, not from here.

use zenoh::handlers::FifoChannelHandler;
use zenoh::liveliness::LivelinessToken;
use zenoh::pubsub::Subscriber;
use zenoh::sample::Sample;
use zenoh::Session;

/// The reserved path segment that marks a liveliness token as a presence
/// declaration under a peer's write root.
pub const PRESENCE_SEGMENT: &str = "presence";

/// The key expression matching every presence token on the fabric.
/// (`**` is safe here: presence keys contain no verbatim `@` chunks.)
pub const ALL_PRESENCE_KEYEXPR: &str = "**/presence/**";

/// A parsed presence-token key.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PresenceId {
    /// The peer's ACL write root — UUID slot anchor (ZAGROS), vendor prefix
    /// (nova-a), or an `ops`-rooted service path for infra.
    pub write_root: String,
    /// The peer's human-meaningful label (display name / service name).
    pub label: String,
}

/// Render the presence-token key for a peer: `<write_root>/presence/<label>`.
pub fn presence_key(write_root: &str, label: &str) -> String {
    format!("{write_root}/{PRESENCE_SEGMENT}/{label}")
}

/// Parse a presence-token key back into its identity halves. Splits on the
/// FIRST `/presence/` segment boundary, so a multi-chunk write root
/// (`ops/foo`) and a multi-chunk label both survive the round-trip.
/// Returns `None` for keys that don't follow the convention.
pub fn parse_presence_key(key: &str) -> Option<PresenceId> {
    let chunks: Vec<&str> = key.split('/').collect();
    let idx = chunks.iter().position(|c| *c == PRESENCE_SEGMENT)?;
    if idx == 0 || idx + 1 >= chunks.len() {
        return None; // no write root, or no label
    }
    Some(PresenceId {
        write_root: chunks[..idx].join("/"),
        label: chunks[idx + 1..].join("/"),
    })
}

/// Declare a presence token for a peer. Hold the returned token for the
/// session's lifetime — dropping it (or the session) undeclares it, which
/// is exactly what tells watchers the peer is gone.
///
/// Requires the router ACL to grant `liveliness_token` on the peer's write
/// namespace; against an un-upgraded router the declaration is silently
/// denied, so callers should treat presence as best-effort
/// advisory, never load-bearing for the data path.
pub async fn declare_presence_token(
    session: &Session,
    write_root: &str,
    label: &str,
) -> zenoh::Result<LivelinessToken> {
    session
        .liveliness()
        .declare_token(presence_key(write_root, label))
        .await
}

/// Declare a presence watch: a liveliness subscriber that first replays the
/// CURRENT token set (`history(true)`) and then streams join/leave edges.
/// `SampleKind::Put` = alive, `SampleKind::Delete` = dropped. Drive it with
/// the usual `.recv_async()` loop.
pub async fn declare_presence_watch(
    session: &Session,
    key_expr: &str,
) -> zenoh::Result<Subscriber<FifoChannelHandler<Sample>>> {
    session
        .liveliness()
        .declare_subscriber(key_expr)
        .history(true)
        .await
}

/// One-shot presence query: the keys of every live token matching
/// `key_expr`, sorted. Use [`ALL_PRESENCE_KEYEXPR`] for the whole fabric.
pub async fn query_presence(
    session: &Session,
    key_expr: &str,
    timeout: std::time::Duration,
) -> zenoh::Result<Vec<String>> {
    let replies = session
        .liveliness()
        .get(key_expr)
        .timeout(timeout)
        .await?;
    let mut keys = Vec::new();
    while let Ok(reply) = replies.recv_async().await {
        if let Ok(sample) = reply.result() {
            keys.push(sample.key_expr().to_string());
        }
    }
    keys.sort();
    keys.dedup();
    Ok(keys)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn key_round_trips_for_anchor_and_vendor_roots() {
        // ZAGROS slot anchor root.
        let anchor = "e7a9c75b00112233445566778899aabb";
        let key = presence_key(anchor, "tarsier-alpha");
        assert_eq!(key, format!("{anchor}/presence/tarsier-alpha"));
        let id = parse_presence_key(&key).expect("parse");
        assert_eq!(id.write_root, anchor);
        assert_eq!(id.label, "tarsier-alpha");

        // nova vendor-prefix root.
        let id = parse_presence_key("notional/presence/kingfisher").expect("parse");
        assert_eq!(id.write_root, "notional");
        assert_eq!(id.label, "kingfisher");
    }

    #[test]
    fn key_round_trips_for_ops_infra_root() {
        let key = presence_key("ops", "fabric-health");
        assert_eq!(key, "ops/presence/fabric-health");
        let id = parse_presence_key(&key).expect("parse");
        assert_eq!(id.write_root, "ops");
        assert_eq!(id.label, "fabric-health");
    }

    #[test]
    fn parse_splits_on_first_presence_segment_only() {
        // A label that itself contains a `presence` chunk stays whole.
        let id = parse_presence_key("root/presence/a/presence/b").expect("parse");
        assert_eq!(id.write_root, "root");
        assert_eq!(id.label, "a/presence/b");
    }

    #[test]
    fn parse_rejects_non_convention_keys() {
        // No presence segment at all.
        assert_eq!(parse_presence_key("notional/kingfisher/contacts/v1"), None);
        // presence as the leading chunk — no write root.
        assert_eq!(parse_presence_key("presence/orphan"), None);
        // presence as the trailing chunk — no label.
        assert_eq!(parse_presence_key("notional/presence"), None);
    }
}
