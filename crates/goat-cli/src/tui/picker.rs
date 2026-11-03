// Picker surface (Phase D) — the merged declared⊕live roster + peer pivot.
//
// Two vantage points on "what can I point at?", behind one toggle:
//   - TOPICS (default): the merged roster `cmd::ui` builds — portal-declared
//     topics joined with panoscope-live topics on the topic key — with a
//     glanceable membership column so declared-but-idle and live-but-
//     undeclared mismatches read at a glance.
//   - PEERS: the fabric-only pivot — who's online, grouped by role class
//     (Sources/Derivers/Sinks), with what each publishes/subscribes.
//
// Local substring search narrows the active list. The detail pane shows
// the selected item's facets (declared format/schema/vendor over live
// eps/pub/sub for a topic; role/vendor/pub-sub lists for a peer).
//
// This surface is a pure renderer + input loop. It computes no verdicts
// (membership marking is presentation, not a health call — see the
// delegation guardrail in cmd/ui.rs). On a lens keystroke it returns the
// selected target to `cmd::ui`, which tears this surface down and
// launches the Diagnose / Tail lens — the Block 94 "hop preserves the
// target" contract. (`/api/index` typeahead is intentionally NOT used:
// the local filter narrows the *merged* declared⊕live view, which the
// index — declared-only — cannot; see the design doc's Open Questions.)

use crossterm::{
    event::{self, Event, KeyCode, KeyEventKind, KeyModifiers},
    execute,
    terminal::{disable_raw_mode, enable_raw_mode, EnterAlternateScreen, LeaveAlternateScreen},
};
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

const TICK: Duration = Duration::from_millis(150);

/// The portal-declared facet of a roster row.
#[derive(Debug, Clone, PartialEq)]
pub(crate) struct DeclaredFacet {
    pub vendor: String,
    pub format: String,
    pub schema_ref: String,
    pub description: String,
}

/// The panoscope-live facet of a roster row.
#[derive(Debug, Clone, PartialEq)]
pub(crate) struct LiveFacet {
    pub eps: f64,
    pub pubs: usize,
    pub subs: usize,
    /// Age at snapshot time, precomputed against panoscope's OWN clock in
    /// `cmd::ui::aggregate_live`. The renderer deliberately holds no clock:
    /// deriving the age here would reintroduce the operator's NTP drift.
    pub last_age_ms: Option<i64>,
}

/// One target in the merged roster. At least one facet is always
/// present (a row exists because it was declared, live, or both).
#[derive(Debug, Clone, PartialEq)]
pub(crate) struct RosterRow {
    pub key: String,
    pub declared: Option<DeclaredFacet>,
    pub live: Option<LiveFacet>,
}

impl RosterRow {
    pub(crate) fn new(key: String) -> Self {
        Self {
            key,
            declared: None,
            live: None,
        }
    }
}

/// One peer in the fabric-only pivot (built from panoscope snapshot nodes).
#[derive(Debug, Clone, PartialEq)]
pub(crate) struct PeerRow {
    pub name: String,
    pub vendor: Option<String>,
    pub role_class: Option<String>,
    pub publishes: Vec<String>,
    pub subscribes: Vec<String>,
}

/// Which inventory the picker is showing.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum Mode {
    Topics,
    Peers,
}

/// What the picker hands back to `cmd::ui` on exit. Diagnose carries a
/// build_url-ready `topic=…`/`vendor=…` param; Tail carries a key
/// expression. `preselect` is the topic key to re-select when the lens
/// hands control back (None for peer-originated hops, which reopen in
/// topic mode).
pub(crate) enum PickerAction {
    Quit,
    Diagnose {
        target: String,
        preselect: Option<String>,
    },
    Tail {
        key_expr: String,
        preselect: Option<String>,
    },
}

/// Membership class of a row — the glanceable declared/live signal.
/// `Both` is the healthy shape; the two single-facet classes are the
/// high-value mismatches. Pure → unit-tested.
fn membership(row: &RosterRow) -> &'static str {
    match (row.declared.is_some(), row.live.is_some()) {
        (true, true) => "both",
        (true, false) => "idle",       // declared, never live
        (false, true) => "undeclared", // live, never declared
        (false, false) => "—",
    }
}

