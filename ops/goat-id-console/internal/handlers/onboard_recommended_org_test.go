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

// mintInviteForm posts the admin mint form and returns the response recorder,
// so a test can assert on the refusal body as well as the code.
func mintInviteForm(t *testing.T, s *Server, h http.Handler, extra url.Values) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{}
	form.Set("csrf", s.csrfNonce)
	form.Set("env", "zagros")
	form.Set("tier", "developer")
	form.Set("ttl_hours", "72")
	for k, vs := range extra {
		form[k] = vs
	}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/onboarding/invite", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// mintedPrebound mints one invite through the admin form and returns what was
// pinned on THAT invite, found by ID. List position cannot name it: ListInvites
// orders by created_at in whole seconds, so the last element is the newest row
// only while both mints share a second, and a slow -race run does not (#1146).
func mintedPrebound(t *testing.T, s *Server, h http.Handler, extra url.Values) onboardstore.Prebound {
	t.Helper()
	seen := map[string]bool{}
	for _, inv := range listInvites(t, h) {
		seen[inv.ID] = true
	}
	if rec := mintInviteForm(t, s, h, extra); rec.Code != http.StatusOK {
		t.Fatalf("mint %v: want 200, got %d: %s", extra, rec.Code, rec.Body.String())
	}
	var minted []onboardstore.Invite
	for _, inv := range listInvites(t, h) {
		if !seen[inv.ID] {
			minted = append(minted, inv)
		}
	}
	if len(minted) != 1 {
		t.Fatalf("mint %v: want exactly one new invite, got %d", extra, len(minted))
	}
	pb, err := minted[0].Prebound()
	if err != nil {
		t.Fatalf("decode minted prebinding: %v", err)
	}
	return pb
}

// The mint is where a recommended org becomes a fact about the invite, so it is
// where the two ways of getting it wrong have to be refused: naming an org the
// registry does not know, and naming one on a goatnet that has no registry at
// all. Both would produce an invite that LOOKS pinned and behaves un-pinned.
func TestInviteMintOrgIsFailClosed(t *testing.T) {
	s := newOnboardTestServer(t)
	h := s.Routes()

	// No registry wired at all — a named org cannot be honoured, so it is refused
	// rather than dropped.
	rec := mintInviteForm(t, s, h, url.Values{"org": {"org39x"}})
	if rec.Code == http.StatusOK {
		t.Fatalf("no registry: mint should refuse a named org, got 200")
	}

	orgs := withOrgs(t, s, true)
	if _, err := orgs.CreateOrg(orgstore.Org{
		ID: "org39x", DisplayName: "org39x", Kind: "vendor", Nation: "ISL",
	}); err != nil {
		t.Fatal(err)
	}
	if err := orgs.AddAlias("org41x", "org39x", "abbreviation", "op"); err != nil {
		t.Fatal(err)
	}

	// Unknown org — refused, and the message says what to do about it.
	rec = mintInviteForm(t, s, h, url.Values{"org": {"org39x-holdings"}})
	if rec.Code == http.StatusOK {
		t.Fatalf("unknown org: mint should refuse, got 200")
	}
	if !strings.Contains(rec.Body.String(), "not a registered org") {
		t.Fatalf("refusal should name the cause, got: %s", rec.Body.String())
	}

	// An ALIAS is accepted and stored CANONICALLY. Storing the alias would put a
	// second spelling of one company back into affiliation.id — the exact defect
	// the registry exists to close.
	if got := mintedPrebound(t, s, h, url.Values{"org": {"org41x"}}).Org; got != "org39x" {
		t.Fatalf("prebound org: want canonical %q, got %q", "org39x", got)
	}

	// Blank stays blank: the whole affordance is optional, and an un-pinned
	// invite must behave exactly as it did before this existed.
	if got := mintedPrebound(t, s, h, nil).Org; got != "" {
		t.Fatalf("prebound org with no input: want empty, got %q", got)
	}
}

