package handlers

// End-to-end coverage of the step-up redirect flow (#174 Story B) and the UX
// that makes the AAL2 requirement visible BEFORE an admin clicks (Story B's
// badge/banner/interstitial).

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/config"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/kratos"
)

// getStepUp drives a GET on a console page as a platform admin.
func getStepUp(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil)
	r.Header.Set("X-Forwarded-Groups", "PLATFORM-ADMIN")
	r.Header.Set("X-Forwarded-Email", "admin@example.org")
	r.Header.Set("X-Forwarded-User", testActorID)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// TestStepUp_AAL1ChallengeThenVerifyCompletesMutation is the headline
// acceptance criterion: a sub-AAL2 admin attempting a gated action is
// redirected to step-up (not 403'd), the interstitial explains the
// requirement, and once Kratos reports a fresh second factor the redemption
// leg hands the operator back to their action.
func TestStepUp_AAL1ChallengeThenVerifyCompletesMutation(t *testing.T) {
	// Phase 1 — password-only admin hits the gate.
	kc, deletedFn := fakeKratos2FA(t, "id-1", "u@example.org", http.StatusOK, http.StatusNoContent, posturePasswordOnly)
	s := newUsersAPITestServer(t, kc)
	w := post2FA(t, s.Routes(), s.csrfNonce, true)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("gate status = %d, want 303", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.HasPrefix(loc, "/step-up?nonce=") {
		t.Fatalf("Location = %q, want /step-up?nonce=…", loc)
	}
	if got := deletedFn(); len(got) != 0 {
		t.Fatalf("challenged action must not mutate; deleted=%v", got)
	}
	nonce := mustNonce(t, loc)

	// The interstitial must NAME the action and offer a way forward. This
	// admin has no factor at all, so it must route them to enrolment rather
	// than to a verify button they cannot satisfy.
	page := getStepUp(t, s.Routes(), loc)
	if page.Code != http.StatusOK {
		t.Fatalf("interstitial status = %d, want 200", page.Code)
	}
	for _, want := range []string{
		"Extra verification needed",
		"Reset second factor",
		"lowers their account security",
		"don't have a second factor enrolled",
		"Enroll an authenticator",
	} {
		if !strings.Contains(page.Body.String(), want) {
			t.Errorf("interstitial missing %q", want)
		}
	}

	// Phase 2 — the operator enrolls + verifies; Kratos now reports a fresh
	// aal2. Swap in a Kratos that says so, keeping the same challenge store.
	freshKC, freshDeleted := fakeKratos2FA(t, "id-1", "u@example.org", http.StatusOK, http.StatusNoContent, postureFreshAAL2)
	s.Kratos["tenant"] = freshKC

	ret := getStepUp(t, s.Routes(), "/step-up/return?nonce="+url.QueryEscape(nonce)+"&idp=tenant")
	if ret.Code != http.StatusOK {
		t.Fatalf("return leg status = %d, want 200 (resume page)", ret.Code)
	}
	body := ret.Body.String()
	if !strings.Contains(body, "Verified — resuming your action") {
		t.Fatalf("return leg did not render the resume page:\n%s", body)
	}
	// The resume page must re-POST to the original mutation with a live CSRF.
	if !strings.Contains(body, `action="/users/id-1/reset-2fa?idp=tenant"`) {
		t.Errorf("resume form does not target the original mutation:\n%s", body)
	}
	if !strings.Contains(body, `value="`+s.csrfNonce+`"`) {
		t.Error("resume form missing a live CSRF token")
	}

	// Phase 3 — the replayed POST now completes, and audits an HONEST
	// step_up_verified:true.
	done := post2FA(t, s.Routes(), s.csrfNonce, true)
	if done.Code != http.StatusSeeOther {
		t.Fatalf("replayed mutation status = %d, want 303", done.Code)
	}
	if got := freshDeleted(); strings.Join(got, ",") != "lookup_secret,totp,webauthn" {
		t.Fatalf("replayed mutation deleted = %v, want all three factors", got)
	}
	if ev := lastAudit(t, s, "reset_2fa"); ev.Result != "ok" || !ev.StepUpVerified {
		t.Fatalf("audit = {result:%q step_up:%v}, want {ok true}", ev.Result, ev.StepUpVerified)
	}
}

// TestStepUp_ReturnWithoutVerifyingDoesNotBurnTheChallenge pins the retry
// affordance: a mistyped TOTP code must leave the operator able to try again,
// not force them to restart the whole action.
func TestStepUp_ReturnWithoutVerifyingDoesNotBurnTheChallenge(t *testing.T) {
	kc, _ := fakeKratos2FA(t, "id-1", "u@example.org", http.StatusOK, http.StatusNoContent, posturePasswordOnly)
	s := newUsersAPITestServer(t, kc)
	loc := post2FA(t, s.Routes(), s.csrfNonce, true).Header().Get("Location")
	nonce := mustNonce(t, loc)

	ret := getStepUp(t, s.Routes(), "/step-up/return?nonce="+url.QueryEscape(nonce)+"&idp=tenant")
	if ret.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (re-rendered interstitial)", ret.Code)
	}
	if !strings.Contains(ret.Body.String(), "didn't see a completed second factor") {
		t.Fatalf("expected the retry explanation, got:\n%s", ret.Body.String())
	}
	if _, ok := s.StepUps.peek(nonce); !ok {
		t.Fatal("a failed verification must NOT consume the challenge")
	}
}

