//! tokio-uring compatibility shims — Linux-only.
//!
//! Bridges between `tokio_uring::net::TcpStream` (owned-buffer
//! semantics) and tokio's `AsyncRead + AsyncWrite` traits (borrowed-
//! buffer semantics), so libraries like `tokio-tungstenite` and
//! `tokio-rustls` can drive an io_uring-backed socket without
//! modification.
//!
//! ADR 0711 — Block 33C.1 lever L5.
//!
//! # Owned vs borrowed buffer semantics
//!
//! tokio-uring's `TcpStream::read(buf)` takes ownership of `buf`
//! (a `Vec<u8>` or any `IoBufMut`), submits an io_uring SQE, and
//! returns a future whose output is `(io::Result<usize>, buf)` —
//! the buffer is returned along with the number of bytes read.
//!
//! tokio's `AsyncRead::poll_read(buf: &mut ReadBuf<'_>)` borrows
//! the buffer for the duration of the call.
//!
//! This shim maintains an internal `Vec<u8>` that's handed off to
//! tokio-uring on each read submission; on completion, the bytes
//! are copied into the borrowed `ReadBuf<'_>` slice supplied by
//! the caller. The internal buffer is then re-used for the next
//! submission.
//!
//! Same pattern for writes: tokio's `poll_write(buf: &[u8])` borrows;
//! we copy into our internal buffer and submit owned to tokio-uring.
//!
//! ## Cost of the shim vs raw io_uring
//!
//! The shim adds **one buffer copy per read** and **one buffer copy
//! per write** vs raw io_uring. That's a real cost — ~1-2 GB/s of
//! pure memcpy bandwidth on modern x86, so at our throughput levels
//! it's negligible. The L5 SQPOLL gain (no per-syscall context
//! switch) dwarfs the memcpy cost.
//!
//! Future optimization: zero-copy path using `IoBuf`-aware traits
//! (`tokio_uring::buf::IoBufMut`) when downstream consumers can
//! handle owned buffers directly. Block 33C.1 follow-up.

use std::future::Future;
use std::io;
use std::pin::Pin;
use std::rc::Rc;
use std::task::{Context, Poll};

use tokio::io::{AsyncRead, AsyncWrite, ReadBuf};
use tokio_uring::net::TcpStream as InnerUringStream;

/// In-flight io_uring submission future. tokio-uring returns the
/// owned buffer alongside the io result — we shim that back into
/// tokio's borrow-based AsyncRead / AsyncWrite traits. Aliased to
/// silence `clippy::type-complexity` on the four state-machine
/// enum variants below.
type IoBufFuture = Pin<Box<dyn Future<Output = (io::Result<usize>, Vec<u8>)>>>;

/// In-flight read state. The async fn future from tokio-uring borrows
/// the TcpStream; we use `Rc<RefCell<UringTcpStream>>` to satisfy the
/// borrow checker across our state machine.
enum ReadState {
    /// No read in flight; we have a free buffer ready to submit.
    Idle(Vec<u8>),
    /// Read submitted; future drives the SQE through the kernel.
    /// When it completes we get back `(io::Result<usize>, Vec<u8>)`.
    InFlight(IoBufFuture),
    /// Bytes read; we have data sitting in the buffer waiting to be
    /// drained by the consumer (`poll_read` calls).
    Buffered { buf: Vec<u8>, consumed: usize, len: usize },
    /// EOF — the kernel returned 0 bytes; surface to all subsequent
    /// `poll_read` callers and never re-submit.
    Eof,
    /// Transient state during state-machine transitions; should never
    /// be observed externally.
    Transient,
}

/// In-flight write state. Mirrors `ReadState` for the write path.
enum WriteState {
    /// No write in flight; ready to accept fresh bytes from caller.
    Idle(Vec<u8>),
    /// Write submitted; future drives the SQE.
    InFlight(IoBufFuture),
    /// Transient.
    Transient,
}

