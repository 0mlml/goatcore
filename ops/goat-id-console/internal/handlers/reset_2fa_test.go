package handlers

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/audit"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/kratos"
	"github.com/usareur-af/goatcore/ops/goat-id-console/web"
)

// renderPage render-executes one page template the way Server.render does, so a
// field typo fails at execute time. Returns the rendered HTML.
func renderPage(t *testing.T, page string, vars *pageVars) string {
	t.Helper()
	base, err := loadTemplates()
	if err != nil {
		t.Fatalf("loadTemplates: %v", err)
	}
	tm, err := base.Clone()
	if err != nil {
		t.Fatalf("clone: %v", err)
	}
	data, err := fs.ReadFile(web.FS, "templates/"+page+".html")
	if err != nil {
		t.Fatalf("read %s: %v", page, err)
	}
	if _, err := tm.New(page).Parse(string(data)); err != nil {
		t.Fatalf("parse %s: %v", page, err)
	}
	var b strings.Builder
	if err := tm.ExecuteTemplate(&b, "layout", vars); err != nil {
		t.Fatalf("execute %s: %v", page, err)
	}
	return b.String()
}

// Story 2: the user-detail page carries the reset-2FA control (form + CSRF +
// confirm) and shows the success banner on ?reset2fa=ok.
func TestTemplates_userDetailResetControl(t *testing.T) {
	out := renderPage(t, "user_detail", &pageVars{
		User: &userDetail{ID: "x", Email: "u@example.org", HasSecondFactor: true}, CSRFToken: "nonce-123",
	})
	for _, m := range []string{
		"Reset second factor", "/users/x/reset-2fa", `value="nonce-123"`,
		"return confirm(", "✓ 2FA enrolled",
	} {
		if !strings.Contains(out, m) {
			t.Errorf("user_detail reset control missing %q", m)
		}
	}
	// Banner is absent without the flag, present with it.
	if strings.Contains(out, "Second factor reset —") {
		t.Error("banner must not render without Reset2FAOK")
	}
	ok := renderPage(t, "user_detail", &pageVars{User: &userDetail{ID: "x"}, Reset2FAOK: true})
	if !strings.Contains(ok, "Second factor reset —") || !strings.Contains(ok, "Mint a new recovery link") {
		t.Error("reset2fa=ok must render the success banner with a mint-recovery nudge")
	}

	// Not enrolled: the reset button is disabled (grayed + unclickable via CSS)
	// and the status shows the amber "2FA not enrolled" badge.
	noMFA := renderPage(t, "user_detail", &pageVars{
		User: &userDetail{ID: "x", Email: "u@example.org", HasSecondFactor: false}, CSRFToken: "n",
	})
	if !strings.Contains(noMFA, `class="danger" disabled>Reset second factor`) {
		t.Error("reset button must be disabled when the user has no second factor")
	}
	if !strings.Contains(noMFA, `<span class="status-badge warn">2FA not enrolled</span>`) {
		t.Error("not-enrolled status must show the amber warn badge")
	}
}

// Story 2: users-list carries the MFA column reflecting HasSecondFactor and the
// client-side filter input + script.
func TestTemplates_usersListMFAColumnAndFilter(t *testing.T) {
	out := renderPage(t, "users_list", &pageVars{Users: []userRow{
		{ID: "a", Email: "has@example.org", HasSecondFactor: true},
		{ID: "b", Email: "no@example.org", HasSecondFactor: false},
	}})
	if !strings.Contains(out, "<th>MFA</th>") {
		t.Error("users_list missing MFA column header")
	}
	if !strings.Contains(out, "✓ 2FA") {
		t.Error("users_list must render ✓ 2FA for a factor-holder")
	}
	for _, m := range []string{`id="user-filter"`, "filterUsers(", `data-filter="has@example.org`} {
		if !strings.Contains(out, m) {
			t.Errorf("users_list filter missing %q", m)
		}
	}
}

// fakeKratos2FA serves the admin API the reset-2FA handler calls: a GET on the
// identity plus a per-credential DELETE it records. getStatus forces the GET
// response (200 identity, 404 unknown, 500 error); deleteStatus forces the
// DELETE response (204 clear, 404 no-op, 500 error). actorPosture selects what
// the fake Kratos reports for the ACTOR's own sessions, which is where the
// step-up gate now reads AAL from (#174 — there is no acr/X-Forwarded-Auth
// header; see stepup.go for why).
type actorPosture string

