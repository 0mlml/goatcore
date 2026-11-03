//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/onboardstore"
)

func TestIntegration_SSHKeysPanel(t *testing.T) {
	t.Parallel()

	// Mock portal for the sponsor lookup.
	portalSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/orgs/sponsorship") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		email := r.URL.Query().Get("email")
		if email == "sponsor@example.com" {
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(map[string]interface{}{
				"sponsor_authorized": true,
				"vendor":             "test-org",
				"alias":              "Test Org",
				"sponsor_max_tier":   "tier2",
			}); err != nil {
				http.Error(w, "could not encode response", http.StatusInternalServerError)
			}
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(portalSrv.Close)

	srv := newIntegrationServer(t)
	srv.Cfg.PortalInternalURL = portalSrv.URL
	srv.Cfg.PortalLookupToken = "test-token"

	// Create an approved key to ensure the queue depth is >0.
	ctx := context.Background()
	k, err := srv.Onboard.SubmitSSHKey(ctx, &onboardstore.SSHKey{
		Handle:  "user-queue",
		Email:   "user@example.com",
		Env:     "zagros",
		KeyType: "ssh-ed25519",
		KeyBody: "AAAAC3NzaC1lZDI1NTE5AAAAIFlqrvvrCRCKdT58z056yi4wpTZwpq9K/iGxL5H8cc1l",
	}, 90)
	if err != nil {
		t.Fatalf("SubmitSSHKey: %v", err)
	}
	if err := srv.Onboard.ApproveSSHKey(ctx, k.ID, "system"); err != nil {
		t.Fatalf("ApproveSSHKey: %v", err)
	}

	// An approved row for ANOTHER goatnet: the installer's per-row env gate
	// will never install it from this console, so the panel's queue depth
	// must not count it either.
	xk, err := srv.Onboard.SubmitSSHKey(ctx, &onboardstore.SSHKey{
		Handle:  "user-other-env",
		Email:   "user-other@example.com",
		Env:     "zagros-prod",
		KeyType: "ssh-ed25519",
		KeyBody: "AAAAC3NzaC1lZDI1NTE5AAAAIFlqrvvrCRCKdT58z056yi4wpTZwpq9K/iGxL5H8cc1l",
	}, 90)
	if err != nil {
		t.Fatalf("SubmitSSHKey: %v", err)
	}
	if err := srv.Onboard.ApproveSSHKey(ctx, xk.ID, "system"); err != nil {
		t.Fatalf("ApproveSSHKey: %v", err)
	}

	// Create an installed key for the admin.
	ak, err := srv.Onboard.SubmitSSHKey(ctx, &onboardstore.SSHKey{
		Handle:  "admin-installed",
		Email:   "admin-inst@example.com",
		Env:     "zagros",
		KeyType: "ssh-ed25519",
		KeyBody: "AAAAC3NzaC1lZDI1NTE5AAAAIFlqrvvrCRCKdT58z056yi4wpTZwpq9K/iGxL5H8cc1l",
	}, 90)
	if err != nil {
		t.Fatalf("SubmitSSHKey: %v", err)
	}
	if err := srv.Onboard.ApproveSSHKey(ctx, ak.ID, "system"); err != nil {
		t.Fatalf("ApproveSSHKey: %v", err)
	}
	if err := srv.Onboard.MarkSSHKeyInstalled(ctx, ak.ID, "system"); err != nil {
		t.Fatalf("MarkSSHKeyInstalled: %v", err)
	}

	// Create a backfilled admin key for testing the legacy backfill viewer class.
	// (installed row exists, but no proposal row)
	bk, err := srv.Onboard.SubmitSSHKey(ctx, &onboardstore.SSHKey{
		Handle:  "admin-backfilled",
		Email:   "admin-backfilled@example.com",
		Env:     "zagros",
		KeyType: "ssh-ed25519",
		KeyBody: "AAAAC3NzaC1lZDI1NTE5AAAAIFlqrvvrCRCKdT58z056yi4wpTZwpq9K/iGxL5H8cc1l",
	}, 90)
	if err != nil {
		t.Fatalf("SubmitSSHKey: %v", err)
	}
	if err := srv.Onboard.ApproveSSHKey(ctx, bk.ID, "system"); err != nil {
		t.Fatalf("ApproveSSHKey: %v", err)
	}
	if err := srv.Onboard.MarkSSHKeyInstalled(ctx, bk.ID, "system"); err != nil {
		t.Fatalf("MarkSSHKeyInstalled: %v", err)
	}

	cases := []struct {
		name          string
		user          string
		email         string
		groups        string
		wantCommands  bool
		wantHint      bool
		wantForbidden bool
	}{
		{
			name:          "Admin with installed row",
			user:          "admin-installed",
			email:         "admin-inst@example.com",
			groups:        "ADMINS",
			wantCommands:  true,
			wantHint:      false,
			wantForbidden: false,
		},
		{
			name:          "Admin without installed row",
			user:          "admin-no-inst",
			email:         "admin-no@example.com",
			groups:        "ADMINS",
			wantCommands:  false,
			wantHint:      true,
			wantForbidden: false,
		},
		{
			name:          "Sponsor admitted by onboardGate",
			user:          "sponsor-user",
			email:         "sponsor@example.com",
			groups:        "VENDORS",
			wantCommands:  false,
			wantHint:      false,
			wantForbidden: false, // Sponsor admitted but doesn't see UI
		},
		{
			name:          "Backfilled admin with no proposal row",
			user:          "admin-backfilled",
			email:         "admin-backfilled@example.com",
			groups:        "ADMINS",
			wantCommands:  true,
			wantHint:      false,
			wantForbidden: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/onboarding/ssh-keys", nil).WithContext(context.Background())
			req.Header.Set("X-Forwarded-User", tc.user)
			req.Header.Set("X-Forwarded-Email", tc.email)
			req.Header.Set("X-Forwarded-Groups", tc.groups)

			rec := httptest.NewRecorder()
			srv.Routes().ServeHTTP(rec, req)

			if tc.wantForbidden {
				if rec.Code != http.StatusForbidden {
					t.Errorf("got status %d, want %d", rec.Code, http.StatusForbidden)
				}
				return
			}

			if rec.Code != http.StatusOK {
				t.Errorf("got status %d, want 200", rec.Code)
			}

			body, _ := io.ReadAll(rec.Body)
			html := html.UnescapeString(string(body))
			t.Logf("HTML for %s:\n%s", tc.name, html)

			// Locate the install command block, if any.
			preStart := strings.Index(html, "<pre class=\"command\">")
			preEnd := -1
			if preStart >= 0 {
				preEnd = strings.Index(html[preStart:], "</pre>")
				if preEnd >= 0 {
					preEnd = preStart + preEnd
				}
			}

			if tc.wantCommands {
				if preStart < 0 || preEnd < 0 {
					t.Fatalf("expected install command block, but none found")
				}
				block := html[preStart:preEnd]
				lines := strings.Split(block, "\n")
				var cmds []string
				for _, line := range lines {
					line = strings.TrimSpace(line)
					if strings.HasPrefix(line, "goat admin ssh-key install ") {
						cmds = append(cmds, line)
					}
				}
				if len(cmds) != 2 {
					t.Fatalf("expected 2 install commands in panel, got %d (%q)", len(cmds), strings.Join(cmds, " | "))
				}
				if !strings.Contains(cmds[0], "--check") {
					t.Errorf("expected preview command with --check first, got %q", cmds[0])
				}
				if strings.Contains(cmds[1], "--check") {
					t.Errorf("expected second command to be real install without --check, got %q", cmds[1])
				}
				// As-of-page-load caption
				if !strings.Contains(html, "as of page load; refresh after a run to see the result") {
					t.Errorf("expected as-of-page-load caption")
				}
				// Queue depth: exactly one key is `approved` for THIS goatnet in
				// this fixture (user-queue); the installed rows must not inflate
				// it, the cross-env approved row (user-other-env) must be
				// excluded — the installer would skip it env_mismatch, so a
				// depth counting it could never drain — and the count must
				// render regardless of the page's status filter.
				if !strings.Contains(html, "Approved queue depth: <strong>1</strong>") {
					t.Errorf("expected approved queue depth of exactly 1 in panel")
				}
			} else {
				if preStart >= 0 && preEnd > preStart {
					t.Errorf("did not expect install command block, but found one")
				}
			}

			hasHint := strings.Contains(html, "installation is run by a shell-holding admin via goat admin ssh-key install")
			if tc.wantHint {
				if !hasHint {
					t.Errorf("expected hint text, but not found")
				}
			} else {
				if hasHint {
					t.Errorf("did not expect hint text, but found one")
				}
			}
		})
	}
}

