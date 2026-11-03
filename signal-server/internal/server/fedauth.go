// fedauth.go — bearer-token authentication on the federation surface
// (mesh-control-path Phase 3, ADR 1085 Amendment 1).
//
// Federation was plaintext + unauthenticated by design while its only
// transport was the WireGuard mesh ("traffic is already on the mesh").
// Phase 3 adds a public fallback path (relay-tunneled, Traefik-TLS-
// terminated) so the signal-a⇄signal-b forwarding channel survives a
// Thor⇄Thor mesh flap — the F-256 deadlock class. That path exits the
// mesh trust boundary, so the federation surface now authenticates
// callers on BOTH paths: the token is checked mesh-side too (defense in
// depth — a config error exposing :10000 no longer exposes an open
// Forward endpoint), while TLS on the public path is Traefik's job
// (the Thor-local dual-path proxy originates TLS toward the relays).
//
// Token model: one shared secret per goatnet (SIGNAL_FEDERATION_TOKEN),
// same value on every federating signal server — peers are mutually
// trusted forwarders, not per-identity principals. Empty token = auth
// off (standalone servers and pre-Phase-3 goatnets keep today's
// behavior; main.go warns when peers are configured without a token).
//
// Rotation without a flag day: the env value may be a comma-separated
// accept-list — the server accepts ANY listed token, the client sends
// the FIRST. Rotate by shipping "new,old" everywhere (all servers accept
// both, clients start sending new), then dropping ",old" — federation
// stays up through every intermediate state.

package server

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// federationMethodPrefix matches every RPC on the SignalFederation
// service (proto package signalfederation). The SignalExchange service is
// peer-facing and stays unauthenticated — netbird clients authenticate at
// the WireGuard/mgmt layer, not here.
const federationMethodPrefix = "/signalfederation.SignalFederation/"

// federationAuth guards the federation service methods with a shared
// bearer token. Construct with newFederationAuth; nil receiver = auth
// disabled (interceptors pass through).
type federationAuth struct {
	// tokenHashes are sha256 of each accepted token. Comparing digests
	// keeps the comparison constant-time regardless of attacker-supplied
	// token length; more than one entry only during rotation.
	tokenHashes [][sha256.Size]byte
	rejected    prometheus.Counter
}

// firstToken returns the first entry of a comma-separated token list —
// the token the CLIENT presents (servers accept the whole list).
func firstToken(tokens string) string {
	first, _, _ := strings.Cut(tokens, ",")
	return strings.TrimSpace(first)
}

// newFederationAuth returns the auth guard, or nil when tokens is empty
// (auth disabled — the pre-Phase-3 mesh-only posture). tokens may be a
// comma-separated accept-list (rotation; see file header).
func newFederationAuth(reg prometheus.Registerer, node, tokens string) *federationAuth {
	var hashes [][sha256.Size]byte
	for _, t := range strings.Split(tokens, ",") {
		if t = strings.TrimSpace(t); t != "" {
			hashes = append(hashes, sha256.Sum256([]byte(t)))
		}
	}
	if len(hashes) == 0 {
		return nil
	}
	rejected := prometheus.NewCounter(prometheus.CounterOpts{
		Name:        "signal_server_federation_auth_rejected_total",
		Help:        "Total federation RPCs rejected for a missing or invalid bearer token.",
		ConstLabels: prometheus.Labels{"node": node},
	})
	reg.MustRegister(rejected)
	return &federationAuth{
		tokenHashes: hashes,
		rejected:    rejected,
	}
}

// authorized checks the incoming metadata for `authorization: Bearer
// <token>` against the accept-list. Standard gRPC metadata keys are
// lowercased by the transport.
func (fa *federationAuth) authorized(ctx context.Context) bool {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return false
	}
	for _, v := range md.Get("authorization") {
		presented, isBearer := strings.CutPrefix(v, "Bearer ")
		if !isBearer {
			continue
		}
		sum := sha256.Sum256([]byte(presented))
		for i := range fa.tokenHashes {
			if subtle.ConstantTimeCompare(sum[:], fa.tokenHashes[i][:]) == 1 {
				return true
			}
		}
	}
	return false
}

// check is the shared guard body for both interceptor shapes: nil error
// = let the call through (non-federation method, auth disabled, or a
// valid token).
func (fa *federationAuth) check(ctx context.Context, fullMethod string) error {
	if fa == nil || !strings.HasPrefix(fullMethod, federationMethodPrefix) {
		return nil
	}
	if !fa.authorized(ctx) {
		fa.rejected.Inc()
		return status.Error(codes.Unauthenticated, "federation: missing or invalid bearer token")
	}
	return nil
}

// unaryInterceptor rejects unauthenticated calls to federation methods.
// Non-federation methods pass through untouched. nil receiver = disabled.
func (fa *federationAuth) unaryInterceptor(
	ctx context.Context,
	req interface{},
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (interface{}, error) {
	if err := fa.check(ctx, info.FullMethod); err != nil {
		return nil, err
	}
	return handler(ctx, req)
}

// streamInterceptor is the stream twin. The federation service is
// unary-only today; this exists so a future streaming RPC under the
// service prefix is guarded by construction rather than by remembering.
func (fa *federationAuth) streamInterceptor(
	srv interface{},
	ss grpc.ServerStream,
	info *grpc.StreamServerInfo,
	handler grpc.StreamHandler,
) error {
	if err := fa.check(ss.Context(), info.FullMethod); err != nil {
		return err
	}
	return handler(srv, ss)
}

// federationTokenUnaryClientInterceptor attaches the shared bearer token
// to every outbound federation RPC. Installed on the peer dial in
// newFederationClient when a token is configured.
func federationTokenUnaryClientInterceptor(token string) grpc.UnaryClientInterceptor {
	header := "Bearer " + token
	return func(
		ctx context.Context,
		method string,
		req, reply interface{},
		cc *grpc.ClientConn,
		invoker grpc.UnaryInvoker,
		opts ...grpc.CallOption,
	) error {
		ctx = metadata.AppendToOutgoingContext(ctx, "authorization", header)
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}
