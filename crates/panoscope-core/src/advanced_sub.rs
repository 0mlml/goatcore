// SPDX-License-Identifier: Apache-2.0
//
// Catch-up subscriber wrapper — the panoscope-core standard for resilient
// subscription across the inspection-sibling cluster (panoscope,
// flashlight, stethoscope) and any goat peer that wants reconnect
// catch-up.
//
// Gap 2 + 4 of docs/design/zenoh-capability-adoption.md (ADR 1040). A
// plain `session.declare_subscriber(ke)` only ever delivers samples that
// arrive AFTER the declaration resolves. A subscriber that reconnects
// after a partition therefore restarts cold — silently missing
// everything published while it was gone. That is exactly the denied-edge
// case our resilience pitch claims to cover, so leaving it hand-rolled
// per-consumer (or absent) is the gap this closes.
//
// zenoh-ext's `AdvancedSubscriber` adds, on the STOCK router (F-193 — this
// is a CLIENT-side zenoh-ext construct, no zenohd fork, no router image
// change):
//   * history catch-up on (re)connect — query matching queryables for
//     what was published while we were absent, then switch seamlessly to
//     live (`HistoryConfig`);
//   * sample-miss recovery — periodically re-query for samples that
//     dropped on the wire (`RecoveryConfig::periodic_queries`).
//
// Brokerless-safe (per docs/design/zenoh-brokerless-consequences.md): the
// history/recovery queries hit whatever queryables already match the key —
// today the router's per-`<peer>/**` rocksdb storage, and once publishers
// adopt `AdvancedPublisher` (gap 5) their own per-producer
// `PublicationCache`. No central component is introduced; this is the
// cooperative, edge-local reliability the brokerless model always intended.
//
// The returned `AdvancedSubscriber` derefs to its channel receiver, so a
// consumer's existing `subscriber.recv_async()` loop is UNCHANGED — only
// the declaration line swaps.

use std::time::Duration;

use zenoh::handlers::FifoChannelHandler;
use zenoh::sample::Sample;
use zenoh::Session;
use zenoh_ext::{AdvancedSubscriber, AdvancedSubscriberBuilderExt, HistoryConfig, RecoveryConfig};

/// Concrete return type of [`declare_catchup_subscriber`]: an
/// [`AdvancedSubscriber`] over the default FIFO channel handler. Callers
/// keep using `.recv_async()` exactly as with a plain subscriber — the
/// `AdvancedSubscriber` derefs to its receiver.
pub type CatchupSubscriber = AdvancedSubscriber<FifoChannelHandler<Sample>>;

/// Default reconnect-history age cap: 1 hour. A subscriber rejoining after
/// a partition catches up the last hour, NOT the storage's full retention
/// (7d on nova-a). Deep historical reads are the explicit audit-tail path
/// (`goat replay` / the MinIO reader), not a live subscriber flooding
/// itself on every reconnect.
pub const DEFAULT_MAX_AGE_SECS: f64 = 3600.0;

/// Default periodic-query recovery period: 5 seconds. Matches the cadence
/// at which our sporadic publishers refresh (schema decls every 30s,
/// health roll-ups on a few-second timer), so a dropped sample heals
/// within a few seconds while adding negligible query load. Periodic
/// queries (not heartbeat) because today's publishers don't yet emit
/// `AdvancedPublisher` heartbeats (gap 5); periodic queries degrade
/// gracefully against any matching queryable.
pub const DEFAULT_RECOVERY_PERIOD: Duration = Duration::from_secs(5);

/// Tunables for [`declare_catchup_subscriber`].
///
/// [`CatchupConfig::default`] is tuned for the inspection-surface use
/// case: detect late-joining publishers, bound the on-connect history to
/// the recent past, and periodically re-query for missed samples. Use
/// [`CatchupConfig::live_only`] for a subscriber that wants the
/// AdvancedSubscriber plumbing (e.g. uniform `sample_miss_listener`
/// wiring) but no historical replay.
#[derive(Debug, Clone)]
pub struct CatchupConfig {
    /// Detect late-joining publishers and query their historical data
    /// (`HistoryConfig::detect_late_publishers`). True late-join detection
    /// requires the publisher to be an `AdvancedPublisher` with
    /// publisher-detection; harmless (a no-op) against plain publishers.
    pub detect_late_publishers: bool,
    /// Cap the history queried per resource by sample count. `None` = no
    /// per-resource count cap (age cap still applies if set).
    pub max_samples: Option<usize>,
    /// Cap the history queried per resource by age in seconds. `None` = no
    /// age cap — bounds reconnect catch-up to "recent" so a long-absent
    /// subscriber doesn't replay the storage's full retention window.
    pub max_age_secs: Option<f64>,
    /// Periodic-query recovery period. `None` disables recovery
    /// (history-only catch-up, no ongoing miss-healing).
    pub recovery_period: Option<Duration>,
    /// Timeout for the history + recovery queries. `None` keeps the
    /// zenoh-ext default.
    pub query_timeout: Option<Duration>,
}

impl Default for CatchupConfig {
    fn default() -> Self {
        Self {
            detect_late_publishers: true,
            max_samples: None,
            max_age_secs: Some(DEFAULT_MAX_AGE_SECS),
            recovery_period: Some(DEFAULT_RECOVERY_PERIOD),
            query_timeout: None,
        }
    }
}

