// Self-test sub-command — Block 61H (multi-platform builds).
//
// Runs every probe step once on the local host, reports per-step
// success/failure as JSON, and exits non-zero if any required-on-this-
// platform step fails. Useful for verifying a fresh install on any
// platform without waiting a full probe cycle.
//
// Step taxonomy:
//   - "required" — the step has a working implementation on this OS and
//     is expected to succeed.
//   - "skip"     — the step is structurally unavailable on this OS
//     (e.g. journalctl on Darwin / Windows). Counted as success
//     for exit-code purposes but flagged in the JSON so operators
//     can see why a measurement is missing.
//   - "fail"     — required step did not produce a value. Triggers
//     non-zero exit.

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/usareur-af/goatcore/ops/snitch/internal/measure"
)

// selfTestStep records the outcome of one probe sub-step.
type selfTestStep struct {
	Name       string `json:"name"`
	Status     string `json:"status"`           // "ok" | "skip" | "fail"
	Reason     string `json:"reason,omitempty"` // why we skipped or failed
	Detail     any    `json:"detail,omitempty"` // step-specific data (e.g. detected SSID)
	DurationMs int    `json:"duration_ms"`
}

// selfTestReport is the top-level JSON object the self-test emits.
type selfTestReport struct {
	GOOS      string         `json:"goos"`
	GOARCH    string         `json:"goarch"`
	NumCPU    int            `json:"num_cpu"`
	Hostname  string         `json:"hostname,omitempty"`
	Steps     []selfTestStep `json:"steps"`
	Required  int            `json:"required_count"`
	Skipped   int            `json:"skipped_count"`
	Failed    int            `json:"failed_count"`
	OverallOK bool           `json:"overall_ok"`
}

