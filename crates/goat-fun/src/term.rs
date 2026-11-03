//! The terminal half of `bound`: a fixed-timestep loop around [`crate::game`],
//! drawn with crossterm (the backend goat-cli's TUI lenses already use).
//!
//! The terminal is always given back. A guard restores raw mode, the cursor,
//! colours and the main screen when the loop returns for any reason, and a
//! panic hook does the same before the panic message prints, so even a bug in
//! the game cannot leave an operator's shell in raw mode.

use std::io::{IsTerminal, Write};
use std::sync::atomic::{AtomicBool, Ordering};
use std::time::{Duration, Instant};

use crossterm::cursor::{Hide, MoveTo, Show};
use crossterm::event::{self, Event, KeyCode, KeyEvent, KeyEventKind, KeyModifiers};
use crossterm::style::{Attribute, Print, ResetColor, SetAttribute};
use crossterm::terminal::{self, EnterAlternateScreen, LeaveAlternateScreen};
use crossterm::{execute, queue};

use crate::game::{self, Input, State, DT, MIN_H, MIN_W};

/// Ok when both ends are an interactive terminal at least the field's size;
/// otherwise the reason, for the one-line refusal.
pub fn check_terminal() -> Result<(), String> {
    if !std::io::stdout().is_terminal() || !std::io::stdin().is_terminal() {
        return Err("this one is piped or redirected".into());
    }
    match terminal::size() {
        Ok((w, h)) if w >= MIN_W && h >= MIN_H => Ok(()),
        Ok((w, h)) => Err(format!("this one is {w}x{h}")),
        Err(e) => Err(format!("its size is unreadable: {e}")),
    }
}

static RESTORED: AtomicBool = AtomicBool::new(true);

/// Give the terminal back: leave raw mode and the alternate screen, show the
/// cursor, reset colours. Safe to call more than once.
pub fn restore() {
    if RESTORED.swap(true, Ordering::SeqCst) {
        return;
    }
    let mut out = std::io::stdout();
    let _ = execute!(out, ResetColor, SetAttribute(Attribute::Reset), Show, LeaveAlternateScreen);
    let _ = terminal::disable_raw_mode();
}

/// Run `restore` before any panic message, then the previous hook. Generic so
/// the chaining is testable without a terminal.
pub fn install_panic_hook<F: Fn() + Send + Sync + 'static>(restore: F) {
    let previous = std::panic::take_hook();
    std::panic::set_hook(Box::new(move |info| {
        restore();
        previous(info);
    }));
}

struct Guard;

impl Drop for Guard {
    fn drop(&mut self) {
        restore();
    }
}

fn input_for(key: KeyEvent) -> Option<Input> {
    if key.kind == KeyEventKind::Release {
        return None;
    }
    match key.code {
        KeyCode::Char(' ') | KeyCode::Up | KeyCode::Char('w') | KeyCode::Char('k') => Some(Input::Jump),
        KeyCode::Down | KeyCode::Char('s') | KeyCode::Char('j') => Some(Input::Duck),
        _ => Some(Input::None),
    }
}

fn is_quit(key: &KeyEvent) -> bool {
    matches!(key.code, KeyCode::Char('q') | KeyCode::Esc)
        || (key.code == KeyCode::Char('c') && key.modifiers.contains(KeyModifiers::CONTROL))
}

fn seed() -> u64 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| d.as_nanos() as u64)
        .unwrap_or(0x9e37_79b9_7f4a_7c15)
}

