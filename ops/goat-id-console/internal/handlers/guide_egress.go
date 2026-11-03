package handlers

import (
	"context"
	"net"
	"sync"
	"time"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/welcome"
)

// guideEgress resolves the firewall topic's hostnames to public addresses.
//
// The addresses live in public DNS, not in config, so they are looked up when
// the page is asked for — which keeps the page right after a relay is rebuilt.
// Results are cached so a burst of page loads is not a burst of lookups, and
// each lookup is bounded so a slow resolver degrades the table to "did not
// resolve" instead of hanging the page.
var guideEgress = &egressResolver{
	lookup:  net.DefaultResolver.LookupHost,
	ttl:     10 * time.Minute,
	timeout: 2 * time.Second,
}

type egressResolver struct {
	lookup  func(ctx context.Context, host string) ([]string, error)
	ttl     time.Duration
	timeout time.Duration

	mu    sync.Mutex
	cache map[string]egressEntry
}

type egressEntry struct {
	ips []string
	at  time.Time
}

func (e *egressResolver) hosts(ctx context.Context, mgmtURL string, relays, stun []string) []welcome.EgressHost {
	hosts := welcome.EgressHostsFrom(mgmtURL, relays, stun)
	var wg sync.WaitGroup
	for i := range hosts {
		wg.Add(1)
		go func(h *welcome.EgressHost) {
			defer wg.Done()
			h.IPs = e.resolve(ctx, h.Host)
			h.Unresolved = len(h.IPs) == 0
		}(&hosts[i])
	}
	wg.Wait()
	return hosts
}

func (e *egressResolver) resolve(ctx context.Context, host string) []string {
	e.mu.Lock()
	if c, ok := e.cache[host]; ok && time.Since(c.at) < e.ttl {
		e.mu.Unlock()
		return c.ips
	}
	e.mu.Unlock()

	lctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()
	addrs, err := e.lookup(lctx, host)
	if err != nil {
		// A failure is not cached: the next page load tries again.
		return nil
	}
	ips := welcome.PublicIPs(addrs)

	e.mu.Lock()
	if e.cache == nil {
		e.cache = map[string]egressEntry{}
	}
	e.cache[host] = egressEntry{ips: ips, at: time.Now()}
	e.mu.Unlock()
	return ips
}
