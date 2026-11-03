package api_test

import (
	"net/http"
	"strings"
	"testing"
)

// TestCreateTopic_UnknownOrgIs404NotA500: a topic under an org that was never
// created is a request the caller can correct, so the answer is a 404 naming
// the missing org. Before, the registry's foreign-key failure surfaced as a 500,
// which the CLI rendered as "portal unreachable" (found by the e2e goat suite).
func TestCreateTopic_UnknownOrgIs404NotA500(t *testing.T) {
	ts := newTestServer(t)
	c := &http.Client{}

	resp := doJSON(t, c, "POST", ts.URL+"/api/vendors/nope/topics", "dev-operator-key", map[string]any{
		"key_pattern": "nope/tracks/v1", "format": "json",
	})
	body := mustReadBody(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", resp.StatusCode, body)
	}
	if !strings.Contains(body, "no org with prefix") {
		t.Errorf("body does not name the missing org: %s", body)
	}
}
