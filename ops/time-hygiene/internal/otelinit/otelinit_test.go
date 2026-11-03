package otelinit

import (
	"context"
	"os"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestHTTPOptionsFromEndpoint(t *testing.T) {
	cases := []struct {
		name     string
		endpoint string
		wantErr  bool
		wantOpts int
	}{
		{"https-no-path", "https://collector:4318", false, 1},
		{"http-explicit-path", "http://10.0.0.1:4318/v1/traces", false, 3},
		{"http-root-path", "http://collector:4318/", false, 2},
		{"missing-host", "://nope", true, 0},
		// Bare host:port parses with empty Host — reject so callers see a
		// clear error rather than a silently-broken exporter.
		{"missing-scheme", "collector:4318", true, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts, err := HTTPOptionsFromEndpoint(tc.endpoint)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err=%v, wantErr=%v", err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}
			if len(opts) != tc.wantOpts {
				t.Errorf("got %d opts, want %d", len(opts), tc.wantOpts)
			}
		})
	}
}

func TestBuildResource(t *testing.T) {
	t.Setenv("DBB_SITE_ID", "vega")
	t.Setenv("DBB_SITE_ROLE", "mgmt")

	res := BuildResource(Opts{
		ServiceName:    "ntp-health-agent",
		ServiceVersion: "v0.1.0",
		Extra: []attribute.KeyValue{
			attribute.String("dbb.listener", ":9753"),
		},
	})
	if res == nil {
		t.Fatal("BuildResource returned nil")
	}
	got := map[string]string{}
	for _, kv := range res.Attributes() {
		got[string(kv.Key)] = kv.Value.AsString()
	}
	want := map[string]string{
		"service.name":      "ntp-health-agent",
		"service.version":   "v0.1.0",
		"service.namespace": "vega",
		"dbb.site_role":     "mgmt",
		"dbb.listener":      ":9753",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("attr %q = %q, want %q", k, got[k], v)
		}
	}
	if got["service.instance.id"] == "" {
		t.Errorf("service.instance.id should be non-empty")
	}
}

func TestBuildResource_Defaults(t *testing.T) {
	t.Setenv("DBB_SITE_ID", "")
	t.Setenv("DBB_SITE_ROLE", "")

	res := BuildResource(Opts{ServiceName: "x", ServiceVersion: "v0"})
	got := map[string]string{}
	for _, kv := range res.Attributes() {
		got[string(kv.Key)] = kv.Value.AsString()
	}
	if got["service.namespace"] != "unknown" {
		t.Errorf("default service.namespace = %q, want unknown", got["service.namespace"])
	}
	if got["dbb.site_role"] != "mgmt" {
		t.Errorf("default dbb.site_role = %q, want mgmt", got["dbb.site_role"])
	}
}

func TestInit_NoEndpoint(t *testing.T) {
	prev, hadPrev := os.LookupEnv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if err := os.Unsetenv("OTEL_EXPORTER_OTLP_ENDPOINT"); err != nil {
		t.Fatalf("unset env: %v", err)
	}
	t.Cleanup(func() {
		if hadPrev {
			_ = os.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", prev)
		}
	})
	t.Cleanup(func() { otel.SetTracerProvider(noop.NewTracerProvider()) })

	shutdown, err := Init(context.Background(), Opts{ServiceName: "x", ServiceVersion: "v0"})
	if err != nil {
		t.Fatalf("Init without endpoint should not error: %v", err)
	}
	if shutdown == nil {
		t.Fatal("Init returned nil shutdown func")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	if err := shutdown(ctx); err != nil {
		t.Errorf("shutdown returned error: %v", err)
	}
}

func TestInit_RequiresServiceName(t *testing.T) {
	if _, err := Init(context.Background(), Opts{}); err == nil {
		t.Errorf("Init with empty ServiceName should error")
	}
}

func TestInit_LiveSDKShutdown(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:1") // unreachable on purpose

	prev := otel.GetTracerProvider()
	t.Cleanup(func() { otel.SetTracerProvider(prev) })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	shutdown, err := Init(ctx, Opts{ServiceName: "x", ServiceVersion: "v0"})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	shutdownCtx, cancel2 := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel2()
	if err := shutdown(shutdownCtx); err != nil {
		// Shutdown may surface the dial error; acceptable as long as it
		// returns rather than hanging — the goleak check validates goroutine
		// cleanup independently.
		t.Logf("shutdown returned (non-fatal): %v", err)
	}
}

func TestInit_BadEndpoint(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "://nope")
	t.Cleanup(func() { otel.SetTracerProvider(noop.NewTracerProvider()) })

	_, err := Init(context.Background(), Opts{ServiceName: "x", ServiceVersion: "v0"})
	if err == nil {
		t.Errorf("Init with malformed endpoint should error")
	}
}
