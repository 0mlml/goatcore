//! Read-only local checks for `goat doctor` step 0.
//!
//! These checks do not infer a mesh identity from an interface name or a
//! reachable TCP port. The client reports its own state; the later router
//! probe tests whether that state actually provides a usable path.

use super::doctor::{DiagnosticCheck, Verdict};
use crate::i18n::tr2;
use crate::profile::{InitState, Profile};
use chrono::{DateTime, Utc};
use serde_json::Value;
use std::path::Path;
use std::time::Duration;
use tokio::process::Command;
use tokio::time::timeout;

const CLIENT_TIMEOUT: Duration = Duration::from_secs(3);
const MANAGEMENT_TIMEOUT: Duration = Duration::from_secs(4);

pub async fn run(profile: &Profile) -> Vec<DiagnosticCheck> {
    let mut checks = vec![profile_check(profile)];
    let client = mesh_client().await;
    checks.extend(client.checks);
    if let Some(url) = client.management_url {
        checks.extend(management_checks(&url).await);
    } else {
        checks.push(DiagnosticCheck::new(
            "management.reachability",
            Verdict::Warn,
            tr2!("doctor-step0-management-unknown-summary"),
            tr2!("doctor-step0-management-unknown-hint"),
        ));
        checks.push(DiagnosticCheck::new(
            "clock.skew",
            Verdict::Warn,
            tr2!("doctor-step0-clock-unknown-summary"),
            tr2!("doctor-step0-clock-unknown-hint"),
        ));
    }
    checks
}

fn profile_check(profile: &Profile) -> DiagnosticCheck {
    match profile.init_state() {
        InitState::Absent => {
            let hint = if crate::bundle::ca_root_verifying_keys().is_empty() {
                tr2!("doctor-step0-profile-new-hint")
            } else {
                tr2!("doctor-step0-profile-absent-hint")
            };
            DiagnosticCheck::new(
                "profile.context",
                Verdict::Fail,
                tr2!(
                    "doctor-step0-profile-absent-summary",
                    context = profile.context.clone()
                ),
                hint,
            )
        }
        InitState::Denied => DiagnosticCheck::new(
            "profile.context",
            Verdict::Fail,
            tr2!(
                "doctor-step0-profile-denied-summary",
                path = profile.profile_toml_path().display().to_string()
            ),
            tr2!("doctor-step0-profile-denied-hint"),
        ),
        InitState::Initialized => {
            for (path, label) in [
                (profile.cert_path(), "mtls.cert.pem"),
                (profile.key_path(), "mtls.key.pem"),
                (profile.ca_roots_path(), "ca-roots.pem"),
                (profile.portal_token_path(), "portal.token"),
            ] {
                if let Err(e) = std::fs::File::open(&path) {
                    let hint = if e.kind() == std::io::ErrorKind::PermissionDenied {
                        tr2!("doctor-step0-profile-denied-hint")
                    } else {
                        tr2!("doctor-step0-profile-incomplete-hint")
                    };
                    return DiagnosticCheck::new(
                        if e.kind() == std::io::ErrorKind::PermissionDenied {
                            "profile.permission"
                        } else {
                            "profile.material"
                        },
                        Verdict::Fail,
                        tr2!(
                            "doctor-step0-profile-incomplete-summary",
                            file = label.to_string()
                        ),
                        hint,
                    );
                }
            }
            let body = match std::fs::read_to_string(profile.profile_toml_path()) {
                Ok(b) => b,
                Err(_) => {
                    return DiagnosticCheck::new(
                        "profile.material",
                        Verdict::Fail,
                        tr2!("doctor-step0-profile-invalid-summary"),
                        tr2!("doctor-step0-profile-incomplete-hint"),
                    );
                }
            };
            if ["router_endpoint", "portal_endpoint"].iter().any(|field| {
                crate::profile::extract_toml_field(&body, field)
                    .is_none_or(|value| value.trim().is_empty())
            }) {
                return DiagnosticCheck::new(
                    "profile.material",
                    Verdict::Fail,
                    tr2!("doctor-step0-profile-invalid-summary"),
                    tr2!("doctor-step0-profile-incomplete-hint"),
                );
            }
            match std::fs::read_to_string(profile.portal_token_path()) {
                Ok(token) if !token.trim().is_empty() => {}
                _ => {
                    return DiagnosticCheck::new(
                        "profile.material",
                        Verdict::Fail,
                        tr2!("doctor-step0-profile-empty-token-summary"),
                        tr2!("doctor-step0-profile-incomplete-hint"),
                    )
                }
            }
            DiagnosticCheck::new(
                "profile.context",
                Verdict::Ok,
                tr2!(
                    "doctor-step0-profile-ok-summary",
                    context = profile.context.clone()
                ),
                (String::new(), String::new()),
            )
        }
    }
}

