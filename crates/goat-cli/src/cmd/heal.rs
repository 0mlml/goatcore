// `goat heal` — one-action mesh recovery. `goat status` tells you you're
// off-mesh; `heal` does what the operator otherwise types by hand every
// time (`netbird down` → `netbird up`), waits for the router to become
// reachable again, and re-runs the status probes so the reported
// after-state is verified, not assumed.
//
// Actuators, in preference order:
//   1. the `netbird` CLI (operator laptops; --netbird-bin /
//      $GOAT_NETBIRD_BIN override the PATH lookup)
//   2. the local goat-clientd IPC socket (goat-client installs), via its
//      disconnect/connect RPCs
// Neither present → nothing is actuated and the report says so.
//
// Scope honesty: heal fixes mesh connectivity only. An expired cert can't
// be bounced back to health (that's a re-issue — `goat admin rotate`),
// and a portal that stays down while the mesh is green is a server-side
// problem; both are explained, not retried.

use clap::Args;
use serde::Serialize;
use std::path::{Path, PathBuf};
use std::time::{Duration, Instant};

use super::status::{self, Verdicts};

/// Interval between ZTOL re-probes while waiting for the mesh to return.
const RECHECK_INTERVAL: Duration = Duration::from_secs(2);
/// Hard cap on each actuator command (`netbird up` blocks while it
/// retries an unreachable management plane — don't hang forever).
const ACTUATOR_CMD_TIMEOUT: Duration = Duration::from_secs(60);

#[derive(Args, Debug)]
pub struct HealArgs {
    /// Emit one JSON object (before / actions / after / outcome) instead
    /// of pretty lines.
    #[arg(long)]
    pub json: bool,
    /// Seconds to wait for the mesh to come back after the bounce.
    #[arg(long, default_value_t = 30)]
    pub wait: u64,
    /// Mesh client binary to bounce (PATH lookup unless a path is given).
    #[arg(long, env = "GOAT_NETBIRD_BIN", default_value = "netbird")]
    pub netbird_bin: String,
}

/// What happened, as one machine-stable word (the `--json` contract).
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize)]
#[serde(rename_all = "kebab-case")]
enum Outcome {
    /// All checks were already green — nothing was actuated.
    NoActionNeeded,
    /// Mesh bounced and the router is reachable again.
    Healed,
    /// Mesh bounced but the router never came back within --wait.
    NotHealed,
    /// Mesh is down but there is nothing to actuate (no netbird CLI, no
    /// goat-clientd socket).
    NoActuator,
    /// The failing check isn't mesh connectivity (cert / portal) — a
    /// bounce can't fix it, so none was attempted.
    NotHealable,
}

#[derive(Debug, Serialize)]
struct HealReport {
    before: Verdicts,
    actions: Vec<Action>,
    #[serde(skip_serializing_if = "Option::is_none")]
    after: Option<Verdicts>,
    outcome: Outcome,
    /// Human explanation of the outcome (English; mirrors the pretty
    /// closing line).
    explanation: String,
}

/// One actuator step and what it returned. `detail` carries the raw
/// (trimmed) command/RPC output — the evidence for "what happened".
#[derive(Debug, Serialize)]
struct Action {
    step: String,
    ok: bool,
    detail: String,
}

/// Which single next move the before-verdicts call for.
#[derive(Debug, PartialEq, Eq)]
enum Plan {
    NoActionNeeded,
    BounceMesh,
    /// Mesh is up; the failure is cert and/or portal — not bounce-fixable.
    NotHealable,
}

fn classify(v: &Verdicts) -> Plan {
    if !v.ztol.ok {
        Plan::BounceMesh
    } else if v.any_fail() {
        Plan::NotHealable
    } else {
        Plan::NoActionNeeded
    }
}

