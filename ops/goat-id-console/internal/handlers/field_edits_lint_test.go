package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/onboardstore"
)

// A manifest the approve gate accepts, in the shape the console assembles for
// a developer-tier person.
const gateCleanManifest = `{
  "affiliation": {"id":"demoforge","nation":"UKR","relationship":"assigned"},
  "collab": {"enabled":true},
  "identity": {"added":"2026-09-23","added_by":"sponsor@example.test","email":"pat@demoforge.example",
               "handle":"pat-vendor","name":"Pat Vendor","nationality":"UKR","status":"active","type":"person"},
  "machine_quota": 10,
  "netbird": {"auto_groups":["DEVELOPER","OBSERVER"],"enabled":true,"handset_limit":1,"role":"peer",
              "sites":["zagros"],"ttl_days":7,"usage_limit":10},
  "ory_idp": {"enabled":false,"fido2_required":false},
  "ory_site": {"credentials":{"passkey":false,"password":true},"enabled":true,"netbird_mgmt_admin":false,"sites":["zagros"]},
  "vendor": {"display_name":"Demoforge"},
  "zenoh": {"enabled":true,"trial":{"vendor":"pat-vendor"}}
}`

// gateWedgedManifest is the same row with vendor.display_name cleared, as a
// first Apply on the draft could leave it. The schema requires a name, so the
// approve gate refuses it.
func gateWedgedManifest() string {
	return strings.Replace(gateCleanManifest, `"display_name":"Demoforge"`, `"display_name":""`, 1)
}

// An edit that takes a gate-clean manifest to a gate-failing one is refused
// before the write, naming the field. A manifest that already failed is not
// held hostage to the edit, which may be the correction.
func TestLintRegression_refusesTheEditThatWedgedTheApproveGate(t *testing.T) {
	clean, wedged := []byte(gateCleanManifest), []byte(gateWedgedManifest())

	ferr := lintRegression(clean, wedged)
	if ferr == nil || ferr.Status != http.StatusBadRequest {
		t.Fatalf("clean → wedged: want 400, got %v", ferr)
	}
	if !strings.Contains(ferr.Message, "vendor.display_name") {
		t.Errorf("refusal must name the field: %s", ferr.Message)
	}
	if ferr := lintRegression(clean, clean); ferr != nil {
		t.Errorf("clean → clean must pass: %v", ferr)
	}
	if ferr := lintRegression(wedged, wedged); ferr != nil {
		t.Errorf("a manifest that failed before the edit is not the edit's fault: %v", ferr)
	}
	if ferr := lintRegression(wedged, clean); ferr != nil {
		t.Errorf("the correction that fixes a wedged row must pass: %v", ferr)
	}
}

// The schema forbids an empty display name, so "clear" has no valid meaning
// for this field: a blank is refused, not written, on both doors.
func TestApplyFieldEdits_blankDisplayNameRefused(t *testing.T) {
	s, id := fieldEditServer(t, wrongOrgManifest, onboardstore.ProposalProposed)
	r := httptest.NewRequestWithContext(context.Background(), "POST", "/api/onboard/proposals/"+id+"/fields", nil)

	_, ferr := s.applyFieldEdits(r, id, map[string]string{FieldOrgDisplayName: ""})
	if ferr == nil || ferr.Status != http.StatusBadRequest {
		t.Fatalf("blank display name: want 400, got %v", ferr)
	}
	if !strings.Contains(ferr.Message, FieldOrgDisplayName) {
		t.Errorf("refusal must name the field: %s", ferr.Message)
	}
	m := mustManifest(t, s, id)
	if dn := m["vendor"].(map[string]any)["display_name"]; dn != "Taylor Example" {
		t.Errorf("a refused edit must leave the display name alone, got %v", dn)
	}
}