// testActorID is the test admin's Kratos identity UUID, carried on
// X-Forwarded-User the way oauth2-proxy forwards the OIDC sub in production
// (goat#247). It MUST be UUID-shaped: actorFromHeaders rejects anything else,
// which is itself part of what these tests pin.
const testActorID = "11111111-2222-4333-8444-555555555555"

const (
	posturePasswordOnly actorPosture = "password-only" // no second factor anywhere
	postureFreshAAL2    actorPosture = "fresh"         // stepped up just now
	// postureFreshSettled is fresh but NOT re-authenticating on every request:
	// the aal2 completed_at sits a little in the past, the way a real session's
	// does. postureFreshAAL2 restamps at serve time so the actor looks like
	// they just authenticated on every call, which is right for the redemption
	// leg but hides anything that compares against a moment in between — a
	// deliberate re-lock, most obviously.
	postureFreshSettled actorPosture = "fresh-settled"
	postureStaleAAL2    actorPosture = "stale" // stepped up long ago
)

func fakeKratos2FA(t *testing.T, id, email string, getStatus, deleteStatus int, posture actorPosture) (*kratos.Client, func() []string) {
	t.Helper()
	var deleted []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/credentials/"):
			parts := strings.Split(r.URL.Path, "/credentials/")
			deleted = append(deleted, parts[len(parts)-1])
			w.WriteHeader(deleteStatus)

		// The actor's own session list · the step-up freshness signal.
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/sessions"):
			_ = json.NewEncoder(w).Encode(actorSessions(posture))

		// The actor's own identity record — the step-up ENROLLMENT signal
		// (credential truth, goat#247 defect 2). Served unconditionally so
		// getStatus keeps forcing only the TARGET fetch below, the way the
		// fail-closed target tests rely on.
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, testActorID):
			_ = json.NewEncoder(w).Encode(actorIdentity(posture))

		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/admin/identities/"):
			if getStatus != http.StatusOK {
				w.WriteHeader(getStatus)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id": id, "state": "active",
				"traits": map[string]interface{}{"email": email},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return kratos.NewClient(srv.URL), func() []string { sort.Strings(deleted); return deleted }
}

// actorIdentity renders the actor's own identity record. Enrollment mirrors
// the posture: every posture except password-only means a TOTP credential is
// on the identity (a stale aal2 admin is still ENROLLED — that distinction is
// goat#247 defect 2).
func actorIdentity(p actorPosture) map[string]interface{} {
	ident := map[string]interface{}{
		"id": testActorID, "state": "active",
		"traits": map[string]interface{}{"email": "admin@example.org"},
	}
	if p != posturePasswordOnly {
		ident["credentials"] = map[string]interface{}{
			"totp": map[string]interface{}{"config": map[string]interface{}{}},
		}
	}
	return ident
}

// actorSessions renders the Kratos /sessions payload for a given posture.
func actorSessions(p actorPosture) []map[string]interface{} {
	base := map[string]interface{}{
		"id": "sess-1", "active": true,
		"expires_at":       time.Now().Add(time.Hour).Format(time.RFC3339),
		"authenticated_at": time.Now().Format(time.RFC3339),
	}
	switch p {
	case postureFreshAAL2:
		base["authenticator_assurance_level"] = "aal2"
		base["authentication_methods"] = []map[string]interface{}{
			{"method": "password", "aal": "aal1", "completed_at": time.Now().Add(-time.Minute).Format(time.RFC3339)},
			// Stamped at serve time so it post-dates any challenge issued
			// microseconds earlier — the redemption leg requires the aal2
			// authentication to be NEWER than the challenge.
			{"method": "totp", "aal": "aal2", "completed_at": time.Now().Format(time.RFC3339Nano)},
		}
	case postureFreshSettled:
		settled := time.Now().Add(-30 * time.Second)
		base["authenticator_assurance_level"] = "aal2"
		base["authentication_methods"] = []map[string]interface{}{
			{"method": "password", "aal": "aal1", "completed_at": settled.Add(-time.Minute).Format(time.RFC3339)},
			{"method": "totp", "aal": "aal2", "completed_at": settled.Format(time.RFC3339Nano)},
		}
	case postureStaleAAL2:
		// Reached aal2, but hours ago — must NOT satisfy the freshness window.
		old := time.Now().Add(-4 * time.Hour)
		base["authenticator_assurance_level"] = "aal2"
		base["authentication_methods"] = []map[string]interface{}{
			{"method": "password", "aal": "aal1", "completed_at": old.Format(time.RFC3339)},
			{"method": "totp", "aal": "aal2", "completed_at": old.Format(time.RFC3339)},
		}
	default:
		base["authenticator_assurance_level"] = "aal1"
		base["authentication_methods"] = []map[string]interface{}{
			{"method": "password", "aal": "aal1", "completed_at": time.Now().Add(-time.Minute).Format(time.RFC3339)},
		}
	}
	return []map[string]interface{}{base}
}

