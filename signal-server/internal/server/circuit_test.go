// circuit_test.go — Block 50B.6 production-readiness hardening.
//
// Verifies the per-peer Forward circuit-breaker transitions:
//   - closed → open after N consecutive failures
//   - open: skipped, federation_circuit_skipped_total increments
//   - open → half-open after timeout elapses, breaker probes
//   - half-open → closed on probe success
//
// Uses an in-process gRPC server with a programmable Forward handler so
// we can fail-on-demand without external mocks.

package server

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/sony/gobreaker/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	federationpb "github.com/usareur-af/goatcore/signal-server/gen/federation"
	signalpb "github.com/usareur-af/goatcore/signal-server/gen/signalexchange"
)

// programmableFederation is a minimal SignalFederationServer whose Forward
// behavior is controlled by an atomic. mode=0 → Delivered=true; mode=1 →
// return error; mode=2 → block (then fail) to drive the per-call timeout
// path.
type programmableFederation struct {
	federationpb.UnimplementedSignalFederationServer
	mode    *atomic.Int32 // 0=ok, 1=err, 2=hang
	calls   *atomic.Int32
	hangFor time.Duration
}

func (p *programmableFederation) Forward(ctx context.Context, _ *signalpb.EncryptedMessage) (*federationpb.ForwardResult, error) {
	p.calls.Add(1)
	switch p.mode.Load() {
	case 1:
		return nil, errors.New("synthetic-failure")
	case 2:
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(p.hangFor):
			return nil, errors.New("synthetic-failure-after-hang")
		}
	}
	return &federationpb.ForwardResult{Delivered: true}, nil
}

// startProgrammablePeer fires a SignalFederation server on a bufconn,
// returning a dialable address (the bufconn itself, exposed via a custom
// dialer in setup).
func startProgrammablePeer(t *testing.T, mode *atomic.Int32, calls *atomic.Int32, hangFor time.Duration) (string, func()) {
	t.Helper()
	lis := bufconn.Listen(1 << 16)
	grpcSrv := grpc.NewServer()
	federationpb.RegisterSignalFederationServer(grpcSrv, &programmableFederation{
		mode:    mode,
		calls:   calls,
		hangFor: hangFor,
	})

	serveDone := make(chan struct{})
	go func() {
		defer close(serveDone)
		_ = grpcSrv.Serve(lis)
	}()

	cleanup := func() {
		grpcSrv.GracefulStop()
		<-serveDone
	}
	// We return a sentinel string and rely on the dialer side to route
	// "bufconn" through lis.Dial. We construct the federation client by
	// hand rather than via newFederationClient so we can plug in the
	// bufconn dialer.
	_ = lis
	t.Cleanup(cleanup)

	// Wrap the bufconn in a separate gRPC client that the test will swap
	// into the federationPeer. We need the listener to construct it.
	// Return a pointer-ish "address" the caller can ignore; the real
	// bufconn handle is passed back via a closure in setup.
	addr := "bufconn-peer"
	t.Cleanup(func() { _ = lis.Close() })

	// Stash the dialer keyed by addr — see makeFederationClientForBufconn.
	bufconnRegistry.Store(addr, lis)
	return addr, cleanup
}

// bufconnRegistry maps a peer "address" string to its bufconn listener so
// the test-only dialer can find it. Reset per-test by using unique addrs.
var bufconnRegistry = newBufconnReg()

type bufconnReg struct {
	m map[string]*bufconn.Listener
}

func newBufconnReg() *bufconnReg { return &bufconnReg{m: make(map[string]*bufconn.Listener)} }
func (r *bufconnReg) Store(k string, v *bufconn.Listener) {
	r.m[k] = v
}
func (r *bufconnReg) Get(k string) *bufconn.Listener {
	return r.m[k]
}

