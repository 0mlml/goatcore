// Package measure — daily throughput sample probe.
//
// Background. The review (deliverable A.5) called out the
// gap between snitch's per-cycle latency / loss measurements and the
// goat-relay tier-4b bench harness: snitch had no idea what end-to-end
// goodput a peer pair achieves, so a fleet-wide regression like F-040
// (placement variance) or F-041 (silent WS-instead-of-QUIC) couldn't
// be caught from the snitch dashboard.
//
// Strategy. Once-per-day per-region sentinel pair: 10s × 1 flow +
// 10s × 4 flow iperf3 ramp. One result row per day per region. The
// minimal signal that catches "fleet went sideways" without burning
// fleet bandwidth on every probe cycle.
//
// Scope (intentionally small):
//   - Opt-in via systemd timer / cron — never runs from the 60s probe
//     loop. The 60s loop is for "is the mesh healthy now"; this is
//     for "is the mesh's *capacity* drifting."
//   - One target endpoint per probe run. The ansible deployment picks
//     the sentinel pair at config time.
//   - Output: JSONL to stdout, plus a POST to the collector if
//     configured. Storage layer wires up in a follow-up — the
//     instrumentation is the hard part; persisting the JSON is trivial
//     once the dashboard has a place to display it.

package measure

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"time"
)

// ThroughputSample is one daily-throughput probe result.
type ThroughputSample struct {
	Timestamp      time.Time `json:"timestamp"`
	TargetHost     string    `json:"target_host"`
	TargetPort     int       `json:"target_port"`
	SingleFlowMbps float64   `json:"single_flow_mbps"`
	MultiFlowMbps  float64   `json:"multi_flow_mbps"`
	Streams        int       `json:"streams"`
	DurationS      int       `json:"duration_s"`
	Error          string    `json:"error,omitempty"`
}

// IperfRunner shells out to iperf3 -c <host> -p <port> -t <s> -P <streams> -J.
// Tests inject a stub that returns canned JSON.
type IperfRunner func(ctx context.Context, host string, port, durationS, streams int) ([]byte, error)

// DefaultIperfRunner runs the production iperf3 binary.
func DefaultIperfRunner() IperfRunner { return runIperf3 }

// RunThroughputProbe executes the 1-flow + 4-flow ramp and returns
// the sample. Errors are encoded into the sample's Error field rather
// than returned — the cron writer wants ALL runs (including failures)
// in the JSONL stream so dashboards can show probe-failure events.
func RunThroughputProbe(ctx context.Context, host string, port int, runner IperfRunner) ThroughputSample {
	out := ThroughputSample{
		Timestamp:  time.Now().UTC(),
		TargetHost: host,
		TargetPort: port,
		DurationS:  10,
		Streams:    4,
	}
	if runner == nil {
		runner = DefaultIperfRunner()
	}
	if mbps, err := runIperfStep(ctx, runner, host, port, 10, 1); err != nil {
		out.Error = "single-flow: " + err.Error()
		return out
	} else {
		out.SingleFlowMbps = mbps
	}
	if mbps, err := runIperfStep(ctx, runner, host, port, 10, 4); err != nil {
		out.Error = "multi-flow: " + err.Error()
		return out
	} else {
		out.MultiFlowMbps = mbps
	}
	return out
}

// ParseIperf3JSON pulls the aggregate sum-received Mbps from an
// iperf3 -J payload. Pure function for tests.
func ParseIperf3JSON(payload []byte) (float64, error) {
	var doc struct {
		End struct {
			SumReceived struct {
				BitsPerSecond float64 `json:"bits_per_second"`
			} `json:"sum_received"`
		} `json:"end"`
	}
	if err := json.Unmarshal(payload, &doc); err != nil {
		return 0, fmt.Errorf("iperf3 JSON parse: %w", err)
	}
	return doc.End.SumReceived.BitsPerSecond / 1_000_000, nil
}

func runIperfStep(ctx context.Context, runner IperfRunner, host string, port, durationS, streams int) (float64, error) {
	payload, err := runner(ctx, host, port, durationS, streams)
	if err != nil {
		return 0, err
	}
	return ParseIperf3JSON(payload)
}

func runIperf3(ctx context.Context, host string, port, durationS, streams int) ([]byte, error) {
	args := []string{
		"-c", host,
		"-p", strconv.Itoa(port),
		"-t", strconv.Itoa(durationS),
		"-P", strconv.Itoa(streams),
		"-J",
	}
	// gosec G204 false-positive: every element of args above is built
	// from operator-controlled iperf3 flags + numeric ints + a relay
	// host from management.json. No shell.
	cmd := exec.CommandContext(ctx, "iperf3", args...) //nolint:gosec // see comment above
	return cmd.Output()
}