// TestStepUp_ChallengeIsBoundToItsOperator pins that one admin cannot redeem
// another's challenge, even holding the nonce.
func TestStepUp_ChallengeIsBoundToItsOperator(t *testing.T) {
	kc, _ := fakeKratos2FA(t, "id-1", "u@example.org", http.StatusOK, http.StatusNoContent, postureFreshAAL2)
	s := newUsersAPITestServer(t, kc)
	nonce, err := s.StepUps.issue(stepUpChallenge{
		Action: "delete_identity", Method: http.MethodPost,
		ReturnTo: "/users/id-1/delete", ActorEmail: "someone-else@example.org",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/step-up", "/step-up/return"} {
		w := getStepUp(t, s.Routes(), path+"?nonce="+url.QueryEscape(nonce)+"&idp=tenant")
		if w.Code != http.StatusForbidden {
			t.Fatalf("%s status = %d, want 403 (challenge belongs to another operator)", path, w.Code)
		}
	}
}

// A challenge's ReturnTo is server-captured, so it is same-origin by
// construction and none of this is reachable today. It is guarded anyway
// because the value flows into FOUR navigation targets — the redemption
// redirect, the replayed form action, and the interstitial's "Cancel and go
// back" link on both the initial and the failed-verify render — and a future
// caller that captures a ReturnTo from anywhere less trustworthy would turn
// all four into open redirects at once.
//
// The store is seeded directly because that is the only way to express the
// hostile value: the capture path normalises it, which is exactly the property
// being pinned. Nothing that reaches a navigation target may carry it.
func TestStepUp_HostileReturnToNeverReachesANavigationTarget(t *testing.T) {
	for _, bad := range []string{
		"https://evil.example/phish",
		"//evil.example/phish",
		`/\evil.example`,
		"javascript:alert(1)",
	} {
		t.Run(bad, func(t *testing.T) {
			// Interstitial + failed-verify render: password-only, so the return
			// leg re-renders rather than redeeming.
			kc, _ := fakeKratos2FA(t, "id-1", "u@example.org", http.StatusOK, http.StatusNoContent, posturePasswordOnly)
			s := newUsersAPITestServer(t, kc)
			nonce, err := s.StepUps.issue(stepUpChallenge{
				Action: "reset_2fa", Method: http.MethodPost,
				ReturnTo: bad, ActorEmail: "admin@example.org", IdPKey: "tenant",
			})
			if err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{"/step-up", "/step-up/return"} {
				w := getStepUp(t, s.Routes(), path+"?nonce="+url.QueryEscape(nonce)+"&idp=tenant")
				if strings.Contains(w.Body.String(), bad) {
					t.Errorf("%s rendered the hostile return path %q", path, bad)
				}
			}

			// Redemption, POST: the value would become the replay form action.
			freshKC, _ := fakeKratos2FA(t, "id-1", "u@example.org", http.StatusOK, http.StatusNoContent, postureFreshAAL2)
			s.Kratos["tenant"] = freshKC
			ret := getStepUp(t, s.Routes(), "/step-up/return?nonce="+url.QueryEscape(nonce)+"&idp=tenant")
			if strings.Contains(ret.Body.String(), bad) {
				t.Errorf("resume form targets the hostile return path %q", bad)
			}
			if !strings.Contains(ret.Body.String(), `action="/"`) {
				t.Errorf("resume form did not fall back to the safe default:\n%s", ret.Body.String())
			}

			// Redemption, GET: the value would become the Location header.
			getNonce, err := s.StepUps.issue(stepUpChallenge{
				Action: "reset_2fa", Method: http.MethodGet,
				ReturnTo: bad, ActorEmail: "admin@example.org", IdPKey: "tenant",
			})
			if err != nil {
				t.Fatal(err)
			}
			got := getStepUp(t, s.Routes(), "/step-up/return?nonce="+url.QueryEscape(getNonce)+"&idp=tenant")
			if loc := got.Header().Get("Location"); loc != "/" {
				t.Errorf("redemption redirected to %q, want the safe default /", loc)
			}
		})
	}
}

// The capture path is where the invariant is actually established, so pin that
// a rejected path is never STORED — the redemption-leg check is defense in
// depth, and a test that only exercised it would pass with capture unguarded.
func TestStepUp_CapturedReturnToIsNormalisedBeforeItIsStored(t *testing.T) {
	kc, _ := fakeKratos2FA(t, "id-1", "u@example.org", http.StatusOK, http.StatusNoContent, posturePasswordOnly)
	s := newUsersAPITestServer(t, kc)
	loc := post2FA(t, s.Routes(), s.csrfNonce, true).Header().Get("Location")

	ch, ok := s.StepUps.peek(mustNonce(t, loc))
	if !ok {
		t.Fatal("challenge not stored")
	}
	if !isLocalURL(ch.ReturnTo) {
		t.Fatalf("stored ReturnTo %q would be rejected by the guard", ch.ReturnTo)
	}
	// Still the real mutation, not a blanket fallback: normalising must not
	// quietly discard a legitimate return path.
	if ch.ReturnTo != "/users/id-1/reset-2fa?idp=tenant" {
		t.Fatalf("stored ReturnTo = %q, want the pinned mutation", ch.ReturnTo)
	}
}

// TestStepUp_UnknownNonceIsRefused covers the expired / already-used case.
func TestStepUp_UnknownNonceIsRefused(t *testing.T) {
	kc, _ := fakeKratos2FA(t, "id-1", "u@example.org", http.StatusOK, http.StatusNoContent, postureFreshAAL2)
	s := newUsersAPITestServer(t, kc)
	w := getStepUp(t, s.Routes(), "/step-up?nonce=not-a-real-nonce&idp=tenant")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "expired or was already used") {
		t.Fatalf("unhelpful message: %s", w.Body.String())
	}
}

