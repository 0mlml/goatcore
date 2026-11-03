package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/onboardstore"
)

// The follow-ups from the review of the draft-completion path: one request id
// per request, wrong-typed fields refused at submit, and a completion update
// that matched zero rows because another admin got there first.

func countDraftCompletionAudits(s *Server, id string) int {
	n := 0
	for _, e := range s.Audit.Recent(50) {
		if e.Action == "onboard_draft_complete" && e.Target.InviteTokenPrefix == id {
			n++
		}
	}
	return n
}

// requestID mints once per request: the second call returns what the first
// minted, and a caller-supplied header is used as given.
func TestRequestID_MintedOncePerRequest(t *testing.T) {
	r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/", nil)
	first := requestID(r)
	if first == "" {
		t.Fatal("no request id minted")
	}
	if again := requestID(r); again != first {
		t.Fatalf("second call minted a new id: %q then %q", first, again)
	}
	given := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/", nil)
	given.Header.Set("X-Request-ID", "caller-set")
	if got := requestID(given); got != "caller-set" {
		t.Fatalf("caller-set id replaced: got %q", got)
	}
}

// One click that completes a draft and decides it writes two audit rows, and
// they share the request id so the completion can be joined to the decision
// that caused it. The web gate is used because it is the path that writes a
// decision row.
func TestWebApprove_DraftCompletionSharesTheDecisionRequestID(t *testing.T) {
	s := newOnboardTestServer(t)
	h := s.Routes()
	id := storeDraft(t, s, onboardstore.Prebound{Tier: "developer"},
		map[string]any{"name": "Same Request", "email": "same-request@example.test", "organization": "Draft Org", "sector": "commercial"})
	if err := s.Onboard.DecideProposal(context.Background(), id, onboardstore.ProposalSponsorApproved, "sp@example.test", ""); err != nil {
		t.Fatalf("stage the draft at the admin gate: %v", err)
	}

	form := url.Values{}
	form.Set("csrf", s.csrfNonce)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/onboarding/"+id+"/approve", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("web approve: want 303, got %d: %s", rec.Code, rec.Body.String())
	}

	var completion, decision string
	for _, e := range s.Audit.Recent(50) {
		if e.Target.InviteTokenPrefix != id {
			continue
		}
		switch e.Action {
		case "onboard_draft_complete":
			completion = e.RequestID
		case "onboard_approve":
			decision = e.RequestID
		}
	}
	if completion == "" || decision == "" {
		t.Fatalf("expected both audit rows for %s: completion=%q decision=%q", id, completion, decision)
	}
	if completion != decision {
		t.Fatalf("completion and decision carry different request ids: %q vs %q", completion, decision)
	}
}

// A field of the wrong JSON type is refused at submit, naming the field, and
// the invite is not consumed: a corrected submission on the same link lands.
func TestSubmit_WrongTypedFieldRefusedNamingTheField(t *testing.T) {
	s := newOnboardTestServer(t)
	h := s.Routes()
	tok := mintPrebound(t, s, onboardstore.Prebound{Tier: "developer"})

	body, _ := json.Marshal(map[string]any{"token": tok, "submission": map[string]any{
		"name": "Quota String", "email": "quota-string@example.test", "nationality": "CAN",
		"organization": "Quota Org", "sector": "commercial", "machine_quota": "5"}})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/public/onboard/submit", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("wrong-typed field: want 400, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Error onboardAPIError `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode refusal: %v: %s", err, rec.Body.String())
	}
	if resp.Error.Code != "bad_field_type" {
		t.Fatalf("want error code bad_field_type, got %q: %s", resp.Error.Code, rec.Body.String())
	}
	if !strings.Contains(resp.Error.Message, "machine_quota") {
		t.Fatalf("refusal must name the field, got %q", resp.Error.Message)
	}

	pid := submit(t, h, tok, map[string]any{"name": "Quota String", "email": "quota-string@example.test",
		"organization": "Quota Org", "sector": "commercial", "machine_quota": 5})
	if pid == "" {
		t.Fatal("corrected submission on the same link must land")
	}
}

// Two admins advance the same draft. The loser's completion update matches
// zero rows because the winner already completed it; the loser re-reads,
// adopts the winner's row, and writes no second completion audit row.
func TestCompleteDraft_LosingRaceAdoptsTheWinnersRow(t *testing.T) {
	s := newOnboardTestServer(t)
	id := storeDraft(t, s, onboardstore.Prebound{Tier: "developer"},
		map[string]any{"name": "Raced Draft", "email": "raced@example.test", "organization": "Draft Org", "sector": "commercial"})
	winner, _ := loadProposal(t, s, id)
	loser, _ := loadProposal(t, s, id) // read before the winner clicked
	ctx := context.Background()

	if out, why, err := s.completeDraft(ctx, winner, sweepCompleter); err != nil || out != draftCompleted {
		t.Fatalf("winner: outcome %v why=%q err=%v", out, why, err)
	}
	out, why, err := s.completeDraft(ctx, loser, sweepCompleter)
	if err != nil || out != draftCompleted {
		t.Fatalf("loser: want completed, got outcome %v why=%q err=%v", out, why, err)
	}
	if !loser.Complete || loser.ManifestJSON != winner.ManifestJSON || loser.Handle != winner.Handle {
		t.Fatalf("loser must adopt the winner's row: complete=%v handle=%q", loser.Complete, loser.Handle)
	}
	if n := countDraftCompletionAudits(s, id); n != 1 {
		t.Fatalf("want exactly one completion audit row, got %d", n)
	}
}

// The row left the review stages while the request was in flight (another
// admin rejected it). Completion reports the change and the decision answers
// 409, not the 500 a bare zero-row update produced.
func TestCompleteDraft_RowDecidedUnderneathAnswersConflict(t *testing.T) {
	s := newOnboardTestServer(t)
	id := storeDraft(t, s, onboardstore.Prebound{Tier: "developer"},
		map[string]any{"name": "Pulled Draft", "email": "pulled@example.test", "organization": "Draft Org", "sector": "commercial"})
	stale, _ := loadProposal(t, s, id)
	ctx := context.Background()
	if err := s.Onboard.DecideProposal(ctx, id, onboardstore.ProposalRejected, "other-admin", "withdrawn"); err != nil {
		t.Fatalf("reject: %v", err)
	}

	if _, _, err := s.completeDraft(ctx, stale, sweepCompleter); !errors.Is(err, errDraftChanged) {
		t.Fatalf("want errDraftChanged, got %v", err)
	}
	if countDraftCompletionAudits(s, id) != 0 {
		t.Fatal("a completion that did not persist must not write an audit row")
	}
	derr := s.advanceDraft(httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/", nil), stale)
	if derr == nil || derr.status != http.StatusConflict {
		t.Fatalf("advance on a row decided underneath: want 409, got %+v", derr)
	}
}
