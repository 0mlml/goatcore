// `Narrator` — Phase D.C inline cascade emission for `--narrate` on
// mutating commands. The full design-doc shape (an inline audit-tail line +
// panoscope topology line per mutation) isn't wired yet — the audit-tail
// read path itself now exists (G.6 portal-presign, see cmd/replay.rs +
// `goat narrate`), but an inline per-mutation poll within the cascade
// deadline is a separate follow-on. What we observe end-to-end today:
//
//   - Portal mutation HTTP response — the call we just made.
//   - Stethoscope post-mutation diagnostic verdict — best-effort, short
//     deadline, surfaces the most-immediate substrate consequence.
//   - Zenoh PUT confirmation — already in our hands for `pub`.
//   - Elapsed wall-clock since narrate-begin for each event.
//
// Lines that DON'T appear today are documented inline below. They land
// when D.D resolves how operator-side reads access the audit-tail
// (MinIO creds in bundle | portal proxy | presigned URL).

use i18n_embed_fl::fl;
use std::time::{Duration, Instant};

/// Substrate cascade events surfaced for a single mutating command.
/// Pretty-printed as `[t+N.NNs] <human description>` lines on stdout;
/// `--narrate --json` swaps to one NDJSON line per event with the same
/// elapsed-time field.
#[derive(Debug, Clone)]
pub enum Consequence {
    /// The mutating HTTP request to the portal returned a response. The
    /// portal is the authoritative write surface for vendor/topic CRUD;
    /// this is "the substrate accepted the change."
    PortalConfirmed {
        method: &'static str,
        path: String,
        http_status: u16,
        outcome: String,
    },
    /// A Zenoh PUT was accepted by the local Zenoh stack. From `goat
    /// pub`. Doesn't say anything about subscriber delivery — that's
    /// `SubscriberDelivery`, which requires liveliness observation.
    ZenohPutConfirmed {
        key: String,
        bytes: usize,
    },
    /// A stethoscope diagnostic check observed post-mutation. The
    /// verdict reflects whether the substrate has caught up with the
    /// mutation (e.g., the registration check sees the new topic).
    StethoscopeVerdict {
        check_id: String,
        verdict: String,
        summary: String,
    },
    /// A best-effort observation note that's interesting in pretty mode
    /// but doesn't carry structural meaning beyond context. Used when a
    /// post-mutation probe didn't return in time, etc.
    Note(String),
}

/// Inline narrator used by mutating commands when `--narrate` is set.
/// All emit operations are no-ops when `enabled = false` so call sites
/// don't need to guard each call themselves.
pub struct Narrator {
    enabled: bool,
    json: bool,
    start: Instant,
    count: std::cell::Cell<u32>,
}

impl Narrator {
    pub fn new(enabled: bool, json: bool) -> Self {
        Self {
            enabled,
            json,
            start: Instant::now(),
            count: std::cell::Cell::new(0),
        }
    }

    pub fn is_enabled(&self) -> bool {
        self.enabled
    }

    /// Emit one cascade event to stdout.
    pub fn emit(&self, c: Consequence) {
        if !self.enabled {
            return;
        }
        self.count.set(self.count.get() + 1);
        let t = self.start.elapsed().as_secs_f64();
        if self.json {
            let obj = serde_json::json!({
                "t_secs": format!("{t:.3}").parse::<f64>().unwrap_or(t),
                "event": render_json(&c),
            });
            println!("{}", serde_json::to_string(&obj).unwrap_or_default());
        } else {
            println!("[t+{t:.2}s] {}", render_pretty(&c));
        }
    }

    // NOTE: the `--narrate --json` branch above emits structured fields
    // and stays locale-invariant by rule (ADR 1024); only `render_pretty`
    // (the human path) localizes.

