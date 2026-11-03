package api_test

import (
	"net/http"
	"testing"
)

// TestCatalog_OrgRowsCarryPublishRollupForSlotKeys: the catalog is the one
// request the Slots page makes, and it is readable by any authenticated
// principal. Its org rows carry the per-org publish roll-up (publish_count
// always present; last_publish_at and alias only when recorded) so a partner
// on a slot key sees "Last publish …" without the operator-only progress
// endpoint, and without the page making a second request.
func TestCatalog_OrgRowsCarryPublishRollupForSlotKeys(t *testing.T) {
	ts := newTestServer(t)
	c := &http.Client{}

	resp := doJSON(t, c, "POST", ts.URL+"/api/vendors", "dev-operator-key", map[string]any{
		"prefix": "acme", "auto_approve": true,
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register acme: %d", resp.StatusCode)
	}
	acmeKey, _ := mustReadJSON(t, resp)["api_key"].(string)
	resp = doJSON(t, c, "POST", ts.URL+"/api/vendors/acme/topics", acmeKey, map[string]any{
		"key_pattern": "acme/tracks/v1", "format": "json",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create topic: %d body=%s", resp.StatusCode, mustReadBody(t, resp))
	}
	resp.Body.Close()

	// Read with the org's OWN key, not the operator key.
	resp = doJSON(t, c, "GET", ts.URL+"/api/catalog", acmeKey, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("catalog with org key: %d body=%s", resp.StatusCode, mustReadBody(t, resp))
	}
	got := mustReadJSON(t, resp)
	orgs, _ := got["orgs"].([]any)
	if len(orgs) != 1 {
		t.Fatalf("orgs = %d, want 1: %v", len(orgs), got)
	}
	row := orgs[0].(map[string]any)
	if n, ok := row["publish_count"].(float64); !ok || n != 0 {
		t.Errorf("publish_count = %v (%T), want 0 present as a number", row["publish_count"], row["publish_count"])
	}
	for _, absent := range []string{"last_publish_at", "alias"} {
		if v, present := row[absent]; present {
			t.Errorf("%s = %v; want absent when nothing is recorded (omitempty, never null)", absent, v)
		}
	}
}
