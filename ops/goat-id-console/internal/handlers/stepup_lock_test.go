package handlers

// "Lock now" — the control that lets an operator end their own AAL2 freshness
// window early.
//
// The behavior under test is deliberately one-directional: locking may only
// make the gate stricter. These tests assert both halves of that — that a lock
// actually causes the next gated action to challenge, and that it can never
// admit an action that would otherwise have been refused.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/config"
)

// postLock drives the "Lock now" control as the same platform admin the other
// step-up tests use.
func postLock(t *testing.T, h http.Handler, csrf, returnTo string) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{"csrf": {csrf}, "return_to": {returnTo}}
	r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/step-up/lock", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("X-Forwarded-Groups", "PLATFORM-ADMIN")
	r.Header.Set("X-Forwarded-Email", "admin@example.org")
	r.Header.Set("X-Forwarded-User", testActorID)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// TestStepUpLock_FreshActorIsChallengedAfterLocking is the whole point: an
// admin whose window is open completes a gated action without interruption,
// and after locking the very same action challenges instead.
//
// This is what makes verifying the gate deterministic. Enrolling a factor
// opens a window of its own, so without this the #188 live-fire has to wait
// out AAL2_FRESH_WINDOW_SEC or start a new session before it can observe a
// challenge at all.
func TestStepUpLock_FreshActorIsChallengedAfterLocking(t *testing.T) {
	kc, deletedFn := fakeKratos2FA(t, "id-1", "u@example.org", http.StatusOK, http.StatusNoContent, postureFreshSettled)
	s := newUsersAPITestServer(t, kc)

	// Before locking: fresh, so the mutation runs.
	if w := post2FA(t, s.Routes(), s.csrfNonce, true); w.Code == http.StatusSeeOther &&
		strings.HasPrefix(w.Header().Get("Location"), "/step-up?nonce=") {
		t.Fatalf("precondition failed: a fresh actor was challenged; this test cannot show the lock caused anything")
	}
	if got := deletedFn(); len(got) == 0 {
		t.Fatalf("precondition failed: fresh actor's mutation did not run")
	}

	// Lock, then retry the same action.
	lock := postLock(t, s.Routes(), s.csrfNonce, "/users")
	if lock.Code != http.StatusSeeOther {
		t.Fatalf("lock status = %d, want 303", lock.Code)
	}
	if loc := lock.Header().Get("Location"); loc != "/users" {
		t.Fatalf("lock redirected to %q, want /users", loc)
	}

	w := post2FA(t, s.Routes(), s.csrfNonce, true)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("post-lock status = %d, want 303 into step-up", w.Code)
	}
	if loc := w.Header().Get("Location"); !strings.HasPrefix(loc, "/step-up?nonce=") {
		t.Fatalf("post-lock Location = %q, want /step-up?nonce=…", loc)
	}
}

// A lock must never admit anything. An actor with no second factor at all is
// refused before and after — locking cannot be turned into a bypass.
func TestStepUpLock_CannotAdmitAnActionItWouldOtherwiseRefuse(t *testing.T) {
	kc, deletedFn := fakeKratos2FA(t, "id-1", "u@example.org", http.StatusOK, http.StatusNoContent, posturePasswordOnly)
	s := newUsersAPITestServer(t, kc)

	postLock(t, s.Routes(), s.csrfNonce, "/users")

	w := post2FA(t, s.Routes(), s.csrfNonce, true)
	if w.Code != http.StatusSeeOther || !strings.HasPrefix(w.Header().Get("Location"), "/step-up?nonce=") {
		t.Fatalf("after locking, a factor-less actor must still be challenged; got %d %q",
			w.Code, w.Header().Get("Location"))
	}
	if got := deletedFn(); len(got) != 0 {
		t.Fatalf("locking admitted a mutation it must have refused; deleted=%v", got)
	}
}

