package api_test

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// Happy path: a well-formed available prefix returns a json5 body that
// embeds the prefix in the header comment and the canonical TLS block.
func TestFabricZenohdConfig_Available(t *testing.T) {
	ts := newTestServer(t)
	resp, err := http.Get(ts.URL + "/api/fabric/zenohd-config?prefix=my-vendor")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("X-Prefix-Status"); got != "available" {
		t.Errorf("X-Prefix-Status: got %q want available", got)
	}
	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "application/json5") {
		t.Errorf("Content-Type: got %q want application/json5*", got)
	}
	body, _ := io.ReadAll(resp.Body)
	s := string(body)
	for _, want := range []string{
		"prefix=my-vendor",       // header comment substitution
		"enable_mtls: true",      // TLS block
		"verify_name_on_connect", // pinning
		"mode: \"router\"",       // router mode
	} {
		if !strings.Contains(s, want) {
			t.Errorf("body missing %q\n--- body ---\n%s", want, s)
		}
	}
}

func TestFabricZenohdConfig_MissingPrefix(t *testing.T) {
	ts := newTestServer(t)
	resp, err := http.Get(ts.URL + "/api/fabric/zenohd-config")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400", resp.StatusCode)
	}
}

func TestFabricZenohdConfig_InvalidPrefix(t *testing.T) {
	ts := newTestServer(t)
	for _, bad := range []string{"Foo", "with space", "-leading-dash", "trailing-", "way-too-long-vendor-prefix-that-exceeds-the-32-char-limit"} {
		resp, err := http.Get(ts.URL + "/api/fabric/zenohd-config?prefix=" + bad)
		if err != nil {
			t.Fatalf("GET prefix=%q: %v", bad, err)
		}
		got := resp.StatusCode
		resp.Body.Close()
		if got != http.StatusBadRequest {
			t.Errorf("prefix=%q: status got %d want 400", bad, got)
		}
	}
}

func TestFabricZenohdConfig_ReservedPrefix(t *testing.T) {
	ts := newTestServer(t)
	for _, reserved := range []string{"system", "attest"} {
		resp, err := http.Get(ts.URL + "/api/fabric/zenohd-config?prefix=" + reserved)
		if err != nil {
			t.Fatalf("GET prefix=%q: %v", reserved, err)
		}
		got := resp.StatusCode
		resp.Body.Close()
		if got != http.StatusConflict {
			t.Errorf("prefix=%q: status got %d want 409", reserved, got)
		}
	}
}
