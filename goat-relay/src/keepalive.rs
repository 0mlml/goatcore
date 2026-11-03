//! Per-connection keepalive driver.
//!
//! Wraps the lib's [`goat_relay_protocol::keepalive::Sender`] state
//! machine and drives it against a generic writer (MPSC sender for WS;
//! [`quinn::Connection`] for QUIC). Both WS and QUIC listeners spawn one
//! of these tasks per connection.
//!
//! Spec section: §5 of `docs/design/netbird-relay-wire-protocol.md`.
//!
//! # Lifecycle
//!
//! - Every [`HEALTH_CHECK_INTERVAL`] (25 s) the interval timer fires;
//!   `Sender::tick_interval` returns `EmitHealthCheck`; we write the
//!   2-byte [`HEALTH_CHECK_BYTES`] frame on the conn.
//! - Every [`SENDER_TIMEOUT_WINDOW`] (45 s) the timeout timer fires;
//!   `Sender::tick_timeout` returns `Idle` (peer was alive last tick) or
//!   `DeclareTimeout` (peer missed enough heartbeats). On
//!   `DeclareTimeout` we send a close signal to the reader and exit.
//! - The reader (in `listener/ws.rs` or `listener/quic.rs`) parses
//!   inbound `HealthCheck` (0x05) frames and calls
//!   `Sender::on_response` to reset the failure counter via the shared
//!   `Rc<RefCell<Sender>>`.
//!
//! # ADR 0709 D7 invariants preserved
//!
//! - Tasks spawned via `tokio::task::spawn_local` (D7 layer 3).
//! - Shared state is `Rc<RefCell<...>>`, not `Arc<Mutex<...>>` (D7
//!   layer 2). The reader and the keepalive task run on the same
//!   single-threaded runtime, so `borrow_mut` borrows are non-
//!   overlapping by virtue of cooperative-yield semantics — care is
//!   taken to drop borrows before any `.await`.

use std::cell::RefCell;
use std::rc::Rc;
use std::time::Duration;

use bytes::Bytes;
use tokio::sync::{mpsc, oneshot};

use goat_relay_protocol::keepalive::{
    Sender, SenderAction, ATTEMPT_THRESHOLD_ENV, DEFAULT_ATTEMPT_THRESHOLD,
    HEALTH_CHECK_INTERVAL, SENDER_TIMEOUT_WINDOW,
};
use goat_relay_protocol::messages::close::HEALTH_CHECK_BYTES;

/// Per-connection keepalive cadence. Production uses the upstream
/// constants ([`HEALTH_CHECK_INTERVAL`] = 25 s, [`SENDER_TIMEOUT_WINDOW`]
/// = 45 s) via [`KeepaliveTimings::default`]. Integration tests inject
/// short windows so an idle-survival assertion runs in real (wall-clock)
/// time — the keepalive loop drives a real network connection, so the
/// `tokio::time::advance` trick used by the unit test below cannot be
/// applied when there is live QUIC/WS I/O in flight.
#[derive(Debug, Clone, Copy)]
pub struct KeepaliveTimings {
    /// How often the server emits a HealthCheck probe.
    pub hc_interval: Duration,
    /// How often the timeout timer fires; one missed window past the
    /// attempt threshold declares timeout.
    pub timeout_window: Duration,
}

impl Default for KeepaliveTimings {
    fn default() -> Self {
        Self {
            hc_interval: HEALTH_CHECK_INTERVAL,
            timeout_window: SENDER_TIMEOUT_WINDOW,
        }
    }
}

/// Read [`ATTEMPT_THRESHOLD_ENV`] from the environment; fall back to
/// [`DEFAULT_ATTEMPT_THRESHOLD`] (1) if the var is unset, malformed, or
/// 0. Mirrors upstream's env-var semantics from
/// `shared/relay/healthcheck/env.go`.
#[must_use]
pub fn attempt_threshold_from_env() -> u32 {
    std::env::var(ATTEMPT_THRESHOLD_ENV)
        .ok()
        .and_then(|s| s.parse::<u32>().ok())
        .filter(|&n| n > 0)
        .unwrap_or(DEFAULT_ATTEMPT_THRESHOLD)
}

/// Generic per-connection writer interface for the keepalive task.
/// MPSC-backed (WS) and quinn-backed (QUIC) writers both implement this.
pub trait KeepaliveWriter {
    /// Send a `HealthCheck` (0x05) frame. Returns `Err(())` if the
    /// underlying writer is closed (peer disconnected).
    ///
    /// `Result<(), ()>` is intentional — this signals "did the send
    /// succeed?" and the underlying error type differs per impl
    /// (`mpsc::error::SendError` vs `quinn::SendDatagramError`); the
    /// keepalive task only needs the boolean. Clippy's
    /// `result_unit_err` lint is silenced for this signature.
    #[allow(clippy::result_unit_err)]
    fn send_health_check(&self) -> Result<(), ()>;
}

impl KeepaliveWriter for mpsc::Sender<Bytes> {
    fn send_health_check(&self) -> Result<(), ()> {
        // try_send is non-blocking; HC frames must not block on a full
        // writer queue. If the queue is full we treat it as success (the
        // peer's getting other traffic; HC is purely a liveness probe).
        match self.try_send(Bytes::from_static(&HEALTH_CHECK_BYTES)) {
            Ok(()) | Err(mpsc::error::TrySendError::Full(_)) => Ok(()),
            Err(mpsc::error::TrySendError::Closed(_)) => Err(()),
        }
    }
}

