//! The pure half of `bound`: state, one fixed-timestep step, and a renderer
//! that turns a state into text. No terminal, no clock, no randomness beyond
//! a seeded generator, so every rule of play is a plain unit test.
//!
//! Units: x in columns, heights in rows above the ledge, time in seconds.

/// The smallest terminal the field fits in.
pub const MIN_W: u16 = 50;
pub const MIN_H: u16 = 12;
/// The fixed timestep: 30 steps a second.
pub const DT: f32 = 1.0 / 30.0;
/// The goat's left edge and width in columns.
pub const GOAT_X: f32 = 6.0;
pub const GOAT_W: f32 = 5.0;
/// Every this many points the sky turns, the way the offline dinosaur's does.
pub const NIGHT_EVERY: u32 = 700;

const GRAVITY: f32 = 40.0;
const JUMP_V: f32 = 15.0;
const FAST_FALL_V: f32 = -10.0;
const DUCK_HOLD: f32 = 0.4;
const START_SPEED: f32 = 18.0;
const MAX_SPEED: f32 = 40.0;
const SPEED_PER_100: f32 = 0.6;
const SCORE_PER_COL: f32 = 0.5;
/// Hitboxes are a little smaller than the art, so a near miss is a miss.
const FORGIVE: f32 = 0.5;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Kind {
    Rock,
    BigRock,
    Gap,
    Eagle,
}

impl Kind {
    pub fn width(self) -> f32 {
        match self {
            Kind::Rock => 2.0,
            Kind::BigRock => 3.0,
            Kind::Gap => 4.0,
            Kind::Eagle => 3.0,
        }
    }
    /// The heights it occupies, `[low, high)` in rows above the ledge; a gap
    /// occupies none (it is a hole in the ledge instead).
    pub fn levels(self) -> Option<(f32, f32)> {
        match self {
            Kind::Rock => Some((0.0, 1.0)),
            Kind::BigRock => Some((0.0, 2.0)),
            Kind::Gap => None,
            Kind::Eagle => Some((1.0, 2.0)),
        }
    }
}

#[derive(Clone, Copy, Debug, PartialEq)]
pub struct Obstacle {
    pub x: f32,
    pub kind: Kind,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Input {
    None,
    Jump,
    Duck,
}

#[derive(Clone, Debug)]
pub struct State {
    pub field_w: u16,
    /// Height of the goat's hooves above the ledge, and its vertical speed.
    pub y: f32,
    pub vy: f32,
    /// Seconds of crouch left (a terminal sends no key-up, so a press holds
    /// the duck briefly and key-repeat extends it).
    pub duck: f32,
    pub obstacles: Vec<Obstacle>,
    pub speed: f32,
    pub score: f32,
    /// Best score this session; never written anywhere.
    pub best: u32,
    pub started: bool,
    pub alive: bool,
    /// Seconds since the run started; drives the leg and wing animation.
    pub t: f32,
    pub next_spawn: f32,
    /// How far the horizon has drifted (slower than the ledge: parallax).
    pub horizon: f32,
    rng: u64,
}

impl State {
    pub fn new(seed: u64, field_w: u16) -> State {
        State {
            field_w,
            y: 0.0,
            vy: 0.0,
            duck: 0.0,
            obstacles: Vec::new(),
            speed: START_SPEED,
            score: 0.0,
            best: 0,
            started: false,
            alive: true,
            t: 0.0,
            next_spawn: field_w as f32 * 0.6,
            horizon: 0.0,
            rng: seed | 1,
        }
    }

    /// A new run after a crash, keeping the session's best.
    pub fn restart(&self) -> State {
        let mut s = State::new(self.rng.wrapping_mul(6364136223846793005).wrapping_add(1), self.field_w);
        s.best = self.best.max(self.score as u32);
        s.started = true;
        s
    }

    pub fn on_ground(&self) -> bool {
        self.y <= 0.0
    }

    pub fn ducking(&self) -> bool {
        self.duck > 0.0
    }

    pub fn night(&self) -> bool {
        (self.score as u32 / NIGHT_EVERY) % 2 == 1
    }

    fn next_rand(&mut self) -> u32 {
        // xorshift64: deterministic for a seed, which is all a game needs.
        let mut x = self.rng;
        x ^= x << 13;
        x ^= x >> 7;
        x ^= x << 17;
        self.rng = x;
        (x >> 32) as u32
    }

    fn rand_unit(&mut self) -> f32 {
        self.next_rand() as f32 / u32::MAX as f32
    }

