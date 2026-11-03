//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/audit"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/config"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/handlers"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/kratos"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/onboardstore"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/permissions"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/pgtest"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/store"
)

// newIntegrationServer wires a minimal real goat-id-console Server instance
// backed by in-memory stores, used to prove the integration tests bind to the
// production handlers rather than local stand-ins.
func newIntegrationServer(t *testing.T) *handlers.Server {
	t.Helper()

	ob, err := onboardstore.Open(pgtest.DSN(t))
	if err != nil {
		t.Fatalf("open onboard store: %v", err)
	}
	t.Cleanup(func() { _ = ob.Close() })

	cfg := &config.Config{
		IdPs: map[string]config.IdP{
			"tenant": {
				Key:   "tenant",
				Label: "Tenant",
			},
		},
		IdPOrder:        []string{"tenant"},
		DefaultIdP:      "tenant",
		StepUpEnforced:  true,
		AAL2FreshWindow: 5 * time.Minute,
		StepUpSSOIdP:    "tenant",
		NotifyGoatnet:   "zagros",
	}
	perms := &permissions.Matrix{
		SchemaVersion: 1,
		Groups: []permissions.Group{
			{Name: "ADMINS", Grants: []permissions.Grant{{Service: "id-admin-console", Access: "admin"}}},
			{Name: "GRAFANA-READONLY", Grants: []permissions.Grant{{Service: "grafana", Access: "read"}}},
		},
	}

	srv, err := handlers.NewServer(cfg, map[string]*kratos.Client{}, nil,
		store.NewMemory(), audit.NewStderrEmitter(50), perms, ob)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return srv
}

func requireRealKratos(t *testing.T) string {
	t.Helper()
	adminURL := os.Getenv("IDP_TENANT_KRATOS_ADMIN_URL")
	if adminURL == "" {
		t.Skip("IDP_TENANT_KRATOS_ADMIN_URL not set; Kratos stack not running")
	}
	if err := RequireLoopback(map[string]string{"IDP_TENANT_KRATOS_ADMIN_URL": adminURL}); err != nil {
		t.Fatalf("pre-flight guard refused the configured endpoint: %v", err)
	}
	return adminURL
}

func createRawIdentity(t *testing.T, adminURL string, payload map[string]interface{}) kratos.Identity {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		adminURL+"/admin/identities", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create identity: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create identity status = %d, want 201", resp.StatusCode)
	}
	var identity kratos.Identity
	if err := json.NewDecoder(resp.Body).Decode(&identity); err != nil {
		t.Fatalf("decode created identity: %v", err)
	}
	return identity
}

// Kratos v1.3.1 itself is the authority for the non-standard but documented
// EnsurePathExistsOnAdd behavior used by PatchGroups. These cases prove the
// child-path add preserves sibling metadata and creates an absent or null
// parent/key while always storing an array.
func TestIntegration_PatchGroupsRealKratosV131(t *testing.T) {
	adminURL := requireRealKratos(t)
	cli := kratos.NewClient(adminURL)

	cases := []struct {
		name string
		meta interface{}
	}{
		{name: "migrated", meta: map[string]interface{}{
			"groups": []string{"PLATFORM-ADMIN"},
			"org":    map[string]interface{}{"short_tag": "acme"},
			"aff":    map[string]interface{}{"id": "acme", "kind": "partner"},
			"vendor": map[string]interface{}{"short_tag": "legacy"},
			"other":  "keep-me",
		}},
		{name: "absent parent"},
		{name: "null parent", meta: nil},
		{name: "absent groups", meta: map[string]interface{}{"other": "keep-me"}},
		{name: "null groups", meta: map[string]interface{}{"groups": nil, "other": "keep-me"}},
	}

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := map[string]interface{}{
				"schema_id": "default",
				"traits":    map[string]interface{}{"email": fmt.Sprintf("groups-%d-%d@example.test", i, time.Now().UnixNano())},
			}
			if tc.name != "absent parent" {
				payload["metadata_public"] = tc.meta
			}
			created := createRawIdentity(t, adminURL, payload)
			t.Cleanup(func() { _ = cli.DeleteIdentity(context.Background(), created.ID) })

			updated, err := cli.PatchGroups(context.Background(), created.ID, nil)
			if err != nil {
				t.Fatalf("PatchGroups: %v", err)
			}
			groups, exists := updated.MetadataPublic["groups"]
			if !exists {
				t.Fatalf("metadata_public.groups absent after patch: %v", updated.MetadataPublic)
			}
			if got, ok := groups.([]interface{}); !ok || len(got) != 0 {
				t.Fatalf("metadata_public.groups = %#v, want []", groups)
			}
			if tc.name == "migrated" {
				for _, key := range []string{"org", "aff", "vendor", "other"} {
					if updated.MetadataPublic[key] == nil {
						t.Fatalf("metadata_public.%s was lost: %v", key, updated.MetadataPublic)
					}
				}
			}
		})
	}
}

