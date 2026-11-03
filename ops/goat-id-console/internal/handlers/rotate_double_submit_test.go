package handlers

// Rotate double-submit protection (goat#580): the hand-over page IS the POST
// response, so a browser refresh, back-then-forward, or double-click re-POSTs
// and rotates AGAIN — silently invalidating the key the admin is mid-way
// through reading out, with no way to recover it. Written red-first from the
// issue: the user-detail page mints a one-time nonce with the rotate form,
// the handler consumes it before the portal is touched, and a replayed or
// nonce-less POST is refused with 409 — never a second rotation. The static
// per-instance CSRF nonce cannot do this job: it is the same on every render.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/hydra"
)

var rotateNonceRE = regexp.MustCompile(`name="rotate_nonce"\s+value="([^"]+)"`)

// getUserDetailRotateNonce renders the real user-detail page through the
// routes and pulls the rotate nonce out of the form — the same coupling the
// browser has. Empty string = the page mints none.
func getUserDetailRotateNonce(t *testing.T, s *Server) string {
	t.Helper()
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/users/id-1?idp=tenant", nil)
	r.Header.Set("X-Forwarded-Groups", "PLATFORM-ADMIN")
	r.Header.Set("X-Forwarded-Email", "admin@example.org")
	r.Header.Set("X-Forwarded-Preferred-Username", "actor-1")
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("GET user detail = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	m := rotateNonceRE.FindStringSubmatch(w.Body.String())
	if m == nil {
		return ""
	}
	return m[1]
}

// postRotateWithNonce drives the rotate POST carrying the given one-time
// nonce (omitted entirely when empty), with a valid CSRF token throughout —
// these tests are about the nonce, not CSRF.
func postRotateWithNonce(t *testing.T, s *Server, nonce string) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{}
	form.Set("csrf", s.csrfNonce)
	if nonce != "" {
		form.Set("rotate_nonce", nonce)
	}
	r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/users/id-1/rotate-credential?idp=tenant", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("X-Forwarded-Groups", "PLATFORM-ADMIN")
	r.Header.Set("X-Forwarded-Email", "admin@example.org")
	r.Header.Set("X-Forwarded-Preferred-Username", "actor-1")
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, r)
	return w
}

// newRotateNonceTestServer is newUsersAPITestServer plus the hydra client the
// user-detail page reads consent sessions through (best-effort; an
// unreachable admin URL renders as "no sessions", never an error).
func newRotateNonceTestServer(t *testing.T) (*Server, func() []rotatePortalCall) {
	t.Helper()
	kc, _ := fakeKratos2FA(t, "id-1", "u@example.org", http.StatusOK, http.StatusNoContent, posturePasswordOnly)
	portal, callsFn := fakeRotatePortal(t, http.StatusOK, map[string]any{
		"slot_id": "anchor-aaaa", "system_handle": "fra-land-bde",
		"api_key": "vp_freshkey123", "bundle_url": "/api/org-bundle/anchor-aaaa/tok.tar.gz",
		"uri_san": "urn:goat:zagros:org=anchor-aaaa",
	})
	s := newUsersAPITestServer(t, kc)
	s.Hydra = map[string]*hydra.Client{"tenant": hydra.NewClient("http://127.0.0.1:1")}
	s.Cfg.PortalInternalURL = portal.URL
	s.Cfg.PortalLookupToken = "svc-token"
	s.Cfg.NotifyPortalURL = "https://portal.example"
	return s, callsFn
}

// The rotate form carries a one-time nonce, minted fresh per render — two
// renders of the same page must not share one, or a stale tab replays clean.
func TestRotateCredential_FormMintsAFreshNoncePerRender(t *testing.T) {
	s, _ := newRotateNonceTestServer(t)

	first := getUserDetailRotateNonce(t, s)
	if first == "" {
		t.Fatal("user-detail rotate form carries no rotate_nonce field — a re-POSTed form is indistinguishable from a fresh one (goat#580)")
	}
	if first == s.csrfNonce {
		t.Fatal("rotate nonce is the static CSRF nonce — it must be one-time, minted per render")
	}
	if second := getUserDetailRotateNonce(t, s); second == first {
		t.Fatal("two renders minted the same rotate nonce — a stale tab's re-POST would pass as fresh")
	}
}

