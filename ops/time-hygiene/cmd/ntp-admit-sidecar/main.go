// ntp-admit-sidecar: h2c-capable reverse proxy for the Netbird management API.
//
// Two independent admit guards (both must pass for admit-class calls):
//   1. NTP health check — blocks when local ntp_class != healthy (fail-closed)
//   2. Rate limiter    — token-bucket cap on admits per rolling window (C5)
//
// Admit-class (both guards applied):
//   HTTP REST: POST /api/setup-keys
//              PUT  /api/setup-keys/{id}
//              POST /api/peers
//              POST /api/groups
//              PUT  /api/groups/{id}
//   gRPC:      /management.ManagementService/Login   (peer enrollment)
//
// Revoke-class (always pass-through regardless of NTP or rate limit):
//   DELETE on any /api/* path (revoke/remove operations)
//   All GET/HEAD/OPTIONS requests
//   All other gRPC ManagementService methods (Sync, GetServerKey, etc.)
//
// Production-readiness hardening (Block 50B):
//   50B.1 — Bounded concurrency on the proxy path. A semaphore ahead of the
//           rate limiter caps in-flight requests at --max-inflight (default
//           256). Saturation returns 503 + Retry-After: 1 immediately rather
//           than letting goroutines pile up against a slow backend.
//   50B.2 — Panic recovery. Every spawned goroutine uses safego.Spawn so a
//           panic logs a structured event + increments time_hygiene_panic_total
//           rather than killing the process. A panicMiddleware wraps the proxy
//           handler so a panic inside checkNTP / rl.Allow / proxy code path is
//           also caught and counted (net/http already recovers panics in
//           handlers, but we want the metric for fleet-wide visibility).
//   50B.7 — Per-peer rate limiter. Replaces the v1 global token bucket with a
//           keyed limiter (LRU-bounded, 4096 entries default) so a single
//           misbehaving peer cannot drain the bucket for everyone. A separate
//           higher-cap global limiter still defends against many-peer
//           coordinated abuse.
//
// Design: docs/design/universal-ntp-hygiene.md §6
//         docs/design/mgmt-plane-threat-mitigations.md C5 (rate limit)
// Block:  docs/project/implementation-plan.md Block 12B-sidecar
//         docs/project/implementation-plan.md Block 50B
package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/usareur-af/goatcore/ops/time-hygiene/internal/otelinit"
	"github.com/usareur-af/goatcore/ops/time-hygiene/internal/ratelimit"
	"github.com/usareur-af/goatcore/ops/time-hygiene/internal/safego"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c" //nolint:staticcheck // Keep the existing h2c listener.
	"golang.org/x/sync/semaphore"
)

const serviceName = "ntp-admit-sidecar"

var (
	flagListen          = flag.String("listen", "127.0.0.1:8080", "address to listen on")
	flagBackend         = flag.String("backend", "http://127.0.0.1:8081", "management-a backend URL")
	flagNTPSocket       = flag.String("ntp-socket", "/run/ntp-health.sock", "ntp-health-agent Unix socket path")
	flagNTPTimeout      = flag.Duration("ntp-timeout", 200*time.Millisecond, "max time to wait for NTP verdict")
	flagRateLimitMax    = flag.Int("rate-limit-max", 20, "max admit-class requests per window, PER PEER (50B.7)")
	flagRateLimitWindow = flag.Duration("rate-limit-window", 24*time.Hour, "rolling window for per-peer rate limit")
	flagPeerLRUCap      = flag.Int("peer-lru-cap", 4096, "max distinct peers tracked by per-peer limiter (LRU eviction beyond this)")
	flagGlobalRateMax   = flag.Int("global-rate-max", 1000, "fleet-wide backstop admit cap (50B.7) — guards against many-peer coordinated abuse")
	flagMaxInflight     = flag.Int64("max-inflight", 256, "max in-flight proxy requests; saturation returns 503 (50B.1)")
	flagMetricsListen   = flag.String("metrics-listen", "127.0.0.1:9754", "/metrics listener address — separate from proxy port (50B.1)")
)

