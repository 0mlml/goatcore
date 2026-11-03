// Per-actor throughput-envelope registry + band classifier (ADR 1045 /
// docs/design/qos-throughput-envelopes.md).
//
// The portal declares each actor's *expected* traffic as four ascending levels
// — min ≤ mean ≤ high ≤ max — on two axes (msgs/s and bytes/s). This module is
// the reveal-side mirror of the alias registry: it caches the portal's
// `GET /api/envelopes` map and, given a producer's LIVE rate, computes the
// **band** the producer is in — LOW / normal / HIGH / OVER. `goat top` renders
// that band per row.
//
// Honesty (inherited from the env-health surface): a band is produced ONLY when
// an envelope is known. No envelope → `None`, which the caller renders as an
// honest `—`, never a faked verdict. The live rate is always grounded (a stale
// footprint has already aged out before it reaches here).

use crate::portal::EnvelopeRow;
use std::collections::HashMap;

/// Where a producer's live rate sits relative to its declared envelope. Both
/// off-nominal ends are first-class — `Low` (under the floor) is a fault
/// signal, not benign quiet, and `Over` (past the ceiling) is a flood.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Band {
    /// `< min` — under-producing the declared floor: possibly stalled,
    /// disconnected, upstream-starved, or misconfigured. Suppressed when
    /// `min == 0` (the actor declared that quiet is normal for it).
    Low,
    /// `[min, high]` — within the expected range.
    Normal,
    /// `(high, max]` — elevated, approaching the ceiling.
    High,
    /// `> max` — flooding past the declared max (the enforce trigger, later).
    Over,
}

impl Band {
    /// Short uppercase label for display. `Normal` reads `ok` (lowercase) so it
    /// recedes — the eye should catch the off-nominal rows.
    pub fn label(self) -> &'static str {
        match self {
            Band::Low => "LOW",
            Band::Normal => "ok",
            Band::High => "HIGH",
            Band::Over => "OVER",
        }
    }

    /// Honesty-palette color string (matches the env-health 5-state vocabulary
    /// the rest of `goat top` uses): green normal, amber elevated, red flood,
    /// cyan for the distinct "under-floor" fault.
    pub fn color(self) -> &'static str {
        match self {
            Band::Low => "cyan",
            Band::Normal => "green",
            Band::High => "amber",
            Band::Over => "red",
        }
    }

    /// Severity rank for combining the two axes (ADR 1045 §2, "the worst of the
    /// two governs"). `Over` (a present flood) is worst; `Low` (a present
    /// under-floor fault) outranks `High` (a not-yet-over warning that is still
    /// in range); `Normal` is best.
    fn rank(self) -> u8 {
        match self {
            Band::Normal => 0,
            Band::High => 1,
            Band::Low => 2,
            Band::Over => 3,
        }
    }
}

/// One actor's declared envelope, reduced to what the band classifier needs.
#[derive(Debug, Clone)]
pub struct Envelope {
    pub anchor: String,
    pub min_msgs_per_s: f64,
    pub high_msgs_per_s: f64,
    pub max_msgs_per_s: f64,
    pub min_bytes_per_s: f64,
    pub high_bytes_per_s: f64,
    pub max_bytes_per_s: f64,
    /// "default" (rough class-baseline placeholder) | "edited" (party-set).
    pub source: String,
}

impl Envelope {
    fn from_row(r: EnvelopeRow) -> Self {
        Self {
            anchor: r.anchor,
            min_msgs_per_s: r.min_msgs_per_s,
            high_msgs_per_s: r.high_msgs_per_s,
            max_msgs_per_s: r.max_msgs_per_s,
            min_bytes_per_s: r.min_bytes_per_s,
            high_bytes_per_s: r.high_bytes_per_s,
            max_bytes_per_s: r.max_bytes_per_s,
            source: r.source,
        }
    }

    /// Band of one axis: `> max` Over, else `> high` High, else `< min` Low
    /// (only when `min > 0` — a declared-bursty actor with `min == 0` never
    /// trips Low), else Normal. A degenerate level of 0 disables that edge
    /// (so an all-zero envelope reads Normal, never a false flood).
    fn axis_band(rate: f64, min: f64, high: f64, max: f64) -> Band {
        if max > 0.0 && rate > max {
            Band::Over
        } else if high > 0.0 && rate > high {
            Band::High
        } else if min > 0.0 && rate < min {
            Band::Low
        } else {
            Band::Normal
        }
    }

    /// The producer's band from the worst of its msgs/s and bytes/s axes.
    pub fn band(&self, msgs_per_s: f64, bytes_per_s: f64) -> Band {
        let m = Self::axis_band(msgs_per_s, self.min_msgs_per_s, self.high_msgs_per_s, self.max_msgs_per_s);
        let b = Self::axis_band(bytes_per_s, self.min_bytes_per_s, self.high_bytes_per_s, self.max_bytes_per_s);
        if m.rank() >= b.rank() {
            m
        } else {
            b
        }
    }
}

