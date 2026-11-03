// `goat tutor` — vimtutor for the fabric (docs/design/goat-cli-onboarding-and-delight.md).
// A self-paced, hands-on tour: each lesson explains a piece of the fabric,
// shows the real `goat` command for it, and — where it's network-free —
// runs a live sandbox demo so the learner sees real output. Progress is
// saved per context and resumable.
//
// TTY discipline: the interactive walkthrough blocks on Enter between
// lessons, so it requires a terminal. On a non-TTY (pipe / CI) `goat tutor`
// never blocks — it prints the lesson outline and exits. `--list` works
// anywhere; `--reset` clears saved progress.

use clap::Args;
use std::io::{IsTerminal, Write};

#[derive(Args, Debug)]
pub struct TutorArgs {
    /// List the lessons and exit.
    #[arg(long)]
    pub list: bool,

    /// Forget saved progress and start from the first lesson.
    #[arg(long)]
    pub reset: bool,
}

/// One lesson. `demo` is `true` when the example is network-free and safe
/// to run live in the sandbox during the walkthrough.
struct Lesson {
    title: &'static str,
    body: &'static str,
    try_cmd: &'static str,
    demo: bool,
}

const LESSONS: &[Lesson] = &[
    Lesson {
        title: "Orientation — what the fabric is",
        body: "goat is the operator CLI for a data fabric: a self-healing mesh (WireGuard \
P2P) that carries identity-gated pub/sub over Zenoh. You publish to a topic; \
anyone allowed to read it receives your data — no central broker. Let's prove \
the round-trip in the sandbox.",
        try_cmd: "goat hello --sandbox",
        demo: true,
    },
    Lesson {
        title: "Identity & profile",
        body: "Your identity lives in a profile (an mTLS cert + a portal token + the router \
endpoint), keyed by a context. The sandbox needs none of this; a real env is \
attached with `goat profile init <bundle.cbor>`.",
        try_cmd: "goat profile show",
        demo: false,
    },
    Lesson {
        title: "Reachability — goat doctor",
        body: "Before you trust the fabric, prove you can reach it. `goat doctor` runs a \
stepped probe: ZTOL membership -> Zenoh router -> cert acceptance -> portal. \
Each failed step prints a specific remediation hint.",
        try_cmd: "goat doctor",
        demo: false,
    },
    Lesson {
        title: "Publish & subscribe",
        body: "The everyday verbs. `goat pub <key> <payload>` writes to a topic; \
`goat sub <key>` watches it live. Keys are slash-paths like \
`acme/radar1/tracks`. `goat hello` you just ran is a pub+sub round-trip under \
the hood.",
        try_cmd: "goat sub 'acme/**'",
        demo: false,
    },
    Lesson {
        title: "Compose — build a command with live substrate context",
        body: "`--help` knows the grammar; `goat compose` also knows this goatnet. It opens an \
interactive builder where, as you type, the suggestions fill with the fabric's real state \
at your cursor: the prefixes you can publish under, the topic keys already declared, the \
formats in use — and a warning if the key you're typing would collide or land somewhere \
you can't write. Reach for it over flat `--help` when you want that live context, not just \
the flag list — building a `topic create`, or a command you'll paste into a runbook (Enter \
emits the line to stdout; it doesn't execute). It needs a terminal, so this one's hands-on: \
launch it, explore with the arrow keys and Tab, then Esc to come back here.",
        try_cmd: "goat compose topic create",
        demo: false,
    },
    Lesson {
        title: "Diagnose — goat inspect",
        body: "When something looks wrong, `goat inspect <target>` delegates a diagnostic \
to stethoscope and renders each finding + remedy. Targets are \
`vendor=<prefix>`, `topic=<key>`, or `system=<name>`.",
        try_cmd: "goat inspect topic=acme/radar1/tracks",
        demo: false,
    },
    Lesson {
        title: "Cascade — the --narrate flag",
        body: "Add `--narrate` to a mutating command and goat narrates the substrate \
consequences it sets off — the portal accept, the fabric route, the \
post-mutation diagnostic — so you see the ripple, not just the ack.",
        try_cmd: "goat pub acme/radar1/tracks '{\"hello\":1}' --narrate",
        demo: false,
    },
    Lesson {
        title: "Access — who can read what",
        body: "Read access is broad by design; write integrity is the cut. The \
data-access common operating picture shows who is allowed where. Explore it \
with the access surface when you're on a real env.",
        try_cmd: "goat access --help",
        demo: false,
    },
    Lesson {
        title: "Sealed data — encryption that carries its own read cut",
        body: "Read access is broad, but some streams need the read cut to travel WITH the \
data. `goat pub --sealed <key> <payload>` encrypts to the release-set's granted \
audience; `goat sub --open <key>` opens what you hold a key for and prints DENIED \
for what you don't — the cut is the crypto, not the router. goat is a CONVENIENCE \
consumer of the grant-crypto SDK here: the same SDK seals/opens from any language, \
so this isn't the only path, just the operator one.",
        try_cmd: "goat pub --sealed --help",
        demo: false,
    },
    Lesson {
        title: "Rotate your key — forward secrecy on your terms",
        body: "Sealed streams are only as private as your leaf key. `goat access key rotate` \
retires this member's grant-encryption key: `--destroy` (default) drops the old key \
so a future theft reveals nothing sealed to it — full forward secrecy; `--keep-access` \
re-wraps your still-live DEKs to the new key, trading some of that secrecy to keep \
reading old generations. It emits the new key for you to install via the CA-coupled \
leaf re-issue — distinct from `goat admin rotate` (org cert reissue).",
        try_cmd: "goat access key rotate --help",
        demo: false,
    },
    Lesson {
        title: "Wrap — from sandbox to the real thing",
        body: "That's the tour. Everything here works the same against a real env. When you \
have an onboarding bundle: `goat profile init <bundle.cbor>`, then \
`goat init` to pick it up and `goat doctor` to confirm. Qapla'.",
        try_cmd: "goat init",
        demo: false,
    },
];

