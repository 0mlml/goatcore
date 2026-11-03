// Tail lens (Phase C) — the interactive form of `goat sub`.
//
// Renders flashlight's focused live slice for a key expression as a
// full-screen ratatui surface: a live event table, a rolling rate
// sparkline, and an on-demand decoded/hex payload viewer for the
// selected row. The operator can pause the auto-following tail, scroll
// back through recent events, and inspect any held sample's bytes.
//
// DELEGATION GUARDRAIL (docs/design/goat-cli-tui.md): the decode logic
// is not forked here. Bytes are decoded through the portal's `.fds`
// descriptor via `cmd::decode::DecodeCache` — the same select-by-
// declared-format path `goat sub --decode` uses. The lens only renders.
//
// DIRECT-SUBSCRIBER WRINKLE: unlike flashlight's web Tail (which keeps
// payload bytes off the wire and fetches them on demand from
// `/api/sample`), goat-cli is itself the Zenoh subscriber, so each
// sample arrives with its bytes already in hand. We therefore keep a
// bounded in-memory ring of recent samples (with a per-sample byte cap)
// and decode lazily on row-select — only the bytes the operator
// actually looks at pay the decode cost. No `/api/sample` endpoint is
// involved.
//
// Concurrency: a producer task owns the Zenoh subscriber and feeds
// samples into an unbounded channel; a decoder task owns the
// `DecodeCache` and answers decode-requests; the ratatui event loop
// runs synchronously on a blocking thread (`spawn_blocking`) and drains
// both result streams each tick, requesting a decode the first time an
// undecoded row is selected. `UnboundedSender::send` is synchronous, so
// the blocking UI loop can dispatch decode-requests without an await.

use crate::cmd::decode::DecodeCache;
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
    widgets::{Block, Borders, List, ListItem, ListState, Paragraph, Sparkline, Wrap},
    Terminal,
};
use std::collections::{HashMap, HashSet, VecDeque};
use std::io::stdout;
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};
use tokio::sync::mpsc::{self, UnboundedReceiver, UnboundedSender};

/// Poll cadence for the input loop — also the redraw interval when new
/// samples arrive over the channel with no key event.
const TICK: Duration = Duration::from_millis(120);
/// Bounded recent-sample ring. Older samples evict from the front; a
/// selection held on an evicted sample degrades to "evicted" rather
/// than erroring (the Phase C live-fire gate).
const RING_CAP: usize = 1000;
/// Per-sample byte cap retained in the ring. Typical fabric telemetry
/// is well under this; larger payloads are truncated for display and
/// decode (noted in the viewer). Bounds worst-case memory at
/// RING_CAP × SAMPLE_CAP.
const SAMPLE_CAP: usize = 8 * 1024;
/// Rate sparkline window, in seconds.
const RATE_WINDOW: usize = 60;
/// Max bytes rendered in the hexdump pane (keeps the draw cheap).
const HEX_MAX_BYTES: usize = 512;

/// What the producer / decoder tasks send to the UI loop.
enum TailMsg {
    Sample(Box<Sample>),
    Decoded { seq: u64, text: String },
    Error(String),
}

/// A decode request the UI loop dispatches to the decoder task on the
/// first selection of an undecoded row.
struct DecodeReq {
    seq: u64,
    key: String,
    bytes: Vec<u8>,
}

/// One received sample, retained in the ring. `bytes` is capped to
/// `SAMPLE_CAP`; `len` is the true on-wire length so the table and
/// viewer can flag truncation.
struct Sample {
    seq: u64,
    key: String,
    bytes: Vec<u8>,
    len: usize,
    ts: SystemTime,
}

