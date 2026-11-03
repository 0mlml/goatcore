//! `--verbose`/`-v` — a diagnostic log on stderr, so `-o json|yaml` keeps stdout
//! a clean document a pipeline parses (goat's choice,
//! crates/goat-cli/src/main.rs). Off by default: the report on stdout and the
//! interpreted error on stderr are the whole surface a run shows.
//!
//! `-v` raises goatbuilder's own events to debug and keeps every dependency's
//! warnings; `RUST_LOG` overrides when `-v` is absent (`RUST_LOG=goatbuilder=trace`).
//! The events are the external commands a run shells out to — ansible, git, the
//! deploy drivers — and the network phase of `update`, which is what an operator
//! reaches for `-v` to see.

use std::ffi::OsStr;
use std::process::Command;

use tracing_subscriber::EnvFilter;

/// Install the stderr subscriber once. `verbose` makes `-v` a SUPERSET of the
/// default: `goatbuilder=debug` plus the bare `warn` fallback, so a dependency's
/// WARN is not filtered out (goat's filter shape, same reason).
pub fn init(verbose: bool) {
    let filter = if verbose {
        EnvFilter::new("goatbuilder=debug,warn")
    } else {
        EnvFilter::try_from_default_env().unwrap_or_else(|_| EnvFilter::new("warn"))
    };
    // `try_init` so a second call (tests, a nested run) is a no-op, not a panic.
    let _ = tracing_subscriber::fmt()
        .with_env_filter(filter)
        .with_writer(std::io::stderr)
        .with_target(false)
        .without_time()
        .try_init();
}

/// Log, at debug, an external command a run is about to spawn. A no-op unless
/// `-v` (or `RUST_LOG`) raised goatbuilder to debug, so the call is cheap to
/// sprinkle at every shell-out.
pub fn cmd(c: &Command) {
    tracing::debug!("exec: {}", render(c));
}

/// `program arg arg …`, each argument quoted only when it holds whitespace, so
/// the line can be copied back into a shell.
fn render(c: &Command) -> String {
    let mut out = quote(c.get_program());
    for a in c.get_args() {
        out.push(' ');
        out.push_str(&quote(a));
    }
    out
}

fn quote(s: &OsStr) -> String {
    let s = s.to_string_lossy();
    if s.is_empty() || s.contains(|ch: char| ch.is_whitespace()) {
        format!("'{s}'")
    } else {
        s.into_owned()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn renders_a_copy_pasteable_command_line() {
        let mut c = Command::new("ansible-playbook");
        c.args(["-i", "inventory/hosts.ini", "site.yml"]);
        assert_eq!(render(&c), "ansible-playbook -i inventory/hosts.ini site.yml");
    }

    #[test]
    fn quotes_an_argument_that_holds_a_space() {
        let mut c = Command::new("git");
        c.args(["commit", "-m", "a message"]);
        assert_eq!(render(&c), "git commit -m 'a message'");
    }

    #[test]
    fn init_is_idempotent() {
        init(false);
        init(true); // a second call must not panic
    }
}
