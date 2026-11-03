// Package safego provides panic-safe goroutine spawning for snitch.
//
// Block 50E.4 — Panic-recovery middleware. Mirror of the time-hygiene
// safego package shape (ops/time-hygiene/internal/safego) so the two
// load-bearing Go services share the same panic-recovery surface and a
// future move to a shared lib is mechanical.
//
// Rationale: a panic in any unrecovered goroutine kills the entire snitch
// process, taking down the relay-health visibility surface across every
// production peer that is reporting. The satlink prober and the
// admin-triggered ad-hoc probe both spawn long-running goroutines that
// today have no recovery; one panic in a measure helper would silently
// kill the listener.
//
// Spawn wraps `go func(){ ... }()` with `defer recover()`. On panic it
// logs a structured slog event with the panic value + full stack trace +
// the caller-supplied name and increments a process-local atomic counter
// (PanicTotal()) for any future /metrics endpoint to read. The wrapped
// function does NOT re-panic; the goroutine returns normally and the
// surrounding loop / caller is expected to either re-spawn or accept the
// loss.
//
// No Prometheus dep is added because snitch does not yet expose /metrics.
// If/when it does, swap PanicTotal() for a real CounterVec without
// changing call sites.
package safego

import (
	"log/slog"
	"runtime/debug"
	"sync/atomic"
)

var panicTotal atomic.Int64

// PanicTotal returns the cumulative count of recovered panics across all
// Spawn-and-Recover goroutines for the lifetime of the process. Suitable
// for exposing through a future /metrics endpoint.
func PanicTotal() int64 {
	return panicTotal.Load()
}

// Spawn launches fn in a new goroutine guarded by a defer-recover. On
// panic it logs the panic value + full stack trace + name via slog,
// increments the process-local panic counter, and returns from the
// goroutine without re-panicking. The caller is responsible for any
// restart logic.
//
// name should be a stable, low-cardinality descriptor of the goroutine's
// purpose (e.g. "satlink-prober", "satlink-adhoc-probe"). It will
// become a Prometheus label when /metrics lands, so do not embed request
// IDs or per-call data.
func Spawn(name string, fn func()) {
	go func() {
		defer Recover(name)
		fn()
	}()
}

// Recover is the deferred-recover body used by Spawn and exposed for
// callers who already have their own go-statement (e.g. wrapped in a
// sync.WaitGroup) but still want the same panic-handling shape.
//
// Usage:
//
//	go func() {
//	    defer wg.Done()
//	    defer safego.Recover("my-goroutine")
//	    // body
//	}()
//
// When chaining defers, place safego.Recover INNERMOST (closest to the
// body) so any other defers (Done, Close) still fire after recovery.
func Recover(name string) {
	if r := recover(); r != nil {
		stack := debug.Stack()
		slog.Error("snitch goroutine panic recovered",
			"goroutine", name,
			"panic", r,
			"stack", string(stack),
		)
		panicTotal.Add(1)
	}
}
