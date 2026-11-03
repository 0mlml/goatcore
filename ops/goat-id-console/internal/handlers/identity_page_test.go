package handlers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/onboardstore"
)

func getIdentityPage(t *testing.T, s *Server, email string) *httptest.ResponseRecorder {
	t.Helper()
	s.Cfg.DevBypassAuth = false
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/identity", nil)
	r.Header.Set("X-Forwarded-Email", email)
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, r)
	return w
}

func TestIdentityPageShowsCanonicalHandleForSessionEmail(t *testing.T) {
	s := newOnboardTestServer(t)
	s.Cfg.NotifyGoatnet = "zagros"
	const email = "casey@example.test"
	const handle = "unit-casey"
	if err := s.Onboard.CreateProposal(context.Background(), &onboardstore.Proposal{
		Sponsor: "sponsor", Env: "zagros", Email: email, Handle: handle,
		Status: onboardstore.ProposalOnboarded,
	}); err != nil {
		t.Fatalf("CreateProposal: %v", err)
	}

	w := getIdentityPage(t, s, email)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /identity = %d, want 200: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{"Your handle", `value="` + handle + `"`, "request an account or access change"} {
		if !strings.Contains(body, want) {
			t.Errorf("identity page missing %q", want)
		}
	}
	if strings.Contains(body, `name="email"`) || strings.Contains(body, `name="handle"`) {
		t.Error("identity page must not accept a caller-supplied identity")
	}

	// Even a valid session cannot select somebody else's identity through the
	// query string: the handler's only lookup input is actorFromCtx.
	if err := s.Onboard.CreateProposal(context.Background(), &onboardstore.Proposal{
		Sponsor: "sponsor", Env: "zagros", Email: "attacker@example.test", Handle: "attacker-handle",
		Status: onboardstore.ProposalOnboarded,
	}); err != nil {
		t.Fatalf("CreateProposal(attacker): %v", err)
	}
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet,
		"/identity?email=attacker@example.test&handle=attacker-handle", nil)
	r.Header.Set("X-Forwarded-Email", email)
	forged := httptest.NewRecorder()
	s.Routes().ServeHTTP(forged, r)
	if !strings.Contains(forged.Body.String(), `value="`+handle+`"`) || strings.Contains(forged.Body.String(), "attacker-handle") {
		t.Error("identity page honored caller-supplied identity parameters")
	}
}

func TestIdentityPageDoesNotExposeForeignGoatnetProposal(t *testing.T) {
	t.Run("newer foreign proposal cannot override local identity", func(t *testing.T) {
		s := newOnboardTestServer(t)
		s.Cfg.NotifyGoatnet = "zagros"
		const email = "multi-goatnet@example.test"
		for _, p := range []*onboardstore.Proposal{
			{Sponsor: "sponsor", Env: "zagros", Email: email, Handle: "local-handle", Status: onboardstore.ProposalOnboarded, CreatedAt: 100},
			{Sponsor: "sponsor", Env: "zagros-prod", Email: email, Handle: "foreign-handle", Status: onboardstore.ProposalOnboarded, CreatedAt: 200},
		} {
			if err := s.Onboard.CreateProposal(context.Background(), p); err != nil {
				t.Fatalf("CreateProposal(%s): %v", p.Env, err)
			}
		}

		w := getIdentityPage(t, s, email)
		if w.Code != http.StatusOK {
			t.Fatalf("GET /identity = %d, want 200: %s", w.Code, w.Body.String())
		}
		body := w.Body.String()
		if strings.Contains(body, "foreign-handle") || strings.Contains(body, `id="identity-handle"`) {
			t.Error("foreign proposal overrode the local-goatnet identity")
		}
		if !strings.Contains(body, "Your handle could not be found") {
			t.Error("cross-goatnet mismatch did not fail closed as a missing mapping")
		}
	})

	t.Run("foreign-only proposal is not exposed", func(t *testing.T) {
		s := newOnboardTestServer(t)
		s.Cfg.NotifyGoatnet = "zagros"
		const email = "foreign-only@example.test"
		if err := s.Onboard.CreateProposal(context.Background(), &onboardstore.Proposal{
			Sponsor: "sponsor", Env: "zagros-prod", Email: email, Handle: "foreign-only-handle",
			Status: onboardstore.ProposalOnboarded,
		}); err != nil {
			t.Fatalf("CreateProposal: %v", err)
		}

		body := getIdentityPage(t, s, email).Body.String()
		if strings.Contains(body, "foreign-only-handle") || strings.Contains(body, `id="identity-handle"`) {
			t.Error("foreign-only proposal was exposed as the local canonical handle")
		}
		if !strings.Contains(body, "Your handle could not be found") {
			t.Error("foreign-only proposal did not render the missing-handle state")
		}
	})
}