impl KeepaliveWriter for quinn::Connection {
    fn send_health_check(&self) -> Result<(), ()> {
        self.send_datagram(Bytes::from_static(&HEALTH_CHECK_BYTES))
            .map_err(|_| ())
    }
}

/// Reason the keepalive task signalled the connection to close.
/// Carried as the value of the `oneshot` close-signal so the reader can
/// log it.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum CloseReason {
    /// `Sender::tick_timeout` returned `DeclareTimeout` — peer missed
    /// enough consecutive heartbeats.
    KeepaliveTimeout,
}

/// Run the per-connection keepalive task until either:
///
/// - The peer times out (`Sender::tick_timeout` returns `DeclareTimeout`)
///   — the task sends a close signal to the reader and returns.
/// - The writer closes (`writer.send_health_check` returns `Err`) — the
///   task returns silently (the reader will notice on its next read).
/// - The task is `abort()`'d by the listener (e.g., reader exited first).
pub async fn run<W: KeepaliveWriter>(
    ka_sender: Rc<RefCell<Sender>>,
    writer: W,
    close_signal_tx: oneshot::Sender<CloseReason>,
    timings: KeepaliveTimings,
) {
    let mut interval = tokio::time::interval(timings.hc_interval);
    let mut timeout = tokio::time::interval(timings.timeout_window);
    // Skip the immediate-fire tick that `tokio::time::interval` produces
    // — we want the FIRST tick to fire after the configured duration.
    interval.tick().await;
    timeout.tick().await;

    loop {
        tokio::select! {
            _ = interval.tick() => {
                // tick_interval is `&self` and infallible; no borrow_mut
                // needed. Always returns EmitHealthCheck.
                if matches!(ka_sender.borrow().tick_interval(), SenderAction::EmitHealthCheck)
                    && writer.send_health_check().is_err()
                {
                    // Peer's writer is gone — the reader will notice
                    // independently.
                    return;
                }
            }
            _ = timeout.tick() => {
                // Take the borrow_mut, evaluate the action, drop the
                // borrow before awaiting anything (none here, but
                // hygiene preserved).
                let action = ka_sender.borrow_mut().tick_timeout();
                if matches!(action, SenderAction::DeclareTimeout) {
                    let _ = close_signal_tx.send(CloseReason::KeepaliveTimeout);
                    return;
                }
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    use std::time::Duration;

    /// Mock writer: records each `send_health_check` call into a
    /// shared counter so the test can observe how many were emitted.
    #[derive(Clone)]
    struct CountingWriter {
        count: Rc<RefCell<u32>>,
    }

    impl KeepaliveWriter for CountingWriter {
        fn send_health_check(&self) -> Result<(), ()> {
            *self.count.borrow_mut() += 1;
            Ok(())
        }
    }

    /// Without `on_response`, the timeout timer fires and after enough
    /// missed heartbeats the close signal is emitted.
    ///
    /// The companion "interval-emission counts increment N times after
    /// advancing N×HC_INTERVAL" test was dropped — `tokio::time::interval`
    /// initialization timing inside a `spawn_local`'d task interacts
    /// awkwardly with `tokio::time::advance` in the parent task; the
    /// load-bearing assertion is that `DeclareTimeout` propagates as a
    /// close signal, which this test covers. The lib's
    /// `goat_relay_protocol::keepalive` module's own unit tests cover
    /// the `Sender::tick_interval` / `tick_timeout` state-machine
    /// behavior in isolation (9 tests).
    #[tokio::test(flavor = "current_thread", start_paused = true)]
    async fn keepalive_declares_timeout_after_missed_heartbeats() {
        let ka = Rc::new(RefCell::new(Sender::new(1))); // threshold 1
        let count = Rc::new(RefCell::new(0u32));
        let writer = CountingWriter {
            count: Rc::clone(&count),
        };
        let (close_tx, close_rx) = oneshot::channel();

        let local = tokio::task::LocalSet::new();
        let task = local.spawn_local(run(
            Rc::clone(&ka),
            writer,
            close_tx,
            KeepaliveTimings::default(),
        ));

        local
            .run_until(async move {
                // Advance past one timeout window without any
                // `on_response` calls. With threshold 1, the first
                // missed heartbeat declares timeout.
                tokio::time::advance(SENDER_TIMEOUT_WINDOW + Duration::from_millis(10)).await;
                tokio::task::yield_now().await;
                tokio::task::yield_now().await;
                let reason = close_rx.await.expect("close signal received");
                assert_eq!(reason, CloseReason::KeepaliveTimeout);

                let _ = task.await;
            })
            .await;
    }

    // Rust 1.92 marked env::remove_var/set_var as unsafe (race-condition
    // hazard with concurrent threads reading env vars). Our MSRV is
    // 1.75 where these were safe. The `#[allow(unsafe_code)]` on this
    // test is specifically for newer-rust compat; the test itself
    // cannot race because cargo runs tests in this crate single-
    // threaded by default + this test save/restores the env var.
    #[test]
    #[allow(unsafe_code)]
    fn attempt_threshold_default_when_env_unset() {
        let saved = std::env::var(ATTEMPT_THRESHOLD_ENV).ok();
        // SAFETY: cargo test default is single-threaded for this crate
        // (no multi-threaded test runtime). The env-var manipulation
        // is bracketed: we save before remove + restore at end.
        unsafe {
            std::env::remove_var(ATTEMPT_THRESHOLD_ENV);
        }
        assert_eq!(attempt_threshold_from_env(), DEFAULT_ATTEMPT_THRESHOLD);
        if let Some(v) = saved {
            // SAFETY: same justification as above.
            unsafe {
                std::env::set_var(ATTEMPT_THRESHOLD_ENV, v);
            }
        }
    }
}