struct ClientChecks {
    checks: Vec<DiagnosticCheck>,
    management_url: Option<String>,
}

async fn mesh_client() -> ClientChecks {
    // Prefer NetBird when it reports a tunnel. A running but disconnected
    // NetBird daemon must not hide a connected goat-clientd on the same host.
    let netbird_bin = std::env::var("GOAT_NETBIRD_BIN").unwrap_or_else(|_| "netbird".into());
    let status = netbird_status(&netbird_bin, CLIENT_TIMEOUT).await;
    let netbird = if let Ok(out) = &status {
        if out.status.success() {
            if let Ok(json) = serde_json::from_slice::<Value>(&out.stdout) {
                Some(netbird_checks(&json))
            } else {
                None
            }
        } else {
            None
        }
    } else {
        None
    };
    if let Some(ref c) = netbird {
        if c.checks
            .iter()
            .any(|check| check.id == "mesh.tunnel" && check.verdict == Verdict::Ok)
        {
            return netbird.expect("present in this branch");
        }
    }

    let mut client_socket_seen = false;
    if let Ok(socket) = super::client::default_socket() {
        if Path::new(&socket).exists() {
            client_socket_seen = true;
            if let Ok(raw) = super::client::call(&socket, "getStatus", serde_json::json!({})).await
            {
                if let Ok(json) = serde_json::from_str::<Value>(&raw) {
                    return goat_client_checks(&json);
                }
            }
        }
    }

    if let Some(netbird) = netbird {
        return netbird;
    }

    let installed = client_socket_seen
        || !matches!(status, Err(ref e) if e.kind() == std::io::ErrorKind::NotFound);
    let (summary, hint) = if installed {
        (
            tr2!("doctor-step0-client-stopped-summary"),
            tr2!("doctor-step0-client-stopped-hint"),
        )
    } else {
        (
            tr2!("doctor-step0-client-absent-summary"),
            tr2!("doctor-step0-client-absent-hint"),
        )
    };
    ClientChecks {
        checks: vec![DiagnosticCheck::new(
            "mesh.client",
            Verdict::Fail,
            summary,
            hint,
        )],
        management_url: None,
    }
}

pub(super) async fn netbird_status(
    bin: &str,
    budget: Duration,
) -> Result<std::process::Output, std::io::Error> {
    let mut command = Command::new(bin);
    command.args(["status", "--json"]).kill_on_drop(true);
    // F-098: a missing daemon socket can leave `netbird status` retrying
    // forever. Dropping the timed-out output future must kill its child.
    match timeout(budget, command.output()).await {
        Ok(result) => result,
        Err(_) => Err(std::io::Error::new(
            std::io::ErrorKind::TimedOut,
            "netbird status exceeded its deadline",
        )),
    }
}

fn netbird_checks(v: &Value) -> ClientChecks {
    let url = v
        .pointer("/management/url")
        .and_then(Value::as_str)
        .filter(|s| s.starts_with("https://"))
        .map(str::to_owned);
    let connected = v.pointer("/management/connected").and_then(Value::as_bool);
    let mesh_ip = v.get("netbirdIp").and_then(Value::as_str).unwrap_or("");
    let profile = v
        .get("profileName")
        .and_then(Value::as_str)
        .unwrap_or("unknown");
    let mut checks = vec![DiagnosticCheck::new(
        "mesh.client",
        Verdict::Ok,
        tr2!(
            "doctor-step0-netbird-ok-summary",
            profile = profile.to_string()
        ),
        (String::new(), String::new()),
    )];
    checks.push(match connected {
        Some(true) => DiagnosticCheck::new(
            "mesh.management",
            Verdict::Ok,
            tr2!("doctor-step0-management-connected-summary"),
            (String::new(), String::new()),
        ),
        Some(false) => DiagnosticCheck::new(
            "mesh.management",
            Verdict::Warn,
            tr2!("doctor-step0-management-disconnected-summary"),
            tr2!("doctor-step0-management-disconnected-hint"),
        ),
        None => DiagnosticCheck::new(
            "mesh.management",
            Verdict::Warn,
            tr2!("doctor-step0-management-unknown-summary"),
            tr2!("doctor-step0-management-unknown-hint"),
        ),
    });
    checks.push(if mesh_ip.is_empty() {
        DiagnosticCheck::new(
            "mesh.tunnel",
            Verdict::Fail,
            tr2!("doctor-step0-tunnel-down-summary"),
            tr2!("doctor-step0-tunnel-down-hint"),
        )
    } else {
        DiagnosticCheck::new(
            "mesh.tunnel",
            Verdict::Ok,
            tr2!(
                "doctor-step0-tunnel-address-summary",
                address = mesh_ip.to_string()
            ),
            (String::new(), String::new()),
        )
    });
    ClientChecks {
        checks,
        management_url: url,
    }
}

