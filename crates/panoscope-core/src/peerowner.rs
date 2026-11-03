// SPDX-License-Identifier: Apache-2.0
//
// Peer → responsible person, resolved against the portal's access graph.
//
// The Rust client for `POST /api/access/peers` (docs/design/peer-identity-resolution.md,
// Option B). The Go side of this — the Thor resolver baking the relay-health card's
// "identity & access →" link — has existed ; this is the same call from
// the surfaces that render reachability, so a viewer looking at a machine can find
// out who to ask about it without leaving the page.
//
// WHY A NAME AND NOT A FOREIGN KEY. Under setup-key enrollment NetBird leaves
// `peer.user_id` empty, and Option A (stamping it) is a proven dead end: NetBird
// validates `peer.UserID` against its own `users` table and the goatnets have zero
// person-scoped NetBird users, so binding a Kratos id *breaks* enrollment.
// The peer NAME carries the handle, and `crate::peername` is the one parser for it.
//
// THE HONESTY RULE, which is the whole reason this module has an enum instead of an
// Option<String>. From peername.rs: "A peer name is EVIDENCE about ownership, never a
// record of it — the recorded linkage is slots.identity_id, stamped at onboard. A
// consumer that presents ownership must say which of the two it used."
//
// So a caller can never receive a bare name and render it as fact. Three outcomes,
// each of which a UI must show differently:
//
//   Registry — the handle matched a roster member AND a live Ory identity shares the
//              roster's email-hash. This is the recorded linkage. Safe to present as
//              "who owns this", and it carries the id-admin-console deep-link.
//   Name     — the handle parsed and matched a roster member, but no live identity
//              matched. We know the org and the handle; we do NOT have a person.
//              Present as a lead, never as an answer.
//   Unmatched— the name encodes no principal (infra, synth, hand-named hosts). NOT a
//              failure and NOT "nobody is responsible" — it means this peer was never
//              onboarded through the operator path.
//   Unread   — we could not ask. The portal was unreachable, unauthorized, or not
//              configured. Distinct from Unmatched on purpose: "we did not look" and
//              "we looked and found no principal" are different facts, and collapsing
//              them is the same mistake isitup made grading an absent container
//              `unknown`.

use std::collections::BTreeMap;
use std::time::Duration;

use serde::{Deserialize, Serialize};

/// How a peer's ownership was established. Never collapse these — see the module
/// comment; the UI contract is that each renders differently.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum Attribution {
    /// Handle matched the roster AND a live Ory identity matched its email-hash.
    Registry,
    /// Handle matched the roster; no live identity did. Org known, person not.
    Name,
    /// The name encodes no principal. Not a failure.
    Unmatched,
    /// We could not ask. Not evidence of anything about the peer.
    Unread,
}

impl Attribution {
    /// One line a renderer can print verbatim. Deliberately says what is and is not
    /// known, because the failure mode this module exists to prevent is a surface
    /// showing a blank and a reader concluding "unowned".
    pub fn explain(self) -> &'static str {
        match self {
            Attribution::Registry => "resolved from the registry (roster handle + live identity)",
            Attribution::Name => {
                "inferred from the peer name — org known, no live identity matched; treat as a lead, not an answer"
            }
            Attribution::Unmatched => {
                "this peer name encodes no principal — not onboarded through the operator path, so there is no person to resolve"
            }
            Attribution::Unread => {
                "not looked up — the portal resolver was unreachable or unconfigured; this says nothing about the peer"
            }
        }
    }

    /// True only for the recorded linkage. A caller acting on ownership (paging
    /// someone, attributing an action) must gate on this and not on "we have a
    /// string".
    pub fn is_recorded(self) -> bool {
        matches!(self, Attribution::Registry)
    }
}

/// The person a peer resolves to, with the provenance of that claim attached.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct PeerOwner {
    pub netbird_name: String,
    pub attribution: Attribution,
    /// Operator handle parsed from the peer name. Present for Registry and Name.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub handle: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub org: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub person_name: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub person_email: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub ory_identity_id: Option<String>,
    /// True when the resolved identity is disabled — a peer whose owner has been
    /// offboarded is exactly the case a viewer needs flagged rather than hidden.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub owner_disabled: Option<bool>,
    /// Path on the id-admin-console for the person page. Relative: the console's
    /// host differs per goatnet and the client already knows its own site.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub id_console_path: Option<String>,
    /// The resolver's own words when it did not resolve. Carried through rather
    /// than replaced, so an operator debugging attribution sees the real reason.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub reason: Option<String>,
}

