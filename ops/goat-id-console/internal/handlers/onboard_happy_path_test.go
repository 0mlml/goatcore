package handlers

// onboard_happy_path_test.go locks the post-ADR 1151 intake happy path.
//
// The console mint form and the intake page stopped asking for
// affiliation.kind, so an invite minted from the console reaches the assembler
// with no kind. The assembler used to treat that as an incomplete draft and
// both decision gates refused it, which is why no console-minted invite could
// reach the runner. These tests pin the derived-kind behaviour (ADR 1151 step
// 3), the draft-completion path for the rows the old rule stranded, the
// sponsor-prebound sector, and the tier dead end.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/onboardstore"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/orgstore"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/pgtest"
)

// mintPrebound mints an invite straight into the store with exactly the given
// prebound set, so a test controls what the sponsor did and did not fix.
func mintPrebound(t *testing.T, s *Server, pb onboardstore.Prebound) string {
	t.Helper()
	pbJSON, _ := json.Marshal(pb)
	inv := &onboardstore.Invite{Sponsor: "dev@bypass.local", Env: "zagros", PreboundJSON: string(pbJSON)}
	tok, err := s.Onboard.MintInvite(context.Background(), inv)
	if err != nil {
		t.Fatalf("mint invite: %v", err)
	}
	return tok
}

// withOrgRegistry attaches a fresh org registry holding the given orgs.
func withOrgRegistry(t *testing.T, s *Server, orgs ...orgstore.Org) {
	t.Helper()
	reg, err := orgstore.Open(pgtest.DSN(t))
	if err != nil {
		t.Fatalf("open orgstore: %v", err)
	}
	t.Cleanup(func() { _ = reg.Close() })
	for _, o := range orgs {
		if o.Provenance == "" {
			o.Provenance = "operator-verified"
		}
		if o.Status == "" {
			o.Status = "active"
		}
		if o.CreatedBy == "" {
			o.CreatedBy = "test"
		}
		if _, err := reg.CreateOrg(o); err != nil {
			t.Fatalf("register org %s: %v", o.ID, err)
		}
	}
	s.Orgs = reg
}

