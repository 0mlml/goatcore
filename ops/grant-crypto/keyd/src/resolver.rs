//! The launch `AudienceResolver` — operator-declared members + F-267 cert-dir keys.
//!
//! Composes the two halves the portal does not give in one call: the member *list*
//! (from config, `release_sets`) and each member's *key* (from a [`RecipientKeys`]
//! source, the cert dir at launch). The grant-row enumeration that replaces the
//! config-declared list later implements the same [`AudienceResolver`] trait, so
//! the daemon loop is untouched by that swap.
//!
//! **Fail-closed:** if any declared member's key cannot be resolved, the whole
//! audience fails — never a partial audience, which would silently drop an
//! authorized member from the bundle (a silent denial). A failed resolve aborts
//! the rotation; the previous generation stays valid.

use std::collections::BTreeMap;

use grant_crypto::service::{AudienceResolver, Member, ServiceError};

use crate::recipient::RecipientKeys;

/// An `AudienceResolver` over an operator-declared member map and a key source.
pub struct ConfiguredAudience<R: RecipientKeys> {
    members_by_rs: BTreeMap<String, Vec<String>>,
    keys: R,
}

impl<R: RecipientKeys> ConfiguredAudience<R> {
    pub fn new(members_by_rs: BTreeMap<String, Vec<String>>, keys: R) -> Self {
        Self {
            members_by_rs,
            keys,
        }
    }
}

impl<R: RecipientKeys> AudienceResolver for ConfiguredAudience<R> {
    fn resolve(&self, release_set: &str) -> Result<Vec<Member>, ServiceError> {
        let ids = self.members_by_rs.get(release_set).ok_or_else(|| {
            ServiceError::Resolve(format!(
                "no audience declared for release-set {release_set:?}"
            ))
        })?;
        let mut members = Vec::with_capacity(ids.len());
        for id in ids {
            let key = self.keys.key(id)?; // fail-closed: a missing key aborts the audience
            members.push(Member {
                id: id.clone(),
                key,
            });
        }
        Ok(members)
    }
}

/// The grant-row `AudienceResolver` — the member *list* comes from the portal's
/// audience endpoint (`GET /api/access/audience`, Piece B), the member *key* from
/// the same [`RecipientKeys`] source as [`ConfiguredAudience`]. This is what makes
/// a data owner's `goat access grant` drive the audience instead of an operator
/// editing `release_sets` in config: the endpoint derives the member anchors from
/// in-effect grant rows (grant-keyed-encryption §3a). Swapping it in for
/// `ConfiguredAudience` leaves the daemon loop untouched — only the list source
/// changes. Fail-closed: any member whose key can't be resolved aborts the whole
/// audience (never a partial/silent-denial bundle).
pub struct GrantRowAudienceResolver<R: RecipientKeys> {
    client: crate::directory::AudienceClient,
    keys: R,
}

impl<R: RecipientKeys> GrantRowAudienceResolver<R> {
    pub fn new(client: crate::directory::AudienceClient, keys: R) -> Self {
        Self { client, keys }
    }
}