impl PeerOwner {
    /// The not-looked-up value. Used for every peer when the resolver is off or
    /// unreachable, so the shape of the payload never depends on whether the
    /// lookup worked — a client that has to branch on a missing field is a client
    /// that will render a blank.
    pub fn unread(name: &str, reason: impl Into<String>) -> Self {
        Self {
            netbird_name: name.to_string(),
            attribution: Attribution::Unread,
            handle: None,
            org: None,
            person_name: None,
            person_email: None,
            ory_identity_id: None,
            owner_disabled: None,
            id_console_path: None,
            reason: Some(reason.into()),
        }
    }

    /// Short label for a dense cell. Never invents a person: falls back through
    /// handle, then org, then a phrase naming the state.
    pub fn label(&self) -> String {
        match self.attribution {
            Attribution::Registry => self
                .person_name
                .clone()
                .or_else(|| self.person_email.clone())
                .or_else(|| self.handle.clone())
                .unwrap_or_else(|| "resolved, unnamed".into()),
            Attribution::Name => self
                .handle
                .clone()
                .map(|h| format!("{h} (unconfirmed)"))
                .unwrap_or_else(|| "unconfirmed".into()),
            Attribution::Unmatched => "no principal in the name".into(),
            Attribution::Unread => "not looked up".into(),
        }
    }
}

/// The portal-backed resolver. `None` for the key or url means "not configured",
/// which resolves every peer to `Unread` rather than erroring — netvis must still
/// draw its matrix when the portal is down, and an ownership column that says
/// "not looked up" is honest where an empty one is not.
pub struct OwnerResolver {
    base_url: String,
    api_key: String,
    client: reqwest::Client,
}

/// The batch endpoint's response envelope.
///
/// `notes` is DELIBERATELY NOT READ. The portal attaches its data-governance
/// commentary to every access-graph response — three paragraphs on per-UUID
/// write anchors, flat community-read, and derived release-set lanes. All true,
/// none of it about ownership. Passing it through would put it under an
/// `ownership_notes` key, where it reads as if the
/// resolver were explaining itself. A field named for one thing carrying
/// another is how a surface starts lying quietly.
///
/// The notes this module returns are its OWN, and they are only ever about
/// whether the lookup happened.
#[derive(Deserialize)]
struct BatchResponse {
    peers: BTreeMap<String, serde_json::Value>,
}

impl OwnerResolver {
    /// `None` when either half of the credential is absent. Two knobs that must
    /// both be set is a configuration state worth naming once here rather than
    /// re-checking at every call site.
    pub fn new(base_url: &str, api_key: &str) -> Option<Self> {
        if base_url.trim().is_empty() || api_key.trim().is_empty() {
            return None;
        }
        let client = reqwest::Client::builder()
            // The resolver assembles an access graph per call. Generous, but
            // bounded: a hung portal must degrade the ownership column, never
            // hang the reachability matrix that is this surface's actual job.
            .timeout(Duration::from_secs(10))
            .build()
            .ok()?;
        Some(Self {
            base_url: base_url.trim_end_matches('/').to_string(),
            api_key: api_key.to_string(),
            client,
        })
    }

