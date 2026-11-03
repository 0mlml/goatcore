// `goat presence` — who is alive on the fabric, right now, natively.
//
// Reads the liveliness presence layer (ADR 1040 gap 3 /
// docs/design/zenoh-capability-adoption.md): peers declare a liveliness
// token at `<write_root>/presence/<label>` (panoscope-core convention);
// this command queries the live token set one-shot, or `--watch`es
// join/leave edges as the router pushes them — the quiet-vs-dead
// distinction traffic-based inference can't make.
//
// Reads only; declares no token itself. Write roots that are 32-hex slot
// anchors render through the alias layer (ADR 1039) — `--uuid` forces raw,
// `--json` is always raw.

use clap::Args;
use panoscope_core::presence::{parse_presence_key, ALL_PRESENCE_KEYEXPR};
use std::io::IsTerminal;
use std::time::Duration;
use zenoh::sample::SampleKind;

/// One-shot liveliness query wait — token replies come straight from the
/// router's routing tables, so this is generous.
const QUERY_TIMEOUT: Duration = Duration::from_secs(5);

#[derive(Args, Debug)]
pub struct PresenceArgs {
    /// Key expression to scope the presence view (alias OK for the leading
    /// segment). Default: every presence token on the fabric.
    #[arg(long, value_name = "KEYEXPR")]
    pub key: Option<String>,

    /// Watch live: print the current set, then join (+) / leave (−) edges
    /// as they happen, until Ctrl-C.
    #[arg(long)]
    pub watch: bool,

    /// Force raw UUID anchors (no alias rendering).
    #[arg(long)]
    pub uuid: bool,

    /// Machine output (one-shot only): JSON array of live tokens. Raw keys,
    /// no aliases in machine paths (ADR 1039 invariant); the alias rides
    /// alongside as a display field.
    #[arg(long, conflicts_with = "watch")]
    pub json: bool,

    /// The subscriber-delivery line: count the live catch-up subscribers on
    /// a topic (alias OK) via their AdvancedSubscriber detection tokens at
    /// `<topic>/@adv/sub/…`. Plain subscribers don't declare one, so this is
    /// a floor, not a census.
    #[arg(long, value_name = "TOPIC", conflicts_with_all = ["key", "watch"])]
    pub subscribers_of: Option<String>,
}

pub async fn run(ctx: &super::Ctx, args: PresenceArgs) -> anyhow::Result<()> {
    let registry = crate::alias::load_aliases(&ctx.profile).await;

    // Resolve an alias in the requested key's leading segment (fail closed
    // on an unknown alias — never silently widen; ADR 1039 / F-177).
    let key_expr = match &args.key {
        Some(k) => crate::alias::resolve_key_volatile_aware(&registry, k)
            .map_err(|e| anyhow::anyhow!("cannot resolve `{k}`: {e}"))?,
        None => ALL_PRESENCE_KEYEXPR.to_string(),
    };

    let session = super::pubsub::open_session_for(&ctx.profile).await?;
    let result = if let Some(topic) = &args.subscribers_of {
        let resolved = crate::alias::resolve_key_volatile_aware(&registry, topic)
            .map_err(|e| anyhow::anyhow!("cannot resolve `{topic}`: {e}"))?;
        subscribers_of(&session, &resolved, args.json).await
    } else if args.watch {
        watch(&session, &key_expr, &registry, args.uuid).await
    } else {
        one_shot(&session, &key_expr, &registry, &args).await
    };
    let _ = session.close().await;
    result
}

