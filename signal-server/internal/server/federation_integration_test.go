package server_test

import (
	"context"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	federationpb "github.com/usareur-af/goatcore/signal-server/gen/federation"
	signalpb "github.com/usareur-af/goatcore/signal-server/gen/signalexchange"
	"github.com/usareur-af/goatcore/signal-server/internal/server"
)

// startTestServer launches a signal-server on an ephemeral 127.0.0.1 TCP
// port, registered with an isolated Prometheus registry. The returned
// addr is dialable over loopback; t.Cleanup tears the grpc.Server and the
// server.Server down.
func startTestServer(t *testing.T, node string, federationPeers []string) string {
	t.Helper()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	reg := prometheus.NewRegistry()
	srv, err := server.New(node, logger, reg, federationPeers, nil)
	if err != nil {
		t.Fatalf("server.New %s: %v", node, err)
	}

	grpcSrv := grpc.NewServer(srv.ServerOptions()...)
	signalpb.RegisterSignalExchangeServer(grpcSrv, srv)
	federationpb.RegisterSignalFederationServer(grpcSrv, srv)

	// Test-startup listen (Block 50A.3 noctx): lifecycle bound to t.Cleanup.
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

// TestFederationEndToEnd is the one test that ties the whole cross-site
// story together over real gRPC:
//
//  1. Server B runs standalone (no federation peers).
//  2. A client registers the WireGuard public key "alpha" on B via
//     SignalExchange.ConnectStream (real header handshake).
//  3. Server A is federated to B.
//  4. A second client calls Send on A with remoteKey="alpha". A misses
//     locally, broadcasts Forward to B, B delivers via alpha's stream.
//  5. The first client's Recv() receives the original body intact.
//
// If this passes, the Forward handler, broadcast client, registry
// delivery, and ConnectStream outbound pump all agree end-to-end.
func TestFederationEndToEnd(t *testing.T) {
	addrB := startTestServer(t, "B", nil)
	addrA := startTestServer(t, "A", []string{addrB})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// --- register "alpha" on B --------------------------------------
	connB, err := grpc.NewClient(addrB, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial B: %v", err)
	}
	t.Cleanup(func() { _ = connB.Close() })

	streamCtx := metadata.AppendToOutgoingContext(ctx, "x-wiretrustee-peer-id", "alpha")
	streamB, err := signalpb.NewSignalExchangeClient(connB).ConnectStream(streamCtx)
	if err != nil {
		t.Fatalf("ConnectStream(B): %v", err)
	}

	// Header arrives after Server B has already placed alpha in its
	// registry — see exchange.go ConnectStream; register runs before
	// SendHeader. So once we see the header, Forward targeting alpha
	// will hit.
	hdr, err := streamB.Header()
	if err != nil {
		t.Fatalf("streamB.Header: %v", err)
	}
	if got := hdr.Get("x-wiretrustee-peer-registered"); len(got) == 0 || got[0] != "1" {
		t.Fatalf("registration header = %v, want [1]", got)
	}

	// --- Send via A, targeting alpha ---------------------------------
	connA, err := grpc.NewClient(addrA, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial A: %v", err)
	}
	t.Cleanup(func() { _ = connA.Close() })

	payload := []byte("hello across the mesh")
	if _, err := signalpb.NewSignalExchangeClient(connA).Send(ctx, &signalpb.EncryptedMessage{
		Key:       "sender-beta",
		RemoteKey: "alpha",
		Body:      payload,
	}); err != nil {
		t.Fatalf("Send via A (expected forward→B→delivery): %v", err)
	}

	// --- B's stream should receive the forwarded message ------------
	recvDone := make(chan *signalpb.EncryptedMessage, 1)
	recvErr := make(chan error, 1)
	go func() {
		msg, err := streamB.Recv()
		if err != nil {
			recvErr <- err
			return
		}
		recvDone <- msg
	}()

	select {
	case msg := <-recvDone:
		if got, want := string(msg.GetBody()), string(payload); got != want {
			t.Errorf("body = %q, want %q", got, want)
		}
		if got, want := msg.GetRemoteKey(), "alpha"; got != want {
			t.Errorf("remoteKey = %q, want %q", got, want)
		}
	case err := <-recvErr:
		t.Fatalf("streamB.Recv: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for forwarded message on B's stream")
	}
}

// TestStandaloneSendDeliversLocally confirms that when a client sends a
// message via the same server that holds its target peer's registration,
// delivery is direct (no federation required). This is the trivial
// happy path, kept here to flag any regression where the local-first
// check accidentally falls through to the federation path.
func TestStandaloneSendDeliversLocally(t *testing.T) {
	addr := startTestServer(t, "solo", nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	// Register "target" on this server.
	streamCtx := metadata.AppendToOutgoingContext(ctx, "x-wiretrustee-peer-id", "target")
	stream, err := signalpb.NewSignalExchangeClient(conn).ConnectStream(streamCtx)
	if err != nil {
		t.Fatalf("ConnectStream: %v", err)
	}
	if _, err := stream.Header(); err != nil {
		t.Fatalf("Header: %v", err)
	}

	// Send from a second, unregistered RPC session to the registered target.
	payload := []byte("direct local delivery")
	if _, err := signalpb.NewSignalExchangeClient(conn).Send(ctx, &signalpb.EncryptedMessage{
		Key:       "sender",
		RemoteKey: "target",
		Body:      payload,
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	recvDone := make(chan *signalpb.EncryptedMessage, 1)
	recvErr := make(chan error, 1)
	go func() {
		msg, err := stream.Recv()
		if err != nil {
			recvErr <- err
			return
		}
		recvDone <- msg
	}()

	select {
	case msg := <-recvDone:
		if got, want := string(msg.GetBody()), string(payload); got != want {
			t.Errorf("body = %q, want %q", got, want)
		}
	case err := <-recvErr:
		t.Fatalf("stream.Recv: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for locally-delivered message")
	}
}