// makeFederationClientForBufconn builds a federationClient whose single
// peer dials a bufconn. Lets us drive Broadcast against a programmable
// server without real TCP.
func makeFederationClientForBufconn(t *testing.T, reg prometheus.Registerer, peerAddr string, cfg federationClientConfig) *federationClient {
	t.Helper()
	if cfg.CircuitConsecutiveFailures == 0 {
		cfg.CircuitConsecutiveFailures = defaultCircuitConsecutiveFailures
	}
	if cfg.CircuitOpenDuration <= 0 {
		cfg.CircuitOpenDuration = defaultCircuitOpenDuration
	}

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	_ = logger

	// Build metrics + gauges by hand using the same shape as
	// newFederationClient.
	r := newRegistry()
	m := newMetrics(reg, "test", r)

	labels := prometheus.Labels{"node": "test"}
	timeoutTotal := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:        "federation_forward_timeout_total",
			Help:        "tt",
			ConstLabels: labels,
		},
		[]string{"peer"},
	)
	reg.MustRegister(timeoutTotal)
	circuitState := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name:        "federation_circuit_state",
			Help:        "cs",
			ConstLabels: labels,
		},
		[]string{"peer"},
	)
	reg.MustRegister(circuitState)
	skippedTotal := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:        "federation_circuit_skipped_total",
			Help:        "st",
			ConstLabels: labels,
		},
		[]string{"peer"},
	)
	reg.MustRegister(skippedTotal)
	forwardLatency := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:        "federation_forward_duration_seconds",
			Help:        "ld",
			ConstLabels: labels,
		},
		[]string{"peer"},
	)
	reg.MustRegister(forwardLatency)

	lis := bufconnRegistry.Get(peerAddr)
	if lis == nil {
		t.Fatalf("no bufconn registered for %q", peerAddr)
	}
	dialer := func(ctx context.Context, _ string) (net.Conn, error) {
		return lis.DialContext(ctx)
	}
	conn, err := grpc.NewClient(
		"passthrough:///"+peerAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(dialer),
	)
	if err != nil {
		t.Fatalf("dial bufconn: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	breaker := gobreaker.NewCircuitBreaker[*federationpb.ForwardResult](gobreaker.Settings{
		Name:    "federation:" + peerAddr,
		Timeout: cfg.CircuitOpenDuration,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures >= cfg.CircuitConsecutiveFailures
		},
		OnStateChange: func(_ string, _ gobreaker.State, to gobreaker.State) {
			circuitState.WithLabelValues(peerAddr).Set(circuitStateToFloat(to))
		},
	})
	circuitState.WithLabelValues(peerAddr).Set(0)

	return &federationClient{
		peers: []*federationPeer{{
			addr:    peerAddr,
			conn:    conn,
			rpc:     federationpb.NewSignalFederationClient(conn),
			breaker: breaker,
		}},
		metrics:        m,
		timeoutTotal:   timeoutTotal,
		circuitState:   circuitState,
		skippedTotal:   skippedTotal,
		forwardLatency: forwardLatency,
	}
}

// TestCircuitBreakerOpensAfterConsecutiveFailures drives the breaker
// transition closed → open after the configured threshold and verifies
// the open state short-circuits subsequent calls.
func TestCircuitBreakerOpensAfterConsecutiveFailures(t *testing.T) {
	mode := &atomic.Int32{}
	calls := &atomic.Int32{}
	mode.Store(1) // fail every call

	addr, _ := startProgrammablePeer(t, mode, calls, 0)

	reg := prometheus.NewRegistry()
	fc := makeFederationClientForBufconn(t, reg, addr, federationClientConfig{
		CircuitConsecutiveFailures: 3,
		CircuitOpenDuration:        500 * time.Millisecond,
	})
	t.Cleanup(func() { _ = fc.Close() })

	msg := &signalpb.EncryptedMessage{Key: "k", RemoteKey: "r", Body: []byte("p")}

	// 3 consecutive failures should drive closed → open.
	for i := 0; i < 3; i++ {
		fc.Broadcast(context.Background(), msg)
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("expected 3 RPC calls before breaker opens, got %d", got)
	}

	// Next attempt should be skipped (no new RPC).
	fc.Broadcast(context.Background(), msg)
	if got := calls.Load(); got != 3 {
		t.Errorf("breaker did not short-circuit: calls=%d, want 3", got)
	}

	// Skipped counter should be 1.
	skipped := testutil.ToFloat64(fc.skippedTotal.WithLabelValues(addr))
	if skipped != 1 {
		t.Errorf("federation_circuit_skipped_total{peer=%q} = %v, want 1", addr, skipped)
	}
	// Circuit-state gauge should read open (=1).
	state := testutil.ToFloat64(fc.circuitState.WithLabelValues(addr))
	if state != 1 {
		t.Errorf("federation_circuit_state{peer=%q} = %v, want 1 (open)", addr, state)
	}
}

