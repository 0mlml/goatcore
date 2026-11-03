// SPDX-License-Identifier: Apache-2.0
//
// Identifier types + role taxonomy. Pure-data; no Zenoh runtime dep.

use serde::{Deserialize, Serialize};

/// Peer identifier — Zenoh peer ID (ZID) as a hex string. Stable for the
/// lifetime of a peer's session.
pub type PeerId = String;

/// Topic key expression — Zenoh key-expression string (e.g.
/// `org11x/lattice/entity/track/v1`).
pub type TopicKey = String;

/// Role classification per docs/design/panoscope.md §"Role classification
/// heuristic". Computed by lexical key-expression overlap between a peer's
/// publish and subscribe sets. v2 will hook into lineage declarations once
/// Block 23 lands.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum Role {
    /// Peer publishes on topics it does not subscribe to.
    Source,
    /// Peer subscribes to topics it does not publish.
    Sink,
    /// Peer consumes upstream data AND publishes derived data — fusion,
    /// correlation, translation, C2 fire-orders synthesized from sensor
    /// feeds, etc. Distinct from a relay (Zenoh handles routing; a
    /// relay would be a separate concept like a non-Zenoh bridge).
    Deriver,
    /// No publish or subscribe declarations seen yet.
    Unknown,
}

/// Node-kind classification per F-147. Distinct from [`Role`] (which is
/// computed from pub/sub overlap) — `NodeKind` records the substrate
/// fact "this peer is a Zenoh router / a Zenoh peer / observed-only-via-
/// subscriber-declaration" learned from the admin namespace. Synth-
/// vendor self-attests don't populate this; schema-decl-derived nodes
/// stay `None` until an admin sample also confirms them.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum NodeKind {
    /// Zenoh router process — surfaced from `@/router/<zid>` admin keys.
    Router,
    /// Zenoh peer process — surfaced from `@/peer/<zid>` admin keys.
    Peer,
    /// Subscriber-only observer (e.g. audit-subscriber). Created when an
    /// admin-namespace subscriber declaration carries a distinct ZID
    /// that hasn't been observed publishing.
    Subscriber,
}

/// Participant class — the single axis that separates *underlying
/// infrastructure* (the substrate that makes the fabric run) from
/// *application data participants* (the vendors / systems whose mission
/// data the fabric carries). Computed once in [`crate::peer::PeerInfo::node_class`]
/// from several signals (explicit self-declaration, substrate node-kind,
/// reserved-namespace publishing, known-infra vendor) so the frontend
/// can badge / filter / isolate infra without recomputing the heuristic
/// or special-casing each kind of substrate node one at a time.
///
/// This replaces the previous scatter of per-thing checks
/// (`node_kind == "router"`, `vendor == "trashgoat"`, …) with one
/// classification an operator can show / hide / isolate as a group. See
/// docs/design/panoscope.md §"Infrastructure participant class".
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum NodeClass {
    /// Underlying infrastructure — Zenoh router, audit/observer
    /// subscriber, schema-registry / operator-system publisher, chaos
    /// generator, or anything self-declaring `infra: true`. NOT an
    /// application data participant.
    Infra,
    /// Ordinary application data participant — a vendor / system
    /// publishing or consuming mission data.
    App,
}

/// Top-level key-expression prefixes reserved for the operator /
/// substrate rather than vendor mission data. A peer whose publishes fall
/// *entirely* under these prefixes is infrastructure, not an application
/// participant. The trailing `/` is deliberate: it matches children
/// (`ops/...`, `attest/schema/...`) without matching an unrelated vendor
/// literally named `ops` or `attest`.
///
/// `ops/` is the canonical operator/control namespace (ADR 0844 / ADR 1035 —
/// "operational events emitted by any service running on the fabric").
/// `system/` is the deprecated trial-fabric predecessor, kept here for the
/// transition window so peers still on it (e.g. trashgoat pre-migration)
/// keep classifying as infrastructure until they move to `ops/`; drop it
/// once nothing publishes `system/**`. `attest/` is the schema-declaration
/// namespace.
pub const RESERVED_NAMESPACE_PREFIXES: &[&str] = &["ops/", "system/", "attest/"];

/// Logical vendors that are platform-internal infrastructure rather than
/// data participants, regardless of what they publish. `trashgoat` is the
/// chaos actor (docs/design/trashgoat.md) that publishes deliberate
/// garbage under `trashgoat/<experiment>/...` to exercise the fabric.
pub const INFRA_VENDORS: &[&str] = &["trashgoat"];

/// True when a topic key-expression sits under a reserved operator /
/// substrate namespace ([`RESERVED_NAMESPACE_PREFIXES`]).
pub fn is_reserved_namespace(topic: &str) -> bool {
    RESERVED_NAMESPACE_PREFIXES
        .iter()
        .any(|p| topic.starts_with(p))
}

/// True when `topic` matches any blocklist `patterns` entry — as a whole-key
/// PREFIX (the original semantics, back-compat) OR as a `/`-delimited path
/// SEGMENT that starts with the pattern. The segment rule catches a marker
/// that isn't the key's leading prefix: notably `<uuid>/loadtest-wedge/<n>`,
/// where the `loadtest` marker sits mid-path and the per-peer UUID varies, so
/// a prefix pattern can NEVER match it (there are as many prefixes as peers).
/// This lets `PANOSCOPE_TOPIC_BLOCKLIST=loadtest` drop a load test's samples
/// at INGEST — on both the live tail and the MinIO replay — instead of after
/// they have already polluted panoscope's model and the COP. Interim
/// enforcement of the provenance-hygiene principle (ADR 1103) until publishers
/// carry a real provenance tag. Empty patterns never match.
pub fn topic_matches_blocklist(topic: &str, patterns: &[String]) -> bool {
    patterns.iter().any(|p| {
        let p = p.as_str();
        !p.is_empty()
            && (topic.starts_with(p) || topic.split('/').any(|seg| seg.starts_with(p)))
    })
}

#[cfg(test)]
mod blocklist_tests {
    use super::topic_matches_blocklist;
    fn pats(v: &[&str]) -> Vec<String> {
        v.iter().map(|s| s.to_string()).collect()
    }
    #[test]
    fn matches_mid_path_segment_marker() {
        // The load-test flood: marker is mid-path, UUID prefix varies.
        let t = "288cd2cdba528c1ca256bca4eb4799aa/loadtest-wedge/0";
        assert!(topic_matches_blocklist(t, &pats(&["loadtest"])));
        assert!(topic_matches_blocklist(t, &pats(&["loadtest-wedge"])));
    }
    #[test]
    fn still_matches_leading_prefix() {
        assert!(topic_matches_blocklist("org116x/legion/x", &pats(&["org116x/legion"])));
    }
    #[test]
    fn does_not_over_match_unrelated() {
        let t = "org11x/lattice/v1/entity";
        assert!(!topic_matches_blocklist(t, &pats(&["loadtest"])));
        assert!(!topic_matches_blocklist(t, &pats(&[""]))); // empty never matches
    }
}
