//! Shard — one pinned OS thread, one single-threaded tokio runtime, one core.
//!
//! # Architecture invariants (ADR 0709 D7)
//!
//! Each `Shard` is one OS thread executing its own
//! [`tokio::runtime::Builder::new_current_thread()`] runtime. **No
//! multi-threaded runtime exists anywhere in the binary** — work-stealing
//! is structurally impossible at runtime, not just disabled.
//!
//! ## Five-layer mitigation (ADR 0709 D7)
//!
//! - **Layer 1 — Runtime config.** [`Builder::new_current_thread`] in
//!   [`Shard::spawn`]. There is no scheduler that could steal tasks.
//! - **Layer 2 — Type system.** Caller-supplied futures run inside a
//!   [`LocalSet`]; per-connection state can be `Rc<RefCell<...>>`
//!   (`!Send`). The compiler refuses to move shard-local state across
//!   threads.
//! - **Layer 3 — Clippy lint.** Workspace `clippy.toml` blocks
//!   `tokio::spawn` and `tokio::task::spawn` (the global multi-threaded
//!   APIs). Spawn must use `tokio::task::spawn_local` (which targets
//!   the current `LocalSet`).
//! - **Layer 4 — Architecture comment.** This module-level doc comment.
//! - **Layer 5 — Boot-time assertion.** Deferred until we add
//!   `RUSTFLAGS="--cfg tokio_unstable"` (`Handle::metrics()` is
//!   currently gated). Layers 1-4 are sufficient.
//!
//! # Init closure pattern (step 2)
//!
//! [`Shard::spawn`] takes a user-supplied `init` closure that receives
//! a shutdown [`oneshot::Receiver<()>`] and returns a `Future<Output =
//! ()>`. The future runs inside the shard's [`LocalSet`]; the shard
//! exits when the future returns. Standard pattern: the future races
//! the shutdown receiver against the shard's real work (a listener,
//! a routing loop, etc.) and returns when shutdown fires or work
//! completes.

use std::future::Future;
use std::io;
use std::sync::mpsc;
use std::thread;

use tokio::sync::oneshot;

/// A shard: one pinned OS thread running its own current-thread tokio
/// runtime.
///
/// Construct via [`Shard::spawn`] with an init closure. The OS thread
/// runs the closure's returned future inside a [`LocalSet`] until it
/// completes.
pub struct Shard {
    /// OS thread join handle, taken on shutdown.
    handle: Option<thread::JoinHandle<()>>,
    /// Shutdown signal sender — the init future awaits this.
    shutdown: Option<oneshot::Sender<()>>,
}

impl Shard {
    /// Spawn a new shard pinned (best-effort) to `core_id`, running the
    /// future returned by `init`.
    ///
    /// `init` is called *on the shard's thread* (not on the caller's
    /// thread), inside the shard's [`LocalSet`]. It receives a shutdown
    /// [`oneshot::Receiver<()>`] that fires when [`Shard::shutdown`] is
    /// called. The returned future drives the shard's real work; when
    /// it completes, the shard exits.
    ///
    /// Returns once the OS thread has booted and the init future has
    /// reported readiness via an internal boot-channel send.
    ///
    /// # Errors
    ///
    /// Returns `Err` if [`thread::Builder::spawn`] fails or the runtime
    /// fails to build. A core-pinning failure is logged but does NOT
    /// return an error — the runtime config (layer 1) is the load-bearing
    /// invariant, not the pinning.
    // `io::Error::new(io::ErrorKind::Other, ...)` is used in place of
    // `io::Error::other(...)` (clippy's `io_other_error` suggestion)
    // because `Error::other` was stabilized in Rust 1.87 and our
    // `rust-version` is pinned to 1.75.
    #[allow(clippy::io_other_error)]
    pub fn spawn<F, Fut>(core_id: core_affinity::CoreId, init: F) -> io::Result<Self>
    where
        F: FnOnce(oneshot::Receiver<()>) -> Fut + Send + 'static,
        Fut: Future<Output = ()>,
    {
        let (shutdown_tx, shutdown_rx) = oneshot::channel::<()>();
        let (boot_tx, boot_rx) = mpsc::channel::<io::Result<()>>();

        let handle = thread::Builder::new()
            .name(format!("goat-relay-shard-core{}", core_id.id))
            .spawn(move || {
                if !core_affinity::set_for_current(core_id) {
                    eprintln!(
                        "goat-relay-shard: core_affinity::set_for_current returned false for core_id={} \
                         — continuing without pinning (runtime config is the load-bearing invariant)",
                        core_id.id
                    );
                }

                // ADR 0709 D7 layer 1 + ADR 0711 lever L5: single-threaded
                // runtime per pinned thread. On Linux, this is tokio-uring's
                // io_uring-backed runtime (SQPOLL + multishot recv); on
                // non-Linux it's vanilla tokio current_thread + LocalSet.
                // The cfg-gating lives in `crate::runtime::start_local`.
                let boot_tx_for_init = boot_tx.clone();
                let result = crate::runtime::start_local(async move {
                    // Signal boot success once we're inside the runtime.
                    let _ = boot_tx_for_init.send(Ok(()));
                    init(shutdown_rx).await;
                });
                if let Err(e) = result {
                    let _ = boot_tx.send(Err(e));
                }
            })?;

        match boot_rx.recv() {
            Ok(Ok(())) => Ok(Self {
                handle: Some(handle),
                shutdown: Some(shutdown_tx),
            }),
            Ok(Err(e)) => {
                let _ = handle.join();
                Err(e)
            }
            Err(_) => Err(io::Error::new(
                io::ErrorKind::Other,
                "shard thread exited before boot signal",
            )),
        }
    }

    /// Signal the shard to shut down and wait for the OS thread to exit.
    ///
    /// Idempotent: a second call after the shard has already shut down is
    /// a no-op.
    pub fn shutdown(mut self) {
        if let Some(tx) = self.shutdown.take() {
            let _ = tx.send(());
        }
        if let Some(handle) = self.handle.take() {
            let _ = handle.join();
        }
    }
}

impl Drop for Shard {
    /// Best-effort shutdown if the caller forgot to call [`Shard::shutdown`].
    fn drop(&mut self) {
        if let Some(tx) = self.shutdown.take() {
            let _ = tx.send(());
        }
        if let Some(handle) = self.handle.take() {
            let _ = handle.join();
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn shard_boots_idle_init_and_shuts_down() {
        let cores = core_affinity::get_core_ids().expect("at least one core");
        let core = *cores.first().expect("at least one core");
        // Idle init: just await the shutdown signal and return.
        let shard = Shard::spawn(core, |shutdown| async move {
            let _ = shutdown.await;
        })
        .expect("shard boots");
        shard.shutdown();
    }

    #[test]
    fn shard_drop_handles_shutdown() {
        let cores = core_affinity::get_core_ids().expect("at least one core");
        let core = *cores.first().expect("at least one core");
        let shard = Shard::spawn(core, |shutdown| async move {
            let _ = shutdown.await;
        })
        .expect("shard boots");
        // Drop without explicit shutdown — Drop impl should signal + join.
        drop(shard);
    }
}
