// `goat demo` — one command to run a fabric demo end-to-end.
//
// Today the only target is `demogame`: it opens the service's `/demo`
// page (the eight auto-pilot bots in one tab, see
// the game's own documentation) in your browser AND tails the
// resulting Zenoh traffic in this terminal — both halves of the demo from a
// single command, no macOS-only window-positioning script.
//
// Pure convenience over capabilities that already exist: the browser does the
// playing, `goat sub` does the watching. The demo host + site_id are derived
// from the active profile's portal endpoint (sibling-surface swap), overridable
// with --url / --site for off-profile or localhost runs.

use clap::{Args, ValueEnum};

#[derive(Copy, Clone, Debug, PartialEq, Eq, ValueEnum)]
pub enum DemoTarget {
    /// Demo Game: 8 auto-pilot bots fill the co-op room and drive all four
    /// event types onto the fabric while you watch the tail.
    Demogame,
}

#[derive(Args, Debug)]
pub struct DemoArgs {
    /// Which demo to run. Currently only `demogame`.
    #[arg(value_enum, default_value = "demogame")]
    pub target: DemoTarget,

    /// Override the demo page URL (e.g. http://localhost:8095 for a local
    /// run, or a different goatnet). The `/demo` path is appended if absent.
    #[arg(long, value_name = "URL")]
    pub url: Option<String>,

    /// Override the site_id used for the watch key `demogame/<site>/**`.
    /// Defaults to the site derived from the profile (or the --url host).
    #[arg(long, value_name = "SITE")]
    pub site: Option<String>,

    /// Open the browser only; don't tail the fabric in this terminal.
    /// Combined with --no-open it's a dry-run: print the resolved URL +
    /// watch command and exit without opening anything.
    #[arg(long)]
    pub no_watch: bool,

    /// Tail the fabric only; don't open the browser.
    #[arg(long)]
    pub no_open: bool,

    /// Use the interactive Tail lens (event table + rate sparkline) for the
    /// watch instead of the default line-oriented tail. Requires a terminal.
    #[arg(long)]
    pub tui: bool,
}

pub async fn run(ctx: &super::Ctx, args: DemoArgs) -> anyhow::Result<()> {
    match args.target {
        DemoTarget::Demogame => run_demogame(ctx, args).await,
    }
}

async fn run_demogame(ctx: &super::Ctx, args: DemoArgs) -> anyhow::Result<()> {
    // Resolve the demo URL + site_id: explicit flags win, else derive from the
    // active profile's portal endpoint.
    let (base_url, site) = resolve_target(ctx, &args)?;
    let demo_url = if base_url.trim_end_matches('/').ends_with("/demo") {
        base_url.trim_end_matches('/').to_string()
    } else {
        format!("{}/demo", base_url.trim_end_matches('/'))
    };
    let watch_key = format!("demogame/{site}/**");

    if !args.no_open {
        match open_in_browser(&demo_url) {
            Ok(()) => println!("opened {demo_url}  (8 auto-pilot bots fill the room)"),
            Err(e) => {
                // Non-fatal: print the URL so the operator can open it by hand.
                eprintln!("could not open a browser automatically ({e}).");
                eprintln!("open this URL yourself:  {demo_url}");
            }
        }
    }

    if args.no_watch {
        if args.no_open {
            // Nothing to do — both halves were suppressed.
            println!("demo url: {demo_url}");
            println!("watch:    goat sub --literal '{watch_key}' --follow");
        }
        return Ok(());
    }

    println!("watching the fabric:  goat sub --literal '{watch_key}' --follow   (Ctrl-C to stop)");
    println!();

    // Delegate to the real `sub` path — same decode/alias/TUI behaviour as
    // `goat sub`, so the demo watch is the canonical tail, not a reimplementation.
    let sub_args = super::pubsub::SubArgs {
        key_pattern: watch_key,
        // `follow` is the accepted no-op alias for what is now the default;
        // `count: None` is what actually makes the watch unbounded.
        follow: true,
        decode: true,
        tui: args.tui,
        count: None,
        keys_only: false,
        for_: None,
        since: None,
        open: false,
        key_service_bundle: None,
        trust_domain: None,
        key_service_id: None,
        // The key is generated from the site, not typed, and `demogame/`
        // is a seeded vendor prefix, not a slot alias — on a slot-pool
        // goatnet the resolver would refuse it.
        literal: true,
    };
    super::pubsub::run_sub(ctx, sub_args).await
}

/// Resolve (base_url, site_id) from explicit flags or the active profile.
fn resolve_target(ctx: &super::Ctx, args: &DemoArgs) -> anyhow::Result<(String, String)> {
    // Both flags given: trust them outright (covers localhost / off-profile).
    if let (Some(url), Some(site)) = (&args.url, &args.site) {
        return Ok((url.clone(), site.clone()));
    }

    let derived = ctx.profile.derive_demogame();

    let base_url = match &args.url {
        Some(u) => u.clone(),
        None => derived
            .as_ref()
            .map(|(u, _)| u.clone())
            .ok_or_else(|| {
                anyhow::anyhow!(
                    "couldn't derive the Demo Game URL from profile '{}'. \
                     Pass --url (e.g. --url https://demogame.<site>.netbird.<backbone>, \
                     if your goatnet runs one; the game server is not part of this repository).",
                    ctx.profile.context
                )
            })?,
    };

    let site = match &args.site {
        Some(s) => s.clone(),
        None => args
            .url
            .as_deref()
            .and_then(site_from_url)
            .or_else(|| derived.as_ref().map(|(_, s)| s.clone()))
            .ok_or_else(|| {
                anyhow::anyhow!(
                    "couldn't derive site_id — pass --site <site> so the watch key \
                     'demogame/<site>/**' is correct."
                )
            })?,
    };

    Ok((base_url, site))
}

/// Best-effort site_id from an explicit `--url` of shape
/// `<scheme>://demogame.<site>.netbird.<backbone>`. Returns None for
/// hosts that don't carry a site (e.g. localhost).
fn site_from_url(url: &str) -> Option<String> {
    let host = url
        .trim_start_matches("https://")
        .trim_start_matches("http://");
    let host = host.split('/').next().unwrap_or(host);
    let parts: Vec<&str> = host.split('.').collect();
    let idx = parts.iter().position(|p| *p == "netbird")?;
    if idx < 1 {
        return None;
    }
    Some(parts[idx - 1].to_string())
}

/// Open a URL in the operator's default browser, cross-platform. No new crate
/// dependency — just the platform launcher.
fn open_in_browser(url: &str) -> anyhow::Result<()> {
    use std::process::Command;
    let status = if cfg!(target_os = "macos") {
        Command::new("open").arg(url).status()
    } else if cfg!(target_os = "windows") {
        // `start` is a cmd builtin; the empty "" is the window title arg.
        Command::new("cmd").args(["/C", "start", "", url]).status()
    } else {
        Command::new("xdg-open").arg(url).status()
    }?;
    if !status.success() {
        anyhow::bail!("browser launcher exited with {status}");
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn site_from_url_extracts_label_before_netbird() {
        assert_eq!(
            site_from_url("https://demogame.nova.netbird.nova.example"),
            Some("nova".to_string())
        );
        assert_eq!(
            site_from_url("https://demogame.zagros.netbird.example.net/demo"),
            Some("zagros".to_string())
        );
    }

    #[test]
    fn site_from_url_none_for_localhost() {
        assert_eq!(site_from_url("http://localhost:8095"), None);
        assert_eq!(site_from_url("http://127.0.0.1:8099/demo"), None);
    }
}
