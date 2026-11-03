package handlers

import (
	"bytes"
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

// Submitting an intake whose organization the org registry does not know
// parks an admin-completable draft instead of minting a proposal that carries
// a fresh slug into the registry at approval. The candidate sees nothing new;
// the sponsor gets one note; the admin gets the marker, the panel headed with
// the typed name, and an approve button that is honest about the state.

const (
	unregOrgTyped = "Demoforge Dynamics"
	unregOrgSlug  = "demoforge-dynamics"
	unregOrgNote  = `INCOMPLETE (unregistered organization "` + unregOrgTyped + `"); admin to complete`
	sponsorNote   = "Organization not registered yet. An administrator will register it or match it to an existing one before approval."
	queuePill     = "org not registered"
	approveWhy    = "Approve is unavailable until the organization above is registered or attached."
	approveWarn   = "This organization is not registered. Approving now provisions the person under an unregistered organization"
)

func unregSubmission(email string) map[string]any {
	return map[string]any{"name": "Pat Person", "email": email, "organization": unregOrgTyped}
}

func mintDeveloper(t *testing.T, s *Server) string {
	t.Helper()
	return mintPrebound(t, s, onboardstore.Prebound{Tier: "developer", Sector: "commercial"})
}

func TestSubmit_RegisteredOrgCompletes(t *testing.T) {
	s := newOnboardTestServer(t)
	orgs := withOrgs(t, s, true)
	if _, err := orgs.CreateOrg(orgstore.Org{ID: unregOrgSlug, Nation: "CAN", Sector: "commercial", Provenance: "operator-verified"}); err != nil {
		t.Fatal(err)
	}
	h := s.Routes()
	pid := submit(t, h, mintDeveloper(t, s), unregSubmission("reg@example.test"))
	p, m := loadProposal(t, s, pid)
	if !p.Complete {
		t.Fatalf("registered org must complete, note=%q", p.Note)
	}
	if id, _ := affField(m, "id"); id != unregOrgSlug {
		t.Fatalf("affiliation.id = %q, want %q", id, unregOrgSlug)
	}
	if strings.Contains(p.Note, "INCOMPLETE") {
		t.Fatalf("note carries a draft marker: %q", p.Note)
	}
}

func TestSubmit_RegisteredAliasCompletes(t *testing.T) {
	s := newOnboardTestServer(t)
	orgs := withOrgs(t, s, true)
	if _, err := orgs.CreateOrg(orgstore.Org{ID: "demoforge", Nation: "CAN", Sector: "commercial", Provenance: "operator-verified"}); err != nil {
		t.Fatal(err)
	}
	if err := orgs.AddAlias(unregOrgSlug, "demoforge", "name-variant", "test"); err != nil {
		t.Fatal(err)
	}
	h := s.Routes()
	pid := submit(t, h, mintDeveloper(t, s), unregSubmission("alias@example.test"))
	p, _ := loadProposal(t, s, pid)
	if !p.Complete {
		t.Fatalf("alias must complete, note=%q", p.Note)
	}
}

func TestSubmit_UnregisteredOrgParksDraftAndConsumesInvite(t *testing.T) {
	s := newOnboardTestServer(t)
	withOrgs(t, s, true)
	h := s.Routes()
	tok := mintDeveloper(t, s)
	pid := submit(t, h, tok, unregSubmission("draft@example.test"))
	p, m := loadProposal(t, s, pid)
	if p.Complete {
		t.Fatalf("unregistered org must park a draft, note=%q", p.Note)
	}
	if sv, _ := m["schema_version"].(string); sv != intakeRecordSchema {
		t.Fatalf("schema_version = %q, want %q", sv, intakeRecordSchema)
	}
	if !strings.Contains(p.Note, unregOrgNote) {
		t.Fatalf("note = %q, want it to contain %q", p.Note, unregOrgNote)
	}
	if code := submitRaw(t, h, tok, withPersonDefaults(unregSubmission("draft@example.test"))); code != http.StatusConflict {
		t.Fatalf("second submit on a consumed token: got %d, want 409", code)
	}
}

func TestSubmit_UnregisteredOrgCompletesWhenRegistryOff(t *testing.T) {
	t.Run("orgs nil", func(t *testing.T) {
		s := newOnboardTestServer(t)
		s.Cfg.OrgMatchEnabled = true
		h := s.Routes()
		p, _ := loadProposal(t, s, submit(t, h, mintDeveloper(t, s), unregSubmission("nil@example.test")))
		if !p.Complete {
			t.Fatalf("no registry wired must complete, note=%q", p.Note)
		}
	})
	t.Run("match disabled", func(t *testing.T) {
		s := newOnboardTestServer(t)
		withOrgs(t, s, false)
		h := s.Routes()
		p, _ := loadProposal(t, s, submit(t, h, mintDeveloper(t, s), unregSubmission("off@example.test")))
		if !p.Complete {
			t.Fatalf("ORG_MATCH_ENABLED=false must complete, note=%q", p.Note)
		}
	})
}

func TestUnregisteredOrgAtIntake_OrgMatchDisabledBypassesLookup(t *testing.T) {
	s := newOnboardTestServer(t)
	s.Cfg.OrgMatchEnabled = false
	in := effectiveInputs{Organization: "New Moon Corp"}
	if got := s.unregisteredOrgAtIntake(in); got != "" {
		t.Fatalf("orgMatchActive=false: unregisteredOrgAtIntake = %q, want empty", got)
	}
}

func TestUnregisteredOrgAtIntake_ResolveNotFoundReturnsTypedName(t *testing.T) {
	s := newOnboardTestServer(t)
	withOrgs(t, s, true)
	in := effectiveInputs{Organization: unregOrgTyped}
	if got := s.unregisteredOrgAtIntake(in); got != unregOrgTyped {
		t.Fatalf("ErrNotFound: unregisteredOrgAtIntake = %q, want typed %q", got, unregOrgTyped)
	}
}

func TestUnregisteredOrgAtIntake_ResolveErrorFailsQuietly(t *testing.T) {
	s := newOnboardTestServer(t)
	orgs := withOrgs(t, s, true)
	_ = orgs.Close() // closed DB simulates store trouble (non-ErrNotFound)
	in := effectiveInputs{Organization: unregOrgTyped}
	if got := s.unregisteredOrgAtIntake(in); got != "" {
		t.Fatalf("store error: unregisteredOrgAtIntake = %q, want empty (fail-quiet)", got)
	}
}

// A submission with no organization is refused at intake (#257), before the
// invite is consumed and before the registry is consulted; the same link then
// works once the organization is typed, and the registry check runs on it.
func TestSubmit_NoOrganizationIsRefusedBeforeTheRegistryCheck(t *testing.T) {
	s := newOnboardTestServer(t)
	withOrgs(t, s, true)
	h := s.Routes()
	tok := mintDeveloper(t, s)
	body, _ := json.Marshal(map[string]any{"token": tok, "submission": withPersonDefaults(map[string]any{"name": "No Org", "email": "noorg@example.test"})})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/public/onboard/submit", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("no organization: want 422, got %d: %s", rec.Code, rec.Body.String())
	}
	var res map[string]onboardAPIError
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res["error"].Code != "validation_failed" || res["error"].Fields["organization"] == "" {
		t.Fatalf("no organization: want validation_failed naming organization, got %s", rec.Body.String())
	}
	pid := submit(t, h, tok, unregSubmission("noorg@example.test"))
	if p, _ := loadProposal(t, s, pid); p.Complete {
		t.Fatalf("unregistered org after a refusal must still park a draft, note=%q", p.Note)
	}
}

