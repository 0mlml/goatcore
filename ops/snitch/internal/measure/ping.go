package measure

import (
	"context"
	"fmt"
	"math"
	"os/exec"
	"regexp"
	"runtime"
	"sort"
	"strconv"
)

const defaultPingCount = 5

// PingResult holds latency statistics from an ICMP ping.
type PingResult struct {
	AvgMs    float64
	MinMs    float64
	MaxMs    float64
	P50Ms    float64
	P95Ms    float64
	JitterMs float64
	LossPct  float64
	Samples  int
}

// Ping runs ICMP ping to the target IP and returns latency statistics.
func Ping(ctx context.Context, ip string, count int) (*PingResult, error) {
	if count <= 0 {
		count = defaultPingCount
	}

	// gosec G204 false-positive: `ip` is a peer IP from the netbird mesh
	// status output (operator-controlled fleet); `count` is an int from
	// the caller's ProbeOpts. Both pass as separate argv to ping; no
	// shell. fmt.Sprintf("%d", count) yields strict numeric.
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		// macOS / BSD ping: -W is per-reply timeout in milliseconds.
		cmd = exec.CommandContext(ctx, "ping", "-c", fmt.Sprintf("%d", count), "-W", "3000", ip) //nolint:gosec // see comment above
	case "windows":
		// Windows ping.exe: -n echoes, -w timeout-ms (per-reply).
		cmd = exec.CommandContext(ctx, "ping", "-n", fmt.Sprintf("%d", count), "-w", "3000", ip) //nolint:gosec // see comment above
	default:
		// Linux iputils ping: -W is per-reply timeout in seconds.
		cmd = exec.CommandContext(ctx, "ping", "-c", fmt.Sprintf("%d", count), "-W", "3", ip) //nolint:gosec // see comment above
	}
	out, err := cmd.CombinedOutput()

	// Extract per-packet RTTs regardless of exit code (partial loss still has data).
	re := regexp.MustCompile(`time[=<](\d+\.?\d*)\s*ms`)
	matches := re.FindAllStringSubmatch(string(out), -1)

	var samples []float64
	for _, m := range matches {
		v, parseErr := strconv.ParseFloat(m[1], 64)
		if parseErr == nil {
			samples = append(samples, v)
		}
	}

	if len(samples) == 0 {
		if err != nil {
			return nil, fmt.Errorf("ping %s: %w", ip, err)
		}
		return nil, fmt.Errorf("ping %s: no RTT values in output", ip)
	}

	lossPct := float64(count-len(samples)) / float64(count) * 100

	sort.Float64s(samples)
	n := len(samples)
	sum := 0.0
	for _, v := range samples {
		sum += v
	}
	avg := sum / float64(n)

	jitterSum := 0.0
	for _, v := range samples {
		jitterSum += math.Abs(v - avg)
	}
	jitter := jitterSum / float64(n)

	return &PingResult{
		AvgMs:    avg,
		MinMs:    samples[0],
		MaxMs:    samples[n-1],
		P50Ms:    percentile(samples, 50),
		P95Ms:    percentile(samples, 95),
		JitterMs: jitter,
		LossPct:  lossPct,
		Samples:  n,
	}, nil
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if len(sorted) == 1 {
		return sorted[0]
	}
	rank := (p / 100.0) * float64(len(sorted)-1)
	lower := int(math.Floor(rank))
	upper := int(math.Ceil(rank))
	if lower == upper {
		return sorted[lower]
	}
	frac := rank - float64(lower)
	return sorted[lower]*(1-frac) + sorted[upper]*frac
}
