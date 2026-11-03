package api_test

import (
	"net/http"
	"testing"
)

// TestDecodedEndpoint_XMLFormatAccepted verifies the decode gate now
// routes xml:<Root> topics (previously rejected 400) into the XML decoder.
// The fake zenoh proxy returns a JSON envelope rather than an XML sample,
// so the per-sample decode itself fails — but the endpoint returns 200 and
// echoes the xml format, which is what proves the dispatch wired through.
func TestDecodedEndpoint_XMLFormatAccepted(t *testing.T) {
	ts := newTestServer(t)
	c := &http.Client{}
	resp := doJSON(t, c, "POST", ts.URL+"/api/vendors", "dev-operator-key", map[string]any{
		"prefix": "caracal", "auto_approve": true,
	})
	apiKey, _ := mustReadJSON(t, resp)["api_key"].(string)
	resp = doJSON(t, c, "POST", ts.URL+"/api/vendors/caracal/topics", apiKey, map[string]any{
		"key_pattern": "caracal/cop/cot/v1",
		"format":      "xml:event",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create topic: %d body=%s", resp.StatusCode, mustReadBody(t, resp))
	}

	// No schema uploaded — XML is self-describing, so this must NOT 409.
	r, _ := http.NewRequest("GET", ts.URL+"/api/topics/decoded?key=caracal/cop/cot/v1", nil)
	r.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := c.Do(r)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for xml topic, got %d body=%s", resp.StatusCode, mustReadBody(t, resp))
	}
	got := mustReadJSON(t, resp)
	if got["format"] != "xml:event" {
		t.Errorf("format = %v, want xml:event", got["format"])
	}
	if _, ok := got["samples"].([]any); !ok {
		t.Errorf("response missing samples array: %v", got)
	}
}

// TestDecodedEndpoint_NonXMLNonProtoStillRejected guards that the widened
// gate didn't start accepting plain formats — a json topic is still 400.
func TestDecodedEndpoint_NonXMLNonProtoStillRejected(t *testing.T) {
	ts := newTestServer(t)
	c := &http.Client{}
	resp := doJSON(t, c, "POST", ts.URL+"/api/vendors", "dev-operator-key", map[string]any{
		"prefix": "caracal", "auto_approve": true,
	})
	apiKey, _ := mustReadJSON(t, resp)["api_key"].(string)
	resp = doJSON(t, c, "POST", ts.URL+"/api/vendors/caracal/topics", apiKey, map[string]any{
		"key_pattern": "caracal/plain/v1",
		"format":      "json",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create topic: %d", resp.StatusCode)
	}
	r, _ := http.NewRequest("GET", ts.URL+"/api/topics/decoded?key=caracal/plain/v1", nil)
	r.Header.Set("Authorization", "Bearer "+apiKey)
	resp, _ = c.Do(r)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for json topic, got %d", resp.StatusCode)
	}
}