/// The not-bounce-fixable explanation, specific to which check is red.
fn not_healable_explanation(v: &Verdicts) -> String {
    let mut parts = Vec::new();
    if !v.cert.ok {
        parts.push(
            "the cert check is failing — a mesh bounce can't fix that; the cert needs a re-issue \
             (`goat admin rotate <anchor|alias>`, or ask your admin)",
        );
    }
    if !v.portal.ok {
        parts.push(
            "the mesh is up but the portal check is failing — that's server-side or a route \
             problem, not something a client bounce fixes; try `goat doctor` and check with \
             your admin",
        );
    }
    if !v.session.ok {
        parts.push(
            "the router port answers but a zenoh mTLS session does not open — the local TLS \
             files or config are unusable, or the router is wedged or rejecting this cert, \
             and a client bounce can't fix any of those; run `goat doctor` and check with \
             your admin",
        );
    }
    if !v.device.ok {
        parts.push(
            "the goat-clientd name store is present but its forwarder is not answering — \
             restart goat-clientd (launchctl kickstart / systemctl restart goat-clientd) or \
             read its logs; a mesh bounce won't touch it",
        );
    }
    format!("nothing to bounce: {}", parts.join("; also "))
}

pub async fn run(ctx: &super::Ctx, args: HealArgs) -> anyhow::Result<()> {
    let before = status::gather(ctx).await?;
    if !args.json {
        status::print_glyph_lines(&before);
    }

    let mut actions: Vec<Action> = Vec::new();
    let mut after: Option<Verdicts> = None;

    let (outcome, explanation) = match classify(&before) {
        Plan::NoActionNeeded => (
            Outcome::NoActionNeeded,
            "nothing to heal — all checks green".to_string(),
        ),
        Plan::NotHealable => (Outcome::NotHealable, not_healable_explanation(&before)),
        Plan::BounceMesh => match pick_actuator(&args.netbird_bin) {
            None => (
                Outcome::NoActuator,
                format!(
                    "mesh is down but there is nothing to actuate: `{}` is not on PATH and no \
                     goat-clientd socket was found — bring the mesh client up by hand",
                    args.netbird_bin
                ),
            ),
            Some(actuator) => {
                if !args.json {
                    println!();
                    println!("healing: mesh is down — bouncing {} (down → up)…", actuator.label());
                }
                bounce(&actuator, &mut actions, args.json).await;

                // Wait for the router to become reachable again. Poll just
                // the ZTOL probe (cheap TCP connect) — the full re-gather
                // happens once at the end.
                let router = status::router_endpoint(&ctx.profile)?;
                let started = Instant::now();
                let deadline = started + Duration::from_secs(args.wait);
                let mut recovered = false;
                loop {
                    if status::probe_ztol(&router).await.ok {
                        recovered = true;
                        break;
                    }
                    if Instant::now() >= deadline {
                        break;
                    }
                    tokio::time::sleep(RECHECK_INTERVAL).await;
                }

                if !args.json {
                    if recovered {
                        println!("  mesh recovered in {}s", started.elapsed().as_secs().max(1));
                    } else {
                        println!("  mesh did not recover within {}s", args.wait);
                    }
                    println!();
                }

                // Verify: re-run all the status probes so the reported end
                // state is observed, not assumed.
                let end = status::gather(ctx).await?;
                let (outcome, explanation) = if end.ztol.ok {
                    if end.any_fail() {
                        (
                            Outcome::Healed,
                            format!(
                                "mesh recovered after the bounce, but not everything is green: {}",
                                not_healable_explanation(&end)
                            ),
                        )
                    } else {
                        (
                            Outcome::Healed,
                            "healed: mesh recovered; all checks green".to_string(),
                        )
                    }
                } else {
                    (
                        Outcome::NotHealed,
                        format!(
                            "not healed: bounced {} but the router is still unreachable {}s \
                             later — run `{}` for the management-connection reason, and check \
                             your network path to the enrollment endpoint",
                            actuator.label(),
                            args.wait,
                            actuator.status_hint(),
                        ),
                    )
                };
                after = Some(end);
                (outcome, explanation)
            }
        },
    };

    if let Some(end) = &after {
        if !args.json {
            status::print_glyph_lines(end);
        }
    }

    // Exit on the end state (after if a heal ran, else before), using the
    // documented per-domain codes so wrappers can branch without parsing.
    // Computed before the report takes ownership of the verdicts.
    let code = exit_code_for(after.as_ref().unwrap_or(&before));

    if args.json {
        let report = HealReport {
            before,
            actions,
            after,
            outcome,
            explanation,
        };
        println!("{}", serde_json::to_string(&report).unwrap_or_default());
    } else {
        println!("{explanation}");
    }

    if code != crate::exit_codes::EXIT_SUCCESS {
        std::process::exit(code);
    }
    Ok(())
}

