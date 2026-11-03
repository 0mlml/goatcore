package handlers

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/config"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/onboardstore"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/orgstore"
)

// reviewView assembles the review card's view the way the queue does for an
// admin session at a pending stage: editable fields, then the resolved chain,
// then the nation seed that depends on both.
func reviewView(t *testing.T, s *Server, id string) proposalView {
	t.Helper()
	p, err := s.Onboard.GetProposal(context.Background(), id)
	if err != nil {
		t.Fatalf("get proposal: %v", err)
	}
	s.Cfg = &config.Config{OrgMatchEnabled: true}
	v := proposalToView(*p)
	f, cs := s.orgMatchesForProposal(*p)
	v.OrgTyped, v.OrgCandidates, v.OrgKind, v.OrgNation = f.AffID, cs, f.Kind, f.Nation
	v.OrgTypedName, v.OrgClaimed = f.Typed, f.Registered
	s.fillEditable(&v, *p, loadIntakeHints)
	v.OrgResolved = s.orgChainForProposal(*p)
	v.seedNationFromRegisteredOrg()
	return v
}

func TestSeedNation(t *testing.T) {
	ukr := []orgstore.OrgView{{Org: orgstore.Org{ID: "demoforge", Nation: "UKR"}}}
	cases := []struct {
		name           string
		manifestNation string
		resolved       []orgstore.OrgView
		wantNation     string
		wantFromOrg    string
	}{
		{"manifest value always wins", "USA", ukr, "USA", ""},
		{"empty manifest takes the registered nation", "", ukr, "UKR", "demoforge"},
		{"unregistered org seeds nothing", "", nil, "", ""},
		{"registered org with no nation seeds nothing", "", []orgstore.OrgView{{Org: orgstore.Org{ID: "x"}}}, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			nation, fromOrg := seedNation(tc.manifestNation, tc.resolved)
			if nation != tc.wantNation || fromOrg != tc.wantFromOrg {
				t.Fatalf("seedNation = (%q, %q), want (%q, %q)", nation, fromOrg, tc.wantNation, tc.wantFromOrg)
			}
		})
	}
}

// #1122 part 1. A candidate who typed an org that resolves in the registry
// carries no affiliation.nation (intake asks only for the person's nationality),
// so the correct-a-field Nation box rendered empty and the reviewer typed the
// nation by hand. The registered org's nation is a registry fact, so seeding the
// box from it keeps ADR 1171: nothing here comes from the candidate's answer.
func TestReviewNation_ResolvedOrgSeedsTheCorrectAFieldBox(t *testing.T) {
	const manifest = `{
	  "identity": {"handle":"user246x","email":"l@example.test","nationality":"CAN"},
	  "affiliation": {"id":"demoforge","kind":"vendor"},
	  "org": {"display_name":"Demoforge"}
	}`
	s, id := fieldEditServer(t, manifest, onboardstore.ProposalProposed)
	// The issue's case: the candidate TYPED the org, nobody pinned it.
	if err := s.Onboard.SetProposalOrgSource(context.Background(), id, onboardstore.OrgSourceTyped); err != nil {
		t.Fatalf("mark the org as typed: %v", err)
	}
	v := reviewView(t, s, id)

	out := renderPage(t, "onboarding", &pageVars{
		CSRFToken: "nonce-123", OnboardStatus: "proposed", Proposal: &v,
	})
	if !strings.Contains(out, `name="affiliation.nation" value="UKR"`) {
		t.Fatalf("the registered org's nation must seed the box; view nation=%q", v.OrgNation)
	}
	// The box now holds a value the manifest does not. Apply writes it, so the
	// card says where it came from.
	if !strings.Contains(out, "From the registered org") {
		t.Error("a seeded nation must name its source")
	}
	if strings.Contains(out, "This box is the one that counts") {
		t.Error("a seeded box agrees with the registry, so the divergence hint must not render")
	}
	if strings.Contains(out, `value="CAN"`) {
		t.Error("the candidate's nationality must never reach a nation box as a value")
	}
}