/// Play until the player quits.
pub fn play() -> std::io::Result<()> {
    let no_color = std::env::var_os("NO_COLOR").is_some();
    let mut out = std::io::stdout();

    install_panic_hook(restore);
    RESTORED.store(false, Ordering::SeqCst);
    let _guard = Guard;
    terminal::enable_raw_mode()?;
    execute!(out, EnterAlternateScreen, Hide)?;

    let (mut w, mut h) = terminal::size()?;
    let mut s = State::new(seed(), w.min(120));
    let frame = Duration::from_secs_f32(DT);
    let mut next = Instant::now();
    let mut crashed_at: Option<Instant> = None;

    loop {
        // Input: drain what arrived, waiting no longer than the next frame.
        // After a crash, wait longer: nothing moves, so nothing to draw.
        let wait = if s.alive { next.saturating_duration_since(Instant::now()) } else { Duration::from_millis(500) };
        let mut input = Input::None;
        let mut pressed = false;
        if event::poll(wait)? {
            loop {
                match event::read()? {
                    Event::Key(k) => {
                        if is_quit(&k) {
                            return Ok(());
                        }
                        if let Some(i) = input_for(k) {
                            pressed = true;
                            if i != Input::None {
                                input = i;
                            }
                        }
                    }
                    Event::Resize(nw, nh) => {
                        w = nw;
                        h = nh;
                    }
                    _ => {}
                }
                if !event::poll(Duration::ZERO)? {
                    break;
                }
            }
        }

        if !s.alive {
            // A short grace so the jump that crashed does not restart at once.
            let ready = crashed_at.map(|t| t.elapsed() > Duration::from_millis(400)).unwrap_or(true);
            if ready && pressed {
                s = s.restart();
                crashed_at = None;
                next = Instant::now();
            }
        }

        // Fixed timestep: catch up on whole steps, never more than a few.
        let mut steps = 0;
        while s.alive && Instant::now() >= next && steps < 5 {
            game::step(&mut s, input);
            input = Input::None;
            next += frame;
            steps += 1;
        }
        if !s.alive && crashed_at.is_none() {
            crashed_at = Some(Instant::now());
        }
        if steps == 5 {
            next = Instant::now() + frame;
        }

        draw(&mut out, &s, w, h, no_color)?;
    }
}

fn draw(out: &mut std::io::Stdout, s: &State, w: u16, h: u16, no_color: bool) -> std::io::Result<()> {
    if w < MIN_W || h < MIN_H {
        queue!(out, MoveTo(0, 0), ResetColor, Print(format!("make the window at least {MIN_W}x{MIN_H} (q quits)")))?;
        return out.flush();
    }
    let fw = w.min(120);
    let fh = h.min(24);
    let f = game::render(s, fw, fh);
    let night = f.night && !no_color;
    queue!(out, MoveTo(0, 0))?;
    if night {
        queue!(out, SetAttribute(Attribute::Reverse))?;
    }
    for (row, line) in f.lines.iter().enumerate() {
        queue!(out, MoveTo(0, row as u16), Print(line))?;
    }
    if night {
        queue!(out, SetAttribute(Attribute::Reset))?;
    }
    out.flush()
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::atomic::AtomicUsize;
    use std::sync::Arc;

    #[test]
    fn a_panic_restores_the_terminal_before_its_message() {
        let restored = Arc::new(AtomicUsize::new(0));
        let r = restored.clone();
        let original = std::panic::take_hook();
        std::panic::set_hook(Box::new(|_| {})); // keep the test output quiet
        install_panic_hook(move || {
            r.fetch_add(1, Ordering::SeqCst);
        });
        let result = std::panic::catch_unwind(|| panic!("the goat slipped"));
        let _ = std::panic::take_hook();
        std::panic::set_hook(original);
        assert!(result.is_err());
        assert_eq!(restored.load(Ordering::SeqCst), 1);
    }

    #[test]
    fn restore_is_safe_to_call_twice_without_a_terminal() {
        restore();
        restore();
    }

    #[test]
    fn keys_map_to_moves_and_quit() {
        let k = |c| KeyEvent::new(c, KeyModifiers::NONE);
        assert_eq!(input_for(k(KeyCode::Char(' '))), Some(Input::Jump));
        assert_eq!(input_for(k(KeyCode::Up)), Some(Input::Jump));
        assert_eq!(input_for(k(KeyCode::Down)), Some(Input::Duck));
        assert!(is_quit(&k(KeyCode::Char('q'))));
        assert!(is_quit(&k(KeyCode::Esc)));
        assert!(is_quit(&KeyEvent::new(KeyCode::Char('c'), KeyModifiers::CONTROL)));
        assert!(!is_quit(&k(KeyCode::Char(' '))));
    }

    #[test]
    fn a_pipe_is_refused_not_played() {
        // cargo test runs with stdout captured, so this is the CI and pipe case.
        if !std::io::stdout().is_terminal() {
            assert!(check_terminal().is_err());
        }
    }
}
