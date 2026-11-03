// Unit tests for the admission-policy key computation. Run with `cargo test`.
//
// These cover the ADR 1020 boundary behavior that matters most for correctness: quarantine
// rewriting, passthrough allow-listing, and the prefix matcher's edge cases. They need no
// Zenoh session, so they run in CI without a fabric.

#[cfg(test)]
mod tests {
    use crate::config::{key_matches_prefix, AdmitMode, Config};

    fn cfg(mode: AdmitMode, allow: &[&str]) -> Config {
        Config {
            peering_entity: "org112x".into(),
            peering_id: "peering-7f3a".into(),
            admit_mode: mode,
            passthrough_allow: allow.iter().map(|s| s.to_string()).collect(),
            ingress_key_expr: "**".into(),
            partner_connect_endpoint: "tls/partner:7447".into(),
            partner_tls_root_ca: "/x".into(),
            partner_tls_client_cert: "/x".into(),
            partner_tls_client_key: "/x".into(),
            core_connect_endpoint: "tls/core:7447".into(),
            core_tls_root_ca: "/x".into(),
            core_tls_client_cert: "/x".into(),
            core_tls_client_key: "/x".into(),
            provenance_ndjson_path: None,
        }
    }

    #[test]
    fn quarantine_rewrites_every_key() {
        let c = cfg(AdmitMode::Quarantine, &[]);
        // The lattice-flood worked example from ADR 1020.
        assert_eq!(
            c.admitted_key("org11x/lattice/entity/track/v1"),
            "org112x/peering-7f3a/org11x/lattice/entity/track/v1"
        );
    }

    #[test]
    fn quarantine_ignores_passthrough_allow() {
        // Even if an allow-list is set, quarantine mode rewrites everything.
        let c = cfg(AdmitMode::Quarantine, &["release/org112x/**"]);
        assert_eq!(
            c.admitted_key("release/org112x/tracks/v1"),
            "org112x/peering-7f3a/release/org112x/tracks/v1"
        );
    }

    #[test]
    fn passthrough_admits_allowed_prefix_verbatim() {
        let c = cfg(AdmitMode::Passthrough, &["release/org112x/**"]);
        assert_eq!(
            c.admitted_key("release/org112x/tracks/v1"),
            "release/org112x/tracks/v1"
        );
    }

    #[test]
    fn passthrough_quarantines_non_allowed_key() {
        // An onboarded pod that tries to write OUTSIDE its first-party prefix is quarantined,
        // not admitted first-class — the core ACL is the hard backstop, this is defense in depth.
        let c = cfg(AdmitMode::Passthrough, &["release/org112x/**"]);
        assert_eq!(
            c.admitted_key("notional/secret/v1"),
            "org112x/peering-7f3a/notional/secret/v1"
        );
    }

    #[test]
    fn prefix_matcher_edges() {
        assert!(key_matches_prefix("a/b/c", "a/b/**"));
        assert!(key_matches_prefix("a/b", "a/b/**")); // the base itself
        assert!(key_matches_prefix("a/b", "a/b")); // exact
        assert!(!key_matches_prefix("a/bc", "a/b/**")); // sibling, not a child
        assert!(!key_matches_prefix("a/b/c", "a/b")); // exact must not match descendants
    }
}