async fn one_shot(
    session: &zenoh::Session,
    key_expr: &str,
    registry: &panoscope_core::AliasRegistry,
    args: &PresenceArgs,
) -> anyhow::Result<()> {
    let keys = panoscope_core::query_presence(session, key_expr, QUERY_TIMEOUT)
        .await
        .map_err(|e| anyhow::anyhow!("liveliness query on `{key_expr}`: {e}"))?;

    if args.json {
        let rows: Vec<serde_json::Value> = keys
            .iter()
            .map(|k| {
                let parsed = parse_presence_key(k);
                serde_json::json!({
                    "key": k,
                    "write_root": parsed.as_ref().map(|p| p.write_root.clone()),
                    "alias": parsed.as_ref().and_then(|p| registry.alias_of(&p.write_root)),
                    "label": parsed.as_ref().map(|p| p.label.clone()),
                    "alive": true,
                })
            })
            .collect();
        println!("{}", serde_json::to_string_pretty(&rows)?);
        return Ok(());
    }

    if keys.is_empty() {
        println!("no live presence tokens match `{key_expr}`.");
        println!("  ⓘ nothing declaring presence ≠ nothing running — peers predating the");
        println!("    presence rollout (or a router ACL without the liveliness grant) are");
        println!("    invisible here. `goat ui` still shows traffic-inferred liveness.");
        return Ok(());
    }

    println!("{} live peer(s):", keys.len());
    for k in &keys {
        println!("  {} {}", alive_glyph(true), render(registry, args.uuid, k));
    }
    Ok(())
}

async fn watch(
    session: &zenoh::Session,
    key_expr: &str,
    registry: &panoscope_core::AliasRegistry,
    uuid_mode: bool,
) -> anyhow::Result<()> {
    let subscriber = panoscope_core::declare_presence_watch(session, key_expr)
        .await
        .map_err(|e| anyhow::anyhow!("liveliness subscriber on `{key_expr}`: {e}"))?;
    println!("watching presence on `{key_expr}` (history replays the current set; Ctrl-C to stop)…\n");
    let mut recv_error: Option<String> = None;
    loop {
        tokio::select! {
            _ = tokio::signal::ctrl_c() => break,
            recv = subscriber.recv_async() => {
                let sample = match recv {
                    Ok(s) => s,
                    Err(e) => {
                        recv_error = Some(e.to_string());
                        break;
                    }
                };
                let key = sample.key_expr().to_string();
                let alive = sample.kind() == SampleKind::Put;
                println!("  {} {}", alive_glyph(alive), render(registry, uuid_mode, &key));
            }
        }
    }
    if let Some(e) = recv_error {
        anyhow::bail!("presence watch ended (connection lost, not Ctrl-C) — {e}");
    }
    Ok(())
}

/// The subscriber-delivery line ("N subscribers live on this key"):
/// zenoh-ext AdvancedSubscribers with subscriber-detection (the
/// panoscope-core CatchupSubscriber declares it unconditionally) hold a
/// liveliness token at `<key>/@adv/sub/<zid>/<eid>` — query it.
async fn subscribers_of(
    session: &zenoh::Session,
    topic: &str,
    json: bool,
) -> anyhow::Result<()> {
    let probe = format!("{topic}/@adv/sub/**");
    let keys = panoscope_core::query_presence(session, &probe, QUERY_TIMEOUT)
        .await
        .map_err(|e| anyhow::anyhow!("liveliness query on `{probe}`: {e}"))?;
    if json {
        println!(
            "{}",
            serde_json::to_string_pretty(&serde_json::json!({
                "topic": topic,
                "detected_subscribers": keys.len(),
                "tokens": keys,
            }))?
        );
        return Ok(());
    }
    println!("{} catch-up subscriber(s) live on `{topic}`.", keys.len());
    if keys.is_empty() {
        println!("  ⓘ a floor, not a census — plain subscribers declare no detection token;");
        println!("    inspection surfaces on the catch-up wrapper (flashlight) do.");
    }
    Ok(())
}

/// Render one token key for humans: alias-rendered write root + label, with
/// the raw key form preserved when it doesn't parse as a presence key.
fn render(registry: &panoscope_core::AliasRegistry, uuid_mode: bool, key: &str) -> String {
    match parse_presence_key(key) {
        Some(id) => {
            let root = if uuid_mode {
                id.write_root.clone()
            } else {
                registry
                    .alias_of(&id.write_root)
                    .unwrap_or_else(|| id.write_root.clone())
            };
            format!("{root} / {}", id.label)
        }
        None => key.to_string(),
    }
}

/// Join/alive vs leave glyph, dimmed-ASCII safe off-TTY.
fn alive_glyph(alive: bool) -> &'static str {
    if std::io::stdout().is_terminal() {
        if alive { "\x1b[32m●\x1b[0m" } else { "\x1b[31m○\x1b[0m" }
    } else if alive {
        "+"
    } else {
        "-"
    }
}
