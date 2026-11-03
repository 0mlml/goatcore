package handlers

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/audit"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/config"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/onboardstore"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/permissions"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/pgtest"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/store"
)

func newSSHKeysAPITestServer(t *testing.T) *Server {
	t.Helper()
	ob, err := onboardstore.Open(pgtest.DSN(t))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = ob.Close() })
	cfg := &config.Config{
		OnboardOperatorAPIKeys: map[string]string{"runner-key": "onboard-runner"},
		IdPs:                   map[string]config.IdP{"tenant": {Key: "tenant", Label: "Tenant"}},
		IdPOrder:               []string{"tenant"},
		DefaultIdP:             "tenant",
	}
	perms := &permissions.Matrix{
		SchemaVersion: 1,
		Groups: []permissions.Group{
			{Name: "ADMINS", Grants: []permissions.Grant{{Service: "id-admin-console", Access: "admin"}}},
		},
	}
	srv, err := NewServer(cfg, nil, nil, store.NewMemory(), audit.NewStderrEmitter(50), perms, ob)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return srv
}

func submitKey(t *testing.T, srv *Server, handle string) string {
	t.Helper()
	k, err := srv.Onboard.SubmitSSHKey(context.Background(), &onboardstore.SSHKey{
		Handle: handle, KeyType: "ssh-ed25519", KeyBody: "AAAAC3NzaC1lZDI1NTE5AAAAIGo4gB7f5W3VlqYwV0F6+K6m0I7w+oO2p3jP1B6RkF1q", Comment: "test",
	}, 90)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	return k.ID
}

func TestApiReportSSHKeySkip_RejectsMissingAuthKey(t *testing.T) {
	srv := newSSHKeysAPITestServer(t)
	h := srv.Routes()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/onboard/ssh-keys/sshk-1/skip-report", strings.NewReader(`{"reason":"no_manifest"}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestApiReportSSHKeySkip_RequiresValidEnum(t *testing.T) {
	srv := newSSHKeysAPITestServer(t)
	h := srv.Routes()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/onboard/ssh-keys/sshk-1/skip-report", strings.NewReader(`{"reason":"typo"}`))
	req.Header.Set("Authorization", "Bearer runner-key")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestApiReportSSHKeySkip_RefusesParticipantPrincipal(t *testing.T) {
	srv := newSSHKeysAPITestServer(t)
	// The gate resolves participants via portal delegation, which this test
	// doesn't stand up — inject the resolved principal directly (same
	// package) and call the handler the way the gate would.
	ctx := context.WithValue(context.Background(), ctxKeyOnboardPrincipal, onboardPrincipal{Handle: "partner-sponsor", IsOperator: false})
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/onboard/ssh-keys/sshk-1/skip-report", strings.NewReader(`{"reason":"no_manifest"}`))
	req.SetPathValue("id", "sshk-1")
	rec := httptest.NewRecorder()
	srv.apiReportSSHKeySkip(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("participant principal: status = %d, want 403", rec.Code)
	}
}

func TestApiReportSSHKeySkip_RejectsMalformedContext(t *testing.T) {
	srv := newSSHKeysAPITestServer(t)
	h := srv.Routes()
	for name, body := range map[string]string{
		"env not a slug":   `{"reason":"no_manifest","env":"ZAGROS PROD!"}`,
		"group not a role": `{"reason":"no_scope","groups":["ADMIN","<script>"]}`,
		"too many groups":  `{"reason":"no_scope","groups":[` + strings.Repeat(`"G",`, 32) + `"G"]}`,
	} {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/onboard/ssh-keys/sshk-1/skip-report", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer runner-key")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", name, rec.Code)
		}
	}
}

func TestApiReportSSHKeySkip_NotFound(t *testing.T) {
	srv := newSSHKeysAPITestServer(t)
	h := srv.Routes()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/onboard/ssh-keys/sshk-ghost/skip-report", strings.NewReader(`{"reason":"no_manifest"}`))
	req.Header.Set("Authorization", "Bearer runner-key")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestApiReportSSHKeySkip_RefusesWrongState(t *testing.T) {
	srv := newSSHKeysAPITestServer(t)
	id := submitKey(t, srv, "tuser")
	h := srv.Routes()

	// A submitted key should be refused
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/onboard/ssh-keys/"+id+"/skip-report", strings.NewReader(`{"reason":"no_scope"}`))
	req.Header.Set("Authorization", "Bearer runner-key")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "submitted") {
		t.Errorf("409 response should name the current state, got: %s", rec.Body.String())
	}
}

