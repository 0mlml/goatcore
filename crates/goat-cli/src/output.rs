// Single source of truth for "should this output be colored / a TTY-only
// affordance / a CI-friendly format?" Read once in main.rs from CLI
// flags + env, then carried through `Ctx`. Per docs/design/goat-cli.md
// §Conventions adopted from modern-CLI research item 3.
//
// Inputs (highest-precedence first):
//   1. `--color always|never|auto` flag
//   2. `CLICOLOR_FORCE=1` env (force color even off-TTY)
//   3. `NO_COLOR` env (per https://no-color.org)
//   4. `is_tty(stdout)` for color default
//   5. `CI=true` env — separate from TTY because Jenkins has TTYs but
//      shouldn't get interactive spinners.
//
// Phase B uses this for the doctor verdict glyphs and the cargo-style
// error wrapper; later phases also reach for it for table rendering,
// progress, and confirmation prompts.

use std::io::IsTerminal;

#[derive(Debug, Clone, Copy, PartialEq, Eq, clap::ValueEnum)]
#[clap(rename_all = "lowercase")]
pub enum ColorChoice {
    Auto,
    Always,
    Never,
}

#[derive(Debug, Clone, Copy)]
#[allow(dead_code)] // Phase C+ consumers (table render, spinner, prompt) read these.
pub struct OutputContext {
    pub color: bool,
    pub stdout_is_tty: bool,
    pub stderr_is_tty: bool,
    pub ci: bool,
}

impl OutputContext {
    pub fn from_env(choice: ColorChoice) -> Self {
        let stdout_is_tty = std::io::stdout().is_terminal();
        let stderr_is_tty = std::io::stderr().is_terminal();
        let ci = std::env::var_os("CI").is_some_and(|v| v == "true" || v == "1");

        let no_color = std::env::var_os("NO_COLOR").is_some();
        let force = std::env::var_os("CLICOLOR_FORCE").is_some_and(|v| v != "0");

        let color = match choice {
            ColorChoice::Always => true,
            ColorChoice::Never => false,
            ColorChoice::Auto => {
                if force {
                    true
                } else if no_color {
                    false
                } else {
                    stdout_is_tty
                }
            }
        };

        Self {
            color,
            stdout_is_tty,
            stderr_is_tty,
            ci,
        }
    }

    /// `true` when human-affordances (spinners, prompts, ANSI escapes)
    /// are appropriate. Distinct from `self.color` because some
    /// affordances care about TTY but not color (e.g. a clear-line
    /// progress overwrite).
    #[allow(dead_code)] // Phase C+ consumers (table render, spinner, prompt).
    pub fn interactive(&self) -> bool {
        self.stdout_is_tty && !self.ci
    }
}

/// Await `fut`, animating a stderr spinner with an elapsed-seconds counter
/// while it's in flight, so a slow call reads as "working" not "hung". The
/// spinner is a pure affordance — it draws only when stderr is an interactive
/// TTY (skipped under `--json`-piped stderr redirection, CI, or a non-TTY),
/// so it never corrupts captured output. Gates on `stderr_is_tty` (not stdout)
/// because the payload goes to stdout and the progress belongs on stderr, so
/// `goat access strays > out.txt` still shows the spinner in the terminal.
///
/// The seconds counter is the load-bearing part: the portal's NetBird poll can
/// take 30–60s on a lossy mesh, and a static line looks like a wedge. Motion +
/// a rising counter is the honest signal that the call is progressing.
pub async fn with_spinner<F, T>(out: &OutputContext, label: &str, fut: F) -> T
where
    F: std::future::Future<Output = T>,
{
    use std::io::Write;

    if !out.stderr_is_tty || out.ci {
        return fut.await;
    }

    const FRAMES: [&str; 10] = ["⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"];
    tokio::pin!(fut);
    let mut ticker = tokio::time::interval(std::time::Duration::from_millis(100));
    let started = std::time::Instant::now();
    let mut frame = 0usize;

    let result = loop {
        tokio::select! {
            biased;
            r = &mut fut => break r,
            _ = ticker.tick() => {
                let mut err = std::io::stderr();
                let _ = write!(
                    err,
                    "\r{} {} ({}s)\x1b[K",
                    FRAMES[frame % FRAMES.len()],
                    label,
                    started.elapsed().as_secs()
                );
                let _ = err.flush();
                frame += 1;
            }
        }
    };

    // Clear the spinner line so it leaves no residue before the payload prints.
    let mut err = std::io::stderr();
    let _ = write!(err, "\r\x1b[K");
    let _ = err.flush();
    result
}

#[cfg(test)]
mod tests {
    use super::*;

    fn ctx(stderr_is_tty: bool, ci: bool) -> OutputContext {
        OutputContext {
            color: false,
            stdout_is_tty: false,
            stderr_is_tty,
            ci,
        }
    }

    #[tokio::test]
    async fn spinner_returns_future_value_off_tty() {
        // Non-TTY (the test harness): the future's value passes through
        // untouched, no spinner drawn.
        let out = ctx(false, false);
        let v = with_spinner(&out, "x", async { 41 + 1 }).await;
        assert_eq!(v, 42);
    }

    #[tokio::test]
    async fn spinner_returns_future_value_on_tty() {
        // TTY path exercises the select! loop + line-clear; a
        // ready-immediately future still yields its value (biased select
        // polls the future first, so no frame is even drawn here).
        let out = ctx(true, false);
        let v = with_spinner(&out, "x", async { "done" }).await;
        assert_eq!(v, "done");
    }

    #[tokio::test]
    async fn spinner_suppressed_in_ci_even_on_tty() {
        // CI with a TTY (Jenkins) must not get interactive frames; the
        // value still passes through.
        let out = ctx(true, true);
        let v = with_spinner(&out, "x", async { 7 }).await;
        assert_eq!(v, 7);
    }
}
