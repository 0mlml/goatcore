//! In-conn keepalive state machines (server-side `Sender`, client-side
//! `Receiver`) for the `MsgTypeHealthCheck` ping/pong.
//!
//! Spec section: §5 of `docs/design/netbird-relay-wire-protocol.md`.
//!
//! Both state machines are **runtime-agnostic** — they expose `tick()` and
//! `on_response()` / `on_heartbeat()` methods that the consuming binary
//! crate's async runtime drives via timers and inbound-message events.
//! Tokio, glommio, or std::thread are all fine drivers.
//!
//! The wire bytes for emitting and parsing HealthCheck messages are in
//! [`crate::messages::close::HEALTH_CHECK_BYTES`] /
//! [`crate::messages::close::marshal_health_check`] /
//! [`crate::messages::close::parse_health_check`] (already shipped step 1).
//!
//! # Sender (server-side, runs on the relay)
//!
//! - Every `HEALTH_CHECK_INTERVAL` (25s) → emit a HealthCheck on the conn.
//! - Every `HEALTH_CHECK_INTERVAL + HEALTH_CHECK_TIMEOUT` (45s) → if no
//!   `on_response()` arrived, increment failure counter; once it reaches
//!   `attempt_threshold` (default 1), the conn is declared dead and the
//!   binary crate must close it.
//!
//! # Receiver (client-side, runs on the netbird daemon)
//!
//! - Every `HEARTBEAT_TIMEOUT` (35s) → if no `on_heartbeat()` arrived,
//!   increment failure counter; once it reaches threshold, declare timeout.
//!
//! The receiver is implemented here only for completeness / for the
//! conformance soak driver (which simulates a client). Production peers
//! run upstream's Receiver, not ours.

use core::time::Duration;

/// How often the server should emit a HealthCheck message to a connected
/// peer. Mirrors upstream `defaultHealthCheckInterval`
/// (`shared/relay/healthcheck/sender.go:13`).
pub const HEALTH_CHECK_INTERVAL: Duration = Duration::from_secs(25);

/// Additional timeout window beyond [`HEALTH_CHECK_INTERVAL`] before the
/// server declares a missed response. Mirrors upstream
/// `defaultHealthCheckTimeout` (`shared/relay/healthcheck/sender.go:14`).
pub const HEALTH_CHECK_TIMEOUT: Duration = Duration::from_secs(20);

/// Effective server-side timeout window. Equals
/// `HEALTH_CHECK_INTERVAL + HEALTH_CHECK_TIMEOUT` (45s). Mirrors upstream
/// `Sender.timeout` (`shared/relay/healthcheck/sender.go:57`).
pub const SENDER_TIMEOUT_WINDOW: Duration =
    Duration::from_secs(HEALTH_CHECK_INTERVAL.as_secs() + HEALTH_CHECK_TIMEOUT.as_secs());

/// Client-side heartbeat timeout. Slightly longer than the server's
/// interval (25s + 10s slack). Mirrors upstream `defaultHeartbeatTimeout`
/// (`shared/relay/healthcheck/receiver.go:11`).
pub const HEARTBEAT_TIMEOUT: Duration =
    Duration::from_secs(HEALTH_CHECK_INTERVAL.as_secs() + 10);

/// Default consecutive-failure threshold before either side declares
/// the peer dead. Mirrors upstream `defaultAttemptThreshold`
/// (`shared/relay/healthcheck/sender.go:11`).
pub const DEFAULT_ATTEMPT_THRESHOLD: u32 = 1;

/// Environment variable name used by upstream to override
/// [`DEFAULT_ATTEMPT_THRESHOLD`]. Mirrors `defaultAttemptThresholdEnv`
/// (`shared/relay/healthcheck/env.go:11`).
pub const ATTEMPT_THRESHOLD_ENV: &str = "NB_RELAY_HC_ATTEMPT_THRESHOLD";

/// What the [`Sender`] should do next, returned by [`Sender::tick`] and
/// [`Sender::tick_timeout`].
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
#[non_exhaustive]
pub enum SenderAction {
    /// Nothing to do — keep waiting.
    Idle,
    /// Emit a HealthCheck message on the conn (write
    /// [`crate::messages::close::HEALTH_CHECK_BYTES`]).
    EmitHealthCheck,
    /// Peer has missed enough heartbeats; close the conn.
    DeclareTimeout,
}

