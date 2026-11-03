// Package measure — probe-policy fetch + apply.
//
// Background. The do-no-harm review (4779101) closed the
// fratricide-at-scale issues — TLS cert probe was 60s instead of 1h,
// CAP_NET_ADMIN was overgranted, UDP probes were poisoning the relay
// QUIC listener. But "right shape at design time" doesn't help if the
// fleet's probe load gets out of control in production for any reason
// (config drift, unforeseen scale, debugging session left on). We need
// a runtime kill switch + telemetry.
//
// Two control points:
//
//   1. Local kill switch (per-peer): env var SNITCH_PROBE_DISABLED. Any
//      non-empty value forces PolicyOff regardless of collector. Useful
//      when an operator needs to silence one peer without touching the
//      collector. Survives restarts as long as the systemd unit env
//      file says so.
//   2. Collector-side policy (fleet-wide): GET /api/probe-policy returns
//      the current policy. Probe fetches once per cycle and applies.
//      Operators set the collector's policy via env var (today) or
//      admin PUT API (follow-up); change propagates within 1 cycle.
//
// Policy precedence (highest wins):
//   1. SNITCH_PROBE_DISABLED env → PolicyOff
//   2. Collector's current policy (if reachable + not expired)
//   3. Last-known cached policy (if collector unreachable)
//   4. PolicyDefault (boot fallback)

package measure

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/usareur-af/goatcore/ops/snitch/internal/types"
)

// PolicyClient fetches the current probe policy from the collector.
// Caches the last-known policy so that collector-unreachable cycles
// continue with the last-known value rather than reverting to default.
type PolicyClient struct {
	collectorURL string
	httpClient   *http.Client

	mu           sync.Mutex
	cached       types.ProbePolicy
	cachedAt     time.Time
	cachedSource string
}

// NewPolicyClient constructs a PolicyClient. collectorURL is the same
// URL the probe POSTs reports to. Timeouts are aggressive (the policy
// fetch should NEVER block the probe's main work).
func NewPolicyClient(collectorURL string) *PolicyClient {
	return &PolicyClient{
		collectorURL: collectorURL,
		httpClient: &http.Client{
			Timeout: 3 * time.Second,
		},
	}
}

// CurrentPolicy returns the policy the probe should apply this cycle.
// Order of preference:
//  1. SNITCH_PROBE_DISABLED env → PolicyOff (kill switch, source="kill-switch-env")
//  2. GET /api/probe-policy → fresh policy, source="collector"
//  3. cached last-known policy, source="fallback-cached"
//  4. PolicyDefault, source="fallback-default"
//
// `expiredAt` checking happens at the application site (cmd-side), not
// here — this just returns whatever the collector said.
func (pc *PolicyClient) CurrentPolicy(ctx context.Context) (types.ProbePolicy, string) {
	if killed, ok := killSwitchActive(); ok {
		return types.ProbePolicy{
			Mode:   types.PolicyOff,
			Reason: killed,
			SetBy:  "kill-switch-env",
			SetAt:  time.Now(),
		}, "kill-switch-env"
	}

	if p, ok := pc.fetchFromCollector(ctx); ok {
		pc.mu.Lock()
		pc.cached = p
		pc.cachedAt = time.Now()
		pc.cachedSource = "collector"
		pc.mu.Unlock()
		return p, "collector"
	}

	pc.mu.Lock()
	defer pc.mu.Unlock()
	if pc.cachedSource != "" && time.Since(pc.cachedAt) < 1*time.Hour {
		return pc.cached, "fallback-cached"
	}
	return defaultPolicy(), "fallback-default"
}

// fetchFromCollector does the GET /api/probe-policy call. Best-effort.
// Returns (policy, true) on success, (zero, false) on any failure.
func (pc *PolicyClient) fetchFromCollector(ctx context.Context) (types.ProbePolicy, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pc.collectorURL+"/api/probe-policy", nil)
	if err != nil {
		return types.ProbePolicy{}, false
	}
	resp, err := pc.httpClient.Do(req)
	if err != nil {
		return types.ProbePolicy{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return types.ProbePolicy{}, false
	}
	var p types.ProbePolicy
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		return types.ProbePolicy{}, false
	}
	if p.Mode == "" {
		p.Mode = types.PolicyDefault
	}
	return p, true
}

// killSwitchActive reads SNITCH_PROBE_DISABLED. Any non-empty value
// activates the kill switch. Returns (reason, true) when active.
func killSwitchActive() (string, bool) {
	v := os.Getenv("SNITCH_PROBE_DISABLED")
	if v == "" {
		return "", false
	}
	return "SNITCH_PROBE_DISABLED=" + v, true
}

func defaultPolicy() types.ProbePolicy {
	return types.ProbePolicy{
		Mode:  types.PolicyDefault,
		SetBy: "fallback-default",
		SetAt: time.Now(),
	}
}

// AppliedPolicy returns the policy with expiry-handling applied: if
// the policy has ExpiresAt set and it's in the past, this returns
// PolicyDefault instead. Pure function; tests can call it directly.
func AppliedPolicy(p types.ProbePolicy, now time.Time) types.ProbePolicy {
	if !p.ExpiresAt.IsZero() && now.After(p.ExpiresAt) {
		return defaultPolicy()
	}
	return p
}

// PolicyAllowsRelayProbe / PolicyAllowsTLSProbe / PolicyAllowsMTU /
// PolicyAllowsDialerCounters / PolicyAllowsPeerPing decide whether
// each sub-probe runs under the given policy mode. Pure functions;
// the cmd-side switches probe options based on these.
//
// Mode → sub-probe table:
//
//   off              : nothing runs (heartbeat only)
//   minimal          : peer ping + ss-counts (transport assertion via ss is free; classify only)
//   default          : everything in the do-no-harm config (TLS cached 1h, MTU iface-change, etc.)
//   focused-research : everything, but TLS cache is bypassed and MTU re-runs every cycle

func PolicyAllowsPeerPing(mode string) bool {
	return mode != types.PolicyOff
}

func PolicyAllowsRelayProbe(mode string) bool {
	return mode == types.PolicyDefault ||
		mode == types.PolicyFocusedResearch ||
		mode == types.PolicyMinimal // ss-only relay probe is free
}

func PolicyAllowsTLSProbe(mode string) bool {
	return mode == types.PolicyDefault || mode == types.PolicyFocusedResearch
}

func PolicyAllowsMTU(mode string) bool {
	return mode == types.PolicyDefault || mode == types.PolicyFocusedResearch
}

func PolicyAllowsDialerCounters(mode string) bool {
	return mode == types.PolicyDefault || mode == types.PolicyFocusedResearch
}

// PolicyForcesUncached returns true when the policy demands fresh
// probes regardless of caching. Only "focused-research" sets this —
// it's a deliberately heavier mode meant for time-bounded windows.
func PolicyForcesUncached(mode string) bool {
	return mode == types.PolicyFocusedResearch
}