// post2FA drives POST /users/{id}/reset-2fa with the given actor AAL and CSRF.
// post2FA drives POST /users/{id}/reset-2fa. The actor's AAL is NOT a header
// any more — it comes from the fake Kratos's session list, selected by the
// posture passed to fakeKratos2FA. What the request must carry is the
// identity ID, since that is how the console asks Kratos about the caller.
func post2FA(t *testing.T, h http.Handler, csrf string, withCSRF bool) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{}
	if withCSRF {
		form.Set("csrf", csrf)
	}
	r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/users/id-1/reset-2fa?idp=tenant", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("X-Forwarded-Groups", "PLATFORM-ADMIN")
	r.Header.Set("X-Forwarded-Email", "admin@example.org")
	r.Header.Set("X-Forwarded-User", testActorID)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func lastAudit(t *testing.T, s *Server, action string) audit.Event {
	t.Helper()
	for _, e := range s.Audit.Recent(50) {
		if e.Action == action {
			return e
		}
	}
	t.Fatalf("no audit event with action=%q", action)
	return audit.Event{}
}

func TestReset2FA_OKClearsAllThree(t *testing.T) {
	kc, deletedFn := fakeKratos2FA(t, "id-1", "u@example.org", http.StatusOK, http.StatusNoContent, postureFreshAAL2)
	s := newUsersAPITestServer(t, kc)
	w := post2FA(t, s.Routes(), s.csrfNonce, true)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303; body=%s", w.Code, w.Body.String())
	}
	if got := deletedFn(); strings.Join(got, ",") != "lookup_secret,totp,webauthn" {
		t.Fatalf("deleted = %v, want all three factor types", got)
	}
	ev := lastAudit(t, s, "reset_2fa")
	if ev.Result != "ok" || !ev.StepUpVerified {
		t.Fatalf("audit = {result:%q step_up:%v}, want {ok true}", ev.Result, ev.StepUpVerified)
	}
	if ev.Actor.Email != "admin@example.org" {
		t.Fatalf("actor = %q, want admin@example.org", ev.Actor.Email)
	}
}

// A sub-AAL2 admin is now REDIRECTED into the step-up flow rather than
// dead-ended on a 403 (#174 Story B). The mutation must still not happen, and
// the audit must not claim a second factor.
func TestReset2FA_AAL1RedirectsToStepUp(t *testing.T) {
	kc, deletedFn := fakeKratos2FA(t, "id-1", "u@example.org", http.StatusOK, http.StatusNoContent, posturePasswordOnly)
	s := newUsersAPITestServer(t, kc)
	w := post2FA(t, s.Routes(), s.csrfNonce, true)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303 (redirect to step-up)", w.Code)
	}
	if loc := w.Header().Get("Location"); !strings.HasPrefix(loc, "/step-up?nonce=") {
		t.Fatalf("Location = %q, want a /step-up?nonce= challenge", loc)
	}
	if got := deletedFn(); len(got) != 0 {
		t.Fatalf("AAL1 refusal must not delete credentials; got %v", got)
	}
	if ev := lastAudit(t, s, "reset_2fa"); ev.Result != "refused" || ev.StepUpVerified {
		t.Fatalf("audit = {result:%q step_up:%v}, want {refused false}", ev.Result, ev.StepUpVerified)
	}
}

// TestReset2FA_StepUpNotEnforcedProceeds pins the deployed ZAGROS path: on a
// goatnet that declares STEP_UP_ENFORCED=false (no AAL2 signal is wired — the
// oauth2-proxy sidecar emits no acr/X-Forwarded-Auth), the unconditional
// requireAAL2 gate short-circuits so the reset actually executes, and the audit
// row still lands with step_up_verified=false. Without this policy the action
// would 403 every admin. See docs/design/id-admin-console.md §"Step-up
// enforcement policy" and ZAGROS-CORE/transport-identity-console#6/#7.
func TestReset2FA_StepUpNotEnforcedProceeds(t *testing.T) {
	kc, deletedFn := fakeKratos2FA(t, "id-1", "u@example.org", http.StatusOK, http.StatusNoContent, posturePasswordOnly)
	s := newUsersAPITestServer(t, kc)
	s.Cfg.StepUpEnforced = false
	w := post2FA(t, s.Routes(), s.csrfNonce, true)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303; body=%s", w.Code, w.Body.String())
	}
	if got := deletedFn(); strings.Join(got, ",") != "lookup_secret,totp,webauthn" {
		t.Fatalf("deleted = %v, want all three factor types", got)
	}
	ev := lastAudit(t, s, "reset_2fa")
	if ev.Result != "ok" {
		t.Fatalf("audit result = %q, want ok", ev.Result)
	}
	if ev.StepUpVerified {
		t.Fatal("step_up_verified must stay false when the gate was policy-skipped, not satisfied")
	}
}

