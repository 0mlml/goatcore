package api_test

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

// validTrackFDSBytes builds a minimal valid FileDescriptorSet whose
// single top-level message is named "Track". Lets upload tests pair
// against the `protobuf:Track` format the shared topic helper declares
// without each test rebuilding the descriptor inline. Adding a
// version-disambiguating field name keeps the bytes distinct between
// v1 / v2 uploads (real schemas evolve too).
func validTrackFDSBytes(t *testing.T, suffixField string) []byte {
	t.Helper()
	syntax := "proto3"
	pkg := "caracal"
	fname := "track.proto"
	tname := "Track"
	idName := "id"
	idType := descriptorpb.FieldDescriptorProto_TYPE_STRING
	idLabel := descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL
	idNum := int32(1)
	fields := []*descriptorpb.FieldDescriptorProto{{
		Name: &idName, Number: &idNum, Type: &idType, Label: &idLabel,
	}}
	if suffixField != "" {
		fname2 := suffixField
		ftype := descriptorpb.FieldDescriptorProto_TYPE_STRING
		flabel := descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL
		fnum := int32(2)
		fields = append(fields, &descriptorpb.FieldDescriptorProto{
			Name: &fname2, Number: &fnum, Type: &ftype, Label: &flabel,
		})
	}
	fds := &descriptorpb.FileDescriptorSet{
		File: []*descriptorpb.FileDescriptorProto{{
			Name: &fname, Package: &pkg, Syntax: &syntax,
			MessageType: []*descriptorpb.DescriptorProto{{
				Name:  &tname,
				Field: fields,
			}},
		}},
	}
	b, err := proto.Marshal(fds)
	if err != nil {
		t.Fatalf("marshal valid Track fds: %v", err)
	}
	return b
}

// uploadSchemaForm builds a multipart/form-data request body for the
// schema upload endpoint and returns body bytes + content-type.
func uploadSchemaForm(t *testing.T, file []byte, version, compatibility, format string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if version != "" {
		_ = w.WriteField("version", version)
	}
	if compatibility != "" {
		_ = w.WriteField("compatibility", compatibility)
	}
	if format != "" {
		_ = w.WriteField("format", format)
	}
	fw, err := w.CreateFormFile("file", "schema.fds")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := fw.Write(file); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	return &buf, w.FormDataContentType()
}