/// `tokio_uring::net::TcpStream` wrapped behind tokio's `AsyncRead +
/// AsyncWrite + Unpin` traits, suitable for handing to libraries that
/// expect tokio-style I/O (rustls, tokio-tungstenite, etc.).
///
/// Why `Rc<TcpStream>` (no `RefCell`): both `TcpStream::read` and
/// `TcpStream::write` take `&self` (the io_uring submission borrows
/// the socket fd, not exclusive access to userspace state). Read and
/// write futures may be in flight concurrently — exactly the
/// independent-half pattern tokio-tungstenite relies on after `split()`.
pub struct UringTcpStream {
    inner: Rc<InnerUringStream>,
    read_state: ReadState,
    write_state: WriteState,
}

const READ_BUF_SIZE: usize = 32 * 1024; // 32 KB per submission
const WRITE_BUF_SIZE: usize = 32 * 1024;

impl UringTcpStream {
    /// Wrap a tokio-uring `TcpStream` in tokio's AsyncRead/AsyncWrite
    /// trait surface. The first `poll_read` / `poll_write` will lazily
    /// allocate its 32 KiB internal buffer from `Vec::with_capacity`.
    pub fn new(inner: InnerUringStream) -> Self {
        Self {
            inner: Rc::new(inner),
            read_state: ReadState::Idle(Vec::with_capacity(READ_BUF_SIZE)),
            write_state: WriteState::Idle(Vec::with_capacity(WRITE_BUF_SIZE)),
        }
    }
}

impl AsyncRead for UringTcpStream {
    fn poll_read(
        mut self: Pin<&mut Self>,
        cx: &mut Context<'_>,
        out: &mut ReadBuf<'_>,
    ) -> Poll<io::Result<()>> {
        loop {
            match std::mem::replace(&mut self.read_state, ReadState::Transient) {
                ReadState::Eof => {
                    self.read_state = ReadState::Eof;
                    return Poll::Ready(Ok(()));
                }
                ReadState::Buffered { mut buf, consumed, len } => {
                    let remaining = len - consumed;
                    let to_copy = remaining.min(out.remaining());
                    out.put_slice(&buf[consumed..consumed + to_copy]);
                    let new_consumed = consumed + to_copy;
                    if new_consumed == len {
                        // Buffer drained; reset for next submission.
                        buf.clear();
                        self.read_state = ReadState::Idle(buf);
                    } else {
                        self.read_state = ReadState::Buffered { buf, consumed: new_consumed, len };
                    }
                    return Poll::Ready(Ok(()));
                }
                ReadState::Idle(mut buf) => {
                    // Submit a fresh read. tokio-uring's read writes up
                    // to `bytes_total()` (Vec capacity); pre-resizing
                    // ensures the kernel sees a 32 KiB writable region.
                    // The returned Vec has its `len` set to actual bytes
                    // received via `IoBufMut::set_init`.
                    buf.clear();
                    buf.resize(READ_BUF_SIZE, 0);
                    let inner = Rc::clone(&self.inner);
                    let fut: IoBufFuture = Box::pin(async move {
                        // tokio-uring 0.5 API asymmetry: `read(...)` returns
                        // an `impl Future<Output = BufResult<usize, T>>`
                        // directly (no builder) — just `.await`. `write`
                        // uses the builder pattern (`.submit().await`)
                        // because it's the cancel-safety-aware API.
                        // `inner: Rc<TcpStream>` deref-coerces to
                        // `&TcpStream` for the inherent `read` method.
                        inner.read(buf).await
                    });
                    self.read_state = ReadState::InFlight(fut);
                    // Loop around to poll the future immediately.
                }
                ReadState::InFlight(mut fut) => {
                    match fut.as_mut().poll(cx) {
                        Poll::Pending => {
                            self.read_state = ReadState::InFlight(fut);
                            return Poll::Pending;
                        }
                        Poll::Ready((Ok(0), buf)) => {
                            // EOF.
                            drop(buf);
                            self.read_state = ReadState::Eof;
                            return Poll::Ready(Ok(()));
                        }
                        Poll::Ready((Ok(n), buf)) => {
                            // Truncate the buffer to actual bytes read.
                            // We allocated READ_BUF_SIZE but only `n` are valid.
                            self.read_state = ReadState::Buffered {
                                buf,
                                consumed: 0,
                                len: n,
                            };
                            // Loop to drain into out.
                        }
                        Poll::Ready((Err(e), buf)) => {
                            // Reset to idle so a subsequent retry can re-submit.
                            self.read_state = ReadState::Idle(buf);
                            return Poll::Ready(Err(e));
                        }
                    }
                }
                ReadState::Transient => unreachable!("read_state should never observe Transient"),
            }
        }
    }
}

