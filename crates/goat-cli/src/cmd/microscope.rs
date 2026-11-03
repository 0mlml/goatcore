// `goat microscope <selector>` — the per-entity behavior lens (ADR 1090,
// Phase B). Delegation guardrail (ADR 1019): this command RENDERS what the
// microscope service computed — spine, capability, merged timeline, per-
// source status — and never re-runs the joins itself. Plain-text one-frame
// output by default (scriptable, ticket-pasteable); `--json` is the stable
// machine contract. An interactive TUI lens is a deliberate later scope
// decision (goat-cli-tui.md).

use anyhow::{bail, Context as _};
use clap::Args;

#[derive(Args, Debug)]
pub struct MicroscopeArgs {
    /// The entity: email | handle | write-anchor UUID | slot alias.
    pub selector: String,

    /// Microscope service origin (default: $GOAT_MICROSCOPE_URL).
    #[arg(long, value_name = "URL")]
    pub service: Option<String>,

    /// Raw view-model JSON (stable machine contract).
    #[arg(long)]
    pub json: bool,

    /// Max timeline rows in the plain-text frame (JSON is never truncated).
    #[arg(long, default_value_t = 40)]
    pub limit: usize,
}

pub async fn run(ctx: &super::Ctx, args: MicroscopeArgs) -> anyhow::Result<()> {
    // Service URL precedence: --service > GOAT_MICROSCOPE_URL > derived from the
    // profile's portal endpoint (portal.* → microscope.*, ADR 1090).
    let base = args
        .service
        .clone()
        .or_else(|| std::env::var("GOAT_MICROSCOPE_URL").ok())
        .or_else(|| {
            ctx.profile
                .profile_field("portal_endpoint")
                .and_then(|p| crate::narrator::derive_microscope_from_portal(&p))
        })
        .unwrap_or_default();
    if base.is_empty() {
        bail!(
            "no microscope service configured — pass --service <url>, set GOAT_MICROSCOPE_URL, \
             or run in an initialized profile whose portal_endpoint follows the \
             portal.<site>.netbird.<zone> convention (see docs/design/microscope.md)"
        );
    }
    // CLI-through-fence auth (ADR 1090): present the operator's portal key as a
    // bearer. A Traefik route sends bearer requests past oauth2-proxy straight
    // to the app, which validates the key against the portal. Without a key the
    // request would hit the browser login wall and fail — so require one.
    let token = ctx.profile.portal_token().ok_or_else(|| {
        anyhow::anyhow!(
            "no operator portal key in the active profile — microscope's CLI path \
             authenticates with it (run `goat profile init <bundle.cbor>` first)"
        )
    })?;
    let url = format!(
        "{}/api/entity?id={}",
        base.trim_end_matches('/'),
        urlencoding_encode(&args.selector)
    );
    let resp = reqwest::Client::new()
        .get(&url)
        .bearer_auth(&token)
        .send()
        .await
        .with_context(|| format!("microscope service unreachable at {base}"))?;
    let status = resp.status();

    // F-207 fold (ADR 1090): examining an entity is itself an operator action.
    // Emit an age-encrypted OperatorActionV1 onto ops/audit/operator/<actor> —
    // the same encrypted, `goat audit ops`-reviewable stream every other admin
    // action lands in, so a CLI look is watched exactly like a mutation. The
    // microscope service also keeps its own local look-log; this is the durable
    // encrypted copy. Best-effort by contract — never blocks the query.
    {
        let actor = crate::cmd::audit::actor_handle(&ctx.profile);
        let outcome = if status.is_success() {
            "ok"
        } else if status == reqwest::StatusCode::FORBIDDEN {
            "refused"
        } else {
            "failed"
        };
        crate::cmd::audit::emit(
            &ctx.profile,
            crate::cmd::audit::OperatorActionV1::new(
                &actor,
                "microscope",
                "microscope.look",
                &args.selector,
                ctx.profile.context.clone(),
                outcome,
            )
            .with_args_digest(&format!("microscope.look selector={}", args.selector)),
        )
        .await;
    }

    if status == reqwest::StatusCode::FORBIDDEN {
        bail!(
            "microscope refused this operator key (403) — the key must resolve as an \
             operator at the portal; identity-admin tier is required"
        );
    }
    if !status.is_success() {
        bail!("microscope returned {status}");
    }
    let view: serde_json::Value = resp.json().await.context("bad microscope response")?;

    if args.json {
        println!("{}", serde_json::to_string_pretty(&view)?);
        return Ok(());
    }
    render_plain(&view, args.limit);
    Ok(())
}

