package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A template that fails at execute time must not ship the half it managed to
// write. render executed straight into the ResponseWriter, so the page went
// out as a 200 cut off at the failing field, and the error reached only the
// log (#1491). The layout here writes a marker, then reads a field pageVars
// does not have.
func TestRender_executeErrorIsA500NotATruncatedPage(t *testing.T) {
	s := newOnboardTestServer(t)
	base, err := loadTemplates()
	if err != nil {
		t.Fatalf("loadTemplates: %v", err)
	}
	// Keep the template New returns: html/template resets the replaced
	// "layout" (base itself) to an empty template without the func map.
	broken, err := base.New("layout").Parse(`<html><body>PARTIAL-PAGE-MARKER {{.NoSuchField}}</body></html>`)
	if err != nil {
		t.Fatalf("parse broken layout: %v", err)
	}
	s.Templates = broken

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/onboarding", nil)
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("broken template: want 500, got %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "PARTIAL-PAGE-MARKER") {
		t.Fatal("the half-rendered page reached the client")
	}
	if strings.Contains(body, "NoSuchField") {
		t.Fatal("the template error text reached the client; it belongs in the log")
	}
	if !strings.Contains(body, "failed to render") {
		t.Fatalf("500 body does not say the page failed to render: %q", body)
	}
}