func TestOrgAttach_CompletesUnregisteredDraft(t *testing.T) {
	s := newOnboardTestServer(t)
	draftID, _ := parkUnregisteredDraft(t, s)
	rr := webForm(t, s, "/onboarding/"+draftID+"/org-attach", url.Values{"org": {"known-org"}})
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("org-attach: got %d body %s", rr.Code, rr.Body.String())
	}
	p, m := loadProposal(t, s, draftID)
	if !p.Complete {
		t.Fatalf("draft not completed after org-attach, note=%q", p.Note)
	}
	if id, _ := affField(m, "id"); id != "known-org" {
		t.Fatalf("affiliation.id = %q, want known-org", id)
	}
	body := webGet(t, s, "/onboarding?status=proposed&id="+draftID).Body.String()
	if strings.Contains(body, queuePill) || strings.Contains(body, "is not a registered org") {
		t.Fatal("stale unregistered marker after attach")
	}
}

func TestSubmit_PreboundOrgMissingFromRegistryParksDraft(t *testing.T) {
	s := newOnboardTestServer(t)
	withOrgs(t, s, true)
	h := s.Routes()
	tok := mintPrebound(t, s, onboardstore.Prebound{Tier: "developer", Sector: "commercial", Org: "vanished-org"})
	pid := submit(t, h, tok, map[string]any{"name": "Pre Bound", "email": "prebound@example.test"})
	p, _ := loadProposal(t, s, pid)
	if p.Complete {
		t.Fatalf("prebound org absent from the registry must park a draft, note=%q", p.Note)
	}
	if !strings.Contains(p.Note, `unregistered organization "vanished-org"`) {
		t.Fatalf("note = %q, want it to name the prebound id", p.Note)
	}
}

