//! Surprise-and-delight affordances, gated by the Three Rules of goat
//! delight (docs/design/goat-cli-onboarding-and-delight.md):
//!   1. Never on the machine surface — nothing here touches `--json`,
//!      exit codes, completion candidates, or non-TTY/piped output.
//!   2. Opt-in or success-only — the only thing that appears unprompted
//!      is a flourish on FULL success, and only in pretty mode.
//!   3. One kill switch — `GOAT_NO_FUN=1` silences everything at once.
//!
//! This module owns the kill-switch + TTY guard. Each call site is still
//! responsible for its own success-only / pretty-only gating.

use std::io::IsTerminal;

/// Whether fun output is allowed right now: off when `GOAT_NO_FUN` is set,
/// or when stdout is not a terminal (piped / redirected / CI). Callers add
/// their own success-only / `!--json` gate on top.
pub fn enabled() -> bool {
    if std::env::var_os("GOAT_NO_FUN").is_some() {
        return false;
    }
    std::io::stdout().is_terminal()
}

/// The localized all-green flourish line for `goat doctor` / `goat status`
/// when every check passed. `None` when fun is suppressed, so the caller
/// can `if let Some(line) = fun::all_green_flourish() { println!(...) }`
/// without duplicating the guard. Pretty + success-only is the caller's
/// responsibility; this only applies the kill switch.
pub fn all_green_flourish() -> Option<String> {
    if !enabled() {
        return None;
    }
    Some(i18n_embed_fl::fl!(
        crate::i18n::LANGUAGE_LOADER,
        "flourish-all-green"
    ))
}

/// Emit an opt-out next-step `hint:` to STDERR (git-advice shaped) after a
/// successful action. Honors the kill switch + TTY guard like every other
/// flourish, and stderr-only so it never pollutes stdout or a `--json`
/// surface. A no-op when fun is suppressed.
pub fn hint(msg: &str) {
    if enabled() {
        eprintln!("hint: {msg}");
    }
}

/// HTTP 418 "I'm a teapot" (HTCPCP/1.0, RFC 2324, 1 April 1998) for the
/// hidden `goat brew` — with the obvious goat twist.
pub const TEAPOT_418: &str = r#"418 I'm a teapot.

  …except this is a goat, and goats brew neither coffee nor tea.
  (HTCPCP/1.0 — RFC 2324, 1 April 1998)
"#;

/// RFC 1925 — "The Twelve Networking Truths" (R. Callon, 1 April 1996).
/// A deep-cut easter egg for the hidden `goat truths`. Apt for a
/// brokerless data fabric: truth 11 is the broker we keep rejecting, and
/// truth 12 is goat's own "minimal" design principle. Reproduced (lightly
/// condensed) from the freely-distributable RFC.
pub const RFC_1925_TRUTHS: &str = r#"RFC 1925 — The Twelve Networking Truths (R. Callon, 1 April 1996)

  (1)  It Has To Work.
  (2)  No matter how hard you push and no matter what the priority,
       you can't increase the speed of light.
  (3)  With sufficient thrust, pigs fly just fine. However, this is
       not necessarily a good idea.
  (4)  Some things in life can never be fully appreciated nor
       understood unless experienced firsthand.
  (5)  It is always possible to agglutinate multiple separate problems
       into a single complex interdependent solution. In most cases
       this is a bad idea.
  (6)  It is easier to move a problem around than to solve it.
  (6a) It is always possible to add another level of indirection.
  (7)  It is always something.
  (7a) Good, Fast, Cheap: Pick any two (you can't have all three).
  (8)  It is more complicated than you think.
  (9)  For all resources, whatever it is, you need more.
  (10) One size never fits all.
  (11) Every old idea will be proposed again with a different name and
       a different presentation, regardless of whether it works.
  (12) In protocol design, perfection has been reached not when there
       is nothing left to add, but when there is nothing left to take away.
"#;

/// The canonical Colossal Cave Adventure (1976) response to the magic
/// word, for the hidden `goat xyzzy`.
pub const XYZZY: &str = "Nothing happens.";

/// A charming ASCII goat for the hidden `goat goat` easter egg. Printed
/// only when explicitly invoked, so it ignores the kill switch (the user
/// asked for it on purpose) — but stays out of `--json` by never being
/// wired to a machine surface.
pub const ASCII_GOAT: &str = r#"
        ___      _
       (o o)    | |   the data fabric,
      (  V  )   | |   in your hands.
     /--m-m----/ /
"#;
