package handlers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/kratos"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/onboardstore"
)

const validDeliveryManifest = `{"identity":{"handle":"legacy","email":"legacy@example.test","name":"Legacy","status":"active","added":"2026-06-01","added_by":"admin@example.test"},"netbird":{"enabled":false},"ory_site":{"enabled":false},"ory_idp":{"enabled":false},"zenoh":{"enabled":false}}`

func deliveryRequest(t *testing.T, s *Server, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{"csrf": {s.csrfNonce}, "setup_key": {"replacement-synthetic-key"}}
	r := httptest.NewRequestWithContext(context.Background(), method, path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, r)
	return w
}

func deliveryRequestWithoutKey(t *testing.T, s *Server, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{"csrf": {s.csrfNonce}}
	r := httptest.NewRequestWithContext(context.Background(), method, path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, r)
	return w
}

// A manifest that does not say netbird is disabled cannot prove the person
// needs no mesh key, so a keyless package is held on every delivery path.
func TestManifestWithoutMeshDecisionHoldsKeylessWelcome(t *testing.T) {
	for index, manifest := range []string{"{}", "", "not json", "null", "[]"} {
		t.Run(fmt.Sprintf("keyless-%d", index), func(t *testing.T) {
			s := newOnboardTestServer(t)
			messages := captureMail(t, s)
			var recoveryCalls atomic.Int32
			identity := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				recoveryCalls.Add(1)
				w.WriteHeader(http.StatusInternalServerError)
			}))
			defer identity.Close()
			s.Kratos = map[string]*kratos.Client{"tenant": kratos.NewClient(identity.URL)}
			p := &onboardstore.Proposal{ID: "legacy", Handle: "legacy", Email: "legacy@example.test", Env: "zagros",
				Status: onboardstore.ProposalOnboarded, ManifestJSON: manifest}
			if err := s.Onboard.CreateProposal(context.Background(), p); err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/", nil)
			s.sendWelcomeEmail(r, p, "", "", "", "", false)
			if events := noticeEvents(s, "onboard_welcome_email"); len(events) != 1 || events[0].Result != "held" {
				t.Fatalf("keyless automatic delivery not held: %+v", events)
			}
			if result, _ := s.resendWelcomePackage(r, p, "", "", Actor{Email: "admin@example.test"}); result != "held" {
				t.Fatalf("keyless shared resend returned %q", result)
			}
			if w := deliveryRequestWithoutKey(t, s, http.MethodGet, "/onboarding/legacy/welcome"); w.Code != http.StatusConflict ||
				!strings.Contains(w.Body.String(), "mesh enrollment key") {
				t.Fatalf("keyless download: %d %s", w.Code, w.Body.String())
			}
			if w := deliveryRequestWithoutKey(t, s, http.MethodPost, "/onboarding/legacy/welcome/resend"); w.Code != http.StatusConflict {
				t.Fatalf("keyless resend: %d %s", w.Code, w.Body.String())
			}
			stored, err := s.Onboard.GetProposal(context.Background(), p.ID)
			if err != nil || stored.SetupKey != "" || stored.Status != onboardstore.ProposalOnboarded {
				t.Fatalf("refused resend changed credentials or state: %+v %v", stored, err)
			}
			if len(messages) != 0 || recoveryCalls.Load() != 0 {
				t.Fatalf("keyless package caused %d messages and %d recovery calls", len(messages), recoveryCalls.Load())
			}
		})
	}
}