// #1122 part 2. For an org the registry does not know, the create-org Nation
// box stays empty (ADR 1171 Decision 6) and the candidate's nationality sits
// beside it as a labeled reference, so the reviewer does not have to open the
// identity card to read it.
func TestReviewNation_UnregisteredOrgShowsNationalityAsReference(t *testing.T) {
	const manifest = `{
	  "identity": {"handle":"user246x","email":"l@example.test","nationality":"CAN"},
	  "affiliation": {"id":"northwind-labs","kind":"vendor"},
	  "org": {"display_name":"Northwind Labs"}
	}`
	s, id := fieldEditServer(t, manifest, onboardstore.ProposalProposed)
	v := reviewView(t, s, id)
	if v.OrgTyped == "" {
		t.Fatal("precondition: the org match panel must be open for an unregistered typed org")
	}

	out := renderPage(t, "onboarding", &pageVars{
		CSRFToken: "nonce-123", OnboardStatus: "proposed", Proposal: &v,
	})
	if !strings.Contains(out, `name="nation" id="org-nation" value=""`) {
		t.Error("the create-org Nation box must stay empty: an org's nation is typed by the admin")
	}
	if !strings.Contains(out, "Candidate nationality") || !strings.Contains(out, "<code>CAN</code>") {
		t.Error("the candidate's nationality must show beside the create-org Nation box as a reference")
	}
}

// postSeededForm renders the review card for id, then posts the correct-a-field
// form the way the browser does: every rendered box, the hidden seed marker
// included, with `override` applied on top as the admin's edits.
func postSeededForm(t *testing.T, s *Server, id string, override map[string]string) (int, string) {
	t.Helper()
	v := reviewView(t, s, id)
	s.csrfNonce = "nonce-123"
	form := url.Values{}
	form.Set("csrf", "nonce-123")
	form.Set(FieldAffiliationID, v.OrgID)
	form.Set(FieldAffiliationNat, v.OrgNation)
	form.Set(FieldOrgDisplayName, v.OrgDisplayName)
	form.Set(FieldIdentityGitHub, v.GitHub)
	if v.OrgNationFromOrg != "" {
		form.Set(nationSeedField, v.OrgNationFromOrg)
	}
	for k, val := range override {
		form.Set(k, val)
	}
	r := httptest.NewRequestWithContext(context.Background(), "POST", "/onboarding/"+id+"/fields", strings.NewReader(form.Encode()))
	r.SetPathValue("id", id)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	s.handleFieldEdits(w, r)
	return w.Code, w.Body.String()
}

