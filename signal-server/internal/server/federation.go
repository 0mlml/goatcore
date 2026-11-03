package server

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sony/gobreaker/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/status"

	federationpb "github.com/usareur-af/goatcore/signal-server/gen/federation"
	signalpb "github.com/usareur-af/goatcore/signal-server/gen/signalexchange"
)

// Forward is the server-to-server RPC handler. When another signal server
// can't find a target peer in its local registry, it calls this Forward on
// every peer (including us). We attempt **local delivery only** — no further
// federation fan-out — which keeps the topology at one hop and makes
// A → B → A loops structurally impossible.
// PresenceUpdate (ADR 1108) records a federation peer's pushed presence snapshot
// so relay() can forward cross-shard messages directly to the owner instead of
// broadcasting. The ack carries THIS node's name so the caller can map our
// address to our node for its own outbound routing. Auth (and TLS) are enforced
// by the same inbound path as Forward — this is the same federation surface.
func (s *Server) PresenceUpdate(_ context.Context, d *federationpb.PresenceDelta) (*federationpb.PresenceAck, error) {
	s.presence.applyInbound(d)
	return &federationpb.PresenceAck{Node: s.Node}, nil
}

func (s *Server) Forward(_ context.Context, msg *signalpb.EncryptedMessage) (*federationpb.ForwardResult, error) {
	cid := correlationID(msg)

	if msg.GetRemoteKey() == "" {
		s.metrics.forwardReceivedTotal.WithLabelValues(outcomeInvalidArg).Inc()
		s.Logger.Info("forward received with empty remoteKey",
			"cid", cid,
			"node", s.Node)
		return &federationpb.ForwardResult{Delivered: false}, nil
	}

	err := s.registry.deliver(msg.GetRemoteKey(), msg)

	var outcome string
	switch {
	case err == nil:
		outcome = outcomeDelivered
	case errors.Is(err, errUnknownPeer):
		outcome = outcomeNotFound
	case errors.Is(err, errOutboundFull):
		outcome = outcomeOutboundFull
	default:
		outcome = outcomeOutboundFull
	}
	s.metrics.forwardReceivedTotal.WithLabelValues(outcome).Inc()
	s.Logger.Info("forward handled",
		"cid", cid,
		"to", msg.GetRemoteKey(),
		"outcome", outcome,
		"node", s.Node)

	return &federationpb.ForwardResult{Delivered: err == nil}, nil
}

// federationForwardTimeout is the DEFAULT per-peer Forward RPC deadline.
//
// Block 52A.4: tightened from 5 s → 200 ms per
// docs/design/ha-multi-site-validation.md §5.5. Inter-site RTT is
// 50–200 ms over WireGuard; 200 ms gives a one-RTT-plus-jitter
// budget. A slow peer that exceeds this is the symptom we want to
// surface as a SignalServerForwardRPCError, not absorb silently.
//
// Phase 3 made this operator-tunable (Config.FederationForwardTimeout /
// SIGNAL_FEDERATION_FORWARD_TIMEOUT_MS): the 52A.4 analysis priced the
// MESH path, but the dual-path proxy's public leg
// (Thor→relay→rathole→peer-Traefik) can exceed 200 ms steady-state —
// goatnets running the dual path set ~1000 ms so a mesh-flap failover
// doesn't feed the breaker a burst of spurious deadline failures.
//
// Paired with the broadcast budget below, which caps the total
// wall-clock time the Send() caller will block on Broadcast()
// regardless of per-peer behavior.
const federationForwardTimeout = 200 * time.Millisecond

// federationBroadcastBudget is the DEFAULT total Broadcast() wall-clock
// cap. Even though each peer call has its own per-peer deadline,
// concurrent goroutines can collectively keep the WaitGroup pinned
// past the per-peer cap (e.g. a goroutine scheduling delay on a
// loaded host). The effective budget scales with a raised forward
// timeout (max(500ms, 2×timeout)) so the per-peer deadline is never
// starved by the outer ceiling.
//
// Per docs/design/ha-multi-site-validation.md §5.5 (Wave 3.4
// prerequisite). Tested by federation_test.go
// TestBroadcastHonorsTotalBudget.
const federationBroadcastBudget = 500 * time.Millisecond

// Block 50B.6 — circuit-breaker default tunings. Orthogonal to the
// per-peer deadline (federationForwardTimeout): the deadline catches
// slow Forward calls; the breaker catches consistently-failing peers
// and stops dialing them entirely until the open window expires.
const (
	defaultCircuitConsecutiveFailures = uint32(5)
	defaultCircuitOpenDuration        = 30 * time.Second
)

