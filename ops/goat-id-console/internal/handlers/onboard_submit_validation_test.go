package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/onboardstore"
)

// TestSubmit_RealCallerShape verifies validation using the EXACT full payload structure sent
// by the browser JS (all keys present, some empty, reflecting a real post).
// It also tests AC 1b: A submission with a blank organization, a blank open tier, and a lowercase nationality
// returns ONE 422 whose error.fields names all three.
func TestSubmit_RealCallerShape(t *testing.T) {
	s := newOnboardTestServer(t)
	h := s.Routes()

	inv := &onboardstore.Invite{
		Sponsor:      "sponsor@bypass.local",
		Env:          "zagros",
		PreboundJSON: "{\"tier\":\"\"}",
	}
	tok, err := s.Onboard.MintInvite(context.Background(), inv)
	if err != nil {
		t.Fatal(err)
	}

	// This is the REAL shape from onboard.html's submit event.
	// Empty strings for fields the candidate left blank.
	fullPayload := map[string]any{
		"name":              "Real Caller",
		"email":             "person-109@example.org",
		"principal_type":    "person",
		"organization":      "",    // Blank triggers validation_failed
		"nationality":       "can", // Lowercase nationality triggers bad validation
		"unit_code":         "",
		"unit_code_scheme":  "uic",
		"title":             "",
		"github_username":   "", // Blank triggers fallback to email, or handle_required if email lacks @
		"alliance_standing": "organic",
		"sector":            "defense",
		"notes":             "",
		"spire_federated":   false,
		"tier":              "", // Blank open tier triggers validation_failed
		"gov_branch":        "",
		// machine_quota is absent because readNumber("mqRow") returns null
	}

	body, _ := json.Marshal(map[string]any{"token": tok, "submission": fullPayload})
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/public/onboard/submit", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("real shape validation: want 422, got %d. body: %s", w.Code, w.Body.String())
	}

	var res map[string]onboardAPIError
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}

	errObj := res["error"]
	if errObj.Code != "validation_failed" {
		t.Fatalf("want error code validation_failed, got %q", errObj.Code)
	}

	fields := errObj.Fields
	if _, ok := fields["organization"]; !ok {
		t.Errorf("missing organization in error.fields")
	}
	if _, ok := fields["tier"]; !ok {
		t.Errorf("missing tier in error.fields")
	}
	if _, ok := fields["nationality"]; !ok {
		t.Errorf("missing nationality in error.fields")
	}
	if len(fields) != 3 {
		t.Errorf("expected exactly 3 fields in error.fields, got %d: %v", len(fields), fields)
	}

	// Verify invite is unconsumed by querying its status.
	// We can check if we can submit it successfully once fields are fixed (AC 1).
	fullPayload["organization"] = "Valid Organization"
	fullPayload["tier"] = "browse"
	fullPayload["nationality"] = "CAN"

	body2, _ := json.Marshal(map[string]any{"token": tok, "submission": fullPayload})
	req2 := httptest.NewRequestWithContext(context.Background(), "POST", "/public/onboard/submit", bytes.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, req2)

	if w2.Code != http.StatusCreated {
		t.Fatalf("subsequent valid submit should succeed, invite should be unconsumed. got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestSubmitRejectsMalformedPrebindingWithoutConsumingInvite(t *testing.T) {
	s := newOnboardTestServer(t)
	h := s.Routes()
	inv := &onboardstore.Invite{
		Sponsor:      "limited-sponsor@example.com",
		Env:          "zagros",
		PreboundJSON: `{not-json`,
	}
	tok, err := s.Onboard.MintInvite(context.Background(), inv)
	if err != nil {
		t.Fatal(err)
	}

	submission := map[string]any{
		"name": "Privilege Probe", "email": "probe@example.com",
		"principal_type": "system", "organization": "Example Org",
		"tier": "platform-admin",
	}
	body, err := json.Marshal(map[string]any{"token": tok, "submission": submission})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/public/onboard/submit", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("malformed prebinding submit = %d, want 500; body: %s", w.Code, w.Body.String())
	}
	stored, err := s.Onboard.GetInviteByToken(context.Background(), tok)
	if err != nil {
		t.Fatalf("reload invite: %v", err)
	}
	if stored.Status != onboardstore.InviteInvited {
		t.Fatalf("malformed invite status = %q, want %q", stored.Status, onboardstore.InviteInvited)
	}
	proposals, err := s.Onboard.ListProposals(context.Background(), "")
	if err != nil {
		t.Fatalf("list proposals: %v", err)
	}
	if len(proposals) != 0 {
		t.Fatalf("malformed prebinding created %d proposals", len(proposals))
	}
}