    /// Which obstacle comes next: rocks from the start, gaps after 100 points,
    /// big rocks after 150, the eagle once the pace is up after 300.
    fn pick_kind(&mut self) -> Kind {
        let r = self.rand_unit();
        let s = self.score;
        if s >= 300.0 && r < 0.18 {
            Kind::Eagle
        } else if s >= 100.0 && r < 0.36 {
            Kind::Gap
        } else if s >= 150.0 && r < 0.52 {
            Kind::BigRock
        } else {
            Kind::Rock
        }
    }
}

/// The pace for a score: up 0.6 columns a second per 100 points, capped.
pub fn speed_for(score: f32) -> f32 {
    (START_SPEED + score / 100.0 * SPEED_PER_100).min(MAX_SPEED)
}

/// Whether the goat touches this obstacle right now.
pub fn hits(s: &State, o: &Obstacle) -> bool {
    let (gx0, gx1) = (GOAT_X + FORGIVE, GOAT_X + GOAT_W - FORGIVE);
    match o.kind.levels() {
        None => {
            // A gap swallows the goat only if its hooves are on the ledge over it.
            let (fx0, fx1) = (GOAT_X + 1.5, GOAT_X + 3.5);
            s.on_ground() && o.x < fx1 && o.x + o.kind.width() > fx0
        }
        Some((lo, hi)) => {
            let (ox0, ox1) = (o.x + FORGIVE / 2.0, o.x + o.kind.width() - FORGIVE / 2.0);
            if !(ox0 < gx1 && ox1 > gx0) {
                return false;
            }
            let h = if s.ducking() && s.on_ground() { 1.0 } else { 2.0 };
            s.y < hi && s.y + h > lo
        }
    }
}

/// One fixed step of play.
pub fn step(s: &mut State, input: Input) {
    if !s.alive {
        return;
    }
    if !s.started {
        if input == Input::Jump {
            s.started = true;
        } else {
            return;
        }
    }
    match input {
        Input::Jump if s.on_ground() && !s.ducking() => s.vy = JUMP_V,
        Input::Duck => {
            s.duck = DUCK_HOLD;
            if !s.on_ground() {
                s.vy = s.vy.min(FAST_FALL_V);
            }
        }
        _ => {}
    }
    s.t += DT;
    s.vy -= GRAVITY * DT;
    s.y += s.vy * DT;
    if s.y <= 0.0 {
        s.y = 0.0;
        s.vy = 0.0;
    }
    if s.duck > 0.0 {
        s.duck -= DT;
    }

    s.speed = speed_for(s.score);
    let dx = s.speed * DT;
    for o in &mut s.obstacles {
        o.x -= dx;
    }
    s.obstacles.retain(|o| o.x + o.kind.width() > 0.0);
    s.horizon += dx * 0.1;
    s.score += dx * SCORE_PER_COL;

    s.next_spawn -= dx;
    if s.next_spawn <= 0.0 {
        let kind = s.pick_kind();
        s.obstacles.push(Obstacle { x: s.field_w as f32, kind });
        let spread = s.rand_unit() * s.speed * 0.8;
        s.next_spawn = 14.0 + s.speed * 0.5 + spread;
    }

    if s.obstacles.iter().any(|o| hits(s, o)) {
        s.alive = false;
        s.best = s.best.max(s.score as u32);
    }
}

/// One frame of text, `h` lines of exactly `w` characters, plus whether the
/// sky is dark (the terminal loop draws night in reverse video).
pub struct Frame {
    pub lines: Vec<String>,
    pub night: bool,
}

const GOAT_TOP: &str = "  __^";
const GOAT_LEGS: [&str; 2] = [" /\\/\\", " |  |"];
const GOAT_DUCK: &str = " ~__^";
const EAGLE: [&str; 2] = ["\\v/", "-v-"];
const ARGAN: [&str; 3] = [" ,%%%%, ", "%%%%%%%%", "   ||   "];

fn put(grid: &mut [Vec<char>], row: i32, col: i32, text: &str) {
    if row < 0 || row as usize >= grid.len() {
        return;
    }
    let line = &mut grid[row as usize];
    for (i, ch) in text.chars().enumerate() {
        let c = col + i as i32;
        if c >= 0 && (c as usize) < line.len() && ch != ' ' {
            line[c as usize] = ch;
        }
    }
}

/// Draw a state into a `w`×`h` frame. Row `h-2` is the ledge; the last row
/// holds the controls; the score sits top right.
pub fn render(s: &State, w: u16, h: u16) -> Frame {
    let (w, h) = (w as usize, h as usize);
    let mut grid = vec![vec![' '; w]; h];
    let ledge = h as i32 - 2;
    let night = s.night();

    // Sky: a moon and a fixed scatter of stars at night.
    if night {
        put(&mut grid, 1, w as i32 - 24, "C");
        for i in 0..(w / 6) {
            let col = (i * 37 + 11) % w;
            let row = 1 + (i * 13) % (ledge.max(4) as usize - 3);
            put(&mut grid, row as i32, col as i32, ".");
        }
    }

    // Horizon: a far ridge and an argan tree drifting by, slower than the ledge.
    let ridge_row = ledge - 4;
    for c in 0..w {
        let p = (c as f32 + s.horizon * 0.5) as i32;
        let ch = match p.rem_euclid(14) {
            3 => '/',
            4 => '\\',
            9 => '_',
            _ => ' ',
        };
        if ch != ' ' {
            put(&mut grid, ridge_row, c as i32, &ch.to_string());
        }
    }
    let span = w as f32 + 30.0;
    let tree_x = (w as f32 + 10.0 - s.horizon.rem_euclid(span)) as i32;
    for (i, row) in ARGAN.iter().enumerate() {
        put(&mut grid, ledge - 3 + i as i32 - 1, tree_x, row);
    }

    // The ledge, with its gaps.
    let mut ledge_line = vec!['='; w];
    for o in s.obstacles.iter().filter(|o| o.kind == Kind::Gap) {
        let (a, b) = (o.x.round() as i32, (o.x + o.kind.width()).round() as i32);
        for c in a.max(0)..b.min(w as i32) {
            ledge_line[c as usize] = ' ';
        }
    }
    grid[ledge as usize] = ledge_line;

    // Rocks and the eagle.
    for o in &s.obstacles {
        let col = o.x.round() as i32;
        match o.kind {
            Kind::Rock => put(&mut grid, ledge - 1, col, "##"),
            Kind::BigRock => {
                put(&mut grid, ledge - 1, col, "###");
                put(&mut grid, ledge - 2, col, "###");
            }
            Kind::Eagle => put(&mut grid, ledge - 2, col, EAGLE[((s.t * 6.0) as usize) % 2]),
            Kind::Gap => {}
        }
    }

    // The goat.
    let lift = s.y.round() as i32;
    let col = GOAT_X as i32;
    if s.ducking() && s.on_ground() {
        put(&mut grid, ledge - 1, col, GOAT_DUCK);
    } else {
        let legs = if s.on_ground() { GOAT_LEGS[((s.t * 10.0) as usize) % 2] } else { GOAT_LEGS[0] };
        put(&mut grid, ledge - 1 - lift, col, legs);
        put(&mut grid, ledge - 2 - lift, col, GOAT_TOP);
    }

    // Score, top right.
    let score = format!("HI {:05}  {:05}", s.best.max(s.score as u32), s.score as u32);
    put(&mut grid, 0, w as i32 - score.len() as i32 - 1, &score);

    // Messages and controls.
    let mid = (ledge / 2).max(1);
    if !s.started {
        center(&mut grid, mid, "press space to bound");
    } else if !s.alive {
        center(&mut grid, mid, "the goat slipped");
        center(&mut grid, mid + 1, "any key to bound again");
    }
    put(&mut grid, h as i32 - 1, 1, "space/up jump   down duck   q quit");

    Frame { lines: grid.into_iter().map(|r| r.into_iter().collect()).collect(), night }
}

fn center(grid: &mut [Vec<char>], row: i32, text: &str) {
    let w = grid.first().map(|r| r.len()).unwrap_or(0) as i32;
    put(grid, row, (w - text.len() as i32) / 2, text);
}

#[cfg(test)]
mod tests {
    use super::*;