// federationPeer is one outbound connection to another signal server,
// guarded by a circuit breaker. The breaker isolates a degraded peer from
// the broadcast hot path: once `CircuitConsecutiveFailures` Forwards to
// this peer fail (timeout, RPC error, or DNS), Broadcast skips it
// entirely for `CircuitOpenDuration`.
type federationPeer struct {
	addr    string
	conn    *grpc.ClientConn
	rpc     federationpb.SignalFederationClient
	breaker *gobreaker.CircuitBreaker[*federationpb.ForwardResult]
}

// federationClientConfig is the subset of Config that the federation
// client needs. Zero fields are replaced with package defaults. The
// per-call deadline is NOT configurable here — it lives as
// federationForwardTimeout per the 52A.4 RTT analysis; introduce a
// flag only if a future deployment topology demonstrates the constant
// is wrong.
type federationClientConfig struct {
	CircuitConsecutiveFailures uint32
	CircuitOpenDuration        time.Duration

	// FederationToken, when non-empty, is attached to every outbound
	// Forward as `authorization: Bearer <token>` (fedauth.go, Phase 3).
	// May be a comma-separated accept-list on the server side; the
	// FIRST entry is what the client sends.
	FederationToken string

	// FederationAuthority, when non-empty, overrides the HTTP/2
	// :authority on the peer dial (Phase 3). Needed on the dual-path
	// proxy topology: the dial target is the local proxy alias, but the
	// public leg terminates at the peer's Traefik, whose federation
	// router matches Host() against the peer's federation hostname.
	// Ignored by the direct gRPC server on the mesh leg.
	FederationAuthority string

	// ForwardTimeout overrides the per-peer Forward deadline. Zero =
	// package default (federationForwardTimeout).
	ForwardTimeout time.Duration
}

// federationClient is the aggregate of every configured outbound peer.
// nil is a valid value and means "standalone server, no federation."
type federationClient struct {
	peers   []*federationPeer
	metrics *metrics

	// Resolved deadlines (defaults applied at construction).
	forwardTimeout  time.Duration
	broadcastBudget time.Duration

	// Per-peer circuit-breaker telemetry. Owned here because the breaker
	// state-change callback fires on a goroutine controlled by the
	// breaker library, and we want one place to update gauges.
	timeoutTotal   *prometheus.CounterVec   // peer
	circuitState   *prometheus.GaugeVec     // peer
	skippedTotal   *prometheus.CounterVec   // peer
	forwardLatency *prometheus.HistogramVec // peer
}

// newFederationClient dials every configured peer once at startup and
// equips each with its own circuit breaker.
//
// Connections use insecure gRPC credentials: the peer address is either a
// mesh IP (WireGuard-encrypted end to end) or the Thor-local dual-path
// proxy (Phase 3), which originates TLS itself toward the relay-tunneled
// public path. Application-layer auth is the bearer token attached below
// when configured — checked by the peer on both transports.
func newFederationClient(addrs []string, m *metrics, reg prometheus.Registerer, node string, cfg federationClientConfig) (*federationClient, error) {
	if len(addrs) == 0 {
		return nil, nil
	}

	if cfg.CircuitConsecutiveFailures == 0 {
		cfg.CircuitConsecutiveFailures = defaultCircuitConsecutiveFailures
	}
	if cfg.CircuitOpenDuration <= 0 {
		cfg.CircuitOpenDuration = defaultCircuitOpenDuration
	}
	forwardTimeout := cfg.ForwardTimeout
	if forwardTimeout <= 0 {
		forwardTimeout = federationForwardTimeout
	}
	// The outer budget must never starve the per-peer deadline.
	broadcastBudget := federationBroadcastBudget
	if 2*forwardTimeout > broadcastBudget {
		broadcastBudget = 2 * forwardTimeout
	}

	labels := prometheus.Labels{"node": node}

	timeoutTotal := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:        "federation_forward_timeout_total",
			Help:        "Total Forward RPCs that exceeded the per-call timeout, labeled by peer.",
			ConstLabels: labels,
		},
		[]string{"peer"},
	)
	reg.MustRegister(timeoutTotal)

	circuitState := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name:        "federation_circuit_state",
			Help:        "Per-peer circuit breaker state: 0=closed, 1=open, 2=half-open.",
			ConstLabels: labels,
		},
		[]string{"peer"},
	)
	reg.MustRegister(circuitState)

	skippedTotal := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:        "federation_circuit_skipped_total",
			Help:        "Total Forward attempts skipped because the per-peer circuit was open.",
			ConstLabels: labels,
		},
		[]string{"peer"},
	)
	reg.MustRegister(skippedTotal)

	forwardLatency := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:        "federation_forward_duration_seconds",
			Help:        "Wall-clock duration of Forward RPC attempts, labeled by peer (no outcome dimension).",
			Buckets:     prometheus.ExponentialBuckets(0.001, 2, 12),
			ConstLabels: labels,
		},
		[]string{"peer"},
	)
	reg.MustRegister(forwardLatency)

	fc := &federationClient{
		metrics:         m,
		timeoutTotal:    timeoutTotal,
		circuitState:    circuitState,
		skippedTotal:    skippedTotal,
		forwardLatency:  forwardLatency,
		forwardTimeout:  forwardTimeout,
		broadcastBudget: broadcastBudget,
	}
	// Client keepalive (Phase 3): the federation channel is idle between
	// forwards, and the dual-path proxy's public leg re-handshakes TLS via
	// a relay on reconnect — easily blowing the 200 ms per-Forward budget
	// on the first post-idle call and feeding the breaker a spurious
	// failure. Pings keep the conn established through haproxy/Traefik so
	// forwards always ride a warm transport. Server side permits this via
	// the enforcement policy in ServerOptions().
	dialOpts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                30 * time.Second,
			Timeout:             10 * time.Second,
			PermitWithoutStream: true,
		}),
	}
	if tok := firstToken(cfg.FederationToken); tok != "" {
		dialOpts = append(dialOpts, grpc.WithUnaryInterceptor(federationTokenUnaryClientInterceptor(tok)))
	}
	if cfg.FederationAuthority != "" {
		dialOpts = append(dialOpts, grpc.WithAuthority(cfg.FederationAuthority))
	}
	for _, addr := range addrs {
		conn, err := grpc.NewClient(addr, dialOpts...)
		if err != nil {
			_ = fc.Close() //nolint:errcheck // preserve the dial failure that triggered cleanup
			return nil, fmt.Errorf("dial federation peer %q: %w", addr, err)
		}

		// Capture addr for the state-change closure.
		peerAddr := addr
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
		// Initialize gauge to closed (0) for every peer at startup.
		circuitState.WithLabelValues(peerAddr).Set(0)

		fc.peers = append(fc.peers, &federationPeer{
			addr:    peerAddr,
			conn:    conn,
			rpc:     federationpb.NewSignalFederationClient(conn),
			breaker: breaker,
		})
	}
	return fc, nil
}

