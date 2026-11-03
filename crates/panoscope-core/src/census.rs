// SPDX-License-Identifier: Apache-2.0
//
// Pub/sub CENSUS — the who-subscribes-to-what and who-publishes-what surface,
// read from Zenoh node adminspaces. Shared by panoscope, `goat top`, and
// `goat who` so the pub/sub *dynamics* (production AND consumption) are ONE
// primitive, not a per-surface re-derivation.
//
// The census is the authoritative answer the fabric already holds: every
// Zenoh node tracks the subscriptions + publications routing through it to
// route at all, and exposes them in its adminspace at
//   @/<zid>/<whatami>/subscriber/<subscribed_keyexpr>  -> {routers,peers,clients:[zid…]}
//   @/<zid>/<whatami>/publisher/<published_keyexpr>     -> {…}
// (`whatami` = "router" for the router today; "peer" once peers route P2P).
// No self-declaration — this is ground truth, so it surfaces pure subscribers
// (audit-subscriber on `**`, non-publishing sinks) that leave no data-tail or
// schema-decl trace.
//
// P2P-READY BY CONSTRUCTION: `query_census` folds replies from every node it
// reaches into one map, so a single-router fabric (query the one router) and a
// peer mesh (query every reachable peer, union) use the same code — only the
// node set and the ACL whatami grant widen. See
// docs/design/subscriber-consumption-visibility.md §"P2P evolution".
//
// Two attribution filters:
//   - `routers` is propagation metadata (always carries the local router), NOT
//     a subscriber — only `clients` + `peers` are the real endpoints.
//   - a source ZID equal to the reply's OWNER (the node whose adminspace this
//     is) is that node's own in-process storage-manager subscription (every
//     `<uuid>/**` storage prefix) — filtered, else the router renders as a
//     ~1000-key mega-subscriber.

use std::collections::{BTreeSet, HashMap};
use std::time::Duration;

use zenoh::key_expr::KeyExpr;
use zenoh::Session;

use crate::types::{PeerId, TopicKey};

/// True if two key-expressions overlap by Zenoh's real intersection semantics
/// (handles `**`, `*`, and mid/suffix wildcards like `**/alpha-fob/**` that a
/// prefix heuristic misses — notably a bare `**` subscriber matches every
/// published key). Fail-soft: a non-parseable key-expression never intersects.
fn keyexprs_intersect(a: &str, b: &str) -> bool {
    match (KeyExpr::try_from(a), KeyExpr::try_from(b)) {
        (Ok(ka), Ok(kb)) => ka.intersects(&kb),
        _ => false,
    }
}

/// The two census dimensions a node's adminspace exposes. Queryables
/// (`@/<zid>/<whatami>/queryable/**`) are intentionally out of scope — the
/// consumption/production dynamics are pub/sub.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ResourceRole {
    Subscriber,
    Publisher,
}

/// The pub/sub census: per key-expression, the set of peer ZIDs subscribing to
/// it / publishing it. Unioned across every node queried.
#[derive(Debug, Default, Clone)]
pub struct Census {
    /// subscribed key-expression -> subscriber ZIDs.
    pub subscriptions: HashMap<TopicKey, BTreeSet<PeerId>>,
    /// published key-expression -> publisher ZIDs.
    pub publications: HashMap<TopicKey, BTreeSet<PeerId>>,
}

impl Census {
    pub fn is_empty(&self) -> bool {
        self.subscriptions.is_empty() && self.publications.is_empty()
    }

    /// Record one (role, keyexpr, subscriber/publisher zid) fact.
    pub fn insert(&mut self, role: ResourceRole, keyexpr: TopicKey, zid: PeerId) {
        let map = match role {
            ResourceRole::Subscriber => &mut self.subscriptions,
            ResourceRole::Publisher => &mut self.publications,
        };
        map.entry(keyexpr).or_default().insert(zid);
    }

