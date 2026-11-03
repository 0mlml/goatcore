use panoscope_core::portal::{AffiliationRepairSpec, PortalClient};
use tokio::io::{AsyncReadExt, AsyncWriteExt};

async fn mock_portal(
    status: u16,
    response: &str,
) -> (PortalClient, tokio::task::JoinHandle<serde_json::Value>) {
    let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
    let url = format!("http://{}", listener.local_addr().unwrap());
    let redirect = if status == 307 || status == 308 {
        format!("Location: {url}/redirected\r\n")
    } else {
        String::new()
    };
    let response = format!("HTTP/1.1 {status} fixture\r\n{redirect}Content-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{response}", response.len());
    let task = tokio::task::spawn_local(async move {
        tokio::time::timeout(std::time::Duration::from_secs(10), async {
            let (mut stream, _) = listener.accept().await.unwrap();
            let mut bytes = Vec::new();
            let request = loop {
                let mut chunk = [0u8; 4096];
                let count = stream.read(&mut chunk).await.unwrap();
                assert!(count > 0, "incomplete request");
                bytes.extend_from_slice(&chunk[..count]);
                let Some(end) = bytes.windows(4).position(|window| window == b"\r\n\r\n") else {
                    continue;
                };
                let headers = std::str::from_utf8(&bytes[..end]).unwrap();
                assert!(headers.starts_with("POST /api/machines/affiliation/repair HTTP/1.1"));
                assert!(headers
                    .to_lowercase()
                    .contains("authorization: bearer fixture-token"));
                let length: usize = headers
                    .lines()
                    .find_map(|line| {
                        line.to_lowercase()
                            .strip_prefix("content-length:")
                            .map(|value| value.trim().parse().unwrap())
                    })
                    .unwrap();
                if bytes.len() >= end + 4 + length {
                    break serde_json::from_slice(&bytes[end + 4..end + 4 + length]).unwrap();
                }
            };
            stream.write_all(response.as_bytes()).await.unwrap();
            request
        })
        .await
        .unwrap()
    });
    (PortalClient::new(url, "fixture-token".into()), task)
}

fn spec(apply: bool) -> AffiliationRepairSpec {
    AffiliationRepairSpec {
        from: vec!["Unit Alpha".into()],
        to: "unit-alpha".into(),
        reason: "Owner verified".into(),
        apply,
        expected_revision: apply.then(|| "a".repeat(64)),
    }
}

#[tokio::test]
async fn affiliation_repair_preview_and_apply_wire_contract() {
    tokio::task::LocalSet::new().run_until(async {

    for apply in [false, true] {
        let response = serde_json::json!({"from":["Unit Alpha"], "to":"unit-alpha", "revision":"a".repeat(64), "rows":[{"kind":"grant", "id":"fixture", "affiliation":"Unit Alpha"}], "grants":1, "slots":0, "applied":apply, "audit_id":if apply { Some(17) } else { None }}).to_string();
        let (client, server) = mock_portal(200, &response).await;
        let result = client
            .repair_machine_affiliation(&spec(apply))
            .await
            .unwrap();
        assert_eq!(result.applied, apply);
        assert_eq!(result.audit_id, if apply { Some(17) } else { None });
        let request = server.await.unwrap();
        assert_eq!(request["from"], serde_json::json!(["Unit Alpha"]));
        assert_eq!(request["apply"], apply);
        assert_eq!(request.get("expected_revision").is_some(), apply);
    }

 }).await;
}

#[tokio::test]
async fn affiliation_repair_preserves_refusals_and_rejects_malformed_success() {
    tokio::task::LocalSet::new()
        .run_until(async {
            for status in [307, 308, 400, 401, 403, 404, 409, 500] {
                let (client, server) = mock_portal(
                    status,
                    r#"{"error":{"code":"fixture_refusal","message":"fixture detail"}}"#,
                )
                .await;
                let error = client
                    .repair_machine_affiliation(&spec(true))
                    .await
                    .unwrap_err();
                assert_eq!(error.http_status().unwrap().as_u16(), status);
                assert_eq!(error.server_message(), Some("fixture detail"));
                server.await.unwrap();
            }
            let (client, server) = mock_portal(200, "{}").await;
            assert!(client
                .repair_machine_affiliation(&spec(false))
                .await
                .is_err());
            server.await.unwrap();
        })
        .await;
}

#[tokio::test]
async fn affiliation_repair_does_not_retry_after_response_loss() {
    tokio::task::LocalSet::new()
        .run_until(async {
            let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
            let url = format!("http://{}", listener.local_addr().unwrap());
            let (completed, completion) = tokio::sync::oneshot::channel();
            let server = tokio::task::spawn_local(async move {
                let (mut stream, _) = listener.accept().await.unwrap();
                let mut bytes = Vec::new();
                loop {
                    let mut chunk = [0u8; 4096];
                    let n = stream.read(&mut chunk).await.unwrap();
                    assert!(n > 0);
                    bytes.extend_from_slice(&chunk[..n]);
                    if let Some(end) = bytes.windows(4).position(|v| v == b"\r\n\r\n") {
                        let headers = std::str::from_utf8(&bytes[..end]).unwrap().to_lowercase();
                        assert!(headers.contains("x-goat-operator-actor: fixture-human"));
                        let length: usize = headers
                            .lines()
                            .find_map(|line| {
                                line.strip_prefix("content-length:")
                                    .map(|v| v.trim().parse().unwrap())
                            })
                            .unwrap();
                        if bytes.len() >= end + 4 + length {
                            break;
                        }
                    }
                }
                drop(stream); // The mutation was received, but its receipt was lost.
                tokio::select! {
                    biased;
                    _ = listener.accept() => panic!("apply was sent more than once"),
                    result = completion => result.expect("client completion signal lost"),
                }
            });
            let client =
                PortalClient::new(url, "fixture-token".into()).with_operator_actor("fixture-human");
            let result = tokio::time::timeout(
                std::time::Duration::from_secs(10),
                client.repair_machine_affiliation(&spec(true)),
            )
            .await
            .expect("repair did not finish");
            completed.send(()).expect("replay observer failed");
            assert!(result.is_err());
            server.await.unwrap();
        })
        .await;
}