// loadProposal returns the stored row and its decoded manifest.
func loadProposal(t *testing.T, s *Server, id string) (*onboardstore.Proposal, map[string]any) {
	t.Helper()
	p, err := s.Onboard.GetProposal(context.Background(), id)
	if err != nil {
		t.Fatalf("get proposal %s: %v", id, err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(p.ManifestJSON), &m); err != nil {
		t.Fatalf("manifest of %s is not JSON: %v", id, err)
	}
	return p, m
}

func affField(m map[string]any, key string) (string, bool) {
	aff, _ := m["affiliation"].(map[string]any)
	v, ok := aff[key].(string)
	return v, ok
}

// storeDraft stores a minimal-burden draft in the older submit-path shape: the
// candidate-intake/v-draft record in manifest_json and complete=0. Stores can
// still hold rows of this shape.
func storeDraft(t *testing.T, s *Server, pb onboardstore.Prebound, sub map[string]any) string {
	t.Helper()
	pbJSON, _ := json.Marshal(pb)
	subJSON, _ := json.Marshal(sub)
	inv := &onboardstore.Invite{ID: "inv-draft-" + pb.Tier, Sponsor: "dev@bypass.local", Env: "zagros", PreboundJSON: string(pbJSON)}
	rec, handle, email, err := onboardstore.AssembleIntakeRecord(inv, subJSON)
	if err != nil {
		t.Fatalf("assemble intake record: %v", err)
	}
	p := &onboardstore.Proposal{
		Sponsor: inv.Sponsor, Env: inv.Env, Handle: handle, Email: email,
		Status: onboardstore.ProposalProposed, ManifestJSON: string(rec), Complete: false,
		Tier: pb.Tier,
		// The submit path records where affiliation.id came from; a fabricated
		// draft carries the same provenance, or the review gate reads a
		// sponsor-pinned org as the candidate's own claim.
		OrgSource: orgSourceForPrebound(pb.Org),
		Note:      "via invite " + inv.ID + " — INCOMPLETE (affiliation); admin to complete",
	}
	if err := s.Onboard.CreateProposal(context.Background(), p); err != nil {
		t.Fatalf("create draft: %v", err)
	}
	return p.ID
}

// A console-minted invite (tier + sector prebound, no kind) must produce a
// complete proposal whose kind is derived from the sector, and that proposal
// must clear both decision gates.
func TestSubmit_NoPreboundKind_DerivesVendorFromCommercialSector(t *testing.T) {
	s := newOnboardTestServer(t)
	h := s.Routes()

	tok := mintPrebound(t, s, onboardstore.Prebound{Tier: "developer", Sector: "commercial"})
	pid := submit(t, h, tok, map[string]any{"name": "Pat Vendor", "email": "pat@demoforge.example", "organization": "Demoforge"})

	p, m := loadProposal(t, s, pid)
	if !p.Complete {
		t.Fatalf("proposal must be complete, note=%q", p.Note)
	}
	if kind, _ := affField(m, "kind"); kind != "vendor" {
		t.Fatalf("affiliation.kind = %q, want vendor (derived from sector=commercial)", kind)
	}
	if sector, _ := affField(m, "sector"); sector != "commercial" {
		t.Fatalf("affiliation.sector = %q, want commercial", sector)
	}
	if got := decideMachine(t, h, pid, "sponsor-approve"); got != onboardstore.ProposalSponsorApproved {
		t.Fatalf("sponsor-approve: got %q", got)
	}
	if got := decideMachine(t, h, pid, "approve"); got != onboardstore.ProposalApproved {
		t.Fatalf("approve: got %q", got)
	}
}

// A state-sector org derives organic or partner from the REGISTRY's nation
// against the operating sovereign (ADR 1171: the org's nation is a registry
// fact, never the candidate's answer), and the manifest carries that nation.
func TestSubmit_NoPreboundKind_StateSectorDerivesFromRegisteredOrgNation(t *testing.T) {
	cases := []struct {
		name, org, wantKind, wantNation string
	}{
		{"own nation is organic", "usareur-af", "organic", "USA"},
		{"allied nation is partner", "lva-nag", "partner", "LVA"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newOnboardTestServer(t)
			s.Cfg.OperatingSovereign = "USA"
			withOrgRegistry(t, s,
				orgstore.Org{ID: "usareur-af", DisplayName: "USAREUR-AF", Kind: "organic", Nation: "USA"},
				orgstore.Org{ID: "lva-nag", DisplayName: "National Team LVA", Kind: "partner", Nation: "LVA"},
			)
			h := s.Routes()

			tok := mintPrebound(t, s, onboardstore.Prebound{Tier: "browse", Org: c.org})
			pid := submit(t, h, tok, map[string]any{
				"name": "Sam State", "email": "sam@" + c.org + ".example",
				"sector": "state", "gov_branch": "military",
			})
			p, m := loadProposal(t, s, pid)
			if !p.Complete {
				t.Fatalf("proposal must be complete, note=%q", p.Note)
			}
			if kind, _ := affField(m, "kind"); kind != c.wantKind {
				t.Fatalf("affiliation.kind = %q, want %s", kind, c.wantKind)
			}
			if nation, _ := affField(m, "nation"); nation != c.wantNation {
				t.Fatalf("affiliation.nation = %q, want %s (from the org registry)", nation, c.wantNation)
			}
			if id, _ := affField(m, "id"); id != c.org {
				t.Fatalf("affiliation.id = %q, want %s", id, c.org)
			}
			if b, _ := affField(m, "gov_branch"); b != "military" {
				t.Fatalf("affiliation.gov_branch = %q, want military", b)
			}
		})
	}
}