fn exit_code_for(v: &Verdicts) -> i32 {
    if !v.ztol.ok {
        crate::exit_codes::EXIT_OFF_MESH
    } else if !v.cert.ok {
        crate::exit_codes::EXIT_CERT
    } else if !v.portal.ok {
        crate::exit_codes::EXIT_PORTAL
    } else if v.any_fail() {
        // session / names / pqc / device red with mesh, cert and portal
        // green: no per-domain code exists, but the outcome is still not
        // success. `goat status` exits 1 for the same state (#1399), and a
        // wrapper must never read `not-healable` as 0.
        crate::exit_codes::EXIT_GENERIC
    } else {
        crate::exit_codes::EXIT_SUCCESS
    }
}

// ---------------------------------------------------------------------
// Actuators
// ---------------------------------------------------------------------

enum Actuator {
    /// The netbird CLI (resolved binary path + the name the operator gave).
    Netbird { bin: PathBuf, name: String },
    /// The goat-clientd JSON-RPC socket.
    Daemon(PathBuf),
}

impl Actuator {
    fn label(&self) -> String {
        match self {
            Actuator::Netbird { name, .. } => name.clone(),
            Actuator::Daemon(_) => "goat-clientd".to_string(),
        }
    }

    fn status_hint(&self) -> String {
        match self {
            Actuator::Netbird { name, .. } => format!("{name} status"),
            Actuator::Daemon(_) => "goat client status".to_string(),
        }
    }
}

fn pick_actuator(netbird_bin: &str) -> Option<Actuator> {
    if let Some(bin) = find_on_path(netbird_bin) {
        // Display by short name even when the operator gave a full path —
        // the full path still runs (and lands in `--json` via the resolved
        // `bin`), but every human line reads `netbird up`, not `/opt/…/netbird up`.
        let name = bin
            .file_name()
            .map(|s| s.to_string_lossy().into_owned())
            .unwrap_or_else(|| netbird_bin.to_string());
        return Some(Actuator::Netbird { bin, name });
    }
    if let Ok(sock) = super::client::default_socket() {
        if sock.exists() {
            return Some(Actuator::Daemon(sock));
        }
    }
    None
}

/// Resolve a binary: explicit path → itself if it exists; bare name →
/// first hit on PATH. No extra deps — mirrors what the shell would run.
fn find_on_path(bin: &str) -> Option<PathBuf> {
    // Windows accepts BOTH `/` and `\` as path separators — checking only
    // `MAIN_SEPARATOR` (`\` on Windows) meant `--netbird-bin ./netbird.exe`
    // (a forward slash, which every other Windows tool also accepts) was
    // misclassified as a bare PATH-lookup name instead of an explicit path,
    // and predictably failed to resolve.
    if bin.contains('/') || bin.contains('\\') {
        let p = PathBuf::from(bin);
        return p.is_file().then_some(p);
    }
    let path = std::env::var_os("PATH")?;
    for dir in std::env::split_paths(&path) {
        let cand = dir.join(bin);
        if cand.is_file() {
            return Some(cand);
        }
        #[cfg(windows)]
        {
            let exe = dir.join(format!("{bin}.exe"));
            if exe.is_file() {
                return Some(exe);
            }
        }
    }
    None
}