func TestSubmit_CandidateStatusIdenticalEitherWay(t *testing.T) {
	s := newOnboardTestServer(t)
	orgs := withOrgs(t, s, true)
	if _, err := orgs.CreateOrg(orgstore.Org{ID: "known-org", Nation: "CAN", Sector: "commercial", Provenance: "operator-verified"}); err != nil {
		t.Fatal(err)
	}
	h := s.Routes()
	regTok := mintDeveloper(t, s)
	submit(t, h, regTok, map[string]any{"name": "Reg", "email": "reg@example.test", "organization": "Known Org"})
	unregTok := mintDeveloper(t, s)
	submit(t, h, unregTok, unregSubmission("unreg@example.test"))

	reg, unreg := preview(t, h, regTok), preview(t, h, unregTok)
	if reg.Submission == nil || unreg.Submission == nil {
		t.Fatalf("submission status missing: reg=%v unreg=%v", reg.Submission, unreg.Submission)
	}
	// The two submissions are seconds-resolution timestamps taken a few
	// milliseconds apart; under -race they can straddle a second boundary.
	// When a request was submitted is not what this test is about.
	r, u := *reg.Submission, *unreg.Submission
	r.SubmittedAt, u.SubmittedAt = 0, 0
	a, _ := json.Marshal(r)
	b, _ := json.Marshal(u)
	if string(a) != string(b) {
		t.Fatalf("candidate-visible status differs:\nregistered   %s\nunregistered %s", a, b)
	}
}

// parkUnregisteredDraft submits an unregistered-org intake and returns the
// draft id; the registry knows "known-org" so a registered control row can be
// created beside it. The control row's invite pins that org, so it carries no
// org guard at all: a typed name that resolves is a claim with its own guard
// (#1086, onboard_claimed_org_test.go).
func parkUnregisteredDraft(t *testing.T, s *Server) (draftID, regID string) {
	t.Helper()
	orgs := withOrgs(t, s, true)
	if _, err := orgs.CreateOrg(orgstore.Org{ID: "known-org", Nation: "CAN", Sector: "commercial", Provenance: "operator-verified"}); err != nil {
		t.Fatal(err)
	}
	h := s.Routes()
	draftID = submit(t, h, mintDeveloper(t, s), unregSubmission("draft@example.test"))
	pinned := mintPrebound(t, s, onboardstore.Prebound{Tier: "developer", Sector: "commercial", Org: "known-org"})
	regID = submit(t, h, pinned, map[string]any{"name": "Reg", "email": "reg@example.test"})
	return draftID, regID
}

func rowFor(t *testing.T, body, id string) string {
	t.Helper()
	i := strings.Index(body, "&id="+id+`"`)
	if i < 0 {
		t.Fatalf("row for %s not in list", id)
	}
	j := strings.Index(body[i:], "</a>")
	return body[i : i+j]
}

func TestOnboardingList_UnregisteredOrgPill(t *testing.T) {
	s := newOnboardTestServer(t)
	draftID, regID := parkUnregisteredDraft(t, s)
	body := webGet(t, s, "/onboarding?status=proposed").Body.String()
	if !strings.Contains(rowFor(t, body, draftID), queuePill) {
		t.Fatal("draft row lacks the org-not-registered pill")
	}
	if strings.Contains(rowFor(t, body, regID), queuePill) {
		t.Fatal("registered row carries the pill")
	}
}

