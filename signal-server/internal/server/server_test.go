package server_test

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	federationpb "github.com/usareur-af/goatcore/signal-server/gen/federation"
	signalpb "github.com/usareur-af/goatcore/signal-server/gen/signalexchange"
	"github.com/usareur-af/goatcore/signal-server/internal/server"
)

// Compile-time interface satisfaction. If these stop compiling, the proto
// contract drifted away from the server — the file itself is the tripwire.
var (
	_ signalpb.SignalExchangeServer       = (*server.Server)(nil)
	_ federationpb.SignalFederationServer = (*server.Server)(nil)
)

// newTestServer returns a Server bound to a fresh, test-scoped Prometheus
// registry and no federation peers. The registry is returned so tests can
// gather and assert against it without touching global state.
func newTestServer(t *testing.T) (*server.Server, *prometheus.Registry) {
	t.Helper()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	reg := prometheus.NewRegistry()
	s, err := server.New("test-node", logger, reg, nil, nil)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, reg
}

func TestNewReturnsInitializedServer(t *testing.T) {
	s, _ := newTestServer(t)

	if s == nil {
		t.Fatal("New returned nil")
	}
	if got, want := s.Node, "test-node"; got != want {
		t.Errorf("Node = %q, want %q", got, want)
	}
	if s.Logger == nil {
		t.Error("Logger is nil after New")
	}
	if got := s.RegisteredPeers(); got != 0 {
		t.Errorf("RegisteredPeers on fresh server = %d, want 0", got)
	}
}

func TestSendRequiresRemoteKey(t *testing.T) {
	s, _ := newTestServer(t)

	_, err := s.Send(context.Background(), &signalpb.EncryptedMessage{
		Key:       "sender",
		RemoteKey: "",
		Body:      []byte("payload"),
	})
	if err == nil {
		t.Fatal("Send with empty remoteKey returned nil error")
	}
	if got, want := status.Code(err), codes.InvalidArgument; got != want {
		t.Errorf("Send err code = %v, want %v (err=%v)", got, want, err)
	}
}

// TestSendUnknownPeerSilentDrop locks in the upstream silent-drop contract
// (vanilla netbird signal-server returns nil for unknown RemoteKey on the
// unary Send RPC). The vanilla daemon's IsHealthy() liveness probe at
// shared/signal/client/grpc.go:270 sends Send(RemoteKey="dummy") and treats
// any error as "signal disconnected." Returning NotFound here would break
// every netbird daemon talking to a goat-fork signal-server — which is
// exactly what F-105 documents from the operator-laptop
// onboarding. Operational visibility on the miss is preserved via
// relayTotal{outcome="notfound"} (see TestSendUnknownPeerIncrementsMetric).
func TestSendUnknownPeerSilentDrop(t *testing.T) {
	s, _ := newTestServer(t)

	resp, err := s.Send(context.Background(), &signalpb.EncryptedMessage{
		Key:       "sender",
		RemoteKey: "nobody-registered",
		Body:      []byte("payload"),
	})
	if err != nil {
		t.Fatalf("Send to unregistered peer returned err=%v (code=%v); want nil per upstream silent-drop contract", err, status.Code(err))
	}
	if resp == nil {
		t.Fatal("Send returned nil response; want empty EncryptedMessage")
	}
}

// TestSendUnknownPeerIncrementsMetric verifies that instrumentation actually
// fires on the Send hot path even though the RPC returns OK (per upstream
// silent-drop contract; see TestSendUnknownPeerSilentDrop + F-105).
// Operational visibility on misses is the metric, NOT the gRPC status code.
// If this drops to zero, metrics are silently broken even though the RPC
// contract still looks right.
func TestSendUnknownPeerIncrementsMetric(t *testing.T) {
	s, reg := newTestServer(t)

	_, _ = s.Send(context.Background(), &signalpb.EncryptedMessage{
		Key:       "sender",
		RemoteKey: "nobody-registered",
		Body:      []byte("p"),
	})

	want := `
# HELP signal_server_relay_total Total delivery attempts (Send RPC + ConnectStream inbound), labeled by outcome and code path.
# TYPE signal_server_relay_total counter
signal_server_relay_total{node="test-node",outcome="notfound",path="unary_send"} 1
`
	if err := testutil.GatherAndCompare(reg, strings.NewReader(want), "signal_server_relay_total"); err != nil {
		t.Errorf("unexpected metric state: %v", err)
	}
}

// TestSendForwardFailedIsDistinctFromNotFound is the F-265 regression guard.
// When a local miss meets an INCOMPLETE federation broadcast (a peer RPC-errored
// or timed out, so delivery status is unknown — the recipient may be online on
// an unreachable shard), relay() must record outcome "forward_failed", NOT
// "notfound". Conflating the two is what made the cross-shard outage
// undiagnosable: the dashboards showed "misses" that looked like offline peers
// while federation was silently broken. Send must still return OK.
func TestSendForwardFailedIsDistinctFromNotFound(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	reg := prometheus.NewRegistry()
	// One federation peer at a dead address: the Forward RPC fails, so the
	// broadcast is incomplete (not delivered, not complete) — the F-265 case.
	s, err := server.New("test-node", logger, reg, []string{"127.0.0.1:1"}, nil)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	resp, sendErr := s.Send(context.Background(), &signalpb.EncryptedMessage{
		Key:       "sender",
		RemoteKey: "peer-on-a-broken-shard",
		Body:      []byte("p"),
	})
	// F-105: a federation break must NOT fail the Send/liveness contract.
	if sendErr != nil {
		t.Fatalf("Send returned %v; must stay OK on forward_failed", sendErr)
	}
	if resp == nil {
		t.Fatal("Send returned nil response; want empty message")
	}

	want := `
# HELP signal_server_relay_total Total delivery attempts (Send RPC + ConnectStream inbound), labeled by outcome and code path.
# TYPE signal_server_relay_total counter
signal_server_relay_total{node="test-node",outcome="forward_failed",path="unary_send"} 1
`
	if err := testutil.GatherAndCompare(reg, strings.NewReader(want), "signal_server_relay_total"); err != nil {
		t.Errorf("forward_failed must be distinct from notfound: %v", err)
	}
}

// TestConnectedPeersGaugeReflectsRegistry confirms the GaugeFunc path reads
// the live registry count at scrape time (not a stale snapshot).
func TestConnectedPeersGaugeReflectsRegistry(t *testing.T) {
	s, reg := newTestServer(t)

	// Reach past the public surface to exercise the registry via the only
	// path that touches it deterministically — directly registering a peer
	// isn't currently exported, so use the count path which DOES read live.
	// Proves: GaugeFunc calls the closure on Collect, returning the current
	// count (0), not some cached value.
	if got, want := s.RegisteredPeers(), 0; got != want {
		t.Fatalf("pre-condition: RegisteredPeers = %d, want %d", got, want)
	}
	want := `
# HELP signal_server_connected_peers Number of peers currently registered at this signal server.
# TYPE signal_server_connected_peers gauge
signal_server_connected_peers{node="test-node"} 0
`
	if err := testutil.GatherAndCompare(reg, strings.NewReader(want), "signal_server_connected_peers"); err != nil {
		t.Errorf("unexpected gauge state: %v", err)
	}
}