// circuitStateToFloat maps the gobreaker state to the metric encoding
// (0=closed, 1=open, 2=half-open). Kept as a function rather than inline
// so all encoding lives in one place.
func circuitStateToFloat(s gobreaker.State) float64 {
	switch s {
	case gobreaker.StateClosed:
		return 0
	case gobreaker.StateOpen:
		return 1
	case gobreaker.StateHalfOpen:
		return 2
	default:
		return -1
	}
}

// Close tears down all outbound peer connections. Safe on a nil receiver.
func (fc *federationClient) Close() error {
	if fc == nil {
		return nil
	}
	var firstErr error
	for _, p := range fc.peers {
		if err := p.conn.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Broadcast fans Forward out to every configured peer concurrently.
// Returns delivered = any peer reported delivery, and complete = every
// peer gave a DEFINITIVE answer (delivered or not-found — no breaker
// skip, no RPC error, no timeout, no budget cut). complete is the
// per-call honesty signal for the ADR 1081 L2 negative cache
// (ADR 1085 Amendment 1 §3): "recipient absent across federation" may
// only be concluded — and cached — when every peer was actually asked
// and answered. A nil receiver (standalone) is trivially complete: a
// local miss IS a full miss.
//
// Three layers of bounding:
//
//   - Per-peer deadline: fc.forwardTimeout (default 200 ms) on each
//     Forward call so a hung peer cannot stall the broadcast.
//   - Total wall-clock budget: fc.broadcastBudget caps the entire
//     Broadcast regardless of per-peer behavior.
//   - Per-peer circuit breaker: a peer that consistently fails is
//     short-circuited entirely; the Forward RPC is not even attempted
//     while the breaker is open, and federation_circuit_skipped_total
//     increments instead.
//
// Per docs/design/ha-multi-site-validation.md §5.5 (Block 52A.4) plus
// Block 50B.6 circuit-breaker discipline. Tested by
// TestBroadcastHonorsTotalBudget (52A.4) and the
// circuit_test.go suite (50B.6).
// forwardOne forwards to the single federation peer at addr — presence-directed
// routing (ADR 1108) — reusing that peer's circuit breaker + per-Forward
// timeout. Returns (delivered, complete) with the same meaning as Broadcast:
// complete=false means the peer gave a non-answer (RPC error / timeout /
// breaker), so a miss is forward_failed, not a definitive not-found. An unknown
// addr returns (false, false) so the caller falls back to Broadcast.
func (fc *federationClient) forwardOne(ctx context.Context, addr string, msg *signalpb.EncryptedMessage) (delivered, complete bool) {
	if fc == nil {
		return false, true
	}
	for _, p := range fc.peers {
		if p.addr == addr {
			d, definitive := fc.broadcastOne(ctx, p, msg)
			return d, definitive
		}
	}
	return false, false
}

func (fc *federationClient) Broadcast(ctx context.Context, msg *signalpb.EncryptedMessage) (delivered, complete bool) {
	if fc == nil || len(fc.peers) == 0 {
		return false, true
	}

	// Total broadcast budget — derived from the caller's ctx so a caller
	// cancellation also tears down in-flight Forwards. The budget caps
	// the worst case where a hung peer would otherwise let us overshoot
	// the SLO even on a long-lived parent ctx. Zero-value fields (a
	// hand-constructed client, e.g. in tests) fall back to the package
	// defaults here rather than producing an already-expired context.
	budget := fc.broadcastBudget
	if budget <= 0 {
		budget = federationBroadcastBudget
	}
	bcastCtx, bcastCancel := context.WithTimeout(ctx, budget)
	defer bcastCancel()

	var (
		wg           sync.WaitGroup
		anyDelivered atomic.Bool
		incompletes  atomic.Int32
	)

	for _, p := range fc.peers {
		wg.Add(1)
		peer := p // capture loop variable for the goroutine
		safeGo("federation-broadcast", func() {
			defer wg.Done()
			del, definitive := fc.broadcastOne(bcastCtx, peer, msg)
			if del {
				anyDelivered.Store(true)
			}
			if !definitive {
				incompletes.Add(1)
			}
		})
	}

	// Belt-and-suspenders: wg.Wait() will unblock as soon as every
	// per-peer ctx (derived from bcastCtx) cancels, but we don't want a
	// misbehaving gRPC client library to keep us pinned past the
	// budget. Race the wait against bcastCtx.Done(). Counters are
	// atomic so the early-return reads are race-free if any goroutines
	// are still in flight.
	done := make(chan struct{})
	safeGo("federation-broadcast-wait", func() {
		wg.Wait()
		close(done)
	})
	select {
	case <-done:
		return anyDelivered.Load(), incompletes.Load() == 0
	case <-bcastCtx.Done():
		// Budget exceeded; per-peer goroutines will tear themselves
		// down via their derived contexts. Peers still in flight never
		// answered — the broadcast is NOT complete.
		return anyDelivered.Load(), false
	}
}

// broadcastOne issues one Forward to peer p with timeout + breaker
// protection. delivered = p reported delivery; definitive = p gave a
// real answer (delivered or not-found) rather than a skip/error/timeout.
// Pulled out of Broadcast so the per-peer logic is testable without
// spinning up a goroutine pool.
func (fc *federationClient) broadcastOne(ctx context.Context, p *federationPeer, msg *signalpb.EncryptedMessage) (delivered, definitive bool) {
	start := time.Now()

	forwardTimeout := fc.forwardTimeout
	if forwardTimeout <= 0 {
		forwardTimeout = federationForwardTimeout
	}
	resp, err := p.breaker.Execute(func() (*federationpb.ForwardResult, error) {
		peerCtx, cancel := context.WithTimeout(ctx, forwardTimeout)
		defer cancel()
		return p.rpc.Forward(peerCtx, msg)
	})
	elapsed := time.Since(start).Seconds()
	fc.forwardLatency.WithLabelValues(p.addr).Observe(elapsed)

	var outcome string
	switch {
	case errors.Is(err, gobreaker.ErrOpenState), errors.Is(err, gobreaker.ErrTooManyRequests):
		// Breaker rejected this attempt. Don't issue an RPC; record the skip.
		fc.skippedTotal.WithLabelValues(p.addr).Inc()
		outcome = outcomeForwardRPCError
	case err != nil:
		// Real RPC failure. Distinguish timeout for operator visibility.
		// gRPC errors carry a status code rather than wrapping
		// context.DeadlineExceeded directly, so check both.
		if errors.Is(err, context.DeadlineExceeded) || status.Code(err) == codes.DeadlineExceeded {
			fc.timeoutTotal.WithLabelValues(p.addr).Inc()
		}
		outcome = outcomeForwardRPCError
	case resp.GetDelivered():
		outcome = outcomeDelivered
	default:
		outcome = outcomeNotFound
	}
	fc.metrics.forwardSentTotal.WithLabelValues(outcome, p.addr).Inc()
	fc.metrics.forwardDuration.WithLabelValues(outcome, p.addr).Observe(elapsed)

	// err == nil means the peer answered — delivered OR a definitive
	// not-found. Breaker skips and RPC errors are non-answers.
	return err == nil && resp.GetDelivered(), err == nil
}