func TestIntegration_SSHKeysPanel_Quoting(t *testing.T) {
	t.Parallel()

	srv := newIntegrationServer(t)
	// Malicious goatnet name
	srv.Cfg.NotifyGoatnet = "zagros'; rm -rf /; echo '"

	// Create an installed key for the admin.
	ctx := context.Background()
	ak, err := srv.Onboard.SubmitSSHKey(ctx, &onboardstore.SSHKey{
		Handle:  "admin-installed",
		Email:   "admin-inst@example.com",
		Env:     srv.Cfg.NotifyGoatnet,
		KeyType: "ssh-ed25519",
		KeyBody: "AAAAC3NzaC1lZDI1NTE5AAAAIFlqrvvrCRCKdT58z056yi4wpTZwpq9K/iGxL5H8cc1l",
	}, 90)
	if err != nil {
		t.Fatalf("SubmitSSHKey: %v", err)
	}
	if err := srv.Onboard.ApproveSSHKey(ctx, ak.ID, "system"); err != nil {
		t.Fatalf("ApproveSSHKey: %v", err)
	}
	if err := srv.Onboard.MarkSSHKeyInstalled(ctx, ak.ID, "system"); err != nil {
		t.Fatalf("MarkSSHKeyInstalled: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/onboarding/ssh-keys", nil).WithContext(context.Background())
	req.Header.Set("X-Forwarded-User", "admin-installed")
	req.Header.Set("X-Forwarded-Email", "admin-inst@example.com")
	req.Header.Set("X-Forwarded-Groups", "ADMINS")

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("got status %d, want 200", rec.Code)
	}

	body, _ := io.ReadAll(rec.Body)
	html := html.UnescapeString(string(body))

	// It should be quoted as 'zagros'\'''; rm -rf /; echo '\'''
	expectedQuoted := "'zagros'\\''; rm -rf /; echo '\\'''"
	if !strings.Contains(html, "goat admin ssh-key install --env "+expectedQuoted) {
		t.Errorf("expected safely quoted env string %s, got HTML:\n%s", expectedQuoted, html)
	}
	// The prose names the goatnet unquoted — shell quoting belongs to the
	// command lines only.
	if !strings.Contains(html, "<code>"+srv.Cfg.NotifyGoatnet+"</code>") {
		t.Errorf("expected prose to show the raw goatnet slug in <code>, got HTML:\n%s", html)
	}
}