// The lock is a state-changing POST, so it carries the same CSRF requirement as
// every other console mutation.
func TestStepUpLock_RejectsMissingCSRF(t *testing.T) {
	kc, _ := fakeKratos2FA(t, "id-1", "u@example.org", http.StatusOK, http.StatusNoContent, postureFreshAAL2)
	s := newUsersAPITestServer(t, kc)

	w := postLock(t, s.Routes(), "not-the-nonce", "/users")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad-CSRF lock status = %d, want 400", w.Code)
	}
}

// return_to is attacker-influencable, so it must not be usable as an open
// redirect off this console.
func TestStepUpLock_RefusesOffSiteReturnTo(t *testing.T) {
	kc, _ := fakeKratos2FA(t, "id-1", "u@example.org", http.StatusOK, http.StatusNoContent, postureFreshAAL2)
	s := newUsersAPITestServer(t, kc)

	for _, bad := range []string{
		"https://evil.example/phish",
		"//evil.example/phish",
		"javascript:alert(1)",
	} {
		w := postLock(t, s.Routes(), s.csrfNonce, bad)
		if loc := w.Header().Get("Location"); loc != "/" {
			t.Errorf("return_to %q redirected to %q, want the safe default /", bad, loc)
		}
	}
}

// A lock changes the actor's own step-up posture, so it has to leave a trace.
// Without this the emission is a claim in a comment: the handler would still
// redirect 303 and every other lock test would pass with the audit line deleted.
//
// StepUpVerified must be false. Locking needs no second factor precisely
// because it can only tighten the gate, and an audit row asserting a factor
// that was never presented is the one dishonesty ADR 1015 §"audit honesty"
// rules out.
func TestStepUpLock_AuditRecorded(t *testing.T) {
	kc, _ := fakeKratos2FA(t, "id-1", "u@example.org", http.StatusOK, http.StatusNoContent, postureFreshSettled)
	s := newUsersAPITestServer(t, kc)

	if w := postLock(t, s.Routes(), s.csrfNonce, "/users"); w.Code != http.StatusSeeOther {
		t.Fatalf("lock status = %d, want 303", w.Code)
	}

	ev := lastAudit(t, s, "step_up_lock")
	if ev.Result != "ok" {
		t.Errorf("result = %q, want ok", ev.Result)
	}
	if ev.StepUpVerified {
		t.Error("step_up_verified = true; locking presents no second factor and must never claim one")
	}
	if ev.Actor.Email != "admin@example.org" || ev.Actor.IdentityID != testActorID {
		t.Errorf("actor = %+v, want the locking operator attributed by email and identity ID", ev.Actor)
	}
}

// A lock that fails leaves the window open, so it must not leave an "ok" row
// behind claiming otherwise. The no-identity-ID path is the only way to fail.
func TestStepUpLock_FailedLockIsNotAuditedAsOK(t *testing.T) {
	kc, _ := fakeKratos2FA(t, "id-1", "u@example.org", http.StatusOK, http.StatusNoContent, postureFreshSettled)
	s := newUsersAPITestServer(t, kc)

	form := url.Values{"csrf": {s.csrfNonce}, "return_to": {"/users"}}
	r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/step-up/lock", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("X-Forwarded-Groups", "PLATFORM-ADMIN")
	r.Header.Set("X-Forwarded-Email", "admin@example.org")
	// No X-Forwarded-User: no identity ID to key the lock on.
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
	for _, e := range s.Audit.Recent(50) {
		if e.Action == "step_up_lock" {
			t.Fatalf("a lock that did not take was audited as %q; the row claims a window was closed that is still open", e.Result)
		}
	}
}

