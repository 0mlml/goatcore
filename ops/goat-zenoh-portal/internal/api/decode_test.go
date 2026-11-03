package api_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/decoder"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

// buildTrackFDSAndPayload builds an inline FileDescriptorSet that
// matches what a vendor would upload, plus a serialized payload that
// matches that schema. Used by the decoded-endpoint smoke test below.
func buildTrackFDSAndPayload(t *testing.T) (fdsBytes []byte, payload []byte) {
	t.Helper()
	syntax := "proto3"
	file := &descriptorpb.FileDescriptorProto{
		Name:    proto.String("track.proto"),
		Package: proto.String("caracal"),
		Syntax:  &syntax,
		MessageType: []*descriptorpb.DescriptorProto{{
			Name: proto.String("Track"),
			Field: []*descriptorpb.FieldDescriptorProto{
				{
					Name:   proto.String("id"),
					Number: proto.Int32(1),
					Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
					Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
				},
				{
					Name:   proto.String("speed"),
					Number: proto.Int32(2),
					Type:   descriptorpb.FieldDescriptorProto_TYPE_UINT32.Enum(),
					Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
				},
			},
		}},
	}
	fds := &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{file}}
	fdsBytes, err := proto.Marshal(fds)
	if err != nil {
		t.Fatalf("marshal fds: %v", err)
	}
	files, _ := protodesc.NewFiles(fds)
	d, _ := files.FindDescriptorByName("caracal.Track")
	md := d.(protoreflect.MessageDescriptor)
	msg := dynamicpb.NewMessage(md)
	msg.Set(md.Fields().ByName("id"), protoreflect.ValueOfString("track-7"))
	msg.Set(md.Fields().ByName("speed"), protoreflect.ValueOfUint32(310))
	payload, err = proto.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return fdsBytes, payload
}

// TestDecodedEndpoint_NotFound exercises the 404 path: topic not in
// the registry.
func TestDecodedEndpoint_NotFound(t *testing.T) {
	ts := newTestServer(t)
	c := &http.Client{}
	r, _ := http.NewRequest("GET", ts.URL+"/api/topics/decoded?key=ghost/no/such/v1", nil)
	r.Header.Set("Authorization", "Bearer dev-operator-key")
	resp, err := c.Do(r)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest && resp.StatusCode != http.StatusNotFound {
		// Vendor "ghost" doesn't pass the prefix regex, so 400 is also acceptable here.
		t.Errorf("expected 400 or 404, got %d body=%s", resp.StatusCode, mustReadBody(t, resp))
	}
}

// TestDecodedEndpoint_TopicWithoutSchema exercises the 409 path: topic
// exists but no schema has been uploaded.
func TestDecodedEndpoint_TopicWithoutSchema(t *testing.T) {
	ts := newTestServer(t)
	c := &http.Client{}

	// Register vendor + protobuf topic.
	resp := doJSON(t, c, "POST", ts.URL+"/api/vendors", "dev-operator-key", map[string]any{
		"prefix": "caracal", "auto_approve": true,
	})
	apiKey, _ := mustReadJSON(t, resp)["api_key"].(string)
	keyPattern := "caracal/tracks/aircraft/v1"
	resp = doJSON(t, c, "POST", ts.URL+"/api/vendors/caracal/topics", apiKey, map[string]any{
		"key_pattern": keyPattern,
		"format":      "protobuf:Track",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create topic: %d", resp.StatusCode)
	}

	// Hit /api/topics/decoded — no schema yet, should 409.
	r, _ := http.NewRequest("GET", ts.URL+"/api/topics/decoded?key="+keyPattern, nil)
	r.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := c.Do(r)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("expected 409, got %d body=%s", resp.StatusCode, mustReadBody(t, resp))
	}
}

// TestDecodedEndpoint_WrongFormat — non-protobuf topic returns 400.
func TestDecodedEndpoint_WrongFormat(t *testing.T) {
	ts := newTestServer(t)
	c := &http.Client{}
	resp := doJSON(t, c, "POST", ts.URL+"/api/vendors", "dev-operator-key", map[string]any{
		"prefix": "caracal", "auto_approve": true,
	})
	apiKey, _ := mustReadJSON(t, resp)["api_key"].(string)
	resp = doJSON(t, c, "POST", ts.URL+"/api/vendors/caracal/topics", apiKey, map[string]any{
		"key_pattern": "caracal/json-topic/v1",
		"format":      "json",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create topic: %d", resp.StatusCode)
	}
	r, _ := http.NewRequest("GET", ts.URL+"/api/topics/decoded?key=caracal/json-topic/v1", nil)
	r.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := c.Do(r)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 (format), got %d body=%s", resp.StatusCode, mustReadBody(t, resp))
	}
}

