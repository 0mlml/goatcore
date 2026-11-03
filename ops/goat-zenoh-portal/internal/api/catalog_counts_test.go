package api_test

import (
	"net/http"
	"testing"
)

// TestListVendors_DeclaredTopicCounts: the org listing carries each org's live
// declared-topic count so a catalog can fold "nothing declared" rows without
// a per-org fan-out. An org with no rows reports 0 (not absent), and a retired
// topic does not count.
func TestListVendors_DeclaredTopicCounts(t *testing.T) {
	ts := newTestServer(t)
	c := &http.Client{}

	keys := map[string]string{}
	for _, p := range []string{"acme", "beta"} {
		resp := doJSON(t, c, "POST", ts.URL+"/api/vendors", "dev-operator-key", map[string]any{
			"prefix": p, "auto_approve": true,
		})
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("register %s: %d", p, resp.StatusCode)
		}
		keys[p], _ = mustReadJSON(t, resp)["api_key"].(string)
	}
	for _, kp := range []string{"acme/tracks/v1", "acme/status/v1"} {
		resp := doJSON(t, c, "POST", ts.URL+"/api/vendors/acme/topics", keys["acme"], map[string]any{
			"key_pattern": kp, "format": "json",
		})
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("create topic %s: %d body=%s", kp, resp.StatusCode, mustReadBody(t, resp))
		}
		resp.Body.Close()
	}

	resp := doJSON(t, c, "GET", ts.URL+"/api/vendors", "dev-operator-key", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list: %d", resp.StatusCode)
	}
	got := mustReadJSON(t, resp)
	rows, _ := got["orgs"].([]any)
	counts := map[string]float64{}
	for _, r := range rows {
		m := r.(map[string]any)
		n, ok := m["declared_topics"].(float64)
		if !ok {
			t.Fatalf("org %v: declared_topics missing or not a number: %v", m["prefix"], m["declared_topics"])
		}
		counts[m["prefix"].(string)] = n
	}
	if counts["acme"] != 2 {
		t.Errorf("acme declared_topics = %v, want 2", counts["acme"])
	}
	if counts["beta"] != 0 {
		t.Errorf("beta declared_topics = %v, want 0 (present, not absent)", counts["beta"])
	}
}
