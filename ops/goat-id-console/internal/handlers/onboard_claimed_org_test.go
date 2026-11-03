package handlers

// #1086: a candidate-typed organisation that happens to resolve in the registry
// was accepted as membership with no decision behind it. affiliation.id is the
// roster grouping key, the org-scope revoke cascade key and what data-sharing
// decisions are made about, so these tests pin BOTH halves: a typed org raises
// the review panel and holds the approval, and a sponsor-bound one still asks
// nothing.

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/onboardstore"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/orgstore"
)

const (
	claimDisabled   = `disabled aria-disabled="true"`
	claimApproveWhy = "Approve is unavailable until the organization above is attached."
)

// registerClaimTestOrg puts one commercial org in the registry. Commercial
// keeps ADR 1171's relationship question out of the way (a commercial org
// implies `assigned`), so an approval that is refused here is refused over the
// organisation and nothing else.
func registerClaimTestOrg(t *testing.T, orgs *orgstore.Store, id, display string) {
	t.Helper()
	if _, err := orgs.CreateOrg(orgstore.Org{
		ID: id, DisplayName: display, Sector: "commercial", Composition: "national",
		Nation: "USA", Provenance: "operator-verified", Status: "active", CreatedBy: "test",
	}); err != nil {
		t.Fatalf("register %s: %v", id, err)
	}
}

