//! The daemon's observability surface — a Prometheus `/metrics` endpoint and a
//! `/healthz` that can actually fail.
//!
//! Until this module existed grant-keyd spoke only to stderr: a keyd that had
//! stopped rotating, or that was running its UNVALIDATED break-glass key source
//! in production, was undetectable from outside the process (identity
//! fragility audit §1.3). What it needed was not a metrics stack but the four
//! facts an operator cannot otherwise obtain: is the loop still turning, when did
//! a generation last go out, which key source is admitting members, and how often
//! is a sweep failing.
//!
//! **Dependency-free by requirement.** This crate's tree is FIPS-sensitive — the
//! aws-lc / rustls pins are coupled fleet-wide and are not to be perturbed for an
//! observability feature — so the server is a `std::net::TcpListener` and a
//! hand-written response, not an HTTP framework. It also keeps the module free of
//! the `publisher` feature, so a `default-features = false` consumer still gets it.
//!
//! **Two clocks, deliberately distinct — alerting on the wrong one cries wolf:**
//!
//! - [`Metrics::record_sweep`] advances `grant_keyd_last_sweep_timestamp_seconds`.
//!   That is the LIVENESS signal: on a healthy daemon it advances every
//!   `poll_interval_secs`. Stale ⇒ the loop is wedged (a hung resolve or publish)
//!   or the process is gone.
//! - `grant_keyd_last_rotation_timestamp_seconds` advances only when a *new
//!   generation was published*. The daemon rotates only when an audience CHANGES,
//!   so on a correct, quiet fabric this is legitimately days old. It is a change
//!   record, not a heartbeat.
//!
//! The endpoint serves plaintext HTTP on a container-internal port. It exposes no
//! key material and no member identities — release-set names only.

use std::collections::BTreeMap;
use std::io::{Read, Write};
use std::net::{SocketAddr, TcpListener, TcpStream};
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::{Arc, Mutex};
use std::time::{Duration, SystemTime, UNIX_EPOCH};

use crate::daemon::{FailureStage, SweepOutcome};

/// Which recipient-key source(s) are admitting members — the fact that
/// distinguishes a production posture from a break-glass one.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum KeySourceMode {
    /// No validated source configured: keys taken from `cert_dir` unvalidated.
    /// Break-glass/offline only; `grant_keyd_key_source_validated` reads 0.
    UnvalidatedCertDir,
    /// Portal-minted members validated against the offline-CA root.
    PortalCa,
    /// SPIRE workload SVIDs validated against the trust root.
    Svid,
    /// Both validated sources chained.
    PortalCaAndSvid,
}

impl KeySourceMode {
    /// Derive the mode from which validated source pairs the config carries.
    /// `Config::validate` already enforces both-or-neither on each pair.
    pub fn from_sources(portal_ca: bool, svid: bool) -> Self {
        match (portal_ca, svid) {
            (true, true) => KeySourceMode::PortalCaAndSvid,
            (true, false) => KeySourceMode::PortalCa,
            (false, true) => KeySourceMode::Svid,
            (false, false) => KeySourceMode::UnvalidatedCertDir,
        }
    }

    pub fn as_str(&self) -> &'static str {
        match self {
            KeySourceMode::UnvalidatedCertDir => "unvalidated_cert_dir",
            KeySourceMode::PortalCa => "portal_ca",
            KeySourceMode::Svid => "svid",
            KeySourceMode::PortalCaAndSvid => "portal_ca+svid",
        }
    }

    /// False only for the break-glass source — the alertable condition.
    pub fn validated(&self) -> bool {
        !matches!(self, KeySourceMode::UnvalidatedCertDir)
    }
}

/// Where each release-set's audience comes from.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum AudienceSource {
    /// The portal's grant rows (a data owner's `goat access grant` drives it).
    PortalGrantRows,
    /// The operator-declared `release_sets` member lists.
    OperatorDeclared,
}

impl AudienceSource {
    pub fn as_str(&self) -> &'static str {
        match self {
            AudienceSource::PortalGrantRows => "portal_grant_rows",
            AudienceSource::OperatorDeclared => "operator_declared",
        }
    }
}

/// Per-release-set state, rendered as labelled series.
#[derive(Debug, Default, Clone)]
struct SetState {
    generation: u64,
    members: u64,
    last_rotation: u64,
    failures: u64,
    /// Unix second at which a still-unpublished audience change was first seen;
    /// 0 = in sync. Rendered as an age, so a stuck rotation grows a number an
    /// alert can bound instead of ticking a counter once and going quiet.
    unpublished_since: u64,
    /// Wall time of the last successful rotate, milliseconds.
    last_rotation_ms: u64,
}