func doUpload(t *testing.T, c *http.Client, url, bearer string, body *bytes.Buffer, contentType string) *http.Response {
	t.Helper()
	req, err := http.NewRequest("POST", url, body)
	if err != nil {
		t.Fatalf("new req: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Content-Type", contentType)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	return resp
}

// registerVendorAndTopic is a setup helper used by every schema test: it
// creates a vendor + a single topic and returns the vendor api_key + the
// topic id + the key_pattern.
func registerVendorAndTopic(t *testing.T, ts string, c *http.Client) (apiKey string, topicID int64, keyPattern string) {
	return registerVendorAndTopicWithFormat(t, ts, c, "protobuf:Track")
}

// registerVendorAndTopicWithFormat is the format-explicit variant used
// when a test needs a topic whose `format` declaration aligns with a
// specific descriptor set (e.g. testdata/valid.proto defines
// DenObservation, not Track).
func registerVendorAndTopicWithFormat(t *testing.T, ts string, c *http.Client, format string) (apiKey string, topicID int64, keyPattern string) {
	t.Helper()
	resp := doJSON(t, c, "POST", ts+"/api/vendors", "dev-operator-key", map[string]any{
		"prefix": "caracal", "display_name": "Caracal", "auto_approve": true,
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register vendor: %d", resp.StatusCode)
	}
	regResp := mustReadJSON(t, resp)
	apiKey, _ = regResp["api_key"].(string)

	keyPattern = "caracal/tracks/aircraft/v1"
	resp = doJSON(t, c, "POST", ts+"/api/vendors/caracal/topics", apiKey, map[string]any{
		"key_pattern": keyPattern,
		"format":      format,
		"description": "Aircraft track stream",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create topic: %d body=%s", resp.StatusCode, mustReadBody(t, resp))
	}
	tBody := mustReadJSON(t, resp)
	idF, _ := tBody["id"].(float64)
	return apiKey, int64(idF), keyPattern
}

// TestSchema_UploadFetchRoundTrip walks the v1 happy path: upload a
// protobuf .fds body, list versions, fetch by topic+version, fetch by
// key+version, fetch latest. Every step asserts what an operator would
// actually check.
func TestSchema_UploadFetchRoundTrip(t *testing.T) {
	ts := newTestServer(t)
	c := &http.Client{}

	apiKey, topicID, keyPattern := registerVendorAndTopic(t, ts.URL, c)

	// Real .fds with a top-level Track message — must match the
	// `protobuf:Track` format the topic declares so the write-time
	// format ↔ FDS validator accepts the upload.
	descriptor := validTrackFDSBytes(t, "")
	body, ctype := uploadSchemaForm(t, descriptor, "v1", "backward", "protobuf-fds")
	url := ts.URL + "/api/vendors/caracal/topics/" + itoa(topicID) + "/schemas"
	resp := doUpload(t, c, url, apiKey, body, ctype)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("upload v1: status %d body=%s", resp.StatusCode, mustReadBody(t, resp))
	}
	entry := mustReadJSON(t, resp)
	if entry["version"] != "v1" {
		t.Errorf("uploaded version = %v", entry["version"])
	}
	if entry["compatibility"] != "backward" {
		t.Errorf("uploaded compatibility = %v", entry["compatibility"])
	}
	if entry["format"] != "protobuf-fds" {
		t.Errorf("uploaded format = %v", entry["format"])
	}
	if int(entry["size_bytes"].(float64)) != len(descriptor) {
		t.Errorf("size_bytes = %v, want %d", entry["size_bytes"], len(descriptor))
	}

	// List schemas for the topic.
	resp = doJSON(t, c, "GET", ts.URL+"/api/vendors/caracal/topics/"+itoa(topicID)+"/schemas", apiKey, nil)
	listBody := mustReadJSON(t, resp)
	schemas, _ := listBody["schemas"].([]any)
	if len(schemas) != 1 {
		t.Fatalf("list schemas: got %d, want 1", len(schemas))
	}

	// Fetch by key (latest).
	resp, err := doFetch(c, ts.URL+"/api/schemas/"+keyPattern, apiKey)
	if err != nil {
		t.Fatalf("fetch by key: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("fetch latest: %d body=%s", resp.StatusCode, mustReadBody(t, resp))
	}
	if got := resp.Header.Get("X-Schema-Version"); got != "v1" {
		t.Errorf("X-Schema-Version = %q, want v1", got)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/x-protobuf-descriptor-set" {
		t.Errorf("Content-Type = %q", got)
	}
	gotBytes, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !bytes.Equal(gotBytes, descriptor) {
		t.Errorf("fetched bytes (%d) != uploaded bytes (%d)", len(gotBytes), len(descriptor))
	}

	// Fetch by key with explicit version.
	resp, err = doFetch(c, ts.URL+"/api/schemas/"+keyPattern+"?version=v1", apiKey)
	if err != nil {
		t.Fatalf("fetch by key+version: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("fetch by key+version: %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Topic's schema_ref should now be populated.
	resp = doJSON(t, c, "GET", ts.URL+"/api/vendors/caracal/topics/"+itoa(topicID), apiKey, nil)
	tBody := mustReadJSON(t, resp)
	if ref, _ := tBody["schema_ref"].(string); ref != "attest/schema/"+keyPattern+"/v1" {
		t.Errorf("schema_ref = %q after upload", ref)
	}

	// Upload v2 — latest should flip. Add a second field so the bytes
	// differ from v1 while keeping the top-level Track message.
	body, ctype = uploadSchemaForm(t, validTrackFDSBytes(t, "callsign"), "v2", "none", "protobuf-fds")
	resp = doUpload(t, c, url, apiKey, body, ctype)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("upload v2: %d body=%s", resp.StatusCode, mustReadBody(t, resp))
	}
	resp, err = doFetch(c, ts.URL+"/api/schemas/"+keyPattern, apiKey)
	if err != nil {
		t.Fatalf("fetch latest after v2: %v", err)
	}
	if got := resp.Header.Get("X-Schema-Version"); got != "v2" {
		t.Errorf("latest version after v2 upload = %q, want v2", got)
	}
	resp.Body.Close()
}

// TestSchema_UploadValidation exercises the four bad-input paths. Each
// case should be caught before the disk write, surfaced as 4xx, and
// leave no orphan file behind.
func TestSchema_UploadValidation(t *testing.T) {
	ts := newTestServer(t)
	c := &http.Client{}
	apiKey, topicID, _ := registerVendorAndTopic(t, ts.URL, c)
	url := ts.URL + "/api/vendors/caracal/topics/" + itoa(topicID) + "/schemas"

	t.Run("bad-version", func(t *testing.T) {
		body, ctype := uploadSchemaForm(t, []byte("fake"), "version-1", "none", "protobuf-fds")
		resp := doUpload(t, c, url, apiKey, body, ctype)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("bad version: got %d, want 400", resp.StatusCode)
		}
	})
	t.Run("bad-compatibility", func(t *testing.T) {
		body, ctype := uploadSchemaForm(t, []byte("fake"), "v1", "kindof", "protobuf-fds")
		resp := doUpload(t, c, url, apiKey, body, ctype)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("bad compat: got %d, want 400", resp.StatusCode)
		}
	})
	t.Run("bad-format", func(t *testing.T) {
		body, ctype := uploadSchemaForm(t, []byte("fake"), "v1", "none", "yaml-handwritten")
		resp := doUpload(t, c, url, apiKey, body, ctype)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("bad format: got %d, want 400", resp.StatusCode)
		}
	})
	t.Run("missing-file", func(t *testing.T) {
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		_ = w.WriteField("version", "v1")
		_ = w.WriteField("compatibility", "none")
		_ = w.Close()
		resp := doUpload(t, c, url, apiKey, &buf, w.FormDataContentType())
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("missing file: got %d, want 400", resp.StatusCode)
		}
	})
}

