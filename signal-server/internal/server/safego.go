// safego.go — Block 50B.2 production-readiness hardening.
//
// safeGo wraps a goroutine spawn with structured panic recovery. A panic in
// any background goroutine in the signal-server crashes the process by Go's
// default; in production that means a rolling outage every time a corner-case
// bug fires anywhere in the binary. This is unacceptable for a control-plane
// service that other peers depend on.
//
// The wrapper:
//   - Recovers any panic value
//   - Logs a structured slog event with the panic value, the caller-supplied
//     name (so the log identifies which goroutine entry point died), the
//     goroutine label, and a full stack trace
//   - Increments the signal_server_panic_total counter labeled by goroutine
//     name and the constant site label (so per-site dashboards can attribute)
//   - Returns control instead of unwinding past the deferred recover —
//     the caller's goroutine simply ends
//
// Every bare `go func() { ... }()` in the package is rewritten to use
// safeGo. Tests are intentionally NOT migrated (test goroutines are scoped
// to the test runner and their panics-as-test-failures are desirable).

package server

import (
	"log/slog"
	"runtime/debug"
	"sync/atomic"

	"github.com/prometheus/client_golang/prometheus"
)

// panicCounter is the package-level Prometheus collector for goroutine and
// gRPC-handler panics. Set by registerPanicCounter (called once per
// metrics construction); nil-safe so calls from tests or pre-registration
// paths don't blow up.
//
// Guarded by atomic.Pointer because:
//   - Multiple Server instances in one test process (parallel subtests,
//     test-helper newTestServer pattern) call registerPanicCounter
//     concurrently with goroutines that read it through safeGo.
//   - Tests deliberately swap the global to nil to assert nil-safety.
var panicCounter atomic.Pointer[prometheus.CounterVec]

// loadPanicCounter is a small helper that returns the current package-level
// counter or nil if none has been registered. Read paths use this instead of
// touching the atomic directly.
func loadPanicCounter() *prometheus.CounterVec {
	return panicCounter.Load()
}

// registerPanicCounter creates and registers the panic_total CounterVec on
// the supplied Registerer. Returns the CounterVec so callers can inspect
// it in tests if needed. Safe to call multiple times in different test
// processes — each test uses a fresh registry.
//
// Labels:
//   - goroutine: caller-supplied name passed to safeGo, or "grpc-handler"
//     for handler-recovery interceptor invocations
//   - kind: "goroutine" for safeGo recoveries, "grpc-handler" for unary or
//     streaming gRPC handler recoveries
func registerPanicCounter(reg prometheus.Registerer, node string) *prometheus.CounterVec {
	cv := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:        "signal_server_panic_total",
			Help:        "Total recovered panics, labeled by goroutine name and kind (goroutine vs grpc-handler).",
			ConstLabels: prometheus.Labels{"node": node},
		},
		[]string{"goroutine", "kind"},
	)
	reg.MustRegister(cv)
	panicCounter.Store(cv)
	return cv
}

// SafeSpawn is the exported entry point for callers outside this package
// (Block 50E.6 — cmd/signal-server/main.go's grpc + metrics listener
// goroutines). It delegates to the private safeGo so internal and
// external callers share the same panic-handling + slog + panic_total
// machinery.
//
// Internal callers should keep using the unexported safeGo for brevity.
func SafeSpawn(name string, fn func()) {
	safeGo(name, fn)
}

// safeGo spawns fn in a new goroutine, recovering any panic. name identifies
// the goroutine in logs and metrics — keep it short, descriptive, and
// stable (it's a metric label, so cardinality matters).
//
// Use this in place of every `go func() { ... }()` in production code paths.
// The recover-and-continue posture is deliberate: the alternative (process
// exit on any background panic) creates user-visible outages from
// non-fatal bugs.
func safeGo(name string, fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				stack := debug.Stack()
				slog.Error("recovered panic in goroutine",
					"goroutine", name,
					"panic", r,
					"stack", string(stack),
				)
				if pc := loadPanicCounter(); pc != nil {
					pc.WithLabelValues(name, "goroutine").Inc()
				}
			}
		}()
		fn()
	}()
}