/// Anchor→envelope map, goatnet-local. Built from `GET /api/envelopes`; an
/// empty registry is the portal-down / no-envelopes-declared degraded state,
/// where every `band()` is `None` (an honest `—`).
#[derive(Debug, Clone, Default)]
pub struct EnvelopeRegistry {
    by_anchor: HashMap<String, Envelope>,
}

impl EnvelopeRegistry {
    /// The degraded/empty registry — no envelope known for anyone.
    pub fn empty() -> Self {
        Self::default()
    }

    pub fn from_rows(rows: impl IntoIterator<Item = EnvelopeRow>) -> Self {
        let mut by_anchor = HashMap::new();
        for r in rows {
            if r.anchor.is_empty() {
                continue;
            }
            by_anchor.insert(r.anchor.clone(), Envelope::from_row(r));
        }
        Self { by_anchor }
    }

    pub fn len(&self) -> usize {
        self.by_anchor.len()
    }

    pub fn is_empty(&self) -> bool {
        self.by_anchor.is_empty()
    }

    /// The declared envelope for an anchor, if known.
    pub fn get(&self, anchor: &str) -> Option<&Envelope> {
        self.by_anchor.get(anchor)
    }

    /// The band a producer's live rates fall in, or `None` when no envelope is
    /// known for the anchor — the honest "no verdict" state.
    pub fn band(&self, anchor: &str, msgs_per_s: f64, bytes_per_s: f64) -> Option<Band> {
        self.by_anchor.get(anchor).map(|e| e.band(msgs_per_s, bytes_per_s))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn row(anchor: &str) -> EnvelopeRow {
        EnvelopeRow {
            anchor: anchor.to_string(),
            min_msgs_per_s: 1.0,
            mean_msgs_per_s: 5.0,
            high_msgs_per_s: 30.0,
            max_msgs_per_s: 120.0,
            min_bytes_per_s: 512.0,
            mean_bytes_per_s: 4096.0,
            high_bytes_per_s: 32768.0,
            max_bytes_per_s: 131072.0,
            source: "default".into(),
        }
    }

    fn reg() -> EnvelopeRegistry {
        EnvelopeRegistry::from_rows([row("acme")])
    }

    #[test]
    fn bands_across_the_range() {
        let r = reg();
        // Within [min,high] on both axes → Normal.
        assert_eq!(r.band("acme", 10.0, 4096.0), Some(Band::Normal));
        // msgs in (high,max] → High (bytes normal).
        assert_eq!(r.band("acme", 60.0, 4096.0), Some(Band::High));
        // msgs > max → Over.
        assert_eq!(r.band("acme", 200.0, 4096.0), Some(Band::Over));
        // msgs < min → Low.
        assert_eq!(r.band("acme", 0.5, 4096.0), Some(Band::Low));
    }

    #[test]
    fn worst_of_two_axes_governs() {
        let r = reg();
        // msgs Normal but bytes Over → Over (the flood surfaces).
        assert_eq!(r.band("acme", 10.0, 200_000.0), Some(Band::Over));
        // msgs Low but bytes High → Low outranks High (present fault > warning).
        assert_eq!(r.band("acme", 0.5, 60_000.0), Some(Band::Low));
        // msgs High, bytes Normal → High.
        assert_eq!(r.band("acme", 60.0, 4096.0), Some(Band::High));
    }

    #[test]
    fn min_zero_suppresses_low() {
        // A declared-bursty actor (min = 0 on both axes): zero traffic is
        // Normal, never Low.
        let bursty = EnvelopeRow {
            anchor: "bursty".into(),
            min_msgs_per_s: 0.0,
            mean_msgs_per_s: 0.0,
            high_msgs_per_s: 50.0,
            max_msgs_per_s: 500.0,
            min_bytes_per_s: 0.0,
            mean_bytes_per_s: 0.0,
            high_bytes_per_s: 5000.0,
            max_bytes_per_s: 50000.0,
            source: "edited".into(),
        };
        let r = EnvelopeRegistry::from_rows([bursty]);
        assert_eq!(r.band("bursty", 0.0, 0.0), Some(Band::Normal));
        // It still floods past max.
        assert_eq!(r.band("bursty", 600.0, 0.0), Some(Band::Over));
    }

    #[test]
    fn unknown_anchor_is_none_not_a_faked_verdict() {
        assert_eq!(reg().band("nobody", 10.0, 4096.0), None);
        assert_eq!(EnvelopeRegistry::empty().band("acme", 10.0, 4096.0), None);
    }

    #[test]
    fn color_and_label_vocabulary() {
        assert_eq!(Band::Over.color(), "red");
        assert_eq!(Band::High.color(), "amber");
        assert_eq!(Band::Low.color(), "cyan");
        assert_eq!(Band::Normal.color(), "green");
        assert_eq!(Band::Over.label(), "OVER");
        assert_eq!(Band::Normal.label(), "ok");
    }
}