// TestSchema_UploadDuplicateVersion verifies the (topic_id, version)
// UNIQUE constraint surfaces as 409 Conflict.
func TestSchema_UploadDuplicateVersion(t *testing.T) {
	ts := newTestServer(t)
	c := &http.Client{}
	apiKey, topicID, _ := registerVendorAndTopic(t, ts.URL, c)
	url := ts.URL + "/api/vendors/caracal/topics/" + itoa(topicID) + "/schemas"

	body, ctype := uploadSchemaForm(t, validTrackFDSBytes(t, ""), "v1", "none", "protobuf-fds")
	resp := doUpload(t, c, url, apiKey, body, ctype)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("first upload: %d", resp.StatusCode)
	}
	body, ctype = uploadSchemaForm(t, validTrackFDSBytes(t, "second"), "v1", "none", "protobuf-fds")
	resp = doUpload(t, c, url, apiKey, body, ctype)
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("duplicate v1: got %d, want 409", resp.StatusCode)
	}
}

// TestSchema_CrossVendorRejected verifies a vendor cannot upload a
// schema to another vendor's topic. The scopedHandler middleware should
// catch this before the handler runs.
func TestSchema_CrossVendorRejected(t *testing.T) {
	ts := newTestServer(t)
	c := &http.Client{}
	caracalKey, caracalTopicID, _ := registerVendorAndTopic(t, ts.URL, c)

	resp := doJSON(t, c, "POST", ts.URL+"/api/vendors", "dev-operator-key", map[string]any{
		"prefix": "ibex", "display_name": "Ibex", "auto_approve": true,
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register ibex: %d", resp.StatusCode)
	}
	ibexKey, _ := mustReadJSON(t, resp)["api_key"].(string)

	// ibex tries to upload to caracal's topic. The scopedHandler 403 fires
	// before the format ↔ FDS gate so any bytes here are fine.
	body, ctype := uploadSchemaForm(t, []byte("evil"), "v1", "none", "protobuf-fds")
	url := ts.URL + "/api/vendors/caracal/topics/" + itoa(caracalTopicID) + "/schemas"
	resp = doUpload(t, c, url, ibexKey, body, ctype)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("cross-vendor schema upload: got %d, want 403", resp.StatusCode)
	}
	// Sanity: caracal can still upload — use a real Track FDS so the
	// write-time format ↔ FDS validator accepts it.
	body, ctype = uploadSchemaForm(t, validTrackFDSBytes(t, ""), "v1", "none", "protobuf-fds")
	resp = doUpload(t, c, url, caracalKey, body, ctype)
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("caracal self-upload: got %d, want 201", resp.StatusCode)
	}
}

// TestSchema_FetchNoBody returns 404 when no schema has been uploaded.
func TestSchema_FetchNoBody(t *testing.T) {
	ts := newTestServer(t)
	c := &http.Client{}
	apiKey, _, keyPattern := registerVendorAndTopic(t, ts.URL, c)

	resp, err := doFetch(c, ts.URL+"/api/schemas/"+keyPattern, apiKey)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("no-body fetch: got %d, want 404", resp.StatusCode)
	}
	resp.Body.Close()
}

// TestSchema_FetchUnauthRejected verifies unauthenticated fetches 401.
func TestSchema_FetchUnauthRejected(t *testing.T) {
	ts := newTestServer(t)
	resp, err := http.Get(ts.URL + "/api/schemas/caracal/tracks/aircraft/v1")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("unauth fetch: got %d, want 401", resp.StatusCode)
	}
	resp.Body.Close()
}