func TestIntegration_SSHKeysPanel_NoGoatnetConfigured(t *testing.T) {
	t.Parallel()

	srv := newIntegrationServer(t)
	// A console without NOTIFY_GOATNET cannot render a correct install
	// command; admins must still get the hint rather than a silently dark
	// section.
	srv.Cfg.NotifyGoatnet = ""

	req := httptest.NewRequest(http.MethodGet, "/onboarding/ssh-keys", nil).WithContext(context.Background())
	req.Header.Set("X-Forwarded-User", "admin-nogoatnet")
	req.Header.Set("X-Forwarded-Email", "admin-nogoatnet@example.com")
	req.Header.Set("X-Forwarded-Groups", "ADMINS")

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200", rec.Code)
	}

	body, _ := io.ReadAll(rec.Body)
	html := html.UnescapeString(string(body))

	if strings.Contains(html, "goat admin ssh-key install --env") {
		t.Errorf("did not expect install commands without a configured goatnet")
	}
	if !strings.Contains(html, "installation is run by a shell-holding admin via goat admin ssh-key install") {
		t.Errorf("expected the static hint for an admin on a console with no goatnet configured")
	}
}

func TestIntegration_SSHKeysPanel_Empty(t *testing.T) {
	t.Parallel()

	srv := newIntegrationServer(t)

	// Create an installed key for the admin so they see the panel,
	// but DO NOT create any approved keys.
	ctx := context.Background()
	ak, err := srv.Onboard.SubmitSSHKey(ctx, &onboardstore.SSHKey{
		Handle:  "admin-empty",
		Email:   "admin-empty@example.com",
		Env:     "zagros",
		KeyType: "ssh-ed25519",
		KeyBody: "AAAAC3NzaC1lZDI1NTE5AAAAIFlqrvvrCRCKdT58z056yi4wpTZwpq9K/iGxL5H8cc1l",
	}, 90)
	if err != nil {
		t.Fatalf("SubmitSSHKey: %v", err)
	}
	if err := srv.Onboard.ApproveSSHKey(ctx, ak.ID, "system"); err != nil {
		t.Fatalf("ApproveSSHKey: %v", err)
	}
	if err := srv.Onboard.MarkSSHKeyInstalled(ctx, ak.ID, "system"); err != nil {
		t.Fatalf("MarkSSHKeyInstalled: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/onboarding/ssh-keys", nil).WithContext(context.Background())
	req.Header.Set("X-Forwarded-User", "admin-empty")
	req.Header.Set("X-Forwarded-Email", "admin-empty@example.com")
	req.Header.Set("X-Forwarded-Groups", "ADMINS")

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200", rec.Code)
	}

	body, _ := io.ReadAll(rec.Body)
	html := html.UnescapeString(string(body))

	if !strings.Contains(html, "0 approved keys waiting — nothing to install.") {
		t.Errorf("expected empty state message, but not found")
	}
}