    /// Emit the closing "cascade settled" line summarizing the count.
    pub fn settled(&self, summary: &str) {
        if !self.enabled {
            return;
        }
        let t = self.start.elapsed().as_secs_f64();
        let n = self.count.get();
        if self.json {
            let obj = serde_json::json!({
                "t_secs": format!("{t:.3}").parse::<f64>().unwrap_or(t),
                "event": "settled",
                "consequences": n,
                "summary": summary,
            });
            println!("{}", serde_json::to_string(&obj).unwrap_or_default());
        } else {
            let line = fl!(
                crate::i18n::LANGUAGE_LOADER,
                "narrator-settled",
                summary = summary,
                // Pass `n` as a number so Fluent's plural-category select
                // (`{ $n -> [one] … *[other] … }`) resolves per the active
                // locale's CLDR rules.
                n = n
            );
            println!("[t+{t:.2}s] {line}");
        }
    }
}

fn render_pretty(c: &Consequence) -> String {
    match c {
        Consequence::PortalConfirmed {
            method,
            path,
            http_status,
            outcome,
        } => fl!(
            crate::i18n::LANGUAGE_LOADER,
            "narrator-portal-confirmed",
            method = method.to_string(),
            path = path.clone(),
            http_status = http_status.to_string(),
            outcome = outcome.clone()
        ),
        Consequence::ZenohPutConfirmed { key, bytes } => fl!(
            crate::i18n::LANGUAGE_LOADER,
            "narrator-zenoh-put",
            key = key.clone(),
            bytes = bytes.to_string()
        ),
        Consequence::StethoscopeVerdict {
            check_id,
            verdict,
            summary,
        } => fl!(
            crate::i18n::LANGUAGE_LOADER,
            "narrator-stethoscope",
            check_id = check_id.clone(),
            verdict = verdict.clone(),
            summary = summary.clone()
        ),
        // A free-text note supplied by the call site; passed through
        // verbatim (its own prose localizes at the producing site, not
        // here).
        Consequence::Note(s) => s.clone(),
    }
}

fn render_json(c: &Consequence) -> serde_json::Value {
    match c {
        Consequence::PortalConfirmed {
            method,
            path,
            http_status,
            outcome,
        } => serde_json::json!({
            "kind": "portal-confirmed",
            "method": method,
            "path": path,
            "http_status": http_status,
            "outcome": outcome,
        }),
        Consequence::ZenohPutConfirmed { key, bytes } => serde_json::json!({
            "kind": "zenoh-put-confirmed",
            "key": key,
            "bytes": bytes,
        }),
        Consequence::StethoscopeVerdict {
            check_id,
            verdict,
            summary,
        } => serde_json::json!({
            "kind": "stethoscope-verdict",
            "check_id": check_id,
            "verdict": verdict,
            "summary": summary,
        }),
        Consequence::Note(s) => serde_json::json!({
            "kind": "note",
            "text": s,
        }),
    }
}

/// Observe the next stethoscope verdict matching `interesting_check`
/// for a given target, with a deadline. Returns the matching verdict
/// or `None` if the deadline elapsed or no matching outcome appeared.
///
/// Designed for `--narrate`: pulls in just enough of the inspect SSE
/// pattern to score a single check outcome, with a short window so
/// mutators don't block forever waiting on a substrate component
/// that may not be up.
pub async fn observe_stethoscope_check(
    endpoint: &str,
    target_key: &str, // "topic" | "vendor" | "system"
    target_value: &str,
    interesting_check: &str,
    deadline: Duration,
) -> Option<Consequence> {
    let url = format!(
        "{}/api/diagnose?{}={}",
        endpoint.trim_end_matches('/'),
        target_key,
        url_encode(target_value),
    );
    let client = reqwest::Client::builder()
        .timeout(deadline)
        .build()
        .ok()?;
    let resp = client
        .get(&url)
        .header("Accept", "text/event-stream")
        .send()
        .await
        .ok()?;
    if !resp.status().is_success() {
        return None;
    }

    use futures::StreamExt;
    let mut stream = resp.bytes_stream();
    let mut buf = String::new();
    let started = Instant::now();
    while started.elapsed() < deadline {
        let chunk = tokio::select! {
            r = stream.next() => match r {
                Some(Ok(c)) => c,
                _ => return None,
            },
            _ = tokio::time::sleep(deadline.saturating_sub(started.elapsed())) => return None,
        };
        buf.push_str(&String::from_utf8_lossy(&chunk));
        match scan_blocks_for_outcome(&mut buf, interesting_check) {
            BlockScanOutcome::Match(c) => return Some(c),
            BlockScanOutcome::Done => return None,
            BlockScanOutcome::NeedMore => continue,
        }
    }
    None
}