fn membership_glyph(row: &RosterRow) -> (&'static str, Color) {
    match membership(row) {
        "both" => ("✓✓", Color::Green),
        "idle" => ("✓·", Color::Yellow),
        "undeclared" => ("·✓", Color::Magenta),
        _ => ("··", Color::DarkGray),
    }
}

/// Indices of items whose key string contains `query` (case-insensitive).
/// Empty query → all. Generic over the active list's key extractor so it
/// serves both topic rows (by key) and peers (by name). Pure → unit-tested.
fn filter_indices<'a>(keys: impl Iterator<Item = &'a str>, query: &str) -> Vec<usize> {
    let q = query.to_lowercase();
    keys.enumerate()
        .filter(|(_, k)| q.is_empty() || k.to_lowercase().contains(&q))
        .map(|(i, _)| i)
        .collect()
}

struct PickerState {
    topics: Vec<RosterRow>,
    peers: Vec<PeerRow>,
    mode: Mode,
    /// Indices into the active-mode list after the current filter.
    filtered: Vec<usize>,
    list_state: ListState,
    query: String,
    searching: bool,
}

impl PickerState {
    fn new(topics: Vec<RosterRow>, peers: Vec<PeerRow>, preselect: Option<String>) -> Self {
        let mut s = Self {
            topics,
            peers,
            mode: Mode::Topics,
            filtered: Vec::new(),
            list_state: ListState::default(),
            query: String::new(),
            searching: false,
        };
        s.rebuild_filter();
        // Pre-select the requested topic if present, else row 0.
        if let Some(t) = preselect {
            if let Some(fpos) = s
                .topics
                .iter()
                .position(|r| r.key == t)
                .and_then(|ri| s.filtered.iter().position(|&i| i == ri))
            {
                s.list_state.select(Some(fpos));
            }
        }
        s
    }

    fn active_len(&self) -> usize {
        match self.mode {
            Mode::Topics => self.topics.len(),
            Mode::Peers => self.peers.len(),
        }
    }

    /// Recompute `filtered` for the active mode + query, clamping the
    /// selection into range (or to row 0 / None).
    fn rebuild_filter(&mut self) {
        self.filtered = match self.mode {
            Mode::Topics => filter_indices(self.topics.iter().map(|r| r.key.as_str()), &self.query),
            Mode::Peers => filter_indices(self.peers.iter().map(|p| p.name.as_str()), &self.query),
        };
        let sel = match self.list_state.selected() {
            _ if self.filtered.is_empty() => None,
            Some(s) if s < self.filtered.len() => Some(s),
            _ => Some(0),
        };
        self.list_state.select(sel);
    }

    fn toggle_mode(&mut self) {
        self.mode = match self.mode {
            Mode::Topics => Mode::Peers,
            Mode::Peers => Mode::Topics,
        };
        self.query.clear();
        self.searching = false;
        self.list_state.select(None);
        self.rebuild_filter();
        if !self.filtered.is_empty() {
            self.list_state.select(Some(0));
        }
    }

    fn selected_topic(&self) -> Option<&RosterRow> {
        if self.mode != Mode::Topics {
            return None;
        }
        let idx = *self.filtered.get(self.list_state.selected()?)?;
        self.topics.get(idx)
    }

    fn selected_peer(&self) -> Option<&PeerRow> {
        if self.mode != Mode::Peers {
            return None;
        }
        let idx = *self.filtered.get(self.list_state.selected()?)?;
        self.peers.get(idx)
    }

    fn select_next(&mut self) {
        if self.filtered.is_empty() {
            return;
        }
        let i = self.list_state.selected().unwrap_or(0);
        self.list_state.select(Some((i + 1) % self.filtered.len()));
    }

