// `goat init` — the first-run wizard (docs/design/goat-cli-onboarding-and-delight.md).
// gcloud-init / aws-configure shaped: detect state, pick sandbox or real,
// deliver an instant win, and point at the tutor. Deliberately thin glue
// over the pieces that already exist (`goat hello`, `goat profile init`,
// `goat doctor`) — the wizard orchestrates, it doesn't reimplement.
//
// TTY discipline: the one interactive choice is gated on a real terminal.
// On a non-TTY (pipe / CI) the wizard never blocks on stdin — it defaults
// to the sandbox (the safe, network-free path) and prints guidance.

use crate::cmd::hello::HelloArgs;
use clap::Args;
use std::io::{IsTerminal, Write};

#[derive(Args, Debug)]
pub struct InitArgs {
    /// Go straight to the sandbox (skip the prompt).
    #[arg(long)]
    pub sandbox: bool,

    /// Assume defaults; never prompt (sandbox unless a profile already
    /// exists). Useful for scripted first-runs.
    #[arg(long)]
    pub yes: bool,
}

pub async fn run(ctx: &super::Ctx, args: InitArgs) -> anyhow::Result<()> {
    let initialized = ctx.profile.is_initialized();
    let interactive = std::io::stdin().is_terminal() && std::io::stdout().is_terminal();

    println!("Welcome to goat — the operator command-line for the data fabric.");
    if initialized {
        println!(
            "A profile is already set up for context `{}`.",
            ctx.profile.context
        );
    }

    // Decide the mode. Explicit --sandbox wins; an existing profile means
    // a real env is available; otherwise offer the choice (interactive) or
    // default to the sandbox (non-interactive / --yes).
    let sandbox = if args.sandbox {
        true
    } else if initialized && !args.yes {
        // Has a real env. Offer to use it, default real.
        !prompt_is(
            interactive,
            "Use the sandbox instead of your real env? [y/N] ",
            false,
        )
    } else if initialized {
        false
    } else if interactive && !args.yes {
        // No profile yet — explore the sandbox now, or attach a real env?
        prompt_is(
            interactive,
            "No profile yet. Explore the sandbox now? [Y/n] (answer n to attach a real env) ",
            true,
        )
    } else {
        true
    };

    if !sandbox && !initialized {
        // Real chosen but no profile exists — we cannot issue identity;
        // guide to the bundle flow, then a re-run.
        println!();
        println!("To attach a real env you need an onboarding bundle (a `.cbor`):");
        println!(
            "  1. goat ca add <bundle-ca.pem>     # verify its fingerprint with your operator"
        );
        println!("  2. goat profile init <bundle.cbor>");
        println!("  3. goat doctor                  # diagnose setup and reachability");
        println!();
        println!("No bundle yet? Explore now with: goat init --sandbox");
        return Ok(());
    }

    // Deliver the instant win.
    println!();
    if sandbox {
        println!("Let's prove it works — a round-trip through the sandbox:");
        println!();
        crate::cmd::hello::run(
            ctx,
            HelloArgs {
                sandbox: true,
                json: false,
            },
        )
        .await?;
        println!();
        println!("That was simulated. When you have a bundle:");
        println!("  goat ca add <bundle-ca.pem>      # verify its fingerprint with your operator");
        println!("  goat profile init <bundle.cbor>  # attach a real env");
    } else {
        println!("You're on a real env. Verify and say hello:");
        println!("  goat doctor   # stepped reachability check");
        println!("  goat hello    # a live round-trip through the fabric");
    }

    println!();
    println!("Ready to learn it hands-on?  ->  goat tutor");
    Ok(())
}

/// Prompt for a yes/no on a TTY and return the boolean. On a non-TTY (or
/// EOF) returns `default` without blocking. The trailing word in the
/// prompt should make the default obvious (`[Y/n]` vs `[y/N]`).
fn prompt_is(interactive: bool, prompt: &str, default: bool) -> bool {
    if !interactive {
        return default;
    }
    print!("{prompt}");
    let _ = std::io::stdout().flush();
    let mut line = String::new();
    match std::io::stdin().read_line(&mut line) {
        Ok(0) | Err(_) => default, // EOF / error → default
        Ok(_) => match line.trim().to_ascii_lowercase().as_str() {
            "y" | "yes" => true,
            "n" | "no" => false,
            "" => default,
            _ => default,
        },
    }
}
