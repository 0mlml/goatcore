// `goat compose` — interactive command-builder TUI per ADR 1019 §14.
// TTY-gated ratatui overlay: the command-in-progress on top, a status
// line, then one live suggestion list. The input model is freeform —
// every character (space included) types into one editable line; the
// clap command tree is walked only to *derive* suggestions for the
// token under the cursor, never to gate what you can type. ↑/↓ move the
// highlight, Tab accepts a suggestion into the current token, Enter (or
// Ctrl-E) emits the literal line to stdout (the "runbook writer" exit),
// Esc / Ctrl-C cancel (exit 130).
//
// The right half of the value — the live substrate context — is in
// cmd/compose_context.rs (all four §14 feeds: write-prefixes, recent
// topics, declared schemas, conflict warnings). It is resolved once at
// launch into a read-only snapshot, so no network ever touches the sync
// event loop; a failure degrades to the grammar-only suggestions. In
// this freeform layout the list feeds fold into the one suggestion list
// (`kind = prefix | topic | schema`) and conflict warnings render in a
// wrapped context bar. Every wiring point into that module is tagged
// `// CONTEXT-PANE:` so the feature strips cleanly.
//
// Deliberately not here (per the design doc §compose): executing the
// built command in-process (emit-only is the teaching tool), `?`-anywhere
// contextual help, and mid-line cursor editing (append + backspace only).

use clap::{Args, Command, CommandFactory};
use crossterm::{
    event::{self, DisableMouseCapture, EnableMouseCapture, Event, KeyCode, KeyEventKind, KeyModifiers},
    execute,
    terminal::{disable_raw_mode, enable_raw_mode, EnterAlternateScreen, LeaveAlternateScreen},
};
use ratatui::{
    backend::CrosstermBackend,
    layout::{Constraint, Direction, Layout},
    style::{Color, Modifier, Style},
    text::{Line, Span},
    widgets::{Block, Borders, List, ListItem, ListState, Paragraph, Wrap},
    Terminal,
};
use std::io::{stdout, IsTerminal};

// CONTEXT-PANE: live substrate context (all four §14 feeds). Self-contained
// in cmd/compose_context.rs; every wiring point here is tagged the same
// way so the feature strips cleanly. See that module's header.
use super::compose_context::{
    self, ContextKind, ContextSource, ContextState, PortalSnapshot, TopicMeta,
};
use std::time::Duration;

#[derive(Args, Debug)]
pub struct ComposeArgs {
    /// Subcommand path to start the builder at (e.g. `topic create`).
    /// Without it, opens at the root ("which subcommand?").
    pub subcmd: Vec<String>,
    // CONTEXT-PANE: opt out of the live portal-context pane (instant
    // runtime kill-switch; the builder is identical without it).
    /// Disable the live substrate-context pane (write-prefixes etc.).
    #[arg(long)]
    pub no_context: bool,
}

pub async fn run(ctx: &super::Ctx, args: ComposeArgs) -> anyhow::Result<()> {
    if !stdout().is_terminal() || !std::io::stdin().is_terminal() {
        anyhow::bail!(
            "`goat compose` requires an interactive terminal. \
             Non-TTY invocations (pipes / CI) are refused by construction \
             so a script can't accidentally hang waiting for input. \
             See ADR 1019 §14 + docs/design/goat-cli.md §compose."
        );
    }
    // CONTEXT-PANE: resolve the read-only launch snapshot BEFORE entering
    // the sync TUI loop, so no network ever touches the loop. A failure
    // degrades to an "unavailable" pane, never an error.
    let context = if args.no_context {
        None
    } else {
        Some(fetch_context_snapshot(ctx).await)
    };

    let cli = crate::Cli::command();
    let final_action =
        run_tui(cli, args.subcmd, context).map_err(|e| anyhow::anyhow!("compose TUI: {e}"))?;
    match final_action {
        FinalAction::Emit(line) => {
            // Freeform: emit exactly what the operator typed.
            println!("goat {}", line.trim());
            Ok(())
        }
        FinalAction::Cancel => {
            // Exit code 130 matches SIGINT convention; the user explicitly
            // cancelled, not an error.
            std::process::exit(130);
        }
    }
}

enum FinalAction {
    /// The raw command text the operator typed (everything after `goat `).
    Emit(String),
    Cancel,
}

/// CONTEXT-PANE: how long each launch read may take before its feed degrades
/// to `Unavailable`. Two short reads, both off the critical path — the
/// composer opens regardless.
const CONTEXT_FETCH_TIMEOUT: Duration = Duration::from_secs(2);

// CONTEXT-PANE: build the launch snapshot from the operator's profile. No
// profile (or a profile the user can't read) → every feed unavailable, the
// builder still opens. With a client, delegate to `snapshot_from_client`.
async fn fetch_context_snapshot(ctx: &super::Ctx) -> PortalSnapshot {
    match ctx.profile.portal_client() {
        Ok(c) => snapshot_from_client(&c).await,
        // `{e}` on a GoatError renders its bare `E_*` code only (the
        // `#[error("E_...")]` thiserror attribute) — this pane would have
        // shown "unavailable: E_PROFILE_MISSING" with none of the actual
        // explanation GoatError carries. `short_summary()` gives the
        // human sentence instead.
        Err(e) => PortalSnapshot::unavailable(e.short_summary()),
    }
}

