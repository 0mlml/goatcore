//! goat-fun — hidden delight shared by `goat` and `goatbuilder`, so both
//! binaries play the identical game.
//!
//! `bound` is a terminal endless runner in the spirit of the browser's
//! offline dinosaur: a mountain goat bounds along a cliff ledge, jumping rocks
//! and gaps, ducking a swooping eagle once the pace picks up, with an argan
//! tree drifting by on the horizon and the sky turning to night every 700
//! points.
//!
//! The rules it keeps (goat-cli-onboarding-and-delight.md): it runs only when
//! typed (a hidden verb, never launched by an error), it refuses with one line
//! when the terminal is not interactive or too small so a pipe or CI never
//! hangs, it writes no files and opens no connections, and it always gives
//! the terminal back — on quit, on a crash of the game, and on a panic.
//!
//! [`game`] is pure: the step function and the renderer take a state and
//! return one, with no terminal, so every rule of play is tested directly.
//! [`term`] is the thin crossterm loop around it.

pub mod game;
pub mod term;

/// The hidden `bound` verb for either binary. `prog` is the binary's name,
/// used only in the one-line refusal. Returns the process exit code: 0 always,
/// because typing an easter egg is never an error.
pub fn bound(prog: &str) -> i32 {
    match term::check_terminal() {
        Ok(()) => {
            if let Err(e) = term::play() {
                eprintln!("{prog} bound: the game stopped ({e}); your terminal has been restored.");
            }
            0
        }
        Err(why) => {
            eprintln!("{prog} bound needs an interactive terminal of at least {}x{} ({why}).", game::MIN_W, game::MIN_H);
            0
        }
    }
}