/// The daemon's counters and gauges. Shared between the poll loop and the HTTP
/// thread; all recording is lock-free except the per-release-set map.
pub struct Metrics {
    signer_id: String,
    key_source: KeySourceMode,
    audience_source: AudienceSource,
    poll_interval_secs: u64,
    /// A sweep older than this makes `/healthz` fail. Derived from the poll
    /// interval so a slow-polling daemon is not called dead.
    stale_after_secs: u64,
    start_time: u64,
    last_sweep: AtomicU64,
    last_rotation: AtomicU64,
    sweeps_total: AtomicU64,
    rotations_total: AtomicU64,
    resolve_errors: AtomicU64,
    rotate_errors: AtomicU64,
    enumerate_errors: AtomicU64,
    sets_swept: AtomicU64,
    sets: Mutex<BTreeMap<String, SetState>>,
}

fn now_secs() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_secs())
        .unwrap_or(0)
}

impl Metrics {
    /// Build the surface. `poll_interval_secs` sets the health staleness bound at
    /// three intervals (floored at 90s so a fast poll does not page on one slow
    /// sweep).
    pub fn new(
        signer_id: impl Into<String>,
        key_source: KeySourceMode,
        audience_source: AudienceSource,
        poll_interval_secs: u64,
    ) -> Arc<Self> {
        Arc::new(Metrics {
            signer_id: signer_id.into(),
            key_source,
            audience_source,
            poll_interval_secs,
            stale_after_secs: poll_interval_secs.saturating_mul(3).max(90),
            start_time: now_secs(),
            last_sweep: AtomicU64::new(0),
            last_rotation: AtomicU64::new(0),
            sweeps_total: AtomicU64::new(0),
            rotations_total: AtomicU64::new(0),
            resolve_errors: AtomicU64::new(0),
            rotate_errors: AtomicU64::new(0),
            enumerate_errors: AtomicU64::new(0),
            sets_swept: AtomicU64::new(0),
            sets: Mutex::new(BTreeMap::new()),
        })
    }

    /// Record one completed sweep of `swept` release-sets. Call this even when
    /// every outcome is `Unchanged` — that is precisely the heartbeat.
    pub fn record_sweep(&self, swept: usize, outcomes: &[SweepOutcome]) {
        let now = now_secs();
        self.last_sweep.store(now, Ordering::Relaxed);
        self.sweeps_total.fetch_add(1, Ordering::Relaxed);
        self.sets_swept.store(swept as u64, Ordering::Relaxed);
        let mut sets = self.sets.lock().unwrap_or_else(|e| e.into_inner());
        for outcome in outcomes {
            match outcome {
                SweepOutcome::Rotated {
                    release_set,
                    generation,
                    members,
                } => {
                    self.rotations_total.fetch_add(1, Ordering::Relaxed);
                    self.last_rotation.store(now, Ordering::Relaxed);
                    let s = sets.entry(release_set.clone()).or_default();
                    s.generation = *generation;
                    s.members = *members as u64;
                    s.last_rotation = now;
                }
                SweepOutcome::Unchanged { release_set } => {
                    sets.entry(release_set.clone()).or_default();
                }
                SweepOutcome::Failed {
                    release_set, stage, ..
                } => {
                    match stage {
                        FailureStage::Resolve => &self.resolve_errors,
                        FailureStage::Rotate => &self.rotate_errors,
                    }
                    .fetch_add(1, Ordering::Relaxed);
                    sets.entry(release_set.clone()).or_default().failures += 1;
                }
            }
        }
    }

    /// The sweep list could not be enumerated from the portal (the daemon fell
    /// back to the pinned keys) — a degraded sweep, not a dead one.
    pub fn record_enumeration_failure(&self) {
        self.enumerate_errors.fetch_add(1, Ordering::Relaxed);
    }

