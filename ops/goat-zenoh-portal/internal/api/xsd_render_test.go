package api_test

import (
	"net/http"
	"testing"
)

// TestSchema_RenderXSD is the XML/XSD parity of TestSchema_RenderEndpoint:
// register an xml:event topic, upload the CoT .xsd, then GET the render
// endpoint and assert it returns the same-shaped data dictionary + code
// examples, tagged source_lang=xsd. cotXSD is defined in xsd_upload_test.go.
func TestSchema_RenderXSD(t *testing.T) {
	ts := newTestServer(t)
	c := &http.Client{}
	apiKey, topicID, keyPattern := registerVendorAndTopicWithFormat(t, ts.URL, c, "xml:event")

	body, ctype := uploadSchemaForm(t, []byte(cotXSD), "v1", "backward", "xsd")
	url := ts.URL + "/api/vendors/caracal/topics/" + itoa(topicID) + "/schemas"
	resp := doUpload(t, c, url, apiKey, body, ctype)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("upload xsd: %d body=%s", resp.StatusCode, mustReadBody(t, resp))
	}
	resp.Body.Close()

	resp = doJSON(t, c, "GET", ts.URL+"/api/schemas/render/"+keyPattern, apiKey, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("render: %d body=%s", resp.StatusCode, mustReadBody(t, resp))
	}
	r := mustReadJSON(t, resp)
	if r["source_lang"] != "xsd" {
		t.Errorf("source_lang = %v, want xsd", r["source_lang"])
	}
	if r["primary_message"] != "event" {
		t.Errorf("primary_message = %v, want event", r["primary_message"])
	}

	// Data dictionary: same shape as protobuf, with an 'event' message.
	rd, _ := r["rendered"].(map[string]any)
	msgs, _ := rd["messages"].([]any)
	foundEvent := false
	for _, m := range msgs {
		if mm, _ := m.(map[string]any); mm["name"] == "event" {
			foundEvent = true
		}
	}
	if !foundEvent {
		t.Errorf("rendered.messages has no 'event': %v", msgs)
	}

	// XSD source tab is populated under proto_source (schema.xsd).
	src, _ := r["proto_source"].(map[string]any)
	if _, ok := src["schema.xsd"]; !ok {
		t.Errorf("proto_source missing schema.xsd: %v", src)
	}

	// Autogen code examples present, one per language.
	ex, _ := r["examples"].([]any)
	if len(ex) != 4 {
		t.Errorf("examples = %d, want 4 (rust/go/python/typescript)", len(ex))
	}
}