func manifestAffiliation(t *testing.T, s *Server, id string) map[string]any {
	t.Helper()
	p, err := s.Onboard.GetProposal(context.Background(), id)
	if err != nil {
		t.Fatalf("get proposal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(p.ManifestJSON), &m); err != nil {
		t.Fatalf("manifest: %v", err)
	}
	aff, _ := m["affiliation"].(map[string]any)
	return aff
}

const typedDemoforgeManifest = `{
  "identity": {"handle":"user246x","email":"l@example.test","nationality":"CAN"},
  "affiliation": {"id":"demoforge","kind":"vendor"},
  "org": {"display_name":"Demoforge"}
}`

func typedOrgServer(t *testing.T, manifest string) (*Server, string) {
	t.Helper()
	s, id := fieldEditServer(t, manifest, onboardstore.ProposalProposed)
	if err := s.Onboard.SetProposalOrgSource(context.Background(), id, onboardstore.OrgSourceTyped); err != nil {
		t.Fatalf("mark the org as typed: %v", err)
	}
	return s, id
}

// An untouched seed is written, and the audit row says where it came from:
// a registry value, not an admin's correction. The form promises "leave a box
// untouched to leave it alone", and an unrelated fix (here the GitHub login)
// must not read in the audit as the admin having corrected the nation.
func TestReviewNation_UntouchedSeedAuditSaysRegistrySourced(t *testing.T) {
	s, id := typedOrgServer(t, typedDemoforgeManifest)
	code, body := postSeededForm(t, s, id, map[string]string{FieldIdentityGitHub: "lukesky"})
	if code != 303 {
		t.Fatalf("apply: want 303, got %d: %s", code, body)
	}
	if aff := manifestAffiliation(t, s, id); aff["nation"] != "UKR" {
		t.Fatalf("the seed must land on the manifest; affiliation=%v", aff)
	}
	ev := s.Audit.Recent(1)
	if len(ev) != 1 || ev[0].Action != "onboard_field_edit" {
		t.Fatalf("want one onboard_field_edit audit row, got %+v", ev)
	}
	if got := ev[0].After[FieldAffiliationNat+".source"]; got != "registry:demoforge" {
		t.Errorf("audit must name the registry as the nation's source, got %v (after=%v)", got, ev[0].After)
	}
}

// A nation the admin typed over the seed is theirs, and carries no source tag.
func TestReviewNation_TypedNationOverridesSeedUnsourced(t *testing.T) {
	s, id := typedOrgServer(t, typedDemoforgeManifest)
	code, body := postSeededForm(t, s, id, map[string]string{FieldAffiliationNat: "USA"})
	if code != 303 {
		t.Fatalf("apply: want 303, got %d: %s", code, body)
	}
	if aff := manifestAffiliation(t, s, id); aff["nation"] != "USA" {
		t.Fatalf("the typed nation must stand; affiliation=%v", aff)
	}
	if ev := s.Audit.Recent(1); ev[0].After[FieldAffiliationNat+".source"] != nil {
		t.Errorf("a typed nation must not be tagged as registry-sourced: %v", ev[0].After)
	}
}

// The seed follows an org change made in the same Apply. Edits apply in field
// order (affiliation.id before affiliation.nation), so an untouched seed from
// the org the admin just moved AWAY from would otherwise land on the manifest
// beside the new org and stand there; once the manifest carries a nation the
// seed never revisits it.
func TestReviewNation_SeedFollowsOrgChangedInSameApply(t *testing.T) {
	registerOrg := func(t *testing.T, s *Server, id, nation string) {
		t.Helper()
		if _, err := s.Orgs.CreateOrg(orgstore.Org{ID: id, DisplayName: id, Kind: "vendor",
			Nation: nation, Provenance: "operator-verified", Status: "active", CreatedBy: "test"}); err != nil {
			t.Fatalf("register %s: %v", id, err)
		}
	}
	t.Run("new org has a nation: it is written and sourced", func(t *testing.T) {
		s, id := typedOrgServer(t, typedDemoforgeManifest)
		registerOrg(t, s, "northwind", "USA")
		code, body := postSeededForm(t, s, id, map[string]string{FieldAffiliationID: "northwind"})
		if code != 303 {
			t.Fatalf("apply: want 303, got %d: %s", code, body)
		}
		aff := manifestAffiliation(t, s, id)
		if aff["id"] != "northwind" || aff["nation"] != "USA" {
			t.Fatalf("want northwind/USA, got %v", aff)
		}
		if got := s.Audit.Recent(1)[0].After[FieldAffiliationNat+".source"]; got != "registry:northwind" {
			t.Errorf("source must name the NEW org, got %v", got)
		}
	})
	t.Run("new org has no nation: nothing is written, the next render seeds afresh", func(t *testing.T) {
		s, id := typedOrgServer(t, typedDemoforgeManifest)
		registerOrg(t, s, "northwind", "")
		code, body := postSeededForm(t, s, id, map[string]string{FieldAffiliationID: "northwind"})
		if code != 303 {
			t.Fatalf("apply: want 303, got %d: %s", code, body)
		}
		aff := manifestAffiliation(t, s, id)
		if aff["id"] != "northwind" || aff["nation"] != nil {
			t.Fatalf("the old org's nation must not stand beside the new org: %v", aff)
		}
	})
}

// The registry does not shape-check what it stores, setNation does. A stored
// value that would fail Apply is never seeded: the box stays empty (and the
// divergence hint still shows the registry's value), so an unrelated fix on
// the proposal does not 400 on a box the reviewer never touched.
func TestReviewNation_UnshapedRegistryNationIsNotSeeded(t *testing.T) {
	const manifest = `{
	  "identity": {"handle":"user246x","email":"l@example.test","nationality":"CAN"},
	  "affiliation": {"id":"natoact","kind":"vendor"},
	  "org": {"display_name":"NATO ACT"}
	}`
	s, id := typedOrgServer(t, manifest)
	if _, err := s.Orgs.CreateOrg(orgstore.Org{ID: "natoact", DisplayName: "NATO ACT", Kind: "vendor",
		Nation: "NATO-ACT", Provenance: "operator-verified", Status: "active", CreatedBy: "test"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	v := reviewView(t, s, id)
	if v.OrgNation != "" || v.OrgNationFromOrg != "" {
		t.Fatalf("an unshaped registry nation must not seed the box: (%q, %q)", v.OrgNation, v.OrgNationFromOrg)
	}
	out := renderPage(t, "onboarding", &pageVars{CSRFToken: "nonce-123", OnboardStatus: "proposed", Proposal: &v})
	if !strings.Contains(out, "<code>NATO-ACT</code>") {
		t.Error("the registry's value must still be visible beside the empty box")
	}
	code, body := postSeededForm(t, s, id, map[string]string{FieldIdentityGitHub: "lukesky"})
	if code != 303 {
		t.Fatalf("a GitHub-only fix must apply: got %d: %s", code, body)
	}
}
