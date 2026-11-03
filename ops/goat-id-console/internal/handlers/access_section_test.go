package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/onboardstore"
)

// holdRecordsFixturePath is the lifecycle hold-records fixture used by the
// Access section tests.
const holdRecordsFixturePath = "../../../lifecycle/fixtures/hold-records.jsonl"

func newAccessTestServer(t *testing.T, email, name string, groups []string) *Server {
	t.Helper()
	kc, _ := fakeKratos(t, "id-1", email, name, groups, "active")
	s := newUsersAPITestServer(t, kc)
	s.Cfg.NotifyGoatnet = "zagros"
	return s
}

func getUserDetailBody(t *testing.T, ctx context.Context, s *Server) (int, string) {
	t.Helper()
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/users/id-1?idp=tenant", nil)
	req.Header.Set("X-Forwarded-Groups", "ADMINS")
	req.Header.Set("X-Forwarded-Email", "admin@example.test")
	req.Header.Set("X-Forwarded-User", "admin-id")
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func TestUserDetailAccess_FixtureMemberOnHold(t *testing.T) {
	ctx := context.Background()
	// Server with one admin identity whose email matches the proposal below.
	s := newAccessTestServer(t, "jane@example.test", "Jane Doe", []string{"ADMINS"})

	// Seed an onboarding proposal for the identity so the handle can be
	// derived via ActiveProposalForEmail.
	p := &onboardstore.Proposal{
		Sponsor:      "sponsor-1",
		Env:          "zagros",
		Handle:       "jane-doe",
		Email:        "jane@example.test",
		Status:       onboardstore.ProposalOnboarded,
		ManifestJSON: "{}",
		Tier:         "developer",
	}
	if err := s.Onboard.CreateProposal(ctx, p); err != nil {
		t.Fatalf("CreateProposal: %v", err)
	}
	// Persist a stored setup key to ensure the plaintext value never reaches
	// the HTML.
	const plaintextSetupKey = "PLAINTEXT-SETUP-KEY"
	if err := s.Onboard.SetProposalResult(ctx, p.ID, onboardstore.ProposalOnboarded, plaintextSetupKey, "", "", "", "", false); err != nil {
		t.Fatalf("SetProposalResult: %v", err)
	}

	// Access reads the hold records from the fixture file.
	s.Cfg.HoldRecordsPath = filepath.Clean(holdRecordsFixturePath)

	// Fake NetBird management API serving one held peer for jane-doe and one
	// setup key belonging to that handle.
	nb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/peers":
			// Peer for the first fixture record: jane-doe in her hold group.
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{
					"id":        "d0f1e2a3b4c5d6e7f8091a2b",
					"name":      "peer-jane-doe-acme-zagros-20260601-laptop",
					"connected": false,
					"last_seen": "2026-09-10T10:00:00Z",
					"groups": []map[string]any{
						{"id": "g-all", "name": "All"},
						{"id": "g-hold", "name": "ZAGROS-PROBATION-DEVELOPER"},
					},
				},
			})
		case "/api/setup-keys":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{
					"id":         "k-1",
					"name":       "peer-jane-doe-zagros-2026-09-01",
					"used_times": 1,
					"revoked":    false,
					"valid":      true,
					"expires":    "2026-10-01T00:00:00Z",
					"state":      "active",
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(nb.Close)

	s.Cfg.NotifyMgmtURL = nb.URL
	s.Cfg.EnrollWatchToken = "NETBIRD-TOKEN-SECRET"

	// Drive GET /users/{id}.
	code, body := getUserDetailBody(t, ctx, s)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", code, body)
	}

	// Kind + State come from the shared hold_badges define (goat#1042).
	if !strings.Contains(body, "Access") || !strings.Contains(body, `hold-kind warn">dormancy</span>`) || !strings.Contains(body, `hold-state ">held</span>`) {
		t.Fatalf("Access section does not show the dormancy/held badges; body: %s", body)
	}
	if !strings.Contains(body, "ZAGROS-PROBATION-DEVELOPER") {
		t.Fatalf("Access section does not name hold group; body: %s", body)
	}
	if !strings.Contains(body, "Setup keys") {
		t.Fatalf("Access section does not render setup key block; body: %s", body)
	}

	// Security guarantee: neither the NetBird token nor the proposal's
	// plaintext SetupKey value appears in the HTML.
	if strings.Contains(body, s.Cfg.EnrollWatchToken) {
		t.Fatalf("response leaked ENROLL_WATCH_NB_TOKEN")
	}
	if strings.Contains(body, plaintextSetupKey) {
		t.Fatalf("response leaked plaintext setup key from proposal")
	}
}

