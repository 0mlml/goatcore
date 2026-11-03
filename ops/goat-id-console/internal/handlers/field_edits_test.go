package handlers

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/audit"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/config"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/onboardstore"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/orgstore"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/pgtest"
)

// The manifest shape that produced the live failure: the candidate's own name
// slugified into affiliation.id, no nation, no github.
const wrongOrgManifest = `{
  "identity": {"handle":"taylor-example","email":"y@example.test","name":"Taylor Example"},
  "affiliation": {"id":"taylor-example","kind":"vendor"},
  "vendor": {"display_name":"Taylor Example"},
  "zenoh": {"enabled":true,"trial":{"vendor":"taylor-example"}},
  "netbird": {"auto_groups":["DEVELOPER","OBSERVER"]}
}`

func fieldEditServer(t *testing.T, manifest, status string) (*Server, string) {
	t.Helper()

	onboard, err := onboardstore.Open(pgtest.DSN(t))
	if err != nil {
		t.Fatalf("open onboard store: %v", err)
	}
	orgs, err := orgstore.Open(pgtest.DSN(t))
	if err != nil {
		t.Fatalf("open org store: %v", err)
	}
	if _, err := orgs.CreateOrg(orgstore.Org{
		ID: "demoforge", DisplayName: "Demoforge", Kind: "vendor",
		Nation: "UKR", Provenance: "operator-verified", Status: "active",
		CreatedBy: "test",
	}); err != nil {
		t.Fatalf("register org: %v", err)
	}

	p := &onboardstore.Proposal{
		ID: "prop-test-1", Handle: "taylor-example", Email: "y@example.test",
		Env: "zagros", Status: status, ManifestJSON: manifest, Tier: "developer",
		// Sponsor-pinned, so these tests sit off the org-claim axis: a typed org
		// keeps the match panel open even when it resolves (#1086), and the
		// nation-prefill regression below is about the suppressed panel.
		OrgSource: onboardstore.OrgSourceSponsor,
	}
	if err := onboard.CreateProposal(context.Background(), p); err != nil {
		t.Fatalf("insert proposal: %v", err)
	}
	return &Server{Onboard: onboard, Orgs: orgs, Audit: audit.NewStderrEmitter(16)}, p.ID
}

