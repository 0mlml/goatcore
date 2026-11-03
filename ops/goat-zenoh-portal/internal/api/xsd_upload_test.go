package api_test

import (
	"mime"
	"net/http"
	"strings"
	"testing"
)

// cotXSD is a minimal Cursor-on-Target-shaped XML Schema with a top-level
// <xs:element name="event">. Used to exercise the xsd schema-upload path.
const cotXSD = `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:element name="event">
    <xs:complexType>
      <xs:attribute name="uid" type="xs:string"/>
    </xs:complexType>
  </xs:element>
</xs:schema>`

// TestSchema_UploadXSDMatchesRootElement is the happy end-to-end for the
// XML/XSD path: register a topic with format xml:event, upload an .xsd
// declaring a top-level <xs:element name="event"> → 201 Created.
func TestSchema_UploadXSDMatchesRootElement(t *testing.T) {
	ts := newTestServer(t)
	c := &http.Client{}
	apiKey, topicID, keyPattern := registerVendorAndTopicWithFormat(t, ts.URL, c, "xml:event")

	body, ctype := uploadSchemaForm(t, []byte(cotXSD), "v1", "none", "xsd")
	url := ts.URL + "/api/vendors/caracal/topics/" + itoa(topicID) + "/schemas"
	resp := doUpload(t, c, url, apiKey, body, ctype)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 for matching xsd, got %d body=%s", resp.StatusCode, mustReadBody(t, resp))
	}
	resp.Body.Close()

	fetched := doRaw(t, c, "GET", ts.URL+"/api/schemas/"+keyPattern+"?version=v1", apiKey, nil)
	if fetched.StatusCode != http.StatusOK {
		t.Fatalf("fetch xsd: %d body=%s", fetched.StatusCode, mustReadBody(t, fetched))
	}
	disposition := fetched.Header.Get("Content-Disposition")
	mediaType, params, err := mime.ParseMediaType(disposition)
	if err != nil || mediaType != "attachment" || params["filename"] != "caracal_tracks_aircraft_v1-v1.xsd" {
		t.Errorf("Content-Disposition = %q, want XSD attachment filename", disposition)
	}
	if got := mustReadBody(t, fetched); got != cotXSD {
		t.Errorf("fetched XSD body = %q, want original upload", got)
	}
}

// TestSchema_UploadXSDRejectsRootMismatch: topic declares xml:track but the
// .xsd declares only <event> → 422 format_mismatch, naming the found
// elements and suggesting the correction.
func TestSchema_UploadXSDRejectsRootMismatch(t *testing.T) {
	ts := newTestServer(t)
	c := &http.Client{}
	apiKey, topicID, _ := registerVendorAndTopicWithFormat(t, ts.URL, c, "xml:track")

	body, ctype := uploadSchemaForm(t, []byte(cotXSD), "v1", "none", "xsd")
	url := ts.URL + "/api/vendors/caracal/topics/" + itoa(topicID) + "/schemas"
	resp := doUpload(t, c, url, apiKey, body, ctype)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 format_mismatch, got %d body=%s", resp.StatusCode, mustReadBody(t, resp))
	}
	b := mustReadBody(t, resp)
	for _, want := range []string{"format_mismatch", "xml:track", "event", "most likely correct"} {
		if !strings.Contains(b, want) {
			t.Errorf("body should contain %q: %s", want, b)
		}
	}
}

// TestSchema_UploadXSDRejectsMalformed: a body claiming xsd format that
// isn't well-formed XML → 400 xsd_decode, regardless of topic format.
func TestSchema_UploadXSDRejectsMalformed(t *testing.T) {
	ts := newTestServer(t)
	c := &http.Client{}
	apiKey, topicID, _ := registerVendorAndTopicWithFormat(t, ts.URL, c, "xml:event")

	body, ctype := uploadSchemaForm(t, []byte(`<xs:schema><oops`), "v1", "none", "xsd")
	url := ts.URL + "/api/vendors/caracal/topics/" + itoa(topicID) + "/schemas"
	resp := doUpload(t, c, url, apiKey, body, ctype)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 xsd_decode, got %d body=%s", resp.StatusCode, mustReadBody(t, resp))
	}
	if b := mustReadBody(t, resp); !strings.Contains(b, "xsd_decode") {
		t.Errorf("expected error code 'xsd_decode': %s", b)
	}
}

// TestSchema_UploadXSDToOpaqueXML: a topic with plain format=xml carries no
// root-element binding, so a well-formed .xsd uploads without a mismatch
// check → 201.
func TestSchema_UploadXSDToOpaqueXML(t *testing.T) {
	ts := newTestServer(t)
	c := &http.Client{}
	apiKey, topicID, _ := registerVendorAndTopicWithFormat(t, ts.URL, c, "xml")

	body, ctype := uploadSchemaForm(t, []byte(cotXSD), "v1", "none", "xsd")
	url := ts.URL + "/api/vendors/caracal/topics/" + itoa(topicID) + "/schemas"
	resp := doUpload(t, c, url, apiKey, body, ctype)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 for opaque-xml topic, got %d body=%s", resp.StatusCode, mustReadBody(t, resp))
	}
}
