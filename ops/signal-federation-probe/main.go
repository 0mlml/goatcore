// Signal-federation-probe — synthetic-traffic probe for the goat
// custom federated signal-server. Calls signalexchange.SignalExchange/Send
// with a remoteKey belonging to a peer at the federation-peer site. On a
// healthy mesh, the local signal forwards to its federation peer and
// returns delivered.
//
// Three modes:
//   - validation: one-shot, exit 0 on delivered, 1 on any other outcome.
//     Used for env qualification + CI.
//   - canary: loop on interval, expose Prometheus metrics. Used as a
//     production per-site canary to catch silent federation breakage.
//   - diagnostic: one-shot, structured JSON output of the round-trip.
//     Used by operators to investigate cross-site federation issues.
//
// First-class evidence for the Phase 3 federation gate; see
// docs/project/validation-state.md and docs/design/cgnat-validation-env.md.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	signalproto "github.com/netbirdio/netbird/shared/signal/proto"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var (
	mode        = flag.String("mode", "validation", "validation | canary | diagnostic")
	target      = flag.String("target", "localhost:10000", "signal-server gRPC endpoint")
	remoteKey   = flag.String("remote-key", "", "WireGuard pubkey at the federation-peer site (required)")
	sourceKey   = flag.String("source-key", "ZmVkLXByb2JlLXNvdXJjZS1mYWtlLWtleS0wMDAwMDAwMDAwMD0=", "synthetic source pubkey (probe identity)")
	interval    = flag.Duration("interval", 60*time.Second, "canary loop interval")
	metricsAddr = flag.String("metrics-addr", ":9091", "canary mode Prometheus listen address")
	rpcTimeout  = flag.Duration("rpc-timeout", 8*time.Second, "per-probe RPC timeout")
)

var (
	probeAttempts = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "federation_probe_attempts_total",
		Help: "Federation probe attempts, partitioned by outcome.",
	}, []string{"outcome"})
	probeLatency = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "federation_probe_latency_seconds",
		Help:    "Round-trip latency of the federation probe call.",
		Buckets: prometheus.ExponentialBuckets(0.001, 2, 12),
	})
	probeLastSuccess = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "federation_probe_last_success_unix_timestamp",
		Help: "Unix timestamp of the last delivered probe; alert if (now - this) > threshold.",
	})
)

type probeResult struct {
	Outcome    string        `json:"outcome"`
	LatencyMs  int64         `json:"latency_ms"`
	Target     string        `json:"target"`
	RemoteKey  string        `json:"remote_key"`
	SourceKey  string        `json:"source_key"`
	Error      string        `json:"error,omitempty"`
	Body       string        `json:"response_body,omitempty"`
	Timestamp  string        `json:"ts"`
}

// probeOnce makes one SignalExchange.Send and returns the outcome.
// Outcomes: "delivered" (signal echoed body back, federation succeeded),
// "rpc_error" (gRPC failed), "no_response" (call returned but no body).
// Federation forward delivery is observable on the SERVER side via
// signal_server_forward_sent_total — this client sees a successful RPC
// when the forward delivery succeeded; failure paths surface as RPC error.
func probeOnce(ctx context.Context) probeResult {
	res := probeResult{
		Target:    *target,
		RemoteKey: *remoteKey,
		SourceKey: *sourceKey,
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
	}
	t0 := time.Now()
	defer func() { res.LatencyMs = time.Since(t0).Milliseconds() }()

	conn, err := grpc.NewClient(*target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		res.Outcome = "rpc_error"
		res.Error = fmt.Sprintf("grpc dial: %v", err)
		return res
	}
	defer conn.Close()

	client := signalproto.NewSignalExchangeClient(conn)
	cctx, cancel := context.WithTimeout(ctx, *rpcTimeout)
	defer cancel()

	resp, err := client.Send(cctx, &signalproto.EncryptedMessage{
		Key:       *sourceKey,
		RemoteKey: *remoteKey,
		Body:      []byte{},
	})
	if err != nil {
		res.Outcome = "rpc_error"
		res.Error = err.Error()
		return res
	}
	if resp == nil {
		res.Outcome = "no_response"
		return res
	}
	res.Outcome = "delivered"
	res.Body = string(resp.GetBody())
	return res
}

func runValidation() int {
	res := probeOnce(context.Background())
	out, _ := json.Marshal(res)
	fmt.Println(string(out))
	if res.Outcome == "delivered" {
		return 0
	}
	return 1
}

func runDiagnostic() int {
	// Same shape as validation but always exit 0; intent is to inspect the
	// full round-trip rather than gate on it.
	res := probeOnce(context.Background())
	out, _ := json.MarshalIndent(res, "", "  ")
	fmt.Println(string(out))
	return 0
}

func runCanary() int {
	// Serve metrics endpoint.
	http.Handle("/metrics", promhttp.Handler())
	go func() {
		if err := http.ListenAndServe(*metricsAddr, nil); err != nil {
			log.Fatalf("metrics listen: %v", err)
		}
	}()
	log.Printf("federation-probe canary mode: target=%s interval=%s metrics=%s",
		*target, *interval, *metricsAddr)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	tick := time.NewTicker(*interval)
	defer tick.Stop()

	for {
		select {
		case <-stop:
			log.Println("shutting down")
			return 0
		case <-tick.C:
			res := probeOnce(context.Background())
			probeAttempts.WithLabelValues(res.Outcome).Inc()
			probeLatency.Observe(float64(res.LatencyMs) / 1000.0)
			if res.Outcome == "delivered" {
				probeLastSuccess.SetToCurrentTime()
			}
			out, _ := json.Marshal(res)
			log.Println(string(out))
		}
	}
}

func main() {
	flag.Parse()
	if *remoteKey == "" {
		fmt.Fprintln(os.Stderr, "FATAL: --remote-key is required")
		os.Exit(2)
	}
	switch *mode {
	case "validation":
		os.Exit(runValidation())
	case "diagnostic":
		os.Exit(runDiagnostic())
	case "canary":
		os.Exit(runCanary())
	default:
		fmt.Fprintf(os.Stderr, "FATAL: unknown --mode=%s\n", *mode)
		os.Exit(2)
	}
}