/// Outcome of draining all complete SSE blocks from the buffer.
#[derive(Debug)]
enum BlockScanOutcome {
    /// One block was an `event: outcome` whose `check_id` matched
    /// `interesting`. Consume the rest of the buffer as already-parsed.
    Match(Consequence),
    /// A `done` event was reached. The caller should stop reading.
    Done,
    /// No complete block in the buffer matched. Caller should fetch
    /// more bytes.
    NeedMore,
}

/// Drain all complete SSE event-blocks (terminated by blank-line
/// delimiters) from `buf` and return the first outcome that matches
/// `interesting_check`, or Done if a `done` event was seen, or
/// NeedMore if the buffer ended mid-block. Pure function — operates
/// on an in-memory `String`, no I/O. Tests construct the buffer
/// directly with canned SSE bytes.
fn scan_blocks_for_outcome(buf: &mut String, interesting_check: &str) -> BlockScanOutcome {
    while let Some(end) = buf.find("\n\n") {
        let block: String = buf.drain(..=end + 1).collect();
        match parse_sse_block(&block) {
            ParsedBlock::Outcome { check_id, verdict, summary } => {
                if check_id == interesting_check {
                    return BlockScanOutcome::Match(Consequence::StethoscopeVerdict {
                        check_id,
                        verdict,
                        summary,
                    });
                }
            }
            ParsedBlock::Done => return BlockScanOutcome::Done,
            ParsedBlock::Other => continue,
        }
    }
    BlockScanOutcome::NeedMore
}

/// One parsed SSE block. `Other` covers comment/empty/heartbeat blocks
/// plus outcomes that failed to deserialize (treated as ignorable; the
/// caller skips to the next).
#[derive(Debug, PartialEq, Eq)]
enum ParsedBlock {
    Outcome {
        check_id: String,
        verdict: String,
        summary: String,
    },
    Done,
    Other,
}

fn parse_sse_block(block: &str) -> ParsedBlock {
    let mut event = String::new();
    let mut data = String::new();
    for line in block.lines() {
        if let Some(rest) = line.strip_prefix("event:") {
            event = rest.trim().to_string();
        } else if let Some(rest) = line.strip_prefix("data:") {
            if !data.is_empty() {
                data.push('\n');
            }
            data.push_str(rest.trim());
        }
    }
    if event == "done" {
        return ParsedBlock::Done;
    }
    if event == "outcome" {
        #[derive(serde::Deserialize)]
        struct Outcome {
            check_id: String,
            verdict: String,
            summary: String,
        }
        if let Ok(o) = serde_json::from_str::<Outcome>(&data) {
            return ParsedBlock::Outcome {
                check_id: o.check_id,
                verdict: o.verdict,
                summary: o.summary,
            };
        }
    }
    ParsedBlock::Other
}

/// Re-export of the URL-encode helper used by inspect — kept private
/// here to avoid pulling inspect.rs into a public module boundary just
/// for one helper. Identical semantics; mirroring keeps the two
/// surfaces independently revisable.
fn url_encode(s: &str) -> String {
    let mut out = String::with_capacity(s.len());
    for b in s.bytes() {
        let safe = matches!(b,
            b'a'..=b'z' | b'A'..=b'Z' | b'0'..=b'9' |
            b'-' | b'_' | b'.' | b'~'
        );
        if safe {
            out.push(b as char);
        } else {
            out.push_str(&format!("%{:02X}", b));
        }
    }
    out
}