    /// Take the daemon's post-sweep drift and rotation-cost bookkeeping.
    ///
    /// `unpublished` maps a release-set to the unix second at which a change was
    /// first seen and not yet published; a set absent from it is in sync and its
    /// drift is cleared here rather than left at its last value — a stale drift
    /// reading on a set that has since rotated would be worse than none.
    pub fn record_drift(
        &self,
        unpublished: &BTreeMap<String, u64>,
        rotation_ms: &BTreeMap<String, u64>,
    ) {
        let mut sets = self.sets.lock().unwrap_or_else(|e| e.into_inner());
        for (rs, state) in sets.iter_mut() {
            state.unpublished_since = unpublished.get(rs).copied().unwrap_or(0);
            if let Some(ms) = rotation_ms.get(rs) {
                state.last_rotation_ms = *ms;
            }
        }
        // A set can be drifting before it has any other recorded state (its very
        // first rotate failed), so it must still get a series.
        for (rs, since) in unpublished {
            let e = sets.entry(rs.clone()).or_default();
            e.unpublished_since = *since;
        }
    }

    /// True while the loop is turning. False once no sweep has completed for
    /// three poll intervals — counting from process start, so a daemon that
    /// never reached its first sweep (a hung Zenoh connect) also reads unhealthy.
    pub fn healthy(&self) -> bool {
        let last = self.last_sweep.load(Ordering::Relaxed).max(self.start_time);
        now_secs().saturating_sub(last) <= self.stale_after_secs
    }

    /// One line of human-readable state, served by `/healthz` and logged at start.
    pub fn health_line(&self) -> String {
        let last_sweep = self.last_sweep.load(Ordering::Relaxed);
        let age = if last_sweep == 0 {
            "never".to_string()
        } else {
            format!("{}s ago", now_secs().saturating_sub(last_sweep))
        };
        format!(
            "{} signer={} key_source={} audience={} last_sweep={} sweeps={} rotations={}",
            if self.healthy() { "ok" } else { "stale" },
            self.signer_id,
            self.key_source.as_str(),
            self.audience_source.as_str(),
            age,
            self.sweeps_total.load(Ordering::Relaxed),
            self.rotations_total.load(Ordering::Relaxed),
        )
    }