// TestDecodedEndpoint_HappyPath exercises the full chain: register +
// upload an .fds + hit /api/topics/decoded. The fake zenoh proxy
// returns a JSON envelope rather than real proto bytes, so the
// per-sample decode itself will fail — but the response shape, schema
// version, etc. are what's under test here. End-to-end with real proto
// bytes lives in the live-fire on nova-a.
func TestDecodedEndpoint_HappyPath_FakeSampleFailsDecode(t *testing.T) {
	ts := newTestServer(t)
	c := &http.Client{}

	resp := doJSON(t, c, "POST", ts.URL+"/api/vendors", "dev-operator-key", map[string]any{
		"prefix": "caracal", "auto_approve": true,
	})
	apiKey, _ := mustReadJSON(t, resp)["api_key"].(string)
	keyPattern := "caracal/tracks/aircraft/v1"
	resp = doJSON(t, c, "POST", ts.URL+"/api/vendors/caracal/topics", apiKey, map[string]any{
		"key_pattern": keyPattern,
		"format":      "protobuf:Track",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create topic: %d", resp.StatusCode)
	}
	topicID := int64(mustReadJSON(t, resp)["id"].(float64))

	// Upload a real .fds.
	fdsBytes, _ := buildTrackFDSAndPayload(t)
	body, ctype := uploadSchemaForm(t, fdsBytes, "v1", "none", "protobuf-fds")
	resp = doUpload(t, c, ts.URL+"/api/vendors/caracal/topics/"+itoa(topicID)+"/schemas", apiKey, body, ctype)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("upload fds: %d body=%s", resp.StatusCode, mustReadBody(t, resp))
	}

	// Hit decoded endpoint. Fake zenoh returns JSON envelope, not proto.
	r, _ := http.NewRequest("GET", ts.URL+"/api/topics/decoded?key="+keyPattern, nil)
	r.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := c.Do(r)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", resp.StatusCode, mustReadBody(t, resp))
	}
	bodyMap := mustReadJSON(t, resp)
	if bodyMap["format"] != "protobuf:Track" {
		t.Errorf("format = %v, want protobuf:Track", bodyMap["format"])
	}
	if bodyMap["schema_version"] != "v1" {
		t.Errorf("schema_version = %v, want v1", bodyMap["schema_version"])
	}
	samples, _ := bodyMap["samples"].([]any)
	if len(samples) == 0 {
		t.Fatalf("no samples returned")
	}
	first, _ := samples[0].(map[string]any)
	if first["error"] == nil || first["error"] == "" {
		t.Errorf("expected per-sample decode error from fake JSON envelope; got: %#v", first)
	}
}

// TestDecodedEndpoint_RawBase64IncludedWhenRawTrue verifies that
// `&raw=true` includes the base64-encoded payload bytes in each
// per-sample response, off by default. The vendor-decode-rca tool
// + the stethoscope decode-rate check both rely on this so they can
// discriminate failure modes against on-wire bytes without a separate
// Zenoh REST round-trip.
func TestDecodedEndpoint_RawBase64IncludedWhenRawTrue(t *testing.T) {
	ts := newTestServer(t)
	c := &http.Client{}
	resp := doJSON(t, c, "POST", ts.URL+"/api/vendors", "dev-operator-key", map[string]any{
		"prefix": "caracal", "auto_approve": true,
	})
	apiKey, _ := mustReadJSON(t, resp)["api_key"].(string)
	keyPattern := "caracal/tracks/aircraft/v1"
	resp = doJSON(t, c, "POST", ts.URL+"/api/vendors/caracal/topics", apiKey, map[string]any{
		"key_pattern": keyPattern,
		"format":      "protobuf:Track",
	})
	topicID := int64(mustReadJSON(t, resp)["id"].(float64))
	fdsBytes, _ := buildTrackFDSAndPayload(t)
	body, ctype := uploadSchemaForm(t, fdsBytes, "v1", "none", "protobuf-fds")
	resp = doUpload(t, c, ts.URL+"/api/vendors/caracal/topics/"+itoa(topicID)+"/schemas", apiKey, body, ctype)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("upload: %d body=%s", resp.StatusCode, mustReadBody(t, resp))
	}

	// raw=true → expect raw_base64 set on each sample.
	r, _ := http.NewRequest("GET", ts.URL+"/api/topics/decoded?key="+keyPattern+"&raw=true", nil)
	r.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := c.Do(r)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", resp.StatusCode, mustReadBody(t, resp))
	}
	rawOut := mustReadJSON(t, resp)
	samples, _ := rawOut["samples"].([]any)
	if len(samples) == 0 {
		t.Fatalf("no samples returned")
	}
	first, _ := samples[0].(map[string]any)
	rb64, _ := first["raw_base64"].(string)
	if rb64 == "" {
		t.Errorf("raw=true: raw_base64 missing on sample; got %#v", first)
	}
	// Decode the base64 to confirm it's valid.
	if _, derr := base64.StdEncoding.DecodeString(rb64); derr != nil {
		t.Errorf("raw_base64 not valid base64: %v", derr)
	}

	// raw omitted → raw_base64 must be absent.
	r2, _ := http.NewRequest("GET", ts.URL+"/api/topics/decoded?key="+keyPattern, nil)
	r2.Header.Set("Authorization", "Bearer "+apiKey)
	resp2, err := c.Do(r2)
	if err != nil {
		t.Fatalf("do2: %v", err)
	}
	noRawOut := mustReadJSON(t, resp2)
	samples2, _ := noRawOut["samples"].([]any)
	if len(samples2) > 0 {
		first2, _ := samples2[0].(map[string]any)
		if rb := first2["raw_base64"]; rb != nil && rb != "" {
			t.Errorf("raw omitted: raw_base64 should be absent, got %v", rb)
		}
	}
}

