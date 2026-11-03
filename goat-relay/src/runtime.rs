//! Per-shard runtime selection — Linux uses io_uring via `tokio-uring`;
//! macOS dev keeps vanilla tokio `current_thread` (per ADR 0711).
//!
//! # ADR 0711 — Block 33C.1 lever L5
//!
//! Each shard runs its real work via [`start_local`]. On Linux this
//! sets up an io_uring-backed runtime + `LocalSet`; on non-Linux
//! platforms it sets up a vanilla tokio `current_thread` runtime +
//! `LocalSet` (preserving macOS dev ergonomics).
//!
//! The shard architecture invariants from ADR 0709 D7 stay intact:
//!
//! - **Layer 1 — single-threaded runtime per pinned thread.** Both
//!   tokio-uring and vanilla-tokio `current_thread` are single-threaded
//!   by construction.
//! - **Layer 2 — `!Send` per-connection state.** `Rc<RefCell<...>>` in
//!   `goat-relay-protocol`'s `KeepaliveSender` and `goat-relay`'s
//!   `RoutingTable` (subscription registry) keep per-connection state
//!   shard-local. The L8 `Arc<RwLock<...>>` for the cross-shard
//!   connection map is unaffected.
//! - **Layer 3 — clippy lint blocks `tokio::spawn`.** Spawn must use
//!   `tokio::task::spawn_local` (which targets the current `LocalSet`
//!   in both runtimes — tokio-uring re-exports tokio's `spawn_local`
//!   because it builds on tokio's `LocalSet`).
//!
//! # Buffer ownership semantics
//!
//! tokio-uring's I/O traits use *owned-buffer* semantics: a `read(buf)`
//! call takes ownership of `buf` and returns it back along with the
//! result. This is the io_uring submission-queue model — buffers are
//! pinned for the duration of the kernel operation. tokio's vanilla
//! `AsyncRead`/`AsyncWrite` use borrowed-buffer semantics.
//!
//! Adapter shims at the I/O boundary (TCP listener accept, WS upgrade,
//! quinn endpoint) bridge the two models. See `listener/ws.rs` and
//! `listener/quic.rs` for the integration points.

use std::future::Future;

/// Run `future` to completion on a single-threaded runtime + `LocalSet`.
/// This is the single entrypoint for the shard's main work loop.
///
/// On Linux, runtime selection is **gated by the `GOAT_RELAY_USE_URING`
/// env var** (not by cfg alone). When set, uses `tokio_uring::start`
/// (io_uring-backed reactor with SQPOLL + multishot recv support); the
/// io_uring runtime requires `io_uring_setup` syscall access which
/// Docker's default seccomp profile blocks, so this path is
/// reachable only when the operator opts in via env var AND grants
/// the necessary security surface.
///
/// When the env var is absent, falls back to vanilla tokio
/// `current_thread` + `LocalSet` — matching the M-018 baseline runtime
/// shape that does not require io_uring access. This keeps the default
/// production path runnable under Docker's default seccomp profile.
///
/// On non-Linux platforms (macOS dev builds, tests on darwin), uses
/// vanilla tokio `current_thread` + `LocalSet` regardless of env var.
///
/// # Errors
///
/// Returns the runtime construction error if the underlying runtime
/// fails to build (extremely rare; usually only OOM or fd exhaustion).
#[cfg(target_os = "linux")]
pub fn start_local<F>(future: F) -> std::io::Result<()>
where
    F: Future<Output = ()> + 'static,
{
    if std::env::var_os("GOAT_RELAY_USE_URING").is_some() {
        // L5 path — io_uring runtime. Requires `io_uring_setup` syscall
        // access (operator must set seccomp opt-out). See F-038 for the
        // dual-process bind-conflict observed in chainguard glibc-dynamic
        // when this path is used.
        tokio_uring::start(future);
        return Ok(());
    }
    // Default Linux path — vanilla tokio current_thread + LocalSet.
    // Matches M-018 baseline runtime shape; no io_uring access needed.
    use tokio::runtime::Builder;
    use tokio::task::LocalSet;
    let runtime = Builder::new_current_thread().enable_all().build()?;
    let local = LocalSet::new();
    runtime.block_on(local.run_until(future));
    Ok(())
}

/// Non-Linux fallback — vanilla tokio current_thread runtime + LocalSet.
#[cfg(not(target_os = "linux"))]
pub fn start_local<F>(future: F) -> std::io::Result<()>
where
    F: Future<Output = ()> + 'static,
{
    use tokio::runtime::Builder;
    use tokio::task::LocalSet;

    let runtime = Builder::new_current_thread().enable_all().build()?;
    let local = LocalSet::new();
    runtime.block_on(local.run_until(future));
    Ok(())
}
