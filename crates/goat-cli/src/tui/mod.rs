// Opt-in, TTY-gated terminal inspection surface for goat-cli.
//
// Per the ADR 1019 addendum + docs/design/goat-cli-tui.md:
// the four web inspection surfaces collapse, in the terminal, to one
// picker+detail plus two question-lenses (Diagnose, Tail). Every lens
// here is a RENDERER — the verdict/analytics logic stays in the
// canonical services (stethoscope, flashlight). goat-cli consumes their
// stable HTTP/SSE contracts and draws the result.
//
// Phase B ships the Diagnose lens (`goat inspect --tui`). Phase C adds
// Tail (`goat sub --tui`); Phase D adds the merged picker. The
// line-oriented commands stay the default and the only non-TTY path —
// every surface in this module refuses to launch on a non-TTY by
// construction (see `require_tty`), so a pipe/CI invocation can never
// hang waiting for input.

pub mod diagnose;
pub mod picker;
pub mod tail;
pub mod top;
pub mod who;

use std::io::{stdout, IsTerminal};

/// Refuse to launch an interactive surface unless both stdin and stdout
/// are TTYs. Mirrors the `goat compose` gate (ADR 1019 §14); centralized
/// here so every lens shares one refusal message shape. `surface` is the
/// operator-facing command name (e.g. "goat inspect --tui").
pub(crate) fn require_tty(surface: &str) -> anyhow::Result<()> {
    if !stdout().is_terminal() || !std::io::stdin().is_terminal() {
        anyhow::bail!(
            "`{surface}` requires an interactive terminal. \
             Non-TTY invocations (pipes / CI) are refused by construction \
             so a script can't accidentally hang waiting for input. \
             See docs/design/goat-cli-tui.md."
        );
    }
    Ok(())
}

/// Silence stderr for the lifetime of a full-screen TUI so log output can't
/// bleed into the alternate screen and corrupt the render.
///
/// Diagnostic logs (ours via `tracing` → stderr, AND the zenoh transport
/// stack's `tracing`/`log` output that rides the same global subscriber —
/// e.g. the `WARN zenoh_link tls::utils: Skipping name verification of TLS
/// server` line) write to stderr regardless of what the alternate screen is
/// doing on stdout. While a ratatui screen owns the terminal those writes
/// land *on top of* the rendered frame — the visible corruption in goat-who
/// 0.4.7. We redirect stderr at the fd level (so it catches the whole stack,
/// not just our subscriber) to `/dev/null` for the screen's lifetime and
/// restore the original fd on drop — including on panic, since `Drop` runs
/// during unwind, so a crash backtrace still reaches the real terminal after
/// teardown.
///
/// Construct it AFTER `EnterAlternateScreen` and hold it until AFTER
/// `LeaveAlternateScreen`. No-op on non-unix (the TUI lenses aren't a Windows
/// path) and degrades to a no-op if the dup/redirect fails — never fatal.
pub(crate) struct StderrSilencer {
    #[cfg(unix)]
    saved_fd: Option<i32>,
}

impl StderrSilencer {
    pub(crate) fn engage() -> Self {
        #[cfg(unix)]
        {
            // SAFETY: plain libc fd plumbing — dup the current stderr, point
            // fd 2 at /dev/null, close the scratch fd. The saved fd is
            // restored in Drop. No Rust aliasing invariants are involved.
            unsafe {
                let saved = libc::dup(libc::STDERR_FILENO);
                if saved < 0 {
                    return Self { saved_fd: None };
                }
                let devnull = b"/dev/null\0";
                let nullfd = libc::open(devnull.as_ptr() as *const libc::c_char, libc::O_WRONLY);
                if nullfd < 0 {
                    libc::close(saved);
                    return Self { saved_fd: None };
                }
                let _ = libc::dup2(nullfd, libc::STDERR_FILENO);
                libc::close(nullfd);
                Self { saved_fd: Some(saved) }
            }
        }
        #[cfg(not(unix))]
        {
            Self {}
        }
    }
}

impl Drop for StderrSilencer {
    fn drop(&mut self) {
        #[cfg(unix)]
        if let Some(saved) = self.saved_fd.take() {
            // SAFETY: restore the saved stderr onto fd 2 and release the dup.
            unsafe {
                let _ = libc::dup2(saved, libc::STDERR_FILENO);
                libc::close(saved);
            }
        }
    }
}