impl CatchupConfig {
    /// A config that pulls NO history and runs NO recovery queries — an
    /// `AdvancedSubscriber` that behaves like a plain live subscriber.
    /// Useful for a broad `**` observer that already has its own
    /// history-replay path (panoscope) and would otherwise amplify queries
    /// across the whole keyspace, while still wanting a uniform
    /// AdvancedSubscriber call site.
    pub fn live_only() -> Self {
        Self {
            detect_late_publishers: false,
            max_samples: None,
            max_age_secs: None,
            recovery_period: None,
            query_timeout: None,
        }
    }

    /// A config that runs ongoing sample-miss recovery but pulls NO
    /// on-connect history. For a "live tail" surface (flashlight) whose
    /// design intent is "what's happening now, replay comes from the
    /// audit-tail" — it should heal samples dropped DURING a session
    /// without dumping a backlog the moment the operator opens the tail.
    ///
    /// Note: against today's plain publishers, recovery re-queries the
    /// latest retained value on its timer; true sequence-numbered
    /// gap-filling activates once publishers adopt `AdvancedPublisher`
    /// sample-miss detection (gap 5 of the adoption plan).
    pub fn recovery_only(period: Duration) -> Self {
        Self {
            detect_late_publishers: false,
            max_samples: None,
            max_age_secs: None,
            recovery_period: Some(period),
            query_timeout: None,
        }
    }

    /// True when this config requests any on-connect history catch-up.
    pub fn pulls_history(&self) -> bool {
        self.detect_late_publishers || self.max_samples.is_some() || self.max_age_secs.is_some()
    }
}

/// Declare a catch-up subscriber on `key_expr` over `session`.
///
/// Returns a [`CatchupSubscriber`]; drive it with the same
/// `.recv_async()` loop a plain subscriber uses. On (re)connect it queries
/// history for what was missed (bounded by `config`), delivers it, then
/// continues live; if `config.recovery_period` is set it periodically
/// re-queries for dropped samples.
///
/// This is the only place the workspace constructs an AdvancedSubscriber —
/// keeping the history/recovery policy in one reviewable spot rather than
/// hand-rolled per consumer.
pub async fn declare_catchup_subscriber(
    session: &Session,
    key_expr: &str,
    config: &CatchupConfig,
) -> zenoh::Result<CatchupSubscriber> {
    // Start from the plain SubscriberBuilder and lift it to the advanced
    // builder explicitly, then apply each knob. Going through `.advanced()`
    // (rather than the SubscriberBuilderExt shortcuts) keeps every method
    // call on the AdvancedSubscriberBuilder surface we depend on.
    let mut builder = session.declare_subscriber(key_expr).advanced();

    if config.pulls_history() {
        let mut history = HistoryConfig::default();
        if config.detect_late_publishers {
            history = history.detect_late_publishers();
        }
        if let Some(n) = config.max_samples {
            history = history.max_samples(n);
        }
        if let Some(age) = config.max_age_secs {
            history = history.max_age(age);
        }
        builder = builder.history(history);
    }

    if let Some(period) = config.recovery_period {
        builder = builder.recovery(RecoveryConfig::default().periodic_queries(period));
    }

    if let Some(timeout) = config.query_timeout {
        builder = builder.query_timeout(timeout);
    }

    // subscriber_detection() declares a liveliness token for THIS
    // subscriber so AdvancedPublishers can see it appear/leave — the
    // presence half that pairs with gap 1. Cheap and additive.
    builder.subscriber_detection().await
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn default_pulls_history_and_recovers() {
        let c = CatchupConfig::default();
        assert!(c.detect_late_publishers);
        assert!(c.pulls_history());
        assert_eq!(c.max_age_secs, Some(DEFAULT_MAX_AGE_SECS));
        assert_eq!(c.recovery_period, Some(DEFAULT_RECOVERY_PERIOD));
    }

    #[test]
    fn live_only_pulls_no_history_and_no_recovery() {
        let c = CatchupConfig::live_only();
        assert!(!c.detect_late_publishers);
        assert!(!c.pulls_history());
        assert_eq!(c.max_age_secs, None);
        assert_eq!(c.recovery_period, None);
    }

    #[test]
    fn recovery_only_heals_without_history() {
        let c = CatchupConfig::recovery_only(Duration::from_secs(3));
        assert!(!c.pulls_history(), "recovery-only must not pull on-connect history");
        assert!(!c.detect_late_publishers);
        assert_eq!(c.recovery_period, Some(Duration::from_secs(3)));
    }

    #[test]
    fn pulls_history_tracks_each_knob_independently() {
        // Only late-publisher detection on.
        let c = CatchupConfig {
            detect_late_publishers: true,
            max_samples: None,
            max_age_secs: None,
            recovery_period: None,
            query_timeout: None,
        };
        assert!(c.pulls_history());

        // Only a sample-count cap on (count cap implies we query history).
        let c = CatchupConfig {
            detect_late_publishers: false,
            max_samples: Some(8),
            max_age_secs: None,
            recovery_period: None,
            query_timeout: None,
        };
        assert!(c.pulls_history());

        // Only an age cap on.
        let c = CatchupConfig {
            detect_late_publishers: false,
            max_samples: None,
            max_age_secs: Some(60.0),
            recovery_period: None,
            query_timeout: None,
        };
        assert!(c.pulls_history());

        // Nothing on — pure live.
        let c = CatchupConfig {
            detect_late_publishers: false,
            max_samples: None,
            max_age_secs: None,
            recovery_period: Some(Duration::from_secs(5)),
            query_timeout: None,
        };
        assert!(!c.pulls_history());
    }
}
