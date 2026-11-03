package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/kratos"
)

func TestCandidateInvitePages(t *testing.T) {
	for _, mode := range []string{"form", "complete", "invalid", "upstream", "recovery"} {
		t.Run(mode, func(t *testing.T) { testCandidateInvitePage(t, mode) })
	}
}

func testCandidateInvitePage(t *testing.T, mode string) {
	t.Helper()
	s := newOnboardTestServer(t)
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if mode == "upstream" || (mode == "recovery" && r.URL.Path == "/admin/recovery/code") {
			http.Error(w, "private kratos:4434 detail", http.StatusServiceUnavailable)
			return
		}
		switch r.URL.Path {
		case "/admin/identities":
			_, _ = w.Write([]byte(`{"id":"candidate-id","traits":{"email":"candidate@example.invalid"}}`))
		case "/admin/recovery/code":
			_, _ = w.Write([]byte(`{"recovery_link":"https://login.example.invalid/recover?flow=test","recovery_code":"123456","expires_at":"2030-01-01T00:00:00Z"}`))
		default:
			t.Errorf("unexpected identity request: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer fake.Close()
	s.Kratos = map[string]*kratos.Client{"tenant": kratos.NewClient(fake.URL)}
	inv, err := s.Invites.Create("tenant", "sponsor@example.invalid", "", []string{"OBSERVERS"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	token := inv.Token
	method := http.MethodGet
	if mode == "invalid" {
		token = "missing"
	}
	if mode == "complete" || mode == "upstream" || mode == "recovery" {
		method = http.MethodPost
	}
	req := httptest.NewRequestWithContext(context.Background(), method, "/invites/"+token+"/accept", strings.NewReader("email=candidate%40example.invalid&name=Candidate"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, req)
	wantStatus := 200
	if mode == "invalid" {
		wantStatus = 404
	}
	if mode == "upstream" || mode == "recovery" {
		wantStatus = 502
	}
	if rec.Code != wantStatus {
		t.Fatalf("status=%d want=%d: %s", rec.Code, wantStatus, rec.Body.String())
	}
	body := rec.Body.String()
	if mode == "recovery" && !strings.Contains(body, "Your account was created") {
		t.Error("recovery failure must explain account was created")
	}
	for _, forbidden := range []string{"<nav", "stepup-strip", "All users", "User detail", "kratos:4434"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("candidate page contains %q", forbidden)
		}
	}
	if !strings.Contains(body, "<main") {
		t.Error("candidate response has no page layout")
	}
	if mode == "complete" && !strings.Contains(body, `href="https://login.example.invalid/recover?flow=test"`) {
		t.Error("recovery link is not clickable")
	}
	if mode == "complete" && !strings.Contains(body, "123456") {
		t.Error("required recovery code missing")
	}
}
