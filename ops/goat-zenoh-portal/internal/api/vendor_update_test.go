package api_test

import (
	"net/http"
	"testing"
)

// TestUpdateVendor_DisplayNameRoundTrip verifies the operator-only
// PATCH /api/vendors/{vendor} endpoint that fixes the conflation
// failure mode: vendor minted with display_name = the person's name
// (Ory traits.name leak) when it should have been the vendor's
// display name. PATCH renames it; subsequent list reflects the change.
func TestUpdateVendor_DisplayNameRoundTrip(t *testing.T) {
	ts := newTestServer(t)
	c := &http.Client{}

	// Register a vendor with the wrong display_name on purpose.
	resp := doJSON(t, c, "POST", ts.URL+"/api/vendors", "dev-operator-key", map[string]any{
		"prefix":       "rtx-test",
		"display_name": "Person Name",
		"auto_approve": true,
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register: %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Vendor-API key from a different vendor must NOT be able to PATCH.
	// (Use the operator key; vendor keys are scoped to their own prefix
	// for topic ops only — vendor metadata mutation is operator-only by
	// design.)
	resp = doJSON(t, c, "PATCH", ts.URL+"/api/vendors/rtx-test", "wrong-key", map[string]any{
		"display_name": "Should Not Land",
	})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("non-operator PATCH: got %d, want 401", resp.StatusCode)
	}
	resp.Body.Close()

	// Operator PATCHes the display_name to the right value.
	resp = doJSON(t, c, "PATCH", ts.URL+"/api/vendors/rtx-test", "dev-operator-key", map[string]any{
		"display_name": "Vendor Display Name",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("operator PATCH: %d body=%s", resp.StatusCode, mustReadBody(t, resp))
	}
	got := mustReadJSON(t, resp)
	if got["display_name"] != "Vendor Display Name" {
		t.Errorf("display_name after PATCH: %v", got["display_name"])
	}
	if got["prefix"] != "rtx-test" {
		t.Errorf("prefix after PATCH: %v", got["prefix"])
	}

	// List reflects the new name.
	resp = doJSON(t, c, "GET", ts.URL+"/api/vendors", "dev-operator-key", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list: %d", resp.StatusCode)
	}
	listBody := mustReadJSON(t, resp)
	vendors, _ := listBody["vendors"].([]any)
	var found bool
	for _, raw := range vendors {
		v, _ := raw.(map[string]any)
		if v["prefix"] == "rtx-test" {
			found = true
			if v["display_name"] != "Vendor Display Name" {
				t.Errorf("list display_name: %v", v["display_name"])
			}
		}
	}
	if !found {
		t.Errorf("rtx-test missing from list")
	}

	// Empty display_name rejected.
	resp = doJSON(t, c, "PATCH", ts.URL+"/api/vendors/rtx-test", "dev-operator-key", map[string]any{
		"display_name": "",
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("empty display_name: got %d, want 400", resp.StatusCode)
	}
	resp.Body.Close()

	// Body with no mutable field rejected.
	resp = doJSON(t, c, "PATCH", ts.URL+"/api/vendors/rtx-test", "dev-operator-key", map[string]any{})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("empty body: got %d, want 400", resp.StatusCode)
	}
	resp.Body.Close()

	// Unknown vendor → 404.
	resp = doJSON(t, c, "PATCH", ts.URL+"/api/vendors/does-not-exist", "dev-operator-key", map[string]any{
		"display_name": "Anything",
	})
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown vendor: got %d, want 404", resp.StatusCode)
	}
	resp.Body.Close()
}