// CONTEXT-PANE: the two read-only launch reads — `GET /api/orgs` for
// write-prefixes and `GET /api/catalog` for recent-topics + declared-schemas
// + the collision oracle. Each is timeout-bounded and independently
// degradable: one feed failing (down, slow, or a portal predating the
// catalog) leaves the other fully usable. Factored out of `fetch_context_
// snapshot` so it can be exercised against a mock portal without a profile.
async fn snapshot_from_client(client: &panoscope_core::portal::PortalClient) -> PortalSnapshot {
    let prefixes: Result<Vec<String>, String> =
        match tokio::time::timeout(CONTEXT_FETCH_TIMEOUT, client.list_vendors()).await {
            Ok(Ok(vendors)) => Ok(vendors.into_iter().map(|v| v.prefix).collect()),
            Ok(Err(e)) => Err(format!("portal: {e}")),
            Err(_) => Err(unresponsive_reason()),
        };
    let topics: Result<Vec<TopicMeta>, String> =
        match tokio::time::timeout(CONTEXT_FETCH_TIMEOUT, client.catalog()).await {
            Ok(Ok(Some(cat))) => Ok(cat
                .orgs
                .into_iter()
                .flat_map(|o| o.topics)
                .map(|t| TopicMeta {
                    key: t.key_pattern,
                    format: t.format,
                    last_seen_at: t.last_seen_at,
                    created_at: t.created_at,
                })
                .collect()),
            // A portal predating `/api/catalog` answers 404 → `Ok(None)`: the
            // feed is simply unknown here, stated as such.
            Ok(Ok(None)) => Err("this portal has no topic catalog endpoint".to_string()),
            Ok(Err(e)) => Err(format!("portal: {e}")),
            Err(_) => Err(unresponsive_reason()),
        };
    PortalSnapshot::build(prefixes, topics)
}

fn unresponsive_reason() -> String {
    format!(
        "portal did not respond within {}s",
        CONTEXT_FETCH_TIMEOUT.as_secs()
    )
}

/// State for the compose freeform event loop. `input` is the entire
/// command text being typed (everything after `goat `). The grammar walk
/// derives live suggestions from it but never gates what can be typed —
/// space and every character just go into the line.
struct State {
    input: String,
    /// Selection within the live suggestion list.
    suggest: ListState,
    /// Transient one-line status (e.g. "→ topic") cleared on next keypress.
    status: Option<String>,
}

/// One candidate the operator can pick at the current grammar position.
/// `Subcommand` drills into a node; `Flag` appends to tokens (with a
/// placeholder if the flag takes a value).
enum Candidate {
    Subcommand {
        name: String,
        about: String,
    },
    Flag {
        long: String,
        takes_value: bool,
        value_placeholder: String,
        help: String,
        required: bool,
    },
}

impl Candidate {
    /// `(head, help)` — `head` is the short identifier shown first, `help`
    /// the longer description (may be empty). Kept separate so the renderer
    /// can wrap the help across lines instead of clipping it.
    fn head_help(&self) -> (String, String) {
        match self {
            Candidate::Subcommand { name, about } => (name.clone(), about.clone()),
            Candidate::Flag {
                long,
                takes_value,
                value_placeholder,
                help,
                required,
            } => {
                let mut head = if *takes_value {
                    format!("--{long} <{value_placeholder}>")
                } else {
                    format!("--{long}")
                };
                if *required {
                    head.push_str(" [required]");
                }
                (head, help.clone())
            }
        }
    }
}

// CONTEXT-PANE-ADJACENT: RAII terminal restore so a panic mid-loop can't
// leave the operator's shell in raw mode. Net-positive for v0 too.
struct TermGuard;
impl Drop for TermGuard {
    fn drop(&mut self) {
        let _ = disable_raw_mode();
        let _ = execute!(stdout(), LeaveAlternateScreen, DisableMouseCapture);
    }
}

/// A single live suggestion for the token the operator is currently typing.
/// `head` is the short identifier (rendered first), `help` the wrappable
/// description. `complete_to` is the exact text that replaces the partial
/// token on Tab — trailing ` ` for a finished token, `/` to continue a
/// topic-key path, or empty for an informational `value` entry (no Tab).
struct Suggestion {
    head: String,
    help: String,
    complete_to: String,
    kind: &'static str,
}

/// Split freeform input into already-finished tokens and the partial token
/// currently being typed. If the input ends in whitespace, the partial is
/// empty (the operator is starting a fresh token).
fn parse_input(input: &str) -> (Vec<String>, String) {
    let ends_ws = input
        .chars()
        .last()
        .map(char::is_whitespace)
        .unwrap_or(false);
    let mut toks: Vec<String> = input.split_whitespace().map(String::from).collect();
    let partial = if ends_ws {
        String::new()
    } else {
        toks.pop().unwrap_or_default()
    };
    (toks, partial)
}

/// Pure: suggestions for the current token, given the completed tokens'
/// grammar position and the partial being typed. `kind` is the live-context
/// kind active at this position and `context_items` the already-filtered feed
/// (write-prefixes / recent-topics / declared-schemas), empty unless a context
/// position is open and that feed loaded. Flags are offered when the partial
/// looks like a flag (`-…`) or nothing is typed yet; subcommands + context
/// items are offered as you type a bare token. Each kind completes per
/// `compose_context::complete_form` (prefix → trailing `/`; topic / schema →
/// trailing space).
fn compute_suggestions(
    root: &Command,
    completed: &[String],
    partial: &str,
    kind: &ContextKind,          // CONTEXT-PANE
    context_items: &[String],    // CONTEXT-PANE
) -> Vec<Suggestion> {
    // CHANGE 1: if the last finished token is a value-taking flag, the
    // operator is entering THAT flag's value — keep the help on it and
    // don't offer other args until the value is supplied + a space.
    if let Some((flag, value_name, help)) = flag_value_context(root, completed) {
        let mut out = vec![Suggestion {
            head: format!("--{flag} <{value_name}>"),
            help,
            complete_to: String::new(), // informational; type the value freely
            kind: "value",
        }];
        // CONTEXT-PANE: a `--format` / `--encoding` value is a declared-schema
        // position — offer the formats the goatnet actually declares.
        if *kind == ContextKind::DeclaredSchemas {
            out.extend(context_suggestions(kind, context_items));
        }
        return out;
    }

    let cands = candidates_at(root, completed);
    let lower = partial.to_ascii_lowercase();
    let mut out: Vec<Suggestion> = Vec::new();

    if partial.starts_with('-') {
        for c in &cands {
            if let Candidate::Flag { long, .. } = c {
                if format!("--{long}").starts_with(partial) {
                    let (head, help) = c.head_help();
                    out.push(Suggestion {
                        head,
                        help,
                        complete_to: format!("--{long} "),
                        kind: "flag",
                    });
                }
            }
        }
        return out;
    }

    for c in &cands {
        if let Candidate::Subcommand { name, .. } = c {
            if name.to_ascii_lowercase().starts_with(&lower) {
                let (head, help) = c.head_help();
                out.push(Suggestion {
                    head,
                    help,
                    complete_to: format!("{name} "),
                    kind: "subcommand",
                });
            }
        }
    }
    // CONTEXT-PANE: inject the live feed items (write-prefixes / recent-topics)
    // for a bare-token positional slot.
    out.extend(context_suggestions(kind, context_items));
    // With nothing typed yet, also advertise the available flags so the
    // operator sees them without guessing the leading `-`.
    if partial.is_empty() {
        for c in &cands {
            if let Candidate::Flag { long, .. } = c {
                let (head, help) = c.head_help();
                out.push(Suggestion {
                    head,
                    help,
                    complete_to: format!("--{long} "),
                    kind: "flag",
                });
            }
        }
    }
    out
}