// Prometheus metrics. Names anchor on admit_sidecar_* per 50B.1 / 50B.7 spec.
var (
	inflightProxy = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "admit_sidecar_inflight_proxy",
		Help: "Number of proxy requests currently in flight (50B.1 bounded concurrency).",
	})
	proxyRejectedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "admit_sidecar_proxy_rejected_total",
		Help: "Proxy requests rejected before reaching backend.",
	}, []string{"reason"})
	perPeerLimitedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "admit_sidecar_per_peer_limited_total",
		Help: "Admit-class requests rejected by the per-peer rate limiter (50B.7).",
	}, []string{"peer_id_kind"})
	globalLimitedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "admit_sidecar_global_limited_total",
		Help: "Admit-class requests rejected by the fleet-wide backstop limiter (50B.7).",
	})
)

func main() {
	flag.Parse()

	// Tag every safego panic-recovery counter with this service name.
	safego.SetService(serviceName)

	// Block 27C / 50B.4 — OTLP trace producer. Best-effort: init failure is
	// logged to stderr but never blocks daemon startup.
	otelShutdown, err := otelinit.Init(context.Background(), otelinit.Opts{
		ServiceName:    serviceName,
		ServiceVersion: version,
		Extra: []attribute.KeyValue{
			attribute.String("dbb.listen", *flagListen),
		},
	})
	if err != nil {
		slog.Error("otel init failed; continuing without telemetry", "err", err)
	}

	backendURL, err := url.Parse(*flagBackend)
	if err != nil {
		slog.Error("invalid backend URL", "url", *flagBackend, "err", err)
		os.Exit(1)
	}

	// Per-peer limiter (50B.7): primary defense against single-peer abuse.
	peerRL, err := ratelimit.NewPeerLimiter(*flagPeerLRUCap, *flagRateLimitMax, *flagRateLimitWindow)
	if err != nil {
		slog.Error("peer rate limiter init failed", "err", err)
		os.Exit(1)
	}
	// Fleet-wide backstop (50B.7): defense against many-peer coordinated abuse
	// where each individual peer stays under its own cap but the aggregate would
	// still flood the backend.
	globalRL := ratelimit.NewBucket(*flagGlobalRateMax, *flagRateLimitWindow)

	p := newProxy(backendURL, *flagNTPSocket, *flagNTPTimeout, peerRL, globalRL)

	// Bounded-concurrency middleware (50B.1): semaphore ahead of the rate
	// limiter so a backend that's already saturated cannot pile goroutines
	// indefinitely. Saturation returns 503 + Retry-After: 1 quickly so callers
	// retry against the standby.
	sem := semaphore.NewWeighted(*flagMaxInflight)
	// otelhttp middleware (Block 27C / 50B.4) wraps the inbound handler so
	// every request emits a root span with standard HTTP semantic-convention
	// attrs. Sits OUTSIDE concurrency + panic middleware so even rejected /
	// panic-recovered requests are observable. SpanNameFormatter uses the
	// service name + method (path is captured by url.path attr) to keep
	// span-name cardinality bounded.
	handler := otelhttp.NewHandler(
		concurrencyMiddleware(sem, panicMiddleware(p)),
		serviceName,
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			return serviceName + "." + r.Method
		}),
	)

	// h2c wraps the handler so gRPC (HTTP/2 cleartext) clients are served correctly.
	h2s := &http2.Server{} //nolint:staticcheck // h2c.NewHandler requires this server.
	// Timeouts (Block 50A.1):
	// - ReadHeaderTimeout caps the slowloris window during the HTTP/1.1 upgrade
	//   or h2c preface; bounded short.
	// - ReadTimeout / WriteTimeout deliberately UNSET (zero = no limit) because
	//   this listener carries gRPC bidi streams (peer enrollment via
	//   /management.ManagementService/Login). Setting them would kill long-lived
	//   streams. Per-stream cancellation comes from the gRPC layer's keepalive.
	// - IdleTimeout closes idle keep-alive connections after 5 min so a flood of
	//   half-open conns can't exhaust the FD pool indefinitely.
	srv := &http.Server{
		Addr:              *flagListen,
		Handler:           h2c.NewHandler(handler, h2s), //nolint:staticcheck // Keep the existing h2c listener.
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       5 * time.Minute,
	}

	// /metrics on a SEPARATE loopback listener (50B.1): never exposed on the
	// proxy port. Even if the proxy port is bound 0.0.0.0 in front of nftables,
	// the metrics surface stays loopback-only.
	metricsSrv := &http.Server{
		Addr:              *flagMetricsListen,
		Handler:           promhttp.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	// Bound the OTel exporter shutdown so a slow / unreachable collector
	// cannot stall the daemon's SIGTERM cleanup. Skip if init failed (nil).
	if otelShutdown != nil {
		defer func() {
			shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer shutCancel()
			if err := otelShutdown(shutCtx); err != nil {
				slog.Warn("otel shutdown returned error", "err", err)
			}
		}()
	}

	// Shutdown watcher (50B.2): safego.Spawn so a panic in shutdown still
	// keeps the process alive long enough to emit a metric and slog event.
	safego.Spawn("shutdown-watcher", func() {
		<-ctx.Done()
		// Shutdown ctx is intentionally NOT inherited from the parent (which is
		// already canceled by SIGTERM at this point). A fresh 5s budget lets
		// in-flight requests drain cleanly.
		shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutCancel()
		_ = srv.Shutdown(shutCtx)        //nolint:contextcheck // intentional shutdown decoupling — see comment above
		_ = metricsSrv.Shutdown(shutCtx) //nolint:contextcheck // intentional shutdown decoupling — see comment above
	})

	// Metrics listener runs in its own goroutine; failures are logged but do
	// not kill the process. The proxy is the load-bearing path.
	var metricsWG sync.WaitGroup
	metricsWG.Add(1)
	safego.Spawn("metrics-listener", func() {
		defer metricsWG.Done()
		slog.Info("metrics listener started", "addr", *flagMetricsListen)
		if err := metricsSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("metrics listener error", "err", err)
		}
	})

	slog.Info("ntp-admit-sidecar started",
		"listen", *flagListen,
		"backend", *flagBackend,
		"ntp_socket", *flagNTPSocket,
		"per_peer_rate_limit_max", *flagRateLimitMax,
		"per_peer_rate_limit_window", flagRateLimitWindow.String(),
		"peer_lru_cap", *flagPeerLRUCap,
		"global_rate_max", *flagGlobalRateMax,
		"max_inflight", *flagMaxInflight,
		"metrics_listen", *flagMetricsListen,
	)

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("server error", "err", err)
		os.Exit(1)
	}
	metricsWG.Wait()
	slog.Info("ntp-admit-sidecar stopped")
}

