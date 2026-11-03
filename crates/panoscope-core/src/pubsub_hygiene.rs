// SPDX-License-Identifier: Apache-2.0
//
// Pub/sub hygiene classifier — judges *how* a publisher uses the key space
// against the sanctioned topic-design patterns, and surfaces the fixes from
// docs/design/pubsub-best-practices.md as a per-publisher verdict.
//
// Pure (no Zenoh, no I/O) so it's unit-testable and reusable by both the
// panoscope backend (over live topology edges) and `goat` (over the portal's
// declared topics). The retrievability half — "does a storage actually answer
// for this prefix?" — is NOT decidable here (it needs a live `get`); this
// module emits the probe selectors the caller should test, and the caller folds
// the result back in. See docs/design/pubsub-hygiene-diagnostic.md.

use serde::{Deserialize, Serialize};

/// Fan-out families with at least this many topics are analyzed as a structural
/// fan-out (mirrors the snapshot fold's `SINGLETON_FANOUT_THRESHOLD`). Below it,
/// a publisher's handful of topics reads as a normal small channel set.
const FANOUT_THRESHOLD: usize = 7;

/// One topic a publisher emits, plus the cheap facts we can judge it by.
#[derive(Debug, Clone)]
pub struct TopicStat {
    pub key: String,
    /// Total events observed/declared for this topic. Distinguishes
    /// per-entity *state* (keys that keep updating, >1) from per-event
    /// *archive* (fresh key per message, ~1).
    pub total_events: u64,
    /// Whether a wire format / schema family is known for this topic.
    pub has_schema: bool,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum Verdict {
    /// All recognized patterns are sound.
    Good,
    /// At least one advisory finding — retrievable/working, but non-idiomatic.
    Review,
    /// At least one warning finding — a real "published but not usable" risk.
    Poor,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum Severity {
    Info,
    Advisory,
    Warn,
}

/// One judgement about a publisher's key space. `code` is stable for the UI;
/// `message`/`fix` are operator-facing prose.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct HygieneFinding {
    pub code: String,
    pub severity: Severity,
    pub message: String,
    pub fix: String,
}

/// Retrievability of a publisher's data via `get`. Honesty discipline: we can
/// CONFIRM a storage answers (positive `get` reply) but never prove absence
/// from a one-shot passive probe, so silence is `Unconfirmed`, never "none".
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum Retrievability {
    /// A storage replied to a `get` on at least one probe selector.
    Confirmed,
    /// No storage replied — could be no storage, an empty storage, or a
    /// blocked probe. NOT a claim that the data is unretrievable.
    Unconfirmed,
    /// The probe was not run (cheap snapshot-path classification only).
    NotProbed,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct PublisherHygiene {
    pub verdict: Verdict,
    pub findings: Vec<HygieneFinding>,
    pub retrievability: Retrievability,
    /// Key-expression selectors (fold templates, e.g. `a/aircraft/*/v1`) the
    /// caller should `get`-probe to confirm storage coverage. Empty when the
    /// publisher has no fan-out family worth probing.
    pub probe_selectors: Vec<String>,
}

/// A fan-out family: topics under one publisher sharing a segment count, with a
/// single varying ("entity-axis") segment wildcarded.
struct Family<'a> {
    template: String,
    members: Vec<&'a TopicStat>,
    /// Position of the varying segment, and the total segment count.
    vary_pos: usize,
    seg_count: usize,
}

/// Group a publisher's topics into fan-out families. Buckets by segment count;
/// within a bucket the highest-cardinality position is the entity axis and is
/// wildcarded. Ties resolve to the later position (so a trailing-id pattern
/// reproduces the obvious `<prefix>/*`). Mirrors the snapshot fold's
/// `fold_group_templates`; kept local so the classifier answers richer
/// per-position questions (which position varies, is the leaf constant).
fn families(stats: &[TopicStat]) -> Vec<Family<'_>> {
    use std::collections::HashMap;

    let mut by_count: HashMap<usize, Vec<&TopicStat>> = HashMap::new();
    for s in stats {
        by_count.entry(s.key.split('/').count()).or_default().push(s);
    }

