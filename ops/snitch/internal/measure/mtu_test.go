package measure

import (
	"context"
	"testing"
)

func TestPathMTUFromPayload(t *testing.T) {
	tests := []struct {
		dataSize int
		wantMTU  int
	}{
		{1472, 1500},
		{1252, 1280},
		{1000, 1028},
		{0, 28},
	}
	for _, tt := range tests {
		if got := PathMTUFromPayload(tt.dataSize); got != tt.wantMTU {
			t.Errorf("PathMTUFromPayload(%d) = %d; want %d", tt.dataSize, got, tt.wantMTU)
		}
	}
}

// stubMTUPinger returns true for payload sizes <= maxOK, false otherwise.
// This models a path with MTU = maxOK + 28. Distinct from probe_test.go's
// stubPing (which is a PingFunc-shaped latency stub).
type stubMTUPinger struct {
	maxOK int
}

func (s *stubMTUPinger) ping(_ context.Context, _ string, dataSize int) (bool, error) {
	return dataSize <= s.maxOK, nil
}

func TestProbeMTU(t *testing.T) {
	tests := []struct {
		name        string
		maxOK       int
		want1500    bool
		want1280    bool
		wantPathMTU int
	}{
		{"full-ethernet", 1472, true, true, 1500},
		{"wg-floor-only", 1252, false, true, 1280},
		{"tiny-mtu", 500, false, false, 0},
		{"between-1280-and-1500", 1300, false, true, 1280},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := &stubMTUPinger{maxOK: tt.maxOK}
			cfg := MTUProbeConfig{
				PingFn:    stub.ping,
				Interface: "eth0",
				Target:    "10.0.0.1",
			}
			out := ProbeMTU(context.Background(), cfg)
			if out.OK1500 != tt.want1500 {
				t.Errorf("OK1500 = %v; want %v", out.OK1500, tt.want1500)
			}
			if out.OK1280 != tt.want1280 {
				t.Errorf("OK1280 = %v; want %v", out.OK1280, tt.want1280)
			}
			if out.PathMTU != tt.wantPathMTU {
				t.Errorf("PathMTU = %d; want %d", out.PathMTU, tt.wantPathMTU)
			}
		})
	}
}

// TestMTUProberCachingByInterface asserts the do-no-harm posture: the
// caching closure only re-runs the underlying probe on interface
// change, not every cycle. MTU-correlates-with-interface-flips, not
// with the 60s loop. This is the per-cycle DF=on ping cost we're
// avoiding on cellular / Satlink uplinks.
func TestMTUProberCachingByInterface(t *testing.T) {
	prober := MTUProberCachingByInterface("", "")

	// First call on eth0 — cache miss, real probe runs (against a
	// non-routable target so the underlying ping fails fast; we're
	// checking caching, not MTU values).
	r1 := prober(context.Background(), "192.0.2.1", "eth0")
	r2 := prober(context.Background(), "192.0.2.1", "eth0")
	// Both results should be identical (cache hit on second call).
	if r1 != r2 {
		t.Errorf("MTU cache hit should return identical result; got r1=%+v r2=%+v", r1, r2)
	}

	// Interface change → cache miss, fresh probe.
	r3 := prober(context.Background(), "192.0.2.1", "wlan0")
	if r3.Interface != "wlan0" {
		t.Errorf("after interface flip, Interface = %q; want wlan0", r3.Interface)
	}
}

func TestProbeMTU_NilOrEmpty(t *testing.T) {
	t.Run("no-target", func(t *testing.T) {
		cfg := MTUProbeConfig{PingFn: (&stubMTUPinger{maxOK: 1472}).ping}
		out := ProbeMTU(context.Background(), cfg)
		if out.OK1500 || out.OK1280 || out.PathMTU != 0 {
			t.Errorf("empty target should yield zero result; got %+v", out)
		}
	})
	t.Run("nil-ping-fn", func(t *testing.T) {
		cfg := MTUProbeConfig{Target: "10.0.0.1"}
		out := ProbeMTU(context.Background(), cfg)
		if out.OK1500 || out.OK1280 {
			t.Errorf("nil PingFn should yield zero result; got %+v", out)
		}
	})
}