func TestUserDetailAccess_NoRecordPeerNotOnHold(t *testing.T) {
	ctx := context.Background()
	s := newAccessTestServer(t, "alice@example.test", "Alice", []string{"ADMINS"})

	// Proposal for a member with no hold-record row.
	p := &onboardstore.Proposal{
		Sponsor:      "sponsor-1",
		Env:          "zagros",
		Handle:       "alice-nohold",
		Email:        "alice@example.test",
		Status:       onboardstore.ProposalOnboarded,
		ManifestJSON: "{}",
		Tier:         "developer",
	}
	if err := s.Onboard.CreateProposal(ctx, p); err != nil {
		t.Fatalf("CreateProposal: %v", err)
	}

	s.Cfg.HoldRecordsPath = filepath.Clean(holdRecordsFixturePath)

	nb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/peers":
			// Peer in a canonical role group, no hold group.
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{
					"id":        "p-nohold",
					"name":      "peer-alice-nohold-zagros-20260901-laptop",
					"connected": true,
					"last_seen": "2026-09-10T10:00:00Z",
					"groups":    []map[string]any{{"id": "g-dev", "name": "ZAGROS-DEVELOPER"}},
				},
			})
		case "/api/setup-keys":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{
					"id":         "k-1",
					"name":       "peer-alice-nohold-zagros-2026-09-01",
					"used_times": 0,
					"revoked":    false,
					"valid":      true,
					"expires":    "2056-10-01T00:00:00Z",
					"state":      "active",
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(nb.Close)

	s.Cfg.NotifyMgmtURL = nb.URL
	s.Cfg.EnrollWatchToken = "NETBIRD-TOKEN-SECRET"

	code, body := getUserDetailBody(t, ctx, s)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", code, body)
	}

	if !strings.Contains(body, "not on hold") {
		t.Fatalf("Access section does not show not-on-hold state; body: %s", body)
	}
	if !strings.Contains(body, "ZAGROS-DEVELOPER") {
		t.Fatalf("Access section does not show live groups; body: %s", body)
	}
	if !strings.Contains(body, "status-badge active\">valid") {
		t.Fatalf("Access section does not show setup-key state; body: %s", body)
	}
}

func TestUserDetailAccess_TokenUnsetMeshesUnavailable(t *testing.T) {
	ctx := context.Background()
	s := newAccessTestServer(t, "alice@example.test", "Alice", []string{"ADMINS"})

	p := &onboardstore.Proposal{
		Sponsor:      "sponsor-1",
		Env:          "zagros",
		Handle:       "alice-no-netbird",
		Email:        "alice@example.test",
		Status:       onboardstore.ProposalOnboarded,
		ManifestJSON: "{}",
	}
	if err := s.Onboard.CreateProposal(ctx, p); err != nil {
		t.Fatalf("CreateProposal: %v", err)
	}

	s.Cfg.HoldRecordsPath = filepath.Clean(holdRecordsFixturePath)
	s.Cfg.NotifyMgmtURL = "https://netbird.example.test"
	s.Cfg.EnrollWatchToken = "" // default-off

	code, body := getUserDetailBody(t, ctx, s)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", code, body)
	}

	if !strings.Contains(body, "mesh state unavailable") {
		t.Fatalf("Access section does not show mesh state unavailable; body: %s", body)
	}
	if !strings.Contains(body, "ENROLL_WATCH_NB_TOKEN is unset") {
		t.Fatalf("Access section does not name token-unset cause; body: %s", body)
	}
	if !strings.Contains(body, "Handle <code>alice-no-netbird</code>") {
		t.Fatalf("Access section does not render derived handle; body: %s", body)
	}
	if strings.Contains(body, "hold records unavailable") {
		t.Fatalf("Access section incorrectly degraded hold records; body: %s", body)
	}
}

func TestUserDetailAccess_InconsistentStateRendersBanner(t *testing.T) {
	ctx := context.Background()
	s := newAccessTestServer(t, "alice@example.test", "Alice", []string{"ADMINS"})

	p := &onboardstore.Proposal{
		Sponsor:      "sponsor-1",
		Env:          "zagros",
		Handle:       "alice-hold",
		Email:        "alice@example.test",
		Status:       onboardstore.ProposalOnboarded,
		ManifestJSON: "{}",
		Tier:         "developer",
	}
	if err := s.Onboard.CreateProposal(ctx, p); err != nil {
		t.Fatalf("CreateProposal: %v", err)
	}

	// Synthetic hold-record file with one record whose live peer still
	// carries both the hold group and a prior group — the inconsistent
	// row of the truth table.
	tmp := filepath.Join(t.TempDir(), "hold-records.jsonl")
	line := `{"v":1,"lane":"dormancy","site":"zagros","peer_id":"p-hold","peer_name":"peer-alice-hold-zagros","handle":"alice-hold","entered_at":"2026-09-01T10:00:00Z","hold_group":"ZAGROS-PROBATION-DEVELOPER","prior_groups":["ZAGROS-DEVELOPER"],"actor":"sweep"}`
	if err := os.WriteFile(tmp, []byte(line+"\n"), 0o644); err != nil {
		t.Fatalf("write temp hold-records: %v", err)
	}
	s.Cfg.HoldRecordsPath = tmp

	nb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/peers":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{
					"id":        "p-hold",
					"name":      "peer-alice-hold-zagros",
					"connected": false,
					"last_seen": "2026-09-10T10:00:00Z",
					"groups": []map[string]any{
						{"id": "g-all", "name": "All"},
						{"id": "g-hold", "name": "ZAGROS-PROBATION-DEVELOPER"},
						{"id": "g-dev", "name": "ZAGROS-DEVELOPER"},
					},
				},
			})
		case "/api/setup-keys":
			_ = json.NewEncoder(w).Encode([]map[string]any{})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(nb.Close)

	s.Cfg.NotifyMgmtURL = nb.URL
	s.Cfg.EnrollWatchToken = "NETBIRD-TOKEN-SECRET"

	code, body := getUserDetailBody(t, ctx, s)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", code, body)
	}

	if !strings.Contains(body, "inconsistent, investigate") {
		t.Fatalf("Access section does not render inconsistent banner; body: %s", body)
	}
	if !strings.Contains(body, "No setup key found") {
		t.Fatalf("Access section does not render no-key message; body: %s", body)
	}
}