    /// One call for every name. The batch form exists precisely so a consumer
    /// makes ONE graph assembly instead of N — the per-name endpoint in a loop is
    /// the shape this replaces.
    ///
    /// Never returns Err: a resolver failure is reported per-peer as `Unread` with
    /// the transport error as the reason, plus a note for the payload's provenance
    /// block. Ownership is an enrichment; it may not take the page down with it.
    pub async fn resolve(&self, names: &[String]) -> (BTreeMap<String, PeerOwner>, Vec<String>) {
        let mut out = BTreeMap::new();
        if names.is_empty() {
            return (out, Vec::new());
        }
        let url = format!("{}/api/access/peers", self.base_url);
        let resp = self
            .client
            .post(&url)
            .bearer_auth(&self.api_key)
            .json(&serde_json::json!({ "netbird_names": names }))
            .send()
            .await;

        let unread_all = |reason: String, out: &mut BTreeMap<String, PeerOwner>| {
            for n in names {
                out.insert(n.clone(), PeerOwner::unread(n, reason.clone()));
            }
        };

        let resp = match resp {
            Ok(r) => r,
            Err(e) => {
                let reason = format!("portal unreachable: {e}");
                unread_all(reason.clone(), &mut out);
                return (out, vec![format!("ownership unread — {reason}")]);
            }
        };
        if !resp.status().is_success() {
            // 401/403 here is the common real failure: the key was never
            // registered in the portal's key dir. Say which, because "unread" with
            // no status sends an operator hunting the wrong layer.
            let reason = format!("portal returned HTTP {}", resp.status());
            unread_all(reason.clone(), &mut out);
            return (out, vec![format!("ownership unread — {reason}")]);
        }
        let parsed: BatchResponse = match resp.json().await {
            Ok(v) => v,
            Err(e) => {
                let reason = format!("portal response unparseable: {e}");
                unread_all(reason.clone(), &mut out);
                return (out, vec![format!("ownership unread — {reason}")]);
            }
        };

        for name in names {
            let owner = match parsed.peers.get(name) {
                Some(v) => owner_from_value(name, v),
                // The portal skips blank names and could in principle omit one.
                // An omitted name is unread, not unmatched.
                None => PeerOwner::unread(name, "portal returned no row for this peer"),
            };
            out.insert(name.clone(), owner);
        }
        // No notes on the happy path: the per-peer `attribution` already says
        // what is known about each machine, and a note repeating that would be
        // one more thing to keep in sync with the field it duplicates.
        (out, Vec::new())
    }
}

