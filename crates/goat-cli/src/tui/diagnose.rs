// Diagnose lens (Phase B) — the interactive form of `goat inspect`.
//
// Renders stethoscope's `/api/diagnose` SSE verdict stream as a live
// two-pane TUI: a list of check cards on the left, the selected card's
// detail (summary + findings + remediation) on the right, a roll-up in
// the header, keybindings in the footer. Verdicts stream in as they
// arrive; the operator can navigate while the diagnosis is still
// running and after it completes.
//
// DELEGATION GUARDRAIL (docs/design/goat-cli-tui.md): no diagnostic
// logic lives here. stethoscope computes every verdict, finding, and
// remediation; this module is a renderer over the same SSE contract the
// line-oriented `goat inspect` consumes (shared `open_diagnose_response`
// + `parse_sse_event_block` from cmd::inspect).
//
// Concurrency: the SSE producer runs as an async task feeding an
// unbounded channel; the ratatui event loop runs synchronously on a
// blocking thread (`spawn_blocking`) and drains the channel each tick.
// The multi-thread runtime keeps the producer making progress while the
// loop blocks on input polling.

use crate::cmd::inspect::{open_diagnose_response, parse_sse_event_block, CheckOutcome};
use crossterm::{
    event::{self, Event, KeyCode, KeyEventKind, KeyModifiers},
    execute,
    terminal::{disable_raw_mode, enable_raw_mode, EnterAlternateScreen, LeaveAlternateScreen},
};
use futures::StreamExt;
use ratatui::{
    backend::CrosstermBackend,
    layout::{Constraint, Direction, Layout},
    style::{Color, Modifier, Style},
    text::{Line, Span, Text},
    widgets::{Block, Borders, List, ListItem, ListState, Paragraph, Wrap},
    Terminal,
};
use std::io::stdout;
use std::time::Duration;
use tokio::sync::mpsc::{self, UnboundedReceiver, UnboundedSender};

/// Poll cadence for the input loop — also the redraw interval when new
/// outcomes arrive over the channel with no key event.
const TICK: Duration = Duration::from_millis(120);

/// What the SSE producer sends to the UI loop.
enum StreamMsg {
    Outcome(Box<CheckOutcome>),
    Done,
    Error(String),
}

/// Entry point. TTY gate is the caller's responsibility (done in
/// cmd::inspect before any endpoint resolution). `url` is the fully
/// built `/api/diagnose?...` URL; `target` is the human label for the
/// header (e.g. `topic=org11x/observations/v1`).
// disallowed-methods (clippy.toml) targets goat-relay's shard-pinned hot
// path; goat-cli is a CLI, so the explicit allow is the prescribed escape
// hatch. Same shape as the Tail lens.
#[allow(clippy::disallowed_methods)]
pub(crate) async fn run(url: String, target: String) -> anyhow::Result<()> {
    let (tx, rx) = mpsc::unbounded_channel();

    // Producer: drive the SSE stream into the channel. Runs on the async
    // runtime's worker pool.
    let producer = tokio::spawn(async move { produce(url, tx).await });

    // Consumer: the synchronous ratatui loop on a blocking thread so it
    // doesn't occupy an async worker while polling for input.
    let handle = tokio::task::spawn_blocking(move || run_loop(target, rx));
    let result = handle
        .await
        .map_err(|e| anyhow::anyhow!("diagnose TUI thread panicked: {e}"))?;

    // The operator quit (or the stream errored and they quit). Stop the
    // producer if it's still going.
    producer.abort();
    result
}