// ---------------------------------------------------------------------------
// Middleware: bounded concurrency (50B.1)
// ---------------------------------------------------------------------------

// concurrencyMiddleware caps in-flight requests at sem's weight. On saturation
// (TryAcquire returns false), it responds 503 + Retry-After: 1 IMMEDIATELY,
// without blocking. Blocking on a semaphore under sustained overload is
// indistinguishable from goroutine pile-up; failing fast lets clients retry
// against the standby.
//
// The gauge admit_sidecar_inflight_proxy reflects current in-flight count;
// admit_sidecar_proxy_rejected_total{reason="saturation"} counts rejections.
func concurrencyMiddleware(sem *semaphore.Weighted, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !sem.TryAcquire(1) {
			proxyRejectedTotal.WithLabelValues("saturation").Inc()
			slog.Warn("proxy rejected: saturation",
				"method", r.Method, "path", r.URL.Path,
			)
			w.Header().Set("Retry-After", "1")
			http.Error(w, "saturation", http.StatusServiceUnavailable)
			return
		}
		defer sem.Release(1)
		inflightProxy.Inc()
		defer inflightProxy.Dec()
		next.ServeHTTP(w, r)
	})
}

// ---------------------------------------------------------------------------
// Middleware: panic recovery (50B.2)
// ---------------------------------------------------------------------------

// panicMiddleware wraps the proxy handler in defer-recover so a panic in any
// downstream code path (checkNTP, rl.Allow, proxy.ServeHTTP) emits a
// time_hygiene_panic_total{...,goroutine="http-handler"} metric instead of
// just being logged by net/http's default recovery. Combined with the
// safego.Spawn wrappers on every spawned goroutine, this gives a uniform
// fleet-wide signal regardless of which goroutine panicked.
func panicMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer safego.Recover("http-handler")
		next.ServeHTTP(w, r)
	})
}

// ---------------------------------------------------------------------------
// Proxy
// ---------------------------------------------------------------------------

type proxy struct {
	rp         *httputil.ReverseProxy
	ntpSocket  string
	ntpTimeout time.Duration
	peerRL     *ratelimit.PeerLimiter
	globalRL   *ratelimit.Bucket
}