    fn select_prev(&mut self) {
        if self.filtered.is_empty() {
            return;
        }
        let n = self.filtered.len();
        let i = self.list_state.selected().unwrap_or(0);
        self.list_state.select(Some((i + n - 1) % n));
    }

    /// (declared, live, mismatch) over the topic roster — header roll-up.
    fn counts(&self) -> (usize, usize, usize) {
        let mut declared = 0;
        let mut live = 0;
        let mut mismatch = 0;
        for r in &self.topics {
            if r.declared.is_some() {
                declared += 1;
            }
            if r.live.is_some() {
                live += 1;
            }
            if r.declared.is_some() != r.live.is_some() {
                mismatch += 1;
            }
        }
        (declared, live, mismatch)
    }
}

/// Entry point. TTY gate is the caller's responsibility (done in
/// `cmd::ui::run`). Returns the operator's exit action so the caller can
/// launch a lens on the selected target.
pub(crate) fn run(
    topics: Vec<RosterRow>,
    peers: Vec<PeerRow>,
    preselect: Option<String>,
) -> anyhow::Result<PickerAction> {
    enable_raw_mode()?;
    execute!(stdout(), EnterAlternateScreen)?;
    let backend = CrosstermBackend::new(stdout());
    let mut terminal = Terminal::new(backend)?;

    let outcome = event_loop(&mut terminal, PickerState::new(topics, peers, preselect));

    disable_raw_mode()?;
    execute!(terminal.backend_mut(), LeaveAlternateScreen)?;
    terminal.show_cursor()?;

    outcome.map_err(|e| anyhow::anyhow!("picker TUI: {e}"))
}

fn event_loop(
    terminal: &mut Terminal<CrosstermBackend<std::io::Stdout>>,
    mut state: PickerState,
) -> std::io::Result<PickerAction> {
    loop {
        terminal.draw(|f| draw(f, &mut state))?;

        if !event::poll(TICK)? {
            continue;
        }
        let Event::Key(k) = event::read()? else {
            continue;
        };
        if k.kind != KeyEventKind::Press {
            continue;
        }

        if state.searching {
            // In search mode, keystrokes edit the query.
            match k.code {
                KeyCode::Esc | KeyCode::Enter => state.searching = false,
                KeyCode::Backspace => {
                    state.query.pop();
                    state.rebuild_filter();
                }
                KeyCode::Char(c) => {
                    state.query.push(c);
                    state.rebuild_filter();
                }
                KeyCode::Down => state.select_next(),
                KeyCode::Up => state.select_prev(),
                _ => {}
            }
            continue;
        }

        match (k.code, k.modifiers) {
            (KeyCode::Char('q'), _)
            | (KeyCode::Esc, _)
            | (KeyCode::Char('c'), KeyModifiers::CONTROL) => return Ok(PickerAction::Quit),
            (KeyCode::Char('/'), _) => state.searching = true,
            (KeyCode::Char('p'), _) => state.toggle_mode(),
            (KeyCode::Down, _) | (KeyCode::Char('j'), _) => state.select_next(),
            (KeyCode::Up, _) | (KeyCode::Char('k'), _) => state.select_prev(),
            (KeyCode::Char('d'), _) => {
                if let Some(a) = diagnose_action(&state) {
                    return Ok(a);
                }
            }
            (KeyCode::Char('t'), _) => {
                if let Some(a) = tail_action(&state) {
                    return Ok(a);
                }
            }
            _ => {}
        }
    }
}

/// Build the Diagnose hop for the current selection. Topic → `topic=<key>`
/// (re-select on return); peer → `vendor=<v>` if the peer has a vendor
/// (peers without one can't be diagnosed). None when nothing's selected.
fn diagnose_action(state: &PickerState) -> Option<PickerAction> {
    if let Some(r) = state.selected_topic() {
        return Some(PickerAction::Diagnose {
            target: format!("topic={}", r.key),
            preselect: Some(r.key.clone()),
        });
    }
    let p = state.selected_peer()?;
    let v = p.vendor.as_ref()?;
    Some(PickerAction::Diagnose {
        target: format!("vendor={v}"),
        preselect: None,
    })
}