/// Drive the stethoscope SSE stream, forwarding each outcome / done /
/// error to the UI loop. Mirrors the line renderer's stream handling but
/// sends over the channel instead of printing. Any transport error or a
/// stream that ends without `done` becomes a `StreamMsg::Error` so the
/// UI can show it rather than silently stalling.
async fn produce(url: String, tx: UnboundedSender<StreamMsg>) {
    let resp = match open_diagnose_response(&url).await {
        Ok(r) => r,
        Err(e) => {
            let _ = tx.send(StreamMsg::Error(e.to_string()));
            return;
        }
    };

    let mut stream = resp.bytes_stream();
    let mut buf = String::new();

    while let Some(chunk) = stream.next().await {
        let chunk = match chunk {
            Ok(c) => c,
            Err(e) => {
                let _ = tx.send(StreamMsg::Error(format!("read SSE chunk: {e}")));
                return;
            }
        };
        buf.push_str(&String::from_utf8_lossy(&chunk));

        while let Some(end) = buf.find("\n\n") {
            let block: String = buf.drain(..=end + 1).collect();
            let (event, data) = parse_sse_event_block(&block);
            match event.as_str() {
                "outcome" => match serde_json::from_str::<CheckOutcome>(&data) {
                    Ok(o) => {
                        if tx.send(StreamMsg::Outcome(Box::new(o))).is_err() {
                            return; // UI gone — stop.
                        }
                    }
                    // Malformed outcome: surface as a non-fatal note via a
                    // synthetic error-card-free signal. We keep going.
                    Err(_) => { /* skip malformed block, keep streaming */ }
                },
                "done" => {
                    let _ = tx.send(StreamMsg::Done);
                    return;
                }
                _ => {}
            }
        }
    }

    let _ = tx.send(StreamMsg::Error(
        "stethoscope SSE stream ended without a `done` event".into(),
    ));
}

/// Stream status shown in the header.
enum Status {
    Streaming,
    Done,
    Error(String),
}

struct DiagnoseState {
    target: String,
    outcomes: Vec<CheckOutcome>,
    list_state: ListState,
    status: Status,
    detail_scroll: u16,
}

impl DiagnoseState {
    fn new(target: String) -> Self {
        Self {
            target,
            outcomes: Vec::new(),
            list_state: ListState::default(),
            status: Status::Streaming,
            detail_scroll: 0,
        }
    }

    /// Fold one producer message into the state. Extracted from the loop
    /// so it's unit-testable without a terminal.
    fn apply(&mut self, msg: StreamMsg) {
        match msg {
            StreamMsg::Outcome(o) => {
                self.outcomes.push(*o);
                if self.list_state.selected().is_none() {
                    self.list_state.select(Some(0));
                }
            }
            StreamMsg::Done => self.status = Status::Done,
            StreamMsg::Error(e) => self.status = Status::Error(e),
        }
    }

    /// Worst-wins roll-up over received verdicts.
    fn rollup(&self) -> &'static str {
        rollup_verdict(self.outcomes.iter().map(|o| o.verdict.as_str()))
    }

    fn select_next(&mut self) {
        if self.outcomes.is_empty() {
            return;
        }
        let i = self.list_state.selected().unwrap_or(0);
        self.list_state.select(Some((i + 1) % self.outcomes.len()));
        self.detail_scroll = 0;
    }

    fn select_prev(&mut self) {
        if self.outcomes.is_empty() {
            return;
        }
        let n = self.outcomes.len();
        let i = self.list_state.selected().unwrap_or(0);
        self.list_state.select(Some((i + n - 1) % n));
        self.detail_scroll = 0;
    }
}

/// Worst-wins: any fail → fail; else any warn → warn; else any pass →
/// pass; else (all skipped/na, or empty) → skipped. Matches stethoscope's
/// roll-up semantics (skipped/na don't worsen the verdict).
fn rollup_verdict<'a>(verdicts: impl Iterator<Item = &'a str>) -> &'static str {
    let mut any_warn = false;
    let mut any_pass = false;
    for v in verdicts {
        match v {
            "fail" => return "fail",
            "warn" => any_warn = true,
            "pass" => any_pass = true,
            _ => {}
        }
    }
    if any_warn {
        "warn"
    } else if any_pass {
        "pass"
    } else {
        "skipped"
    }
}

fn glyph_for(verdict: &str) -> &'static str {
    match verdict {
        "pass" => "[✓]",
        "warn" => "[!]",
        "fail" => "[✗]",
        "skipped" => "[-]",
        "na" => "[/]",
        _ => "[?]",
    }
}

fn color_for(verdict: &str) -> Color {
    match verdict {
        "pass" => Color::Green,
        "warn" => Color::Yellow,
        "fail" => Color::Red,
        "skipped" => Color::DarkGray,
        "na" => Color::Blue,
        _ => Color::Gray,
    }
}