// --- the "admins know they need AAL2" UX ------------------------------------

// TestTemplates_GatedControlsCarryAAL2Badge is the discoverability guarantee:
// every step-up-gated control on the user-detail page is marked BEFORE it is
// clicked, so the requirement is never a surprise.
func TestTemplates_GatedControlsCarryAAL2Badge(t *testing.T) {
	// Sessions must be non-empty for the revoke-sessions control (and so its
	// badge) to render at all.
	out := renderPage(t, "user_detail", &pageVars{
		User:      &userDetail{ID: "x", Email: "u@example.org", HasSecondFactor: true},
		CSRFToken: "n",
		Sessions:  []sessionRow{{AAL: "aal1"}},
		StepUp:    &stepUpVars{Enforced: true, WindowMinutes: 5},
	})
	// Disable · Save groups · Revoke all sessions · Reset second factor · Delete
	if n := strings.Count(out, "AAL2 required"); n < 5 {
		t.Fatalf("only %d AAL2 badges rendered; want one on each of the 5 gated controls", n)
	}
	if !strings.Contains(out, "second factor (authenticator app or passkey)") {
		t.Error("badge tooltip must say what will be asked for")
	}

	// Inside the freshness window the badge flips to unlocked, so an admin can
	// tell the next click will not interrupt them.
	fresh := renderPage(t, "user_detail", &pageVars{
		User:      &userDetail{ID: "x", Email: "u@example.org", HasSecondFactor: true},
		CSRFToken: "n",
		StepUp:    &stepUpVars{Enforced: true, Fresh: true, RemainingSec: 240, WindowMinutes: 5},
	})
	if !strings.Contains(fresh, "AAL2 ✓") || strings.Contains(fresh, "AAL2 required") {
		t.Error("fresh step-up must render the unlocked badge, not the requirement badge")
	}

	// A goatnet that does not enforce step-up gets no badges at all — the
	// console must not advertise a gate that never fires.
	off := renderPage(t, "user_detail", &pageVars{
		User: &userDetail{ID: "x", Email: "u@example.org"}, CSRFToken: "n",
		StepUp: &stepUpVars{Enforced: false},
	})
	if strings.Contains(off, "aal2-badge") {
		t.Error("STEP_UP_ENFORCED=false must render no AAL2 badges")
	}
}