// When nothing can decide the kind (no sector, or a state sector with no known
// nation) the manifest simply omits it. The schema no longer requires kind, so
// the proposal is still complete and still passes both gates and the lint.
func TestSubmit_KindUnderivable_StillCompleteAndApprovable(t *testing.T) {
	cases := []struct {
		name string
		sub  map[string]any
	}{
		{"no sector declared", map[string]any{"name": "Quiet Person", "email": "quiet@example.test", "organization": "Quiet Org"}},
		{"state sector, nation unknown", map[string]any{"name": "State Person", "email": "state@example.test", "organization": "State Org", "sector": "state"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newOnboardTestServer(t)
			h := s.Routes()
			tok := mintPrebound(t, s, onboardstore.Prebound{Tier: "browse"})
			pid := submit(t, h, tok, c.sub)
			p, m := loadProposal(t, s, pid)
			if !p.Complete {
				t.Fatalf("proposal must be complete, note=%q", p.Note)
			}
			if kind, set := affField(m, "kind"); set {
				t.Fatalf("affiliation.kind = %q, want absent when underivable", kind)
			}
			if got := decideMachine(t, h, pid, "sponsor-approve"); got != onboardstore.ProposalSponsorApproved {
				t.Fatalf("sponsor-approve: got %q", got)
			}
			if got := decideMachine(t, h, pid, "approve"); got != onboardstore.ProposalApproved {
				t.Fatalf("approve: got %q", got)
			}
		})
	}
}

// An explicit kind still wins over derivation (ADR 1151: explicit beats derived).
func TestSubmit_ExplicitPreboundKindBeatsDerivation(t *testing.T) {
	s := newOnboardTestServer(t)
	h := s.Routes()
	tok := mintPrebound(t, s, onboardstore.Prebound{Tier: "browse", Affiliation: "partner", Sector: "commercial"})
	pid := submit(t, h, tok, map[string]any{"name": "Explicit Person", "email": "explicit@example.test", "organization": "Explicit Org"})
	_, m := loadProposal(t, s, pid)
	if kind, _ := affField(m, "kind"); kind != "partner" {
		t.Fatalf("affiliation.kind = %q, want the explicit partner", kind)
	}
}

// A sponsor-prebound sector is a locked field like tier: the preview reports
// it, the manifest carries it, and a different candidate answer becomes a
// petition rather than the effective value.
func TestSubmit_PreboundSectorIsLocked(t *testing.T) {
	s := newOnboardTestServer(t)
	h := s.Routes()
	tok := mintPrebound(t, s, onboardstore.Prebound{Tier: "developer", Sector: "state"})

	pv := preview(t, h, tok)
	if !pv.SectorLocked {
		t.Fatalf("preview: sector_locked must be true when the sponsor prebound a sector")
	}
	pid := submit(t, h, tok, map[string]any{"name": "Pet Person", "email": "pet@example.test", "organization": "Pet Org", "sector": "commercial"})
	p, m := loadProposal(t, s, pid)
	if sector, _ := affField(m, "sector"); sector != "state" {
		t.Fatalf("affiliation.sector = %q, want the sponsor's state", sector)
	}
	var pets []petition
	if err := json.Unmarshal([]byte(p.Petitions), &pets); err != nil {
		t.Fatalf("petitions not JSON: %v", err)
	}
	found := false
	for _, pt := range pets {
		if pt.Field == "sector" && pt.SponsorValue == "state" && pt.RequestedValue == "commercial" {
			found = true
		}
	}
	if !found {
		t.Fatalf("want a sector petition state->commercial, got %s", p.Petitions)
	}
}