fn goat_client_checks(v: &Value) -> ClientChecks {
    let state = v.get("state").and_then(Value::as_str).unwrap_or("unknown");
    let mode = v.get("mode").and_then(Value::as_str);
    let bundle = v
        .get("bundleLoaded")
        .and_then(Value::as_bool)
        .unwrap_or(false);
    let mut checks = vec![DiagnosticCheck::new(
        "mesh.client",
        Verdict::Ok,
        tr2!(
            "doctor-step0-goat-client-ok-summary",
            state = state.to_string()
        ),
        (String::new(), String::new()),
    )];
    checks.push(if !bundle {
        DiagnosticCheck::new(
            "mesh.tunnel",
            Verdict::Fail,
            tr2!("doctor-step0-goat-client-no-bundle-summary"),
            tr2!("doctor-step0-goat-client-no-bundle-hint"),
        )
    } else if mode == Some("netbird-only") {
        DiagnosticCheck::new(
            "mesh.tunnel",
            Verdict::Skip,
            tr2!("doctor-step0-goat-client-outer-disabled-summary"),
            (String::new(), String::new()),
        )
    } else if state.eq_ignore_ascii_case("connected") {
        DiagnosticCheck::new(
            "mesh.tunnel",
            Verdict::Ok,
            tr2!("doctor-step0-goat-client-connected-summary"),
            (String::new(), String::new()),
        )
    } else {
        DiagnosticCheck::new(
            "mesh.tunnel",
            Verdict::Fail,
            tr2!(
                "doctor-step0-goat-client-down-summary",
                state = state.to_string()
            ),
            tr2!("doctor-step0-goat-client-down-hint"),
        )
    });
    let inner = v.pointer("/innerMesh/state").and_then(Value::as_str);
    if mode == Some("wg-cp0-only") {
        checks.push(DiagnosticCheck::new(
            "mesh.inner",
            Verdict::Skip,
            tr2!("doctor-step0-goat-client-inner-disabled-summary"),
            (String::new(), String::new()),
        ));
    } else if inner.is_some() || matches!(mode, Some("netbird-only" | "combined")) {
        let inner = inner.unwrap_or("unknown");
        checks.push(if inner.eq_ignore_ascii_case("connected") {
            DiagnosticCheck::new(
                "mesh.inner",
                Verdict::Ok,
                tr2!("doctor-step0-goat-client-inner-ok-summary"),
                (String::new(), String::new()),
            )
        } else {
            DiagnosticCheck::new(
                "mesh.inner",
                Verdict::Fail,
                tr2!(
                    "doctor-step0-goat-client-inner-down-summary",
                    state = inner.to_string()
                ),
                tr2!("doctor-step0-goat-client-inner-down-hint"),
            )
        });
    }
    ClientChecks {
        checks,
        management_url: None,
    }
}

async fn management_checks(url: &str) -> Vec<DiagnosticCheck> {
    let request_url = format!("{}/api/users", url.trim_end_matches('/'));
    let response = reqwest::Client::builder()
        .timeout(MANAGEMENT_TIMEOUT)
        .https_only(true)
        .redirect(reqwest::redirect::Policy::none())
        .build()
        .map(|client| client.get(request_url).send());
    let response = match response {
        Ok(request) => request.await,
        Err(e) => Err(e),
    };
    match response {
        Ok(r) if management_response_verdict(r.status()) == Verdict::Ok => {
            let date = r
                .headers()
                .get(reqwest::header::DATE)
                .and_then(|h| h.to_str().ok())
                .map(str::to_owned);
            let mut checks = vec![DiagnosticCheck::new(
                "management.reachability",
                Verdict::Ok,
                if r.status().is_success() {
                    tr2!(
                        "doctor-step0-management-reachable-summary",
                        status = r.status().as_u16().to_string()
                    )
                } else {
                    tr2!(
                        "doctor-step0-management-auth-summary",
                        status = r.status().as_u16().to_string()
                    )
                },
                (String::new(), String::new()),
            )];
            checks.push(clock_check(date.as_deref()));
            checks
        }
        Ok(r) => vec![
            DiagnosticCheck::new(
                "management.reachability",
                Verdict::Warn,
                tr2!(
                    "doctor-step0-management-unexpected-summary",
                    status = r.status().as_u16().to_string()
                ),
                tr2!("doctor-step0-management-unexpected-hint"),
            ),
            clock_check(None),
        ],
        Err(e) => vec![
            DiagnosticCheck::new(
                "management.reachability",
                Verdict::Warn,
                tr2!(
                    "doctor-step0-management-unreachable-summary",
                    error = e.to_string()
                ),
                tr2!("doctor-step0-management-unreachable-hint"),
            ),
            clock_check(None),
        ],
    }
}