    fn running(seed: u64) -> State {
        let mut s = State::new(seed, 80);
        step(&mut s, Input::Jump);
        // land again so tests start on the ground
        for _ in 0..60 {
            step(&mut s, Input::None);
        }
        s.obstacles.clear();
        s.next_spawn = 1e9;
        s
    }

    #[test]
    fn nothing_moves_until_the_first_jump() {
        let mut s = State::new(7, 80);
        for _ in 0..30 {
            step(&mut s, Input::None);
        }
        assert!(!s.started);
        assert_eq!(s.score, 0.0);
        step(&mut s, Input::Jump);
        assert!(s.started && s.vy > 0.0);
    }

    #[test]
    fn the_jump_arc_rises_peaks_and_lands() {
        let mut s = running(1);
        step(&mut s, Input::Jump);
        let mut peak: f32 = 0.0;
        let mut steps = 0;
        while steps == 0 || !s.on_ground() {
            step(&mut s, Input::None);
            peak = peak.max(s.y);
            steps += 1;
            assert!(steps < 100, "never landed");
        }
        // apex = v²/2g = 15²/80 ≈ 2.8 rows; airtime 2v/g = 0.75 s ≈ 22 steps
        assert!((2.5..3.0).contains(&peak), "peak {peak}");
        assert!((20..25).contains(&steps), "airtime {steps} steps");
    }

    #[test]
    fn no_double_jump_and_down_fast_falls() {
        let mut s = running(2);
        step(&mut s, Input::Jump);
        step(&mut s, Input::None);
        let vy = s.vy;
        step(&mut s, Input::Jump);
        assert!(s.vy < vy, "a second jump in the air must not boost");
        step(&mut s, Input::Duck);
        assert!(s.vy <= FAST_FALL_V);
    }