/// Server-side keepalive state machine. Mirrors upstream `Sender`.
///
/// The consuming binary crate runs two timers and feeds events here:
///
/// - **Interval timer** every [`HEALTH_CHECK_INTERVAL`] → call
///   [`Sender::tick_interval`]. If the action is `EmitHealthCheck`,
///   write the wire bytes and continue.
/// - **Timeout timer** every [`SENDER_TIMEOUT_WINDOW`] → call
///   [`Sender::tick_timeout`]. If the action is `DeclareTimeout`,
///   close the conn.
/// - **Inbound HealthCheck** (peer's echo) → call [`Sender::on_response`].
#[derive(Debug, Clone)]
pub struct Sender {
    alive: bool,
    failure_counter: u32,
    attempt_threshold: u32,
}

impl Default for Sender {
    fn default() -> Self {
        Self::new(DEFAULT_ATTEMPT_THRESHOLD)
    }
}

impl Sender {
    /// Construct with a custom attempt threshold. Pass
    /// [`DEFAULT_ATTEMPT_THRESHOLD`] to match upstream defaults, or read
    /// [`ATTEMPT_THRESHOLD_ENV`] in the binary crate to honor the env-var
    /// override.
    #[must_use]
    pub const fn new(attempt_threshold: u32) -> Self {
        Self {
            alive: false,
            failure_counter: 0,
            attempt_threshold: if attempt_threshold == 0 {
                DEFAULT_ATTEMPT_THRESHOLD
            } else {
                attempt_threshold
            },
        }
    }

    /// Called by the interval timer every [`HEALTH_CHECK_INTERVAL`].
    /// Always returns [`SenderAction::EmitHealthCheck`] — the binary
    /// crate writes the wire bytes.
    #[must_use]
    pub const fn tick_interval(&self) -> SenderAction {
        SenderAction::EmitHealthCheck
    }

    /// Called by the timeout timer every [`SENDER_TIMEOUT_WINDOW`].
    /// Returns [`SenderAction::DeclareTimeout`] only when the peer has
    /// missed the configured number of consecutive heartbeats.
    pub fn tick_timeout(&mut self) -> SenderAction {
        if self.alive {
            self.alive = false;
            return SenderAction::Idle;
        }
        self.failure_counter += 1;
        if self.failure_counter < self.attempt_threshold {
            SenderAction::Idle
        } else {
            SenderAction::DeclareTimeout
        }
    }

    /// Called when an inbound HealthCheck (the peer's echo) is parsed
    /// from the conn. Resets the failure counter and marks the peer as
    /// alive. A specific case of [`Self::mark_alive`].
    pub fn on_response(&mut self) {
        self.mark_alive();
    }

    /// Mark the peer alive on ANY observed inbound activity, not just a
    /// HealthCheck echo. Any successfully-parsed frame from the peer
    /// (Transport included) is evidence it is alive, so the liveness timer
    /// must reset on data too — otherwise a peer that is actively relaying
    /// but misses a single HC echo (loss / scheduler stall) is declared
    /// dead and torn down, flapping busy-but-HC-unlucky sessions. This is
    /// the deliberate delta from upstream Go, which credits only the HC
    /// echo.
    pub fn mark_alive(&mut self) {
        self.failure_counter = 0;
        self.alive = true;
    }

    /// Currently-recorded failure-streak count (number of consecutive
    /// timeout-window expirations without a response). Returns to 0
    /// after [`Self::on_response`].
    #[must_use]
    pub const fn failure_count(&self) -> u32 {
        self.failure_counter
    }

    /// Whether the peer is currently considered alive.
    #[must_use]
    pub const fn is_alive(&self) -> bool {
        self.alive
    }
}

/// What the [`Receiver`] should do next, returned by [`Receiver::tick`].
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
#[non_exhaustive]
pub enum ReceiverAction {
    /// Nothing to do — keep waiting.
    Idle,
    /// Server has missed enough heartbeats; the client should tear
    /// down the conn and reconnect to a different relay.
    DeclareTimeout,
}