/// Producer: own the session, declare the subscriber, and forward every
/// sample to the UI channel with a monotonic seq. A declare or recv
/// error becomes a `TailMsg::Error` so the UI surfaces it rather than
/// silently stalling.
async fn produce(
    session: zenoh::Session,
    router: String,
    pattern: String,
    tx: UnboundedSender<TailMsg>,
) {
    let subscriber = match session.declare_subscriber(pattern.clone()).await {
        Ok(s) => s,
        Err(e) => {
            // Classify before sending — the status line truncates to 60
            // chars (a one-line UI element), so a raw multi-line zenoh error
            // used to arrive here and get sliced at an arbitrary byte
            // offset. A classified goat-authored summary is short and
            // meaningful within that budget instead.
            let line = crate::classify::classify_zenoh_err_line(e, &router, "declare a subscriber");
            let _ = tx.send(TailMsg::Error(format!("`{pattern}`: {line}")));
            return;
        }
    };
    let mut seq: u64 = 0;
    loop {
        match subscriber.recv_async().await {
            Ok(sample) => {
                let key = sample.key_expr().as_str().to_string();
                let full = sample.payload().to_bytes();
                let len = full.len();
                let mut bytes = full.to_vec();
                bytes.truncate(SAMPLE_CAP);
                if tx
                    .send(TailMsg::Sample(Box::new(Sample {
                        seq,
                        key,
                        bytes,
                        len,
                        ts: SystemTime::now(),
                    })))
                    .is_err()
                {
                    return; // UI gone — stop.
                }
                seq = seq.wrapping_add(1);
            }
            Err(e) => {
                let line = crate::classify::classify_zenoh_err_line(e, &router, "receive a sample");
                let _ = tx.send(TailMsg::Error(format!("recv: {line}")));
                return;
            }
        }
    }
}

/// Decoder: own the `DecodeCache`, answer decode-requests, forward the
/// JSON (or `DECODE-ERR: …`) string back keyed by seq.
async fn decode_worker(
    mut cache: DecodeCache,
    mut req_rx: UnboundedReceiver<DecodeReq>,
    tx: UnboundedSender<TailMsg>,
) {
    while let Some(req) = req_rx.recv().await {
        let text = cache.decode(&req.key, &req.bytes).await;
        if tx.send(TailMsg::Decoded { seq: req.seq, text }).is_err() {
            return;
        }
    }
}

/// Entry point. The TTY gate and session/decoder construction are the
/// caller's responsibility (done in `cmd::pubsub::run_sub` before any
/// session open, so a non-TTY refusal needs no mesh). `decode` is
/// `None` when the profile carries no portal — the lens then runs
/// hex-only and says so.
// The disallowed-methods clippy lint (clippy.toml / ADR 0709 D7 layer 3)
// pins goat-relay's hot-path tasks to one shard via `spawn_local`. goat-cli
// is an admin-path operator binary on the multi-thread runtime — task
// migration is a non-issue here, and the producer (Zenoh subscriber) and
// decoder must run on worker threads so the `spawn_blocking` ratatui loop
// can block on input polling without starving them. This is exactly the
// "non-hot-path use requires explicit allow + review" case the config
// names. Same shape as the Diagnose lens.
#[allow(clippy::disallowed_methods)]
pub(crate) async fn run(
    session: zenoh::Session,
    router: String,
    decode: Option<DecodeCache>,
    pattern: String,
) -> anyhow::Result<()> {
    let (ui_tx, ui_rx) = mpsc::unbounded_channel::<TailMsg>();

    let producer = tokio::spawn(produce(session, router, pattern.clone(), ui_tx.clone()));

    let (decode_available, req_tx, decoder) = match decode {
        Some(cache) => {
            let (req_tx, req_rx) = mpsc::unbounded_channel::<DecodeReq>();
            let d = tokio::spawn(decode_worker(cache, req_rx, ui_tx.clone()));
            (true, Some(req_tx), Some(d))
        }
        None => (false, None, None),
    };
    drop(ui_tx); // the spawned tasks hold the live clones.

    let handle =
        tokio::task::spawn_blocking(move || run_loop(pattern, ui_rx, req_tx, decode_available));
    let result = handle
        .await
        .map_err(|e| anyhow::anyhow!("tail TUI thread panicked: {e}"))?;

    producer.abort();
    if let Some(d) = decoder {
        d.abort();
    }
    result
}

/// Stream status shown in the header.
enum Status {
    Streaming,
    Error(String),
}

struct TailState {
    pattern: String,
    ring: VecDeque<Sample>,
    /// seq → decoded JSON (or `DECODE-ERR: …`).
    decoded: HashMap<u64, String>,
    /// seqs with a decode-request already in flight (no duplicates).
    requested: HashSet<u64>,
    /// Arrival instants within the rate window, for the sparkline.
    rate: VecDeque<Instant>,
    /// Auto-follow the newest sample. Cleared when the operator scrolls
    /// up or pauses; re-armed by scrolling past the newest or toggling.
    follow: bool,
    selected_seq: Option<u64>,
    status: Status,
    list_state: ListState,
    decode_available: bool,
    total: u64,
}