/// Set up the terminal, run the event loop, and guarantee teardown even
/// if the loop errors (so a panic/error doesn't leave the operator's
/// terminal in raw mode).
fn run_loop(target: String, rx: UnboundedReceiver<StreamMsg>) -> anyhow::Result<()> {
    enable_raw_mode()?;
    execute!(stdout(), EnterAlternateScreen)?;
    let backend = CrosstermBackend::new(stdout());
    let mut terminal = Terminal::new(backend)?;

    let outcome = event_loop(&mut terminal, target, rx);

    disable_raw_mode()?;
    execute!(terminal.backend_mut(), LeaveAlternateScreen)?;
    terminal.show_cursor()?;

    outcome.map_err(|e| anyhow::anyhow!("diagnose TUI: {e}"))
}

fn event_loop(
    terminal: &mut Terminal<CrosstermBackend<std::io::Stdout>>,
    target: String,
    mut rx: UnboundedReceiver<StreamMsg>,
) -> std::io::Result<()> {
    let mut state = DiagnoseState::new(target);

    loop {
        // Drain everything the producer has queued since the last tick.
        while let Ok(msg) = rx.try_recv() {
            state.apply(msg);
        }

        // Keep the selection in range as outcomes grow.
        if let Some(sel) = state.list_state.selected() {
            if sel >= state.outcomes.len() && !state.outcomes.is_empty() {
                state.list_state.select(Some(state.outcomes.len() - 1));
            }
        }

        terminal.draw(|f| draw(f, &mut state))?;

        if event::poll(TICK)? {
            if let Event::Key(k) = event::read()? {
                if k.kind != KeyEventKind::Press {
                    continue;
                }
                match (k.code, k.modifiers) {
                    // Quitting a read-only viewer is success (exit 0),
                    // not a cancel — distinct from `goat compose`, whose
                    // Esc means "produced nothing" → 130.
                    (KeyCode::Char('q'), _)
                    | (KeyCode::Esc, _)
                    | (KeyCode::Char('c'), KeyModifiers::CONTROL) => return Ok(()),
                    (KeyCode::Down, _) | (KeyCode::Char('j'), _) => state.select_next(),
                    (KeyCode::Up, _) | (KeyCode::Char('k'), _) => state.select_prev(),
                    (KeyCode::PageDown, _) | (KeyCode::Char(' '), _) => {
                        state.detail_scroll = state.detail_scroll.saturating_add(5);
                    }
                    (KeyCode::PageUp, _) => {
                        state.detail_scroll = state.detail_scroll.saturating_sub(5);
                    }
                    _ => {}
                }
            }
        }
    }
}

fn draw(f: &mut ratatui::Frame, state: &mut DiagnoseState) {
    let chunks = Layout::default()
        .direction(Direction::Vertical)
        .constraints([
            Constraint::Length(3), // header
            Constraint::Min(0),    // body
            Constraint::Length(1), // footer
        ])
        .split(f.area());

    draw_header(f, chunks[0], state);

    let body = Layout::default()
        .direction(Direction::Horizontal)
        .constraints([Constraint::Percentage(48), Constraint::Percentage(52)])
        .split(chunks[1]);

    draw_list(f, body[0], state);
    draw_detail(f, body[1], state);
    draw_footer(f, chunks[2]);
}

fn draw_header(f: &mut ratatui::Frame, area: ratatui::layout::Rect, state: &DiagnoseState) {
    let rollup = state.rollup();
    let (status_label, status_color) = match &state.status {
        Status::Streaming => ("streaming…", Color::Cyan),
        Status::Done => ("complete", Color::Green),
        Status::Error(_) => ("error", Color::Red),
    };
    let rollup_span = if state.outcomes.is_empty() {
        Span::styled("—", Style::default().add_modifier(Modifier::DIM))
    } else {
        Span::styled(
            format!("{} {}", glyph_for(rollup), rollup),
            Style::default()
                .fg(color_for(rollup))
                .add_modifier(Modifier::BOLD),
        )
    };
    let line = Line::from(vec![
        Span::styled("diagnose ", Style::default().add_modifier(Modifier::BOLD)),
        Span::raw(state.target.clone()),
        Span::raw("   roll-up: "),
        rollup_span,
        Span::raw(format!("   ({} checks)   ", state.outcomes.len())),
        Span::styled(status_label, Style::default().fg(status_color)),
    ]);
    let p = Paragraph::new(line)
        .block(Block::default().borders(Borders::ALL).title("stethoscope"));
    f.render_widget(p, area);
}

