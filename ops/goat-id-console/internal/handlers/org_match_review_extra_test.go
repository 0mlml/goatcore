package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/onboardstore"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/orgstore"
)

func TestOrgAttach_CompletionUnexpectedErrorProceeds(t *testing.T) {
	s := newOnboardTestServer(t)
	orgs := withOrgs(t, s, true)
	if _, err := orgs.CreateOrg(orgstore.Org{ID: "demoforge", DisplayName: "Demoforge", Kind: "partner", Nation: "USA"}); err != nil {
		t.Fatal(err)
	}
	pb := onboardstore.Prebound{Tier: "developer"}
	sub := map[string]any{
		"name":         "Pat Draft",
		"email":        "pat@example.test",
		"organization": "Demoforge Typed",
	}
	id := storeDraftWithAffiliation(t, s, pb, sub, "demoforge-typed", "USA")

	s.testCompleteDraftOverride = func(ctx context.Context, p *onboardstore.Proposal, by draftCompleter) (draftOutcome, string, error) {
		return draftBlocked, "", errors.New("boom")
	}
	defer func() { s.testCompleteDraftOverride = nil }()

	rr := webForm(t, s, "/onboarding/"+id+"/org-attach", url.Values{"org": {"demoforge"}})
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("org-attach with completion error: want 303, got %d body %s", rr.Code, rr.Body.String())
	}
}

func TestOrgNew_CompletionUnexpectedErrorProceeds(t *testing.T) {
	s := newOnboardTestServer(t)
	orgs := withOrgs(t, s, true)
	pb := onboardstore.Prebound{Tier: "developer"}
	sub := map[string]any{
		"name":         "Pat Draft",
		"email":        "pat@example.test",
		"organization": "New Org",
	}
	id := storeDraftWithAffiliation(t, s, pb, sub, "new-org-typed", "USA")

	s.testCompleteDraftOverride = func(ctx context.Context, p *onboardstore.Proposal, by draftCompleter) (draftOutcome, string, error) {
		return draftBlocked, "", errors.New("boom")
	}
	defer func() { s.testCompleteDraftOverride = nil }()

	rr := webForm(t, s, "/onboarding/"+id+"/org-new", url.Values{
		"display_name": {"New Org"},
		"nation":       {"USA"},
	})
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("org-new with completion error: want 303, got %d body %s", rr.Code, rr.Body.String())
	}
	if _, err := orgs.GetOrg("new-org-typed"); err != nil {
		t.Fatalf("org should have been created despite completion error: %v", err)
	}
}

func TestOrgAttach_NoteUpdateFailureProceeds(t *testing.T) {
	s := newOnboardTestServer(t)
	orgs := withOrgs(t, s, true)
	if _, err := orgs.CreateOrg(orgstore.Org{ID: "demoforge", DisplayName: "Demoforge", Kind: "partner", Nation: "USA"}); err != nil {
		t.Fatal(err)
	}
	pb := onboardstore.Prebound{Tier: "developer"}
	sub := map[string]any{
		"name":         "Pat Draft",
		"email":        "pat@example.test",
		"organization": "Demoforge",
	}
	id := storeDraftWithAffiliation(t, s, pb, sub, "demoforge-typed", "USA")

	s.testCompleteDraftOverride = func(ctx context.Context, p *onboardstore.Proposal, by draftCompleter) (draftOutcome, string, error) {
		// Sabotage the upcoming UpdateDraftNote call by secretly altering the DB row.
		_, _ = s.Onboard.DB().Exec(`UPDATE onboard_proposals SET status = 'rejected' WHERE id = $1`, p.ID)
		return draftBlocked, "blocked for reasons", nil
	}
	defer func() { s.testCompleteDraftOverride = nil }()

	rr := webForm(t, s, "/onboarding/"+id+"/org-attach", url.Values{"org": {"demoforge"}})
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("org-attach with note update failure: want 303, got %d body %s", rr.Code, rr.Body.String())
	}
}

func TestOrgNew_NoteUpdateFailureProceeds(t *testing.T) {
	s := newOnboardTestServer(t)
	orgs := withOrgs(t, s, true)
	pb := onboardstore.Prebound{Tier: "developer"}
	sub := map[string]any{
		"name":         "Pat Draft",
		"email":        "pat@example.test",
		"organization": "New Org",
	}
	id := storeDraftWithAffiliation(t, s, pb, sub, "new-org-typed", "USA")

	s.testCompleteDraftOverride = func(ctx context.Context, p *onboardstore.Proposal, by draftCompleter) (draftOutcome, string, error) {
		_, _ = s.Onboard.DB().Exec(`UPDATE onboard_proposals SET status = 'rejected' WHERE id = $1`, p.ID)
		return draftBlocked, "blocked for reasons", nil
	}
	defer func() { s.testCompleteDraftOverride = nil }()

	rr := webForm(t, s, "/onboarding/"+id+"/org-new", url.Values{
		"display_name": {"New Org"},
		"nation":       {"USA"},
	})
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("org-new with note update failure: want 303, got %d body %s", rr.Code, rr.Body.String())
	}
	if _, err := orgs.GetOrg("new-org-typed"); err != nil {
		t.Fatalf("org should have been created despite note update failure: %v", err)
	}
}