impl TailState {
    fn new(pattern: String, decode_available: bool) -> Self {
        Self {
            pattern,
            ring: VecDeque::new(),
            decoded: HashMap::new(),
            requested: HashSet::new(),
            rate: VecDeque::new(),
            follow: true,
            selected_seq: None,
            status: Status::Streaming,
            list_state: ListState::default(),
            decode_available,
            total: 0,
        }
    }

    /// Fold one received sample into the ring, evicting the oldest (and
    /// its decode bookkeeping) past the cap. Extracted from the loop so
    /// it's unit-testable without a terminal or a clock.
    fn apply_sample(&mut self, s: Sample) {
        self.total = self.total.wrapping_add(1);
        self.ring.push_back(s);
        while self.ring.len() > RING_CAP {
            if let Some(ev) = self.ring.pop_front() {
                self.decoded.remove(&ev.seq);
                self.requested.remove(&ev.seq);
            }
        }
    }

    fn apply_decoded(&mut self, seq: u64, text: String) {
        self.requested.remove(&seq);
        self.decoded.insert(seq, text);
    }

    /// While following, the selection rides the newest sample.
    fn sync_follow(&mut self) {
        if self.follow {
            self.selected_seq = self.ring.back().map(|s| s.seq);
        }
    }

    fn current_index(&self) -> Option<usize> {
        let seq = self.selected_seq?;
        self.ring.iter().position(|s| s.seq == seq)
    }

    fn select_prev(&mut self) {
        self.follow = false;
        if self.ring.is_empty() {
            return;
        }
        let i = self.current_index().unwrap_or(self.ring.len().saturating_sub(1));
        let ni = i.saturating_sub(1);
        self.selected_seq = self.ring.get(ni).map(|s| s.seq);
    }

    fn select_next(&mut self) {
        if self.ring.is_empty() {
            return;
        }
        let i = self.current_index().unwrap_or(0);
        let ni = i + 1;
        if ni >= self.ring.len() - 1 {
            // Caught up to the newest (live edge) — resume following.
            self.follow = true;
            self.selected_seq = self.ring.back().map(|s| s.seq);
        } else {
            self.follow = false;
            self.selected_seq = self.ring.get(ni).map(|s| s.seq);
        }
    }

    fn toggle_follow(&mut self) {
        self.follow = !self.follow;
        if self.follow {
            self.selected_seq = self.ring.back().map(|s| s.seq);
        }
    }

    /// If the selected row has bytes in the ring and no decode yet (or
    /// in flight), return the request the loop should dispatch. Returns
    /// `None` when decode is unavailable, nothing is selected, the
    /// sample has evicted, or a decode already exists / is pending.
    fn pending_decode(&self) -> Option<DecodeReq> {
        if !self.decode_available {
            return None;
        }
        let seq = self.selected_seq?;
        if self.decoded.contains_key(&seq) || self.requested.contains(&seq) {
            return None;
        }
        let s = self.ring.iter().find(|s| s.seq == seq)?;
        Some(DecodeReq {
            seq,
            key: s.key.clone(),
            bytes: s.bytes.clone(),
        })
    }

    fn selected_sample(&self) -> Option<&Sample> {
        let seq = self.selected_seq?;
        self.ring.iter().find(|s| s.seq == seq)
    }
}

/// `true` if the decode result string is the error sentinel
/// `DecodeCache` produces rather than decoded JSON. Drives the viewer's
/// "fall back to hex" branch.
fn decoded_is_error(s: &str) -> bool {
    s.starts_with("DECODE-ERR")
}

/// Classify a payload by its first non-whitespace byte into the same
/// glanceable icon vocabulary flashlight uses: `{}` JSON object, `[]`
/// JSON array, `txt` printable text, `0x` binary/protobuf, `·` empty.
fn classify(bytes: &[u8]) -> &'static str {
    match bytes.iter().copied().find(|b| !b.is_ascii_whitespace()) {
        None => "·",
        Some(b'{') => "{}",
        Some(b'[') => "[]",
        Some(_) if is_printable(bytes) => "txt",
        Some(_) => "0x",
    }
}

fn is_printable(bytes: &[u8]) -> bool {
    match std::str::from_utf8(bytes) {
        Ok(s) => s
            .chars()
            .all(|c| !c.is_control() || c == '\n' || c == '\t' || c == '\r'),
        Err(_) => false,
    }
}

