// SPDX-License-Identifier: Apache-2.0
//
// goat-consistency-console — the CF-6 consistency console
// (docs/design/consistency-foundation.md §7, ADR 1110 D5).
//
// A STATELESS PROJECTION. It holds a Zenoh session, subscribes the rollup
// (ops/consistency/health/**) and the per-node records (ops/consistency/
// report/**), reads its goatnet's committed IntentV1 from disk VERIFIED AT
// EVERY READ, renders server-side HTML — and stores NOTHING. No database, no
// history, no collector, no cache beyond the ordinary in-memory latest-sample
// map every subscriber holds. Kill it and nothing changes: no node's
// convergence, deploy, rollback or health publication reads it (§7.9).
//
// It EXECUTES NOTHING. Remediation pointers are rendered verbatim from the
// envelope the rollup meter published — the console never composes,
// paraphrases, parameterizes or runs a command, and holds no credential that
// could (§6.4). All comparison semantics live in the shared library
// (goat-consistency); this binary computes no verdict of its own — the live
// grades arrive already computed on the wire, and the repo-vantage grid is
// `grade_intent`, the same call every other consumer makes.
//
// BROKERLESS-SAFE (§7.10): this adds a VANTAGE, not a chokepoint. Losing it
// loses a view; the facts keep publishing.
//
// TWO DEPLOYMENT MODES, one binary (§7.8, ADR 1110 D5):
//   - single (the default): one goatnet from --endpoint/--site, the CF-6
//     console exactly as deployed on a mgmt-host.
//   - aggregator (--aggregate <config.yaml>): several goatnets, one Zenoh
//     session EACH (separate fabrics, separate CAs), one slot of state each,
//     rendered as the §7.4 goatnet band above per-goatnet grids — every grid
//     through the SAME code path as single mode, so the aggregator cannot
//     develop its own semantics. Read-only, never load-bearing: it may
//     render; it may not decide. A goatnet it cannot reach renders
//     indeterminate with the age of its last reading — never green, never
//     absent — and killing the aggregator changes nothing on any goatnet
//     (§7.9 C1: nothing reads it, and it writes nothing anywhere).

mod aggregate;
mod api;
mod release;
mod render;

pub use aggregate::GoatnetCfg;
use anyhow::{Context, Result};
use axum::extract::{Path as AxumPath, RawQuery, State};
use axum::response::{Html, IntoResponse};
use axum::routing::get;
use axum::Json;
use clap::Parser;
use goat_consistency::Grammar;
use goat_consistency_wire as wire;
use serde_json::Value;
use std::collections::BTreeMap;
use std::path::PathBuf;
use std::sync::Arc;
use std::time::{Duration, SystemTime, UNIX_EPOCH};
use tokio::sync::RwLock;
use tracing::{info, warn};

pub const HEALTH_SELECTOR: &str = "ops/consistency/health/**";
pub const HEALTH_PREFIX: &str = "ops/consistency/health/";
pub const REPORT_SELECTOR: &str = "ops/consistency/report/**";
pub const REPORT_PREFIX: &str = "ops/consistency/report/";

fn now_unix_ms() -> i64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_millis() as i64)
        .unwrap_or(0)
}

// ---------------------------------------------------------------------------
// State — the whole of it. Everything here is re-derivable from the bus and
// the disk within one publish interval; that is the §7.3 zero-private-state
// rule stated as a struct.
// ---------------------------------------------------------------------------

#[derive(Debug, Clone)]
pub struct StoredEnvelope {
    pub env: wire::EnvHealthV1,
    pub received_unix_ms: i64,
}

#[derive(Debug, Clone)]
pub struct StoredReport {
    pub report: wire::ConsistencyReportV1,
    pub received_unix_ms: i64,
}

#[derive(Debug, Default)]
pub struct Latest {
    /// Latest rolled envelope per <site> scope. Keyed by the KEY's last
    /// segment, not the payload's claim — the key is what the router's write
    /// ACL authorised (the same rule the rollup applies to reports).
    pub rollups: BTreeMap<String, StoredEnvelope>,
    /// Latest per-node record per <host_id> — the drill's input.
    pub reports: BTreeMap<String, StoredReport>,
    /// Honesty about the console's own vantage: what the Zenoh session is
    /// doing right now. Rendered on the page — a console that cannot hear is
    /// a different fact from a fleet that is not speaking.
    pub bus_note: String,
}