/// CONTEXT-PANE: turn an already-filtered feed into suggestions, each
/// completing and labelled per the feed's `complete_form` grammar.
fn context_suggestions(kind: &ContextKind, items: &[String]) -> Vec<Suggestion> {
    items
        .iter()
        .map(|item| {
            let (complete_to, label) = compose_context::complete_form(kind, item);
            Suggestion {
                head: item.clone(),
                help: String::new(),
                complete_to,
                kind: label,
            }
        })
        .collect()
}

/// When the last completed token is a value-taking flag `--x`, return that
/// flag's clap `Arg` — the operator is entering its value. `None` for bool
/// flags, positionals, and subcommands.
fn flag_value_arg<'a>(root: &'a Command, completed: &[String]) -> Option<&'a clap::Arg> {
    let last = completed.last()?;
    let long = last.strip_prefix("--")?;
    // Walk the tokens BEFORE the flag to the node that owns it.
    let mut node: &Command = root;
    for tok in &completed[..completed.len() - 1] {
        if let Some(sub) = node.find_subcommand(tok) {
            node = sub;
        }
    }
    let arg = node
        .get_arguments()
        .find(|a| a.get_long().map(|l| l == long).unwrap_or(false))?;
    if !arg.get_action().takes_values() {
        return None;
    }
    Some(arg)
}

/// A flag argument's clap value-name, falling back to the UPPER_SNAKE of its
/// long name (what clap displays when none is set — e.g. `FORMAT`).
fn flag_value_name(arg: &clap::Arg) -> String {
    arg.get_value_names()
        .and_then(|n| n.first().map(|s| s.to_string()))
        .unwrap_or_else(|| {
            arg.get_long()
                .unwrap_or_default()
                .to_uppercase()
                .replace('-', "_")
        })
}

/// CHANGE 1: when the last completed token is a value-taking flag, return
/// `(flag-long, value-name, help)` so the suggestion stays on that flag's
/// value. `None` for bool flags, positionals, and subcommands.
fn flag_value_context(root: &Command, completed: &[String]) -> Option<(String, String, String)> {
    let arg = flag_value_arg(root, completed)?;
    let long = arg.get_long().unwrap_or_default().to_string();
    let value_name = flag_value_name(arg);
    let help = arg
        .get_help()
        .map(|h| h.to_string())
        .unwrap_or_default()
        .lines()
        .next()
        .unwrap_or("")
        .to_string();
    Some((long, value_name, help))
}

/// CONTEXT-PANE: the value-name that identifies the live-context position — the
/// flag value being typed, or else the next unfilled positional. A flag with a
/// FIXED set of choices (clap `possible_values`, e.g. `topic export --format
/// yaml|json`) is grammar, not substrate: clap already shows its choices and a
/// declared-schema feed there would mislead, so such a flag yields `None` (no
/// context) while still keeping its value-help line via `flag_value_context`.
fn active_value_name(root: &Command, completed: &[String]) -> Option<String> {
    if let Some(arg) = flag_value_arg(root, completed) {
        if arg.get_possible_values().is_empty() {
            return Some(flag_value_name(arg));
        }
        return None;
    }
    next_positional_value_name(root, completed)
}

/// CHANGE 2: greedy word-wrap to `width` columns (char-count approximate).
/// Renders long suggestion help across lines instead of clipping it.
fn wrap_text(s: &str, width: usize) -> Vec<String> {
    if width == 0 || s.is_empty() {
        return vec![s.to_string()];
    }
    let mut lines: Vec<String> = Vec::new();
    let mut cur = String::new();
    for word in s.split_whitespace() {
        if cur.is_empty() {
            cur = word.to_string();
        } else if cur.chars().count() + 1 + word.chars().count() <= width {
            cur.push(' ');
            cur.push_str(word);
        } else {
            lines.push(std::mem::take(&mut cur));
            cur = word.to_string();
        }
    }
    if !cur.is_empty() {
        lines.push(cur);
    }
    if lines.is_empty() {
        lines.push(String::new());
    }
    lines
}

// CONTEXT-PANE: wrap-around move of a ListState selection by `delta`
// within `len` items (shared by the candidate list and the prefix pane).
fn move_sel(ls: &mut ratatui::widgets::ListState, len: usize, delta: i32) {
    if len == 0 {
        ls.select(None);
        return;
    }
    let i = ls.selected().unwrap_or(0) as i32;
    let n = len as i32;
    ls.select(Some((i + delta).rem_euclid(n) as usize));
}

