package handlers

// Console credential rotation (goat#320): the admin half of ADR 1044's
// in-place re-issue, from the user detail page. These tests are written from
// the issue + credential-rotation.md, red-first: the console names the person
// (the target's Kratos identity UUID from the page URL — never an anchor, and
// never anything form-supplied), the portal resolves what is theirs, the
// fresh credential renders exactly once, and the audit row names the admin
// without ever carrying the secret.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// fakeRotatePortal records the one call the handler must make — POST
// /api/orgs/rotate with the console's bearer, the admin named via
// X-Goat-Operator-Actor, and a body naming ONLY the identity — and answers
// with status/body.
type rotatePortalCall struct {
	Method, Path, Bearer, ActorHeader string
	Body                              map[string]any
}

func fakeRotatePortal(t *testing.T, status int, body map[string]any) (*httptest.Server, func() []rotatePortalCall) {
	t.Helper()
	var calls []rotatePortalCall
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]any
		_ = json.NewDecoder(r.Body).Decode(&reqBody)
		calls = append(calls, rotatePortalCall{
			Method:      r.Method,
			Path:        r.URL.Path,
			Bearer:      strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "),
			ActorHeader: r.Header.Get("X-Goat-Operator-Actor"),
			Body:        reqBody,
		})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []rotatePortalCall { return calls }
}

// postRotate drives POST /users/{id}/rotate-credential as an admin, carrying
// a freshly minted one-time nonce (goat#580) — these tests are about the
// rotation itself; the nonce discipline is covered in
// rotate_double_submit_test.go.
func postRotate(t *testing.T, s *Server, csrf string, withCSRF bool) *httptest.ResponseRecorder {
	t.Helper()
	h := s.Routes()
	form := url.Values{}
	form.Set("rotate_nonce", s.mintRotateNonce())
	if withCSRF {
		form.Set("csrf", csrf)
	}
	r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/users/id-1/rotate-credential?idp=tenant", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("X-Forwarded-Groups", "PLATFORM-ADMIN")
	r.Header.Set("X-Forwarded-Email", "admin@example.org")
	r.Header.Set("X-Forwarded-Preferred-Username", "actor-1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// The happy path: the portal is called exactly once with the trust-boundary
// shape (bearer + named admin + identity-only body), the fresh credential
// renders ONCE with the shown-once warning, and the audit row is attributed
// but secret-free.
func TestRotateCredential_OKShowsKeyOnceAndAuditsWithoutTheSecret(t *testing.T) {
	kc, _ := fakeKratos2FA(t, "id-1", "u@example.org", http.StatusOK, http.StatusNoContent, posturePasswordOnly)
	portal, callsFn := fakeRotatePortal(t, http.StatusOK, map[string]any{
		"slot_id": "anchor-aaaa", "system_handle": "fra-land-bde",
		"api_key": "vp_freshkey123", "bundle_url": "/api/org-bundle/anchor-aaaa/tok.tar.gz",
		"uri_san": "urn:goat:zagros:org=anchor-aaaa",
	})
	s := newUsersAPITestServer(t, kc)
	s.Cfg.PortalInternalURL = portal.URL
	s.Cfg.PortalLookupToken = "svc-token"
	s.Cfg.NotifyPortalURL = "https://portal.example"

	w := postRotate(t, s, s.csrfNonce, true)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (the one-time key can only be shown on THIS response); body=%s",
			w.Code, w.Body.String())
	}

	calls := callsFn()
	if len(calls) != 1 {
		t.Fatalf("portal called %d times, want exactly once", len(calls))
	}
	c := calls[0]
	if c.Method != http.MethodPost || c.Path != "/api/orgs/rotate" {
		t.Errorf("portal saw %s %s, want POST /api/orgs/rotate", c.Method, c.Path)
	}
	if c.Bearer != "svc-token" {
		t.Errorf("portal bearer = %q, want the console's operator token", c.Bearer)
	}
	if c.ActorHeader != "admin@example.org" {
		t.Errorf("X-Goat-Operator-Actor = %q, want the signed-in admin — the portal audit row must name the human", c.ActorHeader)
	}
	if got, _ := c.Body["identity_id"].(string); got != "id-1" {
		t.Errorf("portal body identity_id = %q, want the page's identity", got)
	}
	if len(c.Body) != 1 {
		t.Errorf("portal body = %v — the console must send the identity and NOTHING else (no anchor field to steer)", c.Body)
	}

	out := w.Body.String()
	for _, m := range []string{"vp_freshkey123", "anchor-aaaa", "u@example.org"} {
		if !strings.Contains(out, m) {
			t.Errorf("result page missing %q", m)
		}
	}
	if !strings.Contains(strings.ToLower(out), "shown once") {
		t.Error("result page must say the key is shown once and unrecoverable")
	}

	ev := lastAudit(t, s, "rotate_credential")
	if ev.Result != "ok" {
		t.Fatalf("audit result = %q, want ok", ev.Result)
	}
	if ev.Actor.Email != "admin@example.org" {
		t.Errorf("audit actor = %q, want admin@example.org", ev.Actor.Email)
	}
	evJSON, _ := json.Marshal(ev)
	if strings.Contains(string(evJSON), "vp_freshkey123") {
		t.Error("audit event carries the fresh api_key — secrets must never land in the audit stream")
	}
	if !strings.Contains(string(evJSON), "anchor-aaaa") {
		t.Error("audit event should carry the rotated anchor for attribution")
	}
}

