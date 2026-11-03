// interceptors_test.go — Block 50B.1 + 50B.2 production-readiness hardening.
//
// Verifies the unary + stream gRPC interceptor chain on a real Server:
//   - bounded-concurrency rejects N+1th call with codes.ResourceExhausted
//     and increments signal_server_handler_rejected_total{reason="saturation"}
//   - panic-recovery interceptor returns codes.Internal on a handler panic
//     (covered indirectly via direct interceptor invocation; we don't have
//     a panicking handler in production code to test against)

package server

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestUnaryConcurrencyLimiterRejectsBeyondCap drives the unary interceptor
// directly with a synthetic handler that blocks until released. We acquire
// `cap` slots, confirm the (cap+1)th rejects with ResourceExhausted, then
// release one slot and confirm a fresh call succeeds.
func TestUnaryConcurrencyLimiterRejectsBeyondCap(t *testing.T) {
	const cap = 2
	reg := prometheus.NewRegistry()
	cl := newConcurrencyLimiter(reg, "test", cap)

	gate := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{}, cap)

	blockingHandler := func(_ context.Context, _ interface{}) (interface{}, error) {
		gate <- struct{}{}
		<-release
		return "ok", nil
	}

	// Acquire `cap` slots concurrently.
	for i := 0; i < cap; i++ {
		go func() {
			_, _ = cl.unaryInterceptor(context.Background(), nil, &grpc.UnaryServerInfo{}, blockingHandler)
			done <- struct{}{}
		}()
	}
	// Wait for both blocking handlers to be running and holding slots.
	for i := 0; i < cap; i++ {
		<-gate
	}

	// Now the (cap+1)th must reject — synchronously, no extra goroutine.
	immediateHandler := func(_ context.Context, _ interface{}) (interface{}, error) {
		t.Error("immediate handler should not be invoked when limiter is saturated")
		return nil, nil
	}
	_, err := cl.unaryInterceptor(context.Background(), nil, &grpc.UnaryServerInfo{}, immediateHandler)
	if err == nil {
		t.Fatal("expected ResourceExhausted on saturated limiter, got nil")
	}
	if got, want := status.Code(err), codes.ResourceExhausted; got != want {
		t.Errorf("err code = %v, want %v", got, want)
	}

	// Confirm rejection counter incremented.
	want := `
# HELP signal_server_handler_rejected_total Total handler invocations rejected at admission, labeled by reason.
# TYPE signal_server_handler_rejected_total counter
signal_server_handler_rejected_total{node="test",reason="saturation"} 1
`
	if err := testutil.GatherAndCompare(reg, strings.NewReader(want), "signal_server_handler_rejected_total"); err != nil {
		t.Errorf("rejection counter wrong: %v", err)
	}

	// Release blocked handlers so they finish + return their slots.
	close(release)
	for i := 0; i < cap; i++ {
		<-done
	}

	// A new call should now succeed.
	cleanHandler := func(_ context.Context, _ interface{}) (interface{}, error) {
		return "ok", nil
	}
	if _, err := cl.unaryInterceptor(context.Background(), nil, &grpc.UnaryServerInfo{}, cleanHandler); err != nil {
		t.Errorf("post-release call failed: %v", err)
	}
}

// TestStreamConcurrencyLimiterRejectsBeyondCap mirrors the unary test for
// the streaming interceptor.
func TestStreamConcurrencyLimiterRejectsBeyondCap(t *testing.T) {
	const cap = 1
	reg := prometheus.NewRegistry()
	cl := newConcurrencyLimiter(reg, "test", cap)

	gate := make(chan struct{})
	release := make(chan struct{})
	var wg sync.WaitGroup

	blockingHandler := func(_ interface{}, _ grpc.ServerStream) error {
		gate <- struct{}{}
		<-release
		return nil
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = cl.streamInterceptor(nil, nil, &grpc.StreamServerInfo{}, blockingHandler)
	}()
	<-gate

	// Saturated; another stream should be rejected.
	rejectHandler := func(_ interface{}, _ grpc.ServerStream) error {
		t.Error("reject handler should not be invoked")
		return nil
	}
	err := cl.streamInterceptor(nil, nil, &grpc.StreamServerInfo{}, rejectHandler)
	if err == nil {
		t.Fatal("expected ResourceExhausted on saturated stream limiter, got nil")
	}
	if got, want := status.Code(err), codes.ResourceExhausted; got != want {
		t.Errorf("err code = %v, want %v", got, want)
	}

	close(release)
	wg.Wait()
}

// TestUnboundedLimiterNeverRejects verifies the cfg.MaxInflight<=0 path:
// no semaphore, no rejection, just accounting.
func TestUnboundedLimiterNeverRejects(t *testing.T) {
	reg := prometheus.NewRegistry()
	cl := newConcurrencyLimiter(reg, "test", 0) // unlimited

	for i := 0; i < 100; i++ {
		_, err := cl.unaryInterceptor(context.Background(), nil, &grpc.UnaryServerInfo{},
			func(_ context.Context, _ interface{}) (interface{}, error) {
				return "ok", nil
			})
		if err != nil {
			t.Fatalf("unbounded limiter rejected call %d: %v", i, err)
		}
	}
}

// TestUnaryPanicRecoveryReturnsInternal confirms the panic-recovery
// interceptor catches handler panics and returns codes.Internal instead of
// crashing the server.
func TestUnaryPanicRecoveryReturnsInternal(t *testing.T) {
	reg := prometheus.NewRegistry()
	registerPanicCounter(reg, "test")

	panicHandler := func(_ context.Context, _ interface{}) (interface{}, error) {
		panic("synthetic handler failure")
	}
	resp, err := unaryPanicRecoveryInterceptor(
		context.Background(),
		nil,
		&grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"},
		panicHandler,
	)
	if err == nil {
		t.Fatal("expected error from panicked handler, got nil")
	}
	if resp != nil {
		t.Errorf("expected nil resp, got %v", resp)
	}
	if got, want := status.Code(err), codes.Internal; got != want {
		t.Errorf("err code = %v, want %v", got, want)
	}

	// Counter should reflect one grpc-handler panic.
	got := testutil.ToFloat64(loadPanicCounter().WithLabelValues("grpc-handler", "grpc-handler"))
	if got != 1 {
		t.Errorf("panic_total{grpc-handler} = %v, want 1", got)
	}
}

// TestStreamPanicRecoveryReturnsInternal mirrors the unary test for the
// streaming interceptor.
func TestStreamPanicRecoveryReturnsInternal(t *testing.T) {
	reg := prometheus.NewRegistry()
	registerPanicCounter(reg, "test")

	panicHandler := func(_ interface{}, _ grpc.ServerStream) error {
		panic("synthetic stream handler failure")
	}
	err := streamPanicRecoveryInterceptor(
		nil,
		nil,
		&grpc.StreamServerInfo{FullMethod: "/test.Service/Stream"},
		panicHandler,
	)
	if err == nil {
		t.Fatal("expected error from panicked stream handler, got nil")
	}
	if got, want := status.Code(err), codes.Internal; got != want {
		t.Errorf("err code = %v, want %v", got, want)
	}
}