/// Derive a stethoscope endpoint from a portal endpoint by hostname
/// substitution: `portal.*` → `stethoscope.*`. Mirrors the rule in
/// `cmd/inspect.rs` so `--narrate` can post-check without an extra
/// profile-side endpoint key. Returns `None` if the hostname does not
/// match the convention; the caller skips the stethoscope event in
/// that case (still surfaces portal-confirmed + settled).
pub fn derive_stethoscope_from_portal(portal_endpoint: &str) -> Option<String> {
    let (scheme, rest) = portal_endpoint.split_once("://")?;
    let (host_and_path, _) = rest.split_once('/').unwrap_or((rest, ""));
    let path = if rest.len() > host_and_path.len() {
        &rest[host_and_path.len()..]
    } else {
        ""
    };
    if !host_and_path.starts_with("portal.") {
        return None;
    }
    let host_rest = &host_and_path["portal.".len()..];
    Some(format!("{scheme}://stethoscope.{host_rest}{path}").trim_end_matches('/').to_string())
}

/// Derive a microscope endpoint from a portal endpoint by the same
/// `portal.*` → `microscope.*` hostname substitution (ADR 1090). Both
/// surfaces follow the `<svc>.<site>.netbird.<zone>` convention, so
/// `goat microscope` finds the service without a new profile key. Returns
/// `None` if the hostname does not match the convention.
pub fn derive_microscope_from_portal(portal_endpoint: &str) -> Option<String> {
    let (scheme, rest) = portal_endpoint.split_once("://")?;
    let (host_and_path, _) = rest.split_once('/').unwrap_or((rest, ""));
    let path = if rest.len() > host_and_path.len() {
        &rest[host_and_path.len()..]
    } else {
        ""
    };
    if !host_and_path.starts_with("portal.") {
        return None;
    }
    let host_rest = &host_and_path["portal.".len()..];
    Some(format!("{scheme}://microscope.{host_rest}{path}").trim_end_matches('/').to_string())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn disabled_narrator_is_silent() {
        // No panic, no print. Just verifying the no-op branch compiles
        // and the cell stays at zero.
        let n = Narrator::new(false, false);
        assert!(!n.is_enabled());
        n.emit(Consequence::Note("hidden".into()));
        assert_eq!(n.count.get(), 0);
    }

    #[test]
    fn enabled_narrator_counts() {
        let n = Narrator::new(true, false);
        n.emit(Consequence::Note("a".into()));
        n.emit(Consequence::Note("b".into()));
        assert_eq!(n.count.get(), 2);
    }

    #[test]
    fn render_json_shapes_each_consequence() {
        // The NDJSON contract for `--narrate-json`: a stable `kind`
        // discriminator + typed fields per consequence, so an incident log /
        // script can pin to the shape (Conventions §1 stable --json schema).
        let portal = render_json(&Consequence::PortalConfirmed {
            method: "POST",
            path: "/api/orgs/acme/topics".into(),
            http_status: 201,
            outcome: "created".into(),
        });
        assert_eq!(portal["kind"], "portal-confirmed");
        assert_eq!(portal["method"], "POST");
        assert_eq!(portal["http_status"], 201);
        assert_eq!(portal["outcome"], "created");

        let put = render_json(&Consequence::ZenohPutConfirmed {
            key: "acme/x/v1".into(),
            bytes: 42,
        });
        assert_eq!(put["kind"], "zenoh-put-confirmed");
        assert_eq!(put["key"], "acme/x/v1");
        assert_eq!(put["bytes"], 42);

        let verdict = render_json(&Consequence::StethoscopeVerdict {
            check_id: "registration".into(),
            verdict: "pass".into(),
            summary: "ok".into(),
        });
        assert_eq!(verdict["kind"], "stethoscope-verdict");
        assert_eq!(verdict["check_id"], "registration");
        assert_eq!(verdict["verdict"], "pass");

        let note = render_json(&Consequence::Note("deferred".into()));
        assert_eq!(note["kind"], "note");
        assert_eq!(note["text"], "deferred");
    }

    #[test]
    fn parse_sse_block_outcome_round_trip() {
        let block = "event: outcome\ndata: {\"check_id\":\"registration\",\"verdict\":\"pass\",\"summary\":\"Topic registered.\"}\n\n";
        let p = parse_sse_block(block);
        assert_eq!(
            p,
            ParsedBlock::Outcome {
                check_id: "registration".into(),
                verdict: "pass".into(),
                summary: "Topic registered.".into(),
            }
        );
    }

    #[test]
    fn parse_sse_block_done_event() {
        let block = "event: done\ndata: {}\n\n";
        assert_eq!(parse_sse_block(block), ParsedBlock::Done);
    }

    #[test]
    fn parse_sse_block_unknown_event_is_other() {
        let block = "event: heartbeat\ndata: {}\n\n";
        assert_eq!(parse_sse_block(block), ParsedBlock::Other);
    }

    #[test]
    fn parse_sse_block_outcome_with_malformed_json_is_other() {
        // Stethoscope-side serializer drift could send malformed JSON;
        // we treat that as ignorable rather than crashing — the next
        // block might be well-formed.
        let block = "event: outcome\ndata: {not-json}\n\n";
        assert_eq!(parse_sse_block(block), ParsedBlock::Other);
    }

    #[test]
    fn parse_sse_block_outcome_missing_field_is_other() {
        // Missing `summary` → deserialize fails → treated as Other.
        let block = "event: outcome\ndata: {\"check_id\":\"x\",\"verdict\":\"pass\"}\n\n";
        assert_eq!(parse_sse_block(block), ParsedBlock::Other);
    }

    #[test]
    fn parse_sse_block_multi_line_data_concatenated() {
        // Per SSE spec, multiple `data:` lines in one block concatenate
        // with `\n`. We rebuild the JSON object across two data lines.
        let block = concat!(
            "event: outcome\n",
            "data: {\"check_id\":\"reg\",\n",
            "data: \"verdict\":\"pass\",\"summary\":\"ok\"}\n",
            "\n",
        );
        let p = parse_sse_block(block);
        // Both data lines concatenate before deserialize; result is
        // the structured outcome.
        assert!(matches!(p, ParsedBlock::Outcome { .. }));
    }

    #[test]
    fn scan_blocks_returns_match_for_interesting_check() {
        let mut buf = "event: outcome\ndata: {\"check_id\":\"registration\",\"verdict\":\"pass\",\"summary\":\"ok\"}\n\n".to_string();
        let r = scan_blocks_for_outcome(&mut buf, "registration");
        assert!(matches!(r, BlockScanOutcome::Match(_)));
    }

    #[test]
    fn scan_blocks_skips_uninteresting_check_returns_need_more() {
        let mut buf = "event: outcome\ndata: {\"check_id\":\"latency\",\"verdict\":\"pass\",\"summary\":\"ok\"}\n\n".to_string();
        let r = scan_blocks_for_outcome(&mut buf, "registration");
        assert!(matches!(r, BlockScanOutcome::NeedMore));
        // The block was consumed even though it didn't match — buffer
        // is now empty.
        assert!(buf.is_empty());
    }

    #[test]
    fn scan_blocks_finds_match_after_skipping_uninteresting() {
        let mut buf = String::new();
        buf.push_str("event: outcome\ndata: {\"check_id\":\"latency\",\"verdict\":\"pass\",\"summary\":\"x\"}\n\n");
        buf.push_str("event: outcome\ndata: {\"check_id\":\"registration\",\"verdict\":\"fail\",\"summary\":\"missing\"}\n\n");
        let r = scan_blocks_for_outcome(&mut buf, "registration");
        match r {
            BlockScanOutcome::Match(Consequence::StethoscopeVerdict { check_id, verdict, summary }) => {
                assert_eq!(check_id, "registration");
                assert_eq!(verdict, "fail");
                assert_eq!(summary, "missing");
            }
            other => panic!("expected Match, got {other:?}"),
        }
    }

    #[test]
    fn scan_blocks_reaches_done_event() {
        let mut buf = "event: done\ndata: {}\n\n".to_string();
        let r = scan_blocks_for_outcome(&mut buf, "registration");
        assert!(matches!(r, BlockScanOutcome::Done));
    }

    #[test]
    fn scan_blocks_partial_block_leaves_remainder_in_buffer() {
        // A chunk that ended mid-block (no trailing blank line yet)
        // should be left in the buffer for the next iteration.
        let mut buf = "event: outcome\ndata: {\"check_id\":\"reg\",\"verdict\":\"pa".to_string();
        let r = scan_blocks_for_outcome(&mut buf, "reg");
        assert!(matches!(r, BlockScanOutcome::NeedMore));
        // Buffer preserved for caller to append more bytes to.
        assert!(buf.contains("event: outcome"));
        assert!(buf.contains("\"verdict\":\"pa"));
    }

    #[test]
    fn derive_stethoscope_endpoint() {
        assert_eq!(
            derive_stethoscope_from_portal("https://portal.netbird.legacy.example"),
            Some("https://stethoscope.netbird.legacy.example".into())
        );
        assert_eq!(
            derive_stethoscope_from_portal("https://portal.netbird.x/api"),
            Some("https://stethoscope.netbird.x/api".into())
        );
        assert_eq!(
            derive_stethoscope_from_portal("https://goat-portal.x"),
            None
        );
    }
}

