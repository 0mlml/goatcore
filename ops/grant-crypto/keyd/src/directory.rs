//! The portal role/member resolver client — the audience's *identity* half.
//!
//! `GET /api/access/data-roles` (goat-zenoh-portal, ADR 1074/1075) is the built,
//! authoritative answer to "what can this principal see": it returns the
//! principal's evaluated roles + producer set. E1 drives audience membership from
//! this plus the grant store.
//!
//! What it deliberately does NOT return: **any key material**. It answers per
//! *principal* and yields ids/producers only — no cert, no SPKI. Turning a member
//! id into a recipient public key is a separate concern ([`crate::recipient`], the
//! F-267 gap), and enumerating a release-set's full member set is assembled from
//! grant rows, not a single call. Keeping this client to exactly the endpoint's
//! real contract is the point — it is honest about the seam boundary.
//!
//! Auth: a bearer token (an operator API key or the console↔portal service token),
//! sent as `Authorization: Bearer <token>` — matched in-handler by the portal
//! (`whoami.go` `lookupAuthorized`). Internal consumers only; fails closed.

use serde::Deserialize;

use grant_crypto::service::ServiceError;

/// Which principal to resolve. Exactly one selector per call — the endpoint
/// rejects zero or multiple with HTTP 400.
pub enum Selector {
    /// An Ory/Kratos identity UUID (a person's principal key).
    Identity(String),
    /// A login email — a portal-resolved alias for the identity UUID, never a
    /// stored join key.
    Email(String),
    /// A machine principal / producer write-anchor.
    Anchor(String),
}

impl Selector {
    /// The query parameter (name, value) this selector maps to.
    fn param(&self) -> (&'static str, &str) {
        match self {
            Selector::Identity(v) => ("identity", v),
            Selector::Email(v) => ("email", v),
            Selector::Anchor(v) => ("anchor", v),
        }
    }
}

/// The resolved principal identity (mirrors the portal `dataRolePrincipal`).
#[derive(Debug, Clone, Deserialize)]
pub struct Principal {
    pub id: String,
    /// `person` or `machine`.
    pub kind: String,
}

/// The `GET /api/access/data-roles` response (mirrors the portal
/// `dataRolesResponse`). Only the fields E1 consumes are modeled; unknown fields
/// (e.g. `freshness`) are ignored. `roles`/`producers` are normalized to `[]` by
/// the portal, never null.
#[derive(Debug, Clone, Deserialize)]
pub struct DataRoles {
    pub principal: Principal,
    #[serde(default)]
    pub roles: Vec<String>,
    #[serde(default)]
    pub producers: Vec<String>,
    #[serde(default)]
    pub derived_at: String,
    #[serde(default)]
    pub notes: Vec<String>,
}

/// A client for the portal data-roles resolver.
pub struct DataRolesClient {
    base_url: String,
    token: String,
    agent: ureq::Agent,
}

impl DataRolesClient {
    /// `base_url` is the portal origin (e.g. `https://portal.zagros.…`), no trailing
    /// slash; `token` is the bearer credential.
    pub fn new(base_url: impl Into<String>, token: impl Into<String>) -> Self {
        Self {
            base_url: base_url.into().trim_end_matches('/').to_string(),
            token: token.into(),
            agent: ureq::agent(),
        }
    }

    /// Resolve one principal's data-roles. Maps every failure — transport, non-2xx,
    /// or malformed body — to `ServiceError::Resolve`; the resolver is a hard
    /// dependency and a failure aborts the rotation (fail-closed).
    pub fn data_roles(&self, selector: &Selector) -> Result<DataRoles, ServiceError> {
        let (k, v) = selector.param();
        let url = format!("{}/api/access/data-roles", self.base_url);
        let resp = self
            .agent
            .get(&url)
            .set("Authorization", &format!("Bearer {}", self.token))
            .query(k, v)
            .call()
            .map_err(|e| match e {
                ureq::Error::Status(code, _) => {
                    ServiceError::Resolve(format!("data-roles {k}={v}: HTTP {code}"))
                }
                other => ServiceError::Resolve(format!("data-roles {k}={v}: {other}")),
            })?;
        resp.into_json::<DataRoles>()
            .map_err(|e| ServiceError::Resolve(format!("data-roles {k}={v}: bad body: {e}")))
    }
}