    /// Render the Prometheus text exposition.
    pub fn render(&self) -> String {
        let mut out = String::with_capacity(2048);
        let signer = escape(&self.signer_id);

        push(
            &mut out,
            "grant_keyd_up",
            "gauge",
            "1 whenever the daemon answers a scrape. Alert on absent(), not on the value.",
            format_args!("grant_keyd_up 1\n"),
        );

        push(&mut out, "grant_keyd_build_info", "gauge",
            "Build and posture labels; the value is always 1.",
            format_args!(
                "grant_keyd_build_info{{version=\"{}\",signer_id=\"{}\",key_source=\"{}\",audience_source=\"{}\"}} 1\n",
                escape(env!("CARGO_PKG_VERSION")),
                signer,
                self.key_source.as_str(),
                self.audience_source.as_str(),
            ));

        push(
            &mut out,
            "grant_keyd_start_time_seconds",
            "gauge",
            "Unix time the daemon started.",
            format_args!("grant_keyd_start_time_seconds {}\n", self.start_time),
        );

        push(&mut out, "grant_keyd_poll_interval_seconds", "gauge",
            "Configured seconds between sweeps — the expected period of grant_keyd_last_sweep_timestamp_seconds.",
            format_args!("grant_keyd_poll_interval_seconds {}\n", self.poll_interval_secs));

        push(&mut out, "grant_keyd_key_source_validated", "gauge",
            "1 when members resolve through a validated key source (portal-CA and/or SVID); 0 = the UNVALIDATED break-glass cert-dir source is in use.",
            format_args!(
                "grant_keyd_key_source_validated {}\n",
                u8::from(self.key_source.validated())
            ));

        push(&mut out, "grant_keyd_last_sweep_timestamp_seconds", "gauge",
            "Unix time of the last COMPLETED sweep. This is the liveness clock: it advances every poll interval on a healthy daemon. 0 = no sweep yet.",
            format_args!(
                "grant_keyd_last_sweep_timestamp_seconds {}\n",
                self.last_sweep.load(Ordering::Relaxed)
            ));

        push(&mut out, "grant_keyd_last_rotation_timestamp_seconds", "gauge",
            "Unix time a new generation was last published, any release-set. A quiet fabric rotates rarely — this is a change record, NOT a heartbeat. 0 = none since start.",
            format_args!(
                "grant_keyd_last_rotation_timestamp_seconds {}\n",
                self.last_rotation.load(Ordering::Relaxed)
            ));

        push(
            &mut out,
            "grant_keyd_sweeps_total",
            "counter",
            "Completed sweeps since start.",
            format_args!(
                "grant_keyd_sweeps_total {}\n",
                self.sweeps_total.load(Ordering::Relaxed)
            ),
        );

        push(
            &mut out,
            "grant_keyd_rotations_total",
            "counter",
            "Generations published since start.",
            format_args!(
                "grant_keyd_rotations_total {}\n",
                self.rotations_total.load(Ordering::Relaxed)
            ),
        );

        push(&mut out, "grant_keyd_errors_total", "counter",
            "Sweep failures by stage: resolve = the audience could not be built (fail-closed, no rotation); rotate = mint/wrap/sign/publish failed; enumerate = the portal sweep list was unreachable and the pinned keys were swept instead.",
            format_args!(
                "grant_keyd_errors_total{{stage=\"resolve\"}} {}\ngrant_keyd_errors_total{{stage=\"rotate\"}} {}\ngrant_keyd_errors_total{{stage=\"enumerate\"}} {}\n",
                self.resolve_errors.load(Ordering::Relaxed),
                self.rotate_errors.load(Ordering::Relaxed),
                self.enumerate_errors.load(Ordering::Relaxed),
            ));

        push(&mut out, "grant_keyd_release_sets_swept", "gauge",
            "Release-sets in the last sweep list. A drop in grant-row mode means the portal enumeration failed and only pinned keys were swept.",
            format_args!("grant_keyd_release_sets_swept {}\n", self.sets_swept.load(Ordering::Relaxed)));

        let sets = self.sets.lock().unwrap_or_else(|e| e.into_inner()).clone();
        let mut generations = String::new();
        let mut members = String::new();
        let mut rotations = String::new();
        let mut failures = String::new();
        let mut drift = String::new();
        let mut rot_ms = String::new();
        let now = now_secs();
        for (rs, s) in &sets {
            let l = escape(rs);
            generations.push_str(&format!(
                "grant_keyd_release_set_generation{{release_set=\"{l}\"}} {}\n",
                s.generation
            ));
            members.push_str(&format!(
                "grant_keyd_release_set_members{{release_set=\"{l}\"}} {}\n",
                s.members
            ));
            rotations.push_str(&format!(
                "grant_keyd_release_set_last_rotation_timestamp_seconds{{release_set=\"{l}\"}} {}\n",
                s.last_rotation
            ));
            failures.push_str(&format!(
                "grant_keyd_release_set_failures_total{{release_set=\"{l}\"}} {}\n",
                s.failures
            ));
            drift.push_str(&format!(
                "grant_keyd_release_set_audience_drift_seconds{{release_set=\"{l}\"}} {}\n",
                if s.unpublished_since == 0 {
                    0
                } else {
                    now.saturating_sub(s.unpublished_since)
                }
            ));
            rot_ms.push_str(&format!(
                "grant_keyd_release_set_rotation_duration_seconds{{release_set=\"{l}\"}} {:.3}\n",
                s.last_rotation_ms as f64 / 1000.0
            ));
        }
        push(&mut out, "grant_keyd_release_set_generation", "gauge",
            "Latest generation published for this release-set this process has seen. 0 = swept but not yet rotated by THIS process (a restart forgets prior generations; the fabric keeps them).",
            format_args!("{generations}"));
        push(
            &mut out,
            "grant_keyd_release_set_members",
            "gauge",
            "Members in the audience at that generation.",
            format_args!("{members}"),
        );
        push(
            &mut out,
            "grant_keyd_release_set_last_rotation_timestamp_seconds",
            "gauge",
            "Unix time this release-set last rotated. 0 = not since start.",
            format_args!("{rotations}"),
        );
        push(
            &mut out,
            "grant_keyd_release_set_failures_total",
            "counter",
            "Sweep failures for this release-set.",
            format_args!("{failures}"),
        );
        push(&mut out, "grant_keyd_release_set_audience_drift_seconds", "gauge",
            "Seconds this release-set's PUBLISHED generation has not matched its RESOLVED audience. 0 = in sync. Non-zero means a membership change — including a revoke — has been read and not yet published, so the live generation still wraps to whoever the grant rows dropped. This is the only series that describes a rotation that was DUE and did not happen; the rotation clock and counter only describe ones that did.",
            format_args!("{drift}"));
        push(&mut out, "grant_keyd_release_set_rotation_duration_seconds", "gauge",
            "Wall time of the last successful rotate. Flat DEK wrapping is O(audience size), so this is where the flat-wrap ceiling shows up on a real audience.",
            format_args!("{rot_ms}"));
        out
    }
}