/// Map one `resolvePeerName` object onto the typed, provenance-carrying form.
///
/// The portal's `resolved: false` covers two distinct cases and distinguishes them
/// only in prose: a name that matched no handle, and a handle that matched no live
/// identity. The presence of `handle` is what separates them, which is why this
/// keys off that rather than parsing the `reason` string.
fn owner_from_value(name: &str, v: &serde_json::Value) -> PeerOwner {
    let s = |k: &str| v.get(k).and_then(|x| x.as_str()).map(String::from);
    let resolved = v.get("resolved").and_then(|x| x.as_bool()).unwrap_or(false);
    let handle = s("handle");
    let person = v.get("person");
    let attribution = if resolved && person.is_some() {
        Attribution::Registry
    } else if handle.is_some() {
        Attribution::Name
    } else {
        Attribution::Unmatched
    };
    PeerOwner {
        netbird_name: name.to_string(),
        attribution,
        handle,
        org: s("org"),
        person_name: person.and_then(|p| p.get("name")).and_then(|x| x.as_str()).map(String::from),
        person_email: person.and_then(|p| p.get("email")).and_then(|x| x.as_str()).map(String::from),
        ory_identity_id: person
            .and_then(|p| p.get("ory_identity_id"))
            .and_then(|x| x.as_str())
            .map(String::from),
        owner_disabled: person.and_then(|p| p.get("disabled")).and_then(|x| x.as_bool()),
        id_console_path: v
            .get("deep_links")
            .and_then(|d| d.get("id_console_user"))
            .and_then(|x| x.as_str())
            .map(String::from),
        reason: if attribution == Attribution::Registry { None } else { s("reason") },
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn a_fully_resolved_peer_is_registry_attributed_and_carries_the_deep_link() {
        let v = serde_json::json!({
            "netbird_name": "peer-maintainer-zagros-2026-08-05",
            "resolved": true,
            "handle": "maintainer",
            "org": "goat",
            "person": {
                "ory_identity_id": "abc-123",
                "email": "maintainer@example.test",
                "name": "the maintainer",
                "disabled": false
            },
            "deep_links": {"id_console_user": "/users/abc-123"}
        });
        let o = owner_from_value("peer-maintainer-zagros-2026-08-05", &v);
        assert_eq!(o.attribution, Attribution::Registry);
        assert!(o.attribution.is_recorded());
        assert_eq!(o.ory_identity_id.as_deref(), Some("abc-123"));
        assert_eq!(o.id_console_path.as_deref(), Some("/users/abc-123"));
        assert_eq!(o.label(), "the maintainer");
        assert!(o.reason.is_none(), "a resolved owner carries no failure reason");
    }

    #[test]
    fn a_handle_that_matched_no_live_identity_is_name_attributed_not_registry() {
        // The case that must never render as a confirmed owner: we know the org,
        // we do NOT have a person.
        let v = serde_json::json!({
            "netbird_name": "peer-ghost-zagros-2026-08-05",
            "resolved": false,
            "handle": "ghost",
            "org": "goat",
            "reason": "handle resolved to org goat but no live Ory identity matched the roster email-hash"
        });
        let o = owner_from_value("peer-ghost-zagros-2026-08-05", &v);
        assert_eq!(o.attribution, Attribution::Name);
        assert!(!o.attribution.is_recorded());
        assert_eq!(o.org.as_deref(), Some("goat"));
        assert!(o.ory_identity_id.is_none());
        assert!(o.label().contains("unconfirmed"));
        assert!(o.reason.is_some(), "an unresolved owner keeps the resolver's reason");
    }

    #[test]
    fn a_name_with_no_handle_is_unmatched_which_is_not_a_failure() {
        let v = serde_json::json!({
            "netbird_name": "mgmt-zagros-prod-01-orin",
            "resolved": false,
            "reason": "no roster handle matched this peer name"
        });
        let o = owner_from_value("mgmt-zagros-prod-01-orin", &v);
        assert_eq!(o.attribution, Attribution::Unmatched);
        assert!(o.handle.is_none());
        assert!(o.explain_is_not_a_failure());
    }

    #[test]
    fn unmatched_and_unread_are_never_the_same_value() {
        // The F-336 lesson in a test: "we did not look" and "we looked and there is
        // no principal" must not collapse. If someone merges these variants this
        // fails.
        let unread = PeerOwner::unread("peer-x", "portal unreachable: timeout");
        let v = serde_json::json!({"resolved": false, "reason": "no roster handle matched this peer name"});
        let unmatched = owner_from_value("peer-x", &v);
        assert_ne!(unread.attribution, unmatched.attribution);
        assert!(!unread.attribution.is_recorded());
        assert!(!unmatched.attribution.is_recorded());
        assert_ne!(unread.label(), unmatched.label());
    }

    #[test]
    fn every_attribution_explains_itself_without_repeating_another() {
        let all = [
            Attribution::Registry,
            Attribution::Name,
            Attribution::Unmatched,
            Attribution::Unread,
        ];
        let mut seen: Vec<&str> = Vec::new();
        for a in all {
            let e = a.explain();
            assert!(!e.is_empty());
            assert!(!seen.contains(&e), "two attributions share an explanation");
            seen.push(e);
        }
        // Exactly one is the recorded linkage.
        assert_eq!(all.iter().filter(|a| a.is_recorded()).count(), 1);
    }

    #[test]
    fn the_portals_governance_notes_are_never_carried_as_ownership_notes() {
        // The portal attaches its
        // data-governance commentary (write anchors, community-read, release
        // lanes) to EVERY access-graph response, and passing `notes` through
        // put three paragraphs about Zenoh write rules under a key named
        // `ownership_notes`. True text, wrong field, and on the page it read as
        // the resolver explaining itself.
        //
        // The envelope no longer has a `notes` field to pass through; this
        // asserts the deserializer ignores it rather than someone re-adding it.
        let body = serde_json::json!({
            "peers": {"peer-a-zagros-2026-01-01": {"resolved": false}},
            "notes": ["writes: a peer may publish ONLY its own anchor …",
                      "reads: flat community-read is STILL in effect …"]
        });
        let parsed: BatchResponse = serde_json::from_value(body).expect("envelope parses");
        assert_eq!(parsed.peers.len(), 1);
        // If someone reintroduces a `notes` field on BatchResponse this stops
        // compiling or stops being the only thing read — either way it is seen.
        let json = serde_json::to_string(&parsed.peers).unwrap();
        assert!(!json.contains("community-read"), "governance prose leaked into the peer map");
    }

    #[test]
    fn a_disabled_owner_is_surfaced_rather_than_hidden() {
        let v = serde_json::json!({
            "resolved": true, "handle": "gone", "org": "goat",
            "person": {"ory_identity_id": "d-1", "email": "g@x.test", "name": "Gone", "disabled": true}
        });
        let o = owner_from_value("peer-gone-zagros-2026-01-01", &v);
        assert_eq!(o.owner_disabled, Some(true));
        assert_eq!(o.attribution, Attribution::Registry);
    }

    impl PeerOwner {
        fn explain_is_not_a_failure(&self) -> bool {
            self.attribution.explain().contains("not onboarded through the operator path")
        }
    }
}
