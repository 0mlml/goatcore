// `goat inspect` — Phase E.1. Delegates diagnostic logic to stethoscope
// by consuming its `/api/diagnose` SSE stream and rendering each
// `outcome` event as it arrives. No checks live in goat-cli; this is
// pure transport + render.
//
// Endpoint resolution: --endpoint > $GOAT_STETHOSCOPE_ENDPOINT >
// hostname-substitution from the profile's portal_endpoint
// (`portal.<rest>` → `stethoscope.<rest>`). The substitution rule
// matches the `*.netbird.<env>` naming convention from
// CLAUDE.md §"Hosted services" — operators with a non-conventional
// deployment use --endpoint explicitly.
//
// SSE parsing: minimal hand-roll. Each event block is separated by
// blank lines; within a block, `event:` and `data:` lines accumulate.
// Terminates on `event: done`. No reconnect — `goat inspect` is
// one-shot.

use clap::Args;
use i18n_embed_fl::fl;
use futures::StreamExt;
use serde::Deserialize;

const SSE_TIMEOUT_SECS: u64 = 120;

#[derive(Args, Debug)]
pub struct InspectArgs {
    /// Target spec: `vendor=<prefix>`, `topic=<key>`, or `system=<name>`.
    /// Forwarded verbatim to stethoscope's `/api/diagnose` endpoint.
    pub target: String,
    /// Emit one JSON line per outcome instead of pretty glyph render.
    #[arg(long)]
    pub json: bool,
    /// Open the interactive Diagnose lens instead of the line-oriented
    /// render. Requires a terminal (refuses on a non-TTY); mutually
    /// exclusive with --json.
    #[arg(long, conflicts_with = "json")]
    pub tui: bool,
    /// Override the stethoscope endpoint (default: derived from the
    /// profile's portal_endpoint by substituting `portal.*` →
    /// `stethoscope.*`, also honors `$GOAT_STETHOSCOPE_ENDPOINT`).
    #[arg(long, env = "GOAT_STETHOSCOPE_ENDPOINT")]
    pub endpoint: Option<String>,
}

#[derive(Debug, Clone, Deserialize)]
pub(crate) struct CheckOutcome {
    pub(crate) check_id: String,
    pub(crate) verdict: String,
    pub(crate) summary: String,
    #[serde(default)]
    pub(crate) findings: Vec<Finding>,
    #[serde(default)]
    pub(crate) remediation: Vec<Remediation>,
}

#[derive(Debug, Clone, Deserialize)]
pub(crate) struct Finding {
    pub(crate) label: String,
    pub(crate) severity: String,
    pub(crate) detail: String,
}

#[derive(Debug, Clone, Deserialize)]
pub(crate) struct Remediation {
    pub(crate) title: String,
    pub(crate) detail: String,
}

pub async fn run(ctx: &super::Ctx, args: InspectArgs) -> anyhow::Result<()> {
    // --tui opens the interactive Diagnose lens. TTY-gate FIRST (before
    // any profile/endpoint resolution) so a non-TTY invocation refuses
    // cheaply and deterministically — a piped/CI `goat inspect --tui`
    // never needs a profile to get the refusal. Per docs/design/goat-cli-tui.md.
    if args.tui {
        crate::tui::require_tty("goat inspect --tui")?;
        let endpoint = resolve_endpoint(args.endpoint.as_deref(), &ctx.profile)?;
        let url = build_url(&endpoint, &args.target)?;
        return crate::tui::diagnose::run(url, args.target.clone()).await;
    }

    let endpoint = resolve_endpoint(args.endpoint.as_deref(), &ctx.profile)?;
    let url = build_url(&endpoint, &args.target)?;

    let resp = open_diagnose_response(&url).await?;

    let mut stream = resp.bytes_stream();
    let mut buf = String::new();
    let mut got_any = false;

    while let Some(chunk) = stream.next().await {
        let chunk = chunk.map_err(|e| anyhow::anyhow!("read SSE chunk: {e}"))?;
        buf.push_str(&String::from_utf8_lossy(&chunk));

        while let Some(end) = buf.find("\n\n") {
            let block: String = buf.drain(..=end + 1).collect();
            let (event, data) = parse_sse_event_block(&block);

            match event.as_str() {
                "outcome" => {
                    got_any = true;
                    if args.json {
                        println!("{}", data);
                    } else {
                        match serde_json::from_str::<CheckOutcome>(&data) {
                            Ok(o) => print!("{}", render_outcome(&o)),
                            Err(e) => eprintln!(
                                "warn: outcome event with malformed JSON: {e}; raw: {data}"
                            ),
                        }
                    }
                }
                "done" => {
                    if !got_any && !args.json {
                        eprintln!("{}", fl!(crate::i18n::LANGUAGE_LOADER, "inspect-no-outcomes"));
                    }
                    return Ok(());
                }
                "" => {}
                other => eprintln!("note: unknown SSE event `{other}` ignored"),
            }
        }
    }

    Err(anyhow::anyhow!(
        "stethoscope SSE stream ended without `done` event ({} outcome(s) received)",
        if got_any { "some" } else { "0" }
    ))
}