// cmdSelfTest is the entry point. Wired in main.go alongside cmdProbe.
func cmdSelfTest(args []string) {
	fs := flag.NewFlagSet("self-test", flag.ExitOnError)
	timeout := fs.Duration("timeout", 30*time.Second, "wall-clock budget for the whole self-test")
	pretty := fs.Bool("pretty", true, "pretty-print JSON")
	_ = fs.Parse(args) //nolint:errcheck // ExitOnError terminates instead of returning an error

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}
	report := selfTestReport{
		GOOS:     runtime.GOOS,
		GOARCH:   runtime.GOARCH,
		NumCPU:   runtime.NumCPU(),
		Hostname: hostname,
	}

	// Step: network fingerprint (interface + gateway + SSID).
	report.Steps = append(report.Steps, runStep(ctx, "network-fingerprint", true, func(ctx context.Context) (any, error) {
		fp := measure.DetectNetwork(ctx)
		if fp.Interface == "" || fp.Interface == "unknown" {
			return fp, fmt.Errorf("could not detect default-route interface")
		}
		return fp, nil
	}))

	// Step: ICMP ping to the gateway. Required on every platform that
	// can resolve a gateway IP.
	report.Steps = append(report.Steps, runStep(ctx, "icmp-ping-gateway", true, func(ctx context.Context) (any, error) {
		fp := measure.DetectNetwork(ctx)
		if fp.GatewayIP == "" {
			return nil, fmt.Errorf("no gateway IP (skipped ping)")
		}
		res, err := measure.Ping(ctx, fp.GatewayIP, 3)
		if err != nil {
			return nil, err
		}
		return map[string]any{"target": fp.GatewayIP, "avg_ms": res.AvgMs, "loss_pct": res.LossPct}, nil
	}))

	// Step: relay-cert probe against a configured URL if SNITCH_RELAY_URLS
	// is set (otherwise skip). Doesn't try to be smart about which relay
	// we should hit — operator-supplied list only.
	report.Steps = append(report.Steps, runStep(ctx, "relay-cert-probe", false, func(ctx context.Context) (any, error) {
		urls := strings.TrimSpace(os.Getenv("SNITCH_RELAY_URLS"))
		if urls == "" {
			return nil, fmt.Errorf("SNITCH_RELAY_URLS not set")
		}
		first := strings.TrimSpace(strings.SplitN(urls, ",", 2)[0])
		if first == "" {
			return nil, fmt.Errorf("SNITCH_RELAY_URLS first entry empty")
		}
		cfg := measure.DefaultRelayProbeConfig()
		health := measure.ProbeRelay(ctx, first, cfg)
		return health, nil
	}))

	// Step: dialer-log read. Required on Linux (journalctl); skipped on
	// Darwin / Windows by construction.
	dialerRequired := runtime.GOOS == "linux"
	report.Steps = append(report.Steps, runStep(ctx, "dialer-log-read", dialerRequired, func(ctx context.Context) (any, error) {
		counters, ok := measure.CountDialerOutcomesJournalctl(ctx, 60)
		if !ok {
			if !dialerRequired {
				return nil, errStructurallySkipped("dialer-log read unavailable on this OS")
			}
			return nil, fmt.Errorf("journalctl read failed (netbird may not be running, or no permission)")
		}
		return counters, nil
	}))

	// Step: MTU probe. Best-effort on every platform.
	report.Steps = append(report.Steps, runStep(ctx, "mtu-probe", false, func(ctx context.Context) (any, error) {
		fp := measure.DetectNetwork(ctx)
		if fp.GatewayIP == "" {
			return nil, fmt.Errorf("no gateway IP (skipped MTU probe)")
		}
		mtu := measure.ProbeMTU(ctx, measure.DefaultMTUProbeConfig(fp.GatewayIP, fp.Interface))
		return mtu, nil
	}))

	// Step: netbird-status read. Skipped if netbird isn't installed —
	// the snitch self-test is also useful pre-enrollment for diagnosing
	// the host environment.
	report.Steps = append(report.Steps, runStep(ctx, "netbird-status", false, func(ctx context.Context) (any, error) {
		st, err := measure.NetbirdStatus(ctx)
		if err != nil {
			return nil, errStructurallySkipped(fmt.Sprintf("netbird status unavailable: %v", err))
		}
		return map[string]any{
			"mgmt_connected":   st.Management.Connected,
			"signal_connected": st.Signal.Connected,
			"peers_total":      st.Peers.Total,
			"relays_total":     st.Relays.Total,
		}, nil
	}))

	// Roll up.
	for _, s := range report.Steps {
		switch s.Status {
		case "ok":
			report.Required++
		case "skip":
			report.Skipped++
		case "fail":
			report.Failed++
		}
	}
	report.OverallOK = report.Failed == 0

	enc := json.NewEncoder(os.Stdout)
	if *pretty {
		enc.SetIndent("", "  ")
	}
	if err := enc.Encode(report); err != nil {
		fmt.Fprintf(os.Stderr, "self-test: encode error: %v\n", err)
		os.Exit(2)
	}
	if !report.OverallOK {
		os.Exit(1)
	}
}

// errStructurallySkipped flags a step as skipped-by-design rather than
// failed. Sentinel-checked by runStep.
type errStructurallySkipped string

func (e errStructurallySkipped) Error() string { return string(e) }

// runStep wraps one probe sub-step with a stopwatch + status classifier.
// `required` indicates whether the step is expected to succeed on this
// OS; if true and the step fails, the overall self-test exits non-zero.
func runStep(ctx context.Context, name string, required bool, fn func(ctx context.Context) (any, error)) selfTestStep {
	start := time.Now()
	detail, err := fn(ctx)
	step := selfTestStep{Name: name, DurationMs: int(time.Since(start).Milliseconds())}
	if err == nil {
		step.Status = "ok"
		step.Detail = detail
		return step
	}
	var skipped errStructurallySkipped
	switch {
	case errors.As(err, &skipped):
		step.Status = "skip"
		step.Reason = err.Error()
	case !required:
		step.Status = "skip"
		step.Reason = err.Error()
	default:
		step.Status = "fail"
		step.Reason = err.Error()
	}
	return step
}
