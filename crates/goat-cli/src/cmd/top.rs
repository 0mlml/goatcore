// `goat top` — the live fabric dashboard (ADR 1044, docs/design/goat-top.md).
//
// The top(1) of the data fabric: one auto-refreshing terminal pane that
// composes every environment-health domain (mesh/fabric/time/presence)
// plus the per-producer footprints into a single screen, with a sortable
// producer "top" table as the focus. It is a PURE CONSUMER — it
// subscribes the five health/footprint topics already on the bus, keeps
// a latest-wins live model, and renders. No producers, no enforcement.
//
// This module owns the command surface (args, TTY gate, session open);
// the ratatui screen + the testable state-fold live in crate::tui::top.

use clap::Args;

/// Initial sort order for the producer table; `s` cycles it live.
#[derive(Clone, Copy, Debug, PartialEq, Eq, clap::ValueEnum)]
pub enum TopSort {
    /// By messages/second (default) — the rate that drives fabric pressure.
    Msgs,
    /// By bytes/second.
    Bytes,
    /// By peer name (alias-rendered, then UUID).
    Peer,
}

#[derive(Args, Debug)]
pub struct TopArgs {
    /// Redraw / channel-drain cadence, in seconds.
    #[arg(long, default_value_t = 2, value_name = "SECS")]
    pub interval: u64,

    /// Filter readings to one site scope (when multiple sites publish on
    /// the same fabric). Matches the `<scope>` of `ops/<domain>/health/<scope>`.
    #[arg(long, value_name = "SCOPE")]
    pub site: Option<String>,

    /// Initial producer-table sort; `s` cycles it live.
    #[arg(long, value_enum, default_value = "msgs")]
    pub sort: TopSort,

    /// Start with the producer table filtered to actors whose alias or anchor
    /// contains this substring (case-insensitive). In the interactive screen
    /// `/` edits the filter live (htop idiom); for `--once` it's the only way
    /// to narrow the scripted snapshot.
    #[arg(long, value_name = "SUBSTR")]
    pub filter: Option<String>,

    /// Batch snapshot (the `top -bn1` analog): collect beats for one bounded
    /// window, render ONE frame as plain text to stdout, then exit. Allowed on
    /// a non-TTY — the scriptable / CI counterpart to the interactive screen.
    #[arg(long)]
    pub once: bool,
}

pub async fn run(ctx: &super::Ctx, args: TopArgs) -> anyhow::Result<()> {
    // Interval floor: a 0s cadence would busy-spin the draw loop / give the
    // --once collector no window.
    let interval = args.interval.max(1);

    // Alias registry for rendering peer anchors as human handles (ADR 1039);
    // degrades to UUID display when the portal is absent/unreachable.
    let registry = crate::alias::load_aliases(&ctx.profile).await;
    // Throughput-envelope registry for the per-producer band (ADR 1045);
    // degrades to "no envelope" (honest `—`) when the portal is unreachable.
    let envelopes = crate::alias::load_envelopes(&ctx.profile).await;

    if args.once {
        // Batch mode is the scriptable path — NO TTY gate (it must run under a
        // pipe / CI). Collect one window, render a single plain-text frame.
        // Read-only (random ZID): pinning the slot ZID earns no provenance for a
        // subscriber and would monopolize the operator's single session.
        let session = super::pubsub::open_session_readonly(&ctx.profile).await?;
        return crate::tui::top::run_once(
            session,
            ctx.profile.router_endpoint_label(),
            registry,
            envelopes,
            args.site.clone(),
            args.sort,
            interval,
            args.filter.clone(),
        )
        .await;
    }

    // Interactive full-screen TUI stays TTY-gated: refuse a non-TTY BEFORE any
    // session open so a script can't hang waiting for input.
    crate::tui::require_tty("goat top")?;

    // Read-only (random ZID): a long-lived dashboard must not hold the
    // operator's pinned slot ZID, or concurrent goat commands hit MAX_LINKS
    //. Attribution/authz ride the cert CN, not the ZID.
    let session = super::pubsub::open_session_readonly(&ctx.profile).await?;
    // `crate::tui::top::run` closes/aborts its own producer task + session.
    crate::tui::top::run(
        session,
        ctx.profile.router_endpoint_label(),
        registry,
        envelopes,
        args.site.clone(),
        args.sort,
        interval,
        args.filter.clone(),
    )
    .await
}
