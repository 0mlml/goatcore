// SPDX-License-Identifier: Apache-2.0
//
// goat-consistency-health — the `consistency` domain of the environment-health
// surface (docs/design/consistency-foundation.md §5, ADR 1110 / ADR 1036).
//
// One binary, two modes, following the time domain's raw-feed/rolled-envelope
// split exactly rather than overloading one topic with two scopes:
//
//   --mode publish   one per node. Reads the snapshot the host-side collector
//                    writes (ops/consistency/collect/consistency-snapshot.py)
//                    and puts a ConsistencyReportV1 on
//                    ops/consistency/report/<host_id>. FACTS ONLY — no grade.
//
//   --mode rollup    one passive reader per goatnet. Subscribes
//                    ops/consistency/report/**, reads its goatnet's
//                    intent.json from disk, grades through the SHARED library
//                    (goat-consistency, the same grammar the Python side
//                    executes), and publishes EnvHealthV1{domain:"consistency"}
//                    on ops/consistency/health/<site>.
//
// Both topics are ops/** and therefore C0 control (Block + InteractiveHigh), so
// the consistency signal is never starved by the load it reports on.
//
// BROKERLESS-SAFE, and this is worth stating because a "consistency rollup" is
// exactly the shape a re-introduced broker would take: each node self-publishes
// its own record, so there is no central collector; the rollup is a passive
// reader with no authority, so losing it loses the VIEW and not the facts; the
// comparison is computed at read time and stored nowhere, so there is no
// authoritative state to become the thing everyone must reach; and no
// enforcement path reads any of it. Kill it and nothing changes.

use goat_consistency_wire as wire;

use anyhow::{Context, Result};
use clap::Parser;
use goat_consistency::{project_goatnet, Grammar, ObservedComponent, ObservedNode, ObservedRender};
use prost::Message as _;
use serde_json::Value;
use std::collections::BTreeMap;
use std::path::Path;
use std::time::{Duration, SystemTime, UNIX_EPOCH};
use tracing::{info, warn};
use zenoh::qos::{CongestionControl, Priority};

use wire::{
    ConsistencyDetailV1, ConsistencyReportV1, ConsistencyRowV1, ComponentStateV1, EnvDetail,
    EnvHealthV1, NodeConsistencyV1, RenderStateV1,
};

pub const REPORT_PREFIX: &str = "ops/consistency/report";
pub const REPORT_SELECTOR: &str = "ops/consistency/report/**";
pub const HEALTH_PREFIX: &str = "ops/consistency/health";
pub const DOMAIN: &str = "consistency";

// ---------------------------------------------------------------------------
// The collector's snapshot — the publisher's only input.
// `#[serde(default)]` on every field keeps a partial snapshot from killing the
// whole read; the collector's own `collector_note` says what was partial.
// ---------------------------------------------------------------------------

#[derive(serde::Deserialize, Default)]
pub struct Snapshot {
    #[serde(default)]
    host_id: String,
    #[serde(default)]
    goatnet: String,
    #[serde(default)]
    member_group: String,
    #[serde(default)]
    intent_content_hash: String,
    #[serde(default)]
    intent_serial: u64,
    #[serde(default)]
    intent_generated_at: String,
    #[serde(default)]
    intent_commit: String,
    #[serde(default)]
    intent_verified: bool,
    #[serde(default)]
    applied_at_unix_ms: i64,
    #[serde(default)]
    collected_at_unix_ms: i64,
    #[serde(default)]
    collector_note: String,
    #[serde(default)]
    components: Vec<SnapComponent>,
    #[serde(default)]
    renders: Vec<SnapRender>,
}

#[derive(serde::Deserialize, Default)]
pub struct SnapComponent {
    #[serde(default)]
    component: String,
    #[serde(default)]
    artifact_type: String,
    #[serde(default, rename = "ref")]
    reference: String,
    #[serde(default)]
    prior_ref: String,
    #[serde(default)]
    ts_unix_ms: i64,
    #[serde(default)]
    reversible: bool,
    #[serde(default)]
    reconciled: String,
    #[serde(default)]
    live_ref: String,
}