/// Build the Tail hop for the current selection. Topic → the topic key;
/// peer → the peer's vendor namespace `<v>/**` (a peer publishes many
/// topics; the vendor slice is the natural live tail). None when nothing
/// tail-able is selected.
fn tail_action(state: &PickerState) -> Option<PickerAction> {
    if let Some(r) = state.selected_topic() {
        return Some(PickerAction::Tail {
            key_expr: r.key.clone(),
            preselect: Some(r.key.clone()),
        });
    }
    let p = state.selected_peer()?;
    let v = p.vendor.as_ref()?;
    Some(PickerAction::Tail {
        key_expr: format!("{v}/**"),
        preselect: None,
    })
}

fn draw(f: &mut ratatui::Frame, state: &mut PickerState) {
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
        .constraints([Constraint::Percentage(58), Constraint::Percentage(42)])
        .split(chunks[1]);

    draw_list(f, body[0], state);
    draw_detail(f, body[1], state);
    draw_footer(f, chunks[2], state);
}

fn draw_header(f: &mut ratatui::Frame, area: ratatui::layout::Rect, state: &PickerState) {
    let mut spans = vec![Span::styled("picker ", Style::default().add_modifier(Modifier::BOLD))];
    match state.mode {
        Mode::Topics => {
            let (declared, live, mismatch) = state.counts();
            spans.push(Span::raw(format!("{} topics   ", state.topics.len())));
            spans.push(Span::styled(format!("{declared} declared"), Style::default().fg(Color::Green)));
            spans.push(Span::raw(" · "));
            spans.push(Span::styled(format!("{live} live"), Style::default().fg(Color::Cyan)));
            spans.push(Span::raw(" · "));
            spans.push(Span::styled(format!("{mismatch} mismatch"), Style::default().fg(Color::Yellow)));
        }
        Mode::Peers => {
            spans.push(Span::styled("PEERS  ", Style::default().fg(Color::Cyan).add_modifier(Modifier::BOLD)));
            spans.push(Span::raw(format!("{} peers (Sources/Derivers/Sinks)", state.peers.len())));
        }
    }
    if state.searching || !state.query.is_empty() {
        spans.push(Span::raw("   "));
        spans.push(Span::styled(
            format!("/{}{}", state.query, if state.searching { "_" } else { "" }),
            Style::default().fg(Color::Cyan).add_modifier(Modifier::BOLD),
        ));
    }
    let title = match state.mode {
        Mode::Topics => "declared ⊕ live",
        Mode::Peers => "peer pivot",
    };
    let p = Paragraph::new(Line::from(spans)).block(Block::default().borders(Borders::ALL).title(title));
    f.render_widget(p, area);
}

