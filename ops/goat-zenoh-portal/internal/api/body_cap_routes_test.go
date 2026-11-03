package api_test

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
)

// The four routes that decoded their own request body outside readJSON /
// decodeJSON (#1296). Each keeps the cap it had; what changes is the answer
// to an oversize body: the shared 413 problem document instead of a route-
// specific 400 or a silent truncation.

func postRaw(t *testing.T, url, bearer, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest("POST", url, bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatalf("new req: %v", err)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	return resp
}

func TestBodyCap_ProbeResultOversizeIs413(t *testing.T) {
	ts := newTestServer(t)
	// Over the route's own 64 KiB cap, under the 2 MiB middleware backstop.
	big := `{"result":{"note":"` + strings.Repeat("x", 80<<10) + `"}}`
	resp := postRaw(t, ts.URL+"/api/peering/probe-result", "", big)
	assertProblem(t, resp, http.StatusRequestEntityTooLarge, "payload_too_large")
}

func TestBodyCap_DiagnoseLogOversizeIs413(t *testing.T) {
	ts := newTestServer(t)
	// Over the route's own 512 KiB cap.
	big := `{"content":"` + strings.Repeat("x", 600<<10) + `"}`
	resp := postRaw(t, ts.URL+"/api/peering/diagnose-log", "", big)
	assertProblem(t, resp, http.StatusRequestEntityTooLarge, "payload_too_large")
}

func TestBodyCap_CodeExampleOversizeIs413(t *testing.T) {
	ts := newTestServer(t)
	c := &http.Client{}
	apiKey, topicID, _ := registerVendorAndTopic(t, ts.URL, c)
	url := ts.URL + "/api/vendors/caracal/topics/" + itoa(topicID) + "/examples/rust"
	// Over the route's 64 KiB + 4 KiB body cap: refused by the reader, before
	// the handler's own too_large check on the decoded code can run.
	big := `{"code":"` + strings.Repeat("x", 80<<10) + `"}`
	req, err := http.NewRequest("PUT", url, bytes.NewReader([]byte(big)))
	if err != nil {
		t.Fatalf("new req: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	assertProblem(t, resp, http.StatusRequestEntityTooLarge, "payload_too_large")
}

func TestBodyCap_MachineMeshKeyOversizeIs413(t *testing.T) {
	ts := newTestServer(t)
	// The body is read before the anchor lookup, so no enrolled machine is
	// needed to see the cap answer. Over the route's 64 KiB cap.
	big := `{"exercise":"` + strings.Repeat("x", 80<<10) + `"}`
	resp := postRaw(t, ts.URL+"/api/machines/no-such-anchor/mesh-key", "dev-operator-key", big)
	assertProblem(t, resp, http.StatusRequestEntityTooLarge, "payload_too_large")
}

func TestBodyCap_MachineMeshKeyBadJSONKeepsItsCode(t *testing.T) {
	ts := newTestServer(t)
	resp := postRaw(t, ts.URL+"/api/machines/no-such-anchor/mesh-key", "dev-operator-key", `{"exercise":`)
	assertProblem(t, resp, http.StatusBadRequest, "bad_json")
}