impl<R: RecipientKeys> AudienceResolver for GrantRowAudienceResolver<R> {
    fn resolve(&self, release_set: &str) -> Result<Vec<Member>, ServiceError> {
        let audience = self.client.audience(release_set)?;
        let mut members = Vec::with_capacity(audience.members.len());
        for id in &audience.members {
            let key = self.keys.key(id)?; // fail-closed: a missing key aborts the audience
            members.push(Member {
                id: id.clone(),
                key,
            });
        }
        Ok(members)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::recipient::CertDirRecipientKeys;
    use p256::elliptic_curve::Generate;
    use p256::pkcs8::EncodePublicKey;
    use p256::SecretKey;
    use std::path::PathBuf;

    fn certdir_with(ids: &[&str]) -> (PathBuf, Vec<SecretKey>) {
        let seed = hex::encode(&SecretKey::generate().to_bytes()[..8]);
        let dir = std::env::temp_dir().join(format!("keyd-aud-{seed}"));
        std::fs::create_dir_all(&dir).unwrap();
        let mut secrets = vec![];
        for id in ids {
            let s = SecretKey::generate();
            let pem = s
                .public_key()
                .to_public_key_pem(Default::default())
                .unwrap();
            std::fs::write(dir.join(format!("{id}.pem")), pem).unwrap();
            secrets.push(s);
        }
        (dir, secrets)
    }

    #[test]
    fn resolves_declared_members_to_their_keys() {
        let (dir, secrets) = certdir_with(&["a", "b"]);
        let mut map = BTreeMap::new();
        map.insert("rs".to_string(), vec!["a".to_string(), "b".to_string()]);
        let aud = ConfiguredAudience::new(map, CertDirRecipientKeys::new(&dir));

        let members = aud.resolve("rs").expect("resolve");
        assert_eq!(members.len(), 2);
        assert_eq!(members[0].id, "a");
        assert_eq!(members[0].key, secrets[0].public_key());
        assert_eq!(members[1].key, secrets[1].public_key());
    }

    #[test]
    fn unknown_release_set_errors() {
        let (dir, _) = certdir_with(&["a"]);
        let mut map = BTreeMap::new();
        map.insert("rs".to_string(), vec!["a".to_string()]);
        let aud = ConfiguredAudience::new(map, CertDirRecipientKeys::new(&dir));
        assert!(aud.resolve("nope").is_err());
    }

    #[test]
    fn a_missing_member_key_fails_the_whole_audience() {
        // 'b' is declared but has no PEM — fail-closed, no partial audience.
        let (dir, _) = certdir_with(&["a"]);
        let mut map = BTreeMap::new();
        map.insert("rs".to_string(), vec!["a".to_string(), "b".to_string()]);
        let aud = ConfiguredAudience::new(map, CertDirRecipientKeys::new(&dir));
        assert!(
            aud.resolve("rs").is_err(),
            "must not return a partial audience"
        );
    }

    // --- GrantRowAudienceResolver (Piece B): member list from the portal ---

    use crate::directory::AudienceClient;
    use std::io::{Read, Write};
    use std::net::TcpListener;
    use std::thread;

    // A one-shot HTTP stub returning `body` as the audience response (real ureq →
    // real TCP → real JSON parse), mirroring the directory-client tests.
    fn audience_stub(body: &'static str) -> (String, thread::JoinHandle<()>) {
        let listener = TcpListener::bind("127.0.0.1:0").expect("bind");
        let addr = listener.local_addr().unwrap();
        let handle = thread::spawn(move || {
            let (mut sock, _) = listener.accept().expect("accept");
            let mut buf = [0u8; 4096];
            let _ = sock.read(&mut buf);
            let resp = format!(
                "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{body}",
                body.len()
            );
            let _ = sock.write_all(resp.as_bytes());
            let _ = sock.flush();
        });
        (format!("http://{addr}"), handle)
    }

    #[test]
    fn grant_row_resolver_wraps_the_endpoint_members_to_their_keys() {
        // The cert dir holds keys for a + b; the endpoint says the audience is a + b.
        let (dir, secrets) = certdir_with(&["a", "b"]);
        let (base, handle) =
            audience_stub(r#"{"release_set":"rs","members":["a","b"],"notes":[]}"#);
        let resolver = GrantRowAudienceResolver::new(
            AudienceClient::new(base, "tok"),
            CertDirRecipientKeys::new(&dir),
        );

        let members = resolver.resolve("rs").expect("resolve from endpoint");
        assert_eq!(members.len(), 2);
        assert_eq!(members[0].id, "a");
        assert_eq!(members[0].key, secrets[0].public_key());
        assert_eq!(members[1].key, secrets[1].public_key());
        handle.join().unwrap();
    }

    #[test]
    fn grant_row_resolver_is_fail_closed_on_a_missing_key() {
        // The endpoint names 'b', but only 'a' has a key on disk → the whole
        // audience fails (no partial/silent-denial bundle).
        let (dir, _) = certdir_with(&["a"]);
        let (base, handle) =
            audience_stub(r#"{"release_set":"rs","members":["a","b"],"notes":[]}"#);
        let resolver = GrantRowAudienceResolver::new(
            AudienceClient::new(base, "tok"),
            CertDirRecipientKeys::new(&dir),
        );
        assert!(
            resolver.resolve("rs").is_err(),
            "must not return a partial audience"
        );
        handle.join().unwrap();
    }
}