/// Emit one metric family: HELP, TYPE, then its samples. A family with no samples
/// (no release-set seen yet) still gets its HELP/TYPE so the name is discoverable.
fn push(out: &mut String, name: &str, kind: &str, help: &str, samples: std::fmt::Arguments) {
    out.push_str(&format!("# HELP {name} {help}\n# TYPE {name} {kind}\n"));
    out.push_str(&samples.to_string());
}

/// Prometheus label-value escaping (backslash, quote, newline).
fn escape(v: &str) -> String {
    let mut s = String::with_capacity(v.len());
    for c in v.chars() {
        match c {
            '\\' => s.push_str("\\\\"),
            '"' => s.push_str("\\\""),
            '\n' => s.push_str("\\n"),
            _ => s.push(c),
        }
    }
    s
}

/// Bind the metrics/health listener and serve it from a background thread.
/// Returns the bound address (pass `:0` in tests to get an ephemeral port).
///
/// Fail-closed at startup: a bind failure is returned to the caller rather than
/// swallowed, because a grant-keyd nobody can observe is the defect this module
/// exists to close.
pub fn serve(addr: &str, metrics: Arc<Metrics>) -> std::io::Result<SocketAddr> {
    let listener = TcpListener::bind(addr)?;
    let local = listener.local_addr()?;
    std::thread::Builder::new()
        .name("keyd-metrics".into())
        .spawn(move || {
            for stream in listener.incoming() {
                let Ok(stream) = stream else { continue };
                let m = Arc::clone(&metrics);
                // A thread per connection: scrape volume is one request every
                // scrape interval, and it keeps one slow client from stalling
                // the listener.
                let _ = std::thread::Builder::new()
                    .name("keyd-metrics-conn".into())
                    .spawn(move || {
                        let _ = handle(stream, &m);
                    });
            }
        })?;
    Ok(local)
}

/// GET `path` from a listener and return its `(status, body)`.
///
/// The client half of the same dependency-free contract: the runtime image is
/// Chainguard distroless (no shell, no curl), so the daemon binary has to be its
/// own container healthcheck probe. Reads to EOF — [`respond`] always sends
/// `Connection: close`.
pub fn probe(addr: SocketAddr, path: &str) -> std::io::Result<(u16, String)> {
    let timeout = Duration::from_secs(5);
    let mut stream = TcpStream::connect_timeout(&addr, timeout)?;
    stream.set_read_timeout(Some(timeout))?;
    stream.set_write_timeout(Some(timeout))?;
    // One write_all, not a `write!` — the format macro emits a syscall per
    // fragment, which on loopback puts the request line and the headers in
    // separate segments.
    let request = format!("GET {path} HTTP/1.1\r\nHost: {addr}\r\nConnection: close\r\n\r\n");
    stream.write_all(request.as_bytes())?;
    stream.flush()?;
    let mut raw = String::new();
    stream.read_to_string(&mut raw)?;
    let status = raw
        .split_whitespace()
        .nth(1)
        .and_then(|c| c.parse::<u16>().ok())
        .ok_or_else(|| {
            std::io::Error::new(
                std::io::ErrorKind::InvalidData,
                "no HTTP status line in the response",
            )
        })?;
    let body = raw.split("\r\n\r\n").nth(1).unwrap_or_default().to_string();
    Ok((status, body))
}

/// Where a probe should connect for a listener configured to bind `listen`.
///
/// A wildcard bind (`0.0.0.0` / `[::]`, the container default) is not a
/// connectable address on every stack, so it maps to the matching loopback — the
/// probe always runs inside the same netns as the daemon.
pub fn probe_target(listen: &str) -> Result<SocketAddr, String> {
    let listen = listen.trim();
    if listen.is_empty() {
        return Err(
            "metrics_listen is empty — the listener is disabled, so there is \
                    nothing to probe. Set metrics_listen (default 0.0.0.0:9464) or turn \
                    the healthcheck off."
                .into(),
        );
    }
    let mut addr: SocketAddr = listen
        .parse()
        .map_err(|e| format!("metrics_listen {listen:?} is not an ip:port: {e}"))?;
    if addr.ip().is_unspecified() {
        addr.set_ip(match addr {
            SocketAddr::V4(_) => std::net::Ipv4Addr::LOCALHOST.into(),
            SocketAddr::V6(_) => std::net::Ipv6Addr::LOCALHOST.into(),
        });
    }
    Ok(addr)
}