// TestDecodedEndpoint_StubProxyDecodes exercises real proto decode by
// constructing a sample with a base64-encoded proto payload and
// shoving it into a custom test server that wraps the fake proxy. This
// is the closest unit-test approximation to the operator-side live fire.
func TestDecodedEndpoint_RealProtoBytesDecodeOK(t *testing.T) {
	ts := newTestServer(t)
	c := &http.Client{}

	// Setup
	resp := doJSON(t, c, "POST", ts.URL+"/api/vendors", "dev-operator-key", map[string]any{
		"prefix": "caracal", "auto_approve": true,
	})
	apiKey, _ := mustReadJSON(t, resp)["api_key"].(string)
	keyPattern := "caracal/tracks/aircraft/v1"
	resp = doJSON(t, c, "POST", ts.URL+"/api/vendors/caracal/topics", apiKey, map[string]any{
		"key_pattern": keyPattern,
		"format":      "protobuf:Track",
	})
	topicID := int64(mustReadJSON(t, resp)["id"].(float64))
	fdsBytes, payload := buildTrackFDSAndPayload(t)
	body, ctype := uploadSchemaForm(t, fdsBytes, "v1", "none", "protobuf-fds")
	resp = doUpload(t, c, ts.URL+"/api/vendors/caracal/topics/"+itoa(topicID)+"/schemas", apiKey, body, ctype)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("upload: %d", resp.StatusCode)
	}

	// Sanity: the decoder used by the handler can decode this payload.
	// (The fake zenoh proxy doesn't accept injected samples, so the
	// full HTTP-level round trip falls through to the per-sample error
	// path. This sub-test asserts the decoder wiring works against the
	// uploaded descriptor by replaying the proto bytes through a stub
	// extract → decode pipeline.)
	b64 := base64.StdEncoding.EncodeToString(payload)
	value := json.RawMessage(strconv.Quote(b64))
	out, err := decodeForTest(fdsBytes, "Track", value)
	if err != nil {
		t.Fatalf("decode round trip: %v", err)
	}
	if !bytes.Contains(out, []byte(`"track-7"`)) || !bytes.Contains(out, []byte(`310`)) {
		t.Errorf("decoded JSON missing expected fields: %s", out)
	}
}

// decodeForTest mirrors the handler's extract+decode logic so a unit
// test can verify the decoder wiring without needing to inject samples
// into the zenoh proxy.
func decodeForTest(fdsBytes []byte, msgName string, value json.RawMessage) ([]byte, error) {
	// Inline the same shape as decode.go's extractPayload + DecodeProtobuf.
	var s string
	var payload []byte
	if err := json.Unmarshal(value, &s); err == nil {
		decoded, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			return nil, err
		}
		payload = decoded
	} else {
		var bs []byte
		if err := json.Unmarshal(value, &bs); err == nil {
			payload = bs
		} else {
			payload = []byte(value)
		}
	}
	return decoder.DecodeProtobuf(fdsBytes, msgName, payload)
}