func TestIdentityPageDoesNotExposeHandleWhenGoatnetUnset(t *testing.T) {
	s := newOnboardTestServer(t)
	s.Cfg.NotifyGoatnet = ""
	const email = "unset-goatnet@example.test"
	const handle = "must-not-be-exposed"
	if err := s.Onboard.CreateProposal(context.Background(), &onboardstore.Proposal{
		Sponsor: "sponsor", Env: "zagros", Email: email, Handle: handle,
		Status: onboardstore.ProposalOnboarded,
	}); err != nil {
		t.Fatalf("CreateProposal: %v", err)
	}

	body := getIdentityPage(t, s, email).Body.String()
	if strings.Contains(body, handle) || strings.Contains(body, `id="identity-handle"`) {
		t.Error("identity page exposed a handle without a configured goatnet")
	}
	for _, want := range []string{"Your handle could not be found", "Contact an administrator"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing-goatnet page missing %q", want)
		}
	}
}

func TestIdentityPageMissingMappingDoesNotGuess(t *testing.T) {
	s := newOnboardTestServer(t)
	w := getIdentityPage(t, s, "guessed-handle@example.test")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /identity = %d, want 200: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{"could not be found", "Contact an administrator", "Do not use your email address"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing-mapping page missing %q", want)
		}
	}
	if strings.Contains(body, `id="identity-handle"`) {
		t.Error("missing mapping rendered a copyable handle")
	}
}

func TestIdentityPageBackendFailureIsNotReportedAsMissingHandle(t *testing.T) {
	s := newOnboardTestServer(t)
	if err := s.Onboard.Close(); err != nil {
		t.Fatalf("close onboard store: %v", err)
	}

	w := getIdentityPage(t, s, "casey@example.test")
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("GET /identity with unavailable store = %d, want 500: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{"The request could not be completed. Try again later.", `role="alert"`, "<nav>", "</html>"} {
		if !strings.Contains(body, want) {
			t.Errorf("backend failure did not use the shared error page: missing %q", want)
		}
	}
	if strings.Contains(body, "Your handle could not be found") || strings.Contains(body, "Contact an administrator") {
		t.Error("backend failure was presented as a missing handle mapping")
	}
}

func TestIdentityPageAmbiguousLegacyMappingDoesNotGuess(t *testing.T) {
	s := newOnboardTestServer(t)
	s.Cfg.NotifyGoatnet = "zagros"
	const email = "ambiguous@example.test"
	for i, handle := range []string{"unit-one", "unit-two"} {
		key, err := s.Onboard.SubmitSSHKey(context.Background(), &onboardstore.SSHKey{
			Handle: handle, Email: email, Env: "zagros", KeyType: "ssh-ed25519",
			KeyBody: fmt.Sprintf("AAAAC3NzaC1lZDI1NTE5AAAAIFlqrvvrCRCKdT58z056yi4wpTZwpq9K/iGxL5H8c%03d", i),
		}, 90)
		if err != nil {
			t.Fatalf("SubmitSSHKey(%s): %v", handle, err)
		}
		if err := s.Onboard.ApproveSSHKey(context.Background(), key.ID, "installer"); err != nil {
			t.Fatalf("ApproveSSHKey(%s): %v", handle, err)
		}
		if err := s.Onboard.MarkSSHKeyInstalled(context.Background(), key.ID, "installer"); err != nil {
			t.Fatalf("MarkSSHKeyInstalled(%s): %v", handle, err)
		}
	}

	body := getIdentityPage(t, s, email).Body.String()
	if strings.Contains(body, `id="identity-handle"`) || strings.Contains(body, "unit-one") || strings.Contains(body, "unit-two") {
		t.Error("ambiguous legacy mapping rendered or disclosed a guessed handle")
	}
	if !strings.Contains(body, "Contact an administrator") {
		t.Error("ambiguous legacy mapping did not provide admin guidance")
	}
}
