//! A status the portal ANSWERED is never reported as unreachable (#2087).
//! Each answer has its own code and exit code and names the status and the
//! request path; only a network fault is `E_PORTAL_UNREACHABLE`.
//!
//! Driven through the built binary (`goat topic ls`, whose first request is
//! `GET /api/catalog`) against a local stub portal.

use wiremock::matchers::{method, path};
use wiremock::{Mock, MockServer, ResponseTemplate};

async fn ls(portal_endpoint: &str) -> std::process::Output {
    let dir = tempfile::tempdir().unwrap();
    let context = dir.path().join("contexts/fixture");
    std::fs::create_dir_all(&context).unwrap();
    std::fs::write(
        context.join("profile.toml"),
        format!("portal_endpoint = \"{portal_endpoint}\"\n"),
    )
    .unwrap();
    std::fs::write(context.join("portal.token"), "synthetic-topic-key").unwrap();
    tokio::time::timeout(
        std::time::Duration::from_secs(20),
        tokio::process::Command::new(env!("CARGO_BIN_EXE_goat"))
            .env_clear()
            .args([
                "--profile-dir",
                dir.path().to_str().unwrap(),
                "--context",
                "fixture",
                "topic",
                "ls",
            ])
            .output(),
    )
    .await
    .expect("the listing must finish")
    .unwrap()
}

/// The exit code and stderr of `goat topic ls` against a portal answering
/// `/api/catalog` with `response`.
async fn answered(response: ResponseTemplate) -> (i32, String) {
    let server = MockServer::start().await;
    Mock::given(method("GET"))
        .and(path("/api/catalog"))
        .respond_with(response)
        .mount(&server)
        .await;
    let out = ls(&server.uri()).await;
    (
        out.status.code().unwrap_or(-1),
        String::from_utf8_lossy(&out.stderr).into_owned(),
    )
}

fn assert_answer(exit: i32, stderr: &str, want_exit: i32, code: &str, needles: &[&str]) {
    assert_eq!(exit, want_exit, "{stderr}");
    assert!(stderr.contains(&format!("code: {code}")), "{stderr}");
    assert!(!stderr.contains("unreachable"), "an answer called unreachable: {stderr}");
    for n in needles {
        assert!(stderr.contains(n), "missing {n:?}: {stderr}");
    }
}

#[tokio::test]
async fn a_404_is_not_found_with_the_path_exit_7() {
    // No catalog (404) sends the listing to the per-org walk; a portal whose
    // org listing also answers 404 says so as E_PORTAL_NOT_FOUND.
    let server = MockServer::start().await;
    Mock::given(method("GET"))
        .respond_with(ResponseTemplate::new(404))
        .mount(&server)
        .await;
    let out = ls(&server.uri()).await;
    let stderr = String::from_utf8_lossy(&out.stderr);
    assert_answer(
        out.status.code().unwrap_or(-1),
        &stderr,
        7,
        "E_PORTAL_NOT_FOUND",
        &["HTTP 404", "/api/vendors"],
    );
}

#[tokio::test]
async fn a_429_is_rate_limited_with_retry_after_exit_8() {
    let (exit, stderr) =
        answered(ResponseTemplate::new(429).insert_header("Retry-After", "7")).await;
    assert_answer(
        exit,
        &stderr,
        8,
        "E_PORTAL_RATE_LIMITED",
        &["HTTP 429", "/api/catalog", "retry after 7 s"],
    );
}

#[tokio::test]
async fn a_5xx_is_a_server_error_with_its_status_exit_9() {
    for status in [500u16, 502, 503] {
        let (exit, stderr) = answered(ResponseTemplate::new(status)).await;
        assert_answer(
            exit,
            &stderr,
            9,
            "E_PORTAL_SERVER_ERROR",
            &[&format!("HTTP {status}"), "/api/catalog"],
        );
    }
}

#[tokio::test]
async fn an_uncurated_4xx_is_refused_exit_4() {
    let (exit, stderr) = answered(ResponseTemplate::new(422)).await;
    assert_answer(
        exit,
        &stderr,
        4,
        "E_PORTAL_REFUSED",
        &["HTTP 422", "/api/catalog"],
    );
}

#[tokio::test]
async fn the_portal_own_message_rides_under_the_answer() {
    let (exit, stderr) = answered(
        ResponseTemplate::new(503)
            .set_body_json(serde_json::json!({"error": "registry database is restarting"})),
    )
    .await;
    assert_answer(
        exit,
        &stderr,
        9,
        "E_PORTAL_SERVER_ERROR",
        &["server: registry database is restarting"],
    );
}

#[tokio::test]
async fn no_answer_at_all_is_still_unreachable_exit_4() {
    // A port nothing listens on: a network fault, the one case that IS
    // E_PORTAL_UNREACHABLE.
    let port = std::net::TcpListener::bind("127.0.0.1:0")
        .unwrap()
        .local_addr()
        .unwrap()
        .port();
    let out = ls(&format!("http://127.0.0.1:{port}")).await;
    let stderr = String::from_utf8_lossy(&out.stderr);
    assert_eq!(out.status.code(), Some(4), "{stderr}");
    assert!(stderr.contains("code: E_PORTAL_UNREACHABLE"), "{stderr}");
}