// A live onboarded person whose stored manifest is a historical index row or
// predates a schema tightening still receives a package that carries a key:
// after a device reissue, through an operator-supplied key, and by download.
func TestKeyedWelcomeDeliversDespiteHistoricalManifest(t *testing.T) {
	drifted := strings.Replace(validDeliveryManifest, `"ory_site":{"enabled":false}`,
		`"ory_site":{"enabled":true,"groups":["ADMINS"]}`, 1)
	for index, manifest := range []string{"{}", drifted} {
		t.Run(fmt.Sprintf("historical-%d", index), func(t *testing.T) {
			s := newOnboardTestServer(t)
			messages := captureMail(t, s)
			p := &onboardstore.Proposal{ID: "legacy", Handle: "legacy", Email: "legacy@example.test", Env: "zagros",
				Status: onboardstore.ProposalOnboarded, ManifestJSON: manifest}
			if err := s.Onboard.CreateProposal(context.Background(), p); err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/", nil)
			if outcome, msg := s.resendWelcomePackage(r, p, "reissued-synthetic-key", "", Actor{Email: "admin@example.test"}); outcome != "ok" {
				t.Fatalf("reissued key not delivered: %s %s", outcome, msg)
			}
			if w := deliveryRequest(t, s, http.MethodPost, "/onboarding/legacy/welcome/resend"); w.Code != http.StatusOK {
				t.Fatalf("resend with supplied key: %d %s", w.Code, w.Body.String())
			}
			if w := deliveryRequest(t, s, http.MethodGet, "/onboarding/legacy/welcome"); w.Code != http.StatusOK {
				t.Fatalf("download after the key is stored: %d %s", w.Code, w.Body.String())
			}
			if len(messages) != 2 {
				t.Fatalf("got %d deliveries, want 2", len(messages))
			}
		})
	}
}

// Without a mailer, resend still saves a supplied key, so the download that
// tells the admin to supply it is not a dead end.
func TestResendSavesKeyWhenEmailIsNotConfigured(t *testing.T) {
	s := newOnboardTestServer(t)
	mesh := strings.Replace(validDeliveryManifest, `"netbird":{"enabled":false}`,
		`"netbird":{"enabled":true,"sites":["zagros"],"role":"peer","auto_groups":["OBSERVER"]}`, 1)
	p := &onboardstore.Proposal{ID: "legacy", Handle: "legacy", Email: "legacy@example.test", Env: "zagros",
		Status: onboardstore.ProposalOnboarded, ManifestJSON: mesh}
	if err := s.Onboard.CreateProposal(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	if w := deliveryRequestWithoutKey(t, s, http.MethodGet, "/onboarding/legacy/welcome"); w.Code != http.StatusConflict {
		t.Fatalf("keyless download: %d %s", w.Code, w.Body.String())
	}
	if w := deliveryRequest(t, s, http.MethodPost, "/onboarding/legacy/welcome/resend"); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("resend without mailer: %d %s", w.Code, w.Body.String())
	}
	if w := deliveryRequestWithoutKey(t, s, http.MethodGet, "/onboarding/legacy/welcome"); w.Code != http.StatusOK {
		t.Fatalf("download after the key is saved: %d %s", w.Code, w.Body.String())
	}
}

func TestWelcomeManifestValidDeliveryControls(t *testing.T) {
	mesh := strings.Replace(validDeliveryManifest, `"netbird":{"enabled":false}`,
		`"netbird":{"enabled":true,"sites":["zagros"],"role":"peer","auto_groups":["OBSERVER"]}`, 1)
	for _, manifest := range []string{validDeliveryManifest, mesh} {
		t.Run(manifest, func(t *testing.T) {
			s := newOnboardTestServer(t)
			messages := captureMail(t, s)
			p := &onboardstore.Proposal{ID: "legacy", Handle: "legacy", Email: "legacy@example.test", Env: "zagros",
				Status: onboardstore.ProposalOnboarded, ManifestJSON: manifest, SetupKey: "synthetic-key"}
			if err := s.Onboard.CreateProposal(context.Background(), p); err != nil {
				t.Fatal(err)
			}
			if err := s.Onboard.SetProposalResult(context.Background(), p.ID, p.Status, p.SetupKey, "", "", "", "", false); err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/", nil)
			s.sendWelcomeEmail(r, p, p.SetupKey, "", "", "", false)
			if len(messages) != 1 {
				// Surface the actual boundary outcome if a fixture stops validating.
				t.Fatalf("valid welcome not delivered: %+v", noticeEvents(s, "onboard_welcome_email"))
			}
			if outcome, errMsg := s.resendWelcomePackage(r, p, p.SetupKey, "", Actor{Email: "admin@example.test"}); outcome != "ok" {
				t.Fatalf("valid resend: %s %s", outcome, errMsg)
			}
			if len(messages) != 2 {
				t.Fatal("valid resend did not deliver")
			}
			w := deliveryRequest(t, s, http.MethodGet, "/onboarding/legacy/welcome")
			if w.Code != http.StatusOK || !strings.Contains(w.Header().Get("Content-Disposition"), "attachment") {
				t.Fatalf("valid welcome download: %d %s", w.Code, w.Body.String())
			}
			if manifest == mesh {
				p.SetupKey = ""
				s.sendWelcomeEmail(r, p, "", "", "", "", false)
				if len(messages) != 2 {
					t.Fatal("mesh welcome without key was delivered")
				}
			} else {
				s.sendWelcomeEmail(r, p, "", "", "", "", false)
				if len(messages) != 3 {
					t.Fatal("valid no-mesh welcome was held without a key")
				}
			}
		})
	}
}

