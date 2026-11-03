package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/onboardstore"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/orgstore"
)

// The relationship values only carry information for a governmental
// organisation. These tests pin the gate's shape per organisation kind and the
// refusals that follow from it (ADR 1171, plan invariant 1).

func registerOrg(t *testing.T, s *Server, id, sector, composition, nation string) {
	t.Helper()
	if _, err := s.Orgs.CreateOrg(orgstore.Org{ID: id, DisplayName: id, Sector: sector, Composition: composition, Nation: nation, Provenance: "operator-verified", Status: "active"}); err != nil {
		t.Fatalf("register %s: %v", id, err)
	}
}

// seedGateProposal stages a proposal at the admin gate the way the console
// does: a sponsor-minted invite pinned to a registered org, the candidate's
// intake submission, sponsor-approved. advanceDraft assembles the manifest at
// approval, so the row clears the lint gate.
func seedGateProposal(t *testing.T, s *Server, orgID, sector string) string {
	t.Helper()
	id := storeDraft(t, s, onboardstore.Prebound{Tier: "developer", Org: orgID, Sector: sector},
		map[string]any{"name": "Test Enrollee", "email": "enrollee@" + orgID + ".test"})
	if err := s.Onboard.DecideProposal(context.Background(), id, onboardstore.ProposalSponsorApproved, "sp@example.test", ""); err != nil {
		t.Fatalf("sponsor-approve: %v", err)
	}
	return id
}

func webApprove(t *testing.T, s *Server, id, relationship string) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{}
	form.Set("csrf", s.csrfNonce)
	if relationship != "" {
		form.Set("relationship", relationship)
	}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/onboarding/"+id+"/approve", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, req)
	return rec
}

func relationshipOf(t *testing.T, s *Server, id string) string {
	t.Helper()
	p, err := s.Onboard.GetProposal(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(p.ManifestJSON), &m); err != nil {
		t.Fatal(err)
	}
	aff, _ := m["affiliation"].(map[string]any)
	rel, _ := aff["relationship"].(string)
	return rel
}