/// Integration coverage for `observe_stethoscope_check` — the async
/// `reqwest` + `tokio::select!` path that drives the SSE parser behind
/// `--narrate`'s post-mutation stethoscope probe. The pure
/// `parse_sse_block` / `scan_blocks_for_outcome` helpers are unit-tested
/// above; this module exercises endpoint resolution, HTTP-status
/// handling, and deadline expiry end-to-end against an in-process
/// `wiremock` server serving canned `text/event-stream` bodies. Closes
/// queued gap #1 in
/// docs/project/active-work/queued/goat-cli-test-coverage-followons.yaml.
#[cfg(test)]
mod cascade_integration {
    use super::*;
    use wiremock::matchers::{header, method, path, query_param};
    use wiremock::{Mock, MockServer, ResponseTemplate};

    // Comfortably longer than any local mock round-trip; tests that
    // probe the deadline itself set their own short value inline.
    const MATCH_DEADLINE: Duration = Duration::from_secs(5);

    fn sse_response(body: &str) -> ResponseTemplate {
        ResponseTemplate::new(200)
            .insert_header("content-type", "text/event-stream")
            .set_body_string(body)
    }

    async fn mount_diagnose(server: &MockServer, resp: ResponseTemplate) {
        Mock::given(method("GET"))
            .and(path("/api/diagnose"))
            .respond_with(resp)
            .mount(server)
            .await;
    }

