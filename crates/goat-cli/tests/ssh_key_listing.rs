use serde_json::{json, Value};
use wiremock::matchers::{header, method, path, query_param};
use wiremock::{Mock, MockServer, ResponseTemplate};

const STATES: [&str; 5] = ["submitted", "approved", "installed", "rejected", "revoked"];

async fn run(server: &MockServer, args: &[&str]) -> std::process::Output {
    let dir = tempfile::tempdir().unwrap();
    let context = dir.path().join("contexts/fixture");
    std::fs::create_dir_all(&context).unwrap();
    std::fs::write(
        context.join("profile.toml"),
        format!("portal_endpoint = \"{}\"\n", server.uri()),
    )
    .unwrap();
    std::fs::write(context.join("portal.token"), "synthetic-listing-key").unwrap();
    tokio::time::timeout(
        std::time::Duration::from_secs(10),
        tokio::process::Command::new(env!("CARGO_BIN_EXE_goat"))
            .env_clear()
            .args([
                "--profile-dir",
                dir.path().to_str().unwrap(),
                "--context",
                "fixture",
                "admin",
                "ssh-key",
                "ls",
            ])
            .args(args)
            .output(),
    )
    .await
    .expect("listing must finish")
    .unwrap()
}

async fn serve(server: &MockServer, status: &str, response: ResponseTemplate) {
    Mock::given(method("GET"))
        .and(path("/api/onboard/ssh-keys"))
        .and(header("authorization", "Bearer synthetic-listing-key"))
        .and(query_param("status", status))
        .respond_with(response)
        .expect(1)
        .mount(server)
        .await;
}

#[tokio::test]
async fn ssh_key_listing_all_preserves_states_environment_and_output_modes() {
    for (spelling, json_output) in [("all", true), ("ALL", false)] {
        let server = MockServer::start().await;
        let mut rows: Vec<Value> = STATES
            .iter()
            .map(|s| json!({"id": s, "status": s, "env": "fixture"}))
            .collect();
        rows.push(json!({"id": "foreign-key", "status": "installed", "env": "other"}));
        serve(
            &server,
            "all",
            ResponseTemplate::new(200).set_body_json(&rows),
        )
        .await;
        let mut args = vec!["--status", spelling, "--env", "FIXTURE"];
        if json_output {
            args.push("--json");
        }
        let output = run(&server, &args).await;
        assert!(
            output.status.success(),
            "{}",
            String::from_utf8_lossy(&output.stderr)
        );
        let stdout = String::from_utf8(output.stdout).unwrap();
        if json_output {
            let got: Vec<Value> = serde_json::from_str(&stdout).unwrap();
            assert_eq!(got.len(), 5);
            assert_eq!(
                got.iter()
                    .map(|r| r["status"].as_str().unwrap())
                    .collect::<Vec<_>>(),
                STATES
            );
        } else {
            for state in STATES {
                assert!(stdout.contains(state), "{stdout}");
            }
            assert!(stdout.contains("FINGERPRINT"));
        }
        assert!(!stdout.contains("foreign-key"));
    }
}

#[tokio::test]
async fn ssh_key_listing_default_and_explicit_states_are_sent_unchanged() {
    for status in std::iter::once(None).chain(STATES.into_iter().map(Some)) {
        let server = MockServer::start().await;
        let want = status.unwrap_or("submitted");
        serve(
            &server,
            want,
            ResponseTemplate::new(200).set_body_json(json!([{"id": "key", "status": want}])),
        )
        .await;
        let mut args = vec!["--json"];
        if let Some(value) = status {
            args.extend(["--status", value]);
        }
        let output = run(&server, &args).await;
        assert!(
            output.status.success(),
            "{}",
            String::from_utf8_lossy(&output.stderr)
        );
        let rows: Vec<Value> = serde_json::from_slice(&output.stdout).unwrap();
        assert_eq!(rows[0]["status"], want);
    }
}

#[tokio::test]
async fn ssh_key_listing_empty_and_failed_reads_are_distinct() {
    for status in [200, 401, 403, 500] {
        let server = MockServer::start().await;
        serve(
            &server,
            "all",
            ResponseTemplate::new(status).set_body_json(json!([])),
        )
        .await;
        let output = run(&server, &["--status", "all", "--json"]).await;
        if status == 200 {
            assert!(output.status.success());
            assert_eq!(
                serde_json::from_slice::<Value>(&output.stdout).unwrap(),
                json!([])
            );
        } else {
            assert!(!output.status.success());
            assert!(output.stdout.is_empty());
            assert!(String::from_utf8_lossy(&output.stderr).contains(&status.to_string()));
        }
    }
}
