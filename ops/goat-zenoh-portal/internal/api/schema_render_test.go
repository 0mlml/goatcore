package api_test

import (
	"io"
	"net/http"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

// buildRealFDS produces a serialized FileDescriptorSet containing one
// message `caracal.tracks.v1.Track { string id; double lat; double lon; }`.
// Mirrors decoder/protobuf_test.go's helper — duplicated here because
// package boundaries keep that helper unexported.
func buildRealFDS(t *testing.T) []byte {
	t.Helper()
	syntax := "proto3"
	file := &descriptorpb.FileDescriptorProto{
		Name:    proto.String("track.proto"),
		Package: proto.String("caracal.tracks.v1"),
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
					Name:   proto.String("lat"),
					Number: proto.Int32(2),
					Type:   descriptorpb.FieldDescriptorProto_TYPE_DOUBLE.Enum(),
					Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
				},
				{
					Name:   proto.String("lon"),
					Number: proto.Int32(3),
					Type:   descriptorpb.FieldDescriptorProto_TYPE_DOUBLE.Enum(),
					Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
				},
			},
		}},
	}
	fds := &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{file}}
	b, err := proto.Marshal(fds)
	if err != nil {
		t.Fatalf("marshal fds: %v", err)
	}
	return b
}

// TestSchema_RenderEndpoint walks the path an operator would: upload a
// real .fds, then GET /api/schemas/render/<key> and check the returned
// JSON describes the message fields.
func TestSchema_RenderEndpoint(t *testing.T) {
	ts := newTestServer(t)
	c := &http.Client{}
	apiKey, topicID, keyPattern := registerVendorAndTopic(t, ts.URL, c)

	fdsBytes := buildRealFDS(t)
	body, ctype := uploadSchemaForm(t, fdsBytes, "v1", "backward", "protobuf-fds")
	url := ts.URL + "/api/vendors/caracal/topics/" + itoa(topicID) + "/schemas"
	resp := doUpload(t, c, url, apiKey, body, ctype)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("upload: %d body=%s", resp.StatusCode, mustReadBody(t, resp))
	}
	resp.Body.Close()

	// Fetch the structured render.
	renderURL := ts.URL + "/api/schemas/render/" + keyPattern
	resp = doJSON(t, c, "GET", renderURL, apiKey, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("render: %d body=%s", resp.StatusCode, mustReadBody(t, resp))
	}
	if got := resp.Header.Get("X-Schema-Version"); got != "v1" {
		t.Errorf("X-Schema-Version = %q", got)
	}
	rendered := mustReadJSON(t, resp)
	if rendered["key"] != keyPattern {
		t.Errorf("key = %v", rendered["key"])
	}
	if rendered["vendor"] != "caracal" {
		t.Errorf("vendor = %v", rendered["vendor"])
	}
	if rendered["primary_message"] != "Track" {
		t.Errorf("primary_message = %v", rendered["primary_message"])
	}
	schema, _ := rendered["schema"].(map[string]any)
	if schema["version"] != "v1" {
		t.Errorf("schema.version = %v", schema["version"])
	}
	rd, _ := rendered["rendered"].(map[string]any)
	files, _ := rd["files"].([]any)
	if len(files) != 1 {
		t.Fatalf("files = %d, want 1", len(files))
	}
	msgs, _ := rd["messages"].([]any)
	if len(msgs) != 1 {
		t.Fatalf("messages = %d, want 1", len(msgs))
	}
	m0, _ := msgs[0].(map[string]any)
	if m0["full_name"] != "caracal.tracks.v1.Track" {
		t.Errorf("message full_name = %v", m0["full_name"])
	}
	fields, _ := m0["fields"].([]any)
	if len(fields) != 3 {
		t.Errorf("fields = %d, want 3", len(fields))
	}
}

// TestSchema_RenderOverrideSourceIsOrg covers ADR 1052 Phase 3: a code-example
// override renders with provenance source="org" (was "vendor"); autogen rows
// stay "autogen".
func TestSchema_RenderOverrideSourceIsOrg(t *testing.T) {
	ts := newTestServer(t)
	c := &http.Client{}
	apiKey, topicID, keyPattern := registerVendorAndTopic(t, ts.URL, c)

	fdsBytes := buildRealFDS(t)
	body, ctype := uploadSchemaForm(t, fdsBytes, "v1", "backward", "protobuf-fds")
	resp := doUpload(t, c, ts.URL+"/api/vendors/caracal/topics/"+itoa(topicID)+"/schemas", apiKey, body, ctype)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("upload: %d body=%s", resp.StatusCode, mustReadBody(t, resp))
	}
	resp.Body.Close()

	// PUT a Rust override so that language renders from the org-provided row.
	resp = doJSON(t, c, "PUT", ts.URL+"/api/vendors/caracal/topics/"+itoa(topicID)+"/examples/rust", apiKey, map[string]any{
		"code": "fn main() {}\n",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put override: %d body=%s", resp.StatusCode, mustReadBody(t, resp))
	}
	resp.Body.Close()

	resp = doJSON(t, c, "GET", ts.URL+"/api/schemas/render/"+keyPattern, apiKey, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("render: %d body=%s", resp.StatusCode, mustReadBody(t, resp))
	}
	rendered := mustReadJSON(t, resp)
	examples, _ := rendered["examples"].([]any)
	if len(examples) == 0 {
		t.Fatal("no examples in render response")
	}
	var sawRustOrg, sawAutogen bool
	for _, e := range examples {
		ex, _ := e.(map[string]any)
		switch ex["language"] {
		case "rust":
			if ex["source"] != "org" {
				t.Errorf("rust override source = %v, want \"org\"", ex["source"])
			}
			sawRustOrg = true
		default:
			if ex["source"] == "autogen" {
				sawAutogen = true
			}
			if ex["source"] == "vendor" {
				t.Errorf("language %v still renders legacy source=\"vendor\"", ex["language"])
			}
		}
	}
	if !sawRustOrg {
		t.Error("rust example not found in render output")
	}
	if !sawAutogen {
		t.Error("expected at least one autogen example for a non-overridden language")
	}
}

// TestSchema_RenderMissing covers the two not-found paths: unknown topic
// and known topic with no schema body uploaded.
func TestSchema_RenderMissing(t *testing.T) {
	ts := newTestServer(t)
	c := &http.Client{}
	apiKey, _, keyPattern := registerVendorAndTopic(t, ts.URL, c)

	// Topic exists, no schema body — 404.
	resp := doJSON(t, c, "GET", ts.URL+"/api/schemas/render/"+keyPattern, apiKey, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("no-schema-yet: got %d, want 404", resp.StatusCode)
	}
	resp.Body.Close()

	// Topic does not exist — 404.
	resp = doJSON(t, c, "GET", ts.URL+"/api/schemas/render/caracal/nope/missing/v1", apiKey, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("missing-topic: got %d, want 404", resp.StatusCode)
	}
	resp.Body.Close()
}

// TestSchema_RenderInvalidVendor catches a vendor prefix that fails
// validation before any DB lookup. Cheap input-guard test.
func TestSchema_RenderInvalidVendor(t *testing.T) {
	ts := newTestServer(t)
	c := &http.Client{}
	apiKey, _, _ := registerVendorAndTopic(t, ts.URL, c)
	resp := doJSON(t, c, "GET", ts.URL+"/api/schemas/render/BAD%20VENDOR/topic/v1", apiKey, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("bad-vendor: got %d, want 400", resp.StatusCode)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
}