#[derive(serde::Deserialize, Default)]
pub struct SnapRender {
    #[serde(default)]
    component: String,
    #[serde(default)]
    path: String,
    #[serde(default)]
    sha256: String,
    #[serde(default)]
    recorded_sha256: String,
    #[serde(default)]
    recorded_at_unix_ms: i64,
    #[serde(default)]
    note: String,
}

fn now_unix_ms() -> i64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_millis() as i64)
        .unwrap_or(0)
}

/// `%Y-%m-%dT%H:%M:%SZ` -> unix ms, or 0 when unstatable. Zero is rendered as
/// unstatable and never treated as the epoch.
fn stamp_to_ms(text: &str) -> i64 {
    goat_consistency::intent::parse_utc_stamp(text).map(|s| s * 1000).unwrap_or(0)
}

/// Build the per-node record. Note what is NOT here: a grade. A node knows what
/// it is running and does not know whether that is current.
pub fn report_from(snap: &Snapshot, fallback_host: &str) -> ConsistencyReportV1 {
    ConsistencyReportV1 {
        host_id: if snap.host_id.is_empty() { fallback_host.to_string() } else { snap.host_id.clone() },
        goatnet: snap.goatnet.clone(),
        member_group: snap.member_group.clone(),
        intent_content_hash: snap.intent_content_hash.clone(),
        intent_serial: snap.intent_serial,
        intent_generated_at_unix_ms: stamp_to_ms(&snap.intent_generated_at),
        intent_commit: snap.intent_commit.clone(),
        applied_at_unix_ms: snap.applied_at_unix_ms,
        intent_verified: snap.intent_verified,
        components: snap
            .components
            .iter()
            .map(|c| ComponentStateV1 {
                component: c.component.clone(),
                artifact_type: c.artifact_type.clone(),
                r#ref: c.reference.clone(),
                prior_ref: c.prior_ref.clone(),
                ts_unix_ms: c.ts_unix_ms,
                reversible: c.reversible,
                reconciled: c.reconciled.clone(),
                live_ref: c.live_ref.clone(),
            })
            .collect(),
        renders: snap
            .renders
            .iter()
            .map(|r| RenderStateV1 {
                component: r.component.clone(),
                path: r.path.clone(),
                sha256: r.sha256.clone(),
                recorded_sha256: r.recorded_sha256.clone(),
                recorded_at_unix_ms: r.recorded_at_unix_ms,
                note: r.note.clone(),
            })
            .collect(),
        collected_at_unix_ms: snap.collected_at_unix_ms,
        collector_note: snap.collector_note.clone(),
    }
}

/// The honest "this node could not read its own facts" record: an empty report
/// that still identifies the node. A node that stops publishing entirely
/// vanishes from the denominator; one that publishes an empty record stays
/// visible and grades `indeterminate`, which is what ADR 1109 P15 requires.
pub fn unreadable_report(host_id: &str, note: &str) -> ConsistencyReportV1 {
    ConsistencyReportV1 {
        host_id: host_id.to_string(),
        collected_at_unix_ms: now_unix_ms(),
        collector_note: note.to_string(),
        ..Default::default()
    }
}

// ---------------------------------------------------------------------------
// Rollup
// ---------------------------------------------------------------------------

fn observed_from(report: &ConsistencyReportV1, now_ms: i64) -> ObservedNode {
    ObservedNode {
        host_id: report.host_id.clone(),
        member_group: report.member_group.clone(),
        intent_content_hash: report.intent_content_hash.clone(),
        intent_serial: report.intent_serial,
        intent_verified: report.intent_verified,
        report_age_ms: (now_ms - report.collected_at_unix_ms).max(0),
        components: report
            .components
            .iter()
            .map(|c| ObservedComponent {
                component: c.component.clone(),
                artifact_type: c.artifact_type.clone(),
                reference: c.r#ref.clone(),
                reconciled: c.reconciled.clone(),
                live_ref: c.live_ref.clone(),
            })
            .collect(),
        renders: report
            .renders
            .iter()
            .map(|r| ObservedRender {
                component: r.component.clone(),
                path: r.path.clone(),
                sha256: r.sha256.clone(),
                recorded_sha256: r.recorded_sha256.clone(),
                note: r.note.clone(),
            })
            .collect(),
    }
}