/// Fold one received sample into the latest-map. Pure, so the tests can drive
/// it with exactly what a subscriber hands the loop below.
pub fn ingest(latest: &mut Latest, key: &str, payload: &[u8], now_ms: i64) {
    if let Some(scope) = key.strip_prefix(HEALTH_PREFIX) {
        if scope.is_empty() || scope.contains('/') {
            return; // not a <site> leaf — ignore rather than guess
        }
        match wire::decode_health(payload) {
            Some(mut env) => {
                env.scope = scope.to_string();
                latest
                    .rollups
                    .insert(scope.to_string(), StoredEnvelope { env, received_unix_ms: now_ms });
            }
            None => warn!(key, "undecodable payload on {HEALTH_SELECTOR}"),
        }
    } else if let Some(host) = key.strip_prefix(REPORT_PREFIX) {
        if host.is_empty() || host.contains('/') {
            return;
        }
        match wire::decode_report(payload) {
            Some(mut report) => {
                report.host_id = host.to_string();
                latest
                    .reports
                    .insert(host.to_string(), StoredReport { report, received_unix_ms: now_ms });
            }
            None => warn!(key, "undecodable payload on {REPORT_SELECTOR}"),
        }
    }
}

// ---------------------------------------------------------------------------
// Block 94 siblings (§7.7): Option<String> base URLs, hidden when unset.
// ---------------------------------------------------------------------------

#[derive(Debug, Clone, Default, serde::Serialize)]
pub struct Siblings {
    pub panoscope: Option<String>,
    pub stethoscope: Option<String>,
    pub flashlight: Option<String>,
    pub portal: Option<String>,
    /// The CROSS-ENV comparison page. A site console can only ever see its own
    /// goatnet — the one question it structurally cannot answer is "and how
    /// does that compare to the other environment". Linking there is what
    /// stops three URLs for one question reading as three products.
    pub crossenv: Option<String>,
}

fn sibling_env(name: &str) -> Option<String> {
    std::env::var(name)
        .ok()
        .map(|s| s.trim().trim_end_matches('/').to_string())
        .filter(|s| !s.is_empty())
}

impl Siblings {
    pub fn from_env() -> Self {
        Siblings {
            panoscope: sibling_env("CONSISTENCY_SIBLING_PANOSCOPE_URL"),
            stethoscope: sibling_env("CONSISTENCY_SIBLING_STETHOSCOPE_URL"),
            flashlight: sibling_env("CONSISTENCY_SIBLING_FLASHLIGHT_URL"),
            portal: sibling_env("CONSISTENCY_SIBLING_PORTAL_URL"),
            crossenv: sibling_env("CONSISTENCY_SIBLING_CROSSENV_URL"),
        }
    }
}

// ---------------------------------------------------------------------------
// CLI + session — the GCH_* table's sibling, prefix GCC_.
// ---------------------------------------------------------------------------

#[derive(Parser, Debug, Clone)]
#[command(
    name = "goat-consistency-console",
    about = "CF-6 consistency console — a stateless, read-only, mesh-internal projection of the consistency rollup (consistency-foundation.md §7 / ADR 1110 D5)"
)]
pub struct Args {
    /// Zenoh endpoint, e.g. `tls/<site>-zenoh-router:7447`. A `tcp/`
    /// endpoint skips TLS entirely, which is the local development path.
    /// Required in single mode; refused with --aggregate.
    #[arg(long, env = "GCC_ENDPOINT")]
    pub endpoint: Option<String>,

    /// This goatnet's site tag — which ops/consistency/health/<site> scope is
    /// "ours". The page renders every scope it hears; this one is the page's
    /// subject, and its absence renders as "nothing heard", never as healthy.
    /// Required in single mode; refused with --aggregate.
    #[arg(long, env = "GCC_SITE")]
    pub site: Option<String>,

    /// CF-7 AGGREGATOR MODE (§7.8): a YAML config declaring several goatnets
    /// — per goatnet an endpoint, a cert/CA set, the committed intent path
    /// and the expected site key (see
    /// ops/consistency/aggregator/aggregator.example.yaml). Mutually
    /// exclusive with --endpoint/--site.
    #[arg(long, env = "GCC_AGGREGATE_CONFIG", value_name = "CONFIG_YAML")]
    pub aggregate: Option<String>,

    /// HTTP bind address.
    #[arg(long, env = "GCC_LISTEN", default_value = "0.0.0.0:9760")]
    pub listen: String,