func TestOnboardingDetail_SponsorNoteAndPanelHeading(t *testing.T) {
	s := newOnboardTestServer(t)
	draftID, _ := parkUnregisteredDraft(t, s)
	body := webGet(t, s, "/onboarding?status=proposed&id="+draftID).Body.String()
	// The sponsor's line is for sponsor sessions only: an admin has the
	// Attach / Register panel right above and needs no note about themselves.
	if strings.Contains(body, sponsorNote) {
		t.Error("admin detail page shows the sponsor-only note")
	}
	for _, want := range []string{
		"/onboarding/" + draftID + "/sponsor-approve",
		"<b>" + unregOrgTyped + "</b> (<code>" + unregOrgSlug + "</code>) is not a registered org",
		"/onboarding/" + draftID + "/org-new",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("detail page lacks %q", want)
		}
	}
}

func TestOnboardingDetail_SponsorSessionSeesNoteWithoutAdminPanel(t *testing.T) {
	s := newOnboardTestServer(t)
	draftID, _ := parkUnregisteredDraft(t, s)
	// Switch to sponsor mode: DevBypassAuth off, portal-backed sponsorship.
	s.Cfg.DevBypassAuth = false
	portalURL, _ := fakeSponsorPortal(t, nil, map[string]sponsorLookupResponse{
		"dev@bypass.local": {Org: "0ff404dd", Alias: "dev-slot", SponsorAuthorized: true, SponsorMaxTier: "developer"},
	})
	s.Cfg.PortalInternalURL = portalURL
	s.Cfg.PortalLookupToken = "svc-token"
	h := s.Routes()

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/onboarding?status=proposed&id="+draftID, nil)
	req.Header.Set("X-Forwarded-Email", "dev@bypass.local")
	req.Header.Set("X-Forwarded-Groups", "DEVELOPER")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("sponsor detail = %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, sponsorNote) {
		t.Fatalf("sponsor detail must show unregistered-organisation note, body=\n%s", body)
	}
	if strings.Contains(body, "/onboarding/"+draftID+"/org-new") {
		t.Fatalf("sponsor detail must not render org-new admin panel, body=\n%s", body)
	}
	if strings.Contains(body, "Approve &amp; provision") {
		t.Fatalf("sponsor detail must not render admin approve button, body=\n%s", body)
	}
}

func TestOrgNew_CompletesUnregisteredDraft(t *testing.T) {
	s := newOnboardTestServer(t)
	draftID, _ := parkUnregisteredDraft(t, s)
	rr := webForm(t, s, "/onboarding/"+draftID+"/org-new", url.Values{
		"display_name": {unregOrgTyped}, "sector": {"commercial"}, "nation": {"CAN"},
	})
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("org-new: got %d body %s", rr.Code, rr.Body.String())
	}
	p, m := loadProposal(t, s, draftID)
	if !p.Complete {
		t.Fatalf("draft not completed after org-new, note=%q", p.Note)
	}
	if id, _ := affField(m, "id"); id != unregOrgSlug {
		t.Fatalf("affiliation.id = %q, want %q", id, unregOrgSlug)
	}
	body := webGet(t, s, "/onboarding?status=proposed&id="+draftID).Body.String()
	if strings.Contains(body, queuePill) || strings.Contains(body, "is not a registered org") {
		t.Fatal("stale unregistered marker after registration")
	}
}