fn run_tui(
    root: Command,
    initial_path: Vec<String>,
    context: Option<PortalSnapshot>, // CONTEXT-PANE: None when --no-context
) -> std::io::Result<FinalAction> {
    enable_raw_mode()?;
    let _term_guard = TermGuard;
    let mut out = stdout();
    execute!(out, EnterAlternateScreen, EnableMouseCapture)?;
    let backend = CrosstermBackend::new(out);
    let mut terminal = Terminal::new(backend)?;

    // Freeform: the whole command is one editable line. Seed it from any
    // initial subcommand path (trailing space to start a fresh token).
    let mut input = initial_path.join(" ");
    if !input.is_empty() {
        input.push(' ');
    }
    let mut state = State {
        input,
        suggest: ListState::default(),
        status: None,
    };
    state.suggest.select(Some(0));

    let result = loop {
        let (completed, partial) = parse_input(&state.input);
        // CONTEXT-PANE: the live-context kind for the current position (pure;
        // the snapshot is in memory — no network in the loop). The active
        // value-name is the flag-value being typed (unconstrained flags only)
        // or else the next unfilled positional.
        let active_vn = active_value_name(&root, &completed);
        let kind = compose_context::classify(active_vn.as_deref());
        let context_state = match &context {
            Some(src) => src.lookup(&kind),
            None => ContextState::Disabled,
        };
        let context_items: Vec<String> = match (&active_vn, &context_state) {
            (Some(_), ContextState::Loaded { items, .. }) => {
                compose_context::filter_items(items, &partial)
            }
            _ => Vec::new(),
        };
        // CONTEXT-PANE: a conflict is computed from the typed key against the
        // snapshot (would-collide / can't-write), independent of the list feed.
        let conflict = match &context {
            Some(src) => src.conflict_for(&kind, &partial),
            None => None,
        };
        let suggestions =
            compute_suggestions(&root, &completed, &partial, &kind, &context_items);
        if state.suggest.selected().unwrap_or(0) >= suggestions.len() {
            state.suggest.select(if suggestions.is_empty() {
                None
            } else {
                Some(0)
            });
        }
        terminal.draw(|f| draw(f, &state, &suggestions, &context_state, conflict.as_deref()))?;

        let ev = match event::read()? {
            Event::Key(k) => k,
            _ => continue,
        };
        if !matches!(ev.kind, KeyEventKind::Press) {
            continue;
        }
        state.status = None;
        match (ev.code, ev.modifiers) {
            (KeyCode::Esc, _) | (KeyCode::Char('c'), KeyModifiers::CONTROL) => {
                break FinalAction::Cancel;
            }
            // Enter (or Ctrl-E for muscle memory) emits the whole line.
            (KeyCode::Enter, _) | (KeyCode::Char('e'), KeyModifiers::CONTROL) => {
                break FinalAction::Emit(state.input.clone());
            }
            // Any printable char — INCLUDING space — types into the line.
            (KeyCode::Char(ch), m) if m.is_empty() || m == KeyModifiers::SHIFT => {
                state.input.push(ch);
                state.suggest.select(Some(0));
            }
            (KeyCode::Backspace, _) => {
                state.input.pop();
                state.suggest.select(Some(0));
            }
            (KeyCode::Up, _) => move_sel(&mut state.suggest, suggestions.len(), -1),
            (KeyCode::Down, _) => move_sel(&mut state.suggest, suggestions.len(), 1),
            // Tab accepts the highlighted suggestion into the current token.
            (KeyCode::Tab, _) => {
                if let Some(s) = suggestions.get(state.suggest.selected().unwrap_or(0)) {
                    // CHANGE 1: a `value` entry is informational — Tab is a
                    // no-op there; the operator just types the value.
                    if !s.complete_to.is_empty() {
                        let base = state.input.len() - partial.len();
                        state.input.truncate(base);
                        state.input.push_str(&s.complete_to);
                        state.status = Some(format!("→ {}", s.complete_to.trim()));
                        state.suggest.select(Some(0));
                    }
                }
            }
            _ => {}
        }
    };

    disable_raw_mode()?;
    execute!(terminal.backend_mut(), LeaveAlternateScreen, DisableMouseCapture)?;
    terminal.show_cursor()?;
    Ok(result)
}