// --- F-158: protobuf-source upload ----------------------------------

// uploadSchemaProtoForm is like uploadSchemaForm but takes an explicit
// filename so the test can pin the protoc-style <file>:<line>:<col>
// diagnostic shape returned on syntax errors.
func uploadSchemaProtoForm(t *testing.T, file []byte, filename, version, compatibility, format string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if version != "" {
		_ = w.WriteField("version", version)
	}
	if compatibility != "" {
		_ = w.WriteField("compatibility", compatibility)
	}
	if format != "" {
		_ = w.WriteField("format", format)
	}
	fw, err := w.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := fw.Write(file); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	return &buf, w.FormDataContentType()
}

// TestSchema_UploadProtoSource — happy path. Vendor uploads .proto
// source; portal compiles to a FileDescriptorSet and persists under
// the existing protobuf-fds layout. Response shows format=protobuf-fds
// (clients see no difference); fetched bytes parse as a
// FileDescriptorSet containing google/protobuf/timestamp.proto — proves
// the --include_imports equivalence the decoder depends on.
func TestSchema_UploadProtoSource(t *testing.T) {
	ts := newTestServer(t)
	c := &http.Client{}
	// testdata/valid.proto defines DenObservation; register a topic
	// whose declared format matches so the write-time format ↔ FDS
	// validator accepts the compiled upload.
	apiKey, topicID, keyPattern := registerVendorAndTopicWithFormat(t, ts.URL, c, "protobuf:DenObservation")

	src, err := os.ReadFile("testdata/valid.proto")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	body, ctype := uploadSchemaProtoForm(t, src, "den.proto", "v1", "backward", "protobuf-source")
	url := ts.URL + "/api/vendors/caracal/topics/" + itoa(topicID) + "/schemas"
	resp := doUpload(t, c, url, apiKey, body, ctype)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("upload .proto: %d body=%s", resp.StatusCode, mustReadBody(t, resp))
	}
	entry := mustReadJSON(t, resp)
	if entry["format"] != "protobuf-fds" {
		t.Errorf("persisted format = %v, want protobuf-fds (.proto source is compiled on ingress)", entry["format"])
	}

	// Fetch the compiled bytes and assert they parse as a
	// FileDescriptorSet AND carry the well-known-type import.
	resp, err = doFetch(c, ts.URL+"/api/schemas/"+keyPattern, apiKey)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("fetch: %d body=%s", resp.StatusCode, mustReadBody(t, resp))
	}
	gotBytes, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	var fds descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(gotBytes, &fds); err != nil {
		t.Fatalf("fetched bytes are not a FileDescriptorSet: %v", err)
	}
	var foundUser, foundWKT bool
	for _, f := range fds.File {
		switch f.GetName() {
		case "den.proto":
			foundUser = true
		case "google/protobuf/timestamp.proto":
			foundWKT = true
		}
	}
	if !foundUser {
		t.Errorf("persisted FileDescriptorSet missing user file den.proto; got %d files", len(fds.File))
	}
	if !foundWKT {
		t.Errorf("persisted FileDescriptorSet missing google/protobuf/timestamp.proto — --include_imports equivalence broken")
	}
}

// TestSchema_UploadProtoSource_SyntaxError — `mesage` typo. Handler
// returns 400 with body error message carrying the protoc-style
// <file>:<line>:<col> prefix so the vendor sees the same diagnostic
// they'd see from protoc locally.
func TestSchema_UploadProtoSource_SyntaxError(t *testing.T) {
	ts := newTestServer(t)
	c := &http.Client{}
	apiKey, topicID, _ := registerVendorAndTopic(t, ts.URL, c)

	src, err := os.ReadFile("testdata/syntax_error.proto")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	body, ctype := uploadSchemaProtoForm(t, src, "broken.proto", "v1", "none", "protobuf-source")
	url := ts.URL + "/api/vendors/caracal/topics/" + itoa(topicID) + "/schemas"
	resp := doUpload(t, c, url, apiKey, body, ctype)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("syntax-error upload: got %d, want 400 body=%s", resp.StatusCode, mustReadBody(t, resp))
	}
	bodyJSON := mustReadJSON(t, resp)
	errObj, _ := bodyJSON["error"].(map[string]any)
	if errObj == nil {
		t.Fatalf("response body has no error object: %+v", bodyJSON)
	}
	if code, _ := errObj["code"].(string); code != "compile" {
		t.Errorf("error code = %q, want compile", code)
	}
	msg, _ := errObj["message"].(string)
	if !strings.Contains(msg, "broken.proto:") {
		t.Errorf("error message missing <file>:<line>:<col> prefix: %q", msg)
	}
}

