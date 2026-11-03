package api_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestSmoke_ProgressEndpoint(t *testing.T) {
	ts := newTestServer(t)
	c := &http.Client{}

	// Empty case — no vendors registered yet, operator key, expects [].
	resp := doJSON(t, c, "GET", ts.URL+"/api/progress", "dev-operator-key", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("empty progress: status %d body=%s", resp.StatusCode, mustReadBody(t, resp))
	}
	var rows []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		t.Fatalf("decode empty: %v", err)
	}
	resp.Body.Close()
	if len(rows) != 0 {
		t.Fatalf("expected 0 rows initially, got %d", len(rows))
	}

	// Register a real vendor (will get L1 from bundle mint).
	resp = doJSON(t, c, "POST", ts.URL+"/api/vendors", "dev-operator-key", map[string]any{
		"prefix": "aardwolf", "display_name": "Aardwolf", "auto_approve": true,
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register: %d body=%s", resp.StatusCode, mustReadBody(t, resp))
	}
	resp.Body.Close()

	// Progress should now show aardwolf with l1 set (bundle was minted
	// in fake mode by the createVendor flow). max_level >= 1.
	resp = doJSON(t, c, "GET", ts.URL+"/api/progress", "dev-operator-key", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("progress after register: %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		t.Fatalf("decode after register: %v", err)
	}
	resp.Body.Close()
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0]["prefix"] != "aardwolf" {
		t.Errorf("row prefix = %v, want aardwolf", rows[0]["prefix"])
	}
	if rows[0]["display_name"] != "Aardwolf" {
		t.Errorf("display_name = %v", rows[0]["display_name"])
	}
	// max_level should be 1 (bundle minted in fake-mint path).
	if ml, _ := rows[0]["max_level"].(float64); ml < 1 {
		t.Errorf("max_level = %v, want >= 1", rows[0]["max_level"])
	}

	// Vendor-scoped key should NOT see progress.
	resp = doJSON(t, c, "GET", ts.URL+"/api/vendors", "dev-operator-key", nil)
	body := mustReadJSON(t, resp)
	vendors := body["vendors"].([]any)
	if len(vendors) == 0 {
		t.Fatalf("expected vendors back; got %+v", body)
	}
	// Find the api_key the createVendor returned earlier — re-mint won't return it,
	// so test the gate by sending a non-operator bearer.
	resp = doJSON(t, c, "GET", ts.URL+"/api/progress", "definitely-not-an-operator-or-vendor-key", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("unknown bearer expected 401, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestSmoke_ProgressSyntheticFilter(t *testing.T) {
	ts := newTestServer(t)
	c := &http.Client{}

	// Real vendor.
	resp := doJSON(t, c, "POST", ts.URL+"/api/vendors", "dev-operator-key", map[string]any{
		"prefix": "real-vendor", "display_name": "Real", "auto_approve": true,
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register real: %d body=%s", resp.StatusCode, mustReadBody(t, resp))
	}
	resp.Body.Close()
	resp = doJSON(t, c, "POST", ts.URL+"/api/vendors", "dev-operator-key", map[string]any{
		"prefix": "synth", "display_name": "Synth", "auto_approve": true, "synthetic": true,
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register synth: %d body=%s", resp.StatusCode, mustReadBody(t, resp))
	}
	resp.Body.Close()

	// Default — synthetic filtered out.
	resp = doJSON(t, c, "GET", ts.URL+"/api/progress", "dev-operator-key", nil)
	var rows []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		t.Fatalf("decode default: %v", err)
	}
	resp.Body.Close()
	if len(rows) != 1 {
		t.Fatalf("default filter: expected 1 row (synth filtered), got %d", len(rows))
	}
	if rows[0]["prefix"] != "real-vendor" {
		t.Errorf("default row prefix = %v", rows[0]["prefix"])
	}

	// include=synthetic
	resp = doJSON(t, c, "GET", ts.URL+"/api/progress?include=synthetic", "dev-operator-key", nil)
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		t.Fatalf("decode include: %v", err)
	}
	resp.Body.Close()
	if len(rows) != 2 {
		t.Fatalf("include=synthetic: expected 2 rows, got %d", len(rows))
	}
}