fn draw_list(f: &mut ratatui::Frame, area: ratatui::layout::Rect, state: &mut DiagnoseState) {
    let items: Vec<ListItem> = state
        .outcomes
        .iter()
        .map(|o| {
            ListItem::new(Line::from(vec![
                Span::styled(
                    format!("{} ", glyph_for(&o.verdict)),
                    Style::default()
                        .fg(color_for(&o.verdict))
                        .add_modifier(Modifier::BOLD),
                ),
                Span::raw(format!("{:<22}", truncate(&o.check_id, 22))),
            ]))
        })
        .collect();

    let title = if state.outcomes.is_empty() {
        "checks  (waiting for verdicts…)".to_string()
    } else {
        format!("checks  ({})", state.outcomes.len())
    };
    let list = List::new(items)
        .block(Block::default().borders(Borders::ALL).title(title))
        .highlight_style(
            Style::default()
                .add_modifier(Modifier::REVERSED)
                .add_modifier(Modifier::BOLD),
        )
        .highlight_symbol("▸ ");
    f.render_stateful_widget(list, area, &mut state.list_state);
}

fn draw_detail(f: &mut ratatui::Frame, area: ratatui::layout::Rect, state: &DiagnoseState) {
    let selected = state.list_state.selected().and_then(|i| state.outcomes.get(i));

    let text: Text = match (selected, &state.status) {
        (Some(o), _) => {
            let mut lines: Vec<Line> = Vec::new();
            lines.push(Line::from(vec![
                Span::styled(
                    format!("{} {}", glyph_for(&o.verdict), o.verdict.to_uppercase()),
                    Style::default()
                        .fg(color_for(&o.verdict))
                        .add_modifier(Modifier::BOLD),
                ),
                Span::raw(format!("  {}", o.check_id)),
            ]));
            lines.push(Line::raw(""));
            lines.push(Line::raw(o.summary.clone()));
            if !o.findings.is_empty() {
                lines.push(Line::raw(""));
                lines.push(Line::styled(
                    "findings",
                    Style::default().add_modifier(Modifier::BOLD | Modifier::UNDERLINED),
                ));
                for fnd in &o.findings {
                    lines.push(Line::from(vec![
                        Span::styled(
                            format!("• ({}) ", fnd.severity),
                            Style::default().fg(severity_color(&fnd.severity)),
                        ),
                        Span::styled(
                            format!("{}: ", fnd.label),
                            Style::default().add_modifier(Modifier::BOLD),
                        ),
                        Span::raw(fnd.detail.clone()),
                    ]));
                }
            }
            if !o.remediation.is_empty() {
                lines.push(Line::raw(""));
                lines.push(Line::styled(
                    "remediation",
                    Style::default().add_modifier(Modifier::BOLD | Modifier::UNDERLINED),
                ));
                for r in &o.remediation {
                    lines.push(Line::from(vec![Span::styled(
                        format!("→ {}", r.title),
                        Style::default().add_modifier(Modifier::BOLD),
                    )]));
                    lines.push(Line::raw(format!("  {}", r.detail)));
                }
            }
            Text::from(lines)
        }
        (None, Status::Error(e)) => Text::from(vec![
            Line::styled("stream error", Style::default().fg(Color::Red).add_modifier(Modifier::BOLD)),
            Line::raw(""),
            Line::raw(e.clone()),
        ]),
        (None, _) => Text::from(vec![Line::styled(
            "waiting for the first verdict…",
            Style::default().add_modifier(Modifier::DIM),
        )]),
    };

    let p = Paragraph::new(text)
        .block(Block::default().borders(Borders::ALL).title("detail"))
        .wrap(Wrap { trim: false })
        .scroll((state.detail_scroll, 0));
    f.render_widget(p, area);
}