    /// Subscribers declared exactly on `keyexpr` (verbatim key, not a match).
    pub fn subscribers_of(&self, keyexpr: &str) -> BTreeSet<PeerId> {
        self.subscriptions.get(keyexpr).cloned().unwrap_or_default()
    }

    /// Everything `peer` subscribes to, sorted.
    pub fn subscriptions_of(&self, peer: &str) -> Vec<TopicKey> {
        let mut v: Vec<TopicKey> = self
            .subscriptions
            .iter()
            .filter(|(_, zids)| zids.contains(peer))
            .map(|(ke, _)| ke.clone())
            .collect();
        v.sort();
        v
    }

    /// Everything `peer` publishes, sorted.
    pub fn publications_of(&self, peer: &str) -> Vec<TopicKey> {
        let mut v: Vec<TopicKey> = self
            .publications
            .iter()
            .filter(|(_, zids)| zids.contains(peer))
            .map(|(ke, _)| ke.clone())
            .collect();
        v.sort();
        v
    }

    /// Fan-out for a producer: the set of DISTINCT subscriber ZIDs whose
    /// subscription key-expression is related (wildcard-overlapping) to any
    /// key-expression this producer publishes. This is the core pub->sub
    /// dynamic — how many consumers a producer's output actually reaches —
    /// and it composes publish-side + subscribe-side census in one number.
    /// `published` is the producer's published key-expressions (e.g. from the
    /// publications map, or a footprint's declared topics).
    pub fn subscriber_fanout(&self, published: &[TopicKey]) -> BTreeSet<PeerId> {
        let mut out: BTreeSet<PeerId> = BTreeSet::new();
        for (sub_ke, zids) in &self.subscriptions {
            if published.iter().any(|p| keyexprs_intersect(p, sub_ke)) {
                out.extend(zids.iter().cloned());
            }
        }
        out
    }

    /// Total distinct subscriber ZIDs anywhere in the census (the consumer
    /// population). Cheap top-line "how many consumers on the fabric".
    pub fn distinct_subscribers(&self) -> BTreeSet<PeerId> {
        self.subscriptions.values().flatten().cloned().collect()
    }

    // ---- identity correlation -------------------------------------------
    //
    // The census keys on opaque Zenoh ZIDs; the operator surfaces (`goat top`,
    // `goat who`) key on the slot-ANCHOR (the 32-hex identity, ADR 1025/1039).
    // These are NOT equal: a peer's ZID is
    // unrelated to the anchor it publishes under. The only census-internal
    // bridge is the PUBLISH side: a peer publishes under `<anchor>/…`, so its
    // ZID maps to that anchor. Pure consumers that never publish cannot be
    // anchor-correlated from the census alone — an honest, documented gap.

    /// Map each publisher ZID to the slot-anchor it publishes under (the
    /// leading 32-hex chunk of a published key-expression). First-write-wins.
    pub fn zid_anchors(&self) -> HashMap<PeerId, String> {
        let mut m: HashMap<PeerId, String> = HashMap::new();
        for (ke, zids) in &self.publications {
            let Some(anchor) = ke.split('/').next() else { continue };
            if crate::alias::is_anchor_shaped(anchor) {
                for z in zids {
                    m.entry(z.clone()).or_insert_with(|| anchor.to_string());
                }
            }
        }
        m
    }

    /// Producer-side fan-out for an identity ANCHOR: the distinct subscriber
    /// ZIDs whose subscription intersects any topic published under
    /// `<anchor>/…`. Anchor-native — keys off the published-topic prefix, so it
    /// needs no ZID↔anchor map. This is the load-bearing number for `goat top`'s
    /// consumer column: how many consumers a producer's output reaches.
    pub fn fanout_for_anchor(&self, anchor: &str) -> BTreeSet<PeerId> {
        let published: Vec<TopicKey> = self
            .publications
            .keys()
            .filter(|ke| ke.split('/').next() == Some(anchor))
            .cloned()
            .collect();
        self.subscriber_fanout(&published)
    }

