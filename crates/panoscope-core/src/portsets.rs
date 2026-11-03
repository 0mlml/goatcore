//! The warrant port-set vocabulary — ADR 1129, `ops/warrant/port-sets.yaml`.
//!
//! Seven named sets that between them cover every `(protocol, ports)`
//! combination in the mesh ACL. This module is the ONE place that answers
//! "which named set is this rule's port list", so `goat warrant why-blocked`
//! and the netvis pair view cannot disagree about what a rule grants — the
//! same reason `aclcheck` is shared rather than reimplemented per surface.
//!
//! THE YAML IS NOT COMPILED IN HERE, deliberately. `panoscope-core` is linked
//! by microscope, stethoscope, panoscope and flashlight, and none of them
//! needs the vocabulary; an `include_str!` at library level would make every
//! one of those images ship `ops/warrant/port-sets.yaml` to compile. Each
//! binary that wants it includes its own copy and calls [`Vocabulary::parse`].
//! Same precedent as the ACL declaration in `goat-cli`'s `acl` command.

use std::collections::BTreeMap;

use serde::{Deserialize, Serialize};

/// One protocol arm of a set. A NetBird rule is single-protocol, so a rule
/// names one arm — never the whole set.
#[derive(Debug, Clone, Deserialize, Serialize, PartialEq, Eq)]
pub struct Arm {
    pub protocol: String,
    pub ports: Vec<String>,
}

#[derive(Debug, Clone, Deserialize, Serialize, PartialEq, Eq)]
pub struct PortSet {
    pub name: String,
    /// Prose, and load-bearing: this is the rationale that previously existed
    /// in drifting copies across the rule comments.
    pub purpose: String,
    /// The CODEOWNERS reviewer for changes to this set.
    pub owner: String,
    pub members: Vec<Arm>,
    #[serde(default)]
    pub required_by: Vec<RequiredBy>,
}

#[derive(Debug, Clone, Deserialize, Serialize, PartialEq, Eq)]
pub struct RequiredBy {
    pub src: String,
    pub dst: String,
}

impl PortSet {
    /// This set's arm for a protocol, if it has one.
    pub fn arm(&self, protocol: &str) -> Option<&Arm> {
        self.members.iter().find(|m| m.protocol.eq_ignore_ascii_case(protocol))
    }
}

#[derive(Debug, Clone, Deserialize)]
struct Doc {
    port_sets: Vec<PortSet>,
}

#[derive(Debug, Clone, Default)]
pub struct Vocabulary {
    sets: Vec<PortSet>,
    /// `(protocol, ports)` → index into `sets`. Exact-shape lookup.
    by_shape: BTreeMap<(String, Vec<String>), usize>,
}

impl Vocabulary {
    pub fn parse(yaml: &str) -> Result<Self, serde_yaml::Error> {
        let doc: Doc = serde_yaml::from_str(yaml)?;
        let mut by_shape = BTreeMap::new();
        for (i, set) in doc.port_sets.iter().enumerate() {
            for arm in &set.members {
                // Last writer wins here rather than erroring: the CI gate
                // (`ops/ci/check-warrant-registry.py`) is what refuses a
                // vocabulary where two names claim one shape. A rendering
                // surface must not fail closed on a registry defect it did
                // not cause — it shows what it can and stays up.
                by_shape.insert(
                    (arm.protocol.to_lowercase(), arm.ports.clone()),
                    i,
                );
            }
        }
        Ok(Self { sets: doc.port_sets, by_shape })
    }

    pub fn all(&self) -> &[PortSet] {
        &self.sets
    }

    /// The set whose arm is exactly this `(protocol, ports)`.
    ///
    /// Exact match, not "contains": a rule granting tcp 443 is `web`, and a
    /// rule granting tcp 443,53,5353,7447 is `substrate`. Treating the first
    /// as a subset of the second would name a rule after a set that grants
    /// far more than the rule does.
    pub fn resolve(&self, protocol: &str, ports: &[String]) -> Option<&PortSet> {
        self.by_shape
            .get(&(protocol.to_lowercase(), ports.to_vec()))
            .map(|&i| &self.sets[i])
    }

