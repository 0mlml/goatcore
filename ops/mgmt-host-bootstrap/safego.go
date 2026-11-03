// safego.go — panic-recovering goroutine wrapper for mgmt-host-bootstrap.
//
// Block 50B.2 production-readiness hardening (Tier 2 — durability + tail-risk).
//
// mgmt-host-bootstrap is a one-shot CLI: it seeds DB rows for a single
// invocation and exits. Today the CLI does not spawn any goroutines (the
// surface area is intentionally small), but this helper is here so future
// spawn sites use the recovering wrapper rather than a bare `go fn()`.
//
// Posture difference from long-running daemons (signal-server, time-hygiene):
// for those, a goroutine panic should be recovered + logged + the process
// kept alive (the failure stays scoped to one connection). For this CLI, a
// goroutine panic is unrecoverable program state — bootstrap is one-shot,
// so any background work that crashes leaves the foreground in an
// inconsistent state. We log the panic with name + stack and exit non-zero
// so the operator sees a clean failure instead of a wedged tx.
//
// Usage:
//
//	safeGo("background-tail", func() { /* work */ })
//
// If/when this CLI grows long-running subcommands (e.g. a watch / drain
// mode), revisit the exit-on-panic policy at that time.
package main

import (
	"log/slog"
	"os"
	"runtime/debug"
)

// safeGo runs fn in a new goroutine, recovering any panic and emitting a
// structured log entry with the goroutine name + stack trace. After
// reporting, the process exits 1 — see file header for rationale.
//
// Tests can override exitFn to assert on the recovery path without killing
// the test binary.
var exitFn = os.Exit

func safeGo(name string, fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("goroutine panic in mgmt-host-bootstrap",
					"goroutine", name,
					"panic", r,
					"stack", string(debug.Stack()),
				)
				exitFn(1)
			}
		}()
		fn()
	}()
}