// TestTemplates_StepUpBannerStatesThePosture pins the persistent strip: it
// tells an admin whether they are currently unlocked, and — critically for the
// password-only majority — routes them to enrolment before they hit a gate.
func TestTemplates_StepUpBannerStatesThePosture(t *testing.T) {
	idle := renderPage(t, "users_list", &pageVars{
		StepUp: &stepUpVars{Enforced: true, HasSecondFactor: false, EnrollURL: "https://auth.x/settings", WindowMinutes: 5},
	})
	if !strings.Contains(idle, "High-risk actions require a second factor") {
		t.Error("idle banner must state the requirement")
	}
	if !strings.Contains(idle, "enroll an authenticator first") {
		t.Error("an admin with no factor must be pointed at enrollment from the banner")
	}

	fresh := renderPage(t, "users_list", &pageVars{
		StepUp: &stepUpVars{Enforced: true, Fresh: true, HasSecondFactor: true, RemainingSec: 240, WindowMinutes: 5},
	})
	if !strings.Contains(fresh, "Step-up active") || !strings.Contains(fresh, `data-stepup-expires-in="240"`) {
		t.Error("fresh banner must show the live remaining window")
	}
}

// TestTemplates_StepUpPagesRender guards both new templates against field typos.
func TestTemplates_StepUpPagesRender(t *testing.T) {
	out := renderPage(t, "step_up", &pageVars{
		StepUp: &stepUpVars{Enforced: true},
		StepUpChallenge: &stepUpChallengeVars{
			Nonce: "n1", Action: "delete_identity",
			ActionLabel: "Delete identity", Why: "because it is irreversible",
			ReturnTo: "/users/x", VerifyURL: "https://auth.x/login?aal=aal2",
			EnrollURL: "https://auth.x/settings", HasSecondFactor: true, WindowMinutes: 5,
		},
	})
	for _, want := range []string{
		"Delete identity", "because it is irreversible",
		"Verify with second factor", "https://auth.x/login?aal=aal2",
		"stay unlocked for 5 minutes", "Cancel and go back",
		"NIST SP 800-53",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("step_up page missing %q", want)
		}
	}

	resume := renderPage(t, "step_up_resume", &pageVars{
		CSRFToken: "live-nonce",
		StepUpResume: &stepUpResumeVars{
			ActionLabel: "Delete identity",
			PostTo:      "/users/x/delete?idp=tenant",
			Fields:      url.Values{"confirm_email": {"u@example.org"}},
		},
	})
	for _, want := range []string{
		"Verified — resuming your action", `action="/users/x/delete?idp=tenant"`,
		`name="confirm_email" value="u@example.org"`, `value="live-nonce"`,
	} {
		if !strings.Contains(resume, want) {
			t.Errorf("step_up_resume page missing %q", want)
		}
	}
}

// TestStepUpState_RemainingNeverNegative guards the banner countdown.
func TestStepUpState_RemainingNeverNegative(t *testing.T) {
	past := stepUpState{Fresh: true, ExpiresAt: time.Now().Add(-time.Minute)}
	if got := past.Remaining(); got != 0 {
		t.Fatalf("Remaining() = %v, want 0 for an elapsed window", got)
	}
	if got := (stepUpState{}).Remaining(); got != 0 {
		t.Fatalf("Remaining() = %v, want 0 when not fresh", got)
	}
}

func mustNonce(t *testing.T, loc string) string {
	t.Helper()
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("parse %q: %v", loc, err)
	}
	n := u.Query().Get("nonce")
	if n == "" {
		t.Fatalf("no nonce in %q", loc)
	}
	return n
}

// --- cross-realm administration (the SSO-realm pin) --------------------------

// withOperatorRealm bolts a second identity plane onto the test server: the
// SSO realm ("tenant") serves the ACTOR's sessions; the administered realm
// ("operator") serves the TARGET identity and records credential deletes.
func withOperatorRealm(t *testing.T, s *Server, actorPost actorPosture) func() []string {
	t.Helper()
	opKC, opDeleted := fakeKratos2FA(t, "id-1", "u@example.org", http.StatusOK, http.StatusNoContent, actorPost)
	s.Kratos["operator"] = opKC
	s.Cfg.IdPs["operator"] = config.IdP{Key: "operator", Label: "Operators"}
	s.Cfg.IdPOrder = append(s.Cfg.IdPOrder, "operator")
	return opDeleted
}

// postReset2FAOnRealm drives POST /users/id-1/reset-2fa?idp=<realm> as the
// standard test admin.
func postReset2FAOnRealm(t *testing.T, s *Server, realm string) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{}
	form.Set("csrf", s.csrfNonce)
	r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/users/id-1/reset-2fa?idp="+realm, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("X-Forwarded-Groups", "PLATFORM-ADMIN")
	r.Header.Set("X-Forwarded-Email", "admin@example.org")
	r.Header.Set("X-Forwarded-User", testActorID)
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, r)
	return w
}

