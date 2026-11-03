// SPDX-License-Identifier: Apache-2.0
//
// Topology — the peer registry that tracks every peer panoscope (or
// flashlight) has observed. Holds a `HashMap<PeerId, PeerInfo>` plus a
// topic-to-publisher index used by data-tail attribution.
//
// Per-edge flow aggregation (FlowState / EdgeState) stays in the consumer
// crate because it carries panoscope-specific viz state (EMA decay, drop
// counters) that flashlight does not need.

use std::collections::HashMap;

use serde::{Deserialize, Serialize};

use crate::peer::PeerInfo;
use crate::types::{NodeKind, PeerId, TopicKey};

#[derive(Debug, Default, Clone, Serialize, Deserialize)]
pub struct Topology {
    pub peers: HashMap<PeerId, PeerInfo>,
    /// Lookup: topic key-expression → publisher peer_id. Populated by
    /// schema-declaration apply (each peer's declared publishes[] is
    /// indexed here). The data-tail consults this to attribute incoming
    /// samples to the canonical peer_id (from the schema decl) instead
    /// of the topic-prefix fallback — eliminates the ghost-duplicate
    /// peer that appeared alongside each real one before this lookup
    /// existed.
    pub topic_to_publisher: HashMap<String, PeerId>,
}

impl Topology {
    pub fn upsert_peer(&mut self, peer_id: PeerId, now_ms: u64) -> &mut PeerInfo {
        let entry = self
            .peers
            .entry(peer_id.clone())
            .or_insert_with(|| PeerInfo::new(peer_id, now_ms));
        entry.last_seen_ms = now_ms;
        entry
    }

    /// Drop peers whose `last_seen_ms` is older than `cutoff_ms`, returning the
    /// number removed. The in-RAM peer table otherwise grows unbounded for the
    /// life of the process (upsert never removes), so a background task calls
    /// this with `now - retention` to bound memory over long uptimes. The
    /// history-lens window filter already hides out-of-window peers at display
    /// time, so eviction is hygiene, not correctness — keep the retention at or
    /// above the deepest selectable window so the lens never loses a peer it
    /// would otherwise show (docs/design/panoscope-history-lens.md).
    pub fn evict_stale(&mut self, cutoff_ms: u64) -> usize {
        let before = self.peers.len();
        self.peers.retain(|_, p| p.last_seen_ms >= cutoff_ms);
        before - self.peers.len()
    }

    pub fn record_publish_declaration(
        &mut self,
        peer_id: PeerId,
        topic: TopicKey,
        now_ms: u64,
    ) {
        let p = self.upsert_peer(peer_id.clone(), now_ms);
        p.publishes.insert(topic.clone());
        // Index for data-tail attribution lookup.
        self.topic_to_publisher.insert(topic, peer_id);
    }

    pub fn record_subscribe_declaration(
        &mut self,
        peer_id: PeerId,
        topic: TopicKey,
        now_ms: u64,
    ) {
        let p = self.upsert_peer(peer_id, now_ms);
        p.subscribes.insert(topic);
    }

    /// Upsert a peer learned from an admin-namespace sample.
    /// Sets `node_kind` and `attest_source = "admin"` only when no
    /// richer signal has already done so — schema-decl wins on conflict
    /// so the synth-vendor metadata (vendor / site / role_class / format)
    /// stays intact when admin samples confirm the same ZID after a
    /// schema decl has already landed.
    ///
    /// `kind` is optional because some admin sub-paths (the `/link/`
    /// leaves of the session-transport tree, for instance) don't carry
    /// a `whatami` field on the wire — we want to upsert the peer + its
    /// locator from those samples without locking the node-kind to a
    /// guess. A later sample with whatami can then set or promote the
    /// node-kind. Router beats Peer on conflict (a session described as
    /// router elsewhere is canonically a router), so reply-order
    /// non-determinism doesn't lose the classification.
    pub fn upsert_admin_peer(
        &mut self,
        peer_id: PeerId,
        kind: Option<NodeKind>,
        locator: Option<String>,
        now_ms: u64,
    ) {
        let p = self.upsert_peer(peer_id, now_ms);
        // Schema-decl path sets attest_source = "schema_decl"; in that
        // case we keep its metadata but DO record the locator + node_kind
        // (those are admin-namespace-only facts that schema-decl never
        // surfaces).
        match (p.node_kind, kind) {
            (None, k) => p.node_kind = k,
            (Some(NodeKind::Peer), Some(NodeKind::Router)) => {
                p.node_kind = Some(NodeKind::Router);
            }
            _ => {}
        }
        if p.locator.is_none() {
            p.locator = locator;
        }
        if p.attest_source.is_none() {
            p.attest_source = Some("admin".to_string());
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::types::Role;

    #[test]
    fn topology_upsert_peer_idempotent() {
        let mut t = Topology::default();
        t.upsert_peer("p1".into(), 100);
        t.upsert_peer("p1".into(), 200);
        assert_eq!(t.peers.len(), 1);
        let p = t.peers.get("p1").unwrap();
        assert_eq!(p.first_seen_ms, 100);
        assert_eq!(p.last_seen_ms, 200);
    }

    #[test]
    fn topology_records_pub_sub_declarations() {
        let mut t = Topology::default();
        t.record_publish_declaration("p1".into(), "org11x/lattice/raw".into(), 100);
        t.record_subscribe_declaration("p1".into(), "org11x/lattice/derived".into(), 100);
        let p = t.peers.get("p1").unwrap();
        assert_eq!(p.publishes.len(), 1);
        assert_eq!(p.subscribes.len(), 1);
        assert_eq!(p.role(), Role::Deriver);
    }

    #[test]
    fn topology_evict_stale_drops_only_old_peers() {
        let mut t = Topology::default();
        t.upsert_peer("fresh".into(), 10_000);
        t.upsert_peer("old".into(), 1_000);
        t.upsert_peer("edge".into(), 5_000); // exactly at the cutoff, kept
        let removed = t.evict_stale(5_000);
        assert_eq!(removed, 1);
        assert!(t.peers.contains_key("fresh"));
        assert!(t.peers.contains_key("edge"));
        assert!(!t.peers.contains_key("old"));
    }
}