    #[tokio::test]
    async fn returns_verdict_when_matching_outcome_streams() {
        let server = MockServer::start().await;
        let body = "event: outcome\n\
             data: {\"check_id\":\"registration\",\"verdict\":\"pass\",\"summary\":\"Topic registered.\"}\n\n\
             event: done\ndata: {}\n\n";
        mount_diagnose(&server, sse_response(body)).await;

        let got = observe_stethoscope_check(
            &server.uri(),
            "topic",
            "acme.telemetry.v1",
            "registration",
            MATCH_DEADLINE,
        )
        .await;

        match got {
            Some(Consequence::StethoscopeVerdict { check_id, verdict, summary }) => {
                assert_eq!(check_id, "registration");
                assert_eq!(verdict, "pass");
                assert_eq!(summary, "Topic registered.");
            }
            other => panic!("expected a StethoscopeVerdict, got {other:?}"),
        }
    }

    #[tokio::test]
    async fn returns_none_when_only_uninteresting_outcomes_then_done() {
        // The stream carries a real outcome — for a different check — then
        // the terminal `done`. The probe should walk past the unrelated
        // verdict and stop at `done` with nothing to report.
        let server = MockServer::start().await;
        let body = "event: outcome\n\
             data: {\"check_id\":\"latency\",\"verdict\":\"pass\",\"summary\":\"fast\"}\n\n\
             event: done\ndata: {}\n\n";
        mount_diagnose(&server, sse_response(body)).await;

        let got = observe_stethoscope_check(
            &server.uri(),
            "topic",
            "acme.telemetry.v1",
            "registration",
            MATCH_DEADLINE,
        )
        .await;
        assert!(got.is_none(), "non-matching stream must yield None, got {got:?}");
    }

