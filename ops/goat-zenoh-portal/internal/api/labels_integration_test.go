package api_test

import (
	"net/http"
	"testing"
)

// TestLabels_SetReadDrift exercises the ADR 1075 label surface end to end: an
// unlabeled producer defaults to community, the justification gate refuses a
// restriction with no why, a justified restriction lands, and the drift counter
// reflects the non-community share.
func TestLabels_SetReadDrift(t *testing.T) {
	ts := newTestServer(t)
	c := &http.Client{}

	for _, p := range []string{"acme", "beta"} {
		resp := doJSON(t, c, "POST", ts.URL+"/api/vendors", "dev-operator-key", map[string]any{
			"prefix": p, "auto_approve": true,
		})
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("register %s: %d", p, resp.StatusCode)
		}
		resp.Body.Close()
	}

	// Unlabeled producer reads as the goatnet default (community).
	resp := doJSON(t, c, "GET", ts.URL+"/api/orgs/acme/labels", "dev-operator-key", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get labels: %d", resp.StatusCode)
	}
	got := mustReadJSON(t, resp)
	if got["effective_access"] != "community" {
		t.Errorf("unlabeled effective_access = %v, want community", got["effective_access"])
	}

	// Restriction with no justification is refused (the gate).
	resp = doJSON(t, c, "PUT", ts.URL+"/api/orgs/acme/labels", "dev-operator-key", map[string]any{
		"access": "controlled",
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("restriction without justification: got %d, want 400", resp.StatusCode)
	}
	resp.Body.Close()

	// Justified restriction lands.
	resp = doJSON(t, c, "PUT", ts.URL+"/api/orgs/acme/labels", "dev-operator-key", map[string]any{
		"fidelity": "real", "context": "operational", "access": "controlled", "justification": "PII",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("justified restriction: %d body=%s", resp.StatusCode, mustReadBody(t, resp))
	}
	got = mustReadJSON(t, resp)
	if got["access"] != "controlled" || got["effective_access"] != "controlled" {
		t.Errorf("labels not applied: %v", got)
	}

	// Drift counter: acme restricted, beta community => 1/2.
	resp = doJSON(t, c, "GET", ts.URL+"/api/access/label-drift", "dev-operator-key", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("drift: %d", resp.StatusCode)
	}
	drift := mustReadJSON(t, resp)
	if drift["producers"].(float64) != 2 || drift["restricted"].(float64) != 1 {
		t.Errorf("drift counts off: %v", drift)
	}
}

// TestLabels_InlineAtRegistration checks the optional label block on producer
// registration: an invalid label refuses the whole registration, a valid one
// lands and surfaces on the list.
func TestLabels_InlineAtRegistration(t *testing.T) {
	ts := newTestServer(t)
	c := &http.Client{}

	// Invalid inline label (restriction, no justification) refuses registration.
	resp := doJSON(t, c, "POST", ts.URL+"/api/vendors", "dev-operator-key", map[string]any{
		"prefix": "bad", "auto_approve": true,
		"labels": map[string]any{"access": "org"},
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid inline label: got %d, want 400", resp.StatusCode)
	}
	resp.Body.Close()
	// And no producer was created.
	resp = doJSON(t, c, "GET", ts.URL+"/api/orgs/bad/labels", "dev-operator-key", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("failed registration should not create producer: got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Valid inline label lands.
	resp = doJSON(t, c, "POST", ts.URL+"/api/vendors", "dev-operator-key", map[string]any{
		"prefix": "good", "auto_approve": true,
		"labels": map[string]any{"fidelity": "simulated", "access": "community"},
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("valid inline label registration: %d", resp.StatusCode)
	}
	resp.Body.Close()

	resp = doJSON(t, c, "GET", ts.URL+"/api/orgs/good/labels", "dev-operator-key", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get labels: %d", resp.StatusCode)
	}
	got := mustReadJSON(t, resp)
	if got["fidelity"] != "simulated" || got["effective_access"] != "community" {
		t.Errorf("inline labels not applied: %v", got)
	}
}