/// Read, verify and integrity-check the goatnet's committed generation.
///
/// Since CF-6 the whole read path lives in the library
/// (`goat_consistency::verify::load_verified`) because the console reads the
/// same file the same way, and two copies of a fail-closed check is how one of
/// them quietly stops failing closed. This wrapper keeps the meter's own name
/// for it. P14 fail-closed: an unverifiable artifact is REFUSED, never
/// rendered as fact — see the library function for the three layers.
pub fn load_intent(path: &Path, pubkey_dir: &Path) -> (Option<Value>, String) {
    goat_consistency::verify::load_verified(path, pubkey_dir)
}

/// Roll every reporting node into one EnvHealthV1{domain:"consistency"}.
///
/// `stale_after_ms` is grounding, not a grade: a record older than the window
/// is dropped from the reading entirely rather than aged into a stale fact, so
/// `nodes_reporting` always means "reported within this window" — which is what
/// makes it a legitimate numerator over `nodes_expected`.
#[allow(clippy::too_many_arguments)]
pub fn roll_up(
    intent: Option<&Value>,
    intent_note: &str,
    reports: &[ConsistencyReportV1],
    site: &str,
    window_secs: u32,
    now_ms: i64,
    stale_after_ms: i64,
    g: &Grammar,
) -> Result<EnvHealthV1> {
    let fresh: Vec<&ConsistencyReportV1> = reports
        .iter()
        .filter(|r| now_ms - r.collected_at_unix_ms <= stale_after_ms)
        .collect();
    let observed: Vec<ObservedNode> =
        fresh.iter().map(|r| observed_from(r, now_ms)).collect();

    let projection = project_goatnet(intent, &observed, now_ms / 1000, g)
        .map_err(|e| anyhow::anyhow!("grading: {e}"))?;

    let nodes: Vec<NodeConsistencyV1> = projection
        .nodes
        .iter()
        .map(|n| NodeConsistencyV1 {
            host_id: n.host_id.clone(),
            member_group: n.member_group.clone(),
            color: n.color.clone(),
            grade: n.grade.clone(),
            intent_serial: n.intent_serial,
            intent_content_hash: n.intent_content_hash.clone(),
            behind_by: n.behind_by,
            intent_age_seconds: n.intent_age_seconds,
            report_age_ms: n.report_age_ms,
            intent_verified: n.intent_verified,
            components_governed: n.components_governed as u32,
            components_observed: n.components_observed as u32,
            renders_declared: n.renders_declared as u32,
            renders_observed: n.renders_observed as u32,
            coverage_gaps: n.coverage_gaps as u32,
            rows: n
                .rows
                .iter()
                .map(|r| ConsistencyRowV1 {
                    subject: r.subject.clone(),
                    name: r.name.clone(),
                    artifact_type: r.artifact_type.clone(),
                    grade: r.grade.clone(),
                    color: r.color.clone(),
                    why: r.why.clone(),
                    observed: r.observed.clone(),
                    expected: r.expected.clone(),
                    remediation_kind: r.remediation.as_ref().map(|x| x.kind.clone()).unwrap_or_default(),
                    remediation: r.remediation.as_ref().map(|x| x.pointer.clone()).unwrap_or_default(),
                    remediation_refuses: r.remediation.as_ref().map(|x| x.refuses).unwrap_or(false),
                    coverage: r.coverage.clone(),
                })
                .collect(),
        })
        .collect();

    Ok(EnvHealthV1 {
        domain: DOMAIN.to_string(),
        scope: site.to_string(),
        ts_unix_ms: now_ms,
        overall_color: projection.overall_color.clone(),
        sample_count: nodes.len() as u64,
        window_secs,
        detail: Some(EnvDetail::Consistency(ConsistencyDetailV1 {
            nodes,
            goatnet: projection.goatnet.clone(),
            current_serial: projection.current_serial,
            current_content_hash: projection.current_content_hash.clone(),
            nodes_expected: projection.nodes_expected,
            nodes_reporting: projection.nodes_reporting,
            skew_band_generations: projection.skew_band_generations,
            skew_band_seconds: projection.skew_band_seconds,
            unreported_members: projection.unreported_members.clone(),
            intent_readable: intent.is_some(),
            intent_note: intent_note.to_string(),
        })),
    })
}