pub async fn run(ctx: &super::Ctx, args: TutorArgs) -> anyhow::Result<()> {
    let progress_path = ctx.profile.context_dir().join("tutor-progress.json");

    if args.reset {
        // NotFound means it was already at the reset state — that's success,
        // not a failure to report. Any other error (permission denied, a
        // directory in the way) used to be silently discarded while still
        // unconditionally claiming "reset." — a false claim of success.
        match std::fs::remove_file(&progress_path) {
            Ok(()) => println!("tutor progress reset."),
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => {
                println!("tutor progress reset. (was already at the start)")
            }
            Err(e) => {
                anyhow::bail!("could not reset tutor progress at {}: {e}", progress_path.display());
            }
        }
        if args.list {
            // fall through to listing
        } else {
            return Ok(());
        }
    }

    if args.list {
        println!("goat tutor — {} lessons:", LESSONS.len());
        for (i, l) in LESSONS.iter().enumerate() {
            println!("  {:>2}. {}", i + 1, l.title);
        }
        return Ok(());
    }

    let interactive = std::io::stdin().is_terminal() && std::io::stdout().is_terminal();
    if !interactive {
        // Never block on a non-TTY — show the outline and how to start.
        println!("goat tutor — a hands-on tour of the fabric ({} lessons).", LESSONS.len());
        println!("Run it on a terminal to step through interactively:");
        for (i, l) in LESSONS.iter().enumerate() {
            println!("  {:>2}. {}", i + 1, l.title);
        }
        println!();
        println!("Start:  goat tutor      (resumable · `--reset` to start over)");
        return Ok(());
    }

    let mut idx = load_progress(&progress_path);
    if idx >= LESSONS.len() {
        println!("You've finished the tutor. Starting over — `goat tutor --reset` to clear progress.");
        idx = 0;
    }

    println!("goat tutor — {} lessons. [Enter] advances · [q] quits (progress is saved).", LESSONS.len());
    while idx < LESSONS.len() {
        let l = &LESSONS[idx];
        println!();
        println!("── Lesson {}/{}: {} ──", idx + 1, LESSONS.len(), l.title);
        println!("{}", wrap_body(l.body));
        println!();
        println!("  try:  {}", l.try_cmd);

        if l.demo {
            println!();
            println!("  (running it now in the sandbox)");
            println!();
            crate::cmd::hello::run(ctx, crate::cmd::hello::HelloArgs { sandbox: true, json: false })
                .await?;
        }

        // Save progress as "next lesson" before waiting, so a quit resumes
        // on the lesson after the one just read.
        save_progress(&progress_path, idx + 1);

        if !wait_enter_or_quit() {
            println!("Paused at lesson {}. Resume any time with `goat tutor`.", idx + 2);
            return Ok(());
        }
        idx += 1;
    }

    println!();
    if crate::fun::enabled() {
        println!("🐐 Tour complete — you speak fabric now. `goat init` when you're ready for a real env.");
    } else {
        println!("Tour complete. `goat init` when you're ready for a real env.");
    }
    Ok(())
}