// TestStepUp_CrossRealmAdministration pins the SSO-realm fix: an admin whose
// identity lives in the SSO realm administering the OTHER identity plane must
// still satisfy the gate. The actor's posture resolves against the SSO realm
// (Cfg.StepUpSSOIdP) — never the ?idp= administered realm, which has never
// heard of the actor. Before the fix, the posture lookup ran against the
// administered realm, failed closed, and — with the challenge legs then
// defaulting realms — the resumed mutation re-gated into a fresh challenge
// forever.
func TestStepUp_CrossRealmAdministration(t *testing.T) {
	// SSO realm: the actor is freshly stepped up. Administered realm: the
	// target is password-only (its posture must be irrelevant to the gate).
	ssoKC, ssoDeleted := fakeKratos2FA(t, "unused", "unused@example.org", http.StatusOK, http.StatusNoContent, postureFreshAAL2)
	s := newUsersAPITestServer(t, ssoKC)
	opDeleted := withOperatorRealm(t, s, posturePasswordOnly)

	w := postReset2FAOnRealm(t, s, "operator")

	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303 (gate satisfied via SSO realm); body=%s", w.Code, w.Body.String())
	}
	if loc := w.Header().Get("Location"); !strings.HasPrefix(loc, "/users/id-1?idp=operator") {
		t.Fatalf("Location = %q, want the operator-realm detail page", loc)
	}
	if got := opDeleted(); strings.Join(got, ",") != "lookup_secret,totp,webauthn" {
		t.Fatalf("deleted on administered realm = %v, want all three factor types", got)
	}
	if got := ssoDeleted(); len(got) != 0 {
		t.Fatalf("SSO realm must see no credential deletes; got %v", got)
	}
	if ev := lastAudit(t, s, "reset_2fa"); ev.Result != "ok" || !ev.StepUpVerified {
		t.Fatalf("audit = {result:%q step_up:%v}, want {ok true}", ev.Result, ev.StepUpVerified)
	}
}

// TestStepUp_CrossRealmChallengeRoundTrip walks the full challenge → verify →
// resume loop on the second identity plane: the challenge redirect and the
// Kratos return leg both carry idp=operator, the posture check runs against
// the SSO realm, and the resumed mutation lands back on the operator plane.
func TestStepUp_CrossRealmChallengeRoundTrip(t *testing.T) {
	ssoKC, _ := fakeKratos2FA(t, "unused", "unused@example.org", http.StatusOK, http.StatusNoContent, posturePasswordOnly)
	s := newUsersAPITestServer(t, ssoKC)
	opDeleted := withOperatorRealm(t, s, posturePasswordOnly)

	// Phase 1 — not stepped up: the gate challenges, carrying the plane.
	w := postReset2FAOnRealm(t, s, "operator")
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303 (challenge)", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "idp=operator") {
		t.Fatalf("challenge redirect %q must carry the administered realm", loc)
	}
	nonce := mustNonce(t, loc)
	ch, ok := s.StepUps.peek(nonce)
	if !ok || ch.IdPKey != "operator" || ch.ReturnTo != "/users/id-1/reset-2fa?idp=operator" {
		t.Fatalf("challenge = %+v, want the operator-plane mutation pinned", ch)
	}
	if got := opDeleted(); len(got) != 0 {
		t.Fatalf("challenged action must not mutate; got %v", got)
	}

	// Phase 2 — the operator steps up (in the SSO realm, where their identity
	// lives), then the return leg redeems and resumes onto the operator plane.
	freshSSO, _ := fakeKratos2FA(t, "unused", "unused@example.org", http.StatusOK, http.StatusNoContent, postureFreshAAL2)
	s.Kratos["tenant"] = freshSSO
	ret := getStepUp(t, s.Routes(), "/step-up/return?nonce="+url.QueryEscape(nonce)+"&idp=operator")
	if ret.Code != http.StatusOK {
		t.Fatalf("return leg status = %d, want 200 (resume page); body=%s", ret.Code, ret.Body.String())
	}
	if !strings.Contains(ret.Body.String(), `action="/users/id-1/reset-2fa?idp=operator"`) {
		t.Fatalf("resume form must target the operator-plane mutation:\n%s", ret.Body.String())
	}

	// Phase 3 — the replayed POST completes against the operator plane.
	done := postReset2FAOnRealm(t, s, "operator")
	if done.Code != http.StatusSeeOther {
		t.Fatalf("replayed mutation status = %d, want 303; body=%s", done.Code, done.Body.String())
	}
	if got := opDeleted(); strings.Join(got, ",") != "lookup_secret,totp,webauthn" {
		t.Fatalf("deleted on administered realm = %v, want all three factor types", got)
	}
	if ev := lastAudit(t, s, "reset_2fa"); ev.Result != "ok" || !ev.StepUpVerified {
		t.Fatalf("audit = {result:%q step_up:%v}, want {ok true}", ev.Result, ev.StepUpVerified)
	}
}

