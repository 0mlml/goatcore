// Package measure — path MTU probe.
//
// Background. The review flagged MTU as a missing dimension
// in snitch. Path-MTU mismatches silently degrade throughput without
// triggering latency / loss alarms (TCP works fine; goodput just drops
// by however many bytes-per-packet the path costs). Common causes:
// CGNAT links advertising 1280, MPLS underlays adding tunnel headers,
// VPN-over-VPN stacking on Satlink-like substrates.
//
// Strategy: send `ping -M do -s <N>` with N starting at 1472 (Ethernet
// MTU minus 28 for IP+ICMP headers) and walk down. The smallest N that
// returns OK is the path MTU minus 28. We also test 1252 (WireGuard's
// default safe payload — 1280 minus 28).
//
// One-shot: this is called once at probe boot. MTU doesn't fluctuate
// between probe cycles unless the underlying interface flips.

package measure

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/usareur-af/goatcore/ops/snitch/internal/types"
)

// pingReceivedRE matches the received-count across iputils ("1 received,"),
// BSD/macOS ("1 packets received,"), and Windows ("Received = 1") outputs.
// Captures the integer count.
var pingReceivedRE = regexp.MustCompile(`(?:^|\s)(\d+)\s+(?:packets\s+)?received[,\s]|Received\s*=\s*(\d+)`)

// PingPayloadProber sends a non-fragmenting ping with the given payload
// size and returns whether it succeeded. Tests inject a stub.
//
// Note: the payload size passed is the ICMP DATA size, not the IP-level
// packet size. ping's `-s N` sets data size; total IP packet ≈ N+28.
type PingPayloadProber func(ctx context.Context, target string, dataSize int) (bool, error)

// MTUProbeConfig wires the dependency-inject hooks.
type MTUProbeConfig struct {
	PingFn    PingPayloadProber
	Interface string
	Target    string // host to ping (e.g. the gateway)
}

// DefaultMTUProbeConfig wires production runners.
func DefaultMTUProbeConfig(target, iface string) MTUProbeConfig {
	return MTUProbeConfig{
		PingFn:    pingPayloadDontFragment,
		Interface: iface,
		Target:    target,
	}
}

// PathMTUFromPayload converts a successful ICMP DATA size to its
// implied path MTU (data + ICMP header 8 + IPv4 header 20 = +28).
// Pure function for tests.
func PathMTUFromPayload(dataSize int) int {
	return dataSize + 28
}

// ProbeMTU runs the MTU probe and returns the result. Strategy:
// test 1472 (full Ethernet, 1500 MTU) and 1252 (WG-floor, 1280 MTU)
// independently; set OK1500/OK1280 accordingly. PathMTU = derived
// from the highest-passing payload.
//
// We deliberately don't binary-search the full range — two well-known
// thresholds give us 95% of the operational signal at 2x the cost.
// If both fail, PathMTU=0 and operators should investigate.
func ProbeMTU(ctx context.Context, cfg MTUProbeConfig) types.MTUInfo {
	out := types.MTUInfo{Interface: cfg.Interface}
	if cfg.Target == "" || cfg.PingFn == nil {
		return out
	}
	if ok, _ := cfg.PingFn(ctx, cfg.Target, 1472); ok { //nolint:errcheck // probe errors mean this threshold did not pass
		out.OK1500 = true
		out.PathMTU = PathMTUFromPayload(1472) // 1500
	}
	if ok, _ := cfg.PingFn(ctx, cfg.Target, 1252); ok { //nolint:errcheck // probe errors mean this threshold did not pass
		out.OK1280 = true
		if out.PathMTU == 0 { // didn't already set 1500
			out.PathMTU = PathMTUFromPayload(1252) // 1280
		}
	}
	return out
}