// End-to-end journey: a candidate submits with an unregistered
// organisation, the admin sees the "org not registered" marker and a
// disabled approve button, registers the org via the Org panel, and can
// then approve successfully.
func TestAdminResolvesUnregisteredOrgBeforeApproval(t *testing.T) {
	s := newOnboardTestServer(t)
	withOrgs(t, s, true)
	h := s.Routes()

	// Candidate submit with an unregistered organisation.
	tok := mintPrebound(t, s, onboardstore.Prebound{Tier: "developer", Sector: "commercial"})
	pid := submit(t, h, tok, map[string]any{
		"name": "New Moon Candidate", "email": "newmoon@example.test", "organization": "New Moon Corp",
	})
	p, _ := loadProposal(t, s, pid)
	if p.Complete {
		t.Fatalf("unregistered org must park a draft, note=%q", p.Note)
	}
	// Sponsor vouches; proposal reaches the admin gate.
	stageSponsorApproved(t, s, pid)

	// Require a registered org before approval: queue pill + disabled button
	// on the admin gate (sponsor_approved stage).
	s.Cfg.RequireRegisteredOrg = true
	queueBody := webGet(t, s, "/onboarding?status=sponsor_approved").Body.String()
	row := rowFor(t, queueBody, pid)
	if !strings.Contains(row, queuePill) {
		t.Fatalf("queue row for %s lacks org-not-registered pill", pid)
	}
	const disabled = `disabled aria-disabled="true"`
	detailBefore := webGet(t, s, "/onboarding?status=sponsor_approved&id="+pid).Body.String()
	if !strings.Contains(detailBefore, "Approve &amp; provision") || !strings.Contains(detailBefore, disabled) || !strings.Contains(detailBefore, approveWhy) {
		t.Fatalf("detail before registration must show disabled approve with reason, body=\n%s", detailBefore)
	}

	// Admin registers the organisation via the Org panel.
	rr := webForm(t, s, "/onboarding/"+pid+"/org-new", url.Values{
		"display_name": {"New Moon Corp"}, "sector": {"commercial"}, "nation": {"CAN"},
	})
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("org-new: got %d body %s", rr.Code, rr.Body.String())
	}
	pAfter, _ := loadProposal(t, s, pid)
	if !pAfter.Complete {
		t.Fatalf("draft not completed after org-new, note=%q", pAfter.Note)
	}

	// Detail view no longer carries unregistered markers; approve is enabled.
	detailAfter := webGet(t, s, "/onboarding?status=sponsor_approved&id="+pid).Body.String()
	if strings.Contains(detailAfter, queuePill) || strings.Contains(detailAfter, "is not a registered org") {
		t.Fatalf("detail after registration still carries unregistered markers, body=\n%s", detailAfter)
	}
	if !strings.Contains(detailAfter, "Approve &amp; provision") {
		t.Fatalf("approve button missing after registration, body=\n%s", detailAfter)
	}
	if strings.Contains(detailAfter, disabled) || strings.Contains(detailAfter, approveWhy) {
		t.Fatalf("approve still disabled or guarded after registration, body=\n%s", detailAfter)
	}

	// Admin approval succeeds and marks the proposal approved.
	rr = webForm(t, s, "/onboarding/"+pid+"/approve", url.Values{})
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("approve: got %d body %s", rr.Code, rr.Body.String())
	}
	final, _ := loadProposal(t, s, pid)
	if final.Status != onboardstore.ProposalApproved {
		t.Fatalf("proposal status = %q, want %q after approve", final.Status, onboardstore.ProposalApproved)
	}
}

func stageSponsorApproved(t *testing.T, s *Server, id string) {
	t.Helper()
	ctx := context.Background()
	p, err := s.Onboard.GetProposal(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Onboard.DecideProposal(ctx, id, onboardstore.ProposalSponsorApproved, "sponsor@example.test", p.Note); err != nil {
		t.Fatal(err)
	}
}

func TestOnboardingDetail_ApproveGuardFollowsFlag(t *testing.T) {
	s := newOnboardTestServer(t)
	draftID, regID := parkUnregisteredDraft(t, s)
	stageSponsorApproved(t, s, draftID)
	stageSponsorApproved(t, s, regID)
	const disabled = `disabled aria-disabled="true"`

	for _, armed := range []bool{true, false} {
		s.Cfg.RequireRegisteredOrg = armed
		body := webGet(t, s, "/onboarding?status=sponsor_approved&id="+draftID).Body.String()
		if !strings.Contains(body, "Approve &amp; provision") {
			t.Fatalf("armed=%v: approve button missing", armed)
		}
		if armed {
			if !strings.Contains(body, disabled) || !strings.Contains(body, approveWhy) {
				t.Fatalf("armed: approve must be disabled with the inline reason")
			}
			if !strings.Contains(body, `aria-describedby="ob-approve-why-`+draftID+`"`) {
				t.Fatal("armed: reason not linked to the button")
			}
		} else {
			if strings.Contains(body, disabled) {
				t.Fatal("flag off: approve must stay enabled")
			}
			if !strings.Contains(body, approveWarn) {
				t.Fatal("flag off: warning line missing")
			}
		}
		reg := webGet(t, s, "/onboarding?status=sponsor_approved&id="+regID).Body.String()
		if strings.Contains(reg, disabled) || strings.Contains(reg, approveWarn) || strings.Contains(reg, approveWhy) {
			t.Fatalf("armed=%v: registered org must render neither guard", armed)
		}
	}
}