/// Serve one request. Only the request line is parsed — there is nothing here a
/// header could change.
fn handle(mut stream: TcpStream, metrics: &Metrics) -> std::io::Result<()> {
    let timeout = Some(Duration::from_secs(5));
    stream.set_read_timeout(timeout)?;
    stream.set_write_timeout(timeout)?;

    // Read until the end of the request head. A single `read()` is not enough:
    // TCP may deliver the request line and the headers in separate segments, and
    // parsing a truncated first line yields a bogus 404 while the unread
    // remainder turns the close into an RST at the client. Bounded by the buffer
    // so an endless header stream cannot grow memory.
    let mut buf = [0u8; 2048];
    let mut n = 0;
    while n < buf.len() {
        let read = stream.read(&mut buf[n..])?;
        if read == 0 {
            break; // client closed
        }
        let grown = n + read;
        // The blank line can only straddle the last three bytes read.
        if buf[n.saturating_sub(3)..grown]
            .windows(4)
            .any(|w| w == b"\r\n\r\n")
        {
            n = grown;
            break;
        }
        n = grown;
    }
    let head = String::from_utf8_lossy(&buf[..n]);
    let request_line = head.lines().next().unwrap_or_default();
    let mut parts = request_line.split_whitespace();
    let method = parts.next().unwrap_or_default();
    let path = parts.next().unwrap_or_default();
    // Strip any query string; no endpoint here takes parameters.
    let path = path.split('?').next().unwrap_or_default();

    if method != "GET" && method != "HEAD" {
        return respond(&mut stream, 405, "text/plain", "method not allowed\n");
    }
    match path {
        "/metrics" => respond(
            &mut stream,
            200,
            "text/plain; version=0.0.4",
            &metrics.render(),
        ),
        "/healthz" => {
            let code = if metrics.healthy() { 200 } else { 503 };
            respond(
                &mut stream,
                code,
                "text/plain",
                &format!("{}\n", metrics.health_line()),
            )
        }
        "/" => respond(
            &mut stream,
            200,
            "text/plain",
            "grant-keyd\n/metrics\n/healthz\n",
        ),
        _ => respond(&mut stream, 404, "text/plain", "not found\n"),
    }
}

fn respond(
    stream: &mut TcpStream,
    status: u16,
    content_type: &str,
    body: &str,
) -> std::io::Result<()> {
    let reason = match status {
        200 => "OK",
        404 => "Not Found",
        405 => "Method Not Allowed",
        503 => "Service Unavailable",
        _ => "Unknown",
    };
    write!(
        stream,
        "HTTP/1.1 {status} {reason}\r\nContent-Type: {content_type}\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{body}",
        body.len()
    )?;
    stream.flush()
}

#[cfg(test)]
mod tests {
    use super::*;

    fn metrics() -> Arc<Metrics> {
        Metrics::new(
            "zagros-key-service",
            KeySourceMode::PortalCaAndSvid,
            AudienceSource::PortalGrantRows,
            30,
        )
    }

    #[test]
    fn a_sweep_advances_liveness_but_only_a_rotation_advances_the_change_clock() {
        let m = metrics();
        assert!(m
            .render()
            .contains("grant_keyd_last_sweep_timestamp_seconds 0"));

        // Unchanged sweep: liveness moves, rotation clock does not.
        m.record_sweep(
            1,
            &[SweepOutcome::Unchanged {
                release_set: "rs".into(),
            }],
        );
        let text = m.render();
        assert!(!text.contains("grant_keyd_last_sweep_timestamp_seconds 0"));
        assert!(text.contains("grant_keyd_last_rotation_timestamp_seconds 0"));
        assert!(text.contains("grant_keyd_sweeps_total 1"));
        assert!(text.contains("grant_keyd_rotations_total 0"));

        m.record_sweep(
            1,
            &[SweepOutcome::Rotated {
                release_set: "rs".into(),
                generation: 4,
                members: 3,
            }],
        );
        let text = m.render();
        assert!(!text.contains("grant_keyd_last_rotation_timestamp_seconds 0"));
        assert!(text.contains("grant_keyd_release_set_generation{release_set=\"rs\"} 4"));
        assert!(text.contains("grant_keyd_release_set_members{release_set=\"rs\"} 3"));
    }