    let mut out: Vec<Family> = Vec::new();
    for (n, group) in by_count {
        if n <= 1 || group.len() <= 1 {
            continue;
        }
        let split: Vec<Vec<&str>> = group.iter().map(|s| s.key.split('/').collect()).collect();
        let mut distinct: Vec<std::collections::HashSet<&str>> = vec![Default::default(); n];
        for segs in &split {
            for (i, s) in segs.iter().enumerate() {
                distinct[i].insert(*s);
            }
        }
        let mut vary_pos = 0usize;
        let mut best = 0usize;
        for (p, d) in distinct.iter().enumerate() {
            if d.len() >= best {
                best = d.len();
                vary_pos = p;
            }
        }
        if best <= 1 {
            continue;
        }
        // Group members by their wildcarded template (keeps sub-families apart).
        let mut tmpl_groups: HashMap<String, Vec<&TopicStat>> = HashMap::new();
        for (segs, stat) in split.iter().zip(group.iter()) {
            let mut t = segs.clone();
            t[vary_pos] = "*";
            tmpl_groups.entry(t.join("/")).or_default().push(*stat);
        }
        for (template, members) in tmpl_groups {
            out.push(Family {
                template,
                members,
                vary_pos,
                seg_count: n,
            });
        }
    }
    out
}

/// Does a segment look like a stable version tag (`v1`, `v23`)? Used to spot
/// the version-after-entity inversion.
fn is_version_segment(seg: &str) -> bool {
    let b = seg.as_bytes();
    b.len() >= 2 && (b[0] == b'v' || b[0] == b'V') && seg[1..].bytes().all(|c| c.is_ascii_digit())
}

/// Heuristic: does a segment look like a fresh-minted per-message id (uuid /
/// long hex / long opaque token) rather than a reusable entity handle? High
/// entropy + length is the signal; short ids (aircraft tail numbers) are not.
fn looks_high_entropy(seg: &str) -> bool {
    if seg.len() < 16 {
        return false;
    }
    let hexish = seg
        .bytes()
        .all(|c| c.is_ascii_hexdigit() || c == b'-');
    hexish && seg.bytes().filter(|c| c.is_ascii_digit()).count() >= 4
}