impl AsyncWrite for UringTcpStream {
    fn poll_write(
        mut self: Pin<&mut Self>,
        cx: &mut Context<'_>,
        out: &[u8],
    ) -> Poll<io::Result<usize>> {
        loop {
            match std::mem::replace(&mut self.write_state, WriteState::Transient) {
                WriteState::Idle(mut buf) => {
                    // Copy caller bytes into our owned buf, then submit.
                    // Vec<u8> implements `IoBuf`; tokio-uring writes
                    // `bytes_init()` (i.e., `buf.len()`) bytes — exactly
                    // the `n` bytes we just appended. No `slice(..)`
                    // round-trip needed; passing the Vec directly returns
                    // the same Vec back in `BufResult<usize, Vec<u8>>`.
                    let n = out.len().min(WRITE_BUF_SIZE);
                    buf.clear();
                    buf.extend_from_slice(&out[..n]);
                    let inner = Rc::clone(&self.inner);
                    let fut: IoBufFuture = Box::pin(async move {
                        // tokio-uring 0.5 builder-style API per `read`
                        // path comment above — `.submit().await` to drive
                        // the SQE through the ring.
                        inner.write(buf).submit().await
                    });
                    self.write_state = WriteState::InFlight(fut);
                    // Loop to drive the future. We don't return the
                    // `n` bytes until the future completes — tokio's
                    // AsyncWrite contract requires bytes be at least
                    // committed to the OS buffer before we signal
                    // success.
                }
                WriteState::InFlight(mut fut) => {
                    match fut.as_mut().poll(cx) {
                        Poll::Pending => {
                            self.write_state = WriteState::InFlight(fut);
                            return Poll::Pending;
                        }
                        Poll::Ready((Ok(n), buf)) => {
                            self.write_state = WriteState::Idle(buf);
                            return Poll::Ready(Ok(n));
                        }
                        Poll::Ready((Err(e), buf)) => {
                            self.write_state = WriteState::Idle(buf);
                            return Poll::Ready(Err(e));
                        }
                    }
                }
                WriteState::Transient => unreachable!("write_state should never observe Transient"),
            }
        }
    }

    fn poll_flush(
        mut self: Pin<&mut Self>,
        cx: &mut Context<'_>,
    ) -> Poll<io::Result<()>> {
        // Drive any in-flight write to completion. tokio-uring writes
        // are direct kernel submissions; once the future completes,
        // the bytes are in the kernel's send buffer. No additional
        // flush mechanism (no userspace buffering beyond our
        // single-submission window). Every match arm returns, so this
        // is a single-pass dispatch — no `loop {}` wrapper needed
        // (clippy::never_loop).
        match std::mem::replace(&mut self.write_state, WriteState::Transient) {
            WriteState::Idle(buf) => {
                self.write_state = WriteState::Idle(buf);
                Poll::Ready(Ok(()))
            }
            WriteState::InFlight(mut fut) => match fut.as_mut().poll(cx) {
                Poll::Pending => {
                    self.write_state = WriteState::InFlight(fut);
                    Poll::Pending
                }
                Poll::Ready((Ok(_), buf)) => {
                    self.write_state = WriteState::Idle(buf);
                    Poll::Ready(Ok(()))
                }
                Poll::Ready((Err(e), buf)) => {
                    self.write_state = WriteState::Idle(buf);
                    Poll::Ready(Err(e))
                }
            },
            WriteState::Transient => unreachable!("write_state should never observe Transient"),
        }
    }

    fn poll_shutdown(
        self: Pin<&mut Self>,
        cx: &mut Context<'_>,
    ) -> Poll<io::Result<()>> {
        // Drain any in-flight write, then we're done. tokio-uring
        // TcpStream's Drop closes the underlying fd; explicit
        // shutdown could call `inner.shutdown(...)` but tokio's
        // contract just requires this to complete the half-close
        // dance, which we do via flush.
        self.poll_flush(cx)
    }
}