// ---------------------------------------------------------------------------
// CLI + session
// ---------------------------------------------------------------------------

#[derive(Parser, Debug)]
#[command(
    name = "goat-consistency-health",
    about = "Consistency domain of the environment-health surface — per-node convergence records and the per-goatnet rollup (consistency-foundation.md §5 / ADR 1110)"
)]
pub struct Args {
    /// Zenoh endpoint, e.g. `tls/zagros-sandbox-zenoh-router:7447`. A `tcp/`
    /// endpoint skips TLS entirely, which is the local development path.
    #[arg(long, env = "GCH_ENDPOINT")]
    pub endpoint: String,

    /// publish (default) | rollup
    #[arg(long, env = "GCH_MODE", default_value = "publish")]
    pub mode: String,

    /// Node identifier — the `<host_id>` segment of
    /// ops/consistency/report/<host_id> (publish mode).
    #[arg(long, env = "GCH_NODE", default_value = "")]
    pub node: String,

    /// Site tag — the `<site>` segment of ops/consistency/health/<site>
    /// (rollup mode).
    #[arg(long, env = "GCH_SITE", default_value = "")]
    pub site: String,

    /// Path of the collector's snapshot (bind-mounted read-only; written
    /// atomically by the consistency-snapshot systemd timer).
    #[arg(long, env = "GCH_SNAPSHOT_FILE", default_value = "/state/snapshot.json")]
    pub snapshot_file: String,

    /// rollup mode: the goatnet's committed IntentV1 artifact, read from disk
    /// and verified at every read — detached signature (CF-5; `<file>.sig`
    /// beside it) plus content_hash self-consistency. Unverifiable => the
    /// rollup refuses to grade and says why.
    #[arg(long, env = "GCH_INTENT_FILE", default_value = "/intent/intent.json")]
    pub intent_file: String,

    /// rollup mode: dir of `<ca_id>.pem` SPKI public keys (the committed
    /// ops/enrollment/public-keys set, deployed beside the artifact). The
    /// artifact's ca_id resolves here or verification fails — the key is
    /// never taken on the artifact's word alone (ADR 1082's rule).
    #[arg(long, env = "GCH_PUBKEY_DIR", default_value = "/intent/public-keys")]
    pub pubkey_dir: String,

    /// Leaf X.509 cert (PEM) for mTLS. Required for a `tls/` endpoint.
    #[arg(long, env = "GCH_CERT")]
    pub cert: Option<String>,
    /// Matching private key (PEM).
    #[arg(long, env = "GCH_KEY_FILE")]
    pub key: Option<String>,
    /// CA root cert (PEM) used to verify the router.
    #[arg(long, env = "GCH_ROOT_CA")]
    pub root_ca: Option<String>,

    /// Publish interval (ms) — the same cadence as the sibling env-health
    /// meters, so a one-shot `goat health --domain consistency` catches a beat.
    #[arg(long, env = "GCH_INTERVAL_MS", default_value_t = 30_000)]
    pub interval_ms: u64,

    /// Snapshot staleness window (ms): a snapshot older than this publishes an
    /// empty record rather than a stale one. Default = ~2 collector intervals.
    #[arg(long, env = "GCH_STALE_AFTER_MS", default_value_t = 7_800_000)]
    pub stale_after_ms: u64,

    /// rollup mode: a node record older than this drops out of the reading, so
    /// `nodes_reporting` always means "reported within the window".
    #[arg(long, env = "GCH_RECORD_STALE_AFTER_MS", default_value_t = 900_000)]
    pub record_stale_after_ms: u64,
}