/// Parse one SSE event block (lines between blank-line separators) into
/// its `event:` and `data:` fields. Multiple `data:` lines accumulate
/// newline-joined per the SSE spec. Shared by the line renderer and the
/// TUI Diagnose lens so both parse identically.
pub(crate) fn parse_sse_event_block(block: &str) -> (String, String) {
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
    (event, data)
}

/// Open the stethoscope `/api/diagnose` SSE response: build the client,
/// GET with the event-stream Accept header, and surface a non-2xx as a
/// one-line error. Returns the streaming response for the caller to
/// drain. Shared by the line renderer and the TUI Diagnose lens.
pub(crate) async fn open_diagnose_response(url: &str) -> anyhow::Result<reqwest::Response> {
    let client = reqwest::Client::builder()
        .timeout(std::time::Duration::from_secs(SSE_TIMEOUT_SECS))
        .build()
        .map_err(|e| anyhow::anyhow!("build reqwest client: {e}"))?;

    let resp = client
        .get(url)
        .header("Accept", "text/event-stream")
        .send()
        .await
        .map_err(|e| anyhow::anyhow!("stethoscope GET {url}: {e}"))?;

    let status = resp.status();
    if !status.is_success() {
        let body = resp.text().await.unwrap_or_default();
        anyhow::bail!(
            "stethoscope returned {} for {url}: {}",
            status.as_u16(),
            body.lines().next().unwrap_or("").trim(),
        );
    }
    Ok(resp)
}

pub(crate) fn resolve_endpoint(
    arg: Option<&str>,
    profile: &crate::profile::Profile,
) -> anyhow::Result<String> {
    if let Some(e) = arg {
        return Ok(e.trim_end_matches('/').to_string());
    }
    profile
        .require_initialized()
        .map_err(|e| e.with_extra_hint("Or pass --endpoint <stethoscope-url>."))?;
    let toml = profile.read_classified(&profile.profile_toml_path())?;
    let portal = extract_toml_string(&toml, "portal_endpoint")
        .ok_or_else(|| anyhow::anyhow!("profile.toml missing portal_endpoint"))?;
    let stethoscope = substitute_host(&portal, "portal", "stethoscope").ok_or_else(|| {
        anyhow::anyhow!(
            "could not derive stethoscope endpoint from portal_endpoint `{portal}` \
             (expected hostname starting with `portal.`); pass --endpoint <url>"
        )
    })?;
    Ok(stethoscope.trim_end_matches('/').to_string())
}

/// Derive the panoscope snapshot endpoint for `goat ui` (Phase D). Per
/// the resolved open question in docs/design/goat-cli-tui.md: prefer an
/// explicit arg, then `$GOAT_PANOSCOPE_ENDPOINT`, else derive from the
/// profile's `portal_endpoint` by `portal.* → panoscope.*` host
/// substitution — the same mechanism `resolve_endpoint` uses for
/// stethoscope. No new profile/bundle field.
pub(crate) fn resolve_panoscope_endpoint(
    arg: Option<&str>,
    profile: &crate::profile::Profile,
) -> anyhow::Result<String> {
    if let Some(e) = arg {
        return Ok(e.trim_end_matches('/').to_string());
    }
    if let Ok(e) = std::env::var("GOAT_PANOSCOPE_ENDPOINT") {
        if !e.is_empty() {
            return Ok(e.trim_end_matches('/').to_string());
        }
    }
    profile.require_initialized().map_err(|e| {
        e.with_extra_hint(
            "Or pass --snapshot-endpoint <panoscope-url> / set $GOAT_PANOSCOPE_ENDPOINT.",
        )
    })?;
    let toml = profile.read_classified(&profile.profile_toml_path())?;
    let portal = extract_toml_string(&toml, "portal_endpoint")
        .ok_or_else(|| anyhow::anyhow!("profile.toml missing portal_endpoint"))?;
    let panoscope = substitute_host(&portal, "portal", "panoscope").ok_or_else(|| {
        anyhow::anyhow!(
            "could not derive panoscope endpoint from portal_endpoint `{portal}` \
             (expected hostname starting with `portal.`); pass --snapshot-endpoint <url>"
        )
    })?;
    Ok(panoscope.trim_end_matches('/').to_string())
}

fn substitute_host(url: &str, from_prefix: &str, to_prefix: &str) -> Option<String> {
    let (scheme, rest) = url.split_once("://")?;
    let (host_and_path, _) = rest.split_once('/').unwrap_or((rest, ""));
    let path = if rest.len() > host_and_path.len() {
        &rest[host_and_path.len()..]
    } else {
        ""
    };
    let prefix_with_dot = format!("{from_prefix}.");
    if !host_and_path.starts_with(&prefix_with_dot) {
        return None;
    }
    let host_rest = &host_and_path[prefix_with_dot.len()..];
    Some(format!("{scheme}://{to_prefix}.{host_rest}{path}"))
}