/// Bucket sample ages (seconds-ago) into per-second counts over a
/// window. The returned vec has `window` entries, oldest first and
/// newest last (so a sparkline reads left-to-right as time → now).
/// Pure over the ages so it's testable without a clock.
fn bucket_ages(ages: impl Iterator<Item = u64>, window: usize) -> Vec<u64> {
    let mut counts = vec![0u64; window];
    for age in ages {
        let a = age as usize;
        if a < window {
            counts[window - 1 - a] += 1;
        }
    }
    counts
}

/// HH:MM:SS (UTC) from a SystemTime, no chrono dependency.
fn hms(ts: SystemTime) -> String {
    let secs = ts.duration_since(UNIX_EPOCH).map(|d| d.as_secs()).unwrap_or(0);
    format!(
        "{:02}:{:02}:{:02}",
        (secs / 3600) % 24,
        (secs / 60) % 60,
        secs % 60
    )
}

/// Set up the terminal, run the event loop, and guarantee teardown even
/// on error so a panic doesn't leave the operator's terminal in raw
/// mode. Mirrors the Diagnose lens.
fn run_loop(
    pattern: String,
    ui_rx: UnboundedReceiver<TailMsg>,
    req_tx: Option<UnboundedSender<DecodeReq>>,
    decode_available: bool,
) -> anyhow::Result<()> {
    enable_raw_mode()?;
    execute!(stdout(), EnterAlternateScreen)?;
    let backend = CrosstermBackend::new(stdout());
    let mut terminal = Terminal::new(backend)?;

    let outcome = event_loop(&mut terminal, pattern, ui_rx, req_tx, decode_available);

    disable_raw_mode()?;
    execute!(terminal.backend_mut(), LeaveAlternateScreen)?;
    terminal.show_cursor()?;

    outcome.map_err(|e| anyhow::anyhow!("tail TUI: {e}"))
}

fn event_loop(
    terminal: &mut Terminal<CrosstermBackend<std::io::Stdout>>,
    pattern: String,
    mut ui_rx: UnboundedReceiver<TailMsg>,
    req_tx: Option<UnboundedSender<DecodeReq>>,
    decode_available: bool,
) -> std::io::Result<()> {
    let mut state = TailState::new(pattern, decode_available);

    loop {
        // Drain everything the producer / decoder queued since last tick.
        while let Ok(msg) = ui_rx.try_recv() {
            match msg {
                TailMsg::Sample(s) => {
                    state.apply_sample(*s);
                    state.rate.push_back(Instant::now());
                }
                TailMsg::Decoded { seq, text } => state.apply_decoded(seq, text),
                TailMsg::Error(e) => state.status = Status::Error(e),
            }
        }

        // Expire rate-window entries older than the window.
        let now = Instant::now();
        while let Some(&front) = state.rate.front() {
            if now.duration_since(front).as_secs() >= RATE_WINDOW as u64 {
                state.rate.pop_front();
            } else {
                break;
            }
        }

        state.sync_follow();

        // First time a row is selected, ask the decoder for its bytes.
        if let Some(tx) = &req_tx {
            if let Some(req) = state.pending_decode() {
                state.requested.insert(req.seq);
                let _ = tx.send(req);
            }
        }

        terminal.draw(|f| draw(f, &mut state))?;

        if event::poll(TICK)? {
            if let Event::Key(k) = event::read()? {
                if k.kind != KeyEventKind::Press {
                    continue;
                }
                match (k.code, k.modifiers) {
                    // Quitting a read-only viewer is success (exit 0).
                    (KeyCode::Char('q'), _)
                    | (KeyCode::Esc, _)
                    | (KeyCode::Char('c'), KeyModifiers::CONTROL) => return Ok(()),
                    (KeyCode::Down, _) | (KeyCode::Char('j'), _) => state.select_next(),
                    (KeyCode::Up, _) | (KeyCode::Char('k'), _) => state.select_prev(),
                    (KeyCode::Char(' '), _) | (KeyCode::Char('p'), _) => state.toggle_follow(),
                    _ => {}
                }
            }
        }
    }
}

fn draw(f: &mut ratatui::Frame, state: &mut TailState) {
    let chunks = Layout::default()
        .direction(Direction::Vertical)
        .constraints([
            Constraint::Length(3), // header
            Constraint::Length(3), // rate sparkline
            Constraint::Min(0),    // body (table | viewer)
            Constraint::Length(1), // footer
        ])
        .split(f.area());

    draw_header(f, chunks[0], state);
    draw_rate(f, chunks[1], state);

    let body = Layout::default()
        .direction(Direction::Horizontal)
        .constraints([Constraint::Percentage(55), Constraint::Percentage(45)])
        .split(chunks[2]);

    draw_table(f, body[0], state);
    draw_viewer(f, body[1], state);
    draw_footer(f, chunks[3], state);
}