/// Client-side keepalive state machine. Mirrors upstream `Receiver`.
///
/// The consuming binary crate runs:
///
/// - **Heartbeat timer** every [`HEARTBEAT_TIMEOUT`] → call
///   [`Receiver::tick`]. If the action is `DeclareTimeout`, close the
///   conn and reconnect.
/// - **Inbound HealthCheck** (server's ping) → call
///   [`Receiver::on_heartbeat`]; ALSO emit a HealthCheck back on the
///   wire (which the server's [`Sender`] will see as `on_response`).
#[derive(Debug, Clone)]
pub struct Receiver {
    alive: bool,
    failure_counter: u32,
    attempt_threshold: u32,
}

impl Default for Receiver {
    fn default() -> Self {
        Self::new(DEFAULT_ATTEMPT_THRESHOLD)
    }
}

impl Receiver {
    /// Construct with a custom attempt threshold.
    #[must_use]
    pub const fn new(attempt_threshold: u32) -> Self {
        Self {
            alive: false,
            failure_counter: 0,
            attempt_threshold: if attempt_threshold == 0 {
                DEFAULT_ATTEMPT_THRESHOLD
            } else {
                attempt_threshold
            },
        }
    }

    /// Called by the heartbeat timer every [`HEARTBEAT_TIMEOUT`].
    pub fn tick(&mut self) -> ReceiverAction {
        if self.alive {
            self.alive = false;
            return ReceiverAction::Idle;
        }
        self.failure_counter += 1;
        if self.failure_counter < self.attempt_threshold {
            ReceiverAction::Idle
        } else {
            ReceiverAction::DeclareTimeout
        }
    }

    /// Called when the server's HealthCheck ping is parsed from the conn.
    /// Resets the failure counter; the binary crate should ALSO emit a
    /// HealthCheck on the wire (the echo).
    pub fn on_heartbeat(&mut self) {
        self.failure_counter = 0;
        self.alive = true;
    }

    /// Currently-recorded failure-streak count.
    #[must_use]
    pub const fn failure_count(&self) -> u32 {
        self.failure_counter
    }

