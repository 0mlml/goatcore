package api_test

import (
	"net/http"
	"testing"
)

// TestCodeExamples_PutGetDeleteRoundTrip walks the operator-facing flow:
// register a vendor + topic, PUT a Rust override, GET it back, DELETE
// it. Validates 4xx surfaces on the bad-input paths.
func TestCodeExamples_PutGetDeleteRoundTrip(t *testing.T) {
	ts := newTestServer(t)
	c := &http.Client{}
	apiKey, topicID, _ := registerVendorAndTopic(t, ts.URL, c)
	base := ts.URL + "/api/vendors/caracal/topics/" + itoa(topicID) + "/examples"

	// 404 before any upload.
	resp := doJSON(t, c, "GET", base+"/rust", apiKey, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("get-before-upload: got %d, want 404", resp.StatusCode)
	}
	resp.Body.Close()

	// PUT a vendor override.
	resp = doJSON(t, c, "PUT", base+"/rust", apiKey, map[string]any{
		"code": "fn main() { println!(\"hello\"); }\n",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put: got %d body=%s", resp.StatusCode, mustReadBody(t, resp))
	}
	body := mustReadJSON(t, resp)
	if body["language"] != "rust" {
		t.Errorf("language = %v", body["language"])
	}

	// GET returns the same body.
	resp = doJSON(t, c, "GET", base+"/rust", apiKey, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get: got %d", resp.StatusCode)
	}
	gotBody := mustReadJSON(t, resp)
	if got := gotBody["code"].(string); got != "fn main() { println!(\"hello\"); }\n" {
		t.Errorf("get returned wrong code: %q", got)
	}

	// PUT again to verify UPSERT semantics (replace, not append).
	resp = doJSON(t, c, "PUT", base+"/rust", apiKey, map[string]any{
		"code": "fn main() {}\n",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put-replace: %d", resp.StatusCode)
	}
	resp.Body.Close()

	// DELETE clears it.
	req, _ := http.NewRequest("DELETE", base+"/rust", nil)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("delete: got %d, want 204", resp.StatusCode)
	}
	resp.Body.Close()

	// GET after delete is 404 again.
	resp = doJSON(t, c, "GET", base+"/rust", apiKey, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("get-after-delete: got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestCodeExamples_Validation(t *testing.T) {
	ts := newTestServer(t)
	c := &http.Client{}
	apiKey, topicID, _ := registerVendorAndTopic(t, ts.URL, c)
	base := ts.URL + "/api/vendors/caracal/topics/" + itoa(topicID) + "/examples"

	// Empty body.
	resp := doJSON(t, c, "PUT", base+"/rust", apiKey, map[string]any{"code": ""})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("empty-code: %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Unsupported language.
	resp = doJSON(t, c, "PUT", base+"/brainfuck", apiKey, map[string]any{"code": "+++"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("bad-lang: %d", resp.StatusCode)
	}
	resp.Body.Close()
}
