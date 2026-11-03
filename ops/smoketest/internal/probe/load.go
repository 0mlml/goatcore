package probe

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultConcurrentConns = 20
	defaultHTTPRequests    = 50
)

// LoadProbe runs lightweight load tests against the relays: concurrent TCP
// connections and HTTP throughput. Only runs in "full" mode.
type LoadProbe struct{}

func init() {
	Register(&LoadProbe{})
}

func (p *LoadProbe) Category() Category { return CatLoad }

func (p *LoadProbe) Run(ctx context.Context, cfg *Config) []Result {
	if cfg.Mode != "full" {
		return []Result{{
			Category:  CatLoad,
			Name:      "load-test",
			Status:    Skip,
			Detail:    "skipped in quick mode",
			Timestamp: time.Now().UTC(),
		}}
	}

	var results []Result
	for _, relay := range cfg.Relays {
		results = append(results, p.concurrentTCP(ctx, relay))
		results = append(results, p.httpThroughput(ctx, cfg, relay))
	}
	return results
}

// concurrentTCP opens N TCP connections simultaneously to relay:443 and
// measures success rate and total time.
func (p *LoadProbe) concurrentTCP(ctx context.Context, relay RelayInfo) Result {
	addr := net.JoinHostPort(relay.IP, "443")
	var successes atomic.Int32
	var wg sync.WaitGroup

	start := time.Now()
	for i := 0; i < defaultConcurrentConns; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d := net.Dialer{Timeout: 10 * time.Second}
			conn, err := d.DialContext(ctx, "tcp", addr)
			if err != nil {
				return
			}
			successes.Add(1)
			conn.Close()
		}()
	}
	wg.Wait()
	elapsed := time.Since(start)

	ok := int(successes.Load())
	status := Pass
	if ok < defaultConcurrentConns {
		status = Warn
	}
	if ok == 0 {
		status = Fail
	}

	return Result{
		Category:  CatLoad,
		Name:      fmt.Sprintf("concurrent-tcp-%s", relay.Name),
		Status:    status,
		Detail:    fmt.Sprintf("%d/%d connected in %.1fms", ok, defaultConcurrentConns, float64(elapsed.Milliseconds())),
		LatencyMs: float64(elapsed.Milliseconds()),
		Timestamp: time.Now().UTC(),
		Metadata: map[string]any{
			"successes":  ok,
			"total":      defaultConcurrentConns,
			"elapsed_ms": float64(elapsed.Milliseconds()),
		},
	}
}

// httpThroughput sends N HTTP requests to the management API through a relay
// and measures requests per second.
func (p *LoadProbe) httpThroughput(ctx context.Context, cfg *Config, relay RelayInfo) Result {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			ServerName: cfg.Domain,
		},
		DialContext: func(dctx context.Context, network, _ string) (net.Conn, error) {
			d := net.Dialer{Timeout: 10 * time.Second}
			return d.DialContext(dctx, network, net.JoinHostPort(relay.IP, "443"))
		},
		MaxIdleConnsPerHost: defaultHTTPRequests,
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   15 * time.Second,
	}

	url := fmt.Sprintf("https://%s/api/accounts", cfg.Domain)
	var successes atomic.Int32
	var wg sync.WaitGroup

	// Use a semaphore to limit concurrency to 10 in-flight requests at a time
	// to keep this lightweight.
	sem := make(chan struct{}, 10)

	start := time.Now()
	for i := 0; i < defaultHTTPRequests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			if err != nil {
				return
			}
			resp, err := client.Do(req)
			if err != nil {
				return
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			successes.Add(1)
		}()
	}
	wg.Wait()
	elapsed := time.Since(start)

	ok := int(successes.Load())
	rps := 0.0
	if elapsed.Seconds() > 0 {
		rps = float64(ok) / elapsed.Seconds()
	}

	status := Pass
	if ok < defaultHTTPRequests {
		status = Warn
	}
	if ok == 0 {
		status = Fail
	}

	return Result{
		Category:  CatLoad,
		Name:      fmt.Sprintf("http-throughput-%s", relay.Name),
		Status:    status,
		Detail:    fmt.Sprintf("%d/%d requests in %.1fs (%.1f req/s)", ok, defaultHTTPRequests, elapsed.Seconds(), rps),
		LatencyMs: float64(elapsed.Milliseconds()),
		Timestamp: time.Now().UTC(),
		Metadata: map[string]any{
			"successes":  ok,
			"total":      defaultHTTPRequests,
			"elapsed_s":  elapsed.Seconds(),
			"rps":        rps,
		},
	}
}