    /// The SMALLEST set carrying this port on this protocol.
    ///
    /// Smallest, not first: proposing `substrate` to close a bare :443 gap
    /// would hand over DNS and the fabric as a side effect.
    pub fn minimal_for(&self, protocol: &str, port: &str) -> Option<&PortSet> {
        self.sets
            .iter()
            .filter(|s| {
                s.arm(protocol).is_some_and(|a| a.ports.iter().any(|p| p == port))
            })
            .min_by_key(|s| s.arm(protocol).map_or(usize::MAX, |a| a.ports.len()))
    }
}

/// Registry-only metadata carried on a rule intent and stripped at generation
/// (`ops/warrant/generate.py`), so it never reaches the applied ACL. Surfaced
/// here because "this grant expires" is exactly what a reader of the pair view
/// needs to know and cannot get from the generated matrix.
#[derive(Debug, Clone, Default, Serialize, PartialEq, Eq)]
pub struct IntentMeta {
    pub expires: Option<String>,
    pub owner: Option<String>,
}

impl IntentMeta {
    pub fn is_empty(&self) -> bool {
        self.expires.is_none() && self.owner.is_none()
    }
}

/// Rule name → its registry metadata, read from
/// `ops/warrant/rule-intents/netbird-acl.yaml`.
///
/// Parsed line-wise rather than through serde. The intents file is one
/// document holding four rule-bearing sections plus hand-written comment
/// banners and a YAML anchor; a typed round-trip would need the whole schema
/// modelled to pull two optional fields off a rule. The line scan needs only
/// the shape it actually reads, and an unparseable file yields an empty index
/// — the lens degrades to no annotation rather than taking the page down.
#[derive(Debug, Clone, Default)]
pub struct IntentIndex {
    by_rule: BTreeMap<String, IntentMeta>,
}

impl IntentIndex {
    pub fn parse(yaml: &str) -> Self {
        let mut by_rule: BTreeMap<String, IntentMeta> = BTreeMap::new();
        let mut current: Option<String> = None;
        let mut meta = IntentMeta::default();

        let flush = |by_rule: &mut BTreeMap<String, IntentMeta>,
                     name: &mut Option<String>,
                     meta: &mut IntentMeta| {
            if let Some(n) = name.take() {
                if !meta.is_empty() {
                    by_rule.insert(n, std::mem::take(meta));
                } else {
                    *meta = IntentMeta::default();
                }
            }
        };

        for line in yaml.lines() {
            let t = line.trim_start();
            if t.starts_with('#') {
                continue;
            }
            if let Some(rest) = t.strip_prefix("- name:") {
                flush(&mut by_rule, &mut current, &mut meta);
                current = Some(unquote(rest.trim()));
            } else if let Some(rest) = t.strip_prefix("expires:") {
                meta.expires = Some(unquote(rest.trim()));
            } else if let Some(rest) = t.strip_prefix("owner:") {
                meta.owner = Some(unquote(rest.trim()));
            }
        }
        flush(&mut by_rule, &mut current, &mut meta);
        Self { by_rule }
    }

    pub fn get(&self, rule_name: &str) -> Option<&IntentMeta> {
        self.by_rule.get(rule_name)
    }

    pub fn len(&self) -> usize {
        self.by_rule.len()
    }

    pub fn is_empty(&self) -> bool {
        self.by_rule.is_empty()
    }
}

fn unquote(s: &str) -> String {
    let s = s.trim();
    let s = s.strip_prefix('"').and_then(|r| r.strip_suffix('"')).unwrap_or(s);
    let s = s.strip_prefix('\'').and_then(|r| r.strip_suffix('\'')).unwrap_or(s);
    s.to_string()
}

#[cfg(test)]
mod tests {
    use super::*;

    const SAMPLE: &str = r#"
port_sets:
  - name: substrate
    purpose: Reach the goatnet's own services.
    owner: platform-admin
    members:
      - { protocol: tcp, ports: ["443", "53", "5353", "7447"] }
      - { protocol: udp, ports: ["53", "5353", "7448"] }
  - name: web
    purpose: An HTTPS service and nothing else.
    owner: platform-admin
    members:
      - { protocol: tcp, ports: ["443"] }
  - name: shell
    purpose: Interactive login.
    owner: platform-admin
    members:
      - { protocol: tcp, ports: ["22"] }
"#;