func newProxy(backend *url.URL, ntpSocket string, ntpTimeout time.Duration, peerRL *ratelimit.PeerLimiter, globalRL *ratelimit.Bucket) *proxy {
	// h2c-capable transport — management-a serves gRPC over h2c on the same port.
	h2Transport := &http2.Transport{ //nolint:staticcheck // The backend uses h2c.
		AllowHTTP: true, //nolint:staticcheck // h2c carries HTTP/2 without TLS.
		DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) { //nolint:staticcheck // This h2c connection has no TLS handshake.
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
	}

	rp := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(backend)
			r.Out.Host = r.In.Host
		},
		Transport: h2Transport,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			slog.Warn("proxy error", "path", r.URL.Path, "err", err)
			http.Error(w, "backend unavailable", http.StatusBadGateway)
		},
	}

	return &proxy{rp: rp, ntpSocket: ntpSocket, ntpTimeout: ntpTimeout, peerRL: peerRL, globalRL: globalRL}
}

func (p *proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	admitClass := isAdmitClass(r.Method, r.URL.Path)
	// Annotate the inbound otelhttp span with admit-class disposition. The
	// otelhttp middleware already attached the standard HTTP semantic-
	// convention attrs (http.method, url.path, etc.); these are the goat-
	// specific signals that distinguish admit-class vs revoke-class /
	// pass-through (Block 27C / 50B.4).
	annotateAdmitSpan(r.Context(), admitClass)
	if admitClass {
		if err := p.checkNTP(r.Context()); err != nil {
			annotateAdmitSpan(r.Context(), true,
				attribute.String(attrDenyReason, "ntp-unhealthy"),
			)
			p.deny(w, r, http.StatusServiceUnavailable, "ntp-unhealthy", err.Error())
			return
		}
		// 50B.7: per-peer FIRST. The fleet-wide backstop only counts what
		// passes the per-peer cap, otherwise a single noisy peer would also
		// burn through global tokens.
		peerID, kind := extractPeerID(r)
		annotateAdmitSpan(r.Context(), true,
			attribute.String(attrPeerIDKind, string(kind)),
		)
		if err := p.peerRL.Allow(peerID); err != nil {
			perPeerLimitedTotal.WithLabelValues(string(kind)).Inc()
			annotateAdmitSpan(r.Context(), true,
				attribute.String(attrDenyReason, "rate-limit-per-peer"),
			)
			p.deny(w, r, http.StatusTooManyRequests, "rate-limit-per-peer", err.Error())
			return
		}
		if err := p.globalRL.Allow(); err != nil {
			globalLimitedTotal.Inc()
			annotateAdmitSpan(r.Context(), true,
				attribute.String(attrDenyReason, "rate-limit-global"),
			)
			p.deny(w, r, http.StatusTooManyRequests, "rate-limit-global", err.Error())
			return
		}
	}
	p.rp.ServeHTTP(w, r)
}

func (p *proxy) deny(w http.ResponseWriter, r *http.Request, code int, reason, detail string) {
	slog.Warn("admit blocked",
		"method", r.Method, "path", r.URL.Path,
		"reason", reason, "detail", detail)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	//nolint:errcheck // HTTP response delivery is best-effort after the handler has selected its result.
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error":  "admit blocked",
		"reason": reason,
		"detail": detail,
	})
}