fn draw_list(f: &mut ratatui::Frame, area: ratatui::layout::Rect, state: &mut PickerState) {
    let items: Vec<ListItem> = match state.mode {
        Mode::Topics => state
            .filtered
            .iter()
            .map(|&i| {
                let r = &state.topics[i];
                let (glyph, color) = membership_glyph(r);
                let rate = match &r.live {
                    Some(l) => format!("{:>6.0}/s", l.eps),
                    None => "      —".to_string(),
                };
                ListItem::new(Line::from(vec![
                    Span::styled(format!("{glyph} "), Style::default().fg(color).add_modifier(Modifier::BOLD)),
                    Span::styled(format!("{rate}  "), Style::default().add_modifier(Modifier::DIM)),
                    Span::raw(truncate(&r.key, 48)),
                ]))
            })
            .collect(),
        Mode::Peers => state
            .filtered
            .iter()
            .map(|&i| {
                let p = &state.peers[i];
                let role = p.role_class.as_deref().unwrap_or("?");
                ListItem::new(Line::from(vec![
                    Span::styled(
                        format!("{:<9} ", truncate(role, 9)),
                        Style::default().fg(role_color(role)).add_modifier(Modifier::BOLD),
                    ),
                    Span::styled(
                        format!("{}↑{}↓  ", p.publishes.len(), p.subscribes.len()),
                        Style::default().add_modifier(Modifier::DIM),
                    ),
                    Span::raw(truncate(&p.name, 38)),
                ]))
            })
            .collect(),
    };

    let title = format!("{} ({}/{} shown)",
        if state.mode == Mode::Topics { "targets" } else { "peers" },
        state.filtered.len(), state.active_len());
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

fn draw_detail(f: &mut ratatui::Frame, area: ratatui::layout::Rect, state: &PickerState) {
    let text: Text = match state.mode {
        Mode::Topics => topic_detail(state.selected_topic()),
        Mode::Peers => peer_detail(state.selected_peer()),
    };
    let p = Paragraph::new(text)
        .block(Block::default().borders(Borders::ALL).title("detail"))
        .wrap(Wrap { trim: false });
    f.render_widget(p, area);
}

fn topic_detail(row: Option<&RosterRow>) -> Text<'static> {
    let Some(r) = row else {
        return Text::from(Line::styled(
            "no target selected",
            Style::default().add_modifier(Modifier::DIM),
        ));
    };
    let mut lines: Vec<Line> = Vec::new();
    lines.push(Line::styled(r.key.clone(), Style::default().add_modifier(Modifier::BOLD)));
    let (glyph, color) = membership_glyph(r);
    lines.push(Line::from(vec![
        Span::raw("membership: "),
        Span::styled(format!("{glyph} {}", membership(r)), Style::default().fg(color)),
    ]));
    lines.push(Line::raw(""));
    lines.push(Line::styled(
        "declared (portal)",
        Style::default().add_modifier(Modifier::BOLD | Modifier::UNDERLINED),
    ));
    match &r.declared {
        Some(d) => {
            lines.push(Line::raw(format!("org:      {}", d.vendor)));
            lines.push(Line::raw(format!("format:   {}", if d.format.is_empty() { "—" } else { &d.format })));
            lines.push(Line::raw(format!("schema:   {}", if d.schema_ref.is_empty() { "—" } else { &d.schema_ref })));
            if !d.description.is_empty() {
                lines.push(Line::raw(format!("desc:     {}", d.description)));
            }
        }
        None => lines.push(Line::styled("not declared in the portal", Style::default().fg(Color::Magenta))),
    }
    lines.push(Line::raw(""));
    lines.push(Line::styled(
        "live (panoscope)",
        Style::default().add_modifier(Modifier::BOLD | Modifier::UNDERLINED),
    ));
    match &r.live {
        Some(l) => {
            lines.push(Line::raw(format!("rate:     {:.1}/s", l.eps)));
            // The freshness panoscope was already sending and this pane used
            // to drop on the floor. `—` when panoscope has no observation:
            // that is "unobserved", not "published at the epoch".
            lines.push(Line::raw(match l.last_age_ms {
                Some(age) => format!("last:     {}", crate::health_wire::fmt_age(age)),
                None => "last:     — (no event observed by panoscope)".to_string(),
            }));
            lines.push(Line::raw(format!("pubs:     {}", l.pubs)));
            lines.push(Line::raw(format!("subs:     {}", l.subs)));
        }
        None => lines.push(Line::styled("no live events observed", Style::default().fg(Color::Yellow))),
    }
    Text::from(lines)
}