    fn vocab() -> Vocabulary {
        Vocabulary::parse(SAMPLE).expect("sample parses")
    }

    #[test]
    fn resolves_an_exact_shape() {
        let v = vocab();
        let s = v.resolve("tcp", &["443".into()]).expect("web");
        assert_eq!(s.name, "web");
    }

    #[test]
    fn resolve_is_exact_not_subset() {
        // tcp 443 is `web`. It must NOT resolve to `substrate` just because
        // substrate also contains 443 — naming a rule after a set that grants
        // more than the rule does is the whole failure this avoids.
        let v = vocab();
        assert_eq!(v.resolve("tcp", &["443".into()]).unwrap().name, "web");
        let full = vec!["443".into(), "53".into(), "5353".into(), "7447".into()];
        assert_eq!(v.resolve("tcp", &full).unwrap().name, "substrate");
    }

    #[test]
    fn resolve_is_order_sensitive_and_says_so_by_returning_none() {
        // Ports are matched as the declaration writes them. A reordered list
        // is not a match, and returning None (rather than guessing) keeps the
        // surface honest — the generated matrix is byte-stable, so order is a
        // real invariant, not an accident.
        let v = vocab();
        let reordered = vec!["53".into(), "443".into(), "5353".into(), "7447".into()];
        assert!(v.resolve("tcp", &reordered).is_none());
    }

    #[test]
    fn unknown_shape_is_none_not_a_panic() {
        let v = vocab();
        assert!(v.resolve("tcp", &["9999".into()]).is_none());
        assert!(v.resolve("sctp", &["443".into()]).is_none());
    }

    #[test]
    fn minimal_for_picks_the_smallest_carrier() {
        // Both `web` and `substrate` carry tcp/443; the smaller must win.
        let v = vocab();
        assert_eq!(v.minimal_for("tcp", "443").unwrap().name, "web");
        assert_eq!(v.minimal_for("tcp", "7447").unwrap().name, "substrate");
        assert!(v.minimal_for("tcp", "9999").is_none());
    }

    #[test]
    fn protocol_match_is_case_insensitive() {
        let v = vocab();
        assert_eq!(v.resolve("TCP", &["22".into()]).unwrap().name, "shell");
    }

    const INTENTS: &str = r#"
netbird_acl_matrix:
  zagros:
    rules:
      - name: "breakglass → infra (ssh only)"
        expires: 2026-12-01
        owner: platform-admin
        protocol: tcp
        port_set: shell
      - name: "all → infra (web/dns/fabric tcp)"
        protocol: tcp
        port_set: substrate
"#;

    #[test]
    fn intent_index_reads_expiry_and_owner() {
        let idx = IntentIndex::parse(INTENTS);
        let m = idx.get("breakglass → infra (ssh only)").expect("indexed");
        assert_eq!(m.expires.as_deref(), Some("2026-12-01"));
        assert_eq!(m.owner.as_deref(), Some("platform-admin"));
    }

    #[test]
    fn rules_without_metadata_are_not_indexed() {
        // Only rules that actually carry metadata land in the index, so a
        // surface can treat "present" as "this grant is annotated".
        let idx = IntentIndex::parse(INTENTS);
        assert!(idx.get("all → infra (web/dns/fabric tcp)").is_none());
        assert_eq!(idx.len(), 1);
    }

    #[test]
    fn metadata_does_not_leak_to_the_next_rule() {
        // The scan is stateful; the bug it must not have is carrying one
        // rule's expiry onto the rule declared after it.
        let idx = IntentIndex::parse(INTENTS);
        assert_eq!(idx.len(), 1, "exactly one rule carries metadata");
    }

    #[test]
    fn an_unparseable_intents_file_yields_an_empty_index() {
        assert!(IntentIndex::parse("}{ not yaml at all").is_empty());
    }
}