fn draw_footer(f: &mut ratatui::Frame, area: ratatui::layout::Rect) {
    let line = Line::from(vec![Span::styled(
        " ↑/↓ (j/k) select · PgUp/PgDn scroll detail · q/Esc quit · verdicts from stethoscope (read-only) ",
        Style::default().add_modifier(Modifier::DIM),
    )]);
    f.render_widget(Paragraph::new(line), area);
}

fn severity_color(severity: &str) -> Color {
    match severity {
        "warn" => Color::Yellow,
        "info" => Color::Cyan,
        _ => Color::Gray,
    }
}

fn truncate(s: &str, max: usize) -> String {
    if s.chars().count() <= max {
        s.to_string()
    } else {
        let mut out: String = s.chars().take(max.saturating_sub(1)).collect();
        out.push('…');
        out
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::cmd::inspect::CheckOutcome;

    fn outcome(check_id: &str, verdict: &str) -> CheckOutcome {
        CheckOutcome {
            check_id: check_id.into(),
            verdict: verdict.into(),
            summary: "s".into(),
            findings: vec![],
            remediation: vec![],
        }
    }

    #[test]
    fn rollup_fail_dominates() {
        assert_eq!(
            rollup_verdict(["pass", "warn", "fail", "pass"].into_iter()),
            "fail"
        );
    }

    #[test]
    fn rollup_warn_over_pass() {
        assert_eq!(rollup_verdict(["pass", "warn", "pass"].into_iter()), "warn");
    }

    #[test]
    fn rollup_all_pass() {
        assert_eq!(rollup_verdict(["pass", "pass"].into_iter()), "pass");
    }

    #[test]
    fn rollup_skipped_when_neutral_or_empty() {
        assert_eq!(rollup_verdict(["skipped", "na"].into_iter()), "skipped");
        assert_eq!(rollup_verdict(std::iter::empty()), "skipped");
    }

    #[test]
    fn glyph_and_color_cover_all_verdicts() {
        for v in ["pass", "warn", "fail", "skipped", "na", "bogus"] {
            assert!(glyph_for(v).starts_with('['));
            let _ = color_for(v);
        }
    }

    #[test]
    fn apply_outcome_appends_and_autoselects_first() {
        let mut st = DiagnoseState::new("topic=x".into());
        assert_eq!(st.list_state.selected(), None);
        st.apply(StreamMsg::Outcome(Box::new(outcome("registration", "pass"))));
        assert_eq!(st.outcomes.len(), 1);
        assert_eq!(st.list_state.selected(), Some(0));
        st.apply(StreamMsg::Outcome(Box::new(outcome("flow-rate", "fail"))));
        // Selection stays put when more arrive.
        assert_eq!(st.list_state.selected(), Some(0));
        assert_eq!(st.rollup(), "fail");
    }

    #[test]
    fn apply_done_and_error_set_status() {
        let mut st = DiagnoseState::new("vendor=drs".into());
        st.apply(StreamMsg::Done);
        assert!(matches!(st.status, Status::Done));
        st.apply(StreamMsg::Error("boom".into()));
        assert!(matches!(st.status, Status::Error(_)));
    }

    #[test]
    fn select_wraps_both_directions() {
        let mut st = DiagnoseState::new("t".into());
        for (i, v) in ["pass", "warn", "fail"].iter().enumerate() {
            st.apply(StreamMsg::Outcome(Box::new(outcome(&format!("c{i}"), v))));
        }
        st.list_state.select(Some(0));
        st.select_prev(); // wrap to last
        assert_eq!(st.list_state.selected(), Some(2));
        st.select_next(); // wrap to first
        assert_eq!(st.list_state.selected(), Some(0));
    }

    #[test]
    fn truncate_adds_ellipsis() {
        assert_eq!(truncate("short", 22), "short");
        let t = truncate("a-very-long-check-id-that-overflows", 10);
        assert!(t.chars().count() == 10);
        assert!(t.ends_with('…'));
    }
}