    /// The goatnet's committed IntentV1 artifact, read from disk and verified
    /// on EVERY render — detached signature (CF-5) plus content_hash
    /// self-consistency. Unverifiable => the page says so and refuses to
    /// render the committed-intent grid.
    #[arg(long, env = "GCC_INTENT_FILE", default_value = "/intent/intent.json")]
    pub intent_file: String,

    /// Dir of `<ca_id>.pem` SPKI public keys (the committed
    /// ops/enrollment/public-keys set, deployed beside the artifact).
    #[arg(long, env = "GCC_PUBKEY_DIR", default_value = "/intent/public-keys")]
    pub pubkey_dir: String,

    /// Leaf X.509 cert (PEM) for mTLS. Required for a `tls/` endpoint.
    #[arg(long, env = "GCC_CERT")]
    pub cert: Option<String>,
    /// Matching private key (PEM).
    #[arg(long, env = "GCC_KEY_FILE")]
    pub key: Option<String>,
    /// CA root cert (PEM) used to verify the router.
    #[arg(long, env = "GCC_ROOT_CA")]
    pub root_ca: Option<String>,

    /// A rollup reading older than this renders as stale (idle presentation,
    /// age stated) rather than as a current fact. ~2x the meter's beat.
    #[arg(long, env = "GCC_ROLLUP_STALE_AFTER_MS", default_value_t = 900_000)]
    pub rollup_stale_after_ms: i64,

    /// The `release-readiness-v1` document rendered at /release (release-graduation.md §8),
    /// written by `ops/release/graduate.py readiness` and placed beside the
    /// intent artifact at deploy time. Fleet-scoped, not per-goatnet: the
    /// ladder spans the rungs, so aggregator mode reads the same one file.
    /// Re-read from disk on every render; absent renders as a refusal.
    #[arg(long, env = "GCC_RELEASE_FILE", default_value = "/intent/release-readiness.json")]
    pub release_file: String,
}

/// One goatnet's worth of state: its config and its latest-map. Single mode
/// is exactly one slot; aggregator mode is one PER configured goatnet —
/// separate fabrics, separate CAs, separate sessions, separate honesty.
pub struct Slot {
    pub cfg: GoatnetCfg,
    pub latest: RwLock<Latest>,
}

pub struct App {
    /// Aggregator mode (§7.8)? Selects the page layout only — every slot is
    /// ingested, verified and rendered by the same code path either way.
    pub aggregate: bool,
    pub slots: Vec<Arc<Slot>>,
    pub siblings: Siblings,
    pub grammar: Grammar,
    pub rollup_stale_after_ms: i64,
    pub release_file: String,
}

pub async fn open_session(cfg: &GoatnetCfg) -> Result<zenoh::Session> {
    let mut config = zenoh::Config::default();
    config
        .insert_json5("mode", "\"client\"")
        .map_err(|e| anyhow::anyhow!("config mode: {e}"))?;
    config
        .insert_json5("connect/endpoints", &format!("[\"{}\"]", cfg.endpoint))
        .map_err(|e| anyhow::anyhow!("config connect/endpoints: {e}"))?;
    if cfg.endpoint.starts_with("tls/") {
        let cert = cfg.cert.as_ref().context("cert required for tls/ endpoint")?;
        let key = cfg.key.as_ref().context("key required for tls/ endpoint")?;
        let root_ca = cfg
            .root_ca
            .as_ref()
            .context("root_ca required for tls/ endpoint")?;
        let tls_block = format!(
            r#"{{ "root_ca_certificate": "{}", "connect_certificate": "{}", "connect_private_key": "{}", "enable_mtls": true, "verify_name_on_connect": false }}"#,
            root_ca.display(),
            cert.display(),
            key.display(),
        );
        config
            .insert_json5("transport/link/tls", &tls_block)
            .map_err(|e| anyhow::anyhow!("config transport/link/tls: {e}"))?;
    }
    zenoh::open(config)
        .await
        .map_err(|e| anyhow::anyhow!("zenoh::open failed: {e}"))
}