// The portal's rotate response carries bundle_url as a path relative to
// ITSELF (e.g. "/api/org-bundle/<anchor>/<token>.tar.gz"). The result page
// must resolve that against the goatnet's PUBLIC mesh portal host
// (NotifyPortalURL) — the same one the onboarding welcome email uses — and
// not the console's PortalInternalURL, which is only the backend call
// target and is not guaranteed to be what an admin's browser can reach.
func TestRotateCredential_BundleURLResolvesToThePublicPortalHost(t *testing.T) {
	kc, _ := fakeKratos2FA(t, "id-1", "u@example.org", http.StatusOK, http.StatusNoContent, posturePasswordOnly)
	portal, _ := fakeRotatePortal(t, http.StatusOK, map[string]any{
		"slot_id": "anchor-aaaa", "system_handle": "fra-land-bde",
		"api_key": "vp_freshkey123", "bundle_url": "/api/org-bundle/anchor-aaaa/tok.tar.gz",
		"uri_san": "urn:goat:zagros:org=anchor-aaaa",
	})
	s := newUsersAPITestServer(t, kc)
	s.Cfg.PortalInternalURL = portal.URL // backend target — NOT what should render
	s.Cfg.PortalLookupToken = "svc-token"
	s.Cfg.NotifyPortalURL = "https://portal.zagros.netbird.zagros.example"

	w := postRotate(t, s, s.csrfNonce, true)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}

	out := w.Body.String()
	want := "https://portal.zagros.netbird.zagros.example/api/org-bundle/anchor-aaaa/tok.tar.gz"
	if !strings.Contains(out, want) {
		t.Errorf("result page bundle URL not resolved to the public portal host: want %q in body, got:\n%s", want, out)
	}
	if strings.Contains(out, portal.URL) {
		t.Errorf("result page leaked the internal backend URL (%q) instead of the public one", portal.URL)
	}
}

