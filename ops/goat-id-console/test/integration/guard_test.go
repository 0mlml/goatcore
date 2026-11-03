package integration

import (
	"os"
	"testing"
)

func makeVars(url string) map[string]string {
	return map[string]string{
		"IDP_TENANT_KRATOS_ADMIN_URL":   url,
		"IDP_TENANT_HYDRA_ADMIN_URL":    url,
		"IDP_OPERATOR_KRATOS_ADMIN_URL": url,
		"IDP_OPERATOR_HYDRA_ADMIN_URL":  url,
	}
}

func TestRequireLoopback_AllowsLoopbackHosts(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		url  string
	}{
		{"ipv4_localhost", "http://127.0.0.1:4434"},
		{"ipv4_127_8", "http://127.10.1.1:4434"},
		{"ipv6_loopback", "http://[::1]:4434"},
		{"localhost", "http://localhost:4434"},
	}

	for _, tc := range cases {
		ct := tc
		t.Run(ct.name, func(t *testing.T) {
			t.Parallel()

			vars := makeVars(ct.url)

			if err := RequireLoopback(vars); err != nil {
				t.Fatalf("RequireLoopback(%q) unexpected error: %v", ct.url, err)
			}
		})
	}
}

func TestRequireLoopback_RejectsExternalHosts(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		url  string
	}{
		{"hostname_90at", "https://goat.example:4434"},
		{"hostname_zagros_backbone", "https://zagros.example"},
	}

	for _, tc := range cases {
		ct := tc
		t.Run(ct.name, func(t *testing.T) {
			t.Parallel()

			vars := makeVars(ct.url)

			if err := RequireLoopback(vars); err == nil {
				t.Fatalf("RequireLoopback(%q) expected error, got nil", ct.url)
			}
		})
	}
}

func TestRequireLoopback_RejectsSchemelessOrEmptyHost(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		url  string
	}{
		{"schemeless", "127.0.0.1:4434"},
		{"empty_host", "http:///admin"},
	}

	for _, tc := range cases {
		ct := tc
		t.Run(ct.name, func(t *testing.T) {
			t.Parallel()

			vars := makeVars(ct.url)

			if err := RequireLoopback(vars); err == nil {
				t.Fatalf("RequireLoopback(%q) expected error, got nil", ct.url)
			}
		})
	}
}

func TestRequireLoopback_EmptyValueIsRefused(t *testing.T) {
	t.Parallel()

	vars := map[string]string{
		"IDP_TENANT_KRATOS_ADMIN_URL": "http://127.0.0.1:4434",
		"IDP_TENANT_HYDRA_ADMIN_URL":  "",
	}

	if err := RequireLoopback(vars); err == nil {
		t.Fatalf("RequireLoopback with an empty value expected error, got nil")
	}
}

// A guard handed nothing must refuse. Iterating zero variables and returning
// nil is indistinguishable from having checked something, which is precisely
// how a safety boundary becomes decorative.
func TestRequireLoopback_EmptyMapIsRefused(t *testing.T) {
	t.Parallel()

	if err := RequireLoopback(map[string]string{}); err == nil {
		t.Fatalf("RequireLoopback({}) expected error, got nil")
	}
}

// RequireLoopback must validate the keys it is GIVEN, not a hardcoded list.
// Regression lock: an earlier version ignored its argument entirely, so an
// endpoint passed under any other name went unchecked.
func TestRequireLoopback_ValidatesSuppliedKeysNotAFixedList(t *testing.T) {
	t.Parallel()

	err := RequireLoopback(map[string]string{
		"KRATOS_PUBLIC_URL": "https://auth.zagros.example",
	})
	if err == nil {
		t.Fatalf("RequireLoopback ignored a non-loopback endpoint passed under an unlisted name")
	}
}

func TestRequireLoopbackEnv_RefusesWhenUnset(t *testing.T) {
	for _, k := range requiredAdminURLVars {
		t.Setenv(k, "")
	}
	if err := RequireLoopbackEnv(); err == nil {
		t.Fatalf("RequireLoopbackEnv with unset vars expected error, got nil")
	}
}

// TestRequireLoopbackEnvConfig wires the safety guard up to the process
// environment, so the CI pre-flight step enforces that the configured admin
// URLs are loopback-only. When the expected env vars are not all present we
// skip, since the harness that sets them lives in CI.
func TestRequireLoopbackEnvConfig(t *testing.T) {
	t.Parallel()

	for _, key := range requiredAdminURLVars {
		if os.Getenv(key) == "" {
			t.Skip("admin URL env vars not all set; env-driven guard runs only in CI")
		}
	}

	if err := RequireLoopbackEnv(); err != nil {
		t.Fatalf("RequireLoopbackEnv() error: %v", err)
	}
}