/// Subscribe both selectors and fold samples into the latest-map, forever —
/// for ONE goatnet's fabric. Runs AFTER the HTTP listener is bound (F-168:
/// never leave the server unbound behind a slow warm-up), and keeps retrying
/// a failed session so an unreachable router degrades to an honest "cannot
/// hear" page, not a crash loop that takes the refusal message with it. In
/// aggregator mode one of these runs per slot, so one goatnet's unopenable
/// session costs nothing but its own row (§7.8).
async fn bus_task(slot: Arc<Slot>) {
    loop {
        let session = match open_session(&slot.cfg).await {
            Ok(s) => {
                slot.latest.write().await.bus_note =
                    format!("session open to {} (zid {})", slot.cfg.endpoint, s.zid());
                s
            }
            Err(e) => {
                let note = format!(
                    "cannot open a Zenoh session to {}: {e} — retrying every 15s",
                    slot.cfg.endpoint
                );
                warn!(goatnet = slot.cfg.name.as_str(), "{note}");
                slot.latest.write().await.bus_note = note;
                tokio::time::sleep(Duration::from_secs(15)).await;
                continue;
            }
        };
        let health = match session.declare_subscriber(HEALTH_SELECTOR).await {
            Ok(s) => s,
            Err(e) => {
                warn!(error = %e, "declare subscriber on {HEALTH_SELECTOR}");
                let _ = session.close().await;
                tokio::time::sleep(Duration::from_secs(15)).await;
                continue;
            }
        };
        let reports = match session.declare_subscriber(REPORT_SELECTOR).await {
            Ok(s) => s,
            Err(e) => {
                warn!(error = %e, "declare subscriber on {REPORT_SELECTOR}");
                let _ = session.close().await;
                tokio::time::sleep(Duration::from_secs(15)).await;
                continue;
            }
        };
        info!(
            goatnet = slot.cfg.name.as_str(),
            subscribes = HEALTH_SELECTOR,
            and = REPORT_SELECTOR,
            "console listening — a passive reader; it publishes nothing"
        );
        loop {
            tokio::select! {
                s = health.recv_async() => match s {
                    Ok(sample) => {
                        let key = sample.key_expr().as_str().to_string();
                        let payload = sample.payload().to_bytes().to_vec();
                        ingest(&mut *slot.latest.write().await, &key, &payload, now_unix_ms());
                    }
                    Err(_) => break,
                },
                s = reports.recv_async() => match s {
                    Ok(sample) => {
                        let key = sample.key_expr().as_str().to_string();
                        let payload = sample.payload().to_bytes().to_vec();
                        ingest(&mut *slot.latest.write().await, &key, &payload, now_unix_ms());
                    }
                    Err(_) => break,
                },
            }
        }
        slot.latest.write().await.bus_note =
            "the Zenoh session closed — reconnecting".to_string();
        let _ = session.close().await;
        tokio::time::sleep(Duration::from_secs(5)).await;
    }
}

/// Drive every slot's bus task in ONE future, no free-threaded spawns (the
/// ADR 0709 D7 posture the single-mode select! already follows). Each inner
/// future is an endless retry loop that never resolves, so polling all of
/// them on every wake is O(slots) per event with slots ≤ a handful.
async fn bus_tasks(slots: Vec<Arc<Slot>>) {
    let mut futs: Vec<std::pin::Pin<Box<dyn std::future::Future<Output = ()> + Send>>> =
        slots.into_iter().map(|s| Box::pin(bus_task(s)) as _).collect();
    std::future::poll_fn(move |cx| {
        for f in futs.iter_mut() {
            let _ = f.as_mut().poll(cx);
        }
        std::task::Poll::<()>::Pending
    })
    .await
}

// ---------------------------------------------------------------------------
// HTTP
// ---------------------------------------------------------------------------

/// Minimal query parsing: `site=` is consumed, `domain=` is accepted (the
/// ADR 1036 hop lands here), and every unknown param is IGNORED SILENTLY per
/// the Block 94 contract — the URL stays shareable across surfaces.
pub fn site_param(raw: Option<&str>) -> Option<String> {
    for pair in raw.unwrap_or("").split('&') {
        let (k, v) = pair.split_once('=').unwrap_or((pair, ""));
        if k == "site" {
            let decoded = percent_decode(v);
            if !decoded.is_empty() {
                return Some(decoded);
            }
        }
    }
    None
}