// No CSRF, no call: the rotate is a state-changing POST behind a browser
// session, and a cross-site trigger would re-key (deny service to) a person.
func TestRotateCredential_CSRFRequiredBeforeThePortalIsTouched(t *testing.T) {
	kc, _ := fakeKratos2FA(t, "id-1", "u@example.org", http.StatusOK, http.StatusNoContent, posturePasswordOnly)
	portal, callsFn := fakeRotatePortal(t, http.StatusOK, map[string]any{"api_key": "vp_x"})
	s := newUsersAPITestServer(t, kc)
	s.Cfg.PortalInternalURL = portal.URL
	s.Cfg.PortalLookupToken = "svc-token"

	w := postRotate(t, s, s.csrfNonce, false)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if n := len(callsFn()); n != 0 {
		t.Fatalf("portal called %d times on a CSRF failure, want 0", n)
	}
}

// Fail closed on the target fetch, like reset-2FA: the audit row's whole
// justification is attribution, so an unknown identity aborts before the
// portal is asked to rotate anything.
func TestRotateCredential_UnknownIdentityAbortsBeforeThePortal(t *testing.T) {
	kc, _ := fakeKratos2FA(t, "id-1", "u@example.org", http.StatusNotFound, http.StatusNoContent, posturePasswordOnly)
	portal, callsFn := fakeRotatePortal(t, http.StatusOK, map[string]any{"api_key": "vp_x"})
	s := newUsersAPITestServer(t, kc)
	s.Cfg.PortalInternalURL = portal.URL
	s.Cfg.PortalLookupToken = "svc-token"
	s.Cfg.NotifyPortalURL = "https://portal.example"

	w := postRotate(t, s, s.csrfNonce, true)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
	if n := len(callsFn()); n != 0 {
		t.Fatalf("portal called %d times for an unknown identity, want 0", n)
	}
}

// A portal refusal surfaces as a reader-facing error — the portal's specific
// message, not a generic failure (the goat#214 defect class) — and the audit
// row says error, never ok.
func TestRotateCredential_PortalRefusalIsSurfacedAndAuditedAsError(t *testing.T) {
	kc, _ := fakeKratos2FA(t, "id-1", "u@example.org", http.StatusOK, http.StatusNoContent, posturePasswordOnly)
	portal, _ := fakeRotatePortal(t, http.StatusConflict, map[string]any{
		"error": map[string]any{"code": "not_minted",
			"message": "this identity's slot holds no minted credential, so there is nothing to rotate."},
	})
	s := newUsersAPITestServer(t, kc)
	s.Cfg.PortalInternalURL = portal.URL
	s.Cfg.PortalLookupToken = "svc-token"
	s.Cfg.NotifyPortalURL = "https://portal.example"

	w := postRotate(t, s, s.csrfNonce, true)
	if w.Code == http.StatusOK || w.Code == http.StatusSeeOther {
		t.Fatalf("status = %d — a refused rotation must not read as success", w.Code)
	}
	if !strings.Contains(w.Body.String(), "nothing to rotate") {
		t.Errorf("the portal's specific refusal must reach the reader; body=%s", w.Body.String())
	}
	if ev := lastAudit(t, s, "rotate_credential"); ev.Result != "error" {
		t.Fatalf("audit result = %q, want error", ev.Result)
	}
}

// The portal refusing the CONSOLE's own bearer is a console fault, not the
// admin's. Mirrored as 401/403 it told a permitted admin "you are not allowed
// to do this" when the truth is "this console's portal credential is wrong"
// (#581). It lands in the unwired-portal bucket instead: 503, and the variable
// an admin has to fix. The status of a real refusal (409 above) is untouched.
func TestRotateCredential_PortalAuthFailureReadsAsAConsoleFault(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			kc, _ := fakeKratos2FA(t, "id-1", "u@example.org", http.StatusOK, http.StatusNoContent, posturePasswordOnly)
			portal, _ := fakeRotatePortal(t, status, map[string]any{
				"error": map[string]any{"code": "forbidden", "message": "operator key not accepted"},
			})
			s := newUsersAPITestServer(t, kc)
			s.Cfg.PortalInternalURL = portal.URL
			s.Cfg.PortalLookupToken = "expired-token"
			s.Cfg.NotifyPortalURL = "https://portal.example"

			w := postRotate(t, s, s.csrfNonce, true)
			if w.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want 503 — the portal's %d is about this console, not the admin", w.Code, status)
			}
			if !strings.Contains(w.Body.String(), "PORTAL_LOOKUP_TOKEN") {
				t.Errorf("error must name the credential an admin has to fix; body=%s", w.Body.String())
			}
			if !strings.Contains(w.Body.String(), "operator key not accepted") {
				t.Errorf("error must carry the portal's own words so a wrong token and a vendor key stay distinct; body=%s", w.Body.String())
			}
			if ev := lastAudit(t, s, "rotate_credential"); ev.Result != "error" {
				t.Fatalf("audit result = %q, want error", ev.Result)
			}
		})
	}
}

