package handlers

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/onboardstore"
)

// ADR 1171. The membership relationship is the one fact only the reviewer knows:
// how this person's own nationality relates to the organisation. It is not
// derivable — a US officer and a Canadian officer at the same US command are
// both `assigned`, and what separates them is identity.nationality.

func TestRecordRelationshipWritesIntoTheManifest(t *testing.T) {
	s := newOnboardTestServer(t)
	id := seedOrgMatchProposal(t, s, "usareur-af", "USA")
	r := httptest.NewRequestWithContext(context.Background(), "POST", "/onboarding/"+id+"/approve", nil)

	if err := s.recordRelationship(r, id, "assigned"); err != nil {
		t.Fatalf("recordRelationship: %v", err)
	}
	p, err := s.Onboard.GetProposal(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(p.ManifestJSON), &m); err != nil {
		t.Fatal(err)
	}
	aff, _ := m["affiliation"].(map[string]any)
	if aff == nil || aff["relationship"] != "assigned" {
		t.Errorf("affiliation.relationship = %v, want assigned", aff["relationship"])
	}
	// It must sit BESIDE affiliation.id, not replace anything: affiliation.id is
	// the membership assertion and this is its qualifier.
	if aff["id"] == nil || aff["id"] == "" {
		t.Error("affiliation.id was lost — the relationship qualifies the membership, it does not replace it")
	}
}

func TestRecordRelationshipRefusesAnythingOutsideTheClosedSet(t *testing.T) {
	s := newOnboardTestServer(t)
	id := seedOrgMatchProposal(t, s, "usareur-af", "USA")
	r := httptest.NewRequestWithContext(context.Background(), "POST", "/onboarding/"+id+"/approve", nil)

	for _, bad := range []string{
		"attached",  // plausible, and not the vocabulary
		"member",    // plausible, and not the vocabulary
		"ASSIGNED ", // handled by folding, so this one must SUCCEED — see below
	} {
		err := s.recordRelationship(r, id, bad)
		if strings.TrimSpace(strings.ToLower(bad)) == "assigned" {
			if err != nil {
				t.Errorf("recordRelationship(%q) should fold to a valid value, got %v", bad, err)
			}
			continue
		}
		if err == nil {
			t.Errorf("recordRelationship(%q) was accepted; the vocabulary is closed", bad)
		}
	}
}

func TestApproveRefusesWhenTheRelationshipIsUnrecordable(t *testing.T) {
	// A blank relationship is allowed — it is optional at this stage — but a
	// PRESENT and invalid one must stop the approval rather than mint an
	// identity whose relationship nobody stated. The field exists to end an
	// inference; silently dropping it re-opens the inference.
	s := newOnboardTestServer(t)
	id := seedOrgMatchProposal(t, s, "usareur-af", "USA")
	r := httptest.NewRequestWithContext(context.Background(), "POST", "/onboarding/"+id+"/approve", nil)
	if err := s.recordRelationship(r, id, "sidekick"); err == nil {
		t.Fatal("an invalid relationship must refuse, so the approval refuses with it")
	}
	p, _ := s.Onboard.GetProposal(context.Background(), id)
	var m map[string]any
	_ = json.Unmarshal([]byte(p.ManifestJSON), &m)
	if aff, _ := m["affiliation"].(map[string]any); aff != nil && aff["relationship"] != nil {
		t.Errorf("a refused value must not be written: got %v", aff["relationship"])
	}
	_ = onboardstore.ProposalApproved // keep the import honest about what this gate guards
}
