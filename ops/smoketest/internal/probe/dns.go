package probe

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"
)

// DNSProbe resolves the domain and relay DNS names, verifying round-robin coverage.
type DNSProbe struct{}

func init() {
	Register(&DNSProbe{})
}

func (p *DNSProbe) Category() Category { return CatDNS }

func (p *DNSProbe) Run(ctx context.Context, cfg *Config) []Result {
	var results []Result

	// Check 1: Resolve main domain, count A records.
	start := time.Now()
	ips, err := net.LookupHost(cfg.Domain)
	elapsed := time.Since(start)
	if err != nil {
		results = append(results, Result{
			Category:  CatDNS,
			Name:      "domain-resolve",
			Status:    Fail,
			Detail:    fmt.Sprintf("failed to resolve %s: %v", cfg.Domain, err),
			Timestamp: time.Now().UTC(),
		})
	} else {
		results = append(results, Result{
			Category:  CatDNS,
			Name:      "domain-resolve",
			Status:    Pass,
			Detail:    fmt.Sprintf("%s -> %d A record(s): %s", cfg.Domain, len(ips), strings.Join(ips, ", ")),
			LatencyMs: float64(elapsed.Milliseconds()),
			Timestamp: time.Now().UTC(),
		})
	}

	// Check 2: Resolve each relay DNS name.
	for _, name := range cfg.RelayDNS {
		start = time.Now()
		rIPs, err := net.LookupHost(name)
		elapsed = time.Since(start)
		if err != nil {
			results = append(results, Result{
				Category:  CatDNS,
				Name:      fmt.Sprintf("relay-dns-%s", name),
				Status:    Fail,
				Detail:    fmt.Sprintf("failed to resolve %s: %v", name, err),
				Timestamp: time.Now().UTC(),
			})
			continue
		}
		results = append(results, Result{
			Category:  CatDNS,
			Name:      fmt.Sprintf("relay-dns-%s", name),
			Status:    Pass,
			Detail:    fmt.Sprintf("%s -> %s", name, strings.Join(rIPs, ", ")),
			LatencyMs: float64(elapsed.Milliseconds()),
			Timestamp: time.Now().UTC(),
		})
	}

	// Check 3: Verify round-robin contains all expected IPs.
	if len(cfg.ExpectedIPs) > 0 && ips != nil {
		resolved := make(map[string]bool, len(ips))
		for _, ip := range ips {
			resolved[ip] = true
		}

		var missing []string
		for _, expected := range cfg.ExpectedIPs {
			if !resolved[expected] {
				missing = append(missing, expected)
			}
		}

		sort.Strings(missing)
		if len(missing) == 0 {
			results = append(results, Result{
				Category:  CatDNS,
				Name:      "round-robin-coverage",
				Status:    Pass,
				Detail:    fmt.Sprintf("all %d expected IPs present in DNS", len(cfg.ExpectedIPs)),
				Timestamp: time.Now().UTC(),
			})
		} else {
			results = append(results, Result{
				Category:  CatDNS,
				Name:      "round-robin-coverage",
				Status:    Fail,
				Detail:    fmt.Sprintf("missing IPs from round-robin: %s", strings.Join(missing, ", ")),
				Timestamp: time.Now().UTC(),
			})
		}
	}

	return results
}
