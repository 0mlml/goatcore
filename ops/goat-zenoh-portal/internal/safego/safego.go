// Package safego provides a panic-safe goroutine spawner for goat-zenoh-portal.
//
// Mirror of ops/snitch/internal/safego and ops/time-hygiene/internal/safego so
// the load-bearing Go services share the same panic-recovery shape and a future
// move to a shared lib is mechanical. Block 50B Tier-2 (panic-recovery wrapper
// for every goroutine entry point).
//
// One panic in any SSE stream goroutine or background reload watcher would
// otherwise kill the portal process and take down the vendor-facing UI for the
// trial. Spawn wraps `go func(){ ... }()` with `defer recover()`. On panic it
// logs a structured slog event with the panic value + full stack trace + the
// caller-supplied name and increments a process-local atomic counter
// (PanicTotal()) for any future /metrics endpoint to read. The wrapped function
// does NOT re-panic; the goroutine returns normally.
package safego

import (
	"log/slog"
	"runtime/debug"
	"sync/atomic"
)

var panicTotal atomic.Uint64

// Spawn runs fn in a new goroutine guarded by a panic-recovery wrapper.
// name is used in the structured log on panic; pick a short, stable label.
func Spawn(name string, fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				panicTotal.Add(1)
				slog.Error("goroutine panic recovered",
					"goroutine", name,
					"panic", r,
					"stack", string(debug.Stack()),
				)
			}
		}()
		fn()
	}()
}

// PanicTotal returns the total number of panics recovered since process start.
// Exposed for /metrics or test assertions.
func PanicTotal() uint64 {
	return panicTotal.Load()
}