func TestSubmit_NationCodeValidator(t *testing.T) {
	// Tests AC 7: Intake and review editor call the same validator.
	// A full country name upper-cased (e.g. UKRAINE) should be refused by validNationCode.
	if validNationCode("UKRAINE") {
		t.Errorf("UKRAINE should be refused by the shared validator")
	}
	if !validNationCode("UKR") {
		t.Errorf("UKR should be accepted")
	}
	if validNationCode("ukr") {
		t.Errorf("ukr should be refused (must be uppercase)")
	}
	if validNationCode("LONGNAME") {
		t.Errorf("LONGNAME should be refused")
	}
	if validNationCode("") {
		t.Errorf("empty should be refused")
	}
}

func TestSubmit_ValidationEdgeCases(t *testing.T) {
	s := newOnboardTestServer(t)
	h := s.Routes()

	tests := []struct {
		name       string
		submission map[string]any
		wantStatus int
		wantFields []string
	}{
		{
			name: "missing name and email",
			submission: map[string]any{
				"name":            "   ",
				"email":           "",
				"principal_type":  "person",
				"organization":    "Test Org",
				"nationality":     "CAN",
				"tier":            "browse",
				"github_username": "valid",
			},
			wantStatus: http.StatusUnprocessableEntity,
			wantFields: []string{"name", "email"},
		},
		{
			name: "organization with no letter or digit cannot form an id",
			submission: map[string]any{
				"name":            "Test",
				"email":           "person-118@example.org",
				"principal_type":  "person",
				"organization":    "!!! ---",
				"nationality":     "CAN",
				"tier":            "browse",
				"github_username": "sym",
			},
			wantStatus: http.StatusUnprocessableEntity,
			wantFields: []string{"organization"},
		},
		{
			name: "cannot derive handle from bad email",
			submission: map[string]any{
				"name":            "Test",
				"email":           "no-at-sign",
				"principal_type":  "person",
				"organization":    "Test Org",
				"nationality":     "CAN",
				"tier":            "browse",
				"github_username": "",
			},
			wantStatus: http.StatusUnprocessableEntity,
			wantFields: []string{"handle"},
		},
		{
			name: "system without nationality is accepted",
			submission: map[string]any{
				"name":            "System",
				"email":           "person-119@example.org",
				"principal_type":  "system",
				"organization":    "Test Org",
				"nationality":     "",
				"tier":            "browse",
				"github_username": "sys_handle",
			},
			wantStatus: http.StatusCreated,
			wantFields: nil,
		},
		{
			name: "person without nationality is refused",
			submission: map[string]any{
				"name":            "Person",
				"email":           "person-107@example.org",
				"principal_type":  "person",
				"organization":    "Test Org",
				"nationality":     "",
				"tier":            "browse",
				"github_username": "per_handle",
			},
			wantStatus: http.StatusUnprocessableEntity,
			wantFields: []string{"nationality"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inv := &onboardstore.Invite{
				Sponsor:      "sponsor@bypass.local",
				Env:          "zagros",
				PreboundJSON: `{"tier":""}`,
			}
			tok, err := s.Onboard.MintInvite(context.Background(), inv)
			if err != nil {
				t.Fatal(err)
			}

			body, _ := json.Marshal(map[string]any{"token": tok, "submission": tt.submission})
			req := httptest.NewRequestWithContext(context.Background(), "POST", "/public/onboard/submit", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Fatalf("want status %d, got %d. body: %s", tt.wantStatus, w.Code, w.Body.String())
			}

			if tt.wantStatus == http.StatusUnprocessableEntity {
				var res map[string]onboardAPIError
				if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
					t.Fatal(err)
				}
				fields := res["error"].Fields
				if len(fields) != len(tt.wantFields) {
					t.Errorf("want %d fields %v, got %d: %v", len(tt.wantFields), tt.wantFields, len(fields), fields)
				}
				for _, f := range tt.wantFields {
					if _, ok := fields[f]; !ok {
						t.Errorf("expected field %q in error response, not found", f)
					}
				}
			}
		})
	}
}