    #[test]
    fn failures_are_counted_by_stage() {
        let m = metrics();
        m.record_sweep(
            2,
            &[
                SweepOutcome::Failed {
                    release_set: "a".into(),
                    stage: FailureStage::Resolve,
                    error: "resolve: boom".into(),
                },
                SweepOutcome::Failed {
                    release_set: "a".into(),
                    stage: FailureStage::Rotate,
                    error: "rotate: boom".into(),
                },
            ],
        );
        m.record_enumeration_failure();
        let text = m.render();
        assert!(text.contains("grant_keyd_errors_total{stage=\"resolve\"} 1"));
        assert!(text.contains("grant_keyd_errors_total{stage=\"rotate\"} 1"));
        assert!(text.contains("grant_keyd_errors_total{stage=\"enumerate\"} 1"));
        assert!(text.contains("grant_keyd_release_set_failures_total{release_set=\"a\"} 2"));
        assert!(text.contains("grant_keyd_release_sets_swept 2"));
    }

    #[test]
    fn drift_renders_as_an_age_and_clears_when_the_set_lands() {
        let m = Metrics::new(
            "ks",
            KeySourceMode::Svid,
            AudienceSource::PortalGrantRows,
            30,
        );
        m.record_sweep(
            1,
            &[SweepOutcome::Failed {
                release_set: "rs".into(),
                stage: FailureStage::Rotate,
                error: "router refused".into(),
            }],
        );
        // Stamped 120s ago: the gauge must read an AGE, not the stamp.
        let stamped: BTreeMap<String, u64> =
            [("rs".to_string(), now_secs() - 120)].into_iter().collect();
        m.record_drift(&stamped, &BTreeMap::new());
        let text = m.render();
        let line = text
            .lines()
            .find(|l| {
                l.starts_with("grant_keyd_release_set_audience_drift_seconds{release_set=\"rs\"}")
            })
            .expect("drift series present");
        let age: u64 = line.rsplit(' ').next().unwrap().parse().unwrap();
        assert!(
            (118..=125).contains(&age),
            "drift should render ~120s, got {age}"
        );

        // The set lands: absent from the map means in sync, and the gauge must
        // return to 0 rather than hold its last reading.
        m.record_drift(
            &BTreeMap::new(),
            &[("rs".to_string(), 42u64)].into_iter().collect(),
        );
        let text = m.render();
        assert!(
            text.contains("grant_keyd_release_set_audience_drift_seconds{release_set=\"rs\"} 0")
        );
        assert!(text.contains(
            "grant_keyd_release_set_rotation_duration_seconds{release_set=\"rs\"} 0.042"
        ));
    }

    #[test]
    fn a_set_whose_first_rotate_failed_still_gets_a_drift_series() {
        // It has no other recorded state to hang a label off, and it is exactly
        // the set an operator most needs to see.
        let m = Metrics::new(
            "ks",
            KeySourceMode::Svid,
            AudienceSource::PortalGrantRows,
            30,
        );
        let stamped: BTreeMap<String, u64> = [("newrs".to_string(), now_secs() - 5)]
            .into_iter()
            .collect();
        m.record_drift(&stamped, &BTreeMap::new());
        assert!(m
            .render()
            .contains("grant_keyd_release_set_audience_drift_seconds{release_set=\"newrs\"}"));
    }

    #[test]
    fn the_break_glass_key_source_is_visible_as_a_zero() {
        let unvalidated = Metrics::new(
            "ks",
            KeySourceMode::UnvalidatedCertDir,
            AudienceSource::OperatorDeclared,
            30,
        );
        assert!(unvalidated
            .render()
            .contains("grant_keyd_key_source_validated 0"));
        assert!(unvalidated
            .render()
            .contains("key_source=\"unvalidated_cert_dir\""));
        assert!(metrics()
            .render()
            .contains("grant_keyd_key_source_validated 1"));
    }

    #[test]
    fn health_fails_once_the_loop_has_been_silent_for_three_intervals() {
        let m = metrics();
        // Fresh process, no sweep yet: healthy (start-time grace).
        assert!(m.healthy());
        // Simulate a process that started long ago and swept long ago.
        let old = now_secs() - 10_000;
        let stale = Arc::new(Metrics {
            start_time: old,
            last_sweep: AtomicU64::new(old),
            ..Metrics::clone_shape(&m)
        });
        assert!(!stale.healthy());
        assert!(stale.health_line().starts_with("stale "));
    }

    #[test]
    fn label_values_are_escaped() {
        let m = Metrics::new(
            "sign\"er\\1",
            KeySourceMode::Svid,
            AudienceSource::OperatorDeclared,
            30,
        );
        m.record_sweep(
            1,
            &[SweepOutcome::Unchanged {
                release_set: "we\"ird".into(),
            }],
        );
        let text = m.render();
        assert!(text.contains("signer_id=\"sign\\\"er\\\\1\""));
        assert!(text.contains("release_set=\"we\\\"ird\""));
    }

