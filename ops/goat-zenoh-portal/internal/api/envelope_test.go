package api_test

import (
	"net/http"
	"testing"
)

// envelopeFor finds the envelope row for an anchor in GET /api/envelopes, or
// nil if absent.
func envelopeFor(t *testing.T, c *http.Client, baseURL, anchor string) map[string]any {
	t.Helper()
	resp := doJSON(t, c, "GET", baseURL+"/api/envelopes", "dev-operator-key", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list envelopes: status=%d body=%s", resp.StatusCode, mustReadBody(t, resp))
	}
	body := mustReadJSON(t, resp)
	rows, _ := body["envelopes"].([]any)
	for _, r := range rows {
		m, _ := r.(map[string]any)
		if a, _ := m["anchor"].(string); a == anchor {
			return m
		}
	}
	return nil
}

// TestEnvelope_DefaultAssignedAtRegistration locks ADR 1045 §3: every actor
// gets a rough class-baseline default envelope at registration, with no
// envelope supplied — the data always exists, sourced "default", and it never
// gated the registration (which returned 201).
func TestEnvelope_DefaultAssignedAtRegistration(t *testing.T) {
	ts := newTestServer(t)
	c := &http.Client{}

	resp := doJSON(t, c, "POST", ts.URL+"/api/vendors", "dev-operator-key",
		map[string]any{"prefix": "acme"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register: status=%d body=%s", resp.StatusCode, mustReadBody(t, resp))
	}

	env := envelopeFor(t, c, ts.URL, "acme")
	if env == nil {
		t.Fatal("no default envelope assigned at registration")
	}
	if src, _ := env["source"].(string); src != "default" {
		t.Errorf("default envelope source=%q, want default", src)
	}
	if ck, _ := env["class_key"].(string); ck != "C2" {
		t.Errorf("default envelope class_key=%q, want C2 (the default class)", ck)
	}
	// The C2 baseline ascends and is non-trivial.
	maxM, _ := env["max_msgs_per_s"].(float64)
	minM, _ := env["min_msgs_per_s"].(float64)
	if !(maxM > minM) {
		t.Errorf("default envelope msgs/s not ascending: min=%v max=%v", minM, maxM)
	}
}

// TestEnvelope_OwningVendorEdits locks the edit path: the registering party
// (the vendor's own Bearer token) may PUT its envelope; it stores as "edited"
// with the supplied numbers and is reflected in the list.
func TestEnvelope_OwningVendorEdits(t *testing.T) {
	ts := newTestServer(t)
	c := &http.Client{}

	resp := doJSON(t, c, "POST", ts.URL+"/api/vendors", "dev-operator-key",
		map[string]any{"prefix": "acme"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register: %d %s", resp.StatusCode, mustReadBody(t, resp))
	}
	vendorKey, _ := mustReadJSON(t, resp)["api_key"].(string)

	edit := map[string]any{
		"min_msgs_per_s": 1, "mean_msgs_per_s": 4, "high_msgs_per_s": 10, "max_msgs_per_s": 25,
		"min_bytes_per_s": 100, "mean_bytes_per_s": 1000, "high_bytes_per_s": 5000, "max_bytes_per_s": 20000,
		"class_key": "C1",
	}
	r := doJSON(t, c, "PUT", ts.URL+"/api/vendors/acme/envelope", vendorKey, edit)
	if r.StatusCode != http.StatusOK {
		t.Fatalf("vendor edit own envelope: status=%d body=%s", r.StatusCode, mustReadBody(t, r))
	}

	env := envelopeFor(t, c, ts.URL, "acme")
	if src, _ := env["source"].(string); src != "edited" {
		t.Errorf("after edit source=%q, want edited", src)
	}
	if got, _ := env["max_msgs_per_s"].(float64); got != 25 {
		t.Errorf("after edit max_msgs_per_s=%v, want 25", got)
	}
	if got, _ := env["class_key"].(string); got != "C1" {
		t.Errorf("after edit class_key=%q, want C1", got)
	}
}

// TestEnvelope_NonAscendingRejected — the band invariant (min ≤ mean ≤ high ≤
// max per axis) is enforced; a descending edit is a 400, not silently stored.
func TestEnvelope_NonAscendingRejected(t *testing.T) {
	ts := newTestServer(t)
	c := &http.Client{}
	doJSON(t, c, "POST", ts.URL+"/api/vendors", "dev-operator-key", map[string]any{"prefix": "acme"})

	bad := map[string]any{
		"min_msgs_per_s": 100, "mean_msgs_per_s": 4, "high_msgs_per_s": 10, "max_msgs_per_s": 25,
		"min_bytes_per_s": 0, "mean_bytes_per_s": 1000, "high_bytes_per_s": 5000, "max_bytes_per_s": 20000,
	}
	r := doJSON(t, c, "PUT", ts.URL+"/api/vendors/acme/envelope", "dev-operator-key", bad)
	if r.StatusCode != http.StatusBadRequest {
		t.Fatalf("non-ascending edit: status=%d, want 400 (body=%s)", r.StatusCode, mustReadBody(t, r))
	}
}

// TestEnvelope_MinZeroValid — an event-driven/bursty actor declares min = 0 so
// correct silence never trips the LOW band downstream; min = 0 must be accepted.
func TestEnvelope_MinZeroValid(t *testing.T) {
	ts := newTestServer(t)
	c := &http.Client{}
	doJSON(t, c, "POST", ts.URL+"/api/vendors", "dev-operator-key", map[string]any{"prefix": "bursty"})

	ev := map[string]any{
		"min_msgs_per_s": 0, "mean_msgs_per_s": 0, "high_msgs_per_s": 50, "max_msgs_per_s": 500,
		"min_bytes_per_s": 0, "mean_bytes_per_s": 0, "high_bytes_per_s": 5000, "max_bytes_per_s": 50000,
	}
	r := doJSON(t, c, "PUT", ts.URL+"/api/vendors/bursty/envelope", "dev-operator-key", ev)
	if r.StatusCode != http.StatusOK {
		t.Fatalf("min=0 edit: status=%d, want 200 (body=%s)", r.StatusCode, mustReadBody(t, r))
	}
}

// TestEnvelope_CrossVendorEditRejected — the edit is scoped: vendor A's Bearer
// token cannot edit vendor B's envelope (only B or the operator can).
func TestEnvelope_CrossVendorEditRejected(t *testing.T) {
	ts := newTestServer(t)
	c := &http.Client{}

	doJSON(t, c, "POST", ts.URL+"/api/vendors", "dev-operator-key", map[string]any{"prefix": "alpha"})
	rb := doJSON(t, c, "POST", ts.URL+"/api/vendors", "dev-operator-key", map[string]any{"prefix": "beta"})
	betaKey, _ := mustReadJSON(t, rb)["api_key"].(string)

	edit := map[string]any{
		"min_msgs_per_s": 1, "mean_msgs_per_s": 2, "high_msgs_per_s": 3, "max_msgs_per_s": 4,
		"min_bytes_per_s": 1, "mean_bytes_per_s": 2, "high_bytes_per_s": 3, "max_bytes_per_s": 4,
	}
	// beta's key editing alpha's envelope must be rejected.
	if r := doJSON(t, c, "PUT", ts.URL+"/api/vendors/alpha/envelope", betaKey, edit); r.StatusCode == http.StatusOK {
		t.Errorf("cross-vendor edit succeeded; want scoped rejection")
	}
	// operator can edit anyone's.
	if r := doJSON(t, c, "PUT", ts.URL+"/api/vendors/alpha/envelope", "dev-operator-key", edit); r.StatusCode != http.StatusOK {
		t.Errorf("operator edit alpha envelope: status=%d, want 200", r.StatusCode)
	}
}

// TestEnvelope_UnknownAnchor404 — editing an envelope for an actor that does
// not exist is a 404 (an envelope is never declared for a phantom anchor).
func TestEnvelope_UnknownAnchor404(t *testing.T) {
	ts := newTestServer(t)
	c := &http.Client{}
	edit := map[string]any{
		"min_msgs_per_s": 1, "mean_msgs_per_s": 2, "high_msgs_per_s": 3, "max_msgs_per_s": 4,
		"min_bytes_per_s": 1, "mean_bytes_per_s": 2, "high_bytes_per_s": 3, "max_bytes_per_s": 4,
	}
	if r := doJSON(t, c, "PUT", ts.URL+"/api/vendors/ghost/envelope", "dev-operator-key", edit); r.StatusCode != http.StatusNotFound {
		t.Fatalf("edit unknown anchor: status=%d, want 404 (body=%s)", r.StatusCode, mustReadBody(t, r))
	}
}

// TestEnvelope_SuppliedAtRegistration — a valid envelope block in the
// registration payload is stored as the party's edited declaration instead of
// the default.
func TestEnvelope_SuppliedAtRegistration(t *testing.T) {
	ts := newTestServer(t)
	c := &http.Client{}

	resp := doJSON(t, c, "POST", ts.URL+"/api/vendors", "dev-operator-key", map[string]any{
		"prefix": "acme",
		"envelope": map[string]any{
			"min_msgs_per_s": 2, "mean_msgs_per_s": 8, "high_msgs_per_s": 16, "max_msgs_per_s": 64,
			"min_bytes_per_s": 200, "mean_bytes_per_s": 2000, "high_bytes_per_s": 8000, "max_bytes_per_s": 64000,
			"class_key": "C3",
		},
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register with envelope: %d %s", resp.StatusCode, mustReadBody(t, resp))
	}
	env := envelopeFor(t, c, ts.URL, "acme")
	if src, _ := env["source"].(string); src != "edited" {
		t.Errorf("supplied envelope source=%q, want edited", src)
	}
	if got, _ := env["max_msgs_per_s"].(float64); got != 64 {
		t.Errorf("supplied envelope max_msgs_per_s=%v, want 64", got)
	}
}
