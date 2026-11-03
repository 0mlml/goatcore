package decoder

import (
	"encoding/json"
	"testing"
)

const cotSample = `<event uid="ANDROID-123" how="m-g">
  <point lat="34.0" lon="-118.2"/>
  <detail>alpha</detail>
  <detail>bravo</detail>
</event>`

func TestDecodeXML_HappyPath(t *testing.T) {
	out, err := DecodeXML(nil, "event", []byte(cotSample))
	if err != nil {
		t.Fatalf("DecodeXML: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("result not JSON: %v (%s)", err, out)
	}
	ev, ok := m["event"].(map[string]any)
	if !ok {
		t.Fatalf("no event object: %s", out)
	}
	if ev["@uid"] != "ANDROID-123" {
		t.Errorf("@uid = %v, want ANDROID-123", ev["@uid"])
	}
	// Repeated <detail> collapses into an array.
	details, ok := ev["detail"].([]any)
	if !ok || len(details) != 2 || details[0] != "alpha" {
		t.Errorf("detail = %v, want [alpha bravo]", ev["detail"])
	}
	// Nested element with attributes becomes an object.
	pt, ok := ev["point"].(map[string]any)
	if !ok || pt["@lat"] != "34.0" {
		t.Errorf("point = %v", ev["point"])
	}
}

func TestDecodeXML_RootMismatch(t *testing.T) {
	if _, err := DecodeXML(nil, "track", []byte(cotSample)); err == nil {
		t.Errorf("expected error when declared root != document root")
	}
}

func TestDecodeXML_OpaqueNoRootCheck(t *testing.T) {
	// Empty rootElement (opaque xml topic) accepts any well-formed doc.
	if _, err := DecodeXML(nil, "", []byte(cotSample)); err != nil {
		t.Errorf("opaque decode should accept any well-formed xml: %v", err)
	}
}

func TestDecodeXML_BadInputs(t *testing.T) {
	if _, err := DecodeXML(nil, "event", nil); err == nil {
		t.Errorf("expected error for empty payload")
	}
	if _, err := DecodeXML(nil, "event", []byte(`<event><oops`)); err == nil {
		t.Errorf("expected error for malformed xml")
	}
}
