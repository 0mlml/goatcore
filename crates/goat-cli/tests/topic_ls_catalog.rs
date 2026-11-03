//! `goat topic ls` with no prefix reads the whole goatnet in one request
//! (`GET /api/catalog`), not one request per org (#2088); a portal that
//! predates the catalog answers 404 and gets the per-org walk.

use serde_json::{json, Value};
use wiremock::matchers::{method, path};
use wiremock::{Mock, MockServer, ResponseTemplate};

async fn ls(server: &MockServer) -> std::process::Output {
    let dir = tempfile::tempdir().unwrap();
    let context = dir.path().join("contexts/fixture");
    std::fs::create_dir_all(&context).unwrap();
    std::fs::write(
        context.join("profile.toml"),
        format!("portal_endpoint = \"{}\"\n", server.uri()),
    )
    .unwrap();
    std::fs::write(context.join("portal.token"), "synthetic-topic-key").unwrap();
    tokio::time::timeout(
        std::time::Duration::from_secs(10),
        tokio::process::Command::new(env!("CARGO_BIN_EXE_goat"))
            .env_clear()
            .args([
                "--profile-dir",
                dir.path().to_str().unwrap(),
                "--context",
                "fixture",
                "topic",
                "ls",
                "--json",
            ])
            .output(),
    )
    .await
    .expect("listing must finish")
    .unwrap()
}

fn rows(out: &std::process::Output) -> Vec<Value> {
    assert!(out.status.success(), "{}", String::from_utf8_lossy(&out.stderr));
    String::from_utf8_lossy(&out.stdout)
        .lines()
        .map(|l| serde_json::from_str(l).unwrap())
        .collect()
}

#[tokio::test]
async fn no_prefix_is_one_catalog_request_and_never_a_walk_of_every_org() {
    let server = MockServer::start().await;
    Mock::given(method("GET"))
        .and(path("/api/catalog"))
        .respond_with(ResponseTemplate::new(200).set_body_json(json!({"orgs": [
            {"prefix": "beta", "topics": [{"id": 9, "key_pattern": "beta/b/v1", "format": "json", "description": "b"}]},
            {"prefix": "alpha", "topics": [{"id": 4, "key_pattern": "alpha/a/v1", "format": "cbor"}]}
        ]})))
        .expect(1)
        .mount(&server)
        .await;
    // Any other request is the fan-out this replaces.
    Mock::given(method("GET"))
        .respond_with(ResponseTemplate::new(500))
        .expect(0)
        .mount(&server)
        .await;
    let got = rows(&ls(&server).await);
    assert_eq!(got.len(), 2);
    assert_eq!(got[0]["key_pattern"], "alpha/a/v1");
    assert_eq!(got[0]["vendor_prefix"], "alpha");
    assert_eq!(got[0]["id"], 4);
    assert_eq!(got[1]["key_pattern"], "beta/b/v1");
    assert_eq!(got[1]["format"], "json");
}

#[tokio::test]
async fn a_portal_without_the_catalog_gets_the_per_org_walk() {
    let server = MockServer::start().await;
    Mock::given(method("GET"))
        .and(path("/api/catalog"))
        .respond_with(ResponseTemplate::new(404))
        .expect(1)
        .mount(&server)
        .await;
    Mock::given(method("GET"))
        .and(path("/api/orgs"))
        .respond_with(ResponseTemplate::new(200).set_body_json(json!({"orgs": [{"prefix": "alpha"}]})))
        .mount(&server)
        .await;
    Mock::given(method("GET"))
        .and(path("/api/orgs/alpha/topics"))
        .respond_with(ResponseTemplate::new(200).set_body_json(json!({"topics": [
            {"id": 4, "org_prefix": "alpha", "key_pattern": "alpha/a/v1", "format": "cbor"}
        ]})))
        .expect(1)
        .mount(&server)
        .await;
    let got = rows(&ls(&server).await);
    assert_eq!(got.len(), 1);
    assert_eq!(got[0]["key_pattern"], "alpha/a/v1");
}