    /// The live subscriptions of an identity ANCHOR: key-expressions subscribed
    /// by any ZID that maps to `anchor` (via `zid_anchors`). Empty for a pure
    /// consumer whose ZID cannot be anchor-correlated from the census — `goat
    /// who` renders that honestly (no live subscription observed), never faked.
    pub fn subscriptions_for_anchor(&self, anchor: &str) -> Vec<TopicKey> {
        let anchors = self.zid_anchors();
        let zids: BTreeSet<&PeerId> = anchors
            .iter()
            .filter(|(_, a)| a.as_str() == anchor)
            .map(|(z, _)| z)
            .collect();
        if zids.is_empty() {
            return Vec::new();
        }
        let mut out: BTreeSet<TopicKey> = BTreeSet::new();
        for (ke, subs) in &self.subscriptions {
            if subs.iter().any(|z| zids.contains(z)) {
                out.insert(ke.clone());
            }
        }
        out.into_iter().collect()
    }
}

/// Parse a census reply key
///   `@/<owner_zid>/<whatami>/subscriber/<keyexpr>`  (or `.../publisher/<keyexpr>`)
/// into `(role, owner_zid, keyexpr)`. `<whatami>` is `router` today and `peer`
/// under peer-mode P2P; both are accepted so the parser is topology-agnostic.
/// `owner_zid` is the node whose adminspace this is — the caller filters any
/// source ZID equal to it (that node's own storage-manager subscription).
/// Returns `None` for any other admin shape (session-transport, config,
/// metrics, linkstate, queryable) or a data topic that merely contains the
/// substring.
pub fn parse_resource_key(key: &str) -> Option<(ResourceRole, PeerId, TopicKey)> {
    // Require `@/<plausible_zid>/<router|peer>/` so a data topic that literally
    // contains "/subscriber/" can't match. splitn(4) keeps the keyexpr (which
    // itself contains '/') intact as the 4th element.
    let rest = key.strip_prefix("@/")?;
    let mut segs = rest.splitn(4, '/');
    let owner_zid = segs.next()?;
    if !is_plausible_zid(owner_zid) {
        return None;
    }
    match segs.next()? {
        "router" | "peer" => {}
        _ => return None,
    }
    let role = match segs.next()? {
        "subscriber" => ResourceRole::Subscriber,
        "publisher" => ResourceRole::Publisher,
        _ => return None,
    };
    let keyexpr = segs.next()?;
    if keyexpr.is_empty() {
        return None;
    }
    Some((role, owner_zid.to_string(), keyexpr.to_string()))
}

/// Collect the real endpoint ZIDs from a `Sources { routers, peers, clients }`
/// adminspace payload (zenoh routing/hat/mod.rs). Only `clients` + `peers` —
/// `routers` is propagation metadata (always the local router), never an
/// endpoint. Deduped; implausible entries dropped.
pub fn census_sources(json: &serde_json::Value) -> Vec<PeerId> {
    let mut out: Vec<PeerId> = Vec::new();
    for field in ["clients", "peers"] {
        let Some(arr) = json.get(field).and_then(|v| v.as_array()) else {
            continue;
        };
        for v in arr {
            if let Some(s) = v.as_str() {
                if is_plausible_zid(s) && !out.iter().any(|z| z == s) {
                    out.push(s.to_string());
                }
            }
        }
    }
    out
}

/// Fold one adminspace reply (key + JSON payload) into the census, filtering
/// the reply owner (storage-manager self-subs) and `own_zid` (our own reader
/// session). Returns the number of (role, keyexpr, zid) facts recorded.
pub fn apply_reply(census: &mut Census, key: &str, payload: &serde_json::Value, own_zid: &str) -> usize {
    let Some((role, owner_zid, keyexpr)) = parse_resource_key(key) else {
        return 0;
    };
    let mut n = 0;
    for zid in census_sources(payload) {
        if zid == owner_zid || zid == own_zid {
            continue;
        }
        census.insert(role, keyexpr.clone(), zid);
        n += 1;
    }
    n
}