// --- goat#247 regression pins -------------------------------------------------

// TestStepUp_EnrolledButNotFreshOffersVerifyNotEnrol pins defect 2: enrollment
// is credential truth from the identity record, not session history. An admin
// with TOTP enrolled whose aal2 is stale must be offered the Verify button —
// never told "you don't have a second factor enrolled yet" (the live ZAGROS
// symptom that opened goat#247).
func TestStepUp_EnrolledButNotFreshOffersVerifyNotEnrol(t *testing.T) {
	kc, _ := fakeKratos2FA(t, "id-1", "u@example.org", http.StatusOK, http.StatusNoContent, postureStaleAAL2)
	s := newUsersAPITestServer(t, kc)

	loc := post2FA(t, s.Routes(), s.csrfNonce, true).Header().Get("Location")
	if !strings.HasPrefix(loc, "/step-up?nonce=") {
		t.Fatalf("stale-aal2 admin must still be challenged; Location = %q", loc)
	}
	page := getStepUp(t, s.Routes(), loc)
	body := page.Body.String()
	if !strings.Contains(body, "Verify with second factor") {
		t.Error("enrolled admin must get the Verify button")
	}
	if strings.Contains(body, "don't have a second factor enrolled") {
		t.Error("enrolled admin must NEVER be told they are not enrolled")
	}
}

// TestStepUp_UnenrolledEnrolLinkReturnsToTheChallenge pins defect 6: the enroll
// link opens the Kratos settings flow with a return_to back to the redemption
// endpoint, so enrolment resumes the pinned action instead of dead-ending on
// the settings page.
func TestStepUp_UnenrolledEnrolLinkReturnsToTheChallenge(t *testing.T) {
	kc, _ := fakeKratos2FA(t, "id-1", "u@example.org", http.StatusOK, http.StatusNoContent, posturePasswordOnly)
	s := newUsersAPITestServer(t, kc)

	loc := post2FA(t, s.Routes(), s.csrfNonce, true).Header().Get("Location")
	page := getStepUp(t, s.Routes(), loc)
	body := page.Body.String()
	if !strings.Contains(body, "/self-service/settings/browser?return_to=") {
		t.Fatalf("enroll link must open the settings flow with a return_to:\n%s", body)
	}
	if !strings.Contains(body, url.QueryEscape("/step-up/return?nonce="+mustNonce(t, loc))) {
		t.Error("enroll return_to must carry the challenge nonce back to /step-up/return")
	}
}

// TestStepUp_UnresolvableActorIsUnknownNotUnenrolled pins the display half of
// defect 1. The interstitial already says "couldn't check" when the actor has
// no identity ID; the DISPLAY accessor must agree, because the banner it feeds
// renders on every page. The zero stepUpState would report PostureUnknown:false
// + HasSecondFactor:false — telling an enrolled admin they have no second
// factor, which is the bug this change exists to remove.
func TestStepUp_UnresolvableActorIsUnknownNotUnenrolled(t *testing.T) {
	kc, _ := fakeKratos2FA(t, "id-1", "u@example.org", http.StatusOK, http.StatusNoContent, postureStaleAAL2)
	s := newUsersAPITestServer(t, kc)

	st := s.cachedStepUpState(t.Context(), Actor{Email: "admin@example.org"})
	if st.Err == nil {
		t.Fatal("an actor with no identity ID must resolve to an ERROR state, not a posture")
	}
	if st.Enrolled {
		t.Error("unresolvable actor must never be reported as enrolled")
	}

	// PostureUnknown is what the template keys on, and it supersedes
	// HasSecondFactor — so the banner says "couldn't check", not "enroll first".
	vars := s.stepUpVars(st)
	if !vars.PostureUnknown {
		t.Errorf("banner must render posture-unknown for an unresolvable actor, got %+v", vars)
	}
}

