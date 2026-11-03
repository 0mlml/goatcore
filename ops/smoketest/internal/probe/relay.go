package probe

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

// RelayProbe checks TCP connectivity to rathole control channels and HTTPS
// management API availability on each relay.
type RelayProbe struct{}

func init() {
	Register(&RelayProbe{})
}

func (p *RelayProbe) Category() Category { return CatRelay }

func (p *RelayProbe) Run(ctx context.Context, cfg *Config) []Result {
	var results []Result

	for _, relay := range cfg.Relays {
		results = append(results, p.checkRathole(ctx, relay)...)
		results = append(results, p.checkHTTPS(ctx, cfg, relay)...)
	}

	return results
}

// checkRathole dials TCP port 2333 (rathole control channel) on the relay.
func (p *RelayProbe) checkRathole(ctx context.Context, relay RelayInfo) []Result {
	addr := net.JoinHostPort(relay.IP, "2333")
	start := time.Now()
	d := net.Dialer{Timeout: 5 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	elapsed := time.Since(start)

	if err != nil {
		return []Result{{
			Category:  CatRelay,
			Name:      fmt.Sprintf("rathole-%s", relay.Name),
			Status:    Fail,
			Detail:    fmt.Sprintf("TCP %s: %v", addr, err),
			Timestamp: time.Now().UTC(),
		}}
	}
	conn.Close()

	return []Result{{
		Category:  CatRelay,
		Name:      fmt.Sprintf("rathole-%s", relay.Name),
		Status:    Pass,
		Detail:    fmt.Sprintf("TCP %s connected", addr),
		LatencyMs: float64(elapsed.Milliseconds()),
		Timestamp: time.Now().UTC(),
	}}
}

// checkHTTPS sends an HTTPS request to the management API through a specific
// relay IP (equivalent to curl --resolve). We expect HTTP 401 (authentication
// required) as proof that Traefik is routing and the management server is
// reachable behind the relay.
func (p *RelayProbe) checkHTTPS(ctx context.Context, cfg *Config, relay RelayInfo) []Result {
	// Build a transport that pins the domain to this relay's IP.
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			ServerName: cfg.Domain,
		},
		DialContext: func(dctx context.Context, network, _ string) (net.Conn, error) {
			d := net.Dialer{Timeout: 10 * time.Second}
			return d.DialContext(dctx, network, net.JoinHostPort(relay.IP, "443"))
		},
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   15 * time.Second,
	}

	url := fmt.Sprintf("https://%s/api/accounts", cfg.Domain)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return []Result{{
			Category:  CatRelay,
			Name:      fmt.Sprintf("https-%s", relay.Name),
			Status:    Fail,
			Detail:    fmt.Sprintf("request build error: %v", err),
			Timestamp: time.Now().UTC(),
		}}
	}

	start := time.Now()
	resp, err := client.Do(req)
	elapsed := time.Since(start)

	if err != nil {
		return []Result{{
			Category:  CatRelay,
			Name:      fmt.Sprintf("https-%s", relay.Name),
			Status:    Fail,
			Detail:    fmt.Sprintf("HTTPS via %s: %v", relay.IP, err),
			Timestamp: time.Now().UTC(),
		}}
	}
	defer func() {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	if resp.StatusCode == http.StatusUnauthorized {
		return []Result{{
			Category:  CatRelay,
			Name:      fmt.Sprintf("https-%s", relay.Name),
			Status:    Pass,
			Detail:    fmt.Sprintf("HTTPS via %s: 401 (management reachable)", relay.IP),
			LatencyMs: float64(elapsed.Milliseconds()),
			Timestamp: time.Now().UTC(),
		}}
	}

	return []Result{{
		Category:  CatRelay,
		Name:      fmt.Sprintf("https-%s", relay.Name),
		Status:    Warn,
		Detail:    fmt.Sprintf("HTTPS via %s: unexpected status %d (expected 401)", relay.IP, resp.StatusCode),
		LatencyMs: float64(elapsed.Milliseconds()),
		Timestamp: time.Now().UTC(),
	}}
}