fn peer_detail(peer: Option<&PeerRow>) -> Text<'static> {
    let Some(p) = peer else {
        return Text::from(Line::styled(
            "no peer selected",
            Style::default().add_modifier(Modifier::DIM),
        ));
    };
    let mut lines: Vec<Line> = Vec::new();
    lines.push(Line::styled(p.name.clone(), Style::default().add_modifier(Modifier::BOLD)));
    lines.push(Line::from(vec![
        Span::raw("role:     "),
        Span::styled(
            p.role_class.clone().unwrap_or_else(|| "?".into()),
            Style::default().fg(role_color(p.role_class.as_deref().unwrap_or("?"))),
        ),
    ]));
    lines.push(Line::raw(format!("org:      {}", p.vendor.as_deref().unwrap_or("—"))));
    lines.push(Line::raw(""));
    lines.push(Line::styled(
        format!("publishes ({})", p.publishes.len()),
        Style::default().add_modifier(Modifier::BOLD | Modifier::UNDERLINED),
    ));
    for t in p.publishes.iter().take(12) {
        lines.push(Line::raw(format!("↑ {t}")));
    }
    lines.push(Line::raw(""));
    lines.push(Line::styled(
        format!("subscribes ({})", p.subscribes.len()),
        Style::default().add_modifier(Modifier::BOLD | Modifier::UNDERLINED),
    ));
    for t in p.subscribes.iter().take(12) {
        lines.push(Line::raw(format!("↓ {t}")));
    }
    Text::from(lines)
}

fn draw_footer(f: &mut ratatui::Frame, area: ratatui::layout::Rect, state: &PickerState) {
    let line = if state.searching {
        " type to filter · Enter/Esc done · ↑/↓ select ".to_string()
    } else {
        match state.mode {
            Mode::Topics => " ↑/↓ select · / search · p peers · d Diagnose · t Tail · q quit · ✓✓ both · ✓· idle · ·✓ undeclared ".to_string(),
            Mode::Peers => " ↑/↓ select · / search · p topics · d Diagnose org · t Tail org/** · q quit ".to_string(),
        }
    };
    f.render_widget(
        Paragraph::new(Line::styled(line, Style::default().add_modifier(Modifier::DIM))),
        area,
    );
}

