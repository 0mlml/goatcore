// safego_test.go — Block 50B.2 production-readiness hardening.
//
// Tests for the safeGo helper. The two contract guarantees are:
//   1. A panic inside the goroutine does NOT crash the test process.
//   2. The recovered panic increments signal_server_panic_total
//      labeled by the goroutine name passed to safeGo.

package server

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// TestSafeGoRecoversPanic verifies the recover-and-continue contract: a
// panicking goroutine must not propagate the panic past the deferred
// recover, and the supplied function still gets called.
//
// Test synchronization gotcha: the close(done) below executes during
// panic-unwind BEFORE safeGo's outer recover-and-increment defer fires.
// We poll the counter rather than racing the gather against the recovery.
func TestSafeGoRecoversPanic(t *testing.T) {
	reg := prometheus.NewRegistry()
	cv := registerPanicCounter(reg, "test")

	done := make(chan struct{})
	safeGo("test-panic", func() {
		defer close(done)
		panic("synthetic-failure")
	})
	<-done

	// Counter increment lives in safeGo's outer deferred recover, which
	// runs strictly after close(done) above. Poll briefly until the
	// recovery has finished its work.
	deadline := time.Now().Add(2 * time.Second)
	for testutil.ToFloat64(cv.WithLabelValues("test-panic", "goroutine")) == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("panic counter never incremented within deadline")
		}
		time.Sleep(time.Millisecond)
	}

	want := `
# HELP signal_server_panic_total Total recovered panics, labeled by goroutine name and kind (goroutine vs grpc-handler).
# TYPE signal_server_panic_total counter
signal_server_panic_total{goroutine="test-panic",kind="goroutine",node="test"} 1
`
	if err := testutil.GatherAndCompare(reg, strings.NewReader(want), "signal_server_panic_total"); err != nil {
		t.Errorf("panic counter not incremented as expected: %v", err)
	}
}

// TestSafeGoNormalReturn confirms the wrapper does not interfere with the
// happy path — a fn that returns cleanly should not increment the panic
// counter.
func TestSafeGoNormalReturn(t *testing.T) {
	reg := prometheus.NewRegistry()
	registerPanicCounter(reg, "test")

	var wg sync.WaitGroup
	wg.Add(1)
	safeGo("test-clean", func() {
		defer wg.Done()
		// Do nothing — clean return.
	})
	wg.Wait()

	got := testutil.ToFloat64(loadPanicCounter().WithLabelValues("test-clean", "goroutine"))
	if got != 0 {
		t.Errorf("panic_total{goroutine=test-clean} = %v, want 0", got)
	}
}

// TestSafeGoNilCounterSafe confirms safeGo doesn't panic on a nil package-
// level counter (e.g. early process startup or in test setups that haven't
// called registerPanicCounter). The recover must still log; it just can't
// increment the counter.
func TestSafeGoNilCounterSafe(t *testing.T) {
	// Save and restore so other tests aren't affected. Use the atomic
	// pointer's Store so the Load in safeGo's recover path is race-free.
	saved := panicCounter.Load()
	panicCounter.Store(nil)
	defer panicCounter.Store(saved)

	done := make(chan struct{})
	safeGo("test-nil-counter", func() {
		defer close(done)
		panic("synthetic-failure-nil-counter")
	})
	<-done
	// If we got here, nil-safety holds.
}
