package server_test

import (
	"context"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	signalpb "github.com/usareur-af/goatcore/signal-server/gen/signalexchange"
	"github.com/usareur-af/goatcore/signal-server/internal/server"
)

// TestSendRateLimitReturnsRetryInfo drives the storm-shedding token bucket
// through the real Send handler and asserts a breach returns
// codes.ResourceExhausted carrying an errdetails.RetryInfo hint (the idiomatic
// gRPC retry contract the L1 client reads), plus the rate-limited counter.
func TestSendRateLimitReturnsRetryInfo(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	reg := prometheus.NewRegistry()
	// rate 1/min, burst 1: the first offer drains the bucket, the second
	// breaches within the same instant (1/min refill is negligible here).
	s, err := server.New("n", logger, reg, nil, &server.Config{
		SenderRateLimitPerMin: 1,
		SenderRateLimitBurst:  1,
	})
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	ctx := context.Background()

	// First offer admits — target is offline so it returns the OK/empty
	// silent-drop, no error.
	if _, err := s.Send(ctx, &signalpb.EncryptedMessage{Key: "flood", RemoteKey: "x", Body: []byte("1")}); err != nil {
		t.Fatalf("first Send should admit, got %v", err)
	}

	// Second offer from the same sender breaches.
	_, err = s.Send(ctx, &signalpb.EncryptedMessage{Key: "flood", RemoteKey: "x", Body: []byte("2")})
	if err == nil {
		t.Fatal("second Send from a drained sender should be rate-limited")
	}
	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.ResourceExhausted {
		t.Fatalf("code = %v, want ResourceExhausted (err=%v)", status.Code(err), err)
	}
	var ri *errdetails.RetryInfo
	for _, d := range st.Details() {
		if r, ok := d.(*errdetails.RetryInfo); ok {
			ri = r
		}
	}
	if ri == nil {
		t.Fatal("rate-limit error carries no RetryInfo detail")
	}
	if got := ri.GetRetryDelay().AsDuration(); got != 5*time.Second {
		t.Fatalf("RetryInfo delay = %v, want 5s (first breach)", got)
	}

	want := `
# HELP signal_server_send_rate_limited_total Total unary Send RPCs rejected by the per-sender token bucket (ADR 1081 L2).
# TYPE signal_server_send_rate_limited_total counter
signal_server_send_rate_limited_total{node="n"} 1
`
	if err := testutil.GatherAndCompare(reg, strings.NewReader(want), "signal_server_send_rate_limited_total"); err != nil {
		t.Errorf("unexpected rate-limit metric: %v", err)
	}
}

// startShedServer launches a standalone (no-federation) signal-server with the
// given storm-shedding config on an ephemeral loopback port.
func startShedServer(t *testing.T, cfg *server.Config) string {
	t.Helper()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	srv, err := server.New("shed", logger, prometheus.NewRegistry(), nil, cfg)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	grpcSrv := grpc.NewServer(srv.ServerOptions()...)
	signalpb.RegisterSignalExchangeServer(grpcSrv, srv)

	lis, err := net.Listen("tcp", "127.0.0.1:0") //nolint:noctx // test-only
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	serveDone := make(chan struct{})
	go func() {
		defer close(serveDone)
		_ = grpcSrv.Serve(lis)
	}()
	t.Cleanup(func() {
		grpcSrv.GracefulStop()
		<-serveDone
		_ = srv.Close()
	})
	return lis.Addr().String()
}

// TestNegativeCachePurgeOnRegisterMakesPeerReachable is the mass-reconnect
// guarantee over real gRPC: a recipient cached offline becomes immediately
// deliverable the moment it registers a stream — the purge, not TTL expiry,
// is what clears it. The TTL here (60s) far outlives the test, so a missing
// purge-on-register would leave the second Send short-circuited by the
// negative cache and the recipient would never receive it.
func TestNegativeCachePurgeOnRegisterMakesPeerReachable(t *testing.T) {
	addr := startShedServer(t, &server.Config{NegativeCacheTTL: 60 * time.Second})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	client := signalpb.NewSignalExchangeClient(conn)

	// 1. "recip" is offline: this Send misses and records the offline mark.
	if _, err := client.Send(ctx, &signalpb.EncryptedMessage{Key: "sender", RemoteKey: "recip", Body: []byte("miss")}); err != nil {
		t.Fatalf("Send while recip offline: %v", err)
	}

	// 2. "recip" registers a stream — register() purges the offline mark.
	streamCtx := metadata.AppendToOutgoingContext(ctx, "x-wiretrustee-peer-id", "recip")
	stream, err := client.ConnectStream(streamCtx)
	if err != nil {
		t.Fatalf("ConnectStream(recip): %v", err)
	}
	hdr, err := stream.Header()
	if err != nil {
		t.Fatalf("stream.Header: %v", err)
	}
	if got := hdr.Get("x-wiretrustee-peer-registered"); len(got) == 0 || got[0] != "1" {
		t.Fatalf("registration header = %v, want [1]", got)
	}

	// 3. Send again: must be DELIVERED to recip's stream, not shed as still
	//    offline. This is the assertion that proves purge-on-register.
	payload := []byte("hello after reconnect")
	recvd := make(chan *signalpb.EncryptedMessage, 1)
	recvErr := make(chan error, 1)
	go func() {
		msg, err := stream.Recv()
		if err != nil {
			recvErr <- err
			return
		}
		recvd <- msg
	}()

	if _, err := client.Send(ctx, &signalpb.EncryptedMessage{Key: "sender", RemoteKey: "recip", Body: payload}); err != nil {
		t.Fatalf("Send after reconnect: %v", err)
	}

	select {
	case msg := <-recvd:
		if string(msg.GetBody()) != string(payload) {
			t.Fatalf("delivered body = %q, want %q", msg.GetBody(), payload)
		}
	case err := <-recvErr:
		t.Fatalf("recip Recv: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("recip did not receive after reconnect: purge-on-register missing, message shed by the still-live offline mark")
	}
}