pub async fn open_session(args: &Args) -> Result<zenoh::Session> {
    let mut config = zenoh::Config::default();
    config
        .insert_json5("mode", "\"client\"")
        .map_err(|e| anyhow::anyhow!("config mode: {e}"))?;
    config
        .insert_json5("connect/endpoints", &format!("[\"{}\"]", args.endpoint))
        .map_err(|e| anyhow::anyhow!("config connect/endpoints: {e}"))?;
    if args.endpoint.starts_with("tls/") {
        let cert = args.cert.as_ref().context("--cert required for tls/ endpoint")?;
        let key = args.key.as_ref().context("--key required for tls/ endpoint")?;
        let root_ca = args
            .root_ca
            .as_ref()
            .context("--root-ca required for tls/ endpoint")?;
        let tls_block = format!(
            r#"{{ "root_ca_certificate": "{root_ca}", "connect_certificate": "{cert}", "connect_private_key": "{key}", "enable_mtls": true, "verify_name_on_connect": false }}"#
        );
        config
            .insert_json5("transport/link/tls", &tls_block)
            .map_err(|e| anyhow::anyhow!("config transport/link/tls: {e}"))?;
    }
    zenoh::open(config)
        .await
        .map_err(|e| anyhow::anyhow!("zenoh::open failed: {e}"))
}

/// Read + parse the collector's snapshot, honestly: stale mtime, missing file,
/// or a parse failure all return None with the reason.
fn read_snapshot(path: &Path, stale_after: Duration) -> Result<Snapshot, String> {
    let meta = std::fs::metadata(path).map_err(|e| format!("snapshot missing: {e}"))?;
    if let Ok(mtime) = meta.modified() {
        let age = SystemTime::now().duration_since(mtime).unwrap_or(Duration::ZERO);
        if age > stale_after {
            return Err(format!(
                "snapshot is {}s old — the consistency-snapshot timer is not running",
                age.as_secs()
            ));
        }
    }
    let bytes = std::fs::read(path).map_err(|e| format!("snapshot unreadable: {e}"))?;
    serde_json::from_slice::<Snapshot>(&bytes).map_err(|e| format!("snapshot undecodable: {e}"))
}

async fn publish(session: &zenoh::Session, args: &Args) -> Result<()> {
    anyhow::ensure!(!args.node.is_empty(), "--node required in publish mode");
    let key = format!("{REPORT_PREFIX}/{}", args.node);
    let snapshot_path = std::path::PathBuf::from(&args.snapshot_file);
    let stale_after = Duration::from_millis(args.stale_after_ms);
    let interval = Duration::from_millis(args.interval_ms);
    info!(
        zid = %session.zid(),
        endpoint = args.endpoint.as_str(),
        node = args.node.as_str(),
        publishes = key.as_str(),
        "session open; publishing this node's convergence record (facts, no verdict)"
    );

    let mut ticker = tokio::time::interval(interval);
    ticker.set_missed_tick_behavior(tokio::time::MissedTickBehavior::Skip);
    let mut sigterm =
        tokio::signal::unix::signal(tokio::signal::unix::SignalKind::terminate())
            .context("install SIGTERM handler")?;

    loop {
        tokio::select! {
            _ = ticker.tick() => {
                let report = match read_snapshot(&snapshot_path, stale_after) {
                    Ok(snap) => report_from(&snap, &args.node),
                    Err(note) => {
                        warn!(note = note.as_str(), "publishing an empty record");
                        unreadable_report(&args.node, &note)
                    }
                };
                let n = report.components.len();
                let serial = report.intent_serial;
                match session
                    .put(&key, report.encode_to_vec())
                    .congestion_control(CongestionControl::Block)
                    .priority(Priority::InteractiveHigh)
                    .await
                {
                    Ok(()) => info!(key = key.as_str(), components = n, intent_serial = serial,
                                    "published consistency report"),
                    Err(e) => warn!(error = %e, "publish consistency report failed"),
                }
            }
            _ = sigterm.recv() => { info!("SIGTERM received; shutting down"); break; }
            _ = tokio::signal::ctrl_c() => { info!("SIGINT received; shutting down"); break; }
        }
    }
    let _ = session.close().await;
    Ok(())
}