/// Block for Enter; return false if the learner asked to quit (`q`).
fn wait_enter_or_quit() -> bool {
    print!("  [Enter] next · [q] quit: ");
    let _ = std::io::stdout().flush();
    let mut line = String::new();
    match std::io::stdin().read_line(&mut line) {
        Ok(0) | Err(_) => false, // EOF mid-session → treat as quit
        Ok(_) => !matches!(line.trim().to_ascii_lowercase().as_str(), "q" | "quit"),
    }
}

fn load_progress(path: &std::path::Path) -> usize {
    std::fs::read_to_string(path)
        .ok()
        .and_then(|s| serde_json::from_str::<serde_json::Value>(&s).ok())
        .and_then(|v| v.get("next").and_then(|n| n.as_u64()))
        .map(|n| n as usize)
        .unwrap_or(0)
}

fn save_progress(path: &std::path::Path, next: usize) {
    if let Some(parent) = path.parent() {
        let _ = std::fs::create_dir_all(parent);
    }
    let body = serde_json::json!({ "next": next });
    // A write failure here silently made the tour's own "resume any time"
    // promise false — visible now, still non-fatal (the tour itself
    // shouldn't die over a progress-file write).
    if let Err(e) = std::fs::write(path, serde_json::to_string(&body).unwrap_or_default()) {
        eprintln!("⚠ could not save tutor progress to {}: {e} — resuming later may restart the tour", path.display());
    }
}

/// Soft-wrap a lesson body to ~76 columns with a two-space indent, so the
/// prose reads as a tidy paragraph regardless of how the literal is
/// line-broken in source.
fn wrap_body(body: &str) -> String {
    let words = body.split_whitespace();
    let mut out = String::from("  ");
    let mut col = 2;
    for w in words {
        if col + 1 + w.len() > 78 && col > 2 {
            out.push_str("\n  ");
            col = 2;
        } else if col > 2 {
            out.push(' ');
            col += 1;
        }
        out.push_str(w);
        col += w.len();
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn every_lesson_has_a_title_and_command() {
        assert!(LESSONS.len() >= 6);
        for l in LESSONS {
            assert!(!l.title.is_empty());
            assert!(l.try_cmd.starts_with("goat "));
        }
    }

    #[test]
    fn progress_round_trips() {
        let dir = std::env::temp_dir().join("goat-tutor-test-progress");
        let _ = std::fs::remove_dir_all(&dir);
        let path = dir.join("tutor-progress.json");
        assert_eq!(load_progress(&path), 0); // missing → 0
        save_progress(&path, 3);
        assert_eq!(load_progress(&path), 3);
        let _ = std::fs::remove_dir_all(&dir);
    }

    #[test]
    fn wrap_body_indents_and_breaks() {
        let w = wrap_body("alpha beta gamma");
        assert!(w.starts_with("  alpha"));
    }
}