/// The adminspace selectors a one-shot census reads. Today: the router
/// whatami only (the deployed ACL grants `@/*/router/{subscriber,publisher}/**`
/// — least-privilege, config never exposed). Under peer-mode P2P this widens
/// to the `peer` whatami; the parser already accepts it.
pub const CENSUS_SELECTORS: [&str; 2] = [
    "@/*/router/subscriber/**",
    "@/*/router/publisher/**",
];

/// The single lazy crawler's census MIRROR (ADR 1035 §2;
/// zenoh-cardinality-budget-and-lazy-crawler.md §"Layer-4 implementation"):
/// a byte-faithful relay of the adminspace census replies, published as JSON
/// `{site, sampled_at_ms, selectors, entries: [{key, payload}]}` on
/// `ops/fabric/census/<site>`. Reading it is a storage-backed single-key GET —
/// the poll-once/fan-out contract — instead of a per-consumer adminspace walk.
pub const CENSUS_MIRROR_SELECTOR: &str = "ops/fabric/census/*";

/// Parse one census-mirror document into `census`. Returns the entry count
/// and the document's `sampled_at_ms`. Entries reuse the exact adminspace
/// parse (`apply_reply`) — the mirror is byte-faithful by contract.
fn apply_mirror_doc(census: &mut Census, doc: &serde_json::Value, own_zid: &str) -> (usize, i64) {
    let sampled_at_ms = doc.get("sampled_at_ms").and_then(|v| v.as_i64()).unwrap_or(0);
    let mut n = 0;
    if let Some(entries) = doc.get("entries").and_then(|v| v.as_array()) {
        for e in entries {
            let (Some(key), Some(payload)) = (e.get("key").and_then(|k| k.as_str()), e.get("payload"))
            else {
                continue;
            };
            apply_reply(census, key, payload, own_zid);
            n += 1;
        }
    }
    (n, sampled_at_ms)
}

/// Read the census from the crawler's published mirror instead of walking the
/// adminspace. Folds every reachable site mirror that is FRESH (its
/// `sampled_at_ms` within `max_age` of local wall time) — stale mirrors are
/// ignored, never trusted (a dead crawler must not freeze the census).
/// Returns `None` when no fresh mirror contributed at least one entry: an
/// EMPTY mirror is the crawler's documented census-blind fallback identity
/// (no admin-census grant on its leaf), not proof of an empty fabric — the
/// caller must fall back to the direct query (F-177 makes emptiness
/// ambiguous, so it is never treated as authoritative).
pub async fn query_census_mirror(
    session: &Session,
    own_zid: &str,
    timeout: Duration,
    max_age: Duration,
) -> Option<Census> {
    let mut census = Census::default();
    let mut entries_total = 0usize;
    let now_ms = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| d.as_millis() as i64)
        .unwrap_or(0);
    let replies = session.get(CENSUS_MIRROR_SELECTOR).timeout(timeout).await.ok()?;
    while let Ok(reply) = replies.recv_async().await {
        let Ok(sample) = reply.result() else { continue };
        let bytes = sample.payload().to_bytes();
        let Ok(doc) = serde_json::from_slice::<serde_json::Value>(&bytes) else {
            continue;
        };
        let mut candidate = Census::default();
        let (n, sampled_at_ms) = apply_mirror_doc(&mut candidate, &doc, own_zid);
        let age_ms = now_ms.saturating_sub(sampled_at_ms);
        if n > 0 && age_ms >= 0 && (age_ms as u128) <= max_age.as_millis() {
            for (ke, zids) in candidate.subscriptions {
                for z in zids {
                    census.insert(ResourceRole::Subscriber, ke.clone(), z);
                }
            }
            for (ke, zids) in candidate.publications {
                for z in zids {
                    census.insert(ResourceRole::Publisher, ke.clone(), z);
                }
            }
            entries_total += n;
        }
    }
    (entries_total > 0).then_some(census)
}