async fn rollup(session: &zenoh::Session, args: &Args) -> Result<()> {
    anyhow::ensure!(!args.site.is_empty(), "--site required in rollup mode");
    let g = Grammar::load().map_err(|e| anyhow::anyhow!("the committed grammar is unusable: {e}"))?;
    let health_key = format!("{HEALTH_PREFIX}/{}", args.site);
    let intent_path = std::path::PathBuf::from(&args.intent_file);
    let pubkey_dir = std::path::PathBuf::from(&args.pubkey_dir);
    let interval = Duration::from_millis(args.interval_ms);

    let subscriber = session
        .declare_subscriber(REPORT_SELECTOR)
        .await
        .map_err(|e| anyhow::anyhow!("declare subscriber on `{REPORT_SELECTOR}`: {e}"))?;
    info!(
        zid = %session.zid(),
        subscribes = REPORT_SELECTOR,
        publishes = health_key.as_str(),
        intent = args.intent_file.as_str(),
        "session open; passive rollup — nodes self-publish, this only listens"
    );

    // Keyed by host so a re-publish replaces rather than accumulates. Nothing
    // is persisted: kill this process and the view is rebuilt from the bus in
    // one publish interval.
    let mut latest: BTreeMap<String, ConsistencyReportV1> = BTreeMap::new();
    let mut ticker = tokio::time::interval(interval);
    ticker.set_missed_tick_behavior(tokio::time::MissedTickBehavior::Skip);
    let mut sigterm =
        tokio::signal::unix::signal(tokio::signal::unix::SignalKind::terminate())
            .context("install SIGTERM handler")?;

    loop {
        tokio::select! {
            s = subscriber.recv_async() => {
                let Ok(sample) = s else { break };
                let payload = sample.payload().to_bytes();
                match wire::decode_report(payload.as_ref()) {
                    Some(report) => {
                        // Trust the KEY for identity, not the payload: the key
                        // is what the router's write ACL authorised.
                        let host = sample.key_expr().as_str()
                            .rsplit('/').next().unwrap_or("").to_string();
                        if host.is_empty() { continue; }
                        let mut report = report;
                        report.host_id = host.clone();
                        latest.insert(host, report);
                    }
                    None => warn!(key = sample.key_expr().as_str(),
                                  "undecodable payload on {REPORT_SELECTOR}"),
                }
            }
            _ = ticker.tick() => {
                // Intent is re-read and re-verified EVERY beat (signature +
                // content hash), not cached at start: a regenerated artifact
                // must take effect without a restart, and an artifact that
                // becomes unreadable or unverifiable must stop being graded
                // against immediately.
                let (intent, note) = load_intent(&intent_path, &pubkey_dir);
                if intent.is_none() && !note.is_empty() {
                    warn!(note = note.as_str(), "refusing to grade");
                }
                let reports: Vec<ConsistencyReportV1> = latest.values().cloned().collect();
                let reading = roll_up(
                    intent.as_ref(), &note, &reports, &args.site,
                    (interval.as_secs() as u32).max(1), now_unix_ms(),
                    args.record_stale_after_ms as i64, &g,
                )?;
                let overall = reading.overall_color.clone();
                let n = reading.sample_count;
                match session
                    .put(&health_key, reading.encode_to_vec())
                    .congestion_control(CongestionControl::Block)
                    .priority(Priority::InteractiveHigh)
                    .await
                {
                    Ok(()) => info!(key = health_key.as_str(), n = n,
                                    overall = overall.as_str(), "published consistency health"),
                    Err(e) => warn!(error = %e, "publish consistency health failed"),
                }
            }
            _ = sigterm.recv() => { info!("SIGTERM received; shutting down"); break; }
            _ = tokio::signal::ctrl_c() => { info!("SIGINT received; shutting down"); break; }
        }
    }
    let _ = session.close().await;
    Ok(())
}

#[tokio::main]
async fn main() -> Result<()> {
    tracing_subscriber::fmt()
        .with_env_filter(
            tracing_subscriber::EnvFilter::try_from_default_env()
                .unwrap_or_else(|_| tracing_subscriber::EnvFilter::new("info")),
        )
        .init();

    let args = Args::parse();
    let session = open_session(&args).await.context("open zenoh session")?;
    match args.mode.as_str() {
        "rollup" => rollup(&session, &args).await,
        "publish" => publish(&session, &args).await,
        other => anyhow::bail!("unknown --mode `{other}` (publish|rollup)"),
    }
}

#[cfg(test)]
mod tests;