func TestApiReportSSHKeySkip_Success(t *testing.T) {
	srv := newSSHKeysAPITestServer(t)
	id := submitKey(t, srv, "tuser")
	if err := srv.Onboard.ApproveSSHKey(context.Background(), id, "admin@x"); err != nil {
		t.Fatalf("approve: %v", err)
	}

	h := srv.Routes()
	body, _ := json.Marshal(map[string]interface{}{"reason": "no_manifest", "env": "prod", "groups": []string{"g1"}})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/onboard/ssh-keys/"+id+"/skip-report", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer runner-key")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}

	// Verify state wasn't changed but annotation was saved
	k, _ := srv.Onboard.SSHKeyByID(context.Background(), id)
	if k.Status != onboardstore.SSHKeyApproved {
		t.Errorf("status = %s, want %s", k.Status, onboardstore.SSHKeyApproved)
	}
	if k.SkipReason != "no_manifest" || k.SkipReportedBy != "onboard-runner" {
		t.Errorf("skip annotation not stored correctly: %+v", k)
	}
}

// wireEd25519Line builds a parse-valid authorized_keys line: the body's
// embedded type string must match the declared one (ParseSSHPublicKey checks).
func wireEd25519Line() string {
	kt := "ssh-ed25519"
	wire := []byte{0, 0, 0, byte(len(kt))}
	wire = append(wire, kt...)
	wire = append(wire, 0, 0, 0, 32)
	wire = append(wire, make([]byte, 32)...)
	return kt + " " + base64.StdEncoding.EncodeToString(wire) + " test@backfill"
}

func TestApiSubmitSSHKey_TrimsEnvAndEmail(t *testing.T) {
	srv := newSSHKeysAPITestServer(t)
	h := srv.Routes()
	body, _ := json.Marshal(map[string]interface{}{
		"handle": "tuser",
		"pubkey": wireEd25519Line(),
		"env":    " zagros ",
		"email":  " user@example.com ",
	})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/onboard/ssh-keys", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer runner-key")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201, body: %s", rec.Code, rec.Body.String())
	}
	var k onboardstore.SSHKey
	if err := json.Unmarshal(rec.Body.Bytes(), &k); err != nil {
		t.Fatalf("decode: %v", err)
	}
	stored, err := srv.Onboard.SSHKeyByID(context.Background(), k.ID)
	if err != nil {
		t.Fatalf("SSHKeyByID: %v", err)
	}
	// The installer's per-row env gate compares byte-for-byte; an untrimmed
	// env would wedge the row as env_mismatch on every run forever.
	if stored.Env != "zagros" {
		t.Errorf("Env = %q, want trimmed %q", stored.Env, "zagros")
	}
	if stored.Email != "user@example.com" {
		t.Errorf("Email = %q, want trimmed %q", stored.Email, "user@example.com")
	}
}

func TestApiSubmitSSHKey_RejectsNonSlugEnv(t *testing.T) {
	srv := newSSHKeysAPITestServer(t)
	h := srv.Routes()
	body, _ := json.Marshal(map[string]interface{}{
		"handle": "tuser",
		"pubkey": wireEd25519Line(),
		"env":    "ZAGROS PROD!",
	})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/onboard/ssh-keys", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer runner-key")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400, body: %s", rec.Code, rec.Body.String())
	}
}

func TestApiReportSSHKeySkip_AcceptsEnvMismatch(t *testing.T) {
	srv := newSSHKeysAPITestServer(t)
	id := submitKey(t, srv, "tuser")
	if err := srv.Onboard.ApproveSSHKey(context.Background(), id, "admin@x"); err != nil {
		t.Fatalf("approve: %v", err)
	}

	h := srv.Routes()
	body, _ := json.Marshal(map[string]interface{}{"reason": "env_mismatch", "env": "prod"})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/onboard/ssh-keys/"+id+"/skip-report", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer runner-key")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	k, _ := srv.Onboard.SSHKeyByID(context.Background(), id)
	if k.SkipReason != "env_mismatch" {
		t.Errorf("skip annotation not stored: %+v", k)
	}
}