fn percent_decode(s: &str) -> String {
    let bytes = s.as_bytes();
    let mut out = Vec::with_capacity(bytes.len());
    let mut i = 0;
    while i < bytes.len() {
        match bytes[i] {
            b'%' if i + 2 < bytes.len() => {
                match u8::from_str_radix(&s[i + 1..i + 3], 16) {
                    Ok(b) => {
                        out.push(b);
                        i += 3;
                    }
                    Err(_) => {
                        out.push(bytes[i]);
                        i += 1;
                    }
                }
            }
            b'+' => {
                out.push(b' ');
                i += 1;
            }
            b => {
                out.push(b);
                i += 1;
            }
        }
    }
    String::from_utf8_lossy(&out).into_owned()
}

async fn page(State(app): State<Arc<App>>, RawQuery(raw): RawQuery) -> impl IntoResponse {
    let now_ms = now_unix_ms();
    // Verify-at-read PER GOATNET, independently: one goatnet's unverifiable
    // intent refuses its own grid and touches nothing else (§7.8).
    let mut guards = Vec::with_capacity(app.slots.len());
    for slot in &app.slots {
        guards.push(slot.latest.read().await);
    }
    let intents: Vec<(Option<Value>, String)> = app
        .slots
        .iter()
        .map(|s| goat_consistency::verify::load_verified(&s.cfg.intent_file, &s.cfg.pubkey_dir))
        .collect();
    fn make_view<'a>(
        chosen_site: &'a str,
        latest: &'a Latest,
        intent: &'a (Option<Value>, String),
        app: &'a App,
        now_ms: i64,
    ) -> render::View<'a> {
        render::View {
            chosen_site,
            latest,
            intent,
            siblings: &app.siblings,
            grammar: &app.grammar,
            now_ms,
            stale_after_ms: app.rollup_stale_after_ms,
            version: env!("CARGO_PKG_VERSION"),
        }
    }
    if app.aggregate {
        // §7.4 band + one grid stack per goatnet, each rendered by the exact
        // same code path single mode uses. `site=` is not consumed here —
        // every configured goatnet renders, always; hiding one would be the
        // "never absent" rule broken by a query param.
        let views: Vec<render::NamedView> = app
            .slots
            .iter()
            .enumerate()
            .map(|(i, slot)| render::NamedView {
                name: &slot.cfg.name,
                view: make_view(&slot.cfg.site, &guards[i], &intents[i], &app, now_ms),
                console_url: slot.cfg.console_url.as_deref(),
            })
            .collect();
        Html(render::render_aggregate_page(&views, env!("CARGO_PKG_VERSION")))
    } else {
        let chosen = site_param(raw.as_deref()).unwrap_or_else(|| app.slots[0].cfg.site.clone());
        Html(render::render_page(&make_view(&chosen, &guards[0], &intents[0], &app, now_ms)))
    }
}

async fn meta(State(app): State<Arc<App>>) -> impl IntoResponse {
    // The /api/meta convention (Block 94 obligation 2; microscope is the
    // working precedent for the shape).
    Json(serde_json::json!({
        "service": "consistency-console",
        "version": env!("CARGO_PKG_VERSION"),
        "adr": "1110",
        "siblings": app.siblings,
    }))
}

async fn slot_state(slot: &Slot) -> Value {
    let latest = slot.latest.read().await;
    let (intent, note) =
        goat_consistency::verify::load_verified(&slot.cfg.intent_file, &slot.cfg.pubkey_dir);
    let rollups: BTreeMap<&String, Value> = latest
        .rollups
        .iter()
        .map(|(k, v)| {
            (
                k,
                serde_json::json!({
                    "received_unix_ms": v.received_unix_ms,
                    "envelope": v.env,
                }),
            )
        })
        .collect();
    serde_json::json!({
        "goatnet": slot.cfg.name,
        "site": slot.cfg.site,
        "endpoint": slot.cfg.endpoint,
        "bus": latest.bus_note,
        "intent": {
            "verified": intent.is_some(),
            "note": note,
            "serial": intent.as_ref().and_then(|d| d.get("serial")).cloned().unwrap_or(Value::Null),
            "content_hash": intent.as_ref().and_then(|d| d.get("content_hash")).cloned().unwrap_or(Value::Null),
            "goatnet": intent.as_ref().and_then(|d| d.get("goatnet")).cloned().unwrap_or(Value::Null),
        },
        "rollups": rollups,
        "reports_heard": latest.reports.len(),
    })
}