func TestIntegration_OryGroupMirrorRealKratosV131(t *testing.T) {
	adminURL := requireRealKratos(t)
	cli := kratos.NewClient(adminURL)
	created, err := cli.CreateIdentity(context.Background(), kratos.CreateIdentityRequest{
		Traits: map[string]interface{}{"email": fmt.Sprintf("groups-mirror-%d@example.test", time.Now().UnixNano())},
		MetadataPublic: map[string]interface{}{
			"groups": []string{"MIRROR-SOURCE"}, "org": map[string]interface{}{"short_tag": "acme"}, "other": "keep-me",
		},
	})
	if err != nil {
		t.Fatalf("CreateIdentity: %v", err)
	}
	t.Cleanup(func() { _ = cli.DeleteIdentity(context.Background(), created.ID) })

	script := filepath.Join("..", "..", "..", "scripts", "ory-group-mirror.sh")
	for _, args := range [][]string{
		{"add", "MIRROR-SOURCE", "MIRROR-TARGET"},
		{"remove", "MIRROR-TARGET", "MIRROR-SOURCE"},
	} {
		cmd := exec.Command("bash", append([]string{script}, args...)...)
		cmd.Env = append(os.Environ(), "APPLY=1", "KRATOS_ADMIN="+adminURL)
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("ory-group-mirror.sh %s: %v\n%s", strings.Join(args, " "), err, output)
		}
		if !strings.Contains(string(output), "groups now:") {
			t.Fatalf("ory-group-mirror.sh %s did not report persisted groups:\n%s", strings.Join(args, " "), output)
		}
	}

	stored, err := cli.GetIdentity(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetIdentity after mirror: %v", err)
	}
	if got := stored.Groups(); len(got) != 1 || got[0] != "MIRROR-TARGET" {
		t.Fatalf("persisted groups = %v, want [MIRROR-TARGET]", got)
	}
	if stored.MetadataPublic["org"] == nil || stored.MetadataPublic["other"] != "keep-me" {
		t.Fatalf("mirror clobbered sibling metadata: %v", stored.MetadataPublic)
	}
}

// Drive the same production route used by `goat admin` against real Kratos and
// verify the result with a persisted re-read. Session-revocation success and
// partial failure are covered by the handler tests with identities that have
// sessions; a newly created Kratos identity has none and returns 404 there.
func TestIntegration_ApiGroupsRouteRealKratosV131(t *testing.T) {
	adminURL := requireRealKratos(t)
	cli := kratos.NewClient(adminURL)
	email := fmt.Sprintf("groups-api-%d@example.test", time.Now().UnixNano())
	created, err := cli.CreateIdentity(context.Background(), kratos.CreateIdentityRequest{
		Traits: map[string]interface{}{"email": email},
		MetadataPublic: map[string]interface{}{
			"groups": []string{}, "org": map[string]interface{}{"short_tag": "acme"}, "other": "keep-me",
		},
	})
	if err != nil {
		t.Fatalf("CreateIdentity: %v", err)
	}
	t.Cleanup(func() { _ = cli.DeleteIdentity(context.Background(), created.ID) })

	srv := newIntegrationServer(t)
	srv.Kratos["tenant"] = cli
	srv.Cfg.OnboardOperatorAPIKeys = map[string]string{"integration-key": "integration-runner"}
	body, _ := json.Marshal(map[string]interface{}{
		"id": created.ID, "groups": []string{"GRAFANA-READONLY"}, "revoke_sessions": false,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/users/groups", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer integration-key")
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/users/groups status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	stored, err := cli.GetIdentity(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetIdentity after route: %v", err)
	}
	if got := stored.Groups(); len(got) != 1 || got[0] != "GRAFANA-READONLY" {
		t.Fatalf("persisted groups = %v, want [GRAFANA-READONLY]", got)
	}
	if stored.MetadataPublic["org"] == nil || stored.MetadataPublic["other"] != "keep-me" {
		t.Fatalf("route clobbered sibling metadata: %v", stored.MetadataPublic)
	}
}

// TestIntegration_HealthzUsesRealServer proves the integration suite links
// the real goat-id-console handlers by driving GET /healthz through a
// Server constructed via handlers.NewServer.
func TestIntegration_HealthzUsesRealServer(t *testing.T) {
	t.Parallel()

	srv := newIntegrationServer(t)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /healthz status = %d, want 200", rec.Code)
	}
}