func TestIsLocalURL(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want bool
	}{
		{"/", true},
		{"/users", true},
		{"/users?idp=tenant", true},
		{"", false},
		{"//evil.example", false},
		{"https://evil.example", false},
		{"javascript:alert(1)", false},
		{"users", false},
		// Browsers normalise a backslash to "/" in the authority position, so
		// these leave the origin despite starting with a single "/".
		{`/\evil.example`, false},
		{`/\/evil.example`, false},
		{`/users\..\..\evil`, false},
		// Browsers strip tab, CR and LF before parsing, so each of these
		// becomes "//evil.example".
		{"/\t/evil.example", false},
		{"/\n/evil.example", false},
		{"/\r/evil.example", false},
		{"/users\x00", false},
		{"/users\x7f", false},
		// Percent-encoded forms stay on this origin: the browser does not
		// decode them before resolving the path.
		{"/%09/evil.example", true},
	} {
		if got := isLocalURL(tc.in); got != tc.want {
			t.Errorf("isLocalURL(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// The control must appear only where it means something: while the window is
// open. Offering "Lock now" on a strip that already says the window is closed
// asks the operator to do something that has already happened.
func TestStepUpLock_ControlRendersOnlyWhileTheWindowIsOpen(t *testing.T) {
	const control = `action="/step-up/lock"`

	for _, tc := range []struct {
		name string
		su   *stepUpVars
		want bool
	}{
		{"fresh", &stepUpVars{Enforced: true, Fresh: true, HasSecondFactor: true, RemainingSec: 200}, true},
		{"locked", &stepUpVars{Enforced: true, Locked: true, HasSecondFactor: true}, false},
		{"idle", &stepUpVars{Enforced: true, HasSecondFactor: true}, false},
		{"no factor", &stepUpVars{Enforced: true}, false},
		// Not enforced means no strip at all, so nothing to lock.
		{"not enforced", &stepUpVars{Fresh: true, HasSecondFactor: true}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := renderPage(t, "users_list", &pageVars{StepUp: tc.su, CSRFToken: "tok", RequestPath: "/users"})
			if got := strings.Contains(out, control); got != tc.want {
				t.Errorf("Lock now control present = %v, want %v", got, tc.want)
			}
		})
	}
}

// The locked strip must say who closed the window and what happens next.
// "Not verified recently" would be true but useless — it reads as a fault
// rather than as the consequence of something the operator just did.
func TestStepUpLock_LockedStripExplainsItself(t *testing.T) {
	out := renderPage(t, "users_list", &pageVars{
		StepUp:      &stepUpVars{Enforced: true, Locked: true, HasSecondFactor: true},
		CSRFToken:   "tok",
		RequestPath: "/users",
	})
	for _, want := range []string{"you ended your step-up window", "will ask for your second factor"} {
		if !strings.Contains(out, want) {
			t.Errorf("locked strip missing %q", want)
		}
	}
}

// A lock must outlive the freshness window it closes. Retention was originally
// a hardcoded hour while AAL2_FRESH_WINDOW_SEC is operator-configurable, so a
// window longer than an hour would see the lock pruned while the superseded
// aal2 event was still fresh — silently re-opening what the operator closed.
func TestStepUpLockRetention_AlwaysOutlivesTheFreshnessWindow(t *testing.T) {
	for _, window := range []time.Duration{
		5 * time.Minute,  // the default
		90 * time.Minute, // longer than the old hardcoded hour
		24 * time.Hour,   // pathological but expressible
	} {
		s := &Server{Cfg: &config.Config{AAL2FreshWindow: window}}
		if got := s.stepUpLockRetention(); got <= window {
			t.Errorf("retention %s does not outlive window %s — a lock would expire while the aal2 event it superseded was still fresh",
				got, window)
		}
	}
}

// The lock is keyed on the actor's identity ID. Without one there is nothing to
// key on, and reporting success would tell the operator their window is closed
// when it is not.
func TestStepUpLock_ReportsFailureWithoutAnIdentityID(t *testing.T) {
	s := &Server{Cfg: &config.Config{AAL2FreshWindow: 5 * time.Minute}}
	if s.lockStepUp("") {
		t.Fatal("lockStepUp(\"\") reported success; it cannot have locked anything")
	}
	if s.lockStepUp("actor-1") != true {
		t.Fatal("lockStepUp with an identity ID should report success")
	}
	if _, ok := s.relockedAt("actor-1"); !ok {
		t.Fatal("relockedAt does not see the lock just taken")
	}
	if _, ok := s.relockedAt("someone-else"); ok {
		t.Fatal("one actor's lock must not apply to another actor")
	}
}