func TestRelationship_CommercialOrgIsNotAskedAndImpliesAssigned(t *testing.T) {
	s := newOnboardTestServer(t)
	withOrgs(t, s, true)
	registerOrg(t, s, "org142x", "commercial", "national", "ISL")
	id := seedGateProposal(t, s, "org142x", "commercial")
	p, _ := s.Onboard.GetProposal(context.Background(), id)

	ch := s.relationshipChoiceFor(*p)
	if ch.Ask || ch.Implied != "assigned" || !ch.Registered {
		t.Fatalf("commercial org: want implied assigned from the registry, got %+v", ch)
	}
	if rec := webApprove(t, s, id, ""); rec.Code != http.StatusSeeOther {
		t.Fatalf("approve with nothing declared: want 303, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := relationshipOf(t, s, id); got != "assigned" {
		t.Errorf("affiliation.relationship = %q, want assigned (implied by a commercial org)", got)
	}
}

func TestRelationship_CommercialOrgRefusesLiaisonAndConstituent(t *testing.T) {
	s := newOnboardTestServer(t)
	withOrgs(t, s, true)
	registerOrg(t, s, "org142x", "commercial", "national", "ISL")
	for _, rel := range []string{"liaison", "constituent"} {
		id := seedGateProposal(t, s, "org142x", "commercial")
		rec := webApprove(t, s, id, rel)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("approve %s at a commercial org: want 400, got %d: %s", rel, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "in or out") {
			t.Errorf("refusal must say why: %s", rec.Body.String())
		}
		p, _ := s.Onboard.GetProposal(context.Background(), id)
		if p.Status != onboardstore.ProposalSponsorApproved {
			t.Errorf("a refused relationship must leave the proposal at the gate, got %s", p.Status)
		}
	}
}

func TestRelationship_NationalStateOrgAsksWithoutConstituent(t *testing.T) {
	s := newOnboardTestServer(t)
	withOrgs(t, s, true)
	registerOrg(t, s, "usareur-af", "state", "national", "USA")
	id := seedGateProposal(t, s, "usareur-af", "state")
	p, _ := s.Onboard.GetProposal(context.Background(), id)

	ch := s.relationshipChoiceFor(*p)
	if !ch.Ask || ch.ConstituentOK {
		t.Fatalf("national state org: want asked, constituent withheld, got %+v", ch)
	}
	if rec := webApprove(t, s, id, "constituent"); rec.Code != http.StatusBadRequest {
		t.Errorf("constituent on a national org: want 400 (invariant 1), got %d: %s", rec.Code, rec.Body.String())
	}
	if rec := webApprove(t, s, id, "liaison"); rec.Code != http.StatusSeeOther {
		t.Errorf("liaison at a state org: want 303, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := relationshipOf(t, s, id); got != "liaison" {
		t.Errorf("affiliation.relationship = %q, want liaison", got)
	}
}

func TestRelationship_MultinationalStateOrgOffersConstituent(t *testing.T) {
	s := newOnboardTestServer(t)
	withOrgs(t, s, true)
	registerOrg(t, s, "org79x", "state", "multinational", "NATO")
	id := seedGateProposal(t, s, "org79x", "state")
	p, _ := s.Onboard.GetProposal(context.Background(), id)

	ch := s.relationshipChoiceFor(*p)
	if !ch.Ask || !ch.ConstituentOK {
		t.Fatalf("multinational state org: want asked with constituent, got %+v", ch)
	}
	if rec := webApprove(t, s, id, "constituent"); rec.Code != http.StatusSeeOther {
		t.Errorf("constituent at org79x: want 303, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRelationship_UnregisteredOrgFallsBackToTheManifestSector(t *testing.T) {
	s := newOnboardTestServer(t)
	withOrgs(t, s, true)
	// Not registered; the candidate declared sector=commercial at intake.
	manifest := map[string]any{
		"identity":    map[string]any{"handle": "vendor-person", "email": "v@example.test", "type": "person", "status": "active"},
		"affiliation": map[string]any{"id": "acme-sensors", "kind": "vendor", "nation": "DEU", "sector": "commercial"},
	}
	mj, _ := json.Marshal(manifest)
	p := &onboardstore.Proposal{Sponsor: "op", Env: "zagros", Handle: "vendor-person", Email: "v@example.test",
		Status: onboardstore.ProposalSponsorApproved, ManifestJSON: string(mj)}
	if err := s.Onboard.CreateProposal(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	ch := s.relationshipChoiceFor(*p)
	if ch.Ask || ch.Implied != "assigned" || ch.Registered {
		t.Fatalf("unregistered commercial org: want implied assigned from the manifest, got %+v", ch)
	}
}

func TestRelationship_UnknownSectorAsksEverything(t *testing.T) {
	s := newOnboardTestServer(t)
	// No org store at all and no sector in the manifest: the gate behaves as
	// it did before this scoping existed.
	id := seedOrgMatchProposal(t, s, "somewhere", "USA")
	p, _ := s.Onboard.GetProposal(context.Background(), id)
	ch := s.relationshipChoiceFor(*p)
	if !ch.Ask || !ch.ConstituentOK {
		t.Fatalf("unknown org: want everything asked, got %+v", ch)
	}
}

// A real draft with a commercial sector (console-minted invite with
// sector=commercial) must imply "assigned" without asking: the sponsor's
// prebound sector is the same fact a manifest would have carried.
func TestRelationship_DraftCommercialOrgIsNotAskedAndImpliesAssigned(t *testing.T) {
	s := newOnboardTestServer(t)
	id := storeDraft(t, s, onboardstore.Prebound{Tier: "developer", Sector: "commercial"},
		map[string]any{"name": "Draft Vendor", "email": "draft@example.test", "organization": "Acme Corp"})
	p, err := s.Onboard.GetProposal(context.Background(), id)
	if err != nil {
		t.Fatalf("GetProposal: %v", err)
	}
	if p.Complete {
		t.Fatalf("seeded proposal must be an incomplete draft, got complete note=%q", p.Note)
	}

	ch := s.relationshipChoiceFor(*p)
	if ch.Ask || ch.Implied != "assigned" || strings.ToLower(strings.TrimSpace(ch.Sector)) != "commercial" {
		t.Fatalf("draft commercial org: want implied assigned from draft sector, got %+v", ch)
	}
}

// The review page itself: a commercial org shows the implied value and no
// dropdown; a national state org shows the dropdown without constituent.
func TestRelationship_ReviewPageRendersTheChoice(t *testing.T) {
	s := newOnboardTestServer(t)
	withOrgs(t, s, true)
	registerOrg(t, s, "org142x", "commercial", "national", "ISL")
	registerOrg(t, s, "usareur-af", "state", "national", "USA")
	h := s.Routes()
	get := func(id string) string {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/onboarding?status=sponsor_approved&id="+id, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET review page: %d", rec.Code)
		}
		return rec.Body.String()
	}
	vendor := get(seedGateProposal(t, s, "org142x", "commercial"))
	if !strings.Contains(vendor, "Membership relationship: <strong>assigned</strong>") || strings.Contains(vendor, `<select name="relationship">`) {
		t.Errorf("commercial org page must state the implied value and render no dropdown")
	}
	state := get(seedGateProposal(t, s, "usareur-af", "state"))
	if !strings.Contains(state, `<select name="relationship">`) || strings.Contains(state, `value="constituent"`) {
		t.Errorf("national state org page must render the dropdown without constituent")
	}
}
