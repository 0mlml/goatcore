package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/config"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/kratos"
)

// stepUpTestServer builds a Server whose Kratos reports the given actor
// posture, so the gate can be exercised without a browser.
func stepUpTestServer(t *testing.T, posture actorPosture, enforced, devBypass bool) *Server {
	t.Helper()
	kc, _ := fakeKratos2FA(t, "id-1", "u@example.org", http.StatusOK, http.StatusNoContent, posture)
	return &Server{
		Cfg: &config.Config{
			StepUpEnforced:  enforced,
			DevBypassAuth:   devBypass,
			AAL2FreshWindow: 5 * time.Minute,
			StepUpSSOIdP:    "tenant",
		},
		Kratos:  map[string]*kratos.Client{"tenant": kc},
		StepUps: newStepUpStore(),
	}
}

// reqWithIdP returns a request already carrying the tenant IdP in context,
// since the gate resolves the Kratos client from there.
func reqWithIdP(_ *Server) *http.Request {
	r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/users/id-1/delete", nil)
	return r.WithContext(context.WithValue(r.Context(), ctxKeyIdP, config.IdP{Key: "tenant"}))
}

// TestRequireAAL2_StepUpEnforcedPolicy pins the per-goatnet step-up policy knob
// (STEP_UP_ENFORCED) against the LIVE Kratos-resolved AAL. The gate must fail
// closed by default, be satisfiable only by a FRESH second factor, and be
// waivable ONLY by the explicit sandbox policy or the dev-only bypass — never
// silently.
//
// The old version of this test drove AAL through an X-Forwarded-Auth header.
// That header was never populated by anything (#174: Kratos emits no acr claim
// and oauth2-proxy cannot map an arbitrary claim to a header), which is exactly
// why every gated action 403'd in production. The signal now comes from the
// Kratos session ledger.
func TestRequireAAL2_StepUpEnforcedPolicy(t *testing.T) {
	cases := []struct {
		name        string
		posture     actorPosture
		enforced    bool
		devBypass   bool
		wantAllowed bool
	}{
		{"enforced + password-only -> refuse (prod default)", posturePasswordOnly, true, false, false},
		{"enforced + STALE aal2 -> refuse (freshness window)", postureStaleAAL2, true, false, false},
		{"enforced + fresh aal2 -> allow (real step-up)", postureFreshAAL2, true, false, true},
		{"not enforced + password-only -> allow (sandbox policy)", posturePasswordOnly, false, false, true},
		{"not enforced + stale aal2 -> allow (sandbox policy)", postureStaleAAL2, false, false, true},
		{"dev bypass + password-only -> allow (dev only)", posturePasswordOnly, true, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := stepUpTestServer(t, tc.posture, tc.enforced, tc.devBypass)
			err := s.requireAAL2(reqWithIdP(s), Actor{IdentityID: "actor-1"})
			if tc.wantAllowed && err != nil {
				t.Fatalf("want allowed, got refusal: %v", err)
			}
			if !tc.wantAllowed && err == nil {
				t.Fatalf("want refusal, got allowed")
			}
		})
	}
}

// TestStepUpVerified_ClaimsOnlyRealFactors is the audit-honesty invariant
// (#174 Story C): step_up_verified may be true ONLY when a fresh second factor
// actually backed the request. Every other path — policy-skipped, stale
// session, no factor, dev bypass — must report false, because in none of them
// did anyone present a second factor.
func TestStepUpVerified_ClaimsOnlyRealFactors(t *testing.T) {
	cases := []struct {
		name      string
		posture   actorPosture
		enforced  bool
		devBypass bool
		want      bool
	}{
		{"fresh aal2 -> true", postureFreshAAL2, true, false, true},
		{"fresh aal2 while policy off -> true (a factor really was presented)", postureFreshAAL2, false, false, true},
		{"policy off + password-only -> false", posturePasswordOnly, false, false, false},
		{"stale aal2 -> false", postureStaleAAL2, true, false, false},
		{"dev bypass -> false (nothing was verified)", posturePasswordOnly, true, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := stepUpTestServer(t, tc.posture, tc.enforced, tc.devBypass)
			if got := s.stepUpVerified(reqWithIdP(s), Actor{IdentityID: "actor-1"}); got != tc.want {
				t.Fatalf("stepUpVerified = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestResolveStepUpState_FailsClosed pins that an unusable signal never reads
// as "verified". A Kratos outage or a sidecar that forgot to forward the
// identity ID must refuse the mutation, not admit it.
func TestResolveStepUpState_FailsClosed(t *testing.T) {
	s := stepUpTestServer(t, postureFreshAAL2, true, false)

	if st := s.resolveStepUpState(t.Context(), Actor{}); st.Fresh || st.Err == nil {
		t.Fatalf("missing identity ID must fail closed with an error; got %+v", st)
	}
	s.Cfg.StepUpSSOIdP = "nonexistent-realm"
	if st := s.resolveStepUpState(t.Context(), Actor{IdentityID: "actor-1"}); st.Fresh || st.Err == nil {
		t.Fatalf("unknown SSO realm must fail closed with an error; got %+v", st)
	}
}

// TestStepUpChallenge_OneShot pins that a redeemed nonce cannot be replayed —
// a leaked return URL must not re-run the mutation it was minted for.
func TestStepUpChallenge_OneShot(t *testing.T) {
	st := newStepUpStore()
	nonce, err := st.issue(stepUpChallenge{Action: "delete_identity", ActorEmail: "a@example.org"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := st.peek(nonce); !ok {
		t.Fatal("peek must not consume the challenge")
	}
	if _, ok := st.redeem(nonce); !ok {
		t.Fatal("first redeem must succeed")
	}
	if _, ok := st.redeem(nonce); ok {
		t.Fatal("second redeem must MISS — the nonce is one-shot")
	}
}

// TestStepUpVerifyURL_TargetsKratosAAL2 pins the redirect contract: the
// operator is sent to the Ory self-service login on the auth.* origin (not the
// admin.auth.* console origin), asking for aal2 with a forced fresh
// credential presentation, and returning to the redemption endpoint carrying
// the nonce.
func TestStepUpVerifyURL_TargetsKratosAAL2(t *testing.T) {
	s := &Server{Cfg: &config.Config{
		StepUpReturnToBase: "https://admin.auth.zagros.example.net",
		AAL2FreshWindow:    5 * time.Minute,
	}}
	got := s.stepUpVerifyURL("NONCE123", "operator")

	for _, want := range []string{
		"https://auth.zagros.example.net/login?",
		"aal=aal2",
		"refresh=true",
		// The administered realm rides the return leg so the resumed mutation
		// re-enters the SAME identity plane instead of defaulting.
		"return_to=https%3A%2F%2Fadmin.auth.zagros.example.net%2Fstep-up%2Freturn%3Fnonce%3DNONCE123%26idp%3Doperator",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("verify URL %q missing %q", got, want)
		}
	}
	if s.stepUpSettingsURL() != "https://auth.zagros.example.net/settings" {
		t.Fatalf("settings URL = %q", s.stepUpSettingsURL())
	}
}