// TestSchema_UploadProtoSource_UnsupportedImport — `.proto` importing a
// path the WKT resolver doesn't know about. Vendor sees a clean error
// naming the missing path; they can decide to either inline the import
// or fall back to the .fds upload path with their own assembled
// descriptor set.
func TestSchema_UploadProtoSource_UnsupportedImport(t *testing.T) {
	ts := newTestServer(t)
	c := &http.Client{}
	apiKey, topicID, _ := registerVendorAndTopic(t, ts.URL, c)

	src, err := os.ReadFile("testdata/unsupported_import.proto")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	body, ctype := uploadSchemaProtoForm(t, src, "imp.proto", "v1", "none", "protobuf-source")
	url := ts.URL + "/api/vendors/caracal/topics/" + itoa(topicID) + "/schemas"
	resp := doUpload(t, c, url, apiKey, body, ctype)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unsupported-import upload: got %d, want 400 body=%s", resp.StatusCode, mustReadBody(t, resp))
	}
	bodyJSON := mustReadJSON(t, resp)
	errObj, _ := bodyJSON["error"].(map[string]any)
	msg, _ := errObj["message"].(string)
	if !strings.Contains(msg, "vendor/other.proto") {
		t.Errorf("error message doesn't name missing import vendor/other.proto: %q", msg)
	}
}

// TestSchema_UploadProtoSource_TooLarge — vendor uploads a .proto source
// larger than schemastore.MaxFileSize. The load-bearing property is
// that the compiler is NEVER invoked on over-quota input: a vendor who
// uploads a giant blob pays zero compile cost, and the failure surface
// is a clean 4xx ("too large") rather than a compiler error message
// that would leak parser internals or waste handler time. The exact
// status code depends on which guard catches it first — Go's
// multipart-parse MaxBytesReader returns 400 with "request body too
// large", while a stream that gets past the parser hits the
// schemastore-cap check and returns 413. Both are correct; the test
// asserts the union behavior (4xx + body says "too large") and
// explicitly NOT compiler-style line/col diagnostics.
func TestSchema_UploadProtoSource_TooLarge(t *testing.T) {
	ts := newTestServer(t)
	c := &http.Client{}
	apiKey, topicID, _ := registerVendorAndTopic(t, ts.URL, c)

	// Body deliberately ~2x cap so EITHER guard trips, never the
	// compiler. Comments are irrelevant to the size guard but make the
	// file shape look like a real .proto in case any future check is
	// content-aware.
	hdr := []byte("syntax = \"proto3\";\npackage vendor.big;\n\nmessage Big {\n")
	pad := bytes.Repeat([]byte("// padding line — irrelevant to the size guard\n"), 24000)
	tail := []byte("  string id = 1;\n}\n")
	src := append(append(hdr, pad...), tail...)
	if int64(len(src)) <= 1<<20 {
		t.Fatalf("fixture too small: %d bytes (want > 1 MiB)", len(src))
	}

	body, ctype := uploadSchemaProtoForm(t, src, "big.proto", "v1", "backward", "protobuf-source")
	url := ts.URL + "/api/vendors/caracal/topics/" + itoa(topicID) + "/schemas"
	resp := doUpload(t, c, url, apiKey, body, ctype)
	defer resp.Body.Close()
	respBody := mustReadBody(t, resp)

	if resp.StatusCode != http.StatusRequestEntityTooLarge && resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 413 or 400, got %d body=%s", resp.StatusCode, respBody)
	}
	lowered := strings.ToLower(respBody)
	if !strings.Contains(lowered, "too large") {
		t.Errorf("expected 'too large' in body, got %s", respBody)
	}
	// Negative assertion: compiler diagnostics carry `<file>:<line>:<col>:`.
	// If we see one of those, the compile path ran on the giant body and
	// the size guard didn't do its job.
	if strings.Contains(respBody, "big.proto:") {
		t.Errorf("compile-style diagnostic in body — compiler was invoked on over-quota input: %s", respBody)
	}
}

// --- helpers ---------------------------------------------------------

func doFetch(c *http.Client, url, bearer string) (*http.Response, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	return c.Do(req)
}

func itoa(n int64) string {
	// Keep dep surface tight — we don't need strconv just for this.
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// silence imports when refactored.
var _ = strings.HasPrefix
var _ = json.Marshal