// Sponsor left the tier open and the candidate chose none: nobody can set a
// tier afterwards (ADR 1137 froze it at review), so the submit is refused with
// a message the candidate can act on, and the link stays usable.
func TestSubmit_BlankTierRefusedAndLinkSurvives(t *testing.T) {
	s := newOnboardTestServer(t)
	h := s.Routes()
	tok := mintPrebound(t, s, onboardstore.Prebound{})

	body, _ := json.Marshal(map[string]any{"token": tok, "submission": map[string]any{"name": "No Tier", "email": "notier@example.test", "organization": "Tier Org", "nationality": "CAN"}})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/public/onboard/submit", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("blank tier: want 422, got %d: %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Error struct {
			Code   string            `json:"code"`
			Fields map[string]string `json:"fields"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode validation_failed body: %v", err)
	}
	if out.Error.Code != "validation_failed" {
		t.Fatalf("blank tier: want error.code=validation_failed, got %q", out.Error.Code)
	}
	if got := out.Error.Fields["tier"]; got != errTierRequired.Error() {
		t.Fatalf("blank tier: fields.tier = %q, want %q", got, errTierRequired.Error())
	}
	// Same link, tier chosen (with organisation + nationality present): accepted.
	if pid := submit(t, h, tok, map[string]any{"name": "No Tier", "email": "notier@example.test", "organization": "Tier Org", "nationality": "CAN", "tier": "browse"}); pid == "" {
		t.Fatal("submit with tier after refusal: empty proposal id")
	}
}

// A draft the old rule stranded (kind blank, everything else present) completes
// itself at the sponsor gate under the current rules and then advances.
func TestDecide_StrandedDraftCompletesAtSponsorApprove(t *testing.T) {
	s := newOnboardTestServer(t)
	h := s.Routes()
	id := storeDraft(t, s, onboardstore.Prebound{Tier: "developer"},
		map[string]any{"name": "Stranded Person", "email": "stranded@example.test", "organization": "Draft Org", "sector": "commercial"})

	if got := decideMachine(t, h, id, "sponsor-approve"); got != onboardstore.ProposalSponsorApproved {
		t.Fatalf("sponsor-approve: got %q", got)
	}
	p, m := loadProposal(t, s, id)
	if !p.Complete {
		t.Fatalf("draft must be complete after sponsor-approve, note=%q", p.Note)
	}
	if kind, _ := affField(m, "kind"); kind != "vendor" {
		t.Fatalf("affiliation.kind = %q, want vendor", kind)
	}
	if handle, _ := m["identity"].(map[string]any)["handle"].(string); handle == "" {
		t.Fatalf("assembled manifest has no identity.handle: %s", p.ManifestJSON)
	}
	if strings.Contains(p.Note, "INCOMPLETE") {
		t.Fatalf("note still says INCOMPLETE after completion: %q", p.Note)
	}
	if got := decideMachine(t, h, id, "approve"); got != onboardstore.ProposalApproved {
		t.Fatalf("approve: got %q", got)
	}
}

// A draft with no tier anywhere cannot be completed by anyone; the gate says
// what is missing instead of pointing at a console path that does not exist.
func TestDecide_TierlessDraftStaysBlockedAndNamesTheGap(t *testing.T) {
	s := newOnboardTestServer(t)
	h := s.Routes()
	id := storeDraft(t, s, onboardstore.Prebound{}, map[string]any{"name": "Tierless", "email": "tierless@example.test"})

	got := postMachine(t, h, "/api/onboard/proposals/"+id+"/sponsor-approve", map[string]any{})
	if got.code != http.StatusBadRequest {
		t.Fatalf("tierless sponsor-approve: want 400, got %d: %s", got.code, got.body)
	}
	if !strings.Contains(got.body, "tier") {
		t.Fatalf("refusal must name the missing tier, got %s", got.body)
	}
	if p, _ := loadProposal(t, s, id); p.Complete {
		t.Fatal("tierless draft must stay incomplete")
	}
}

// The boot sweep completes what it can and reports what it cannot, so the
// queue's draft flags are honest without waiting for someone to click.
func TestCompleteDrafts_SweepCompletesWhatItCan(t *testing.T) {
	s := newOnboardTestServer(t)
	good := storeDraft(t, s, onboardstore.Prebound{Tier: "browse"},
		map[string]any{"name": "Good Draft", "email": "good@example.test", "organization": "Draft Org", "sector": "civil-society"})
	stuck := storeDraft(t, s, onboardstore.Prebound{}, map[string]any{"name": "Stuck Draft", "email": "stuck@example.test"})

	done, left, err := s.CompleteDrafts(context.Background())
	if err != nil {
		t.Fatalf("CompleteDrafts: %v", err)
	}
	if done != 1 || left != 1 {
		t.Fatalf("CompleteDrafts = (%d done, %d left), want (1, 1)", done, left)
	}
	if p, m := loadProposal(t, s, good); !p.Complete {
		t.Fatalf("good draft not completed: %q", p.Note)
	} else if kind, _ := affField(m, "kind"); kind != "civil" {
		t.Fatalf("good draft kind = %q, want civil", kind)
	}
	if p, _ := loadProposal(t, s, stuck); p.Complete {
		t.Fatal("stuck draft must stay incomplete")
	}
}

// A field edit on a completable draft completes it first, so the edit lands on
// a manifest instead of being written into the intake record and lost.
func TestFieldEdit_OnStrandedDraftCompletesFirst(t *testing.T) {
	s := newOnboardTestServer(t)
	id := storeDraft(t, s, onboardstore.Prebound{Tier: "developer"},
		map[string]any{"name": "Edit Draft", "email": "edit@example.test", "organization": "Draft Org", "sector": "commercial"})

	r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/onboard/proposals/"+id+"/fields", nil)
	if _, ferr := s.applyFieldEdits(r, id, map[string]string{FieldIdentityGitHub: "edit-person"}); ferr != nil {
		t.Fatalf("field edit on completable draft refused: %v", ferr)
	}
	p, m := loadProposal(t, s, id)
	if !p.Complete {
		t.Fatalf("draft must be complete after the edit, note=%q", p.Note)
	}
	if gh, _ := m["identity"].(map[string]any)["github"].(string); gh != "edit-person" {
		t.Fatalf("identity.github = %q, want edit-person", gh)
	}
}

// A draft that cannot complete refuses the edit and says why, rather than
// silently editing the intake record.
func TestFieldEdit_OnTierlessDraftRefusesWithReason(t *testing.T) {
	s := newOnboardTestServer(t)
	id := storeDraft(t, s, onboardstore.Prebound{}, map[string]any{"name": "Tierless Edit", "email": "tedit@example.test"})

	r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/onboard/proposals/"+id+"/fields", nil)
	_, ferr := s.applyFieldEdits(r, id, map[string]string{FieldIdentityGitHub: "someone"})
	if ferr == nil {
		t.Fatal("edit on an uncompletable draft must be refused")
	}
	if ferr.Status != http.StatusConflict || !strings.Contains(ferr.Message, "tier") {
		t.Fatalf("want 409 naming the missing tier, got %d %q", ferr.Status, ferr.Message)
	}
}

func TestSubmit_NonLatinOrganizationProducesStableAffiliationID(t *testing.T) {
	s := newOnboardTestServer(t)
	h := s.Routes()

	// Submission 1
	tok1 := mintTokenViaStore(t, s)
	person1 := map[string]any{"name": "user903x", "email": "user903x@example.com", "organization": "Збройні сили", "nationality": "UKR", "tier": "browse"}
	pid1 := submit(t, h, tok1, person1)
	if pid1 == "" {
		t.Fatal("first submission failed")
	}

	// Submission 2
	tok2 := mintTokenViaStore(t, s)
	person2 := map[string]any{"name": "Petro", "email": "petro@example.com", "organization": "Збройні сили", "nationality": "UKR", "tier": "browse"}
	pid2 := submit(t, h, tok2, person2)
	if pid2 == "" {
		t.Fatal("second submission failed")
	}

	p1, _ := s.Onboard.GetProposal(context.Background(), pid1)
	p2, _ := s.Onboard.GetProposal(context.Background(), pid2)

	var m1, m2 map[string]any
	if err := json.Unmarshal([]byte(p1.ManifestJSON), &m1); err != nil {
		t.Fatalf("unmarshal manifest for first submission: %v", err)
	}
	if err := json.Unmarshal([]byte(p2.ManifestJSON), &m2); err != nil {
		t.Fatalf("unmarshal manifest for second submission: %v", err)
	}

	if aff1 := m1["affiliation"].(map[string]any)["id"].(string); aff1 == "" || aff1 != m2["affiliation"].(map[string]any)["id"].(string) {
		t.Errorf("expected non-empty identical affiliation IDs, got %q and %q", aff1, m2["affiliation"].(map[string]any)["id"].(string))
	}
}