    #[tokio::test]
    async fn returns_none_on_http_500() {
        let server = MockServer::start().await;
        mount_diagnose(&server, ResponseTemplate::new(500)).await;

        let got = observe_stethoscope_check(
            &server.uri(),
            "vendor",
            "acme",
            "registration",
            MATCH_DEADLINE,
        )
        .await;
        assert!(got.is_none(), "5xx must be swallowed to None, got {got:?}");
    }

    #[tokio::test]
    async fn returns_none_on_http_404() {
        let server = MockServer::start().await;
        mount_diagnose(&server, ResponseTemplate::new(404)).await;

        let got = observe_stethoscope_check(
            &server.uri(),
            "system",
            "all",
            "registration",
            MATCH_DEADLINE,
        )
        .await;
        assert!(got.is_none(), "404 must be swallowed to None, got {got:?}");
    }

    #[tokio::test]
    async fn returns_none_on_connection_refused() {
        // Nothing listening → reqwest `send()` errors → the `.ok()?` in
        // observe_stethoscope_check yields None rather than panicking.
        // Port 9 (discard) is closed on dev + CI hosts. Bounded by the
        // 500ms client timeout if the host filters rather than refuses.
        let got = observe_stethoscope_check(
            "http://127.0.0.1:9",
            "topic",
            "acme.telemetry.v1",
            "registration",
            Duration::from_millis(500),
        )
        .await;
        assert!(got.is_none(), "unreachable endpoint must yield None, got {got:?}");
    }

    #[tokio::test]
    async fn encodes_target_value_into_diagnose_query() {
        // Reserved chars in the target value must survive url_encode and
        // arrive intact server-side. The query_param matcher compares the
        // DECODED value, so a match proves the encode round-trips; a
        // mismatch would 404 and surface as None.
        let server = MockServer::start().await;
        let body = "event: outcome\n\
             data: {\"check_id\":\"reg\",\"verdict\":\"pass\",\"summary\":\"ok\"}\n\n";
        Mock::given(method("GET"))
            .and(path("/api/diagnose"))
            .and(query_param("topic", "acme/telemetry v1"))
            .and(header("accept", "text/event-stream"))
            .respond_with(sse_response(body))
            .mount(&server)
            .await;

        let got = observe_stethoscope_check(
            &server.uri(),
            "topic",
            "acme/telemetry v1",
            "reg",
            MATCH_DEADLINE,
        )
        .await;
        assert!(
            matches!(got, Some(Consequence::StethoscopeVerdict { .. })),
            "expected a match — url_encode must round-trip reserved chars and the \
             Accept header must be set; got {got:?}"
        );
    }

    #[tokio::test]
    async fn respects_deadline_when_server_stalls() {
        // Server holds the response far past the deadline. The client
        // timeout + select! sleep must abandon the probe near the
        // deadline instead of hanging for the full server delay.
        let server = MockServer::start().await;
        mount_diagnose(
            &server,
            sse_response("event: outcome\ndata: {}\n\n").set_delay(Duration::from_secs(10)),
        )
        .await;

        let started = Instant::now();
        let got = observe_stethoscope_check(
            &server.uri(),
            "topic",
            "x",
            "registration",
            Duration::from_millis(300),
        )
        .await;
        let elapsed = started.elapsed();

        assert!(got.is_none(), "stalled server must yield None, got {got:?}");
        assert!(
            elapsed < Duration::from_secs(3),
            "should abandon near the 300ms deadline, not wait out the 10s server delay; took {elapsed:?}"
        );
    }
}