func mustManifest(t *testing.T, s *Server, id string) map[string]any {
	t.Helper()
	p, err := s.Onboard.GetProposal(context.Background(), id)
	if err != nil {
		t.Fatalf("get proposal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(p.ManifestJSON), &m); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	return m
}

// The case that prompted the ADR, end to end: an org with zero similarity to the
// typed value is settable by name, and the nation the candidate left blank comes
// with it.
func TestApplyFieldEdits_fixesWrongOrgAndNation(t *testing.T) {
	s, id := fieldEditServer(t, wrongOrgManifest, onboardstore.ProposalProposed)
	r := httptest.NewRequestWithContext(context.Background(), "POST", "/api/onboard/proposals/"+id+"/fields", nil)

	applied, ferr := s.applyFieldEdits(r, id, map[string]string{
		FieldAffiliationID:  "demoforge",
		FieldAffiliationNat: "UKR",
		FieldOrgDisplayName: "Demoforge",
	})
	if ferr != nil {
		t.Fatalf("edit refused: %v", ferr)
	}
	if len(applied) != 3 {
		t.Fatalf("applied %d edits, want 3: %+v", len(applied), applied)
	}

	m := mustManifest(t, s, id)
	aff := m["affiliation"].(map[string]any)
	if aff["id"] != "demoforge" {
		t.Errorf("affiliation.id = %v, want demoforge", aff["id"])
	}
	if aff["nation"] != "UKR" {
		t.Errorf("affiliation.nation = %v, want UKR (upper-cased)", aff["nation"])
	}
	if dn := m["vendor"].(map[string]any)["display_name"]; dn != "Demoforge" {
		t.Errorf("vendor.display_name = %v, want Demoforge", dn)
	}

	// The URI-SAN authority MUST NOT have followed the org. A shared value 409s
	// every mint after the first, so this is the assertion that matters most.
	tr := m["zenoh"].(map[string]any)["trial"].(map[string]any)
	if tr["vendor"] != "taylor-example" {
		t.Errorf("zenoh.trial.vendor = %v · the per-person URI-SAN authority must not change", tr["vendor"])
	}
	// The applied diff carries before AND after, because the wrong value is
	// evidence about the intake form.
	for _, e := range applied {
		if e.Field == FieldAffiliationID && (e.Before != "taylor-example" || e.After != "demoforge") {
			t.Errorf("diff for %s = %q→%q, want taylor-example→demoforge", e.Field, e.Before, e.After)
		}
	}
}

// An org that is not registered is refused, and the refusal names the remedy ·
// the editor never creates an org (that is the deliberate org-new act).
func TestApplyFieldEdits_unregisteredOrgRefused(t *testing.T) {
	s, id := fieldEditServer(t, wrongOrgManifest, onboardstore.ProposalProposed)
	r := httptest.NewRequestWithContext(context.Background(), "POST", "/x", nil)

	_, ferr := s.applyFieldEdits(r, id, map[string]string{FieldAffiliationID: "skyfortres"}) // typo
	if ferr == nil {
		t.Fatal("a misspelled org must be refused, not created")
	}
	if ferr.Status != 422 {
		t.Errorf("status = %d, want 422", ferr.Status)
	}
	if !strings.Contains(ferr.Message, "not a registered org") {
		t.Errorf("message = %q, want it to say the org is not registered", ferr.Message)
	}
	if got := mustManifest(t, s, id)["affiliation"].(map[string]any)["id"]; got != "taylor-example" {
		t.Errorf("manifest changed on a refused edit: %v", got)
	}
}

// A field outside the closed set refuses the WHOLE request · a partial apply
// would leave the caller guessing which half landed.
func TestApplyFieldEdits_unknownFieldRefusesWholeRequest(t *testing.T) {
	s, id := fieldEditServer(t, wrongOrgManifest, onboardstore.ProposalProposed)
	r := httptest.NewRequestWithContext(context.Background(), "POST", "/x", nil)

	_, ferr := s.applyFieldEdits(r, id, map[string]string{
		FieldAffiliationID: "demoforge",
		"identity.name":    "Someone Else",
	})
	if ferr == nil {
		t.Fatal("an unknown field must be refused")
	}
	if ferr.Status != 400 {
		t.Errorf("status = %d, want 400", ferr.Status)
	}
	if got := mustManifest(t, s, id)["affiliation"].(map[string]any)["id"]; got != "taylor-example" {
		t.Errorf("the valid half of a refused request was applied: %v", got)
	}
}

// The frozen fields refuse with their REASON, not a generic "not allowed" · the
// URI-SAN one especially, since it is the field most likely to be asked for.
func TestApplyFieldEdits_frozenFieldsExplainThemselves(t *testing.T) {
	s, id := fieldEditServer(t, wrongOrgManifest, onboardstore.ProposalProposed)
	r := httptest.NewRequestWithContext(context.Background(), "POST", "/x", nil)

	for field, want := range map[string]string{
		"zenoh.trial.vendor":  "URI-SAN",
		"identity.email":      "dedup",
		"identity.handle":     "peer-label",
		"tier":                "sponsorship-capped",
		"netbird.auto_groups": "authorization",
	} {
		_, ferr := s.applyFieldEdits(r, id, map[string]string{field: "whatever"})
		if ferr == nil {
			t.Errorf("%s must be refused", field)
			continue
		}
		if ferr.Status != 422 {
			t.Errorf("%s status = %d, want 422", field, ferr.Status)
		}
		if !strings.Contains(ferr.Message, want) {
			t.Errorf("%s refusal = %q, want it to mention %q", field, ferr.Message, want)
		}
	}
}

// Past the review stages the manifest is frozen, and the 409 names where the
// change goes instead.
func TestApplyFieldEdits_frozenAfterMint(t *testing.T) {
	for _, status := range []string{
		onboardstore.ProposalApproved,
		onboardstore.ProposalOnboarded,
		onboardstore.ProposalRejected,
	} {
		s, id := fieldEditServer(t, wrongOrgManifest, status)
		r := httptest.NewRequestWithContext(context.Background(), "POST", "/x", nil)

		_, ferr := s.applyFieldEdits(r, id, map[string]string{FieldAffiliationID: "demoforge"})
		if ferr == nil {
			t.Errorf("status %s must refuse the edit", status)
			continue
		}
		if ferr.Status != 409 {
			t.Errorf("status %s → %d, want 409", status, ferr.Status)
		}
		if !strings.Contains(ferr.Message, "goat admin org") {
			t.Errorf("status %s refusal should name the post-mint path, got %q", status, ferr.Message)
		}
	}
}

// sponsor_approved is still editable · the admin reviewing at stage 3 is exactly
// who notices a wrong org.
func TestApplyFieldEdits_editableAtSponsorApproved(t *testing.T) {
	s, id := fieldEditServer(t, wrongOrgManifest, onboardstore.ProposalSponsorApproved)
	r := httptest.NewRequestWithContext(context.Background(), "POST", "/x", nil)

	if _, ferr := s.applyFieldEdits(r, id, map[string]string{FieldAffiliationID: "demoforge"}); ferr != nil {
		t.Fatalf("sponsor_approved must stay editable: %v", ferr)
	}
}

func TestApplyFieldEdits_validation(t *testing.T) {
	s, id := fieldEditServer(t, wrongOrgManifest, onboardstore.ProposalProposed)
	r := httptest.NewRequestWithContext(context.Background(), "POST", "/x", nil)

	// A full country name is the mistake intake already rejects; the
	// editor uses the same regex so a reviewer cannot set what intake refuses.
	if _, ferr := s.applyFieldEdits(r, id, map[string]string{FieldAffiliationNat: "Ukraine"}); ferr == nil {
		t.Error("a full country name must be refused")
	}
	if _, ferr := s.applyFieldEdits(r, id, map[string]string{FieldIdentityGitHub: "not a login!"}); ferr == nil {
		t.Error("a malformed github login must be refused")
	}
	// A leading @ is a paste artifact, not an error.
	if _, ferr := s.applyFieldEdits(r, id, map[string]string{FieldIdentityGitHub: "@TaylorEx"}); ferr != nil {
		t.Errorf("@-prefixed github login should be accepted and stripped: %v", ferr)
	}
	if got := mustManifest(t, s, id)["identity"].(map[string]any)["github"]; got != "TaylorEx" {
		t.Errorf("github = %v, want TaylorEx (@ stripped)", got)
	}
	// Clearing an optional field is a legitimate correction.
	if _, ferr := s.applyFieldEdits(r, id, map[string]string{FieldIdentityGitHub: ""}); ferr != nil {
		t.Errorf("clearing github should be allowed: %v", ferr)
	}
	if _, present := mustManifest(t, s, id)["identity"].(map[string]any)["github"]; present {
		t.Error("github should be absent after being cleared")
	}
	// Clearing the org is NOT: a proposal with no org is unattributable.
	if _, ferr := s.applyFieldEdits(r, id, map[string]string{FieldAffiliationID: ""}); ferr == nil {
		t.Error("clearing affiliation.id must be refused")
	}
	// A no-op is refused rather than logged as a change.
	if _, ferr := s.applyFieldEdits(r, id, map[string]string{FieldOrgDisplayName: "Taylor Example"}); ferr == nil {
		t.Error("an edit that changes nothing must be refused, not audited as a change")
	}
}

// An org id given as an ALIAS resolves to the canonical id · the same
// alias-aware resolve every other consumer uses.
func TestApplyFieldEdits_aliasResolvesToCanonical(t *testing.T) {
	s, id := fieldEditServer(t, wrongOrgManifest, onboardstore.ProposalProposed)
	if err := s.Orgs.AddAlias("sky-fortress-ua", "demoforge", "name-variant", "test"); err != nil {
		t.Fatalf("add alias: %v", err)
	}
	r := httptest.NewRequestWithContext(context.Background(), "POST", "/x", nil)

	if _, ferr := s.applyFieldEdits(r, id, map[string]string{FieldAffiliationID: "sky-fortress-ua"}); ferr != nil {
		t.Fatalf("alias should resolve: %v", ferr)
	}
	if got := mustManifest(t, s, id)["affiliation"].(map[string]any)["id"]; got != "demoforge" {
		t.Errorf("affiliation.id = %v, want the canonical demoforge", got)
	}
}

// The dual-read org block (ADR 1052): a manifest carrying `org` rather than the
// legacy `vendor` must be edited in place, not shadowed by a second block.
func TestApplyFieldEdits_orgBlockDualRead(t *testing.T) {
	manifest := `{"identity":{"handle":"x","email":"x@example.test"},
	  "affiliation":{"id":"taylor-example","kind":"vendor"},
	  "org":{"display_name":"Wrong Name"}}`
	s, id := fieldEditServer(t, manifest, onboardstore.ProposalProposed)
	r := httptest.NewRequestWithContext(context.Background(), "POST", "/x", nil)

	if _, ferr := s.applyFieldEdits(r, id, map[string]string{FieldOrgDisplayName: "Demoforge"}); ferr != nil {
		t.Fatalf("edit refused: %v", ferr)
	}
	m := mustManifest(t, s, id)
	if got := m["org"].(map[string]any)["display_name"]; got != "Demoforge" {
		t.Errorf("org.display_name = %v, want Demoforge", got)
	}
	if _, spurious := m["vendor"]; spurious {
		t.Error("a legacy vendor block must not be invented on an org-shaped manifest")
	}
}

// The audit diff must record what LANDED, not what was typed · an audit row that
// disagrees with the document it describes is worse than none. Regression: the
// first cut recorded "ukr" and "@TaylorEx" while the manifest held "UKR" and
// "TaylorEx".
func TestApplyFieldEdits_diffRecordsStoredValueNotInput(t *testing.T) {
	s, id := fieldEditServer(t, wrongOrgManifest, onboardstore.ProposalProposed)
	r := httptest.NewRequestWithContext(context.Background(), "POST", "/x", nil)

	applied, ferr := s.applyFieldEdits(r, id, map[string]string{
		FieldAffiliationNat: "UKR",
		FieldIdentityGitHub: "@TaylorEx",
	})
	if ferr != nil {
		t.Fatalf("edit refused: %v", ferr)
	}
	got := map[string]string{}
	for _, e := range applied {
		got[e.Field] = e.After
	}
	if got[FieldAffiliationNat] != "UKR" {
		t.Errorf("diff nation = %q, want the stored UKR", got[FieldAffiliationNat])
	}
	if got[FieldIdentityGitHub] != "TaylorEx" {
		t.Errorf("diff github = %q, want the stored TaylorEx", got[FieldIdentityGitHub])
	}
	m := mustManifest(t, s, id)
	if m["affiliation"].(map[string]any)["nation"] != got[FieldAffiliationNat] {
		t.Error("audit diff and manifest disagree on nation")
	}
	if m["identity"].(map[string]any)["github"] != got[FieldIdentityGitHub] {
		t.Error("audit diff and manifest disagree on github")
	}
}

// A country NAME must be refused even though upper-casing it satisfies the
// manifest schema's shared ^[A-Z]{2,8}$ · the editor is deliberately tighter
// than the schema for exactly this input.
func TestApplyFieldEdits_countryNameRefusedNotUppercased(t *testing.T) {
	s, id := fieldEditServer(t, wrongOrgManifest, onboardstore.ProposalProposed)
	r := httptest.NewRequestWithContext(context.Background(), "POST", "/x", nil)

	for _, bad := range []string{"Ukraine", "UKRAINE", "region C", "germany"} {
		_, ferr := s.applyFieldEdits(r, id, map[string]string{FieldAffiliationNat: bad})
		if ferr == nil {
			t.Errorf("%q must be refused · it is a country name, not a code", bad)
			continue
		}
		if !strings.Contains(ferr.Message, "not a code") {
			t.Errorf("%q refusal = %q, want it to name the mistake", bad, ferr.Message)
		}
	}
	// The real shapes still pass.
	for _, ok := range []string{"UKR", "NATO", "EU"} {
		s2, id2 := fieldEditServer(t, wrongOrgManifest, onboardstore.ProposalProposed)
		if _, ferr := s2.applyFieldEdits(r, id2, map[string]string{FieldAffiliationNat: ok}); ferr != nil {
			t.Errorf("%q should be accepted: %v", ok, ferr)
		}
	}
}

// TestHandleFieldEdits_RealCallerShape tests the web form door to the field editor,
// mimicking the exact payload the browser sends: application/x-www-form-urlencoded
// carrying every rendered field, including empty strings for fields left blank.
func TestHandleFieldEdits_RealCallerShape(t *testing.T) {
	s, id := fieldEditServer(t, wrongOrgManifest, onboardstore.ProposalProposed)
	// Set up the CSRF nonce on the server so validateCSRF passes.
	s.csrfNonce = "nonce-123"

	form := url.Values{}
	form.Set("csrf", "nonce-123")
	form.Set("affiliation.id", "demoforge")
	form.Set("affiliation.nation", "UKR")
	form.Set("org.display_name", "Demoforge")
	form.Set("identity.github", "") // Blank github

	r := httptest.NewRequestWithContext(context.Background(), "POST", "/onboarding/"+id+"/fields", strings.NewReader(form.Encode()))
	r.SetPathValue("id", id)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	w := httptest.NewRecorder()
	s.handleFieldEdits(w, r)

	if w.Code != 303 {
		t.Fatalf("handleFieldEdits form post: want 303 redirect, got %d. body: %s", w.Code, w.Body.String())
	}

	// Verify the changes applied
	m := mustManifest(t, s, id)
	if got := m["affiliation"].(map[string]any)["nation"]; got != "UKR" {
		t.Errorf("affiliation.nation = %v, want UKR", got)
	}
}

// Go templates fail at EXECUTE time, so a field typo in the editor form would
// only surface as a broken review page in production. Render it.
func TestTemplates_reviewFieldEditor(t *testing.T) {
	out := renderPage(t, "onboarding", &pageVars{
		CSRFToken:     "nonce-123",
		OnboardStatus: "proposed",
		Proposal: &proposalView{
			ID: "prop-test-1", Handle: "taylor-example", Status: "proposed",
			Editable: true, OrgTyped: "taylor-example",
			OrgNation: "", OrgDisplayName: "Taylor Example", GitHub: "",
		},
	})

	for _, m := range []string{
		"Correct a Field",
		`action="/onboarding/prop-test-1/fields"`,
		`name="affiliation.id"`,
		`name="affiliation.nation"`,
		`name="org.display_name"`,
		`name="identity.github"`,
		`value="nonce-123"`,
		"start typing to search the registry", // the input the attach panel lacks
		"initOrgParentSuggest",                // wired to the same typeahead
		"every change is audited",
		"repo grants skip this person", // why a blank github matters
		// A blank nation used to be explained in a paragraph. It is now shown:
		// the derived panel renders the standing and the ours-vs-foreign answer
		// live, so the operator sees the consequence instead of reading about it.
		"initAffiliationDerive",
	} {
		if !strings.Contains(out, m) {
			t.Errorf("review field editor missing %q", m)
		}
	}
}

// Not editable (post-mint, or a sponsor session) ⇒ the form must not render at
// all rather than render inert.
func TestTemplates_reviewFieldEditorHiddenWhenNotEditable(t *testing.T) {
	out := renderPage(t, "onboarding", &pageVars{
		CSRFToken:     "nonce-123",
		OnboardStatus: "onboarded",
		Proposal: &proposalView{
			ID: "prop-test-1", Handle: "x", Status: "onboarded", Editable: false,
		},
	})
	if strings.Contains(out, "Correct a Field") {
		t.Error("the editor must not render for a non-editable proposal")
	}
	if strings.Contains(out, "/fields") {
		t.Error("the edit endpoint must not be reachable from a non-editable row")
	}
}

// THE REGRESSION. Registering the candidate's org at review suppressed the
// org-match panel · correctly, there is no match left to decide · and the
// nation prefill was sourced from that panel, so the box went empty on a
// proposal whose manifest said USA. Empty is not inert here: the browser door
// posts every rendered box and an empty nation DELETES affiliation.nation, so
// the next Apply correction for an unrelated field would have silently cleared
// it, and a cleared nation resolves coalition standing to `partner`.
//
// fillEditable reads the manifest, so a registered org cannot change what the
// box shows.
func TestFillEditable_nationSurvivesOrgRegistration(t *testing.T) {
	const manifest = `{
	  "identity": {"handle":"user246x","email":"l@example.test","github":"patexample"},
	  "affiliation": {"id":"demoforge","kind":"vendor","nation":"USA"},
	  "org": {"display_name":"Demoforge"}
	}`
	s, id := fieldEditServer(t, manifest, onboardstore.ProposalProposed)
	p, err := s.Onboard.GetProposal(context.Background(), id)
	if err != nil {
		t.Fatalf("get proposal: %v", err)
	}

	// "demoforge" is registered by the harness, so orgMatchesForProposal
	// returns zero facts · exactly the post-create state that blanked the box.
	s.Cfg = &config.Config{OrgMatchEnabled: true}
	if f, _ := s.orgMatchesForProposal(*p); f.Nation != "" {
		t.Fatalf("precondition: expected the match panel to be suppressed for a registered org, got nation %q", f.Nation)
	}

	var v proposalView
	s.fillEditable(&v, *p, loadIntakeHints)
	if v.OrgNation != "USA" {
		t.Errorf("nation prefill = %q, want USA · a blank box silently clears the field on the next Apply", v.OrgNation)
	}
	if v.GitHub != "patexample" || v.OrgDisplayName != "Demoforge" {
		t.Errorf("other prefills regressed: github=%q display=%q", v.GitHub, v.OrgDisplayName)
	}
}

// Every editable field the form RENDERS must be prefilled from the manifest.
// The rule is not tidiness: the browser door cannot tell "untouched" from
// "cleared", so an unprefilled box is a delete waiting for an unrelated edit.
func TestFillEditable_everyRenderedFieldIsPrefilled(t *testing.T) {
	const manifest = `{
	  "identity": {"handle":"user246x","email":"l@example.test","github":"patexample"},
	  "affiliation": {"id":"demoforge","kind":"vendor","nation":"USA"},
	  "org": {"display_name":"Demoforge"}
	}`
	s, id := fieldEditServer(t, manifest, onboardstore.ProposalProposed)
	p, err := s.Onboard.GetProposal(context.Background(), id)
	if err != nil {
		t.Fatalf("get proposal: %v", err)
	}
	s.Cfg = &config.Config{OrgMatchEnabled: true}
	v := proposalToView(*p)
	s.fillEditable(&v, *p, loadIntakeHints)
	v.OrgResolved = s.orgChainForProposal(*p)

	out := renderPage(t, "onboarding", &pageVars{
		CSRFToken: "nonce-123", OnboardStatus: "proposed", Proposal: &v,
	})
	for _, want := range []string{
		`name="affiliation.nation" value="USA"`,
		`name="org.display_name" value="Demoforge"`,
		`name="identity.github"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered form missing prefill %q", want)
		}
	}
	if !strings.Contains(out, "patexample") {
		t.Error("github prefill missing from the rendered form")
	}
	// The harness registers demoforge as UKR while this manifest says USA.
	// A registered org does NOT feed the proposal · nothing copies its nation
	// across, and it is the manifest value that coalition standing and repo
	// grants read · so the divergence has to be visible, not reconciled.
	if !strings.Contains(out, "This box is the one that counts") {
		t.Error("a registered org whose nation differs from the proposal must say so")
	}
}

// Org match OFF, a matcher hiccup, or an org-store read failure: OrgTyped and
// OrgResolved are both empty. Those paths are documented as degrading quietly
// · "never a 500 on the queue" · but the editor composed its org box out of
// them with `index $p.OrgResolved 0`, and index on an empty slice is a
// template execution error, so a quiet degradation took the whole onboarding
// page down. Every box reads from the manifest now; nothing here depends on a
// panel having rendered.
func TestTemplates_editorWithNoOrgPanel(t *testing.T) {
	out := renderPage(t, "onboarding", &pageVars{
		CSRFToken: "n", OnboardStatus: "proposed",
		Proposal: &proposalView{
			ID: "prop-x", Handle: "user246x", Status: "proposed", Editable: true,
			OrgTyped: "", OrgResolved: nil, OrgID: "structure", OrgNation: "USA",
			OrgDisplayName: "Structure", GitHub: "patexample",
		},
	})
	if !strings.Contains(out, "Correct a Field") {
		t.Fatalf("page did not render the editor; out=%.400s", out)
	}
	if !strings.Contains(out, `name="affiliation.nation" value="USA"`) {
		t.Error("nation prefill missing")
	}
	if !strings.Contains(out, `value="structure"`) {
		t.Error("org prefill missing on a page rendered with no org panel · an empty org box 400s every later edit")
	}
}
