package handlers

// onboard_sponsor_actor_test.go: which name an invite records as its sponsor
// when an operator-tier key mints it with the X-Goat-Operator-Actor header
// (ADR 1112 §11 / F-207). A shared machine credential resolves to a service
// label, not a person; the header names the person and must replace the
// label. A real per-person handle must never be overridden (#1942).

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func mintWithActor(t *testing.T, h http.Handler, key, actor string) mintInviteResponse {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"env": "zagros", "tier": "developer"})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/onboard/invite", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	if actor != "" {
		req.Header.Set("X-Goat-Operator-Actor", actor)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("mint: want 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp mintInviteResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode mint: %v", err)
	}
	return resp
}

func TestMintInvite_ActorHeaderReplacesServiceSponsorLabels(t *testing.T) {
	for _, label := range []string{"operator", "onboard-api", "id-console"} {
		t.Run(label, func(t *testing.T) {
			s := newOnboardTestServer(t)
			s.Cfg.OnboardOperatorAPIKeys = map[string]string{"service-key": label}
			resp := mintWithActor(t, s.Routes(), "service-key", "person-handle")
			if resp.Sponsor != "person-handle" || resp.SponsorName != "person-handle" {
				t.Fatalf("key resolving to %q with an actor header: sponsor (%q, %q), want the person",
					label, resp.Sponsor, resp.SponsorName)
			}
			inv, err := s.Onboard.GetInvite(context.Background(), resp.ID)
			if err != nil {
				t.Fatal(err)
			}
			if inv.Sponsor != "person-handle" {
				t.Fatalf("invite row sponsor = %q, want person-handle", inv.Sponsor)
			}
		})
	}
}

func TestMintInvite_ActorHeaderNeverOverridesAPersonalHandle(t *testing.T) {
	s := newOnboardTestServer(t)
	s.Cfg.OnboardOperatorAPIKeys = map[string]string{"personal-key": "real-person"}
	resp := mintWithActor(t, s.Routes(), "personal-key", "someone-else")
	if resp.Sponsor != "real-person" {
		t.Fatalf("a per-person handle was overridden by the header: sponsor %q", resp.Sponsor)
	}
}

// The candidate preview suppresses service labels so a candidate sees a
// person or nothing. id-console is a service label like the other two.
func TestPreviewSuppressesIDConsoleSponsorLabel(t *testing.T) {
	s := newOnboardTestServer(t)
	s.Cfg.OnboardOperatorAPIKeys = map[string]string{"service-key": "id-console"}
	h := s.Routes()
	resp := mintWithActor(t, h, "service-key", "")
	pv := preview(t, h, resp.Token)
	if pv.Sponsor != "" {
		t.Fatalf("preview shows service label %q as the sponsor; want it suppressed", pv.Sponsor)
	}
}