fn draw(
    f: &mut ratatui::Frame,
    state: &State,
    suggestions: &[Suggestion],
    context_state: &ContextState, // CONTEXT-PANE: drives the unavailable hint
    conflict: Option<&str>,       // CONTEXT-PANE: would-collide / can't-write
) {
    // CONTEXT-PANE: a dedicated WRAPPED context bar carries the two things a
    // 1-row status would clip — a conflict warning for the key being typed,
    // and the reason a feed is unavailable. Either, both, or neither. flags +
    // subcommands are unaffected whatever the portal did.
    let area_w = f.area().width as usize;
    let wrap_w = area_w.saturating_sub(4).max(20);
    let mut warn_lines: Vec<String> = Vec::new();
    if let Some(c) = conflict {
        warn_lines.extend(wrap_text(&format!("⚠ {c}"), wrap_w));
    }
    if let ContextState::Unavailable(reason) = context_state {
        warn_lines.extend(wrap_text(
            &format!(
                "⚠ live context unavailable: {reason} — type the name manually (flags + subcommands are still suggested)"
            ),
            wrap_w,
        ));
    }
    let show_warn = !warn_lines.is_empty();
    let warn_h = (warn_lines.len() as u16 + 2).clamp(3, 7);

    let mut constraints = vec![
        Constraint::Length(3), // command-in-progress
        Constraint::Length(3), // status
    ];
    if show_warn {
        constraints.push(Constraint::Length(warn_h)); // unavailability warning
    }
    constraints.push(Constraint::Min(0)); // suggestions
    constraints.push(Constraint::Length(3)); // footer
    let chunks = Layout::default()
        .direction(Direction::Vertical)
        .constraints(constraints)
        .split(f.area());
    let warn_idx = if show_warn { Some(2usize) } else { None };
    let sugg_idx = if show_warn { 3 } else { 2 };
    let footer_idx = chunks.len() - 1;

    // command-in-progress: the whole freeform line + blinking cursor.
    let cmd_para = Paragraph::new(Line::from(vec![
        Span::raw("goat "),
        Span::styled(
            state.input.clone(),
            Style::default().add_modifier(Modifier::BOLD),
        ),
        Span::styled("_", Style::default().add_modifier(Modifier::SLOW_BLINK)),
    ]))
    .block(
        Block::default()
            .borders(Borders::ALL)
            .title("command-in-progress"),
    );
    f.render_widget(cmd_para, chunks[0]);

    let status_text = state.status.clone().unwrap_or_else(|| {
        "type the command · ↑/↓ pick · ↹ complete · ⏎ emit · Esc cancel".to_string()
    });
    let status = Paragraph::new(status_text)
        .block(Block::default().borders(Borders::ALL).title("status"))
        .wrap(Wrap { trim: true });
    f.render_widget(status, chunks[1]);

    // CONTEXT-PANE: the wrapped unavailability warning bar (when present).
    if let Some(wi) = warn_idx {
        let lines: Vec<Line> = warn_lines
            .iter()
            .map(|l| Line::from(Span::styled(l.clone(), Style::default().fg(Color::Yellow))))
            .collect();
        let warn = Paragraph::new(lines)
            .block(Block::default().borders(Borders::ALL).title("context"))
            .wrap(Wrap { trim: true });
        f.render_widget(warn, chunks[wi]);
    }

    // CHANGE 2: each suggestion = a head line + the help WRAPPED across
    // lines (indented, dim) so long descriptions don't clip off the right
    // edge. Wrap width tracks the actual pane width each frame.
    let wrap_w = (chunks[sugg_idx].width as usize).saturating_sub(8).max(12);
    let items: Vec<ListItem> = suggestions
        .iter()
        .map(|s| {
            let mut lines = vec![Line::from(vec![
                Span::styled(
                    format!("[{:<10}] ", s.kind),
                    Style::default().add_modifier(Modifier::DIM),
                ),
                Span::raw(s.head.clone()),
            ])];
            if !s.help.is_empty() {
                for wl in wrap_text(&s.help, wrap_w) {
                    lines.push(Line::from(Span::styled(
                        format!("    {wl}"),
                        Style::default().add_modifier(Modifier::DIM),
                    )));
                }
            }
            ListItem::new(lines)
        })
        .collect();
    let title = if suggestions.is_empty() {
        "suggestions  (none — keep typing, or ⏎ to emit what you have)".to_string()
    } else {
        format!("suggestions  ({})", suggestions.len())
    };
    let list = List::new(items)
        .block(Block::default().borders(Borders::ALL).title(title))
        .highlight_style(
            Style::default()
                .add_modifier(Modifier::REVERSED)
                .add_modifier(Modifier::BOLD),
        )
        .highlight_symbol("▸ ");
    let mut ss = state.suggest.clone();
    f.render_stateful_widget(list, chunks[sugg_idx], &mut ss);

    let footer = Paragraph::new(Line::from(vec![Span::styled(
        "compose · freeform — type freely, ↹ completes, ⏎ emits · live context: prefixes · topics · schemas · conflicts (`--no-context` to disable) · docs/design/goat-cli.md §compose",
        Style::default().add_modifier(Modifier::DIM),
    )]))
    .block(Block::default().borders(Borders::ALL));
    f.render_widget(footer, chunks[footer_idx]);
}

/// CONTEXT-PANE: the value-name (or id) of the next UNFILLED positional
/// argument at the current grammar position, or None if the leaf node
/// takes no more positionals. Mirrors `candidates_at`'s token walk: flags
/// (and their values) and `<placeholder>` tokens are skipped; subcommand
/// tokens drill in (resetting the positional count); any other bare token
/// at the leaf counts as a supplied positional value.
fn next_positional_value_name(root: &Command, tokens: &[String]) -> Option<String> {
    let mut node: &Command = root;
    let mut positionals_consumed = 0usize;
    let mut i = 0;
    while i < tokens.len() {
        let tok = &tokens[i];
        if tok.starts_with("--") {
            let long = tok.trim_start_matches("--");
            let mut consumed_value = false;
            if let Some(arg) = node
                .get_arguments()
                .find(|a| a.get_long().map(|l| l == long).unwrap_or(false))
            {
                if arg.get_action().takes_values() && i + 1 < tokens.len() {
                    consumed_value = true;
                }
            }
            i += 1;
            if consumed_value {
                i += 1;
            }
            continue;
        }
        if tok.starts_with('<') && tok.ends_with('>') {
            // Placeholder for an unfilled flag value — not a positional.
            i += 1;
            continue;
        }
        if let Some(sub) = node.find_subcommand(tok) {
            node = sub;
            positionals_consumed = 0;
        } else {
            positionals_consumed += 1;
        }
        i += 1;
    }

    node.get_positionals()
        .nth(positionals_consumed)
        .map(positional_display_name)
}

/// CONTEXT-PANE: a positional's clap value-name, falling back to its id
/// (what clap displays when no explicit value_name is set — e.g. `key`).
fn positional_display_name(arg: &clap::Arg) -> String {
    arg.get_value_names()
        .and_then(|names| names.first().map(|n| n.to_string()))
        .unwrap_or_else(|| arg.get_id().as_str().to_string())
}

