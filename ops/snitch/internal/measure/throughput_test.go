package measure

import (
	"context"
	"errors"
	"testing"
)

const sampleIperf3JSON = `{
  "start": {},
  "intervals": [],
  "end": {
    "sum_received": {
      "bits_per_second": 720500000.0
    }
  }
}`

func TestParseIperf3JSON(t *testing.T) {
	mbps, err := ParseIperf3JSON([]byte(sampleIperf3JSON))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mbps < 720.4 || mbps > 720.6 {
		t.Errorf("ParseIperf3JSON Mbps = %.1f; want ~720.5", mbps)
	}
}

func TestParseIperf3JSON_InvalidJSON(t *testing.T) {
	_, err := ParseIperf3JSON([]byte("not json"))
	if err == nil {
		t.Error("expected error on non-JSON")
	}
}

func TestRunThroughputProbe_HappyPath(t *testing.T) {
	runner := func(_ context.Context, _ string, _, _, streams int) ([]byte, error) {
		// Pretend single-flow gets less than multi-flow.
		if streams == 1 {
			return []byte(`{"end":{"sum_received":{"bits_per_second":100000000}}}`), nil
		}
		return []byte(`{"end":{"sum_received":{"bits_per_second":400000000}}}`), nil
	}
	out := RunThroughputProbe(context.Background(), "10.0.0.1", 5201, runner)
	if out.SingleFlowMbps < 99 || out.SingleFlowMbps > 101 {
		t.Errorf("SingleFlowMbps = %.1f; want ~100", out.SingleFlowMbps)
	}
	if out.MultiFlowMbps < 399 || out.MultiFlowMbps > 401 {
		t.Errorf("MultiFlowMbps = %.1f; want ~400", out.MultiFlowMbps)
	}
	if out.Error != "" {
		t.Errorf("Error should be empty, got %q", out.Error)
	}
}

func TestRunThroughputProbe_SingleFlowFailure(t *testing.T) {
	runner := func(_ context.Context, _ string, _, _, _ int) ([]byte, error) {
		return nil, errors.New("connection refused")
	}
	out := RunThroughputProbe(context.Background(), "10.0.0.1", 5201, runner)
	if out.Error == "" {
		t.Error("expected Error to be set on runner failure")
	}
	if out.SingleFlowMbps != 0 || out.MultiFlowMbps != 0 {
		t.Errorf("failed probe should report zero Mbps; got single=%.1f multi=%.1f",
			out.SingleFlowMbps, out.MultiFlowMbps)
	}
}