// TestCircuitBreakerHalfOpenAndRecover transitions the breaker through
// closed → open → half-open → closed by switching the server-side mode
// from failing to succeeding and waiting out the open duration.
func TestCircuitBreakerHalfOpenAndRecover(t *testing.T) {
	mode := &atomic.Int32{}
	calls := &atomic.Int32{}
	mode.Store(1) // start failing

	addr, _ := startProgrammablePeer(t, mode, calls, 0)

	reg := prometheus.NewRegistry()
	openDuration := 200 * time.Millisecond
	fc := makeFederationClientForBufconn(t, reg, addr, federationClientConfig{
		CircuitConsecutiveFailures: 2,
		CircuitOpenDuration:        openDuration,
	})
	t.Cleanup(func() { _ = fc.Close() })

	msg := &signalpb.EncryptedMessage{Key: "k", RemoteKey: "r", Body: []byte("p")}

	// Trip the breaker.
	for i := 0; i < 2; i++ {
		fc.Broadcast(context.Background(), msg)
	}
	if got := testutil.ToFloat64(fc.circuitState.WithLabelValues(addr)); got != 1 {
		t.Fatalf("breaker did not open: state=%v", got)
	}

	// Switch server-side to succeed for the half-open probe.
	mode.Store(0)

	// Wait for the open window to expire.
	time.Sleep(openDuration + 50*time.Millisecond)

	callsBefore := calls.Load()
	delivered, _ := fc.Broadcast(context.Background(), msg)
	if !delivered {
		t.Errorf("expected probe to deliver after open expiry, got delivered=false")
	}
	if calls.Load() <= callsBefore {
		t.Errorf("probe did not call peer: calls before=%d, after=%d", callsBefore, calls.Load())
	}
	// State should be closed (=0) after probe success.
	if got := testutil.ToFloat64(fc.circuitState.WithLabelValues(addr)); got != 0 {
		t.Errorf("breaker did not close after probe: state=%v, want 0", got)
	}
}

// TestForwardTimeoutFires drives the timeout path (mode=2 hang) and
// confirms federation_forward_timeout_total increments. The per-peer
// deadline is the package-level federationForwardTimeout constant
// (200ms per Block 52A.4 RTT analysis); the programmable peer hangs
// for 5s so the deadline is guaranteed to fire.
func TestForwardTimeoutFires(t *testing.T) {
	mode := &atomic.Int32{}
	calls := &atomic.Int32{}
	mode.Store(2) // hang

	addr, _ := startProgrammablePeer(t, mode, calls, 5*time.Second)

	reg := prometheus.NewRegistry()
	fc := makeFederationClientForBufconn(t, reg, addr, federationClientConfig{
		CircuitConsecutiveFailures: 100, // don't open the breaker for this test
		CircuitOpenDuration:        time.Hour,
	})
	t.Cleanup(func() { _ = fc.Close() })

	msg := &signalpb.EncryptedMessage{Key: "k", RemoteKey: "r", Body: []byte("p")}
	delivered, _ := fc.Broadcast(context.Background(), msg)
	if delivered {
		t.Errorf("Broadcast should not have delivered when peer hangs past timeout")
	}
	timeouts := testutil.ToFloat64(fc.timeoutTotal.WithLabelValues(addr))
	if timeouts != 1 {
		t.Errorf("federation_forward_timeout_total{peer=%q} = %v, want 1", addr, timeouts)
	}
}