/// Walk clap's tree following the typed-token path and return the
/// candidates available at that grammar position. If the path ends
/// inside a subcommand, candidates are (sub-subcommands + the
/// subcommand's args). If the path runs through a placeholder
/// (`<value>`), we treat that token as resolved and look at the next
/// position. Hidden args (e.g., `_complete`) are filtered.
fn candidates_at(root: &Command, tokens: &[String]) -> Vec<Candidate> {
    let mut node: &Command = root;
    let mut i = 0;
    while i < tokens.len() {
        let tok = &tokens[i];
        if tok.starts_with("--") {
            // A flag — does it take a value? Skip the placeholder if so.
            let long = tok.trim_start_matches("--");
            let mut consumed_value = false;
            if let Some(arg) = node
                .get_arguments()
                .find(|a| a.get_long().map(|l| l == long).unwrap_or(false))
            {
                if arg.get_action().takes_values() && i + 1 < tokens.len() {
                    consumed_value = true;
                }
            }
            i += 1;
            if consumed_value {
                i += 1;
            }
            continue;
        }
        if tok.starts_with('<') && tok.ends_with('>') {
            // Standalone placeholder — skip.
            i += 1;
            continue;
        }
        // Subcommand token — drill in.
        if let Some(sub) = node.find_subcommand(tok) {
            node = sub;
        }
        i += 1;
    }

    let mut out: Vec<Candidate> = Vec::new();

    for s in node.get_subcommands() {
        if s.is_hide_set() {
            continue;
        }
        out.push(Candidate::Subcommand {
            name: s.get_name().to_string(),
            about: s
                .get_about()
                .map(|a| a.to_string())
                .unwrap_or_default()
                .lines()
                .next()
                .unwrap_or("")
                .to_string(),
        });
    }

    // Flags applicable at this position — exclude flags already in tokens.
    let already: std::collections::HashSet<&str> = tokens
        .iter()
        .filter(|t| t.starts_with("--"))
        .map(|t| t.trim_start_matches("--"))
        .collect();
    for a in node.get_arguments() {
        if a.is_hide_set() {
            continue;
        }
        let Some(long) = a.get_long() else {
            continue;
        };
        if already.contains(long) {
            continue;
        }
        let takes_value = a.get_action().takes_values();
        let value_placeholder = a
            .get_value_names()
            .and_then(|names| names.first().map(|n| n.to_string()))
            .unwrap_or_else(|| {
                if takes_value {
                    long.to_uppercase().replace('-', "_")
                } else {
                    String::new()
                }
            });
        out.push(Candidate::Flag {
            long: long.to_string(),
            takes_value,
            value_placeholder,
            help: a
                .get_help()
                .map(|h| h.to_string())
                .unwrap_or_default()
                .lines()
                .next()
                .unwrap_or("")
                .to_string(),
            required: a.is_required_set(),
        });
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;

    fn root() -> Command {
        crate::Cli::command()
    }

    #[test]
    fn root_candidates_include_top_level_subcommands() {
        let cs = candidates_at(&root(), &[]);
        let names: Vec<&str> = cs
            .iter()
            .filter_map(|c| match c {
                Candidate::Subcommand { name, .. } => Some(name.as_str()),
                _ => None,
            })
            .collect();
        assert!(names.contains(&"topic"));
        assert!(names.contains(&"org"));
        assert!(names.contains(&"doctor"));
        assert!(names.contains(&"compose"));
    }

    #[test]
    fn topic_create_candidates_include_format_flag() {
        let cs = candidates_at(&root(), &["topic".into(), "create".into()]);
        let flag_longs: Vec<&str> = cs
            .iter()
            .filter_map(|c| match c {
                Candidate::Flag { long, .. } => Some(long.as_str()),
                _ => None,
            })
            .collect();
        assert!(flag_longs.contains(&"format"));
        assert!(flag_longs.contains(&"description"));
        assert!(flag_longs.contains(&"dry-run"));
    }

    #[test]
    fn already_typed_flags_disappear_from_candidates() {
        let cs = candidates_at(
            &root(),
            &[
                "topic".into(),
                "create".into(),
                "--format".into(),
                "<FORMAT>".into(),
            ],
        );
        let flag_longs: Vec<&str> = cs
            .iter()
            .filter_map(|c| match c {
                Candidate::Flag { long, .. } => Some(long.as_str()),
                _ => None,
            })
            .collect();
        assert!(!flag_longs.contains(&"format"), "format should be hidden after typed");
        assert!(flag_longs.contains(&"description"), "other flags still visible");
    }

    #[test]
    fn hidden_subcommands_filtered() {
        let cs = candidates_at(&root(), &[]);
        let names: Vec<&str> = cs
            .iter()
            .filter_map(|c| match c {
                Candidate::Subcommand { name, .. } => Some(name.as_str()),
                _ => None,
            })
            .collect();
        // `_complete` is `#[command(hide = true)]` in main.rs
        assert!(!names.contains(&"_complete"));
    }

    #[test]
    fn flag_head_help_splits_identifier_from_description() {
        let c = Candidate::Flag {
            long: "format".into(),
            takes_value: true,
            value_placeholder: "FMT".into(),
            help: "encoding format".into(),
            required: true,
        };
        let (head, help) = c.head_help();
        assert!(head.contains("--format <FMT>"));
        assert!(head.contains("[required]"));
        assert_eq!(help, "encoding format");
    }

    // CONTEXT-PANE: the position -> context-kind wiring, against the real
    // goat grammar — through `active_value_name` (flag value OR next
    // positional), the same function the event loop uses. These prove each
    // feed surfaces at the right slot and nowhere else.
    use crate::cmd::compose_context::{classify, ContextKind};

    fn kind_at(tokens: &[&str]) -> ContextKind {
        let toks: Vec<String> = tokens.iter().map(|s| s.to_string()).collect();
        classify(active_value_name(&root(), &toks).as_deref())
    }

    #[test]
    fn write_commands_trigger_write_prefixes() {
        // pub / topic create / topic delete compose a key under YOUR namespace.
        assert_eq!(kind_at(&["pub"]), ContextKind::WritePrefixes);
        assert_eq!(kind_at(&["topic", "create"]), ContextKind::WritePrefixes);
        assert_eq!(kind_at(&["topic", "delete"]), ContextKind::WritePrefixes);
    }

    #[test]
    fn read_commands_trigger_recent_topics() {
        // sub (`key_pattern`) / flow (`key_expr`) name an EXISTING key.
        assert_eq!(kind_at(&["sub"]), ContextKind::RecentTopics);
        assert_eq!(kind_at(&["flow"]), ContextKind::RecentTopics);
    }

    #[test]
    fn format_flag_triggers_declared_schemas() {
        // Entering an unconstrained `--format` value is a schema position.
        assert_eq!(
            kind_at(&["topic", "create", "goat-maintainer/x/v1", "--format"]),
            ContextKind::DeclaredSchemas
        );
        // `pub --encoding <value>` likewise.
        assert_eq!(kind_at(&["pub", "k/x/v1", "--encoding"]), ContextKind::DeclaredSchemas);
    }

    #[test]
    fn constrained_enum_flag_has_no_substrate_context() {
        // `topic export --format` is a fixed yaml|json choice — clap shows the
        // options; the schema feed would mislead, so no context fires.
        assert_eq!(
            kind_at(&["topic", "export", "goat-maintainer", "--format"]),
            ContextKind::None
        );
    }

    #[test]
    fn root_and_subcommand_groups_have_no_context() {
        assert_eq!(kind_at(&[]), ContextKind::None);
        // `topic` is a group (create/delete/ls), takes no positional itself.
        assert_eq!(kind_at(&["topic"]), ContextKind::None);
    }

    #[test]
    fn filled_positional_clears_the_context() {
        // Once the single topic-key positional is supplied, `pub` has no
        // further positional → no context.
        assert_eq!(kind_at(&["pub", "org11x/observations/v1"]), ContextKind::None);
    }

    #[test]
    fn flags_do_not_consume_the_positional_slot() {
        // A bool flag before the positional must not be mistaken for the
        // positional value — the read key slot is still open.
        assert_eq!(kind_at(&["sub", "--tui"]), ContextKind::RecentTopics);
    }

    // --- freeform input model -------------------------------------------

    #[test]
    fn parse_input_splits_completed_and_partial() {
        assert_eq!(parse_input(""), (vec![], "".to_string()));
        assert_eq!(parse_input("pub"), (vec![], "pub".to_string()));
        assert_eq!(
            parse_input("pub "),
            (vec!["pub".to_string()], "".to_string())
        );
        assert_eq!(
            parse_input("pub and"),
            (vec!["pub".to_string()], "and".to_string())
        );
        assert_eq!(
            parse_input("topic create --for"),
            (
                vec!["topic".to_string(), "create".to_string()],
                "--for".to_string()
            )
        );
    }

    #[test]
    fn suggestions_offer_subcommands_as_you_type() {
        let s = compute_suggestions(&root(), &[], "top", &ContextKind::None, &[]);
        assert!(s
            .iter()
            .any(|x| x.kind == "subcommand" && x.complete_to == "topic "));
    }

    #[test]
    fn suggestions_offer_flags_when_typing_a_dash() {
        let completed = vec!["topic".to_string(), "create".to_string()];
        let s = compute_suggestions(&root(), &completed, "--for", &ContextKind::None, &[]);
        assert!(s
            .iter()
            .any(|x| x.kind == "flag" && x.complete_to == "--format "));
        // Typing a flag partial yields only flags — no subcommands/prefixes.
        assert!(s.iter().all(|x| x.kind == "flag"));
    }

    #[test]
    fn suggestions_offer_prefixes_at_a_write_slot() {
        let completed = vec!["pub".to_string()];
        let prefixes = vec!["org12x".to_string()];
        let s = compute_suggestions(
            &root(),
            &completed,
            "and",
            &ContextKind::WritePrefixes,
            &prefixes,
        );
        assert!(s
            .iter()
            .any(|x| x.kind == "prefix" && x.complete_to == "org12x/"));
    }

    #[test]
    fn suggestions_offer_recent_topics_at_a_read_slot() {
        // A read key slot (sub) completes an EXISTING key as a finished token.
        let completed = vec!["sub".to_string()];
        let topics = vec!["org11x/observations/v1".to_string()];
        let s = compute_suggestions(
            &root(),
            &completed,
            "and",
            &ContextKind::RecentTopics,
            &topics,
        );
        assert!(s
            .iter()
            .any(|x| x.kind == "topic" && x.complete_to == "org11x/observations/v1 "));
    }

    #[test]
    fn flag_value_appends_declared_schemas() {
        // Entering `--format`'s value: the value-help line PLUS the goatnet's
        // declared formats, each a finished `schema` token.
        let completed = vec![
            "topic".to_string(),
            "create".to_string(),
            "goat-maintainer/x/v1".to_string(),
            "--format".to_string(),
        ];
        let schemas = vec!["json".to_string(), "protobuf:Observation".to_string()];
        let s = compute_suggestions(
            &root(),
            &completed,
            "",
            &ContextKind::DeclaredSchemas,
            &schemas,
        );
        assert_eq!(s[0].kind, "value", "value-help still leads");
        assert!(s
            .iter()
            .any(|x| x.kind == "schema" && x.complete_to == "protobuf:Observation "));
    }

    #[test]
    fn empty_partial_advertises_available_flags() {
        let completed = vec!["topic".to_string(), "create".to_string()];
        let s = compute_suggestions(&root(), &completed, "", &ContextKind::None, &[]);
        assert!(s.iter().any(|x| x.kind == "flag"));
    }

    // CHANGE 1: stay on a value-taking flag while its value is being typed.
    #[test]
    fn entering_a_flag_value_stays_on_that_flag() {
        // `pub --payload-file ` (or mid-value) — the only suggestion is the
        // value help for --payload-file, not other flags (and not a schema
        // feed: payload-file is not a schema position).
        let completed = vec!["pub".to_string(), "--payload-file".to_string()];
        let s = compute_suggestions(&root(), &completed, "", &ContextKind::None, &[]);
        assert_eq!(s.len(), 1);
        assert_eq!(s[0].kind, "value");
        assert!(s[0].head.contains("--payload-file"));
        // Informational — Tab is a no-op (no completion text).
        assert!(s[0].complete_to.is_empty());
    }

    #[test]
    fn value_mode_exits_once_the_value_is_supplied() {
        // After `pub --payload-file /tmp/x ` the value is in; other args
        // (e.g. --encoding) are offered again.
        let completed = vec![
            "pub".to_string(),
            "--payload-file".to_string(),
            "/tmp/x".to_string(),
        ];
        let s = compute_suggestions(&root(), &completed, "", &ContextKind::None, &[]);
        assert!(s.iter().all(|x| x.kind != "value"));
    }

    #[test]
    fn bool_flag_does_not_capture_a_value() {
        // `--confirm` takes no value, so it must not trigger value mode.
        let completed = vec!["pub".to_string(), "--confirm".to_string()];
        let s = compute_suggestions(&root(), &completed, "", &ContextKind::None, &[]);
        assert!(s.iter().all(|x| x.kind != "value"));
    }

    // CHANGE 2: word-wrap helper for non-clipping help.
    #[test]
    fn wrap_text_respects_width_and_keeps_words() {
        let w = wrap_text("the quick brown fox jumps over", 11);
        assert!(w.len() >= 2, "should wrap onto multiple lines");
        assert!(
            w.iter().all(|l| l.chars().count() <= 11),
            "no line over width"
        );
        assert_eq!(w.join(" "), "the quick brown fox jumps over");
        assert_eq!(wrap_text("", 10), vec!["".to_string()]);
    }

    // CONTEXT-PANE: the real launch-read path against a mock portal — the two
    // GETs, the catalog → feed derivations, and conflict from live data. This
    // is the production `snapshot_from_client`; only the HTTP backend is mocked.
    #[tokio::test]
    async fn snapshot_from_client_builds_all_feeds_from_the_portal() {
        use wiremock::matchers::{method, path};
        use wiremock::{Mock, MockServer, ResponseTemplate};

        let server = MockServer::start().await;
        Mock::given(method("GET"))
            .and(path("/api/orgs"))
            .respond_with(ResponseTemplate::new(200).set_body_json(serde_json::json!({
                "orgs": [{ "prefix": "orgbeta" }, { "prefix": "orgalpha" }]
            })))
            .mount(&server)
            .await;
        Mock::given(method("GET"))
            .and(path("/api/catalog"))
            .respond_with(ResponseTemplate::new(200).set_body_json(serde_json::json!({
                "orgs": [
                    {
                        "prefix": "orgbeta",
                        "topics": [
                            { "key_pattern": "orgbeta/test/v1", "format": "json", "created_at": 30 },
                            { "key_pattern": "orgbeta/probe/v2", "format": "json", "last_seen_at": 500, "created_at": 50 }
                        ]
                    },
                    {
                        "prefix": "orgalpha",
                        "topics": [
                            { "key_pattern": "orgalpha/obs/v1", "format": "protobuf:Obs", "created_at": 10 }
                        ]
                    }
                ]
            })))
            .mount(&server)
            .await;

        let client = panoscope_core::portal::PortalClient::new(server.uri(), "k".into());
        let snap = snapshot_from_client(&client).await;

        match snap.lookup(&ContextKind::WritePrefixes) {
            ContextState::Loaded { items, .. } => assert_eq!(items, vec!["orgalpha", "orgbeta"]),
            o => panic!("write-prefixes: {o:?}"),
        }
        match snap.lookup(&ContextKind::RecentTopics) {
            // probe (last_seen 500) sorts ahead of the rest.
            ContextState::Loaded { items, .. } => {
                assert_eq!(items[0], "orgbeta/probe/v2");
                assert!(items.contains(&"orgalpha/obs/v1".to_string()));
            }
            o => panic!("recent-topics: {o:?}"),
        }
        match snap.lookup(&ContextKind::DeclaredSchemas) {
            ContextState::Loaded { items, .. } => {
                assert_eq!(items, vec!["json", "protobuf:Obs"])
            }
            o => panic!("declared-schemas: {o:?}"),
        }
        assert!(snap
            .conflict_for(&ContextKind::WritePrefixes, "orgbeta/test/v1")
            .unwrap()
            .contains("already exists"));
        assert!(snap
            .conflict_for(&ContextKind::WritePrefixes, "someoneelse/x/v1")
            .unwrap()
            .contains("write-prefixes"));
    }

    #[tokio::test]
    async fn snapshot_from_client_degrades_when_catalog_absent() {
        use wiremock::matchers::{method, path};
        use wiremock::{Mock, MockServer, ResponseTemplate};

        // orgs up, catalog 404 (a portal predating `/api/catalog`): the write
        // feed still loads, the topic feeds say why — the composer opens fine.
        let server = MockServer::start().await;
        Mock::given(method("GET"))
            .and(path("/api/orgs"))
            .respond_with(ResponseTemplate::new(200).set_body_json(serde_json::json!({
                "orgs": [{ "prefix": "goat-maintainer" }]
            })))
            .mount(&server)
            .await;
        Mock::given(method("GET"))
            .and(path("/api/catalog"))
            .respond_with(ResponseTemplate::new(404))
            .mount(&server)
            .await;

        let client = panoscope_core::portal::PortalClient::new(server.uri(), "k".into());
        let snap = snapshot_from_client(&client).await;

        assert!(matches!(
            snap.lookup(&ContextKind::WritePrefixes),
            ContextState::Loaded { .. }
        ));
        assert!(matches!(
            snap.lookup(&ContextKind::RecentTopics),
            ContextState::Unavailable(_)
        ));
        assert!(matches!(
            snap.lookup(&ContextKind::DeclaredSchemas),
            ContextState::Unavailable(_)
        ));
    }
}
