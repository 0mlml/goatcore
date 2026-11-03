//go:build integration

package integration

// Tests that need a REAL enrolled second factor.
//
// These are the cases the fake-Kratos unit suite structurally cannot cover, and
// the reason the harness exists at all: PR #187 shipped the AAL2 step-up gate
// without ever exercising a real `?aal=aal2` login, and #188 flips
// STEP_UP_ENFORCED=true on a goatnet with 19 admins. Finding the login leg
// misbehaving here is much cheaper than finding it mid-flip.

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/kratos"
)

// liveKratos returns the admin and public base URLs, or skips.
func liveKratos(t *testing.T) (adminURL, publicURL string) {
	t.Helper()
	adminURL = os.Getenv("IDP_TENANT_KRATOS_ADMIN_URL")
	publicURL = os.Getenv("KRATOS_PUBLIC_URL")
	if adminURL == "" || publicURL == "" {
		t.Skip("IDP_TENANT_KRATOS_ADMIN_URL / KRATOS_PUBLIC_URL not set; Kratos stack not running")
	}
	// The suite must never be pointed at a deployed goatnet.
	if err := RequireLoopback(map[string]string{
		"IDP_TENANT_KRATOS_ADMIN_URL": adminURL,
		"KRATOS_PUBLIC_URL":           publicURL,
	}); err != nil {
		t.Fatalf("pre-flight guard refused the configured endpoints: %v", err)
	}
	return adminURL, publicURL
}

func seedForTest(ctx context.Context, t *testing.T, adminURL, publicURL string) (*TOTPIdentity, *kratos.Client) {
	t.Helper()
	email := fmt.Sprintf("totp-%d@example.test", time.Now().UnixNano())
	id, err := SeedTOTPIdentity(ctx, adminURL, publicURL, email, "Correct-Horse-Battery-9!")
	if err != nil {
		t.Fatalf("SeedTOTPIdentity: %v", err)
	}
	cli := kratos.NewClient(adminURL)
	t.Cleanup(func() { _ = cli.DeleteIdentity(context.Background(), id.ID) })
	return id, cli
}

// TestIntegration_RealTOTPFactorFlipsAndResets is the canon gate from epic #161:
// a genuinely enrolled factor must make HasSecondFactor true, and clearing it
// must flip it back. Seeding is load-bearing — Kratos v1.3.1 admin import
// rejects credentials.totp outright, so nothing weaker produces this state.
func TestIntegration_RealTOTPFactorFlipsAndResets(t *testing.T) {
	adminURL, publicURL := liveKratos(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	id, cli := seedForTest(ctx, t, adminURL, publicURL)

	before, err := cli.GetIdentity(ctx, id.ID)
	if err != nil {
		t.Fatalf("GetIdentity before reset: %v", err)
	}
	if _, ok := before.Credentials["totp"]; !ok {
		t.Fatalf("precondition failed: seeded identity %q has no totp credential; enrolment did not take effect", id.ID)
	}
	if !before.HasSecondFactor() {
		t.Fatalf("HasSecondFactor() = false after enrolling a real TOTP factor; want true")
	}

	if err := cli.DeleteIdentityCredential(ctx, id.ID, "totp"); err != nil {
		t.Fatalf("DeleteIdentityCredential(totp): %v", err)
	}

	after, err := cli.GetIdentity(ctx, id.ID)
	if err != nil {
		t.Fatalf("GetIdentity after reset: %v", err)
	}
	if _, ok := after.Credentials["totp"]; ok {
		t.Errorf("totp credential still present after reset")
	}
	if after.HasSecondFactor() {
		t.Errorf("HasSecondFactor() = true after reset; want false (the ✓ 2FA → — flip)")
	}
}

// TestIntegration_AAL2StepUpLoginLeg proves the leg #187 could not: a real
// Kratos `?aal=aal2&refresh=true` login completed with a TOTP code raises the
// session to aal2 AND records a per-method ledger entry, which is the signal
// the console reads live to decide AAL and freshness.
func TestIntegration_AAL2StepUpLoginLeg(t *testing.T) {
	adminURL, publicURL := liveKratos(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	id, _ := seedForTest(ctx, t, adminURL, publicURL)

	aal, methods, err := StepUpAAL2(ctx, publicURL, id)
	if err != nil {
		t.Fatalf("StepUpAAL2: %v", err)
	}
	if aal != "aal2" {
		t.Fatalf("session AAL after TOTP step-up = %q, want aal2", aal)
	}

	// The console does not trust a bare AAL string — it reads
	// authentication_methods[] for a per-factor aal + completed_at. Assert that
	// ledger really carries what the gate depends on.
	var fresh *AuthMethod
	for i := range methods {
		if methods[i].Method == "totp" && methods[i].AAL == "aal2" {
			if fresh == nil || methods[i].CompletedAt.After(fresh.CompletedAt) {
				fresh = &methods[i]
			}
		}
	}
	if fresh == nil {
		t.Fatalf("no totp/aal2 entry in authentication_methods %+v; the console's live AAL signal would be empty", methods)
	}
	if fresh.CompletedAt.IsZero() {
		t.Errorf("totp/aal2 entry has zero completed_at; the freshness window cannot be evaluated without it")
	}
	if age := time.Since(fresh.CompletedAt); age > 5*time.Minute {
		t.Errorf("freshest totp/aal2 completed_at is %s old; expected a just-completed authentication", age)
	}
}

// TestIntegration_WrongTOTPCodeIsRefused is the negative control for the test
// above. Without it, TestIntegration_AAL2StepUpLoginLeg would still pass if
// Kratos accepted any six digits — the assertion would be measuring nothing.
// A wrong code must leave the session un-elevated.
func TestIntegration_WrongTOTPCodeIsRefused(t *testing.T) {
	adminURL, publicURL := liveKratos(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	id, _ := seedForTest(ctx, t, adminURL, publicURL)

	// A code guaranteed wrong: enumerate the valid codes across Kratos's skew
	// window (previous/current/next 30s step) and pick a six-digit string that
	// is none of them. A fixed "000000" has a ~1-in-10^6 chance of being the
	// live code and would turn this negative control into a silent flake.
	now := time.Now()
	valid := map[string]bool{}
	for _, off := range []time.Duration{-30 * time.Second, 0, 30 * time.Second} {
		c, err := id.TOTPCode(now.Add(off))
		if err != nil {
			t.Fatalf("totpCode: %v", err)
		}
		valid[c] = true
	}
	wrong := ""
	for i := 0; wrong == ""; i++ {
		if cand := fmt.Sprintf("%06d", i); !valid[cand] {
			wrong = cand
		}
	}

	bogus := &TOTPIdentity{
		ID:           id.ID,
		Secret:       id.Secret,
		SessionToken: id.SessionToken,
	}
	aal, _, err := stepUpWithCode(ctx, publicURL, bogus, wrong)
	if aal == "aal2" {
		t.Fatalf("session reached aal2 despite an invalid TOTP code %q (err=%v); "+
			"the positive step-up test would pass without validating anything", wrong, err)
	}
}

func TestTOTPCode_MatchesRFC6238Vector(t *testing.T) {
	// RFC 6238 appendix B, SHA-1: seed "12345678901234567890" (ASCII), which is
	// base32 GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ. At T=59 the expected code is
	// 94287082. Truncated to the 6 digits Kratos uses: 287082.
	got, err := totpCode("GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ", time.Unix(59, 0))
	if err != nil {
		t.Fatalf("totpCode: %v", err)
	}
	if want := "287082"; got != want {
		t.Fatalf("totpCode(RFC 6238 vector @T=59) = %s, want %s", got, want)
	}
}