// TestStepUp_PostureUnknownIsHonest pins defect 3: a failed posture lookup
// (Kratos down) must render as "couldn't check", never as the not-enrolled
// wall, and must offer neither verify nor enroll — both would be untruthful.
func TestStepUp_PostureUnknownIsHonest(t *testing.T) {
	kc, _ := fakeKratos2FA(t, "id-1", "u@example.org", http.StatusOK, http.StatusNoContent, posturePasswordOnly)
	s := newUsersAPITestServer(t, kc)

	// Challenge is minted while Kratos still answers…
	loc := post2FA(t, s.Routes(), s.csrfNonce, true).Header().Get("Location")
	nonce := mustNonce(t, loc)

	// …then Kratos goes dark before the interstitial renders.
	dark := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(dark.Close)
	s.Kratos["tenant"] = kratos.NewClient(dark.URL)

	page := getStepUp(t, s.Routes(), "/step-up?nonce="+url.QueryEscape(nonce)+"&idp=tenant")
	body := page.Body.String()
	if !strings.Contains(body, "couldn't check your verification status") {
		t.Fatalf("posture-unknown must say the lookup failed:\n%s", body)
	}
	for _, mustNot := range []string{
		"don't have a second factor enrolled",
		"Verify with second factor",
		"Enroll an authenticator",
	} {
		if strings.Contains(body, mustNot) {
			t.Errorf("posture-unknown page must not render %q", mustNot)
		}
	}

	// The return leg with an unknown posture must keep the challenge alive and
	// stay just as honest.
	ret := getStepUp(t, s.Routes(), "/step-up/return?nonce="+url.QueryEscape(nonce)+"&idp=tenant")
	if !strings.Contains(ret.Body.String(), "couldn't check your verification status") {
		t.Error("return leg with unknown posture must render the honest message")
	}
	if _, ok := s.StepUps.peek(nonce); !ok {
		t.Error("unknown posture must not consume the challenge")
	}
}

// TestStepUp_NonUUIDSubjectNeverPasses pins defect 1's guard: a forwarded
// subject that is not a Kratos identity UUID (a pairwise-subject client, an
// email) must challenge — and never be sent to Kratos as an identity ID.
func TestStepUp_NonUUIDSubjectNeverPasses(t *testing.T) {
	var askedPaths []string
	probe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		askedPaths = append(askedPaths, r.URL.Path)
		// The reset-2fa handler fetches its TARGET before the gate runs; serve
		// that so the request reaches the gate. Everything else 404s.
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/id-1") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"id-1","state":"active","traits":{"email":"u@example.org"}}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(probe.Close)
	s := newUsersAPITestServer(t, kratos.NewClient(probe.URL))

	form := url.Values{"csrf": {s.csrfNonce}}
	r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/users/id-1/reset-2fa?idp=tenant", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("X-Forwarded-Groups", "PLATFORM-ADMIN")
	r.Header.Set("X-Forwarded-Email", "admin@example.org")
	r.Header.Set("X-Forwarded-User", "not-a-uuid-subject")
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, r)

	if loc := w.Header().Get("Location"); !strings.HasPrefix(loc, "/step-up?nonce=") {
		t.Fatalf("non-UUID subject must be challenged; got %d %q", w.Code, loc)
	}
	for _, p := range askedPaths {
		if strings.Contains(p, "not-a-uuid-subject") {
			t.Errorf("non-UUID subject leaked into a Kratos admin call: %s", p)
		}
	}
}

func TestIdentityIDFromSubject(t *testing.T) {
	if got := identityIDFromSubject("11111111-2222-4333-8444-555555555555"); got == "" {
		t.Error("a UUID subject must pass through")
	}
	for _, bad := range []string{"", "actor-1", "admin@example.org", "11111111-2222-4333-8444-55555555555Z"} {
		if got := identityIDFromSubject(bad); got != "" {
			t.Errorf("identityIDFromSubject(%q) = %q, want empty", bad, got)
		}
	}
}

// TestStepUp_LegacyPreferredUsernameHeaderIsNotHonored pins the production
// header NAME — the regression test defect 1 never had. The gate's identity
// ID must come from X-Forwarded-User (the OIDC sub) and nothing else: a
// request carrying a perfectly valid identity UUID on the OLD header,
// X-Forwarded-Preferred-Username, must fail closed into a challenge, because
// honoring that header again would mean trusting a claim this stack never
// mints (and hand-set test headers would be hiding the gap all over again).
func TestStepUp_LegacyPreferredUsernameHeaderIsNotHonored(t *testing.T) {
	// Kratos would report a fully fresh, enrolled actor — IF the console
	// managed to ask about them. The point is that it must never get that far.
	kc, deletedFn := fakeKratos2FA(t, "id-1", "u@example.org", http.StatusOK, http.StatusNoContent, postureFreshAAL2)
	s := newUsersAPITestServer(t, kc)

	form := url.Values{"csrf": {s.csrfNonce}}
	r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/users/id-1/reset-2fa?idp=tenant", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("X-Forwarded-Groups", "PLATFORM-ADMIN")
	r.Header.Set("X-Forwarded-Email", "admin@example.org")
	r.Header.Set("X-Forwarded-Preferred-Username", testActorID) // old header, valid UUID
	// Deliberately NO X-Forwarded-User.
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, r)

	if loc := w.Header().Get("Location"); !strings.HasPrefix(loc, "/step-up?nonce=") {
		t.Fatalf("legacy header must not satisfy the gate; got %d %q", w.Code, loc)
	}
	if got := deletedFn(); len(got) != 0 {
		t.Fatalf("legacy-header request must not mutate; deleted=%v", got)
	}
}

