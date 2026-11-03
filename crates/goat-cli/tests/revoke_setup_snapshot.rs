//! Real command regressions: all service traffic stays on a loopback mock and
//! every command gets a fresh profile, repository, and empty environment.
use serde_json::{json, Value};
use wiremock::matchers::{body_partial_json, method, path};
use wiremock::{Mock, MockServer, ResponseTemplate};

async fn exercise(mode: &str, mesh: bool, snapshot: &str, dry_run: bool, token: bool) {
    let server = MockServer::start().await;
    let tmp = tempfile::tempdir().unwrap();
    let root = tmp.path();
    let profile = root.join("profile");
    let context = profile.join("contexts/fixture");
    let operators = root.join("ops/operators/fixture");
    std::fs::create_dir_all(&context).unwrap();
    std::fs::create_dir_all(&operators).unwrap();
    std::fs::write(context.join("profile.toml"), format!(
        "portal_endpoint = \"{0}\"\nonboard_endpoint = \"{0}\"\nnetbird_mgmt_url = \"{0}\"\nnetbird_token_env = \"FIXTURE_NB_TOKEN\"\ngoatnet = \"fixture\"\n", server.uri())).unwrap();
    std::fs::write(context.join("portal.token"), "synthetic-console-key").unwrap();
    let handles = if mode == "single" {
        vec!["alice"]
    } else {
        vec!["alice", "bob"]
    };
    for handle in &handles {
        // The second batch member is non-mesh, so it must remain a full success.
        let enabled = mesh && *handle == "alice";
        std::fs::write(operators.join(format!("{handle}.yml")), format!(
            "identity:\n  email: {handle}@example.invalid\n  status: active\norg: {{short_tag: {handle}}}\nnetbird: {{enabled: {enabled}}}\nzenoh: {{enabled: false}}\nory_site: {{enabled: false}}\ncollab: {{enabled: false}}\n")).unwrap();
    }
    let roster = root.join("docs/operators/fixture");
    std::fs::create_dir_all(&roster).unwrap();
    std::fs::write(
        roster.join("roster.json"),
        json!({"orgs": [], "members": handles.iter().map(|h|
        json!({"handle": h, "type": "person", "org": "fixture-org", "status": "active"})
    ).collect::<Vec<_>>()} )
        .to_string(),
    )
    .unwrap();
    let list = root.join("handles.txt");
    std::fs::write(&list, handles.join("\n")).unwrap();
    Mock::given(method("GET"))
        .and(path("/api/peers"))
        .respond_with(ResponseTemplate::new(200).set_body_json(json!([])))
        .expect(if token { 1 } else { 0 })
        .mount(&server)
        .await;
    let key = json!({"id": "setup-alice", "name": "peer-alice-fixture-2026-10-21-device", "revoked": false});
    let response = match snapshot {
        "http" => ResponseTemplate::new(500),
        "decode" => ResponseTemplate::new(200).set_body_string("{malformed"),
        "empty" => ResponseTemplate::new(200).set_body_json(json!([])),
        _ => ResponseTemplate::new(200).set_body_json(json!([key.clone(),
            {"id": "other", "name": "peer-carol-fixture-2026-10-21-device", "revoked": false},
            {"id": "already", "name": "peer-alice-fixture-2026-10-20-device", "revoked": true}])),
    };
    Mock::given(method("GET"))
        .and(path("/api/setup-keys"))
        .respond_with(response)
        .expect(if token { 1 } else { 0 })
        .mount(&server)
        .await;
    let should_put = token && mesh && !dry_run && matches!(snapshot, "keys" | "put-failure");
    Mock::given(method("PUT"))
        .and(path("/api/setup-keys/setup-alice"))
        .and(body_partial_json(json!({"revoked": true, "name": "peer-alice-fixture-2026-10-21-device", "auto_groups": []})))
        .respond_with(
            ResponseTemplate::new(if snapshot == "put-failure" { 500 } else { 200 })
                .set_body_json(key),
        )
        .expect(if should_put { 1 } else { 0 })
        .mount(&server)
        .await;
    for endpoint in ["proposals", "ssh-keys"] {
        Mock::given(method("GET"))
            .and(path(format!("/api/onboard/{endpoint}")))
            .respond_with(ResponseTemplate::new(200).set_body_json(json!([])))
            .mount(&server)
            .await;
    }
    Mock::given(method("GET"))
        .and(path("/api/onboard/operator-keys"))
        .respond_with(
            ResponseTemplate::new(200).set_body_json(
                handles
                    .iter()
                    .map(|h| json!({"id": format!("op-{h}"), "handle": h, "status": "active"}))
                    .collect::<Vec<_>>(),
            ),
        )
        .mount(&server)
        .await;
    for handle in &handles {
        Mock::given(method("POST"))
            .and(path(format!(
                "/api/onboard/operator-keys/op-{handle}/revoke"
            )))
            .respond_with(ResponseTemplate::new(200).set_body_json(
                json!({"id": format!("op-{handle}"), "handle": handle, "status": "revoked"}),
            ))
            .expect(if dry_run { 0 } else { 1 })
            .mount(&server)
            .await;
    }
    let mut command = tokio::process::Command::new(env!("CARGO_BIN_EXE_goat"));
    command
        .env_clear()
        .env("HOME", root)
        .env("GOAT_OPERATOR_HANDLE", "fixture-operator")
        .args([
            "--profile-dir",
            profile.to_str().unwrap(),
            "--context",
            "fixture",
            "admin",
            "onboard",
            "--native",
            "--revoke",
            "--env",
            "fixture",
            "--repo-root",
            root.to_str().unwrap(),
        ]);
    if token {
        command.env("FIXTURE_NB_TOKEN", "synthetic-netbird-key");
    }
    match mode {
        "single" => {
            command.arg("alice");
        }
        "batch" => {
            command.args(["--from", list.to_str().unwrap()]);
        }
        "org" => {
            command.args(["--org", "fixture-org", "--yes"]);
        }
        _ => panic!("unknown mode"),
    }
    if dry_run {
        command.arg("--dry-run");
    }
    let output = tokio::time::timeout(std::time::Duration::from_secs(20), command.output())
        .await
        .expect("local revoke must finish")
        .unwrap();
    let stderr = String::from_utf8(output.stderr).unwrap();
    let partial = mesh && (!token || matches!(snapshot, "http" | "decode" | "put-failure"));
    assert_eq!(
        output.status.success(),
        !partial || dry_run,
        "{mode}/{snapshot}: {stderr}"
    );
    // Every independent console cut must happen, even after a mesh failure.
    server.verify().await;
    for handle in &handles {
        let text = std::fs::read_to_string(operators.join(format!("{handle}.yml"))).unwrap();
        let manifest: serde_yaml::Value = serde_yaml::from_str(&text).unwrap();
        assert_eq!(
            manifest["identity"]["status"].as_str().unwrap(),
            if dry_run { "active" } else { "revoked" }
        );
    }
    let audit = profile.join("audit/operator-actions.log");
    if dry_run {
        assert!(!audit.exists(), "dry run must not emit a mutation audit");
        assert!(server
            .received_requests()
            .await
            .unwrap()
            .iter()
            .all(|r| r.method == "GET"));
        if partial {
            assert!(stderr.contains("setup-key"), "{stderr}");
        }
    } else {
        let entries: Vec<Value> = std::fs::read_to_string(audit)
            .unwrap()
            .lines()
            .map(|line| serde_json::from_str(line).unwrap())
            .collect();
        for handle in &handles {
            let finish = entries
                .iter()
                .find(|e| e["target"] == *handle && e["outcome"] != "started")
                .unwrap();
            let failed = partial && *handle == "alice";
            assert_eq!(finish["outcome"], if failed { "partial" } else { "ok" });
            if failed {
                assert!(finish["error_msg"]
                    .as_str()
                    .unwrap()
                    .contains("setup_keys=failed"));
            }
        }
    }
}

#[tokio::test]
async fn failed_snapshot_continues_single_batch_and_org_revoke() {
    for mode in ["single", "batch", "org"] {
        exercise(mode, true, "http", false, true).await;
    }
}

#[tokio::test]
async fn nonmesh_and_dry_run_do_not_fail_for_a_snapshot_outage() {
    exercise("single", false, "http", false, true).await;
    exercise("batch", true, "http", true, true).await;
}

#[tokio::test]
async fn snapshot_and_revoke_controls_are_distinct() {
    for snapshot in ["decode", "empty", "keys", "put-failure"] {
        exercise("single", true, snapshot, false, true).await;
    }
    exercise("single", true, "empty", false, false).await;
    exercise("single", false, "empty", false, false).await;
}

#[tokio::test]
async fn snapshot_matrix_preserves_manifest_and_command_boundaries() {
    for mode in ["single", "batch", "org"] {
        for mesh in [false, true] {
            for dry_run in [false, true] {
                for (snapshot, token) in [("http", true), ("empty", true), ("empty", false)] {
                    exercise(mode, mesh, snapshot, dry_run, token).await;
                }
            }
        }
    }
}