// A real portal refusal keeps its own status as well as its message.
func TestRotateCredential_PortalRefusalKeepsItsStatus(t *testing.T) {
	kc, _ := fakeKratos2FA(t, "id-1", "u@example.org", http.StatusOK, http.StatusNoContent, posturePasswordOnly)
	portal, _ := fakeRotatePortal(t, http.StatusConflict, map[string]any{
		"error": map[string]any{"code": "no_slot", "message": "this identity has no slot."},
	})
	s := newUsersAPITestServer(t, kc)
	s.Cfg.PortalInternalURL = portal.URL
	s.Cfg.PortalLookupToken = "svc-token"
	s.Cfg.NotifyPortalURL = "https://portal.example"

	w := postRotate(t, s, s.csrfNonce, true)
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want the portal's 409", w.Code)
	}
}

// A console with no portal wiring says so plainly instead of pretending.
// NotifyPortalURL is set here specifically so this test isolates the
// variable it's about (PortalInternalURL/PortalLookupToken) rather than
// tripping the separate NotifyPortalURL precondition below.
func TestRotateCredential_UnwiredPortalRefusesPlainly(t *testing.T) {
	kc, _ := fakeKratos2FA(t, "id-1", "u@example.org", http.StatusOK, http.StatusNoContent, posturePasswordOnly)
	s := newUsersAPITestServer(t, kc)
	s.Cfg.NotifyPortalURL = "https://portal.example"

	w := postRotate(t, s, s.csrfNonce, true)
	if w.Code == http.StatusOK {
		t.Fatalf("status = %d — an unwired console cannot have rotated anything", w.Code)
	}
	if !strings.Contains(w.Body.String(), "PORTAL_INTERNAL_URL") {
		t.Errorf("error must tell the admin what to configure; body=%s", w.Body.String())
	}
}

// The bundle-URL fix (goat#961-shape) only holds if NotifyPortalURL is
// actually required: without it, the result page would render a bare
// portal-relative path next to fixed copy that claims it's a fetchable
// one-time link. Checked BEFORE the portal is ever asked to rotate anything
// — discovering the misconfiguration after minting a one-time, unrecoverable
// secret would be strictly worse than refusing up front.
func TestRotateCredential_RequiresNotifyPortalURLConfigured(t *testing.T) {
	kc, _ := fakeKratos2FA(t, "id-1", "u@example.org", http.StatusOK, http.StatusNoContent, posturePasswordOnly)
	portal, callsFn := fakeRotatePortal(t, http.StatusOK, map[string]any{
		"slot_id": "anchor-aaaa", "api_key": "vp_freshkey123",
		"bundle_url": "/api/org-bundle/anchor-aaaa/tok.tar.gz",
	})
	s := newUsersAPITestServer(t, kc)
	s.Cfg.PortalInternalURL = portal.URL
	s.Cfg.PortalLookupToken = "svc-token"
	// NotifyPortalURL deliberately left unset.

	w := postRotate(t, s, s.csrfNonce, true)
	if w.Code == http.StatusOK {
		t.Fatalf("status = %d — must refuse rather than rotate with no way to show a working bundle link", w.Code)
	}
	if !strings.Contains(w.Body.String(), "NOTIFY_PORTAL_URL") {
		t.Errorf("error must tell the admin what to configure; body=%s", w.Body.String())
	}
	if n := len(callsFn()); n != 0 {
		t.Fatalf("portal called %d times despite the missing precondition, want 0 — nothing may be minted", n)
	}
	if strings.Contains(w.Body.String(), "vp_freshkey123") {
		t.Error("a refused rotation must not leak a secret that was never minted")
	}
}

