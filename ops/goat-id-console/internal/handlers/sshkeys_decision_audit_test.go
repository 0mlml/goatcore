package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/audit"
)

func sshKeyDecisionAudit(t *testing.T, s *Server, action string) audit.Event {
	t.Helper()
	var matches []audit.Event
	for _, event := range s.Audit.Recent(50) {
		if event.Action == "ssh_key_"+action {
			matches = append(matches, event)
		}
	}
	if len(matches) != 1 {
		t.Fatalf("ssh_key_%s audit events = %d, want exactly 1", action, len(matches))
	}
	return matches[0]
}

func prepareSSHKeyDecision(t *testing.T, s *Server, action string) (string, string, string) {
	t.Helper()
	id := submitKey(t, s, "audit-user-"+action)
	if action == "installed" {
		if err := s.Onboard.ApproveSSHKey(context.Background(), id, "setup"); err != nil {
			t.Fatalf("prepare installed decision: %v", err)
		}
	}
	k, err := s.Onboard.SSHKeyByID(context.Background(), id)
	if err != nil {
		t.Fatalf("read prepared key: %v", err)
	}
	return id, k.Handle, k.Fingerprint
}

func assertSSHKeyAuditFacts(t *testing.T, event audit.Event, id, handle, fingerprint string, console bool) {
	t.Helper()
	for key, want := range map[string]string{"id": id, "handle": handle, "fingerprint": fingerprint} {
		if got, _ := event.After[key].(string); got != want {
			t.Errorf("audit %s = %q, want %q", key, got, want)
		}
	}
	if console {
		if got, _ := event.After["via"].(string); got != "console" {
			t.Errorf("audit via = %q, want console", got)
		}
	}
}

func TestSSHKeyAPIDecisionAuditKeepsImmutableKeyFacts(t *testing.T) {
	for _, action := range []string{"approve", "reject", "revoke", "installed"} {
		t.Run(action, func(t *testing.T) {
			s := newSSHKeysAPITestServer(t)
			id, handle, fingerprint := prepareSSHKeyDecision(t, s, action)
			req := httptest.NewRequestWithContext(context.Background(), http.MethodPost,
				"/api/onboard/ssh-keys/"+id+"/"+action, nil)
			req.Header.Set("Authorization", "Bearer runner-key")
			rec := httptest.NewRecorder()
			s.Routes().ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("%s status = %d, want 200: %s", action, rec.Code, rec.Body.String())
			}
			assertSSHKeyAuditFacts(t, sshKeyDecisionAudit(t, s, action), id, handle, fingerprint, false)
		})
	}
}

func TestSSHKeyAPIDecisionUnknownIDDoesNotMutateOrAudit(t *testing.T) {
	s := newSSHKeysAPITestServer(t)
	submitKey(t, s, "existing-user")
	before, err := s.Onboard.ListSSHKeys(context.Background(), "")
	if err != nil {
		t.Fatalf("list before: %v", err)
	}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost,
		"/api/onboard/ssh-keys/sshk-missing/approve", nil)
	req.Header.Set("Authorization", "Bearer runner-key")
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body.String())
	}
	after, err := s.Onboard.ListSSHKeys(context.Background(), "")
	if err != nil {
		t.Fatalf("list after: %v", err)
	}
	if len(after) != len(before) || after[0].Status != before[0].Status {
		t.Fatalf("unknown decision changed keys: before=%+v after=%+v", before, after)
	}
	if len(s.Audit.Recent(50)) != 0 {
		t.Fatalf("unknown decision emitted an audit event: %+v", s.Audit.Recent(50))
	}
}

func TestSSHKeyPageDecisionAuditKeepsImmutableKeyFacts(t *testing.T) {
	for _, action := range []string{"approve", "reject", "revoke"} {
		t.Run(action, func(t *testing.T) {
			s := newOnboardTestServer(t)
			id, handle, fingerprint := prepareSSHKeyDecision(t, s, action)
			form := url.Values{"csrf": {s.csrfNonce}}
			req := httptest.NewRequestWithContext(context.Background(), http.MethodPost,
				"/ssh-keys/"+id+"/"+action, strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rec := httptest.NewRecorder()
			s.Routes().ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("%s status = %d, want 200: %s", action, rec.Code, rec.Body.String())
			}
			assertSSHKeyAuditFacts(t, sshKeyDecisionAudit(t, s, action), id, handle, fingerprint, true)
		})
	}
}

func TestSSHKeyPageDecisionUnknownIDDoesNotMutateOrAudit(t *testing.T) {
	s := newOnboardTestServer(t)
	submitKey(t, s, "existing-user")
	before, err := s.Onboard.ListSSHKeys(context.Background(), "")
	if err != nil {
		t.Fatalf("list before: %v", err)
	}
	form := url.Values{"csrf": {s.csrfNonce}}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost,
		"/ssh-keys/sshk-missing/approve", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "that key no longer exists") {
		t.Fatalf("unknown key response = %d, want not-found flash: %s", rec.Code, rec.Body.String())
	}
	after, err := s.Onboard.ListSSHKeys(context.Background(), "")
	if err != nil {
		t.Fatalf("list after: %v", err)
	}
	if len(after) != len(before) || after[0].Status != before[0].Status {
		t.Fatalf("unknown decision changed keys: before=%+v after=%+v", before, after)
	}
	if len(s.Audit.Recent(50)) != 0 {
		t.Fatalf("unknown decision emitted an audit event: %+v", s.Audit.Recent(50))
	}
}
