package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/config"
)

// testServerWithSiblings constructs the bare minimum Server needed to
// exercise handleSiblings — no registry / zenoh / kratos required.
func testServerWithSiblings(t *testing.T, panoscope, stethoscope, flashlight string) *Server {
	t.Helper()
	return &Server{
		cfg: &config.Config{
			SiblingPanoscopeURL:   panoscope,
			SiblingStethoscopeURL: stethoscope,
			SiblingFlashlightURL:  flashlight,
		},
	}
}

func TestHandleSiblings_AllUnset(t *testing.T) {
	s := testServerWithSiblings(t, "", "", "")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/siblings", nil)
	s.handleSiblings(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got siblingsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Panoscope != "" || got.Stethoscope != "" || got.Flashlight != "" {
		t.Errorf("unconfigured deploy should serve empty strings; got %+v", got)
	}
}

func TestHandleSiblings_StripsTrailingSlash(t *testing.T) {
	// Operators frequently include a trailing slash in URL env vars;
	// the server normalizes so the JS can always append `/?key=...`
	// without producing a double slash.
	s := testServerWithSiblings(t,
		"https://panoscope.example/",
		"https://stethoscope.example/",
		"https://flashlight.example/",
	)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/siblings", nil)
	s.handleSiblings(rec, req)
	var got siblingsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Panoscope != "https://panoscope.example" {
		t.Errorf("panoscope trailing slash not stripped: %q", got.Panoscope)
	}
	if got.Stethoscope != "https://stethoscope.example" {
		t.Errorf("stethoscope trailing slash not stripped: %q", got.Stethoscope)
	}
	if got.Flashlight != "https://flashlight.example" {
		t.Errorf("flashlight trailing slash not stripped: %q", got.Flashlight)
	}
}

func TestHandleSiblings_PartialDeployment(t *testing.T) {
	// Block 94 contract: a partial deploy (e.g. portal + panoscope
	// but no stethoscope yet) renders only the configured buttons.
	s := testServerWithSiblings(t,
		"https://panoscope.example",
		"", // no stethoscope
		"https://flashlight.example",
	)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/siblings", nil)
	s.handleSiblings(rec, req)
	var got siblingsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Stethoscope != "" {
		t.Errorf("stethoscope should be empty; got %q", got.Stethoscope)
	}
	if got.Panoscope == "" || got.Flashlight == "" {
		t.Errorf("configured siblings should pass through; got %+v", got)
	}
}
