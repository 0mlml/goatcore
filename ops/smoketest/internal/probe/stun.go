package probe

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/pion/stun/v3"
)

// STUNProbe sends STUN binding requests to each relay's UDP 3478 port and
// validates that a mapped address is returned.
type STUNProbe struct{}

func init() {
	Register(&STUNProbe{})
}

func (p *STUNProbe) Category() Category { return CatSTUN }

func (p *STUNProbe) Run(ctx context.Context, cfg *Config) []Result {
	var results []Result

	for _, relay := range cfg.Relays {
		results = append(results, p.checkSTUN(ctx, relay))
	}

	return results
}

func (p *STUNProbe) checkSTUN(ctx context.Context, relay RelayInfo) Result {
	addr := net.JoinHostPort(relay.IP, "3478")

	start := time.Now()

	conn, err := net.DialTimeout("udp", addr, 5*time.Second)
	if err != nil {
		return Result{
			Category:  CatSTUN,
			Name:      fmt.Sprintf("stun-%s", relay.Name),
			Status:    Fail,
			Detail:    fmt.Sprintf("UDP dial %s: %v", addr, err),
			Timestamp: time.Now().UTC(),
		}
	}
	defer conn.Close()

	// Set deadline so we don't hang forever.
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(5 * time.Second)
	}
	conn.SetDeadline(deadline)

	// Build a STUN binding request.
	msg := stun.MustBuild(stun.TransactionID, stun.BindingRequest)

	// Send request.
	if _, err := conn.Write(msg.Raw); err != nil {
		return Result{
			Category:  CatSTUN,
			Name:      fmt.Sprintf("stun-%s", relay.Name),
			Status:    Fail,
			Detail:    fmt.Sprintf("write to %s: %v", addr, err),
			Timestamp: time.Now().UTC(),
		}
	}

	// Read response.
	buf := make([]byte, 1500)
	n, err := conn.Read(buf)
	if err != nil {
		return Result{
			Category:  CatSTUN,
			Name:      fmt.Sprintf("stun-%s", relay.Name),
			Status:    Fail,
			Detail:    fmt.Sprintf("read from %s: %v", addr, err),
			Timestamp: time.Now().UTC(),
		}
	}
	elapsed := time.Since(start)

	// Parse response.
	resp := new(stun.Message)
	resp.Raw = buf[:n]
	if err := resp.Decode(); err != nil {
		return Result{
			Category:  CatSTUN,
			Name:      fmt.Sprintf("stun-%s", relay.Name),
			Status:    Fail,
			Detail:    fmt.Sprintf("decode response from %s: %v", addr, err),
			Timestamp: time.Now().UTC(),
		}
	}

	// Extract mapped address.
	var mappedAddr stun.XORMappedAddress
	if err := mappedAddr.GetFrom(resp); err != nil {
		// Try non-XOR mapped address as fallback.
		var plainAddr stun.MappedAddress
		if err2 := plainAddr.GetFrom(resp); err2 != nil {
			return Result{
				Category:  CatSTUN,
				Name:      fmt.Sprintf("stun-%s", relay.Name),
				Status:    Fail,
				Detail:    fmt.Sprintf("no mapped address in response from %s: %v", addr, err),
				Timestamp: time.Now().UTC(),
			}
		}
		return Result{
			Category:  CatSTUN,
			Name:      fmt.Sprintf("stun-%s", relay.Name),
			Status:    Pass,
			Detail:    fmt.Sprintf("mapped %s:%d via %s", plainAddr.IP, plainAddr.Port, addr),
			LatencyMs: float64(elapsed.Milliseconds()),
			Timestamp: time.Now().UTC(),
		}
	}

	return Result{
		Category:  CatSTUN,
		Name:      fmt.Sprintf("stun-%s", relay.Name),
		Status:    Pass,
		Detail:    fmt.Sprintf("mapped %s:%d via %s", mappedAddr.IP, mappedAddr.Port, addr),
		LatencyMs: float64(elapsed.Milliseconds()),
		Timestamp: time.Now().UTC(),
	}
}