async fn api_state(State(app): State<Arc<App>>) -> impl IntoResponse {
    if app.aggregate {
        let mut goatnets = Vec::with_capacity(app.slots.len());
        for slot in &app.slots {
            goatnets.push(slot_state(slot).await);
        }
        Json(serde_json::json!({ "mode": "aggregate", "goatnets": goatnets })).into_response()
    } else {
        // Single mode keeps the CF-6 shape verbatim (plus "mode") — the
        // deployed console's consumers see no change.
        let mut state = slot_state(&app.slots[0]).await;
        if let Some(o) = state.as_object_mut() {
            o.insert("mode".into(), Value::String("single".into()));
        }
        Json(state).into_response()
    }
}

async fn api_node(
    State(app): State<Arc<App>>,
    AxumPath(host): AxumPath<String>,
) -> impl IntoResponse {
    for slot in &app.slots {
        let latest = slot.latest.read().await;
        if let Some(stored) = latest.reports.get(&host) {
            return Json(serde_json::json!({
                "goatnet": slot.cfg.name,
                "host_id": host,
                "received_unix_ms": stored.received_unix_ms,
                "report": stored.report,
            }))
            .into_response();
        }
    }
    (
        axum::http::StatusCode::NOT_FOUND,
        Json(serde_json::json!({
            "error": format!(
                "nothing heard from `{host}` on {REPORT_SELECTOR} — the node has not \
                 published within this console's lifetime, or its publisher is not deployed"
            ),
        })),
    )
        .into_response()
}

async fn healthz() -> &'static str {
    "ok"
}

// ---------------------------------------------------------------------------
// /release — the ADR 1120 ladder view. Read from disk per request (no cache,
// the §7.3 rule) and rendered without a verdict of the console's own: every
// grade, blocker and soak clock in the document was computed by
// `graduate.py readiness`, which is the same code `graduate` refuses on.
// ---------------------------------------------------------------------------

async fn release_page(State(app): State<Arc<App>>) -> impl IntoResponse {
    Html(release::render_page(
        &release::load(&app.release_file),
        now_unix_ms(),
        env!("CARGO_PKG_VERSION"),
    ))
}

async fn api_release(State(app): State<Arc<App>>) -> impl IntoResponse {
    match release::load(&app.release_file) {
        Ok(doc) => Json(serde_json::json!({
            "schema": release::SCHEMA,
            "read_unix_ms": now_unix_ms(),
            "path": app.release_file,
            "document": doc,
        }))
        .into_response(),
        Err(e) => (
            axum::http::StatusCode::NOT_FOUND,
            Json(serde_json::json!({ "error": e, "path": app.release_file })),
        )
            .into_response(),
    }
}

// ---------------------------------------------------------------------------
// /api/aggregate — the aggregate state as structured, self-dating JSON
// (§7.8 as-built). Same inputs as the page, same library computations, one
// more projection. Available in both modes: single mode serves one goatnet
// and an honestly-not-comparable cross_env section.
// ---------------------------------------------------------------------------

async fn with_api_envs<T>(
    app: &App,
    f: impl FnOnce(&[api::ApiEnv]) -> T,
) -> T {
    let now_ms = now_unix_ms();
    let mut guards = Vec::with_capacity(app.slots.len());
    for slot in &app.slots {
        guards.push(slot.latest.read().await);
    }
    // Verify-at-read per goatnet, independently — the same rule as the page.
    let intents: Vec<(Option<Value>, String)> = app
        .slots
        .iter()
        .map(|s| goat_consistency::verify::load_verified(&s.cfg.intent_file, &s.cfg.pubkey_dir))
        .collect();
    let envs: Vec<api::ApiEnv> = app
        .slots
        .iter()
        .enumerate()
        .map(|(i, slot)| api::ApiEnv {
            name: &slot.cfg.name,
            endpoint: &slot.cfg.endpoint,
            view: render::View {
                chosen_site: &slot.cfg.site,
                latest: &guards[i],
                intent: &intents[i],
                siblings: &app.siblings,
                grammar: &app.grammar,
                now_ms,
                stale_after_ms: app.rollup_stale_after_ms,
                version: env!("CARGO_PKG_VERSION"),
            },
        })
        .collect();
    f(&envs)
}

async fn api_aggregate(State(app): State<Arc<App>>) -> impl IntoResponse {
    let mode = if app.aggregate { "aggregate" } else { "single" };
    let doc = with_api_envs(&app, |envs| {
        api::aggregate_doc(envs, mode, env!("CARGO_PKG_VERSION"), now_unix_ms())
    })
    .await;
    Json(doc)
}