func TestInvalidRestoreAndRetryReturnActionableConflict(t *testing.T) {
	for _, status := range []string{onboardstore.ProposalArchived, onboardstore.ProposalFailed} {
		s := newOnboardTestServer(t)
		seedProposal(t, s, "legacy", "admin@example.test", "browse", status)
		action := "restore"
		if status == onboardstore.ProposalFailed {
			action = "retry"
		}
		w := machineReq(t, s.Routes(), "runner-key", http.MethodPost, "/api/onboard/proposals/legacy/"+action, "{}")
		if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "invalid_manifest") {
			t.Fatalf("%s API: %d %s", action, w.Code, w.Body.String())
		}
		if status == onboardstore.ProposalArchived {
			w = deliveryRequest(t, s, http.MethodPost, "/onboarding/legacy/restore")
			if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "no supported repair path") {
				t.Fatalf("browser restore: %d %s", w.Code, w.Body.String())
			}
		}
	}
}

func TestWelcomeManifestKeyRequirements(t *testing.T) {
	for _, mesh := range []bool{false, true} {
		s := newOnboardTestServer(t)
		messages := captureMail(t, s)
		manifest := validDeliveryManifest
		wantCode, wantOutcome, wantMessages := http.StatusOK, "ok", 3
		if mesh {
			manifest = strings.Replace(manifest, `"netbird":{"enabled":false}`,
				`"netbird":{"enabled":true,"sites":["zagros"],"role":"peer","auto_groups":["OBSERVER"]}`, 1)
			wantCode, wantOutcome, wantMessages = http.StatusConflict, "held", 0
		}
		p := &onboardstore.Proposal{ID: "legacy", Handle: "legacy", Email: "legacy@example.test", Env: "zagros",
			Status: onboardstore.ProposalOnboarded, ManifestJSON: manifest}
		if err := s.Onboard.CreateProposal(context.Background(), p); err != nil {
			t.Fatal(err)
		}
		w := deliveryRequest(t, s, http.MethodGet, "/onboarding/legacy/welcome")
		if w.Code != wantCode {
			t.Fatalf("mesh=%v keyless download: %d %s", mesh, w.Code, w.Body.String())
		}
		form := url.Values{"csrf": {s.csrfNonce}}
		r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/onboarding/legacy/welcome/resend", strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w = httptest.NewRecorder()
		s.Routes().ServeHTTP(w, r)
		if w.Code != wantCode {
			t.Fatalf("mesh=%v keyless resend: %d %s", mesh, w.Code, w.Body.String())
		}
		if outcome, msg := s.resendWelcomePackage(r, p, "", "", Actor{Email: "admin@example.test"}); outcome != wantOutcome {
			t.Fatalf("mesh=%v shared keyless resend: %s %s", mesh, outcome, msg)
		}
		s.sendWelcomeEmail(r, p, "", "", "", "", false)
		if len(messages) != wantMessages {
			t.Fatalf("mesh=%v: got %d keyless deliveries, want %d", mesh, len(messages), wantMessages)
		}
	}
}