// MTUProberCachingByInterface wraps an MTU prober and only re-runs the
// underlying probe on interface change. This is the do-no-harm shape
// of the MTU probe (review): MTU correlates with interface
// flips, not with the 60s probe loop. Per-cycle re-probing was sending
// a DF=on ping to the gateway every cycle, which is small but real
// fleet-wide cost on cellular / Satlink uplinks.
//
// The returned closure is goroutine-safe (mutex-protected) so it can
// be passed via WithMTUProbe and used across cycles in any probe
// concurrency model.
func MTUProberCachingByInterface(target, _iface string) func(ctx context.Context, target, iface string) types.MTUInfo {
	var mu sync.Mutex
	var lastIface string
	var lastResult types.MTUInfo
	hasResult := false

	return func(ctx context.Context, target, iface string) types.MTUInfo {
		mu.Lock()
		defer mu.Unlock()
		if hasResult && iface == lastIface {
			return lastResult
		}
		lastResult = ProbeMTU(ctx, DefaultMTUProbeConfig(target, iface))
		lastIface = iface
		hasResult = true
		return lastResult
	}
}

// pingPayloadDontFragment shells out to ping with the platform-appropriate
// don't-fragment flag and a single packet of the given payload size.
// Returns true if at least one reply came back.
//
// Per-OS ping flags:
//   - Linux iputils: -M do (set DF), -s <bytes> (data size), -c 1, -W 2 (sec)
//   - macOS / BSD:   -D (set DF), -s <bytes>, -c 1, -W 2000 (ms)
//   - Windows:       -f (set DF), -l <bytes>, -n 1, -w 2000 (ms)
//
// Block 61H (multi-platform builds): pre-Block-61H this was Linux-only.
// macOS + Windows variants land alongside the broader cross-compile push.
func pingPayloadDontFragment(ctx context.Context, target string, dataSize int) (bool, error) {
	// gosec G204 false-positive: `target` is a relay hostname or IP from
	// our own management.json (operator-controlled, not a network input);
	// `dataSize` is an int from the Block 61 MTU search loop bounded
	// to [576, 9000]. Both pass as separate argv to ping; no shell.
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(ctx, "ping", "-c", "1", "-W", "2000", "-D", "-s", strconv.Itoa(dataSize), target) //nolint:gosec // see comment above
	case "windows":
		cmd = exec.CommandContext(ctx, "ping", "-n", "1", "-w", "2000", "-f", "-l", strconv.Itoa(dataSize), target) //nolint:gosec // see comment above
	default:
		cmd = exec.CommandContext(ctx, "ping", "-c", "1", "-W", "2", "-M", "do", "-s", strconv.Itoa(dataSize), target) //nolint:gosec // see comment above
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		// nilerr intentional: non-zero ping exit = the DF-set probe payload
		// did NOT pass at this size. That's a verdict ("MTU too small for
		// this probe"), not an internal error to propagate.
		return false, nil //nolint:nilerr // see comment above
	}
	output := string(out)

	// Windows reports MTU failure as "Packet needs to be fragmented but DF
	// set." Treat that text as a definitive negative even if exit was 0.
	if runtime.GOOS == "windows" && strings.Contains(output, "needs to be fragmented") {
		return false, nil
	}

	// Cross-platform: match "<N> received,", "<N> packets received,", or
	// "Received = <N>" via pingReceivedRE.
	for _, m := range pingReceivedRE.FindAllStringSubmatch(output, -1) {
		var raw string
		if m[1] != "" {
			raw = m[1]
		} else {
			raw = m[2]
		}
		if n, perr := strconv.Atoi(raw); perr == nil && n > 0 {
			return true, nil
		}
	}
	// Some macOS-Sequoia ping summaries put the line break differently;
	// fall back to a simple scan: any line containing "received" followed
	// somewhere by a positive integer wins.
	for _, line := range strings.Split(output, "\n") {
		if !strings.Contains(line, "received") {
			continue
		}
		for _, f := range strings.Fields(line) {
			if n, perr := strconv.Atoi(strings.TrimRight(f, ",")); perr == nil && n > 0 {
				return true, nil
			}
		}
	}
	return false, fmt.Errorf("ping output did not contain a positive received-count")
}
