// fedauth_test.go — mesh-control-path Phase 3 federation transport security.
//
// Covers:
//   - end-to-end bearer-token enforcement on the federation surface via a
//     real gRPC server wired with ServerOptions() (interceptor chain as
//     deployed), including the pass-through for SignalExchange methods
//   - auth-off back-compat (empty token = pre-Phase-3 behavior)
//   - transportHealthy — the ADR 1085 Amendment 1 §3 negcache gate
//   - the relay path NOT negative-caching a miss while a federation
//     breaker is open ("absent across federation" is unknowable then)

package server

import (
	"context"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	federationpb "github.com/usareur-af/goatcore/signal-server/gen/federation"
	signalpb "github.com/usareur-af/goatcore/signal-server/gen/signalexchange"
)

// startAuthedServer boots a Server (token per cfg) on a bufconn with the
// full production interceptor chain and returns a dial function.
func startAuthedServer(t *testing.T, token string) func(opts ...grpc.DialOption) *grpc.ClientConn {
	t.Helper()

	srv, err := New("fedauth-test", slog.New(slog.NewTextHandler(io.Discard, nil)),
		prometheus.NewRegistry(), nil, &Config{FederationToken: token})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })

	lis := bufconn.Listen(1 << 16)
	grpcSrv := grpc.NewServer(srv.ServerOptions()...)
	signalpb.RegisterSignalExchangeServer(grpcSrv, srv)
	federationpb.RegisterSignalFederationServer(grpcSrv, srv)
	go func() { _ = grpcSrv.Serve(lis) }()
	t.Cleanup(grpcSrv.Stop)

	return func(opts ...grpc.DialOption) *grpc.ClientConn {
		t.Helper()
		opts = append(opts,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
				return lis.DialContext(ctx)
			}),
		)
		conn, err := grpc.NewClient("passthrough:///bufconn", opts...)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		t.Cleanup(func() { _ = conn.Close() })
		return conn
	}
}

func forwardOnce(t *testing.T, conn *grpc.ClientConn) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := federationpb.NewSignalFederationClient(conn).Forward(ctx, &signalpb.EncryptedMessage{
		Key:       "sender",
		RemoteKey: "target",
		Body:      []byte("p"),
	})
	return err
}

// TestFederationAuthEnforced: with a token configured, Forward without a
// token or with a wrong token gets Unauthenticated; the correct token (via
// the same client interceptor production uses) succeeds; and the
// SignalExchange surface stays untouched by the guard.
func TestFederationAuthEnforced(t *testing.T) {
	dial := startAuthedServer(t, "correct-horse")

	if err := forwardOnce(t, dial()); status.Code(err) != codes.Unauthenticated {
		t.Errorf("tokenless Forward: got err %v, want Unauthenticated", err)
	}

	wrong := dial(grpc.WithUnaryInterceptor(federationTokenUnaryClientInterceptor("battery-staple")))
	if err := forwardOnce(t, wrong); status.Code(err) != codes.Unauthenticated {
		t.Errorf("wrong-token Forward: got err %v, want Unauthenticated", err)
	}

	right := dial(grpc.WithUnaryInterceptor(federationTokenUnaryClientInterceptor("correct-horse")))
	if err := forwardOnce(t, right); err != nil {
		t.Errorf("correct-token Forward: got err %v, want nil", err)
	}

	// SignalExchange method on a tokenless conn — the guard must not
	// touch peer-facing RPCs (clients have no federation token).
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := signalpb.NewSignalExchangeClient(dial()).Send(ctx, &signalpb.EncryptedMessage{
		Key:       "sender",
		RemoteKey: "nobody",
		Body:      []byte("p"),
	})
	if err != nil {
		t.Errorf("tokenless Send: got err %v, want nil (silent-drop contract, no auth gate)", err)
	}
}

// TestFederationAuthRejectsBeforeConcurrencySlot locks the DDoS-relevant
// interceptor ordering: an unauthenticated federation call must be
// rejected with Unauthenticated BEFORE it takes a concurrency-limiter
// slot. Regression guard — if a future change reorders the limiter ahead
// of fedAuth, a public unauthenticated flood could occupy MaxInflight
// admission slots (shared with the peer-facing SignalExchange surface)
// and starve legitimate mesh clients. We compose the two interceptors in
// the exact ServerOptions order, saturate the limiter to zero free slots,
// then fire an unauthenticated federation call and assert it comes back
// Unauthenticated (auth ran first) rather than ResourceExhausted (limiter
// ran first).
func TestFederationAuthRejectsBeforeConcurrencySlot(t *testing.T) {
	reg := prometheus.NewRegistry()
	fa := newFederationAuth(reg, "order-test", "the-token")
	cl := newConcurrencyLimiter(reg, "order-test", 1) // exactly one slot

	// chain mirrors ServerOptions unary order after panic-recovery:
	// fedAuth (outer) → limiter (inner) → handler.
	chained := func(ctx context.Context, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		return fa.unaryInterceptor(ctx, nil, info, func(ic context.Context, ir interface{}) (interface{}, error) {
			return cl.unaryInterceptor(ic, ir, info, handler)
		})
	}

	// Occupy the single limiter slot with a blocked, AUTHENTICATED call —
	// it must pass fedAuth to reach (and hold) the limiter slot.
	authedCtx := metadata.NewIncomingContext(context.Background(),
		metadata.Pairs("authorization", "Bearer the-token"))
	gate := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	blocking := func(_ context.Context, _ interface{}) (interface{}, error) {
		gate <- struct{}{}
		<-release
		return "ok", nil
	}
	go func() {
		_, _ = chained(authedCtx, &grpc.UnaryServerInfo{FullMethod: "/signalfederation.SignalFederation/Forward"}, blocking)
	}()
	<-gate // the authed call now holds the only slot

	// An UNAUTHENTICATED federation call (no bearer metadata) must be
	// rejected by auth, not queued/rejected by the saturated limiter.
	tripwire := func(_ context.Context, _ interface{}) (interface{}, error) {
		t.Error("handler reached: auth did not reject before the limiter")
		return nil, nil
	}
	_, err := chained(context.Background(), &grpc.UnaryServerInfo{FullMethod: "/signalfederation.SignalFederation/Forward"}, tripwire)
	if got := status.Code(err); got != codes.Unauthenticated {
		t.Errorf("saturated-limiter unauth federation call: got %v, want Unauthenticated (auth must run before the limiter)", got)
	}
}