// Fail-closed on the target fetch: the audit row must always carry target
// attribution, so a failed GetIdentity aborts before any credential DELETE.
func TestReset2FA_TargetFetchFails502(t *testing.T) {
	kc, deletedFn := fakeKratos2FA(t, "id-1", "u@example.org", http.StatusInternalServerError, http.StatusNoContent, postureFreshAAL2)
	s := newUsersAPITestServer(t, kc)
	w := post2FA(t, s.Routes(), s.csrfNonce, true)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", w.Code)
	}
	if got := deletedFn(); len(got) != 0 {
		t.Fatalf("failed target fetch must not delete credentials; got %v", got)
	}
}

// A nonexistent identity is a 404, not a blind-delete "success": without the
// fail-closed fetch, every DELETE would 404 into the benign no-op path and the
// handler would audit "ok" + redirect success for an identity that is not there.
func TestReset2FA_TargetNotFound404(t *testing.T) {
	kc, deletedFn := fakeKratos2FA(t, "id-1", "u@example.org", http.StatusNotFound, http.StatusNoContent, postureFreshAAL2)
	s := newUsersAPITestServer(t, kc)
	w := post2FA(t, s.Routes(), s.csrfNonce, true)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
	if got := deletedFn(); len(got) != 0 {
		t.Fatalf("unknown identity must not trigger credential deletes; got %v", got)
	}
}

func TestReset2FA_BadCSRF(t *testing.T) {
	kc, deletedFn := fakeKratos2FA(t, "id-1", "u@example.org", http.StatusOK, http.StatusNoContent, postureFreshAAL2)
	s := newUsersAPITestServer(t, kc)
	w := post2FA(t, s.Routes(), "wrong-nonce", true)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if got := deletedFn(); len(got) != 0 {
		t.Fatalf("bad CSRF must not delete credentials; got %v", got)
	}
}

func TestReset2FA_KratosErrorIs502(t *testing.T) {
	kc, _ := fakeKratos2FA(t, "id-1", "u@example.org", http.StatusOK, http.StatusInternalServerError, postureFreshAAL2)
	s := newUsersAPITestServer(t, kc)
	w := post2FA(t, s.Routes(), s.csrfNonce, true)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", w.Code)
	}
	if ev := lastAudit(t, s, "reset_2fa"); ev.Result != "error" {
		t.Fatalf("audit result = %q, want error", ev.Result)
	}
}

// A 404 on every credential DELETE (identity had no factors) is a benign
// no-op — the flow still succeeds.
func TestReset2FA_NoFactorNoOp(t *testing.T) {
	kc, _ := fakeKratos2FA(t, "id-1", "u@example.org", http.StatusOK, http.StatusNotFound, postureFreshAAL2)
	s := newUsersAPITestServer(t, kc)
	w := post2FA(t, s.Routes(), s.csrfNonce, true)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303; body=%s", w.Code, w.Body.String())
	}
	if ev := lastAudit(t, s, "reset_2fa"); ev.Result != "ok" {
		t.Fatalf("audit result = %q, want ok", ev.Result)
	}
}

// A non-admin session (no admin-granting group) is blocked by adminGate before
// the handler runs — no Kratos mutation.
func TestReset2FA_NonAdminBlocked(t *testing.T) {
	kc, deletedFn := fakeKratos2FA(t, "id-1", "u@example.org", http.StatusOK, http.StatusNoContent, postureFreshAAL2)
	s := newUsersAPITestServer(t, kc)
	form := url.Values{}
	form.Set("csrf", s.csrfNonce)
	r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/users/id-1/reset-2fa?idp=tenant", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("X-Forwarded-Groups", "OBSERVER")
	r.Header.Set("X-Forwarded-User", testActorID)
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (adminGate)", w.Code)
	}
	if got := deletedFn(); len(got) != 0 {
		t.Fatalf("non-admin must not delete credentials; got %v", got)
	}
}