// --- goat#252 review fixes ----------------------------------------------------

// TestStepUp_EnrolChallengeGetsTheLongerDeadline: a first-time enroller's
// round trip (install an app, scan the QR, type a code) routinely outlives the
// 10-minute verify budget, and the enroll page promises the pinned action will
// resume. Rendering the ENROLL branch widens the challenge deadline to
// stepUpEnrolNonceTTL — fixed cap from IssuedAt, so re-renders cannot keep a
// challenge alive — and rendering the VERIFY branch must not widen anything.
func TestStepUp_EnrolChallengeGetsTheLongerDeadline(t *testing.T) {
	// Unenrolled actor: the interstitial offers enrolment.
	kc, _ := fakeKratos2FA(t, "id-1", "u@example.org", http.StatusOK, http.StatusNoContent, posturePasswordOnly)
	s := newUsersAPITestServer(t, kc)

	loc := post2FA(t, s.Routes(), s.csrfNonce, true).Header().Get("Location")
	nonce := mustNonce(t, loc)
	before, ok := s.StepUps.peek(nonce)
	if !ok {
		t.Fatal("challenge not found")
	}
	if got := before.Deadline.Sub(before.IssuedAt); got != stepUpNonceTTL {
		t.Fatalf("deadline at issue = IssuedAt+%v, want IssuedAt+%v", got, stepUpNonceTTL)
	}

	getStepUp(t, s.Routes(), loc)
	after, _ := s.StepUps.peek(nonce)
	if got := after.Deadline.Sub(after.IssuedAt); got != stepUpEnrolNonceTTL {
		t.Fatalf("deadline after enroll render = IssuedAt+%v, want IssuedAt+%v", got, stepUpEnrolNonceTTL)
	}

	// Idempotent: a second render must not push the deadline further out.
	getStepUp(t, s.Routes(), loc)
	again, _ := s.StepUps.peek(nonce)
	if !again.Deadline.Equal(after.Deadline) {
		t.Fatalf("re-render moved the deadline: %v -> %v", after.Deadline, again.Deadline)
	}

	// Enrolled actor: the verify branch keeps the standard budget.
	kce, _ := fakeKratos2FA(t, "id-1", "u@example.org", http.StatusOK, http.StatusNoContent, postureStaleAAL2)
	se := newUsersAPITestServer(t, kce)
	locE := post2FA(t, se.Routes(), se.csrfNonce, true).Header().Get("Location")
	getStepUp(t, se.Routes(), locE)
	chE, _ := se.StepUps.peek(mustNonce(t, locE))
	if got := chE.Deadline.Sub(chE.IssuedAt); got != stepUpNonceTTL {
		t.Fatalf("verify-branch deadline = IssuedAt+%v, want IssuedAt+%v", got, stepUpNonceTTL)
	}
}

// TestStepUp_VerifiedReturnDropsTheDisplayCache: with the display cache at
// 60s, a completed verify must bust the actor's cached posture, or the banner
// would keep saying "not verified" for up to a minute after the operator just
// presented a factor.
func TestStepUp_VerifiedReturnDropsTheDisplayCache(t *testing.T) {
	kc, _ := fakeKratos2FA(t, "id-1", "u@example.org", http.StatusOK, http.StatusNoContent, posturePasswordOnly)
	s := newUsersAPITestServer(t, kc)

	loc := post2FA(t, s.Routes(), s.csrfNonce, true).Header().Get("Location")
	nonce := mustNonce(t, loc)

	// Warm the display cache with the pre-verify posture.
	actor := Actor{Email: "admin@example.org", IdentityID: testActorID}
	if st := s.cachedStepUpState(t.Context(), actor); st.Fresh {
		t.Fatal("precondition: actor must start not-fresh")
	}

	// The operator completes step-up; the return leg redeems.
	fresh, _ := fakeKratos2FA(t, "id-1", "u@example.org", http.StatusOK, http.StatusNoContent, postureFreshAAL2)
	s.Kratos["tenant"] = fresh
	ret := getStepUp(t, s.Routes(), "/step-up/return?nonce="+url.QueryEscape(nonce)+"&idp=tenant")
	if ret.Code != http.StatusOK {
		t.Fatalf("return leg = %d, want 200; body=%s", ret.Code, ret.Body.String())
	}

	// Without the drop this returns the still-warm stale copy.
	if st := s.cachedStepUpState(t.Context(), actor); !st.Fresh {
		t.Error("display posture must be fresh immediately after a completed verify, not stepUpStateTTL later")
	}
}