    #[test]
    fn a_rock_ends_the_run_unless_jumped() {
        let mut s = running(3);
        s.obstacles.push(Obstacle { x: GOAT_X + 1.0, kind: Kind::Rock });
        step(&mut s, Input::None);
        assert!(!s.alive);

        let mut s = running(3);
        s.y = 1.5;
        assert!(!hits(&s, &Obstacle { x: GOAT_X + 1.0, kind: Kind::Rock }));
    }

    #[test]
    fn a_big_rock_needs_a_high_jump() {
        let mut s = running(4);
        s.y = 1.5;
        assert!(hits(&s, &Obstacle { x: GOAT_X + 1.0, kind: Kind::BigRock }));
        s.y = 2.1;
        assert!(!hits(&s, &Obstacle { x: GOAT_X + 1.0, kind: Kind::BigRock }));
    }

    #[test]
    fn the_eagle_is_ducked_not_jumped_into() {
        let mut s = running(5);
        let eagle = Obstacle { x: GOAT_X + 1.0, kind: Kind::Eagle };
        assert!(hits(&s, &eagle), "standing goat meets the eagle");
        step(&mut s, Input::Duck);
        s.obstacles.clear();
        assert!(!hits(&s, &eagle), "a ducking goat passes under");
        s.duck = 0.0;
        s.y = 1.0;
        assert!(hits(&s, &eagle), "jumping into it is no escape");
    }

    #[test]
    fn a_gap_swallows_only_a_goat_on_the_ledge() {
        let mut s = running(6);
        let gap = Obstacle { x: GOAT_X + 1.0, kind: Kind::Gap };
        assert!(hits(&s, &gap));
        s.y = 0.5;
        assert!(!hits(&s, &gap));
    }

    #[test]
    fn every_obstacle_kind_can_be_cleared_at_the_top_speed() {
        // The longest horizontal span to clear (big rock + goat, forgiven) must
        // fit inside the time the goat is high enough, at every speed.
        let span_big = Kind::BigRock.width() - FORGIVE + GOAT_W - 2.0 * FORGIVE;
        let span_rock = Kind::Rock.width() - FORGIVE + GOAT_W - 2.0 * FORGIVE;
        let high_for = |h: f32| {
            let d = JUMP_V * JUMP_V - 2.0 * GRAVITY * h;
            (2.0 * d.sqrt()) / GRAVITY
        };
        for speed in [START_SPEED, 25.0, MAX_SPEED] {
            assert!(high_for(2.0) * speed >= span_big, "big rock at {speed}");
            assert!(high_for(1.0) * speed >= span_rock, "rock at {speed}");
        }
    }

    #[test]
    fn the_pace_ramps_with_the_score_and_caps() {
        assert_eq!(speed_for(0.0), START_SPEED);
        assert!(speed_for(500.0) > speed_for(100.0));
        assert_eq!(speed_for(1e6), MAX_SPEED);
    }

    #[test]
    fn the_score_counts_up_and_the_sky_turns() {
        let mut s = running(8);
        let before = s.score;
        for _ in 0..30 {
            step(&mut s, Input::None);
        }
        assert!(s.score > before + 8.0, "about ten points a second at the start");
        s.score = NIGHT_EVERY as f32 + 1.0;
        assert!(s.night());
        s.score = 2.0 * NIGHT_EVERY as f32 + 1.0;
        assert!(!s.night());
    }

    #[test]
    fn eagles_wait_for_the_pace_and_the_session_best_survives_a_restart() {
        let mut s = State::new(9, 80);
        for _ in 0..500 {
            assert_ne!(s.pick_kind(), Kind::Eagle);
        }
        s.score = 400.0;
        assert!((0..500).any(|_| s.pick_kind() == Kind::Eagle));
        s.score = 1234.0;
        s.alive = false;
        let r = s.restart();
        assert_eq!(r.best, 1234);
        assert!(r.alive && r.started && r.score == 0.0);
    }

    #[test]
    fn the_renderer_fills_the_frame_exactly() {
        let mut s = running(10);
        s.obstacles.push(Obstacle { x: 30.0, kind: Kind::Rock });
        s.obstacles.push(Obstacle { x: 45.0, kind: Kind::Gap });
        let f = render(&s, 60, 12);
        assert_eq!(f.lines.len(), 12);
        assert!(f.lines.iter().all(|l| l.chars().count() == 60));
        assert!(f.lines[10].contains("=") && f.lines[10].contains(" "), "ledge with a gap");
        assert!(f.lines[9].contains("##"), "the rock sits on the ledge");
        assert!(f.lines[11].contains("q quit"));
        assert!(f.lines[0].contains("HI "));
    }
}
