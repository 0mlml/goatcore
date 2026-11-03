package server_test

import (
	"context"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	federationpb "github.com/usareur-af/goatcore/signal-server/gen/federation"
	signalpb "github.com/usareur-af/goatcore/signal-server/gen/signalexchange"
)

// slowFederationServer is a minimal SignalFederation gRPC handler that
// intentionally sleeps for sleep before returning. It does not honor
// the server-side context cancellation — by ignoring ctx in the handler
// we simulate a slow / saturated peer that doesn't tear itself down
// when the client cancels. The client-side per-peer ctx in
// federation.go is what must enforce the deadline.
type slowFederationServer struct {
	federationpb.UnimplementedSignalFederationServer
	sleep time.Duration
}

func (s *slowFederationServer) Forward(_ context.Context, _ *signalpb.EncryptedMessage) (*federationpb.ForwardResult, error) {
	time.Sleep(s.sleep)
	return &federationpb.ForwardResult{Delivered: false}, nil
}

// startSlowFederationStub serves slowFederationServer on an ephemeral
// 127.0.0.1 port. Returns the dialable address; cleanup is bound to t.
func startSlowFederationStub(t *testing.T, sleep time.Duration) string {
	t.Helper()

	grpcSrv := grpc.NewServer()
	federationpb.RegisterSignalFederationServer(grpcSrv, &slowFederationServer{sleep: sleep})

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
	})
	return lis.Addr().String()
}

// TestBroadcastHonorsForwardTimeout — Block 52A.4 regression guard.
//
// Per docs/design/ha-multi-site-validation.md §5.5, the Forward RPC
// must have a specified-in-code per-peer deadline so a slow site can't
// pin the Send() path. The previous 5 s value was too lenient; the
// design spec calls for 200 ms per peer + 500 ms total broadcast
// budget. This test asserts a slow peer (sleep = 2 s) does not block
// Send() past ~1 s, comfortably under the original 5 s and above the
// 200 ms target with CI-stability headroom.
//
// If you find yourself raising this bound to make CI green, the right
// fix is almost always investigating *why* Send() is slow rather than
// loosening the bound. The whole point of this test is to lock in
// progress against the 5 s default.
func TestBroadcastHonorsForwardTimeout(t *testing.T) {
	t.Parallel()

	slowAddr := startSlowFederationStub(t, 2*time.Second)
	addrA := startTestServer(t, "A", []string{slowAddr})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := grpc.NewClient(addrA, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial A: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	start := time.Now()
	_, err = signalpb.NewSignalExchangeClient(conn).Send(ctx, &signalpb.EncryptedMessage{
		Key:       "sender",
		RemoteKey: "nobody-anywhere",
		Body:      []byte("p"),
	})
	elapsed := time.Since(start)

	// Server should return OK with empty payload — no peer delivered
	// (slow stub returns Delivered=false eventually, but the per-peer
	// ctx fires first so we never see its response). Per upstream
	// silent-drop contract / F-105, miss returns nil err.
	if err != nil {
		t.Fatalf("Send returned err=%v (code=%v); want nil per upstream silent-drop contract", err, status.Code(err))
	}

	// Assertion: elapsed must be well below the original 5 s default
	// AND below the total broadcast budget (500 ms) plus a healthy
	// CI-stability margin. 1 s gives ~5x headroom over the 200 ms
	// per-peer timeout.
	const maxAcceptable = 1 * time.Second
	if elapsed > maxAcceptable {
		t.Errorf("Send took %v with a slow peer; want < %v (federationForwardTimeout=200ms, federationBroadcastBudget=500ms)", elapsed, maxAcceptable)
	}
}

// TestBroadcastHonorsTotalBudget — Block 52A.4 regression guard for
// the federationBroadcastBudget constant. Two slow peers, each well
// over the per-peer timeout. The total wait must still be bounded by
// the broadcast budget rather than scaling with peer count.
//
// In practice with concurrent goroutines + per-peer 200 ms timeout,
// two peers also returns at ~200 ms — the goroutines run in parallel.
// This test stays meaningful as a regression guard: if someone
// accidentally serializes the broadcast or removes the budget cap, a
// wider matrix of slow peers would surface it. Keep N small here for
// CI speed; soak-driver covers the wider matrix.
func TestBroadcastHonorsTotalBudget(t *testing.T) {
	t.Parallel()

	slowA := startSlowFederationStub(t, 2*time.Second)
	slowB := startSlowFederationStub(t, 2*time.Second)
	addrA := startTestServer(t, "A", []string{slowA, slowB})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := grpc.NewClient(addrA, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial A: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	start := time.Now()
	_, err = signalpb.NewSignalExchangeClient(conn).Send(ctx, &signalpb.EncryptedMessage{
		Key:       "sender",
		RemoteKey: "nobody-anywhere",
		Body:      []byte("p"),
	})
	elapsed := time.Since(start)

	// Per upstream silent-drop contract / F-105, miss returns OK + nil err.
	if err != nil {
		t.Fatalf("Send returned err=%v (code=%v); want nil per upstream silent-drop contract", err, status.Code(err))
	}

	// 1 s cap with two slow peers — the broadcast budget is 500 ms,
	// per-peer 200 ms; concurrent goroutines mean the larger of the
	// two governs. 1 s gives generous CI margin.
	const maxAcceptable = 1 * time.Second
	if elapsed > maxAcceptable {
		t.Errorf("Send with 2 slow peers took %v; want < %v", elapsed, maxAcceptable)
	}
}