// checkNTP reads the current verdict from ntp-health-agent's Unix socket.
// Returns nil if verdict is "healthy", an error otherwise (including socket failure).
// The supplied parent ctx (typically the inbound request's context) is wrapped
// with the per-probe timeout so a client-side cancel halts the health check
// promptly.
//
// Wrapped in an OTel child span (ntp-admit-sidecar.checkNTP) so the chrony
// health-check latency surfaces distinct from outer request latency. The
// observed ntp_class is also propagated to the inbound request span as
// dbb.ntp_class so a single query joins admit-class disposition + chrony
// verdict (Block 27C / 50B.4).
func (p *proxy) checkNTP(parent context.Context) error {
	ctx, span := startCheckNTPSpan(parent)
	defer span.End()
	// DialContext over Dialer.Timeout (Block 50A.3 noctx): explicit context-cancel
	// surface so a request cancellation interrupts a stuck health probe.
	dialer := net.Dialer{Timeout: p.ntpTimeout}
	ctx, cancel := context.WithTimeout(ctx, p.ntpTimeout)
	defer cancel()
	conn, err := dialer.DialContext(ctx, "unix", p.ntpSocket)
	if err != nil {
		span.RecordError(err)
		span.SetAttributes(attribute.String(attrCheckNTPErr, "dial_failed"))
		return fmt.Errorf("ntp-health-agent unreachable (fail-closed): %w", err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(p.ntpTimeout)); err != nil {
		span.RecordError(err)
		span.SetAttributes(attribute.String(attrCheckNTPErr, "deadline_failed"))
		return fmt.Errorf("set ntp-health-agent deadline: %w", err)
	}

	var verdict struct {
		NtpClassStr string `json:"ntp_class_str"`
	}
	if err := json.NewDecoder(conn).Decode(&verdict); err != nil {
		span.RecordError(err)
		span.SetAttributes(attribute.String(attrCheckNTPErr, "decode_failed"))
		return fmt.Errorf("ntp verdict decode failed (fail-closed): %w", err)
	}
	// Surface the observed verdict on BOTH the sub-span (this scope) and the
	// parent inbound request span (so a single span query sees admit-class
	// disposition + verdict together).
	span.SetAttributes(attribute.String(attrNTPClass, verdict.NtpClassStr))
	annotateAdmitSpan(parent, true, attribute.String(attrNTPClass, verdict.NtpClassStr))
	if verdict.NtpClassStr != "healthy" {
		return fmt.Errorf("ntp_class=%s", verdict.NtpClassStr)
	}
	return nil
}

// isAdmitClass returns true for requests that mutate the admit plane.
func isAdmitClass(method, path string) bool {
	if isGRPC(path) {
		return path == "/management.ManagementService/Login"
	}
	switch method {
	case http.MethodPost:
		return strings.HasPrefix(path, "/api/setup-keys") ||
			strings.HasPrefix(path, "/api/peers") ||
			strings.HasPrefix(path, "/api/groups")
	case http.MethodPut:
		return strings.HasPrefix(path, "/api/setup-keys/") ||
			strings.HasPrefix(path, "/api/groups/")
	}
	return false
}

func isGRPC(path string) bool {
	return strings.HasPrefix(path, "/management.")
}

// ---------------------------------------------------------------------------
// Per-peer rate-limit key extraction (50B.7)
// ---------------------------------------------------------------------------

// peerIDKind names the source of a peer-ID extraction. Reported as the
// peer_id_kind label on admit_sidecar_per_peer_limited_total so an operator
// can see whether keyed-by-setup-key flows are saturating vs IP-fallback flows
// (which usually means a misconfigured client).
type peerIDKind string

const (
	peerIDKindSetupKey peerIDKind = "setup_key"
	peerIDKindSrcIP    peerIDKind = "src_ip"
)

// extractPeerID derives a peer identifier from the request, preferring the
// authenticated setup key when available. Order:
//  1. gRPC Login: HTTP/2 trailers expose grpc metadata as headers under the
//     "grpc-metadata-<key>" prefix when relayed by grpc-gateway / h2c. We
//     read the canonicalized "Setup-Key" / "X-Nb-Setup-Key" header.
//  2. HTTP REST: X-NB-Setup-Key header — set by trusted clients that already
//     know the setup key (admin tooling, partner-provisioning). This is NOT
//     a security boundary; it's a rate-limit key. The actual key validation
//     happens at the management server, not here.
//  3. Fallback: source IP (RemoteAddr stripped of port).
//
// The chosen value is hashed-by-LRU (golang-lru handles arbitrary string
// keys); we deliberately do NOT log the raw setup key, only its kind.
func extractPeerID(r *http.Request) (string, peerIDKind) {
	// gRPC metadata is forwarded by h2c clients in canonical-cased headers.
	// Try a couple of conventional spellings.
	for _, h := range []string{"Setup-Key", "X-Nb-Setup-Key", "Grpc-Metadata-Setup-Key"} {
		if v := r.Header.Get(h); v != "" {
			return "sk:" + v, peerIDKindSetupKey
		}
	}
	// IP fallback. Strip the port — same /32 different ports must rate-limit
	// together (a single misbehaving client cannot evade by reconnecting on
	// a fresh source port).
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return "ip:" + host, peerIDKindSrcIP
}