async fn api_aggregate_one(
    State(app): State<Arc<App>>,
    AxumPath(name): AxumPath<String>,
) -> impl IntoResponse {
    let known: Vec<String> = app.slots.iter().map(|s| s.cfg.name.clone()).collect();
    if !known.contains(&name) {
        return (
            axum::http::StatusCode::NOT_FOUND,
            Json(serde_json::json!({
                "error": format!(
                    "no goatnet named `{name}` is configured — configured: {}",
                    known.join(", ")
                ),
            })),
        )
            .into_response();
    }
    let doc = with_api_envs(&app, |envs| {
        envs.iter()
            .find(|e| e.name == name)
            .map(api::goatnet_doc)
            .expect("name was checked against the slot set")
    })
    .await;
    Json(serde_json::json!({
        "schema": api::SCHEMA,
        "generated_unix_ms": now_unix_ms(),
        "goatnet": doc,
    }))
    .into_response()
}

pub fn router(app: Arc<App>) -> axum::Router {
    axum::Router::new()
        .route("/", get(page))
        .route("/api/meta", get(meta))
        .route("/api/state", get(api_state))
        .route("/api/aggregate", get(api_aggregate))
        .route("/api/aggregate/:name", get(api_aggregate_one))
        .route("/api/node/:host", get(api_node))
        .route("/release", get(release_page))
        .route("/api/release", get(api_release))
        .route("/healthz", get(healthz))
        .with_state(app)
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
    let (aggregate, cfgs): (bool, Vec<GoatnetCfg>) = match (&args.aggregate, &args.endpoint, &args.site) {
        (Some(config), None, None) => (true, aggregate::load(std::path::Path::new(config))?),
        (Some(_), _, _) => anyhow::bail!(
            "--aggregate is a whole deployment mode: it carries every goatnet's endpoint and \
             site in the config file, so --endpoint/--site (GCC_ENDPOINT/GCC_SITE) must not \
             also be set — remove them rather than let one goatnet be configured twice"
        ),
        (None, Some(endpoint), Some(site)) => {
            anyhow::ensure!(!site.is_empty(), "--site required — it names the page's subject scope");
            (
                false,
                vec![GoatnetCfg {
                    name: site.clone(),
                    site: site.clone(),
                    endpoint: endpoint.clone(),
                    intent_file: PathBuf::from(&args.intent_file),
                    pubkey_dir: PathBuf::from(&args.pubkey_dir),
                    cert: args.cert.as_ref().map(PathBuf::from),
                    key: args.key.as_ref().map(PathBuf::from),
                    root_ca: args.root_ca.as_ref().map(PathBuf::from),
                    // Single mode IS the site console; it links OUT to the
                    // comparison page via the sibling env var, not back at
                    // itself.
                    console_url: None,
                }],
            )
        }
        _ => anyhow::bail!(
            "either --aggregate <config.yaml> (the CF-7 aggregator) or both --endpoint and \
             --site (the single-goatnet console) — see --help"
        ),
    };
    let grammar = Grammar::load()
        .map_err(|e| anyhow::anyhow!("the committed grammar is unusable: {e}"))?;
    let slots: Vec<Arc<Slot>> = cfgs
        .into_iter()
        .map(|cfg| Arc::new(Slot { cfg, latest: RwLock::new(Latest::default()) }))
        .collect();
    let app = Arc::new(App {
        aggregate,
        slots,
        siblings: Siblings::from_env(),
        grammar,
        rollup_stale_after_ms: args.rollup_stale_after_ms,
        release_file: args.release_file.clone(),
    });

    // Bind FIRST, then run the HTTP server and the bus listeners
    // concurrently. select! + one poll_fn-joined future rather than
    // tokio::spawn — the workspace lint (ADR 0709 D7 layer 3) forbids
    // free-threaded spawns, and two futures in one select is all this needs.
    let listener = tokio::net::TcpListener::bind(&args.listen)
        .await
        .with_context(|| format!("bind {}", args.listen))?;
    info!(
        listen = args.listen.as_str(),
        mode = if app.aggregate { "aggregate" } else { "single" },
        goatnets = app.slots.len(),
        "console bound"
    );

    let serve = axum::serve(listener, router(app.clone()));
    let slots = app.slots.clone();
    tokio::select! {
        r = serve => r.context("serve"),
        _ = bus_tasks(slots) => unreachable!("bus tasks retry forever"),
    }
}

#[cfg(test)]
mod tests;