// mintPinnedToken mints an invite pinned to org directly through the store, so a
// test can drive preview/submit with a usable single-use token.
func mintPinnedToken(t *testing.T, s *Server, org string) string {
	t.Helper()
	pb := onboardstore.Prebound{Tier: "developer", Org: org}
	pbJSON, _ := json.Marshal(pb)
	inv := &onboardstore.Invite{Sponsor: "dev@bypass.local", Env: "zagros", PreboundJSON: string(pbJSON)}
	tok, err := s.Onboard.MintInvite(context.Background(), inv)
	if err != nil {
		t.Fatalf("mint via store: %v", err)
	}
	return tok
}

// The candidate half: the intake page is told the org is settled and given the
// LABEL to show, and the assembled manifest carries the registered id rather
// than a slug of whatever the person typed.
func TestRecommendedOrgReachesPreviewAndManifest(t *testing.T) {
	s := newOnboardTestServer(t)
	orgs := withOrgs(t, s, true)
	if _, err := orgs.CreateOrg(orgstore.Org{
		ID: "org22x", DisplayName: "org22x",
		Kind: "vendor", Nation: "ISL",
	}); err != nil {
		t.Fatal(err)
	}
	h := s.Routes()

	pv := preview(t, h, mintPinnedToken(t, s, "org22x"))
	if !pv.Valid || !pv.OrgLocked {
		t.Fatalf("preview: want valid+org_locked, got valid=%v locked=%v", pv.Valid, pv.OrgLocked)
	}
	if pv.OrgName != "org22x" {
		t.Fatalf("preview org_name: want the display name, got %q", pv.OrgName)
	}
	if pv.Prebound.Org != "org22x" {
		t.Fatalf("preview prebound.org: got %q", pv.Prebound.Org)
	}

	// The candidate types a loose variant of their own employer. Before the pin,
	// slug("BAE Systems") became affiliation.id and minted a second org.
	tok := mintPinnedToken(t, s, "org22x")
	pid := submit(t, h, tok, map[string]any{
		"name": "Sam Engineer", "email": "sam@baesystems.example", "organization": "BAE Systems",
		"affiliation": "vendor",
	})
	p, err := s.Onboard.GetProposal(context.Background(), pid)
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Affiliation struct {
			ID string `json:"id"`
		} `json:"affiliation"`
	}
	if err := json.Unmarshal([]byte(p.ManifestJSON), &m); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if m.Affiliation.ID != "org22x" {
		t.Fatalf("affiliation.id: want the registered org, got %q", m.Affiliation.ID)
	}

	// Their words are not discarded — the disagreement reaches the reviewer as a
	// petition, the same mechanic as a tier or machine-quota disagreement.
	var pets []petition
	if err := json.Unmarshal([]byte(p.Petitions), &pets); err != nil {
		t.Fatalf("decode petitions: %v (%q)", err, p.Petitions)
	}
	found := false
	for _, pt := range pets {
		if pt.Field == "organization" && pt.RequestedValue == "BAE Systems" {
			found = true
		}
	}
	if !found {
		t.Fatalf("want an organization petition, got %+v", pets)
	}
}

// Accepting the pre-filled name is the common path and must be silent: the page
// submits the LABEL it displayed, which is not the org id, and that must not be
// read as a disagreement.
func TestAcceptingTheShownOrgLabelRaisesNoPetition(t *testing.T) {
	s := newOnboardTestServer(t)
	orgs := withOrgs(t, s, true)
	if _, err := orgs.CreateOrg(orgstore.Org{
		ID: "org108x", DisplayName: "org108x", Kind: "vendor", Nation: "ISL",
	}); err != nil {
		t.Fatal(err)
	}
	h := s.Routes()
	pid := submit(t, h, mintPinnedToken(t, s, "org108x"), map[string]any{
		"name": "Will Engineer", "email": "will@org109x.example", "organization": "org108x",
		"affiliation": "vendor",
	})
	p, err := s.Onboard.GetProposal(context.Background(), pid)
	if err != nil {
		t.Fatal(err)
	}
	var pets []petition
	_ = json.Unmarshal([]byte(p.Petitions), &pets)
	for _, pt := range pets {
		if pt.Field == "organization" {
			t.Fatalf("accepting the shown label must not petition, got %+v", pt)
		}
	}
}