/// The `GET /api/access/audience` response (mirrors the portal `audienceResponse`).
/// `members` is the release-set's machine-plane audience: the member **anchors**
/// the DEK is wrapped to, derived server-side from in-effect grant rows (Piece B,
/// grant-keyed-encryption §3a). Only the fields E1 consumes are modeled.
#[derive(Debug, Clone, Deserialize)]
pub struct Audience {
    #[serde(default)]
    pub release_set: String,
    #[serde(default)]
    pub members: Vec<String>,
    #[serde(default)]
    pub notes: Vec<String>,
}

/// A client for the portal audience endpoint — the server half of the grant-row
/// audience resolver. Same origin + bearer auth as [`DataRolesClient`].
pub struct AudienceClient {
    base_url: String,
    token: String,
    agent: ureq::Agent,
}

impl AudienceClient {
    pub fn new(base_url: impl Into<String>, token: impl Into<String>) -> Self {
        Self {
            base_url: base_url.into().trim_end_matches('/').to_string(),
            token: token.into(),
            agent: ureq::agent(),
        }
    }

    /// Resolve a release-set's member anchors. Every failure — transport, non-2xx,
    /// malformed body — maps to `ServiceError::Resolve` so a failure aborts the
    /// rotation (fail-closed: never wrap to a partial audience).
    pub fn audience(&self, release_set: &str) -> Result<Audience, ServiceError> {
        let url = format!("{}/api/access/audience", self.base_url);
        let resp = self
            .agent
            .get(&url)
            .set("Authorization", &format!("Bearer {}", self.token))
            .query("release_set", release_set)
            .call()
            .map_err(|e| match e {
                ureq::Error::Status(code, _) => {
                    ServiceError::Resolve(format!("audience {release_set}: HTTP {code}"))
                }
                other => ServiceError::Resolve(format!("audience {release_set}: {other}")),
            })?;
        resp.into_json::<Audience>()
            .map_err(|e| ServiceError::Resolve(format!("audience {release_set}: bad body: {e}")))
    }

    /// Fetch a member's current PUBLIC leaf cert PEM from the portal's by-anchor
    /// recipient-cert store (`GET /api/access/recipient-cert`, the U7
    /// publish-my-key distribution). Distribution only — the caller MUST still
    /// validate the PEM (chain to the portal-CA root + CN/URI-SAN binding)
    /// before wrapping to it; a portal that serves the wrong cert must not be
    /// able to redirect a wrap.
    pub fn recipient_cert(&self, anchor: &str) -> Result<String, ServiceError> {
        let url = format!("{}/api/access/recipient-cert", self.base_url);
        let resp = self
            .agent
            .get(&url)
            .set("Authorization", &format!("Bearer {}", self.token))
            .query("anchor", anchor)
            .call()
            .map_err(|e| match e {
                ureq::Error::Status(code, _) => {
                    ServiceError::Resolve(format!("recipient-cert {anchor}: HTTP {code}"))
                }
                other => ServiceError::Resolve(format!("recipient-cert {anchor}: {other}")),
            })?;
        resp.into_string()
            .map_err(|e| ServiceError::Resolve(format!("recipient-cert {anchor}: bad body: {e}")))
    }

    /// The release-sets to sweep = the producers carrying an active grant
    /// (`GET /api/access/release-sets`). In grant-row mode the daemon calls this
    /// each sweep so a `goat access grant` alone puts a producer in scope — no
    /// operator config edit (WS4 auto-sweep-list). Fail-closed on any error.
    pub fn release_sets(&self) -> Result<Vec<String>, ServiceError> {
        let url = format!("{}/api/access/release-sets", self.base_url);
        let resp = self
            .agent
            .get(&url)
            .set("Authorization", &format!("Bearer {}", self.token))
            .call()
            .map_err(|e| match e {
                ureq::Error::Status(code, _) => {
                    ServiceError::Resolve(format!("release-sets: HTTP {code}"))
                }
                other => ServiceError::Resolve(format!("release-sets: {other}")),
            })?;
        resp.into_json::<ReleaseSets>()
            .map(|r| r.release_sets)
            .map_err(|e| ServiceError::Resolve(format!("release-sets: bad body: {e}")))
    }
}

