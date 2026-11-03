package api_test

// The error shape on the wire: RFC 9457 problem details on every 4xx and
// 5xx the portal writes, the standard library's 404 and 405 included, with
// the legacy error object still inside.

import (
	"net/http"
	"strings"
	"testing"
)

func doRaw(t *testing.T, c *http.Client, method, url, bearer string, hdr map[string]string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	return resp
}

// assertProblem checks the headers and members every problem must carry.
func assertProblem(t *testing.T, resp *http.Response, status int, code string) map[string]any {
	t.Helper()
	if resp.StatusCode != status {
		t.Fatalf("status = %d, want %d", resp.StatusCode, status)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/problem+json; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
	if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q", got)
	}
	if got := resp.Header.Get("Api-Version"); got != "0.2.0" {
		t.Errorf("Api-Version = %q", got)
	}
	body := mustReadJSON(t, resp)
	if body["status"] != float64(status) || body["code"] != code {
		t.Errorf("status/code = %v/%v, want %d/%s", body["status"], body["code"], status, code)
	}
	if body["type"] != "urn:goat:portal:problem:"+code {
		t.Errorf("type = %v", body["type"])
	}
	if body["title"] != http.StatusText(status) {
		t.Errorf("title = %v", body["title"])
	}
	if body["instance"] != resp.Header.Get("X-Request-Id") || body["instance"] == "" {
		t.Errorf("instance = %v, X-Request-Id = %q", body["instance"], resp.Header.Get("X-Request-Id"))
	}
	legacy, _ := body["error"].(map[string]any)
	if legacy["code"] != code || legacy["message"] != body["detail"] {
		t.Errorf("legacy error object = %v, want code %s and the detail", legacy, code)
	}
	return body
}

func TestProblem_UnknownPathUnderAPI(t *testing.T) {
	ts := newTestServer(t)
	resp := doRaw(t, &http.Client{}, "GET", ts.URL+"/api/nope", "", nil)
	assertProblem(t, resp, http.StatusNotFound, "not_found")
}

func TestProblem_WrongMethodKeepsAllow(t *testing.T) {
	ts := newTestServer(t)
	resp := doRaw(t, &http.Client{}, "POST", ts.URL+"/api/health", "", nil)
	assertProblem(t, resp, http.StatusMethodNotAllowed, "method_not_allowed")
	if allow := resp.Header.Get("Allow"); !strings.Contains(allow, "GET") {
		t.Errorf("Allow = %q", allow)
	}
}

func TestProblem_StaticNotFoundIsUntouched(t *testing.T) {
	ts := newTestServer(t)
	resp := doRaw(t, &http.Client{}, "GET", ts.URL+"/no-such-page.html", "", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("static 404 Content-Type = %q, want the standard library's", ct)
	}
}

func TestProblem_Unauthenticated(t *testing.T) {
	ts := newTestServer(t)
	resp := doRaw(t, &http.Client{}, "GET", ts.URL+"/api/vendors", "", nil)
	assertProblem(t, resp, http.StatusUnauthorized, "auth")
}

func TestProblem_ValidationDetailIsKept(t *testing.T) {
	ts := newTestServer(t)
	c := &http.Client{}
	resp := doJSON(t, c, "POST", ts.URL+"/api/vendors", "dev-operator-key", map[string]any{
		"prefix": "Not Valid", "display_name": "x",
	})
	body := assertProblem(t, resp, http.StatusBadRequest, "validate")
	if d, _ := body["detail"].(string); !strings.Contains(d, "prefix") {
		t.Errorf("4xx detail lost the validation message: %q", d)
	}
}

func TestProblem_ApiVersionOtherMajorRefusedWithoutEcho(t *testing.T) {
	ts := newTestServer(t)
	c := &http.Client{}
	resp := doRaw(t, c, "GET", ts.URL+"/api/health", "", map[string]string{"Api-Version": "9.7.junk"})
	body := assertProblem(t, resp, http.StatusBadRequest, "unsupported_api_version")
	if d, _ := body["detail"].(string); strings.Contains(d, "9.7") || !strings.Contains(d, "0.2.0") {
		t.Errorf("detail = %q, want the served version and not the supplied one", d)
	}
	resp = doRaw(t, c, "GET", ts.URL+"/api/health", "", map[string]string{"Api-Version": "0.9"})
	if resp.StatusCode != http.StatusOK {
		t.Errorf("same major refused: %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestProblem_EmptyTopicPatchRefused(t *testing.T) {
	ts := newTestServer(t)
	c := &http.Client{}
	apiKey, topicID, _ := registerVendorAndTopic(t, ts.URL, c)
	resp := doJSON(t, c, "PATCH", ts.URL+"/api/vendors/caracal/topics/"+itoa(topicID), apiKey, map[string]any{})
	body := assertProblem(t, resp, http.StatusBadRequest, "validate")
	if d, _ := body["detail"].(string); !strings.Contains(d, "at least one mutable field") {
		t.Errorf("detail = %q", d)
	}
}

func TestProblem_FieldCapNamesTheFieldNotTheValue(t *testing.T) {
	ts := newTestServer(t)
	c := &http.Client{}
	long := strings.Repeat("x", 201)
	resp := doJSON(t, c, "POST", ts.URL+"/api/vendors", "dev-operator-key", map[string]any{
		"prefix": "capped", "display_name": long,
	})
	body := assertProblem(t, resp, http.StatusBadRequest, "validate")
	d, _ := body["detail"].(string)
	if !strings.Contains(d, "display_name must be at most 200") || strings.Contains(d, long) {
		t.Errorf("detail = %q", d)
	}
}