/// Mirror-first census — THE one-crawler swap point for panoscope /
/// `goat top` / `goat who`: read the crawler's rollup when it is fresh and
/// populated, else run the direct adminspace query (exactly today's
/// behavior). Once the crawler's dedicated leaf lands (its census un-blinds),
/// consumers on this function stop walking the adminspace automatically.
pub async fn query_census_mirror_first(
    session: &Session,
    own_zid: &str,
    timeout: Duration,
    max_age: Duration,
) -> Census {
    match query_census_mirror(session, own_zid, timeout, max_age).await {
        Some(census) => census,
        None => query_census(session, own_zid, timeout).await,
    }
}

/// Read the pub/sub census from every node reachable via `session` (client
/// mode = the one router today; peer mode = every peer whose adminspace the
/// `@/*/...` selector reaches). Modeled on `presence::query_presence`: issue a
/// `session.get` per selector, fold replies. `own_zid` is this reader's ZID so
/// our own subscriber declaration is not counted. Fail-soft per selector — a
/// query error yields an empty contribution, never an error (an empty census
/// is honest "nothing observed / not granted", never fabricated).
pub async fn query_census(session: &Session, own_zid: &str, timeout: Duration) -> Census {
    let mut census = Census::default();
    for selector in CENSUS_SELECTORS {
        let Ok(replies) = session.get(selector).timeout(timeout).await else {
            continue;
        };
        while let Ok(reply) = replies.recv_async().await {
            let Ok(sample) = reply.result() else { continue };
            let key = sample.key_expr().as_str().to_string();
            let bytes = sample.payload().to_bytes();
            let Ok(json) = serde_json::from_slice::<serde_json::Value>(&bytes) else {
                continue;
            };
            apply_reply(&mut census, &key, &json, own_zid);
        }
    }
    census
}