/// The `GET /api/access/release-sets` response (mirrors the portal
/// `releaseSetsResponse`) — the distinct producers with an active grant.
#[derive(Debug, Clone, Deserialize)]
pub struct ReleaseSets {
    #[serde(default)]
    pub release_sets: Vec<String>,
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::io::{Read, Write};
    use std::net::TcpListener;
    use std::thread;

    // A one-shot HTTP/1.1 stub: accept one connection, capture the request line +
    // headers, reply with `status` and `body`. Returns (base_url, join-handle that
    // yields the raw request). Real ureq client → real TCP → real JSON parse.
    fn stub(status: &str, body: &'static str) -> (String, thread::JoinHandle<String>) {
        let listener = TcpListener::bind("127.0.0.1:0").expect("bind");
        let addr = listener.local_addr().unwrap();
        let status = status.to_string();
        let handle = thread::spawn(move || {
            let (mut sock, _) = listener.accept().expect("accept");
            let mut buf = [0u8; 4096];
            let n = sock.read(&mut buf).unwrap_or(0);
            let req = String::from_utf8_lossy(&buf[..n]).to_string();
            let resp = format!(
                "HTTP/1.1 {status}\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{body}",
                body.len()
            );
            let _ = sock.write_all(resp.as_bytes());
            let _ = sock.flush();
            req
        });
        (format!("http://{addr}"), handle)
    }

    #[test]
    fn parses_a_data_roles_response_and_sends_the_selector_and_auth() {
        let body = r#"{
            "principal": {"id": "11111111-2222-3333-4444-555555555555", "kind": "person"},
            "roles": ["data.view.org:NLD", "data.view.producer:abc"],
            "producers": ["abc", "def"],
            "derived_at": "2026-07-29T00:00:00Z",
            "freshness": {"ignored": true},
            "notes": ["heads up"]
        }"#;
        let (base, handle) = stub("200 OK", body);
        let client = DataRolesClient::new(base, "sekret");

        let dr = client
            .data_roles(&Selector::Identity(
                "11111111-2222-3333-4444-555555555555".into(),
            ))
            .expect("resolve ok");

        assert_eq!(dr.principal.kind, "person");
        assert_eq!(dr.roles.len(), 2);
        assert_eq!(dr.producers, vec!["abc", "def"]);
        assert_eq!(dr.notes, vec!["heads up"]);

        // The request carried the selector as a query param and the bearer token.
        let req = handle.join().unwrap();
        assert!(
            req.contains(
                "GET /api/access/data-roles?identity=11111111-2222-3333-4444-555555555555"
            ),
            "request line was: {}",
            req.lines().next().unwrap_or("")
        );
        assert!(
            req.contains("Authorization: Bearer sekret"),
            "missing auth header"
        );
    }

    #[test]
    fn non_2xx_is_a_resolve_error() {
        let (base, handle) = stub("401 Unauthorized", "unauthorized");
        let client = DataRolesClient::new(base, "");
        let err = client
            .data_roles(&Selector::Email("person-3@example.org".into()))
            .expect_err("should fail closed");
        match err {
            ServiceError::Resolve(m) => assert!(m.contains("401"), "msg: {m}"),
            other => panic!("wrong error: {other:?}"),
        }
        let _ = handle.join();
    }

    #[test]
    fn parses_the_release_sets_sweep_list_and_sends_auth() {
        let body = r#"{"release_sets":["prod-a","prod-b"],"derived_at":"2026-07-30T00:00:00Z"}"#;
        let (base, handle) = stub("200 OK", body);
        let client = AudienceClient::new(base, "sekret");
        let sets = client.release_sets().expect("parse release-sets");
        assert_eq!(sets, vec!["prod-a".to_string(), "prod-b".to_string()]);
        let req = handle.join().unwrap();
        assert!(
            req.contains("GET /api/access/release-sets"),
            "wrong path: {req}"
        );
        assert!(
            req.contains("Authorization: Bearer sekret"),
            "missing auth: {req}"
        );
    }

    #[test]
    fn release_sets_non_2xx_is_a_resolve_error() {
        let (base, handle) = stub("503 Service Unavailable", "down");
        let err = AudienceClient::new(base, "")
            .release_sets()
            .expect_err("should fail closed");
        assert!(matches!(err, ServiceError::Resolve(_)));
        let _ = handle.join();
    }
}