// The whole arc through the real intake: a bare invite, a candidate who types
// the name of a registered organisation, and an admin who has to say so.
func TestClaimedRegisteredOrg_PanelAsksAndApproveIsRefusedUntilAttached(t *testing.T) {
	ctx := context.Background()
	s := newOnboardTestServer(t)
	orgs := withOrgs(t, s, true)
	registerClaimTestOrg(t, orgs, "demoforge", "Demoforge")
	h := s.Routes()

	// A bare invite: the sponsor pinned no organisation, so affiliation.id can
	// only come from what the candidate writes.
	tok := mintPrebound(t, s, onboardstore.Prebound{Tier: "developer", Sector: "commercial"})
	id := submit(t, h, tok, map[string]any{
		"name": "Pat Vendor", "email": "pat@demoforge.example", "organization": "Demoforge",
	})

	p, err := s.Onboard.GetProposal(ctx, id)
	if err != nil {
		t.Fatalf("get proposal: %v", err)
	}
	if !p.Complete {
		t.Fatalf("the org resolves, so the proposal must be complete: note=%q", p.Note)
	}
	if p.OrgSource != onboardstore.OrgSourceTyped {
		t.Fatalf("org_source = %q, want %q — the submit path records the provenance",
			p.OrgSource, onboardstore.OrgSourceTyped)
	}
	if err := s.Onboard.DecideProposal(ctx, id, onboardstore.ProposalSponsorApproved, "sp@example.test", ""); err != nil {
		t.Fatalf("sponsor-approve: %v", err)
	}

	// The panel asks, and it asks the right question: the org exists, so the
	// only decision is whether this person belongs to it.
	body := webGet(t, s, "/onboarding?status=sponsor_approved&id="+id).Body.String()
	for _, want := range []string{"is claimed by the candidate", "/onboarding/" + id + "/org-attach"} {
		if !strings.Contains(body, want) {
			t.Fatalf("review page missing %q", want)
		}
	}
	if strings.Contains(body, "/org-new") {
		t.Error("a registered org has nothing to create — the create action must not render")
	}
	// The page says so before the click: Approve renders disabled with the
	// reason linked to it, the way the armed unregistered-org gate does.
	if !strings.Contains(body, claimDisabled) || !strings.Contains(body, claimApproveWhy) ||
		!strings.Contains(body, `aria-describedby="ob-approve-why-`+id+`"`) {
		t.Error("approve over a claimed org must render disabled with its inline reason")
	}

	// The approval is held, and the refusal names the organisation.
	rec := webApprove(t, s, id, "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("approve over an unattached claim: want 409, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "demoforge") {
		t.Errorf("the refusal must name the organisation: %s", rec.Body.String())
	}

	// The attach is the decision. After it the panel closes and the approval
	// goes through.
	if rec := webForm(t, s, "/onboarding/"+id+"/org-attach", url.Values{"org": {"demoforge"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("attach: got %d: %s", rec.Code, rec.Body.String())
	}
	after, err := s.Onboard.GetProposal(ctx, id)
	if err != nil {
		t.Fatalf("reload proposal: %v", err)
	}
	if after.OrgSource != onboardstore.OrgSourceOperator {
		t.Errorf("org_source after attach = %q, want %q", after.OrgSource, onboardstore.OrgSourceOperator)
	}
	if f, cs := s.orgMatchesForProposal(*after); f.AffID != "" || cs != nil {
		t.Errorf("the panel must close once an admin has attached: %q %+v", f.AffID, cs)
	}
	if bodyAfter := webGet(t, s, "/onboarding?status=sponsor_approved&id="+id).Body.String(); strings.Contains(bodyAfter, claimDisabled) || strings.Contains(bodyAfter, claimApproveWhy) {
		t.Error("approve must render enabled once an admin has attached the org")
	}
	if rec := webApprove(t, s, id, ""); rec.Code != http.StatusSeeOther {
		t.Fatalf("approve after the attach: want 303, got %d: %s", rec.Code, rec.Body.String())
	}
}

// A registry that cannot be read holds the approval. The gate exists to refuse
// an unverified membership, so a store error must not read as "not registered"
// and let an unchecked claim through with no decision recorded.
func TestAttachedOrgGate_RefusesWhenTheRegistryCannotBeRead(t *testing.T) {
	s := newOnboardTestServer(t)
	orgs := withOrgs(t, s, true)
	registerClaimTestOrg(t, orgs, "demoforge", "Demoforge")
	id := seedOrgMatchProposal(t, s, "demoforge", "USA")
	p, err := s.Onboard.GetProposal(context.Background(), id)
	if err != nil {
		t.Fatalf("get proposal: %v", err)
	}
	if err := orgs.Close(); err != nil {
		t.Fatalf("close the registry: %v", err)
	}
	derr := s.requireAttachedOrg(p)
	if derr == nil {
		t.Fatal("a registry read error must hold the approval, not pass it")
	}
	if derr.status != http.StatusServiceUnavailable || !strings.Contains(derr.msg, "retry") {
		t.Fatalf("want 503 naming a retry, got %d: %s", derr.status, derr.msg)
	}
}

// The other provenance, unchanged: the invite pinned the org at mint, so
// nothing about it is the candidate's to claim and nothing is asked.
func TestSponsorBoundRegisteredOrg_AsksNothingAndApproves(t *testing.T) {
	ctx := context.Background()
	s := newOnboardTestServer(t)
	orgs := withOrgs(t, s, true)
	registerClaimTestOrg(t, orgs, "demoforge", "Demoforge")
	h := s.Routes()

	tok := mintPrebound(t, s, onboardstore.Prebound{Tier: "developer", Org: "demoforge", Sector: "commercial"})
	id := submit(t, h, tok, map[string]any{"name": "Sam Bound", "email": "sam@demoforge.example"})

	p, err := s.Onboard.GetProposal(ctx, id)
	if err != nil {
		t.Fatalf("get proposal: %v", err)
	}
	if p.OrgSource != onboardstore.OrgSourceSponsor {
		t.Fatalf("org_source = %q, want %q", p.OrgSource, onboardstore.OrgSourceSponsor)
	}
	if f, cs := s.orgMatchesForProposal(*p); f.AffID != "" || cs != nil {
		t.Fatalf("a sponsor-bound org must raise no panel: %q %+v", f.AffID, cs)
	}
	if derr := s.requireAttachedOrg(p); derr != nil {
		t.Fatalf("a sponsor-bound org must not hold the approval: %s", derr.msg)
	}
	if err := s.Onboard.DecideProposal(ctx, id, onboardstore.ProposalSponsorApproved, "sp@example.test", ""); err != nil {
		t.Fatalf("sponsor-approve: %v", err)
	}
	if rec := webApprove(t, s, id, ""); rec.Code != http.StatusSeeOther {
		t.Fatalf("approve: want 303, got %d: %s", rec.Code, rec.Body.String())
	}
}

// The gate is narrow on purpose. An organisation the registry does not know is
// governed by ONBOARD_REQUIRE_REGISTERED_ORG, which ships off by goatnet
// decision; firing here would arm it fleet-wide through the back door. The
// #1054 panel for that case stays exactly as it was.
func TestAttachedOrgGate_LeavesUnregisteredOrgsToTheirOwnGate(t *testing.T) {
	s := newOnboardTestServer(t)
	withOrgs(t, s, true)
	id := seedOrgMatchProposal(t, s, "never-registered", "SVN")
	p, err := s.Onboard.GetProposal(context.Background(), id)
	if err != nil {
		t.Fatalf("get proposal: %v", err)
	}
	if derr := s.requireAttachedOrg(p); derr != nil {
		t.Fatalf("an unregistered org must not be held by this gate: %s", derr.msg)
	}
	f, cs := s.orgMatchesForProposal(*p)
	if f.AffID != "never-registered" || f.Registered || len(cs) != 0 {
		t.Fatalf("the unregistered panel must be unchanged: %+v %+v", f, cs)
	}
	body := webGet(t, s, "/onboarding?status=sponsor_approved&id="+id).Body.String()
	if !strings.Contains(body, "is not a registered org") || !strings.Contains(body, "/org-new") {
		t.Error("the unregistered case keeps its own heading and its create action")
	}
}

// Creating the org from the panel is a decision too. Without recording it the
// new org would resolve on the next render and come straight back as a claim,
// which is a panel the admin cannot clear.
func TestOrgNewRecordsTheDecisionSoThePanelClears(t *testing.T) {
	s := newOnboardTestServer(t)
	withOrgs(t, s, true)
	id := seedOrgMatchProposal(t, s, "brand-new-org", "SVN")

	rec := webForm(t, s, "/onboarding/"+id+"/org-new", url.Values{
		"display_name": {"Brand New Org"}, "nation": {"SVN"},
		"sector": {"commercial"}, "composition": {"national"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create: got %d: %s", rec.Code, rec.Body.String())
	}
	p, err := s.Onboard.GetProposal(context.Background(), id)
	if err != nil {
		t.Fatalf("get proposal: %v", err)
	}
	if p.OrgSource != onboardstore.OrgSourceOperator {
		t.Errorf("org_source after create = %q, want %q", p.OrgSource, onboardstore.OrgSourceOperator)
	}
	if f, cs := s.orgMatchesForProposal(*p); f.AffID != "" || cs != nil {
		t.Errorf("the org an admin just created must not return as a claim: %q %+v", f.AffID, cs)
	}
}

// An org whose registered nation contradicts the manifest's is dropped by the
// name matcher's nation scope. The claimed org still has to be attachable from
// its own panel, or the admin gets a question with no answer: create is refused
// (the id exists) and attach is not offered.
func TestClaimedOrgIsAttachableEvenWhenTheNationScopeDropsIt(t *testing.T) {
	s := newOnboardTestServer(t)
	orgs := withOrgs(t, s, true)
	registerClaimTestOrg(t, orgs, "demoforge", "Demoforge") // nation USA
	id := seedOrgMatchProposal(t, s, "demoforge", "CAN")      // the candidate said CAN
	p, err := s.Onboard.GetProposal(context.Background(), id)
	if err != nil {
		t.Fatalf("get proposal: %v", err)
	}
	f, cs := s.orgMatchesForProposal(*p)
	if !f.Registered {
		t.Fatalf("the typed org resolves: %+v", f)
	}
	if len(cs) == 0 || cs[0].ID != "demoforge" || cs[0].Band != orgstore.BandExact {
		t.Fatalf("the claimed org must lead its own panel: %+v", cs)
	}
}