/// Classify a publisher's key space. `retrievability`/`probe_selectors` are
/// filled for the caller to optionally probe; the verdict here reflects
/// key-shape only (the caller may downgrade nothing — retrievability findings
/// are added by the caller after probing if it wishes).
pub fn classify_publisher(stats: &[TopicStat]) -> PublisherHygiene {
    let mut findings: Vec<HygieneFinding> = Vec::new();
    let mut probe_selectors: Vec<String> = Vec::new();

    let fams = families(stats);
    let mut saw_fanout = false;

    for fam in &fams {
        if fam.members.len() <= FANOUT_THRESHOLD {
            continue;
        }
        saw_fanout = true;
        probe_selectors.push(fam.template.clone());

        let is_trailing_vary = fam.vary_pos + 1 == fam.seg_count;
        let trailing_seg = fam
            .template
            .rsplit('/')
            .next()
            .unwrap_or("");

        // version-after-entity: the entity axis is NOT last and a version-like
        // constant leaf trails it.
        if !is_trailing_vary && is_version_segment(trailing_seg) {
            findings.push(HygieneFinding {
                code: "version_after_entity".into(),
                severity: Severity::Advisory,
                message: format!(
                    "Entity id sits before a constant version segment (`{}`). The idiomatic \
                     order puts the version in the prefix and the entity last.",
                    fam.template
                ),
                fix: "Publish as `<vendor>/<system>/<version>/<entity-id>` (entity last). \
                      It stays retrievable either way, but version-last trips tooling that \
                      keys on the trailing segment."
                    .into(),
            });
        }

        // per-message key minting: ~every key single-use + high-entropy axis.
        let single_use = fam.members.iter().all(|m| m.total_events <= 1);
        // Sample the varying segment of a member to judge entropy.
        let sample_vary = fam
            .members
            .first()
            .map(|m| {
                m.key
                    .split('/')
                    .nth(fam.vary_pos)
                    .unwrap_or("")
                    .to_string()
            })
            .unwrap_or_default();
        if single_use && looks_high_entropy(&sample_vary) {
            findings.push(HygieneFinding {
                code: "per_message_keys".into(),
                severity: Severity::Advisory,
                message: format!(
                    "`{}` looks like a fresh key per message ({} single-use keys). That is the \
                     per-event archive pattern — fine for replay, but you can't `get` \
                     \"the latest state of entity X\" because every message is its own key.",
                    fam.template,
                    fam.members.len()
                ),
                fix: "If consumers want latest-state-per-entity, reuse a STABLE entity id in \
                      the key (`.../v1/<entity-id>`) so the storage keeps the latest per entity. \
                      Keep the per-event shape only if you truly need every event archived."
                    .into(),
            });
        }

        // no declared schema on a whole fan-out family: undecodable downstream.
        if fam.members.iter().all(|m| !m.has_schema) {
            findings.push(HygieneFinding {
                code: "no_declared_schema".into(),
                severity: Severity::Warn,
                message: format!(
                    "The fan-out family `{}` ({} topics) has no declared schema/format, so \
                     subscribers can't decode it.",
                    fam.template,
                    fam.members.len()
                ),
                fix: "Register a schema for the topic in the portal, or publish a self-describing \
                      `Encoding` (`protobuf:<Name>`) on the samples."
                    .into(),
            });
        }
    }

    if saw_fanout && findings.is_empty() {
        findings.push(HygieneFinding {
            code: "per_entity_state".into(),
            severity: Severity::Info,
            message: "Fan-out follows the per-entity state pattern (stable entity keys that \
                      keep updating) — selectively retrievable by entity."
                .into(),
            fix: String::new(),
        });
    }
    if !saw_fanout && !stats.is_empty() {
        findings.push(HygieneFinding {
            code: "flat_single_channel".into(),
            severity: Severity::Info,
            message: "Low-cardinality channel set — no fan-out concern.".into(),
            fix: String::new(),
        });
    }

    let verdict = if findings.iter().any(|f| f.severity == Severity::Warn) {
        Verdict::Poor
    } else if findings.iter().any(|f| f.severity == Severity::Advisory) {
        Verdict::Review
    } else {
        Verdict::Good
    };

    probe_selectors.sort();
    probe_selectors.dedup();

    PublisherHygiene {
        verdict,
        findings,
        retrievability: Retrievability::NotProbed,
        probe_selectors,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn stat(key: &str, events: u64, has_schema: bool) -> TopicStat {
        TopicStat {
            key: key.into(),
            total_events: events,
            has_schema,
        }
    }

    #[test]
    fn version_after_entity_is_flagged_review() {
        // bennett: <anchor>/aircraft/<id>/v1 — id before a constant v1 leaf.
        let stats: Vec<TopicStat> = (0..50)
            .map(|i| stat(&format!("15f1/aircraft/tail-{i}/v1"), 5, true))
            .collect();
        let h = classify_publisher(&stats);
        assert_eq!(h.verdict, Verdict::Review);
        assert!(h.findings.iter().any(|f| f.code == "version_after_entity"));
        assert_eq!(h.probe_selectors, vec!["15f1/aircraft/*/v1".to_string()]);
    }

    #[test]
    fn idiomatic_per_entity_is_good() {
        // <vendor>/<system>/v1/<entity-id>, entities keep updating.
        let stats: Vec<TopicStat> = (0..50)
            .map(|i| stat(&format!("navy/router/v1/tail-{i}"), 8, true))
            .collect();
        let h = classify_publisher(&stats);
        assert_eq!(h.verdict, Verdict::Good);
        assert!(h.findings.iter().all(|f| f.code != "version_after_entity"));
        assert!(h.findings.iter().any(|f| f.code == "per_entity_state"));
    }

    #[test]
    fn per_message_uuid_keys_flagged_review() {
        // fresh uuid-ish key per message, single-use.
        let stats: Vec<TopicStat> = (0..30)
            .map(|i| {
                stat(
                    &format!("navy/router/v1/{:016x}{:016x}", i, i * 7 + 1),
                    1,
                    true,
                )
            })
            .collect();
        let h = classify_publisher(&stats);
        assert_eq!(h.verdict, Verdict::Review);
        assert!(h.findings.iter().any(|f| f.code == "per_message_keys"));
    }

    #[test]
    fn fanout_without_schema_is_poor() {
        let stats: Vec<TopicStat> = (0..20)
            .map(|i| stat(&format!("vend/sys/v1/tail-{i}"), 5, false))
            .collect();
        let h = classify_publisher(&stats);
        assert_eq!(h.verdict, Verdict::Poor);
        assert!(h.findings.iter().any(|f| f.code == "no_declared_schema"));
    }

    #[test]
    fn small_channel_set_is_good() {
        let stats = vec![
            stat("vend/status/heartbeat", 100, true),
            stat("vend/config/v1", 3, true),
        ];
        let h = classify_publisher(&stats);
        assert_eq!(h.verdict, Verdict::Good);
        assert!(h.probe_selectors.is_empty());
    }

    #[test]
    fn distinct_subfamilies_each_probed() {
        let mut stats: Vec<TopicStat> = Vec::new();
        for i in 0..20 {
            stats.push(stat(&format!("a/aircraft/tail-{i}/v1"), 5, true));
            stats.push(stat(&format!("a/sensors/s-{i}/v1"), 5, true));
        }
        let h = classify_publisher(&stats);
        assert!(h.probe_selectors.contains(&"a/aircraft/*/v1".to_string()));
        assert!(h.probe_selectors.contains(&"a/sensors/*/v1".to_string()));
    }

    #[test]
    fn empty_publisher_is_good_no_probe() {
        let h = classify_publisher(&[]);
        assert_eq!(h.verdict, Verdict::Good);
        assert!(h.findings.is_empty());
        assert!(h.probe_selectors.is_empty());
    }
}