fn draw_header(f: &mut ratatui::Frame, area: ratatui::layout::Rect, state: &TailState) {
    let (status_label, status_color) = match &state.status {
        Status::Streaming => {
            if state.follow {
                ("following".to_string(), Color::Cyan)
            } else {
                ("paused".to_string(), Color::Yellow)
            }
        }
        Status::Error(e) => (format!("error: {}", truncate(e, 60)), Color::Red),
    };
    // events-per-second right now = the newest 1s bucket.
    let now = Instant::now();
    let last_sec = state
        .rate
        .iter()
        .filter(|t| now.duration_since(**t).as_secs() < 1)
        .count();
    let line = Line::from(vec![
        Span::styled("tail ", Style::default().add_modifier(Modifier::BOLD)),
        Span::raw(state.pattern.clone()),
        Span::raw(format!("   {} events   ", state.total)),
        Span::raw(format!("{last_sec}/s   ")),
        Span::styled(status_label, Style::default().fg(status_color)),
    ]);
    let p =
        Paragraph::new(line).block(Block::default().borders(Borders::ALL).title("flashlight"));
    f.render_widget(p, area);
}

fn draw_rate(f: &mut ratatui::Frame, area: ratatui::layout::Rect, state: &TailState) {
    let now = Instant::now();
    let buckets = bucket_ages(
        state.rate.iter().map(|t| now.duration_since(*t).as_secs()),
        RATE_WINDOW,
    );
    let max = buckets.iter().copied().max().unwrap_or(0);
    let spark = Sparkline::default()
        .block(
            Block::default()
                .borders(Borders::ALL)
                .title(format!("rate (last {RATE_WINDOW}s · peak {max}/s)")),
        )
        .data(&buckets)
        .style(Style::default().fg(Color::Cyan));
    f.render_widget(spark, area);
}

