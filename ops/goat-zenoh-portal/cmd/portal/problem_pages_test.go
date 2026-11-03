package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func problemPagesServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle("GET /", problemPageHandler())
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

func TestProblemPages_429CarriesRetryAfter(t *testing.T) {
	ts := problemPagesServer(t)
	resp, err := http.Get(ts.URL + "/429.json")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests || resp.Header.Get("Retry-After") != "1" {
		t.Errorf("status %d, Retry-After %q", resp.StatusCode, resp.Header.Get("Retry-After"))
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/problem+json; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q", cc)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("body: %v", err)
	}
	if body["status"] != float64(429) || body["code"] != "rate_limited" || body["title"] != "Too Many Requests" {
		t.Errorf("body = %v", body)
	}
	legacy, _ := body["error"].(map[string]any)
	if legacy["code"] != "rate_limited" || legacy["message"] != body["detail"] {
		t.Errorf("legacy error = %v", legacy)
	}
}

func TestProblemPages_UnknownStatusIsAConstant404(t *testing.T) {
	ts := problemPagesServer(t)
	for _, path := range []string{"/0.json", "/101.json", "/999.json", "/200.json", "/<script>.json", "/", "/x/y"} {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		body := string(raw)
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", path, resp.StatusCode)
		}
		if resp.Header.Get("Retry-After") != "" || strings.Contains(body, "script") {
			t.Errorf("%s: Retry-After %q body %s", path, resp.Header.Get("Retry-After"), body)
		}
	}
}