pub(crate) fn build_url(endpoint: &str, target: &str) -> anyhow::Result<String> {
    let (key, value) = target.split_once('=').ok_or_else(|| {
        anyhow::anyhow!(
            "target must be `vendor=<prefix>` | `topic=<key>` | `system=<name>`; got `{target}`"
        )
    })?;
    match key {
        "vendor" | "topic" | "system" => {}
        other => anyhow::bail!(
            "target key must be `vendor`/`topic`/`system`; got `{other}`"
        ),
    }
    Ok(format!(
        "{}/api/diagnose?{}={}",
        endpoint,
        key,
        url_encode(value),
    ))
}

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

// See crate::profile::extract_toml_field's doc comment: this used to be a
// locally hand-rolled parser with a prefix-collision bug. Now an alias onto
// the one shared, correct implementation.
use crate::profile::extract_toml_field as extract_toml_string;

fn render_outcome(o: &CheckOutcome) -> String {
    let glyph = match o.verdict.as_str() {
        "pass" => "[✓]",
        "warn" => "[!]",
        "fail" => "[✗]",
        "skipped" => "[-]",
        "na" => "[/]",
        _ => "[?]",
    };
    let mut out = format!("{glyph} {:18}  {}\n", o.check_id, o.summary);
    for f in &o.findings {
        out.push_str("      ");
        out.push_str(&fl!(
            crate::i18n::LANGUAGE_LOADER,
            "inspect-finding-label",
            severity = f.severity.clone(),
            label = f.label.clone(),
            detail = f.detail.clone()
        ));
        out.push('\n');
    }
    for r in &o.remediation {
        out.push_str("      ");
        out.push_str(&fl!(
            crate::i18n::LANGUAGE_LOADER,
            "inspect-remedy-label",
            title = r.title.clone(),
            detail = r.detail.clone()
        ));
        out.push('\n');
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn host_substitution_preserves_scheme_and_path() {
        assert_eq!(
            substitute_host("https://portal.netbird.legacy.example", "portal", "stethoscope"),
            Some("https://stethoscope.netbird.legacy.example".into())
        );
        assert_eq!(
            substitute_host(
                "https://portal.netbird.legacy.example/api/x",
                "portal",
                "stethoscope"
            ),
            Some("https://stethoscope.netbird.legacy.example/api/x".into())
        );
    }

    #[test]
    fn host_substitution_refuses_mismatch() {
        assert_eq!(
            substitute_host("https://goat-portal.netbird.x", "portal", "stethoscope"),
            None
        );
    }

    #[test]
    fn build_url_vendor_target() {
        assert_eq!(
            build_url("https://stethoscope.x", "vendor=drs").unwrap(),
            "https://stethoscope.x/api/diagnose?vendor=drs"
        );
    }

    #[test]
    fn build_url_topic_target_with_slashes_encodes() {
        let u = build_url("https://x", "topic=drs/radar1/RADAR-EXAMPLE/tracks").unwrap();
        assert!(u.contains("topic=drs%2Fradar1%2FRADAR-EXAMPLE%2Ftracks"));
    }

    #[test]
    fn build_url_rejects_unknown_key() {
        let err = build_url("https://x", "fleet=nova").unwrap_err();
        assert!(err.to_string().contains("vendor"));
    }

    #[test]
    fn build_url_rejects_missing_equals() {
        let err = build_url("https://x", "drs").unwrap_err();
        assert!(err.to_string().contains("vendor"));
    }

    #[test]
    fn parse_sse_block_extracts_event_and_data() {
        let block = "event: outcome\ndata: {\"check_id\":\"registration\"}\n";
        let (event, data) = parse_sse_event_block(block);
        assert_eq!(event, "outcome");
        assert_eq!(data, "{\"check_id\":\"registration\"}");
    }

    #[test]
    fn parse_sse_block_joins_multiple_data_lines() {
        let block = "event: outcome\ndata: line1\ndata: line2\n";
        let (event, data) = parse_sse_event_block(block);
        assert_eq!(event, "outcome");
        assert_eq!(data, "line1\nline2");
    }

    #[test]
    fn parse_sse_block_done_has_no_data() {
        let (event, data) = parse_sse_event_block("event: done\n");
        assert_eq!(event, "done");
        assert!(data.is_empty());
    }

    #[test]
    fn outcome_decodes_pass_glyph() {
        let raw = r#"{"check_id":"registration","verdict":"pass","summary":"Vendor 'drs' exists with 3 topics."}"#;
        let o: CheckOutcome = serde_json::from_str(raw).unwrap();
        assert!(render_outcome(&o).starts_with("[✓] registration"));
    }

    #[test]
    fn outcome_decodes_fail_with_remediation() {
        let raw = r#"{"check_id":"schema-body","verdict":"fail","summary":"x","findings":[],"remediation":[{"title":"Re-upload","detail":"Use goat topic create --format protobuf:MyMessage"}]}"#;
        let o: CheckOutcome = serde_json::from_str(raw).unwrap();
        let out = render_outcome(&o);
        assert!(out.starts_with("[✗] schema-body"));
        assert!(out.contains("remedy: Re-upload"));
    }
}