fn draw_table(f: &mut ratatui::Frame, area: ratatui::layout::Rect, state: &mut TailState) {
    let items: Vec<ListItem> = state
        .ring
        .iter()
        .map(|s| {
            let trunc = if s.len > s.bytes.len() { "+" } else { " " };
            ListItem::new(Line::from(vec![
                Span::styled(
                    format!("{:>6} ", s.seq),
                    Style::default().add_modifier(Modifier::DIM),
                ),
                Span::raw(format!("{} ", hms(s.ts))),
                Span::styled(
                    format!("{:<2} ", classify(&s.bytes)),
                    Style::default().fg(icon_color(classify(&s.bytes))),
                ),
                Span::raw(format!("{:>6}{} ", s.len, trunc)),
                Span::raw(truncate(&s.key, 28)),
            ]))
        })
        .collect();

    // Map the stable selected-seq onto a list index for the highlight.
    state.list_state.select(state.current_index());

    let title = format!("events  ({} held, cap {RING_CAP})", state.ring.len());
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

fn draw_viewer(f: &mut ratatui::Frame, area: ratatui::layout::Rect, state: &TailState) {
    let panes = Layout::default()
        .direction(Direction::Vertical)
        .constraints([Constraint::Percentage(50), Constraint::Percentage(50)])
        .split(area);

    draw_decoded(f, panes[0], state);
    draw_hex(f, panes[1], state);
}

fn draw_decoded(f: &mut ratatui::Frame, area: ratatui::layout::Rect, state: &TailState) {
    let text: Text = match state.selected_seq {
        None => Text::from(Line::styled(
            "waiting for the first event…",
            Style::default().add_modifier(Modifier::DIM),
        )),
        Some(seq) if state.selected_sample().is_none() => Text::from(vec![
            Line::styled(
                "sample evicted from the live ring",
                Style::default().fg(Color::DarkGray).add_modifier(Modifier::BOLD),
            ),
            Line::raw(""),
            Line::raw(format!(
                "seq {seq} aged out past the {RING_CAP}-sample cap. Pause (space) sooner to hold a sample for inspection."
            )),
        ]),
        Some(_) if !state.decode_available => Text::from(vec![
            Line::styled(
                "decode unavailable",
                Style::default().fg(Color::DarkGray).add_modifier(Modifier::BOLD),
            ),
            Line::raw(""),
            Line::raw("no portal in this profile — showing raw bytes only (hex pane below)."),
        ]),
        Some(seq) => match state.decoded.get(&seq) {
            None => Text::from(Line::styled(
                "decoding…",
                Style::default().fg(Color::Cyan),
            )),
            Some(s) if decoded_is_error(s) => Text::from(vec![
                Line::styled(
                    "decode failed — raw bytes below",
                    Style::default().fg(Color::Yellow).add_modifier(Modifier::BOLD),
                ),
                Line::raw(""),
                Line::raw(s.clone()),
            ]),
            Some(s) => Text::from(s.clone()),
        },
    };
    let p = Paragraph::new(text)
        .block(Block::default().borders(Borders::ALL).title("decoded"))
        .wrap(Wrap { trim: false });
    f.render_widget(p, area);
}

fn draw_hex(f: &mut ratatui::Frame, area: ratatui::layout::Rect, state: &TailState) {
    let body = match state.selected_sample() {
        Some(s) => {
            let shown = &s.bytes[..s.bytes.len().min(HEX_MAX_BYTES)];
            let mut text = hexdump(shown);
            if s.bytes.len() > HEX_MAX_BYTES || s.len > s.bytes.len() {
                text.push_str(&format!(
                    "\n… {} of {} bytes shown",
                    shown.len(),
                    s.len
                ));
            }
            text
        }
        None => String::from("—"),
    };
    let p = Paragraph::new(body)
        .block(Block::default().borders(Borders::ALL).title("hex"))
        .wrap(Wrap { trim: false });
    f.render_widget(p, area);
}

fn draw_footer(f: &mut ratatui::Frame, area: ratatui::layout::Rect, state: &TailState) {
    let follow = if state.follow { "following" } else { "paused" };
    let line = Line::from(vec![Span::styled(
        format!(" ↑/↓ (j/k) select · space/p {follow} · q/Esc quit · live slice from the fabric (read-only) "),
        Style::default().add_modifier(Modifier::DIM),
    )]);
    f.render_widget(Paragraph::new(line), area);
}

/// Classic offset | hex | ascii hexdump, 16 bytes per row.
fn hexdump(bytes: &[u8]) -> String {
    use std::fmt::Write;
    let mut out = String::new();
    for (row, chunk) in bytes.chunks(16).enumerate() {
        let _ = write!(out, "{:04x}  ", row * 16);
        for (i, b) in chunk.iter().enumerate() {
            let _ = write!(out, "{b:02x} ");
            if i == 7 {
                out.push(' ');
            }
        }
        // pad short final row so the ascii column lines up.
        for i in chunk.len()..16 {
            out.push_str("   ");
            if i == 7 {
                out.push(' ');
            }
        }
        out.push(' ');
        for b in chunk {
            out.push(if b.is_ascii_graphic() || *b == b' ' {
                *b as char
            } else {
                '.'
            });
        }
        out.push('\n');
    }
    out
}

fn icon_color(icon: &str) -> Color {
    match icon {
        "{}" | "[]" => Color::Green,
        "txt" => Color::Cyan,
        "0x" => Color::Magenta,
        _ => Color::DarkGray,
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

    fn sample(seq: u64, key: &str, bytes: &[u8]) -> Sample {
        Sample {
            seq,
            key: key.into(),
            bytes: bytes.to_vec(),
            len: bytes.len(),
            ts: UNIX_EPOCH,
        }
    }

    #[test]
    fn classify_covers_each_shape() {
        assert_eq!(classify(b""), "·");
        assert_eq!(classify(b"   "), "·");
        assert_eq!(classify(b"  {\"a\":1}"), "{}");
        assert_eq!(classify(b"[1,2,3]"), "[]");
        assert_eq!(classify(b"hello world"), "txt");
        assert_eq!(classify(&[0x08, 0x96, 0x01]), "0x");
    }

    #[test]
    fn bucket_ages_newest_on_right_and_drops_out_of_window() {
        // ages 0,0,1 within a 3s window; age 5 falls outside.
        let b = bucket_ages([0u64, 0, 1, 5].into_iter(), 3);
        assert_eq!(b.len(), 3);
        assert_eq!(b[2], 2); // newest second has the two age-0 events
        assert_eq!(b[1], 1); // one age-1 event
        assert_eq!(b[0], 0);
    }

    #[test]
    fn ring_evicts_oldest_and_cleans_decode_bookkeeping() {
        let mut st = TailState::new("x/**".into(), true);
        for seq in 0..(RING_CAP as u64 + 2) {
            st.apply_sample(sample(seq, "x/a", b"{}"));
        }
        assert_eq!(st.ring.len(), RING_CAP);
        // first two seqs evicted; front is now seq 2.
        assert_eq!(st.ring.front().map(|s| s.seq), Some(2));
        assert_eq!(st.total, RING_CAP as u64 + 2);
        // a decode recorded for an evicted seq is cleaned on eviction.
        let mut st2 = TailState::new("x/**".into(), true);
        st2.apply_sample(sample(0, "x/a", b"{}"));
        st2.apply_decoded(0, "{\"v\":1}".into());
        assert!(st2.decoded.contains_key(&0));
        for seq in 1..(RING_CAP as u64 + 1) {
            st2.apply_sample(sample(seq, "x/a", b"{}"));
        }
        assert!(!st2.decoded.contains_key(&0), "evicted seq's decode dropped");
    }

    #[test]
    fn follow_tracks_newest_then_pauses_on_scroll() {
        let mut st = TailState::new("x/**".into(), true);
        assert!(st.follow);
        for seq in 0..3 {
            st.apply_sample(sample(seq, "x/a", b"{}"));
        }
        st.sync_follow();
        assert_eq!(st.selected_seq, Some(2)); // newest

        st.select_prev(); // scroll up → pause, hold seq 1
        assert!(!st.follow);
        assert_eq!(st.selected_seq, Some(1));

        // following stays off; a new sample doesn't drag the selection.
        st.apply_sample(sample(3, "x/a", b"{}"));
        st.sync_follow();
        assert_eq!(st.selected_seq, Some(1));

        st.toggle_follow(); // resume → snap to newest
        assert!(st.follow);
        st.sync_follow();
        assert_eq!(st.selected_seq, Some(3));
    }

    #[test]
    fn select_next_past_newest_resumes_follow() {
        let mut st = TailState::new("x/**".into(), true);
        for seq in 0..2 {
            st.apply_sample(sample(seq, "x/a", b"{}"));
        }
        st.sync_follow();
        st.select_prev(); // now paused on seq 0
        assert!(!st.follow);
        st.select_next(); // back to seq 1 (the newest) → resume follow
        assert!(st.follow);
        assert_eq!(st.selected_seq, Some(1));
    }

    #[test]
    fn pending_decode_requests_once_then_suppresses() {
        let mut st = TailState::new("x/**".into(), true);
        st.apply_sample(sample(0, "x/a", b"{}"));
        st.sync_follow();
        // first look: a request is owed.
        let req = st.pending_decode().expect("decode owed");
        assert_eq!(req.seq, 0);
        st.requested.insert(req.seq);
        // in flight → suppressed.
        assert!(st.pending_decode().is_none());
        // once decoded → still suppressed.
        st.apply_decoded(0, "{\"v\":1}".into());
        assert!(st.pending_decode().is_none());
    }

    #[test]
    fn pending_decode_none_when_decode_unavailable() {
        let mut st = TailState::new("x/**".into(), false);
        st.apply_sample(sample(0, "x/a", b"{}"));
        st.sync_follow();
        assert!(st.pending_decode().is_none());
    }

    #[test]
    fn decoded_is_error_distinguishes_json_from_sentinel() {
        assert!(decoded_is_error("DECODE-ERR: no schema for `x`"));
        assert!(!decoded_is_error("{\"v\":1}"));
        assert!(!decoded_is_error("[1,2,3]"));
    }

    #[test]
    fn hms_formats_utc_clock() {
        // 01:01:01 UTC = 3661 seconds past the epoch.
        assert_eq!(hms(UNIX_EPOCH + Duration::from_secs(3661)), "01:01:01");
    }

    #[test]
    fn hexdump_renders_offset_and_ascii() {
        let dump = hexdump(b"AB");
        assert!(dump.starts_with("0000  41 42 "));
        assert!(dump.trim_end().ends_with("AB"));
    }

    #[test]
    fn truncate_adds_ellipsis() {
        assert_eq!(truncate("short", 28), "short");
        let t = truncate("a-very-long-topic-key-that-overflows-the-column", 10);
        assert_eq!(t.chars().count(), 10);
        assert!(t.ends_with('…'));
    }
}
