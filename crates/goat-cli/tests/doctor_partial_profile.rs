#![cfg(unix)]

use std::io::{BufRead, BufReader, Write};
use std::os::unix::net::UnixListener;
use std::process::Command;

fn profile_with_daemon(router_endpoint: bool, portal_endpoint: bool) -> (
    tempfile::TempDir,
    std::path::PathBuf,
    std::thread::JoinHandle<()>,
) {
    let dir = tempfile::tempdir().unwrap();
    let context = dir.path().join("contexts/default");
    std::fs::create_dir_all(&context).unwrap();
    let mut profile = String::new();
    if router_endpoint {
        profile.push_str("router_endpoint = \"tls/127.0.0.1:1\"\n");
    }
    if portal_endpoint {
        profile.push_str("portal_endpoint = \"https://127.0.0.1:1\"\n");
    }
    std::fs::write(context.join("profile.toml"), profile).unwrap();
    std::fs::write(context.join("portal.token"), "test-token").unwrap();

    let key = rcgen::KeyPair::generate().unwrap();
    let cert = rcgen::CertificateParams::new(vec!["localhost".into()])
        .unwrap()
        .self_signed(&key)
        .unwrap();
    std::fs::write(context.join("mtls.cert.pem"), cert.pem()).unwrap();
    std::fs::write(context.join("mtls.key.pem"), key.serialize_pem()).unwrap();
    std::fs::write(context.join("ca-roots.pem"), cert.pem()).unwrap();

    let socket = dir.path().join("custom-client.sock");
    let listener = UnixListener::bind(&socket).unwrap();
    let daemon = std::thread::spawn(move || {
        let (mut stream, _) = listener.accept().unwrap();
        let mut request = String::new();
        BufReader::new(stream.try_clone().unwrap())
            .read_line(&mut request)
            .unwrap();
        assert!(request.contains("getStatus"));
        stream
            .write_all(b"{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"mode\":\"netbird-only\",\"state\":\"disconnected\",\"bundleLoaded\":true,\"innerMesh\":{\"state\":\"connected\"}}}\n")
            .unwrap();
    });
    (dir, socket, daemon)
}

fn doctor(
    dir: &std::path::Path,
    socket: &std::path::Path,
    all: bool,
) -> (Vec<serde_json::Value>, std::process::Output) {
    let mut command = Command::new(env!("CARGO_BIN_EXE_goat"));
    command
        .args(["--profile-dir", dir.to_str().unwrap(), "doctor", "--json"])
        .env("GOAT_CLIENT_SOCKET", socket)
        .env("GOAT_NETBIRD_BIN", dir.join("no-netbird"));
    if all {
        command.arg("--all");
    }
    let output = command.output().unwrap();
    let records = String::from_utf8_lossy(&output.stdout)
        .lines()
        .map(|line| serde_json::from_str(line).unwrap())
        .collect();
    (records, output)
}

#[test]
fn doctor_uses_custom_client_socket() {
    let (dir, socket, daemon) = profile_with_daemon(true, true);
    let (records, output) = doctor(dir.path(), &socket, false);
    daemon.join().unwrap();
    let checks = records[0]["checks"].as_array().unwrap();
    assert!(
        checks
            .iter()
            .any(|c| c["id"] == "mesh.client" && c["verdict"] == "ok"),
        "{}",
        String::from_utf8_lossy(&output.stdout)
    );
    assert!(checks
        .iter()
        .any(|c| c["id"] == "mesh.inner" && c["verdict"] == "ok"));
    assert_eq!(records[0]["verdict"], "warn");
    assert_ne!(records[1]["verdict"], "skip");
}

#[test]
fn all_runs_available_probes_when_portal_endpoint_is_missing() {
    let (dir, socket, daemon) = profile_with_daemon(true, false);
    let (records, output) = doctor(dir.path(), &socket, true);
    daemon.join().unwrap();
    assert_eq!(
        records.len(),
        5,
        "{}",
        String::from_utf8_lossy(&output.stdout)
    );
    assert_eq!(records[0]["verdict"], "fail");
    assert_ne!(records[1]["verdict"], "skip");
    assert_eq!(records[2]["verdict"], "ok");
    assert_eq!(records[4]["verdict"], "skip");
    assert!(records[4]["summary"]
        .as_str()
        .unwrap()
        .contains("portal_endpoint"));
    assert!(!output.status.success());
    assert!(String::from_utf8_lossy(&output.stderr).contains("E_PROFILE_MISSING"));
}

#[test]
fn all_runs_portal_when_router_endpoint_is_missing() {
    let (dir, socket, daemon) = profile_with_daemon(false, true);
    let (records, output) = doctor(dir.path(), &socket, true);
    daemon.join().unwrap();
    assert_eq!(records.len(), 5, "{}", String::from_utf8_lossy(&output.stdout));
    assert_eq!(records[1]["verdict"], "skip");
    assert_eq!(records[2]["verdict"], "ok");
    assert_eq!(records[3]["verdict"], "skip");
    assert_ne!(records[4]["verdict"], "skip");
    assert!(String::from_utf8_lossy(&output.stderr).contains("E_PROFILE_MISSING"));
}