// The editor's promise is "leave a box untouched to leave the field alone",
// and the only thing that keeps it is a prefill from the document Apply will
// write. On an incomplete draft that document does not exist yet, so the boxes
// prefill from what completion would produce, without completing anything.
func TestFillEditable_draftPrefillsWhatCompletionWouldWrite(t *testing.T) {
	s := newOnboardTestServer(t)
	id := storeDraft(t, s, onboardstore.Prebound{Tier: "developer", Sector: "commercial"},
		map[string]any{"name": "Pat Vendor", "email": "pat@demoforge.example", "organization": "Demoforge", "github_username": "patvendor"})
	p, err := s.Onboard.GetProposal(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if p.Complete {
		t.Fatal("fixture must be an incomplete draft")
	}

	v := proposalToView(*p)
	s.fillEditable(&v, *p, loadIntakeHints)

	if v.OrgDisplayName != "Demoforge" {
		t.Errorf("OrgDisplayName = %q, want the organisation completion would write", v.OrgDisplayName)
	}
	if v.GitHub != "patvendor" {
		t.Errorf("GitHub = %q, want the candidate's login", v.GitHub)
	}
	if v.OrgID != "demoforge" {
		t.Errorf("OrgID = %q, want the id completion would derive", v.OrgID)
	}
	after, _ := s.Onboard.GetProposal(context.Background(), id)
	if after.Complete {
		t.Error("prefilling the editor must not persist a completion")
	}
}

// End to end through the browser door: the first Apply on a draft completes it
// and must not clear the display name the completion just wrote. With the boxes
// prefilled from the would-be document, the same click keeps the name and the
// row still clears the approve gate.
func TestHandleFieldEdits_draftApplyKeepsTheDisplayName(t *testing.T) {
	s := newOnboardTestServer(t)
	withOrgs(t, s, true)
	registerOrg(t, s, "demoforge", "commercial", "national", "UKR")
	s.csrfNonce = "nonce-123"
	ctx := context.Background()
	id := storeDraft(t, s, onboardstore.Prebound{Tier: "developer", Sector: "commercial"},
		map[string]any{"name": "Pat Vendor", "email": "pat@demoforge.example", "organization": "Demoforge"})
	p, err := s.Onboard.GetProposal(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	v := proposalToView(*p)
	s.fillEditable(&v, *p, loadIntakeHints)

	// A browser posts every box it rendered, prefilled; the admin's one
	// correction is the nation the candidate left blank.
	if v.OrgID != "demoforge" {
		t.Fatalf("OrgID prefill = %q, want demoforge", v.OrgID)
	}
	form := url.Values{}
	form.Set("csrf", "nonce-123")
	form.Set(FieldAffiliationID, v.OrgID)
	form.Set(FieldAffiliationNat, "UKR")
	form.Set(FieldOrgDisplayName, v.OrgDisplayName)
	form.Set(FieldIdentityGitHub, v.GitHub)
	r := httptest.NewRequestWithContext(ctx, "POST", "/onboarding/"+id+"/fields", strings.NewReader(form.Encode()))
	r.SetPathValue("id", id)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	s.handleFieldEdits(w, r)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("Apply on a draft: want 303, got %d: %s", w.Code, w.Body.String())
	}

	after, _ := s.Onboard.GetProposal(ctx, id)
	if !after.Complete {
		t.Fatalf("Apply must complete the draft, note=%q", after.Note)
	}
	m := mustManifest(t, s, id)
	if dn := orgDisplayName(m); dn != "Demoforge" {
		t.Fatalf("display name after the first Apply on a draft = %q, want Demoforge", dn)
	}
	if nation := m["affiliation"].(map[string]any)["nation"]; nation != "UKR" {
		t.Errorf("affiliation.nation = %v, want the admin's correction UKR", nation)
	}
	if lint := onboardLint([]byte(after.ManifestJSON), "json"); !lint.OK {
		t.Fatalf("the edited proposal must still clear the approve gate: %+v", lint.Findings)
	}
}

// With the boxes prefilled, Apply on a draft with nothing changed is a
// natural click. It must refuse before the completion persists: a 400 that
// has already completed the draft and written its audit row tells the admin
// nothing happened when something did.
func TestHandleFieldEdits_draftNoOpApplyWritesNothing(t *testing.T) {
	s := newOnboardTestServer(t)
	withOrgs(t, s, true)
	registerOrg(t, s, "demoforge", "commercial", "national", "UKR")
	s.csrfNonce = "nonce-123"
	ctx := context.Background()
	id := storeDraft(t, s, onboardstore.Prebound{Tier: "developer", Sector: "commercial"},
		map[string]any{"name": "Pat Vendor", "email": "pat@demoforge.example", "organization": "Demoforge"})
	p, err := s.Onboard.GetProposal(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	v := proposalToView(*p)
	s.fillEditable(&v, *p, loadIntakeHints)

	form := url.Values{}
	form.Set("csrf", "nonce-123")
	form.Set(FieldAffiliationID, v.OrgID)
	form.Set(FieldAffiliationNat, v.OrgNation)
	form.Set(FieldOrgDisplayName, v.OrgDisplayName)
	form.Set(FieldIdentityGitHub, v.GitHub)
	r := httptest.NewRequestWithContext(ctx, "POST", "/onboarding/"+id+"/fields", strings.NewReader(form.Encode()))
	r.SetPathValue("id", id)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	s.handleFieldEdits(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("no-op Apply on a draft: want 400, got %d: %s", w.Code, w.Body.String())
	}

	after, _ := s.Onboard.GetProposal(ctx, id)
	if after.Complete {
		t.Error("a no-op Apply must not complete the draft")
	}
	if n := countDraftCompletionAudits(s, id); n != 0 {
		t.Errorf("a no-op Apply wrote %d onboard_draft_complete audit rows, want 0", n)
	}
}

// The approve gate's re-lint names the field when it refuses a wedged row, so
// the admin can tell a schema bump from a cleared box. The refusal carries the
// findings.
func TestDecide_lintGateRefusalNamesTheFinding(t *testing.T) {
	s := newOnboardTestServer(t)
	h := s.Routes()
	p := &onboardstore.Proposal{
		ID: "prop-wedged", Handle: "pat-vendor", Email: "pat@demoforge.example", Env: "zagros",
		Sponsor: "sponsor@example.test", Status: onboardstore.ProposalSponsorApproved,
		ManifestJSON: gateWedgedManifest(), Complete: true, Tier: "developer",
	}
	if err := s.Onboard.CreateProposal(context.Background(), p); err != nil {
		t.Fatalf("insert proposal: %v", err)
	}

	got := postMachine(t, h, "/api/onboard/proposals/"+p.ID+"/approve", map[string]any{})
	if got.code != http.StatusBadRequest {
		t.Fatalf("approve on a wedged row: want 400, got %d: %s", got.code, got.body)
	}
	if !strings.Contains(got.body, "vendor.display_name") {
		t.Fatalf("the lint-gate refusal must name the failing field, got %s", got.body)
	}
}