// TestIntegration_HasSecondFactorWebauthnPlaceholderRealKratos exercises the
// real Kratos client and HasSecondFactor implementation against the ephemeral
// Kratos v1.3.1 stack started by CI. It creates a fresh identity via the
// admin API and asserts that the WebAuthn placeholder Kratos auto-creates for
// the email identifier does not count as a second factor.
func TestIntegration_HasSecondFactorWebauthnPlaceholderRealKratos(t *testing.T) {
	t.Parallel()

	adminURL := os.Getenv("IDP_TENANT_KRATOS_ADMIN_URL")
	if adminURL == "" {
		t.Skip("IDP_TENANT_KRATOS_ADMIN_URL not set; Kratos stack not running")
	}
	// This test CREATES and DELETES an identity, so it carries the same
	// pre-flight as its siblings in totp_test.go. Without it, a shell holding
	// a goatnet's admin URL turns `go test -tags=integration` into a write
	// against a live identity store — which is exactly what README.md says
	// the suite cannot do.
	if err := RequireLoopback(map[string]string{
		"IDP_TENANT_KRATOS_ADMIN_URL": adminURL,
	}); err != nil {
		t.Fatalf("pre-flight guard refused the configured endpoint: %v", err)
	}

	cli := kratos.NewClient(adminURL)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	email := fmt.Sprintf("placeholder-%d@example.test", time.Now().UnixNano())
	created, err := cli.CreateIdentity(ctx, kratos.CreateIdentityRequest{
		Traits: map[string]interface{}{"email": email},
	})
	if err != nil {
		t.Fatalf("CreateIdentity: %v", err)
	}
	t.Cleanup(func() { _ = cli.DeleteIdentity(context.Background(), created.ID) })

	fetched, err := cli.GetIdentity(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetIdentity: %v", err)
	}

	// Establish the precondition before asserting the conclusion. Without this
	// the test passes for a fresh identity that has NO webauthn credential at
	// all — false == false for a reason that has nothing to do with the
	// placeholder regression it claims to lock. The bug being regression-locked
	// is specifically "a webauthn credential exists, carries zero
	// enrolled keys, and must still not count as a second factor".
	cred, ok := fetched.Credentials["webauthn"]
	if !ok {
		t.Fatalf("precondition failed: identity %q has no webauthn credential. "+
			"Kratos v1.3.1 auto-creates a placeholder when the schema declares email a "+
			"webauthn identifier; without it this test cannot lock the placeholder "+
			"regression. Check the identity schema and GetIdentity's include_credential.",
			created.ID)
	}
	if cred.EnrolledKeys != 0 {
		t.Fatalf("precondition failed: webauthn credential on %q has %d enrolled key(s); "+
			"a placeholder must have zero", created.ID, cred.EnrolledKeys)
	}

	// The actual regression lock: placeholder present, zero keys, still not 2FA.
	if fetched.HasSecondFactor() {
		t.Fatalf("HasSecondFactor() = true for fresh identity %q; want false "+
			"(webauthn placeholder with 0 enrolled keys must not count)", created.ID)
	}
}
