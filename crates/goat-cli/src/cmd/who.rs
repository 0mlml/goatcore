// `goat who` — the identity common operating picture (ADR 1047,
// docs/design/goat-who-identity-cop.md).
//
// The top(1) of *identities*: a searchable master-detail roster browser. Where
// `goat top` ranks producers by how loud they are, `goat who` answers "who is
// in this goatnet, what may each of them do, and what are they doing." The
// left pane is a filterable identity list (declared ⊕ live, default-sorted by
// NAME — searchable-first, not usage-ranked); the detail pane shows the
// selected identity's permissions (the Data-Access COP forward query) and its
// live activity (presence + footprint).
//
// PURE CONSUMER + READ-ONLY PORTAL QUERIES — no producer, no enforcement, no
// mint, no revoke. This module owns the command surface (args, TTY gate,
// session open, declared-roster load); the ratatui screen + the testable
// state-fold live in crate::tui::who.

use crate::profile::Profile;
use clap::Args;

/// Initial roster sort; `s` cycles it live. Default is LIVE — activity-first
/// (who is here and active floats to the top), reconciling the original
/// searchable-first framing with the live-review ask for a meaningful default.
/// It is NOT a msgs/s leaderboard (that is `Use`, and `goat top`); `/` search
/// stays instant in every mode, so the roster is still searchable-first.
#[derive(Clone, Copy, Debug, PartialEq, Eq, clap::ValueEnum)]
pub enum WhoSort {
    /// Alphabetical by rendered name — stable under traffic churn.
    Name,
    /// Live (presence-alive) identities first, then by name (default) — the
    /// activity-first glance: who's here right now.
    Live,
    /// By live usage (messages/second), descending — the `goat top` order, for
    /// when you want "who's busiest" without leaving the roster.
    Use,
}

/// One declared roster entry from the portal registry (`GET /api/orgs`): the
/// write `anchor` and its optional display name. The human alias rides the
/// shared `AliasRegistry`; this carries only what the registry doesn't.
#[derive(Clone, Debug)]
pub(crate) struct DeclaredActor {
    pub anchor: String,
    pub display_name: Option<String>,
}

#[derive(Args, Debug)]
pub struct WhoArgs {
    /// Initial roster sort; `s` cycles it live. Default LIVE (activity-first;
    /// `/` search stays instant, so still searchable-first).
    #[arg(long, value_enum, default_value = "live")]
    pub sort: WhoSort,

    /// Start with the roster filtered. Space-separated terms are AND-ed; a bare
    /// term is a case-insensitive substring of the name/anchor/display-name, and
    /// the facet prefixes `live:on|off` / `use:on|off` narrow by live/producing
    /// state. In the interactive screen `/` edits this live (htop idiom).
    #[arg(long, value_name = "QUERY")]
    pub filter: Option<String>,

    /// Filter the presence roll-up to one site scope (matches the `<scope>` of
    /// `ops/presence/health/<scope>`).
    #[arg(long, value_name = "SCOPE")]
    pub site: Option<String>,

    /// Batch snapshot (the `top -bn1` analog): collect live state for one
    /// bounded window, render ONE plain-text roster frame (name / live / usage /
    /// band / registry-membership) to stdout, then exit. Allowed on a non-TTY.
    /// Permissions are NOT included in `--once` (an N-call-per-identity fetch) —
    /// the scriptable single-identity capability path stays `goat access who`.
    #[arg(long)]
    pub once: bool,
}

pub async fn run(ctx: &super::Ctx, args: WhoArgs) -> anyhow::Result<()> {
    // Interactive full-screen TUI stays TTY-gated, and the gate is the
    // OUTERMOST frame — refuse a non-TTY before any portal read or session
    // open so a script can't hang (mirrors `goat ui`). `--once` is the
    // scriptable path and skips the gate.
    if !args.once {
        crate::tui::require_tty("goat who")?;
    }

    // Alias registry for rendering anchors as human handles (ADR 1039);
    // degrades to UUID display when the portal is absent/unreachable.
    let registry = crate::alias::load_aliases(&ctx.profile).await;
    // Throughput-envelope registry for the per-actor band (ADR 1045); degrades
    // to "no envelope" (honest `—`) when the portal is unreachable.
    let envelopes = crate::alias::load_envelopes(&ctx.profile).await;
    // Declared roster (the portal registry's write-anchored actors); degrades to
    // an empty roster (live-only) when the portal is unreachable.
    let declared = load_declared(&ctx.profile).await;

    if args.once {
        // Batch mode: NO permission fetch (an N-call-per-identity cost).
        // Read-only random ZID (a subscriber earns no provenance from a pinned
        // slot ZID and would monopolize the operator's session, cf. F-195).
        let session = super::pubsub::open_session_readonly(&ctx.profile).await?;
        return crate::tui::who::run_once(
            session,
            ctx.profile.router_endpoint_label(),
            registry,
            envelopes,
            declared,
            args.site.clone(),
            args.sort,
            args.filter.clone(),
        )
        .await;
    }

    // The portal client backs the lazy per-identity `access who` fetcher. None
    // (or disabled) → the detail pane honestly reads "portal unreachable" rather
    // than an empty allow-list.
    let portal = ctx
        .profile
        .portal_client()
        .ok()
        .filter(|c| c.is_enabled());

    // Read-only random ZID: a long-lived browser must not hold the operator's
    // pinned slot ZID, or concurrent goat commands hit MAX_LINKS.
    let session = super::pubsub::open_session_readonly(&ctx.profile).await?;
    crate::tui::who::run(
        session,
        ctx.profile.router_endpoint_label(),
        registry,
        envelopes,
        declared,
        portal,
        args.site.clone(),
        args.sort,
        args.filter.clone(),
    )
    .await
}

/// Load the declared roster from the portal registry (`GET /api/orgs`). On a
/// slot-pool goatnet each alias slot IS a registry actor, so `list_vendors`
/// alone is the declared anchor set; the human names ride the alias registry.
/// Degrades to an empty roster on any error — declared membership is an
/// enhancement over the live-observed set, never a hard dependency.
async fn load_declared(profile: &Profile) -> Vec<DeclaredActor> {
    let Ok(client) = profile.portal_client() else {
        return Vec::new();
    };
    match client.list_vendors().await {
        Ok(vendors) => vendors
            .into_iter()
            .map(|v| DeclaredActor {
                anchor: v.prefix,
                display_name: (!v.display_name.is_empty()).then_some(v.display_name),
            })
            .collect(),
        Err(_) => Vec::new(),
    }
}