/// A "plausible" Zenoh ZID: hex, 8..=32 chars. Conservative — admin keyspaces
/// also carry `version`, `metrics`, etc. as segments, which this rejects.
fn is_plausible_zid(s: &str) -> bool {
    let len = s.len();
    (8..=32).contains(&len) && s.chars().all(|c| c.is_ascii_hexdigit())
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn parse_router_and_peer_whatami() {
        // Router adminspace (today).
        assert_eq!(
            parse_resource_key("@/aabbccddeeff0011/router/subscriber/notional/foo/v1"),
            Some((
                ResourceRole::Subscriber,
                "aabbccddeeff0011".to_string(),
                "notional/foo/v1".to_string()
            ))
        );
        // Peer adminspace (P2P) — same parser, whatami = "peer".
        assert_eq!(
            parse_resource_key("@/aabbccddeeff0011/peer/publisher/ops/**"),
            Some((
                ResourceRole::Publisher,
                "aabbccddeeff0011".to_string(),
                "ops/**".to_string()
            ))
        );
        // `**` subscriber (audit-subscriber shape).
        assert_eq!(
            parse_resource_key("@/72a21b14f9f3a98f497f821ab12026e6/router/subscriber/**"),
            Some((
                ResourceRole::Subscriber,
                "72a21b14f9f3a98f497f821ab12026e6".to_string(),
                "**".to_string()
            ))
        );
    }

    #[test]
    fn mirror_doc_applies_via_the_adminspace_parse() {
        // The mirror is a byte-faithful relay: {key, payload} entries run
        // through the exact adminspace parse. own_zid + owner-zid filtering
        // must behave identically to a direct query.
        let doc = json!({
            "site": "zagros-sandbox",
            "sampled_at_ms": 1_700_000_000_000i64,
            "selectors": ["@/*/router/subscriber/**", "@/*/router/publisher/**"],
            "entries": [
                {"key": "@/aabbccddeeff0011/router/subscriber/notional/foo/v1",
                 "payload": {"clients": ["1122334455667788"], "routers": ["aabbccddeeff0011"]}},
                {"key": "@/aabbccddeeff0011/router/publisher/ops/fabric/health/x",
                 "payload": {"peers": ["99aabbccddeeff00"]}},
                // own subscription — filtered exactly like the direct path
                {"key": "@/aabbccddeeff0011/router/subscriber/self/thing",
                 "payload": {"clients": ["deadbeefdeadbeef"]}}
            ]
        });
        let mut c = Census::default();
        let (n, ts) = apply_mirror_doc(&mut c, &doc, "deadbeefdeadbeef");
        assert_eq!(n, 3);
        assert_eq!(ts, 1_700_000_000_000i64);
        assert!(c.subscribers_of("notional/foo/v1").contains("1122334455667788"));
        assert!(c.publications_of("99aabbccddeeff00").contains(&"ops/fabric/health/x".to_string()));
        // own_zid filtered out.
        assert!(c.subscribers_of("self/thing").is_empty());
    }

    #[test]
    fn mirror_doc_empty_and_malformed_are_zero_entries() {
        // entries: [] (the census-blind fallback identity) => 0 — the caller
        // treats this as non-authoritative and falls back to the direct query.
        let mut c = Census::default();
        let (n, _) = apply_mirror_doc(
            &mut c,
            &json!({"site":"x","sampled_at_ms": 1i64,"entries": []}),
            "z",
        );
        assert_eq!(n, 0);
        // Malformed entries are skipped fail-soft.
        let (n2, _) = apply_mirror_doc(
            &mut c,
            &json!({"entries": [{"nokey": true}, 42]}),
            "z",
        );
        assert_eq!(n2, 0);
        assert!(c.is_empty());
    }

    #[test]
    fn parse_rejects_non_census_shapes() {
        assert!(parse_resource_key("@/aabbccddeeff0011/router/config/access_control/rules/x").is_none());
        assert!(parse_resource_key("@/aabbccddeeff0011/router/queryable/some/key").is_none());
        assert!(parse_resource_key("@/aabbccddeeff0011/router/subscriber").is_none()); // no keyexpr
        assert!(parse_resource_key("notional/router/subscriber/foo").is_none()); // data topic
        assert!(parse_resource_key("@/notazid/router/subscriber/foo").is_none()); // bad zid
    }

    #[test]
    fn census_sources_clients_and_peers_only_not_routers() {
        // Usual shape: routers carries the owner (propagation metadata).
        let j = json!({
            "routers": ["b2ec0bfb5f92ed362a10cff8d181ae96"],
            "peers": ["bbbbbbbbbbbbbbbb"],
            "clients": ["a0b005e42a768aca941ae4eab1850e4", "bbbbbbbbbbbbbbbb"]
        });
        assert_eq!(
            census_sources(&j),
            vec![
                "a0b005e42a768aca941ae4eab1850e4".to_string(),
                "bbbbbbbbbbbbbbbb".to_string()
            ]
        );
    }

    #[test]
    fn apply_reply_filters_owner_storage_self_sub() {
        // Router's own storage-manager sub: <uuid>/** with the router's ZID in
        // clients. Records zero external subscribers.
        let owner = "b2ec0bfb5f92ed362a10cff8d181ae96";
        let key = "@/b2ec0bfb5f92ed362a10cff8d181ae96/router/subscriber/c9122c354e6187cb836d97175e8237c1/**";
        let payload = json!({"routers": [owner], "peers": [], "clients": [owner]});
        let mut c = Census::default();
        let n = apply_reply(&mut c, key, &payload, "ffffffffffffffff");
        assert_eq!(n, 0);
        assert!(c.is_empty());
    }

    #[test]
    fn apply_reply_records_external_subscriber() {
        let owner = "b2ec0bfb5f92ed362a10cff8d181ae96";
        let key = "@/b2ec0bfb5f92ed362a10cff8d181ae96/router/subscriber/**";
        let payload = json!({"routers": [owner], "peers": [], "clients": ["36701131dff1e3a015f69f2440d88ba8"]});
        let mut c = Census::default();
        apply_reply(&mut c, key, &payload, "ffffffffffffffff");
        assert_eq!(
            c.subscribers_of("**"),
            BTreeSet::from(["36701131dff1e3a015f69f2440d88ba8".to_string()])
        );
        assert_eq!(
            c.subscriptions_of("36701131dff1e3a015f69f2440d88ba8"),
            vec!["**".to_string()]
        );
    }

    #[test]
    fn subscriber_fanout_matches_wildcards() {
        let mut c = Census::default();
        // Typical subscriber shapes: universal `**`, a prefix wildcard, an
        // unrelated prefix, and a MID-wildcard `**/alpha-fob/**` (which a prefix
        // heuristic would miss — the whole reason we use real intersection).
        c.insert(ResourceRole::Subscriber, "**".into(), "sub_all".into());
        c.insert(ResourceRole::Subscriber, "notional/foo/**".into(), "sub_foo".into());
        c.insert(ResourceRole::Subscriber, "other/**".into(), "sub_other".into());
        c.insert(ResourceRole::Subscriber, "**/alpha-fob/**".into(), "sub_site".into());
        // A producer publishing a concrete alpha-fob key reaches the universal
        // subscriber, the notional/foo prefix subscriber, AND the site
        // mid-wildcard subscriber — but not the unrelated `other/**` one.
        let fanout = c.subscriber_fanout(&["notional/foo/alpha-fob/entity/v1".to_string()]);
        assert_eq!(
            fanout,
            BTreeSet::from([
                "sub_all".to_string(),
                "sub_foo".to_string(),
                "sub_site".to_string()
            ])
        );
    }

    #[test]
    fn anchor_correlation_and_fanout() {
        let anchor = "5b89a663b56c023c827d8c15d34c2339"; // 32-hex slot anchor
        let pub_zid = "e3998a3d8a10e1943ba585c6b2eae979"; // its (unrelated) ZID
        let mut c = Census::default();
        // Producer publishes under its anchor; its ZID is unrelated to it.
        c.insert(ResourceRole::Publisher, format!("{anchor}/tracks/v1"), pub_zid.into());
        // A universal subscriber + an anchor-scoped subscriber consume it.
        c.insert(ResourceRole::Subscriber, "**".into(), "sub_all".into());
        c.insert(ResourceRole::Subscriber, format!("{anchor}/**"), "sub_scoped".into());
        c.insert(ResourceRole::Subscriber, "other/**".into(), "sub_other".into());

        // ZID -> anchor derived from the publish side (the only bridge).
        assert_eq!(c.zid_anchors().get(pub_zid), Some(&anchor.to_string()));
        // Producer fan-out is anchor-native: reaches sub_all + sub_scoped only.
        assert_eq!(
            c.fanout_for_anchor(anchor),
            BTreeSet::from(["sub_all".to_string(), "sub_scoped".to_string()])
        );
        // A pure consumer (never publishes) has no anchor mapping → its subs
        // aren't attributable to an identity: subscriptions_for_anchor of an
        // anchor that only publishes returns that publisher's own subs (none here).
        assert!(c.subscriptions_for_anchor(anchor).is_empty());
    }

    #[test]
    fn distinct_subscribers_is_the_consumer_population() {
        let mut c = Census::default();
        c.insert(ResourceRole::Subscriber, "**".into(), "a".into());
        c.insert(ResourceRole::Subscriber, "ops/**".into(), "a".into());
        c.insert(ResourceRole::Subscriber, "ops/**".into(), "b".into());
        assert_eq!(
            c.distinct_subscribers(),
            BTreeSet::from(["a".to_string(), "b".to_string()])
        );
    }
}