fn management_response_verdict(status: reqwest::StatusCode) -> Verdict {
    if status.is_success()
        || status == reqwest::StatusCode::UNAUTHORIZED
        || status == reqwest::StatusCode::FORBIDDEN
    {
        Verdict::Ok
    } else {
        Verdict::Warn
    }
}

fn clock_check(date: Option<&str>) -> DiagnosticCheck {
    let skew = date
        .and_then(|d| DateTime::parse_from_rfc2822(d).ok())
        .map(|remote| (Utc::now().timestamp() - remote.timestamp()).abs());
    match skew {
        Some(seconds) if seconds > 60 => DiagnosticCheck::new(
            "clock.skew",
            Verdict::Fail,
            tr2!(
                "doctor-step0-clock-skew-summary",
                seconds = seconds.to_string()
            ),
            tr2!("doctor-step0-clock-skew-hint"),
        ),
        Some(seconds) => DiagnosticCheck::new(
            "clock.skew",
            Verdict::Ok,
            tr2!(
                "doctor-step0-clock-ok-summary",
                seconds = seconds.to_string()
            ),
            (String::new(), String::new()),
        ),
        None => DiagnosticCheck::new(
            "clock.skew",
            Verdict::Warn,
            tr2!("doctor-step0-clock-unknown-summary"),
            tr2!("doctor-step0-clock-unknown-hint"),
        ),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn profile_readiness_rejects_blank_endpoints() {
        let dir = tempfile::tempdir().unwrap();
        let profile = Profile {
            dir: dir.path().to_path_buf(),
            context: "default".into(),
        };
        std::fs::create_dir_all(profile.context_dir()).unwrap();
        for path in [
            profile.cert_path(),
            profile.key_path(),
            profile.ca_roots_path(),
            profile.portal_token_path(),
        ] {
            std::fs::write(path, "synthetic-fixture").unwrap();
        }
        std::fs::write(
            profile.profile_toml_path(),
            "router_endpoint = \"tls/127.0.0.1:1\"\nportal_endpoint = \"https://127.0.0.1:1\"\n",
        )
        .unwrap();
        assert_eq!(profile_check(&profile).verdict, Verdict::Ok);
        for body in [
            "router_endpoint = \"\"\nportal_endpoint = \"https://127.0.0.1:1\"\n",
            "router_endpoint = \"tls/127.0.0.1:1\"\nportal_endpoint = \"   \"\n",
        ] {
            std::fs::write(profile.profile_toml_path(), body).unwrap();
            let check = profile_check(&profile);
            assert_eq!(check.verdict, Verdict::Fail);
            assert_eq!(check.id, "profile.material");
        }
    }

    #[cfg(unix)]
    #[tokio::test]
    async fn timed_out_netbird_process_is_killed() {
        use std::os::unix::fs::PermissionsExt;

        let dir = tempfile::tempdir().unwrap();
        let script = dir.path().join("netbird");
        let pid_file = dir.path().join("pid");
        std::fs::write(
            &script,
            format!(
                "#!/bin/sh\necho $$ > '{}'\nexec sleep 30\n",
                pid_file.display()
            ),
        )
        .unwrap();
        std::fs::set_permissions(&script, std::fs::Permissions::from_mode(0o700)).unwrap();

        let result = netbird_status(script.to_str().unwrap(), Duration::from_secs(1)).await;
        assert_eq!(result.unwrap_err().kind(), std::io::ErrorKind::TimedOut);
        let pid: i32 = std::fs::read_to_string(pid_file)
            .unwrap()
            .trim()
            .parse()
            .unwrap();
        for _ in 0..20 {
            if unsafe { libc::kill(pid, 0) } == -1 {
                return;
            }
            tokio::time::sleep(Duration::from_millis(25)).await;
        }
        panic!("timed-out netbird process {pid} is still running");
    }

    #[test]
    fn netbird_address_is_not_a_router_proof() {
        let c = netbird_checks(&serde_json::json!({
            "management": {"url": "https://mgmt.example", "connected": false},
            "netbirdIp": "100.127.0.3/16", "profileName": "other"
        }));
        assert_eq!(c.checks[1].verdict, Verdict::Warn);
        assert_eq!(c.checks[2].verdict, Verdict::Ok);
        assert_eq!(c.management_url.as_deref(), Some("https://mgmt.example"));
    }

    #[test]
    fn missing_client_bundle_is_distinct_from_a_disconnected_tunnel() {
        let c = goat_client_checks(
            &serde_json::json!({"state": "disconnected", "bundleLoaded": false}),
        );
        assert_eq!(c.checks[1].verdict, Verdict::Fail);
        assert!(c.checks[1].hint.contains("goat client bundle import"));
    }

    #[test]
    fn connected_outer_tunnel_does_not_hide_disconnected_inner_mesh() {
        let c = goat_client_checks(&serde_json::json!({
            "state": "connected", "bundleLoaded": true,
            "innerMesh": {"state": "disconnected"}
        }));
        assert_eq!(c.checks[1].verdict, Verdict::Ok);
        assert_eq!(c.checks[2].id, "mesh.inner");
        assert_eq!(c.checks[2].verdict, Verdict::Fail);
    }

    #[test]
    fn client_mode_checks_only_enabled_tunnel_legs() {
        for (mode, outer, inner, outer_verdict, inner_verdict) in [
            (
                "wg-cp0-only",
                "connected",
                Some("disconnected"),
                Verdict::Ok,
                Verdict::Skip,
            ),
            (
                "netbird-only",
                "disconnected",
                Some("connected"),
                Verdict::Skip,
                Verdict::Ok,
            ),
            (
                "combined",
                "connected",
                Some("connected"),
                Verdict::Ok,
                Verdict::Ok,
            ),
            (
                "combined",
                "disconnected",
                Some("connected"),
                Verdict::Fail,
                Verdict::Ok,
            ),
            (
                "combined",
                "connected",
                Some("disconnected"),
                Verdict::Ok,
                Verdict::Fail,
            ),
            (
                "netbird-only",
                "disconnected",
                None,
                Verdict::Skip,
                Verdict::Fail,
            ),
            ("combined", "connected", None, Verdict::Ok, Verdict::Fail),
        ] {
            let mut status = serde_json::json!({
                "mode": mode, "state": outer, "bundleLoaded": true,
            });
            if let Some(inner) = inner {
                status["innerMesh"] = serde_json::json!({"state": inner});
            }
            let result = goat_client_checks(&status);
            assert_eq!(result.checks[1].verdict, outer_verdict, "{mode} outer");
            assert_eq!(result.checks[2].id, "mesh.inner", "{mode} inner");
            assert_eq!(result.checks[2].verdict, inner_verdict, "{mode} inner");
        }
    }

    #[test]
    fn client_mode_still_requires_a_bundle() {
        let result = goat_client_checks(&serde_json::json!({
            "mode": "netbird-only", "state": "disconnected", "bundleLoaded": false,
            "innerMesh": {"state": "connected"},
        }));
        assert_eq!(result.checks[1].verdict, Verdict::Fail);
    }

    #[test]
    fn legacy_client_status_keeps_outer_only_behavior() {
        let result = goat_client_checks(&serde_json::json!({
            "state": "disconnected", "bundleLoaded": true,
        }));
        assert_eq!(result.checks.len(), 2);
        assert_eq!(result.checks[1].verdict, Verdict::Fail);
    }

    #[test]
    fn clock_requires_a_remote_date() {
        assert_eq!(clock_check(None).verdict, Verdict::Warn);
        assert_eq!(
            clock_check(Some("Wed, 01 Jan 2020 00:00:00 GMT")).verdict,
            Verdict::Fail
        );
    }

    #[test]
    fn management_auth_challenge_proves_reachability_not_api_health() {
        assert_eq!(
            management_response_verdict(reqwest::StatusCode::UNAUTHORIZED),
            Verdict::Ok
        );
        assert_eq!(
            management_response_verdict(reqwest::StatusCode::FORBIDDEN),
            Verdict::Ok
        );
        assert_eq!(
            management_response_verdict(reqwest::StatusCode::NOT_FOUND),
            Verdict::Warn
        );
        assert_eq!(
            management_response_verdict(reqwest::StatusCode::INTERNAL_SERVER_ERROR),
            Verdict::Warn
        );
    }
}