/// Run the bounce: capture pre-state, then down → up. Every step lands in
/// `actions` (the JSON evidence trail); pretty mode echoes as it goes.
async fn bounce(actuator: &Actuator, actions: &mut Vec<Action>, json: bool) {
    match actuator {
        Actuator::Netbird { bin, name } => {
            // Pre-state for the report: the management line explains WHY
            // the mesh was wedged (e.g. "Management: Disconnected, …").
            let pre = run_actuator_cmd(bin, name, "status").await;
            if !json {
                if let Some(line) = management_line(&pre.detail) {
                    println!("  {line}");
                }
            }
            actions.push(pre);

            for verb in ["down", "up"] {
                let a = run_actuator_cmd(bin, name, verb).await;
                if !json {
                    println!("  {}: {}", a.step, first_line(&a.detail));
                }
                actions.push(a);
            }
        }
        Actuator::Daemon(sock) => {
            for (verb, rpc) in [("down", "disconnect"), ("up", "connect")] {
                let a = match super::client::call(sock, rpc, serde_json::json!({})).await {
                    Ok(resp) => Action {
                        step: format!("goat-clientd {rpc}"),
                        ok: true,
                        detail: resp,
                    },
                    Err(e) => Action {
                        step: format!("goat-clientd {rpc}"),
                        ok: false,
                        detail: e.to_string(),
                    },
                };
                if !json {
                    println!("  goat-clientd {verb}: {}", first_line(&a.detail));
                }
                actions.push(a);
            }
        }
    }
}

async fn run_actuator_cmd(bin: &Path, name: &str, verb: &str) -> Action {
    let step = format!("{name} {verb}");
    match tokio::time::timeout(
        ACTUATOR_CMD_TIMEOUT,
        tokio::process::Command::new(bin).arg(verb).output(),
    )
    .await
    {
        Err(_) => Action {
            step,
            ok: false,
            detail: format!("timed out after {}s", ACTUATOR_CMD_TIMEOUT.as_secs()),
        },
        Ok(Err(e)) => Action {
            step,
            ok: false,
            detail: format!("failed to run: {e}"),
        },
        Ok(Ok(out)) => {
            let mut detail = String::from_utf8_lossy(&out.stdout).trim().to_string();
            let err = String::from_utf8_lossy(&out.stderr).trim().to_string();
            if !err.is_empty() {
                if !detail.is_empty() {
                    detail.push_str(" | ");
                }
                detail.push_str(&err);
            }
            if detail.is_empty() {
                detail = format!("(no output, exit {})", out.status.code().unwrap_or(-1));
            }
            Action {
                step,
                ok: out.status.success(),
                detail,
            }
        }
    }
}

/// The `Management:` line out of a `netbird status` dump, if present —
/// the one line that names why the client is wedged.
fn management_line(status_output: &str) -> Option<&str> {
    status_output
        .lines()
        .map(str::trim)
        .find(|l| l.starts_with("Management:"))
}