// POLICY PIN — rotation is deliberately NOT step-up-gated in this slice:
// `rotate_credential` is not in group-permissions.yml step_up_required (the
// documented "highest-blast five"), matching the fabric-credential mint
// precedent. A password-only admin therefore CAN rotate. If you are here
// because you added requireStepUp to the handler: that is a real policy
// change — add the action to step_up_required AND sequence it behind the
// goat#247 fix (PR #252), or every enforced goatnet loses rotation to the
// unwinnable enroll-2FA loop. Then update this test to the gated expectation.
func TestRotateCredential_UngatedByStepUpPolicyDecision(t *testing.T) {
	kc, _ := fakeKratos2FA(t, "id-1", "u@example.org", http.StatusOK, http.StatusNoContent, posturePasswordOnly)
	portal, _ := fakeRotatePortal(t, http.StatusOK, map[string]any{
		"slot_id": "anchor-aaaa", "api_key": "vp_freshkey123",
	})
	s := newUsersAPITestServer(t, kc) // StepUpEnforced: true — and still ungated
	s.Cfg.PortalInternalURL = portal.URL
	s.Cfg.PortalLookupToken = "svc-token"
	s.Cfg.NotifyPortalURL = "https://portal.example"

	w := postRotate(t, s, s.csrfNonce, true)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 — rotate is not on the step_up_required list; body=%s",
			w.Code, w.Body.String())
	}
	if loc := w.Header().Get("Location"); strings.HasPrefix(loc, "/step-up") {
		t.Fatal("rotate redirected into step-up — that gating is a policy change; see the comment above")
	}
}

// The user-detail page carries the rotate control: a CSRF'd POST form with a
// consequence-stating confirm — rotation invalidates what the person holds.
func TestTemplates_userDetailRotateControl(t *testing.T) {
	out := renderPage(t, "user_detail", &pageVars{
		User: &userDetail{ID: "x", Email: "u@example.org"}, CSRFToken: "nonce-123",
	})
	for _, m := range []string{
		"Rotate fabric credential", "/users/x/rotate-credential", `value="nonce-123"`,
	} {
		if !strings.Contains(out, m) {
			t.Errorf("user_detail rotate control missing %q", m)
		}
	}
	if !strings.Contains(out, "invalidates") {
		t.Error("the rotate confirm must state the consequence: it invalidates the credential the person holds")
	}
}

// The result page is the one-time hand-over: anchor, fresh key, bundle URL,
// and the shown-once warning all render.
func TestTemplates_rotateResultShowsTheOneTimeCredential(t *testing.T) {
	out := renderPage(t, "rotate_credential", &pageVars{
		RotatedForEmail: "u@example.org",
		RotateAnchor:    "anchor-aaaa",
		RotateHandle:    "fra-land-bde",
		RotateAPIKey:    "vp_freshkey123",
		RotateBundleURL: "https://portal.example/api/org-bundle/anchor-aaaa/tok.tar.gz",
	})
	for _, m := range []string{"u@example.org", "anchor-aaaa", "fra-land-bde", "vp_freshkey123", "anchor-aaaa/tok.tar.gz"} {
		if !strings.Contains(out, m) {
			t.Errorf("rotate result page missing %q", m)
		}
	}
	if !strings.Contains(strings.ToLower(out), "shown once") {
		t.Error("result page must carry the shown-once warning")
	}
}
