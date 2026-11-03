package api_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

// The fabric-config endpoint is the read-only self-description of the
// fabric; the test harness already builds a Server with sensible fakes,
// so we just hit GET /api/fabric/config and confirm the JSON has the
// shape any partner would parse.
func TestFabricConfig_PublicReadable(t *testing.T) {
	ts := newTestServer(t)
	resp, err := http.Get(ts.URL + "/api/fabric/config")
	if err != nil {
		t.Fatalf("GET /api/fabric/config: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d want 200", resp.StatusCode)
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}

	// Spot-check structural commitments. These are the keys a partner-
	// side script would key off; renaming any of them would break the
	// (small but real) cross-fabric contract.
	for _, key := range []string{
		"fabric_name", "maturity", "router", "tls", "zenoh",
		"namespacing", "registration", "wire_formats", "onboarding_flavors",
	} {
		if _, ok := body[key]; !ok {
			t.Errorf("response missing top-level key %q", key)
		}
	}

	// Onboarding-flavors carries the two-flavor distinction PR-3 docs.
	flavors, _ := body["onboarding_flavors"].([]any)
	if len(flavors) < 2 {
		t.Errorf("expected >=2 onboarding flavors, got %d", len(flavors))
	}

	// taken_vendor_prefixes must be a list (possibly empty) so a parser
	// can iterate without nil-checks.
	ns, _ := body["namespacing"].(map[string]any)
	if _, ok := ns["taken_vendor_prefixes"]; !ok {
		t.Errorf("namespacing missing taken_vendor_prefixes")
	}
}

// Public root-CA bundle: with no configured cert path the handler must
// return 404, not 500 (a dev env without the cert is a routine state).
func TestRootCABundle_404WhenUnconfigured(t *testing.T) {
	ts := newTestServer(t)
	resp, err := http.Get(ts.URL + "/peering/root-ca.pem")
	if err != nil {
		t.Fatalf("GET /peering/root-ca.pem: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status: got %d want 404", resp.StatusCode)
	}
}