fn role_color(role: &str) -> Color {
    let r = role.to_lowercase();
    if r.contains("source") {
        Color::Green
    } else if r.contains("sink") {
        Color::Blue
    } else if r.contains("deriv") {
        Color::Magenta
    } else {
        Color::Gray
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

    /// Flatten a rendered detail pane to plain lines for assertion.
    fn detail_lines(t: &Text<'static>) -> Vec<String> {
        t.lines
            .iter()
            .map(|l| {
                l.spans
                    .iter()
                    .map(|s| s.content.as_ref())
                    .collect::<String>()
            })
            .collect()
    }

    #[test]
    fn topic_detail_renders_the_last_event_age() {
        // Regression: panoscope has always sent `last_event_ms`, `goat ui`
        // parsed it, and this pane rendered rate/pubs/subs only — the
        // freshness was carried the whole way and then dropped.
        let r = row("v/t/v1", true, true);
        let lines = detail_lines(&topic_detail(Some(&r)));
        assert!(
            lines.iter().any(|l| l == "last:     2s ago"),
            "detail pane was:\n{lines:#?}"
        );
    }

    #[test]
    fn topic_detail_marks_an_unobserved_topic_rather_than_showing_the_epoch() {
        let mut r = row("v/t/v1", true, true);
        if let Some(l) = r.live.as_mut() {
            l.last_age_ms = None;
        }
        let lines = detail_lines(&topic_detail(Some(&r)));
        assert!(
            lines
                .iter()
                .any(|l| l == "last:     — (no event observed by panoscope)"),
            "detail pane was:\n{lines:#?}"
        );
        assert!(!lines.iter().any(|l| l.contains("1970")));
    }

    fn row(key: &str, declared: bool, live: bool) -> RosterRow {
        RosterRow {
            key: key.into(),
            declared: declared.then(|| DeclaredFacet {
                vendor: "v".into(),
                format: "protobuf:X".into(),
                schema_ref: "".into(),
                description: "".into(),
            }),
            live: live.then_some(LiveFacet {
                eps: 3.0,
                pubs: 1,
                subs: 2,
                last_age_ms: Some(2_000),
            }),
        }
    }

    fn peer(name: &str, vendor: Option<&str>, role: &str) -> PeerRow {
        PeerRow {
            name: name.into(),
            vendor: vendor.map(|s| s.into()),
            role_class: Some(role.into()),
            publishes: vec!["a/1".into()],
            subscribes: vec![],
        }
    }

    #[test]
    fn membership_classes() {
        assert_eq!(membership(&row("a", true, true)), "both");
        assert_eq!(membership(&row("a", true, false)), "idle");
        assert_eq!(membership(&row("a", false, true)), "undeclared");
    }

    #[test]
    fn filter_is_case_insensitive_substring() {
        let rows = ["org11x/obs/v1", "drs/tracks"];
        assert_eq!(filter_indices(rows.iter().copied(), ""), vec![0, 1]);
        assert_eq!(filter_indices(rows.iter().copied(), "DRS"), vec![1]);
        assert_eq!(filter_indices(rows.iter().copied(), "obs"), vec![0]);
        assert!(filter_indices(rows.iter().copied(), "zzz").is_empty());
    }

    #[test]
    fn preselect_lands_on_requested_target() {
        let rows = vec![row("a/1", true, false), row("b/2", false, true), row("c/3", true, true)];
        let st = PickerState::new(rows, vec![], Some("b/2".into()));
        assert_eq!(st.selected_topic().unwrap().key, "b/2");
    }

    #[test]
    fn rebuild_filter_keeps_selection_in_range() {
        let rows = vec![row("a/1", true, false), row("a/2", true, false), row("b/3", false, true)];
        let mut st = PickerState::new(rows, vec![], None);
        st.list_state.select(Some(2));
        st.query = "a/".into();
        st.rebuild_filter();
        assert_eq!(st.filtered.len(), 2);
        assert!(st.list_state.selected().unwrap() < 2);
    }

    #[test]
    fn counts_tally_declared_live_mismatch() {
        let rows = vec![row("both", true, true), row("idle", true, false), row("undecl", false, true)];
        let st = PickerState::new(rows, vec![], None);
        assert_eq!(st.counts(), (2, 2, 2));
    }

    #[test]
    fn toggle_mode_switches_active_list_and_selection() {
        let rows = vec![row("t/1", true, true)];
        let peers = vec![peer("acme", Some("v"), "source"), peer("zed", None, "sink")];
        let mut st = PickerState::new(rows, peers, None);
        assert_eq!(st.mode, Mode::Topics);
        assert!(st.selected_topic().is_some());
        assert!(st.selected_peer().is_none());
        st.toggle_mode();
        assert_eq!(st.mode, Mode::Peers);
        assert_eq!(st.filtered.len(), 2);
        assert!(st.selected_peer().is_some());
        assert!(st.selected_topic().is_none());
    }

    #[test]
    fn peer_hops_target_vendor_and_skip_when_absent() {
        let peers = vec![peer("acme", Some("org11x"), "source")];
        let mut st = PickerState::new(vec![], peers, None);
        st.toggle_mode();
        match diagnose_action(&st) {
            Some(PickerAction::Diagnose { target, .. }) => assert_eq!(target, "vendor=org11x"),
            _ => panic!("expected vendor diagnose"),
        }
        match tail_action(&st) {
            Some(PickerAction::Tail { key_expr, .. }) => assert_eq!(key_expr, "org11x/**"),
            _ => panic!("expected vendor tail"),
        }
        // a peer with no vendor can't be hopped
        let mut st2 = PickerState::new(vec![], vec![peer("z", None, "sink")], None);
        st2.toggle_mode();
        assert!(diagnose_action(&st2).is_none());
        assert!(tail_action(&st2).is_none());
    }

    #[test]
    fn topic_hops_carry_param_and_preselect() {
        let st = PickerState::new(vec![row("org11x/obs/v1", true, true)], vec![], None);
        match diagnose_action(&st) {
            Some(PickerAction::Diagnose { target, preselect }) => {
                assert_eq!(target, "topic=org11x/obs/v1");
                assert_eq!(preselect.as_deref(), Some("org11x/obs/v1"));
            }
            _ => panic!("expected topic diagnose"),
        }
        match tail_action(&st) {
            Some(PickerAction::Tail { key_expr, .. }) => assert_eq!(key_expr, "org11x/obs/v1"),
            _ => panic!("expected topic tail"),
        }
    }
}