fn render_plain(view: &serde_json::Value, limit: usize) {
    let spine = &view["spine"];
    println!("entity  {}", spine["selector"].as_str().unwrap_or("?"));
    for (label, key) in [("handle", "handle"), ("email", "email"), ("ory", "ory_id")] {
        if let Some(v) = spine[key].as_str() {
            println!("  {label:<8}{v}");
        }
    }
    for (label, key) in [("anchors", "anchors"), ("aliases", "aliases"), ("peers", "netbird_peers")] {
        if let Some(a) = spine[key].as_array() {
            if !a.is_empty() {
                let vals: Vec<&str> = a.iter().filter_map(|v| v.as_str()).collect();
                println!("  {label:<8}{}", vals.join(", "));
            }
        }
    }
    if let Some(groups) = view["capability"]["ory_groups"].as_array() {
        let vals: Vec<&str> = groups.iter().filter_map(|v| v.as_str()).collect();
        println!("  groups  {}", if vals.is_empty() { "(none)".into() } else { vals.join(", ") });
    }
    if let Some(unres) = spine["unresolved"].as_array() {
        for u in unres {
            println!("  join!   {}", u.as_str().unwrap_or("?"));
        }
    }

    println!("\nsources");
    if let Some(sources) = view["sources"].as_array() {
        for s in sources {
            println!(
                "  {:<16} {:<8} n={:<5} {}",
                s["source"].as_str().unwrap_or("?"),
                s["state"].as_str().unwrap_or("?"),
                s["n"].as_u64().unwrap_or(0),
                s["detail"].as_str().unwrap_or("")
            );
        }
    }
    if let Some(silent) = view["silent_layers"].as_array() {
        let vals: Vec<&str> = silent.iter().filter_map(|v| v.as_str()).collect();
        if !vals.is_empty() {
            println!("  silent layers (no denial records exist): {}", vals.join(", "));
        }
    }

    println!("\ntimeline (newest first)");
    let rows = view["timeline"].as_array().cloned().unwrap_or_default();
    if rows.is_empty() {
        println!("  — no attributed events from the ok sources above —");
        return;
    }
    for e in rows.iter().take(limit) {
        let attr = match e["attribution"].as_str() {
            Some("strong") | None => "",
            Some("convention") => " [convention join]",
            Some(other) => {
                if other == "coarse" {
                    " [coarse actor]"
                } else {
                    ""
                }
            }
        };
        println!(
            "  {}  {:<16} {}{}",
            e["ts"].as_str().unwrap_or("?"),
            e["source"].as_str().unwrap_or("?"),
            e["summary"].as_str().unwrap_or("?"),
            attr
        );
    }
    if rows.len() > limit {
        println!("  … {} more (raise --limit or use --json)", rows.len() - limit);
    }
}

fn urlencoding_encode(s: &str) -> String {
    let mut out = String::with_capacity(s.len());
    for b in s.bytes() {
        match b {
            b'A'..=b'Z' | b'a'..=b'z' | b'0'..=b'9' | b'-' | b'_' | b'.' | b'~' => {
                out.push(b as char)
            }
            _ => out.push_str(&format!("%{b:02X}")),
        }
    }
    out
}