// The defect itself: refresh / back-then-forward / double-click re-POSTs the
// same form. The first POST rotates; the replay must 409 and the portal must
// NOT be asked to rotate a second time.
func TestRotateCredential_ReplayedPostRefusedWithoutASecondRotation(t *testing.T) {
	s, callsFn := newRotateNonceTestServer(t)

	nonce := getUserDetailRotateNonce(t, s)
	if nonce == "" {
		t.Fatal("no rotate_nonce on the page — see TestRotateCredential_FormMintsAFreshNoncePerRender")
	}

	if w := postRotateWithNonce(t, s, nonce); w.Code != http.StatusOK {
		t.Fatalf("first POST = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if n := len(callsFn()); n != 1 {
		t.Fatalf("portal called %d times after the first POST, want 1", n)
	}

	w := postRotateWithNonce(t, s, nonce)
	if w.Code != http.StatusConflict {
		t.Fatalf("replayed POST = %d, want 409 — a refresh of the hand-over page must not re-key (goat#580); body=%s",
			w.Code, w.Body.String())
	}
	if n := len(callsFn()); n != 1 {
		t.Fatalf("portal called %d times after the replay, want STILL 1 — the replay silently invalidated the key being read out", n)
	}
}

// No nonce, or a nonce the console never minted: refused before the portal
// is touched. Fail closed — the cost of a wrong refusal is a page reload;
// the cost of a wrong acceptance is an unrecoverable re-key.
func TestRotateCredential_NoncelessOrForeignNonceNeverTouchesThePortal(t *testing.T) {
	s, callsFn := newRotateNonceTestServer(t)

	if w := postRotateWithNonce(t, s, ""); w.Code != http.StatusConflict {
		t.Fatalf("nonce-less POST = %d, want 409; body=%s", w.Code, w.Body.String())
	}
	if w := postRotateWithNonce(t, s, "never-minted"); w.Code != http.StatusConflict {
		t.Fatalf("foreign-nonce POST = %d, want 409; body=%s", w.Code, w.Body.String())
	}
	if n := len(callsFn()); n != 0 {
		t.Fatalf("portal called %d times across two refused POSTs, want 0", n)
	}
}

// The immediate half of goat#580: the rotate form disables its submit button
// on submit, exactly like the strictly-less-destructive offboard form already
// does — a double-click must not fire two POSTs.
func TestTemplates_userDetailRotateFormDisablesItsSubmitButton(t *testing.T) {
	out := renderPage(t, "user_detail", &pageVars{
		User: &userDetail{ID: "x", Email: "u@example.org"}, CSRFToken: "nonce-123",
	})
	i := strings.Index(out, "/users/x/rotate-credential")
	if i < 0 {
		t.Fatal("rotate form not found on user_detail")
	}
	// The form's onsubmit attribute sits in the same tag as the action.
	tag := out[i:]
	if j := strings.Index(tag, ">"); j > 0 {
		tag = tag[:j]
	}
	if !strings.Contains(tag, "disabled=true") {
		t.Errorf("rotate form does not disable its submit button on submit (the offboard pattern); form tag = %q", tag)
	}
}

// The nonce is spent before the portal is called, so a rotation that FAILED
// burns its nonce too. The retry is still refused — that is the point — but
// the refusal must not tell the admin the earlier submission produced a live
// credential, because on this path nothing was rotated at all and the
// person's existing credential is the one still working.
func TestRotateCredential_FailedRotateBurnsItsNonceAndSaysSo(t *testing.T) {
	kc, _ := fakeKratos2FA(t, "id-1", "u@example.org", http.StatusOK, http.StatusNoContent, posturePasswordOnly)
	portal, callsFn := fakeRotatePortal(t, http.StatusBadGateway, map[string]any{"error": "portal down"})
	s := newUsersAPITestServer(t, kc)
	s.Hydra = map[string]*hydra.Client{"tenant": hydra.NewClient("http://127.0.0.1:1")}
	s.Cfg.PortalInternalURL = portal.URL
	s.Cfg.PortalLookupToken = "svc-token"
	s.Cfg.NotifyPortalURL = "https://portal.example"

	nonce := s.mintRotateNonce()
	if w := postRotateWithNonce(t, s, nonce); w.Code != http.StatusBadGateway {
		t.Fatalf("first POST against a failing portal = %d, want 502; body=%s", w.Code, w.Body.String())
	}
	if n := len(callsFn()); n != 1 {
		t.Fatalf("portal called %d times, want 1", n)
	}

	w := postRotateWithNonce(t, s, nonce)
	if w.Code != http.StatusConflict {
		t.Fatalf("retry after a failed rotate = %d, want 409 (the nonce is spent either way)", w.Code)
	}
	if n := len(callsFn()); n != 1 {
		t.Fatalf("portal called %d times after the retry, want STILL 1", n)
	}
	body := w.Body.String()
	if strings.Contains(body, "original rotation is the live one") {
		t.Error("the refusal claims the earlier submission produced the live credential, but that rotation failed — " +
			"the person's existing credential is the live one and the admin is being told the opposite")
	}
	if !strings.Contains(body, "audit trail") {
		t.Errorf("the refusal should send the admin to the audit trail for what the earlier submission actually did; body=%s", body)
	}
}

// A detail page left open past the TTL cannot rotate: the nonce is refused
// and the portal is never touched. Aged in place because waiting out
// rotateNonceTTL in a test is not an option.
func TestRotateCredential_NonceExpiresAfterItsTTL(t *testing.T) {
	s, callsFn := newRotateNonceTestServer(t)

	nonce := s.mintRotateNonce()
	s.rotateNonceMu.Lock()
	s.rotateNonces[nonce] = time.Now().Add(-time.Minute)
	s.rotateNonceMu.Unlock()

	if w := postRotateWithNonce(t, s, nonce); w.Code != http.StatusConflict {
		t.Fatalf("expired-nonce POST = %d, want 409; body=%s", w.Code, w.Body.String())
	}
	if n := len(callsFn()); n != 0 {
		t.Fatalf("portal called %d times on an expired nonce, want 0", n)
	}
}

// Minting prunes what has expired, so the store stays the size of the pages
// rendered in the last window rather than every page ever rendered. A live
// nonce minted alongside the stale ones survives the sweep.
func TestRotateCredential_MintPrunesExpiredNonces(t *testing.T) {
	s, _ := newRotateNonceTestServer(t)

	live := s.mintRotateNonce()
	s.rotateNonceMu.Lock()
	for _, stale := range []string{"stale-1", "stale-2", "stale-3"} {
		s.rotateNonces[stale] = time.Now().Add(-time.Minute)
	}
	before := len(s.rotateNonces)
	s.rotateNonceMu.Unlock()

	fresh := s.mintRotateNonce()

	s.rotateNonceMu.Lock()
	after := len(s.rotateNonces)
	_, staleKept := s.rotateNonces["stale-1"]
	s.rotateNonceMu.Unlock()

	if staleKept {
		t.Error("an expired nonce survived a mint — the store grows with every page render")
	}
	if after >= before {
		t.Errorf("store went from %d to %d entries across a mint that added one and should have dropped three", before, after)
	}
	if !s.consumeRotateNonce(live) {
		t.Error("the prune took a live nonce with it — an admin's open page would refuse for no reason")
	}
	if !s.consumeRotateNonce(fresh) {
		t.Error("the nonce just minted is not spendable")
	}
}