    /// Whether the server is currently considered alive.
    #[must_use]
    pub const fn is_alive(&self) -> bool {
        self.alive
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn constants_match_upstream() {
        assert_eq!(HEALTH_CHECK_INTERVAL.as_secs(), 25);
        assert_eq!(HEALTH_CHECK_TIMEOUT.as_secs(), 20);
        assert_eq!(SENDER_TIMEOUT_WINDOW.as_secs(), 45);
        assert_eq!(HEARTBEAT_TIMEOUT.as_secs(), 35);
        assert_eq!(DEFAULT_ATTEMPT_THRESHOLD, 1);
        assert_eq!(ATTEMPT_THRESHOLD_ENV, "NB_RELAY_HC_ATTEMPT_THRESHOLD");
    }

    #[test]
    fn sender_emits_healthcheck_on_interval() {
        let s = Sender::default();
        assert_eq!(s.tick_interval(), SenderAction::EmitHealthCheck);
    }

    #[test]
    fn sender_default_threshold_one_misses_immediately() {
        // With threshold=1 (default), the very first missed timeout window
        // declares timeout. But upstream's logic requires the peer to have
        // never been seen alive at all — let's verify.
        let mut s = Sender::default();
        // First tick_timeout: no previous response → counter=1 → DeclareTimeout
        // (because failure_counter (1) >= attempt_threshold (1)).
        assert_eq!(s.tick_timeout(), SenderAction::DeclareTimeout);
    }

    #[test]
    fn sender_alive_path_after_response() {
        let mut s = Sender::default();
        // Response arrives — peer is now alive
        s.on_response();
        assert!(s.is_alive());
        assert_eq!(s.failure_count(), 0);
        // First timeout tick: alive flag was set → cleared, no timeout
        assert_eq!(s.tick_timeout(), SenderAction::Idle);
        // Second timeout tick (no response in between): counter=1 ≥ threshold → timeout
        assert_eq!(s.tick_timeout(), SenderAction::DeclareTimeout);
    }

    #[test]
    fn mark_alive_credits_liveness_like_a_response() {
        // F-219: data traffic (mark_alive), not only HC echoes (on_response),
        // must reset the liveness state. After a missed tick, mark_alive
        // must clear the failure streak and revive the peer exactly as a
        // HC response would — otherwise a busy peer that missed one HC echo
        // is wrongly declared dead.
        let mut s = Sender::default();
        // Accrue a failure (default threshold 1: first tick clears nothing
        // since not alive → counter=1).
        assert_eq!(s.tick_timeout(), SenderAction::DeclareTimeout);
        assert_eq!(s.failure_count(), 1);
        // Inbound data frame observed → mark_alive.
        s.mark_alive();
        assert!(s.is_alive());
        assert_eq!(s.failure_count(), 0);
        // Same post-state as on_response: one tolerated tick before timeout.
        assert_eq!(s.tick_timeout(), SenderAction::Idle);
    }

    #[test]
    fn sender_threshold_three_tolerates_two_misses() {
        let mut s = Sender::new(3);
        s.on_response();

        // First tick: alive flag clears, no timeout
        assert_eq!(s.tick_timeout(), SenderAction::Idle);
        assert_eq!(s.failure_count(), 0);

        // Second tick: counter=1 < threshold=3 → idle
        assert_eq!(s.tick_timeout(), SenderAction::Idle);
        assert_eq!(s.failure_count(), 1);

        // Third tick: counter=2 < threshold=3 → idle
        assert_eq!(s.tick_timeout(), SenderAction::Idle);
        assert_eq!(s.failure_count(), 2);

        // Fourth tick: counter=3 ≥ threshold=3 → timeout
        assert_eq!(s.tick_timeout(), SenderAction::DeclareTimeout);
    }

    #[test]
    fn sender_response_resets_counter() {
        let mut s = Sender::new(3);
        s.on_response();
        s.tick_timeout(); // clear alive flag
        s.tick_timeout(); // counter=1
        s.tick_timeout(); // counter=2
        assert_eq!(s.failure_count(), 2);

        // Response arrives — counter resets
        s.on_response();
        assert_eq!(s.failure_count(), 0);
        assert!(s.is_alive());

        // Next timeout: alive flag clears, no timeout, no counter increment
        s.tick_timeout();
        assert_eq!(s.failure_count(), 0);
    }

    #[test]
    fn sender_zero_threshold_treated_as_default() {
        // Pass 0 — should not be treated as 0 (upstream uses default in this case).
        let s = Sender::new(0);
        // Verify by behavior — threshold 0 would mean "never timeout" if taken
        // literally. Default is 1.
        let mut s2 = s;
        assert_eq!(s2.tick_timeout(), SenderAction::DeclareTimeout);
    }

    #[test]
    fn receiver_idle_when_alive() {
        let mut r = Receiver::default();
        r.on_heartbeat();
        assert!(r.is_alive());
        assert_eq!(r.tick(), ReceiverAction::Idle);
        assert!(!r.is_alive()); // alive flag was consumed
    }

    #[test]
    fn receiver_default_threshold_one_misses_immediately() {
        let mut r = Receiver::default();
        // First tick: no previous heartbeat, alive=false, counter=1 ≥ threshold=1
        assert_eq!(r.tick(), ReceiverAction::DeclareTimeout);
    }

    #[test]
    fn receiver_threshold_three() {
        let mut r = Receiver::new(3);
        r.on_heartbeat();
        assert_eq!(r.tick(), ReceiverAction::Idle); // alive clears
        assert_eq!(r.tick(), ReceiverAction::Idle); // counter=1
        assert_eq!(r.tick(), ReceiverAction::Idle); // counter=2
        assert_eq!(r.tick(), ReceiverAction::DeclareTimeout); // counter=3
    }

    #[test]
    fn end_to_end_sender_receiver_round_trip() {
        // Simulate one tick cycle: server sends, client responds, server
        // confirms via on_response.
        let mut sender = Sender::default();
        let mut receiver = Receiver::default();

        // Server's interval timer fires → emits HealthCheck on conn
        assert_eq!(sender.tick_interval(), SenderAction::EmitHealthCheck);

        // Client receives the HealthCheck → records heartbeat + emits echo
        receiver.on_heartbeat();
        assert!(receiver.is_alive());

        // Server receives the echo
        sender.on_response();
        assert!(sender.is_alive());

        // Both timeout timers fire — both should be idle (alive flag clears)
        assert_eq!(sender.tick_timeout(), SenderAction::Idle);
        assert_eq!(receiver.tick(), ReceiverAction::Idle);
    }
}
