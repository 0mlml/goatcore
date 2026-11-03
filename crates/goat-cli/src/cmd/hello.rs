// `goat hello` — the hello-world smoke (docs/design/goat-cli-onboarding-and-delight.md).
// docker-`hello-world` shaped: one command round-trips a greeting through
// the fabric and narrates every substrate hop it exercised, then points
// at the next step. Runs against the in-process sandbox (default when no
// profile is initialized) or a real env, via the `Substrate` seam.
//
// The payload is always `lo` — the first message ever sent over a network
// (ARPANET, 29 Oct 1969): the operators typed "LOGIN" and the link crashed
// after two letters. A fitting first word for a new operator's first
// round-trip.

use crate::substrate::{Mode, Substrate};
use clap::Args;

#[derive(Args, Debug)]
pub struct HelloArgs {
    /// Run against the in-process sandbox — no profile or mesh needed.
    /// Default when no profile is initialized.
    #[arg(long)]
    pub sandbox: bool,

    /// Emit one JSON proof object instead of the friendly narration
    /// (machine-clean: `mode` is `sandbox` or `real`).
    #[arg(long)]
    pub json: bool,
}

pub async fn run(ctx: &super::Ctx, args: HelloArgs) -> anyhow::Result<()> {
    let substrate = Substrate::select(args.sandbox, &ctx.profile)?;
    let who = sanitize(&ctx.profile.context);
    let result = substrate.hello(&who, &ctx.profile).await?;

    // Machine surface: one JSON object, nothing fun, no banner styling.
    if args.json {
        println!("{}", serde_json::to_string(&result).unwrap_or_default());
        return Ok(());
    }

    // The SANDBOX banner is a SAFETY marker, not a flourish — it always
    // shows in sandbox pretty mode, even under GOAT_NO_FUN.
    if substrate.mode() == Mode::Sandbox {
        println!("◇ SANDBOX — simulated fabric, no real mesh. Run `goat init` to attach a real env.");
    }

    println!(
        "goat hello — saying `lo` to the fabric as `{who}` on topic `{}`",
        result.topic
    );
    for h in &result.hops {
        println!("  [t+{:>4}ms] {:<10} {}", h.elapsed_ms, h.label, h.detail);
    }
    // The `lo` deep cut — pretty mode only.
    println!(
        "  └─ `lo`: the first message ever sent over a network (ARPANET, 29 Oct 1969) — \
         they typed \"LOGIN\" and the link crashed after two letters."
    );

    if result.round_trip {
        if crate::fun::enabled() {
            println!("🐐 round-trip complete — you're on the fabric.");
        } else {
            println!("round-trip complete.");
        }
    } else {
        println!("session is live, but the round-trip did not return — see the hop above.");
    }

    // Next-step hints (pretty only).
    println!("next: `goat tutor` to learn the fabric · `goat sub <key>` to watch live traffic.");
    Ok(())
}

/// Reduce a context name to a safe, short topic-segment id: lowercase
/// alphanumerics + hyphen, everything else collapsed to a hyphen.
fn sanitize(s: &str) -> String {
    let cleaned: String = s
        .chars()
        .map(|c| {
            if c.is_ascii_alphanumeric() {
                c.to_ascii_lowercase()
            } else {
                '-'
            }
        })
        .collect();
    let trimmed = cleaned.trim_matches('-');
    if trimmed.is_empty() {
        "operator".to_string()
    } else {
        trimmed.chars().take(32).collect()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn sanitize_keeps_safe_ids() {
        assert_eq!(sanitize("default"), "default");
        assert_eq!(sanitize("DRS-Radar_1"), "drs-radar-1");
        assert_eq!(sanitize("  "), "operator");
        assert_eq!(sanitize(""), "operator");
    }
}