    #[test]
    fn the_endpoint_serves_metrics_health_and_404() {
        let m = metrics();
        m.record_sweep(
            1,
            &[SweepOutcome::Unchanged {
                release_set: "rs".into(),
            }],
        );
        let addr = serve("127.0.0.1:0", Arc::clone(&m)).expect("bind ephemeral");

        let (status, body) = get(addr, "/metrics");
        assert_eq!(status, 200);
        assert!(body.contains("# TYPE grant_keyd_up gauge"));
        assert!(body.contains("grant_keyd_sweeps_total 1"));

        let (status, body) = get(addr, "/healthz");
        assert_eq!(status, 200);
        assert!(body.starts_with("ok "));

        let (status, _) = get(addr, "/nope");
        assert_eq!(status, 404);
    }

    #[test]
    fn a_stale_daemon_answers_healthz_with_503() {
        let old = now_secs() - 10_000;
        let stale = Arc::new(Metrics {
            start_time: old,
            last_sweep: AtomicU64::new(old),
            ..Metrics::clone_shape(&metrics())
        });
        let addr = serve("127.0.0.1:0", stale).expect("bind ephemeral");
        let (status, body) = get(addr, "/healthz");
        assert_eq!(status, 503, "a wedged loop must fail the container probe");
        assert!(body.starts_with("stale "));
    }

    #[test]
    fn a_request_split_across_segments_is_still_parsed_whole() {
        // The bug the first live smoke of `--healthcheck` caught: the server did
        // one read(), so a request delivered in pieces parsed as a truncated path
        // (a bogus 404) and the unread remainder made the close an RST at the
        // client. Any client whose write lands in more than one segment hits it.
        let addr = serve("127.0.0.1:0", metrics()).expect("bind ephemeral");
        let mut s = TcpStream::connect(addr).expect("connect");
        for piece in [
            "GET /heal",
            "thz HTTP/1.1\r\n",
            "Host: x\r\n",
            "Connection: close\r\n",
            "\r\n",
        ] {
            s.write_all(piece.as_bytes()).expect("write piece");
            s.flush().expect("flush");
            std::thread::sleep(Duration::from_millis(5));
        }
        let mut raw = String::new();
        s.read_to_string(&mut raw).expect("no reset, full response");
        assert!(raw.starts_with("HTTP/1.1 200 "), "got: {raw:?}");
    }

    #[test]
    fn probe_target_maps_a_wildcard_bind_to_loopback_and_refuses_a_disabled_listener() {
        assert_eq!(
            probe_target("0.0.0.0:9464").expect("wildcard"),
            "127.0.0.1:9464".parse::<SocketAddr>().unwrap()
        );
        assert_eq!(
            probe_target("[::]:9464").expect("v6 wildcard"),
            "[::1]:9464".parse::<SocketAddr>().unwrap()
        );
        // A specific bind is probed as-is.
        assert_eq!(
            probe_target("127.0.0.1:9999").expect("specific"),
            "127.0.0.1:9999".parse::<SocketAddr>().unwrap()
        );
        assert!(
            probe_target("").is_err(),
            "disabled listener is not probable"
        );
        assert!(probe_target("nonsense").is_err());
    }

    /// Minimal client for the assertions above — the same `probe` the
    /// `--healthcheck` mode uses, so the tests exercise the shipped client.
    fn get(addr: SocketAddr, path: &str) -> (u16, String) {
        probe(addr, path).expect("probe")
    }

    impl Metrics {
        /// Test helper: a copy of the configuration fields with fresh counters, so
        /// a test can override `start_time`/`last_sweep` via struct update syntax.
        fn clone_shape(&self) -> Metrics {
            Metrics {
                signer_id: self.signer_id.clone(),
                key_source: self.key_source,
                audience_source: self.audience_source,
                poll_interval_secs: self.poll_interval_secs,
                stale_after_secs: self.stale_after_secs,
                start_time: self.start_time,
                last_sweep: AtomicU64::new(0),
                last_rotation: AtomicU64::new(0),
                sweeps_total: AtomicU64::new(0),
                rotations_total: AtomicU64::new(0),
                resolve_errors: AtomicU64::new(0),
                rotate_errors: AtomicU64::new(0),
                enumerate_errors: AtomicU64::new(0),
                sets_swept: AtomicU64::new(0),
                sets: Mutex::new(BTreeMap::new()),
            }
        }
    }
}