fn first_line(s: &str) -> &str {
    s.lines().next().unwrap_or("").trim()
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::cmd::status::Verdict;

    fn v(ok: bool) -> Verdict {
        Verdict {
            ok,
            summary: if ok { "ok".into() } else { "fail".into() },
            summary_loc: String::new(),
            neutral: false,
        }
    }

    fn verdicts(ztol: bool, portal: bool, cert: bool) -> Verdicts {
        Verdicts {
            ztol: v(ztol),
            portal: v(portal),
            cert: v(cert),
            names: v(true),
            pqc: v(true),
            device: v(true),
            session: v(true),
        }
    }

    #[test]
    fn classify_routes_each_state() {
        assert_eq!(classify(&verdicts(true, true, true)), Plan::NoActionNeeded);
        // ztol down always plans a bounce, whatever else is red.
        assert_eq!(classify(&verdicts(false, true, true)), Plan::BounceMesh);
        assert_eq!(classify(&verdicts(false, false, false)), Plan::BounceMesh);
        // mesh up but cert/portal red → nothing a bounce can do.
        assert_eq!(classify(&verdicts(true, false, true)), Plan::NotHealable);
        assert_eq!(classify(&verdicts(true, true, false)), Plan::NotHealable);
    }

    #[test]
    fn not_healable_explains_the_red_check() {
        let e = not_healable_explanation(&verdicts(true, true, false));
        assert!(e.contains("re-issue"), "cert path names the fix: {e}");
        let e = not_healable_explanation(&verdicts(true, false, true));
        assert!(e.contains("server-side"), "portal path: {e}");
        // Both red → both named.
        let e = not_healable_explanation(&verdicts(true, false, false));
        assert!(e.contains("re-issue") && e.contains("server-side"));
    }

    #[test]
    fn session_red_with_mesh_up_is_not_healable_and_explained() {
        // Router port answers, session does not open (#1399): a mesh
        // bounce cannot fix that, so heal must not plan one.
        let mut vs = verdicts(true, true, true);
        vs.session = v(false);
        assert_eq!(classify(&vs), Plan::NotHealable);
        let e = not_healable_explanation(&vs);
        assert!(e.contains("session does not open"), "session path: {e}");
    }

    #[test]
    fn exit_codes_map_worst_first() {
        use crate::exit_codes::*;
        assert_eq!(exit_code_for(&verdicts(true, true, true)), EXIT_SUCCESS);
        assert_eq!(exit_code_for(&verdicts(false, true, true)), EXIT_OFF_MESH);
        // off-mesh outranks cert outranks portal.
        assert_eq!(exit_code_for(&verdicts(false, false, false)), EXIT_OFF_MESH);
        assert_eq!(exit_code_for(&verdicts(true, false, false)), EXIT_CERT);
        assert_eq!(exit_code_for(&verdicts(true, false, true)), EXIT_PORTAL);
    }

    #[test]
    fn session_red_alone_is_not_exit_success() {
        // Wedged router (#1399): ztol/cert/portal green, session red. The
        // outcome prints `not-healable`; the exit code must agree.
        use crate::exit_codes::*;
        let mut vs = verdicts(true, true, true);
        vs.session = v(false);
        assert_eq!(exit_code_for(&vs), EXIT_GENERIC);
        // Per-domain codes still win when their domain is red too.
        vs.portal = v(false);
        assert_eq!(exit_code_for(&vs), EXIT_PORTAL);
    }

    #[test]
    fn management_line_found_and_trimmed() {
        let out = "OS: darwin/arm64\nManagement: Disconnected, reason: rpc error\nSignal: Disconnected";
        assert_eq!(
            management_line(out),
            Some("Management: Disconnected, reason: rpc error")
        );
        assert_eq!(management_line("no such line"), None);
    }

    #[test]
    fn outcome_serializes_kebab_case() {
        assert_eq!(
            serde_json::to_string(&Outcome::NoActionNeeded).unwrap(),
            "\"no-action-needed\""
        );
        assert_eq!(serde_json::to_string(&Outcome::Healed).unwrap(), "\"healed\"");
        assert_eq!(
            serde_json::to_string(&Outcome::NotHealed).unwrap(),
            "\"not-healed\""
        );
    }

    #[test]
    fn report_json_shape_omits_absent_after() {
        let report = HealReport {
            before: verdicts(true, true, true),
            actions: vec![],
            after: None,
            outcome: Outcome::NoActionNeeded,
            explanation: "nothing to heal — all checks green".into(),
        };
        let s = serde_json::to_string(&report).unwrap();
        assert!(s.contains("\"outcome\":\"no-action-needed\""));
        assert!(!s.contains("\"after\""), "absent after must be omitted: {s}");
        // The localized half of verdicts never leaks into --json.
        assert!(!s.contains("summary_loc"));
    }

    #[test]
    fn find_on_path_resolves_explicit_and_bare() {
        let dir = std::env::temp_dir().join(format!("goat-heal-path-test-{}", std::process::id()));
        std::fs::create_dir_all(&dir).unwrap();
        let bin = dir.join("fake-netbird");
        std::fs::write(&bin, "#!/bin/sh\n").unwrap();

        // Explicit path → resolves iff the file exists.
        assert_eq!(find_on_path(bin.to_str().unwrap()), Some(bin.clone()));
        assert_eq!(find_on_path(dir.join("absent").to_str().unwrap()), None);

        std::fs::remove_dir_all(&dir).ok();
    }
}