// TestFederationAuthDisabled: empty token = pre-Phase-3 posture, Forward
// works unauthenticated (standalone and mesh-only goatnets unchanged).
func TestFederationAuthDisabled(t *testing.T) {
	dial := startAuthedServer(t, "")
	if err := forwardOnce(t, dial()); err != nil {
		t.Errorf("Forward with auth disabled: got err %v, want nil", err)
	}
}

// TestFederationAuthRotationAcceptList: with "new,old" configured the
// server accepts either token (the no-flag-day rotation window) while
// the client-side helper picks the first.
func TestFederationAuthRotationAcceptList(t *testing.T) {
	dial := startAuthedServer(t, "new-token, old-token")

	for _, tok := range []string{"new-token", "old-token"} {
		conn := dial(grpc.WithUnaryInterceptor(federationTokenUnaryClientInterceptor(tok)))
		if err := forwardOnce(t, conn); err != nil {
			t.Errorf("Forward with accept-listed token %q: got err %v, want nil", tok, err)
		}
	}

	stale := dial(grpc.WithUnaryInterceptor(federationTokenUnaryClientInterceptor("retired-token")))
	if err := forwardOnce(t, stale); status.Code(err) != codes.Unauthenticated {
		t.Errorf("Forward with unlisted token: got err %v, want Unauthenticated", err)
	}

	if got := firstToken("new-token, old-token"); got != "new-token" {
		t.Errorf("firstToken = %q, want new-token (the client-presented entry)", got)
	}
}

// TestBroadcastCompleteness covers the negcache gate signal: a nil
// client (standalone) is trivially complete; a broadcast where a peer
// gave no definitive answer (RPC error — breaker still closed) is
// incomplete from the FIRST failure, not only after the breaker trips.
func TestBroadcastCompleteness(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var nilFC *federationClient
	if _, complete := nilFC.Broadcast(ctx, &signalpb.EncryptedMessage{Key: "s", RemoteKey: "r"}); !complete {
		t.Error("nil federationClient: want complete (a local miss IS a full miss)")
	}

	reg := prometheus.NewRegistry()
	fc, err := newFederationClient([]string{"127.0.0.1:1"}, newMetrics(reg, "bc-test", newRegistry()),
		reg, "bc-test", federationClientConfig{})
	if err != nil {
		t.Fatalf("newFederationClient: %v", err)
	}
	t.Cleanup(func() { _ = fc.Close() })

	// Port 1 refuses: the very first broadcast (breaker closed, default
	// trip threshold 5) must already report incomplete — the pre-trip
	// window is exactly where the old breaker-state gate leaked.
	if _, complete := fc.Broadcast(ctx, &signalpb.EncryptedMessage{Key: "s", RemoteKey: "r"}); complete {
		t.Error("broadcast with unreachable peer (breaker still closed): want incomplete")
	}
}

// TestNegcacheSkipsRecordOnIncompleteBroadcast: a full miss normally
// records the recipient offline (ADR 1081 L2), but when any federation
// peer gave no definitive answer the conclusion is unknowable and must
// not be cached (ADR 1085 Amendment 1 §3 / F-256) — from the first
// failed Forward, before the breaker trips.
func TestNegcacheSkipsRecordOnIncompleteBroadcast(t *testing.T) {
	srv, err := New("negfed-test", slog.New(slog.NewTextHandler(io.Discard, nil)),
		prometheus.NewRegistry(), []string{"127.0.0.1:1"}, &Config{
			NegativeCacheTTL: time.Minute,
		})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// First miss: the peer never answers (connection refused), so the
	// broadcast is incomplete → no cache entry, breaker state irrelevant.
	if _, err := srv.Send(ctx, &signalpb.EncryptedMessage{Key: "s", RemoteKey: "ghost", Body: []byte("p")}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if srv.negCache.offline("ghost") {
		t.Error("recipient negative-cached on incomplete broadcast; want not cached")
	}

	// Control: standalone server (no federation) does record the miss.
	alone, err := New("negfed-alone", slog.New(slog.NewTextHandler(io.Discard, nil)),
		prometheus.NewRegistry(), nil, &Config{NegativeCacheTTL: time.Minute})
	if err != nil {
		t.Fatalf("New standalone: %v", err)
	}
	t.Cleanup(func() { _ = alone.Close() })
	if _, err := alone.Send(ctx, &signalpb.EncryptedMessage{Key: "s", RemoteKey: "ghost", Body: []byte("p")}); err != nil {
		t.Fatalf("standalone Send: %v", err)
	}
	if !alone.negCache.offline("ghost") {
		t.Error("standalone full miss not negative-cached; want cached (L2 behavior unchanged)")
	}
}
