package handlers

// The find box on /onboarding (#1306) rides on a hidden index the page
// renders on every load. These tests pin the rendered index: every stage is
// in it whatever tab is open, each hit opens the right detail pane, a draft
// is findable by the name and GitHub login it submitted, a sponsor's index
// holds only their own candidates, a ?q= on the request is ignored (the term
// lives in the URL fragment and never reaches the server), and an unreadable
// invite table refuses the Invited tab while the other tabs say the index is
// proposals only.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/onboardstore"
)

func getOnboarding(t *testing.T, s *Server, path string, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil)
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, r)
	return w
}

// findIndexBlock cuts the rendered page down to the index container so an
// assertion about "in the index" cannot be satisfied by the stage list.
func findIndexBlock(t *testing.T, body string) string {
	t.Helper()
	start := strings.Index(body, `id="ob-find"`)
	end := strings.Index(body, `class="ob-detail"`)
	if start < 0 || end < 0 || end < start {
		t.Fatalf("index container not found in page:\n%s", body)
	}
	return body[start:end]
}

func TestOnboardingFindIndex_EveryStageOnEveryTab(t *testing.T) {
	s := newOnboardTestServer(t)
	s.Cfg.InviteArchiveAfter = 7 * 24 * time.Hour
	ctx := context.Background()
	future := time.Now().Add(24 * time.Hour).Unix()
	past := time.Now().Add(-48 * time.Hour).Unix()

	var proposalIDs []string
	for _, d := range onboardStageDefs {
		if d.Key == onboardStageInvited {
			continue
		}
		id := "prop-" + d.Key
		seedProposal(t, s, id, "admin@example.org", "developer", d.Key)
		proposalIDs = append(proposalIDs, id)
	}
	open := seedInvite(t, s, "admin@example.org", "Amy, liaison at DEMO-ORG", future, onboardstore.InviteInvited)
	if err := s.Onboard.AddressInvite(ctx, open, "amy@example.org"); err != nil {
		t.Fatal(err)
	}
	expired := seedInvite(t, s, "admin@example.org", "Ben", past, onboardstore.InviteInvited)
	revoked := seedInvite(t, s, "admin@example.org", "Cat", future, onboardstore.InviteRevoked)
	submitted := seedInvite(t, s, "admin@example.org", "Dan", future, onboardstore.InviteSubmitted)
	// An invite that lapsed past the archive window is what the sweep files;
	// the 48-hour one above stays on the working list.
	archived := seedInvite(t, s, "admin@example.org", "Eve", time.Now().Add(-9*24*time.Hour).Unix(), onboardstore.InviteInvited)
	if _, err := s.Onboard.ArchiveStaleInvites(ctx, s.Cfg.InviteArchiveAfter); err != nil {
		t.Fatalf("archive: %v", err)
	}

	for _, path := range []string{"/onboarding", "/onboarding?status=invited", "/onboarding?status=archived"} {
		w := getOnboarding(t, s, path, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s = %d: %s", path, w.Code, w.Body.String())
		}
		idx := findIndexBlock(t, w.Body.String())
		for _, id := range proposalIDs {
			status := strings.TrimPrefix(id, "prop-")
			if !strings.Contains(idx, `href="/onboarding?status=`+status+`&id=`+id+`"`) {
				t.Errorf("%s: index lacks the %s row with its own stage in the link", path, status)
			}
		}
		for _, id := range []string{open, expired, revoked, archived} {
			if !strings.Contains(idx, `href="/onboarding?status=invited&id=`+id+`"`) {
				t.Errorf("%s: index lacks invite %s under the Invited stage", path, id)
			}
		}
		if strings.Contains(idx, `href="/onboarding?status=invited&id=`+submitted+`"`) {
			t.Errorf("%s: a submitted invite is in the index; its proposal row represents it", path)
		}
		for _, want := range []string{"amy@example.org", "Amy, liaison at DEMO-ORG", `<span class="ob-badge pet">expired</span>`, `<span class="ob-badge pet">revoked</span>`, `<span class="ob-badge pet">archived</span>`, `<span class="ob-badge stage">Awaiting admin</span>`} {
			if !strings.Contains(idx, want) {
				t.Errorf("%s: index lacks %q", path, want)
			}
		}
		if !strings.Contains(w.Body.String(), `id="candidate-filter"`) || !strings.Contains(w.Body.String(), "/static/onboard-find.js") {
			t.Errorf("%s: find box or script missing", path)
		}
	}
}

func TestOnboardingFindIndex_FilterCarriesManifestNameAndGitHub(t *testing.T) {
	s := newOnboardTestServer(t)
	p := &onboardstore.Proposal{
		ID: "prop-manifest", Sponsor: "admin@example.org", Env: "zagros", Handle: "jane-doe",
		Email: "jane@example.org", Status: onboardstore.ProposalEnrolled, Tier: "developer", Complete: true,
		ManifestJSON: `{"identity":{"name":"Jane Doe","github":"jane-doe-gh","email":"jane@example.org"}}`,
	}
	if err := s.Onboard.CreateProposal(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	idx := findIndexBlock(t, getOnboarding(t, s, "/onboarding", nil).Body.String())
	want := `data-filter="prop-manifest jane@example.org Jane Doe jane-doe jane-doe-gh"`
	if !strings.Contains(idx, want) {
		t.Errorf("index lacks %s\n%s", want, idx)
	}
	if !strings.Contains(idx, `<div class="h">Jane Doe</div>`) {
		t.Errorf("the manifest name is not the row title")
	}
}

func TestOnboardingFindIndex_SponsorSeesOwnOnly(t *testing.T) {
	s := newOnboardTestServer(t)
	s.Cfg.DevBypassAuth = false
	portalURL, _ := fakeSponsorPortal(t, nil, map[string]sponsorLookupResponse{
		"sponsora@example.org": {Org: "0ff404dd", Alias: "pat-example", SponsorAuthorized: true, SponsorMaxTier: "developer"},
	})
	s.Cfg.PortalInternalURL = portalURL
	s.Cfg.PortalLookupToken = "svc-token"
	future := time.Now().Add(24 * time.Hour).Unix()

	seedProposal(t, s, "prop-own-1", "sponsora@example.org", "developer", onboardstore.ProposalProposed)
	seedProposal(t, s, "prop-own-2", "0ff404dd", "developer", onboardstore.ProposalArchived)
	seedProposal(t, s, "prop-own-3", "pat-example", "developer", onboardstore.ProposalFailed)
	seedProposal(t, s, "prop-foreign-1", "someone-else@example.org", "developer", onboardstore.ProposalProposed)
	seedProposal(t, s, "prop-foreign-2", "other-prefix", "developer", onboardstore.ProposalApproved)
	ownInv := seedInvite(t, s, "sponsora@example.org", "own invitee", future, onboardstore.InviteInvited)
	foreignInv := seedInvite(t, s, "someone-else@example.org", "foreign invitee", future, onboardstore.InviteInvited)

	sponsor := map[string]string{"X-Forwarded-Email": "sponsora@example.org", "X-Forwarded-Groups": "DEVELOPER,OBSERVER"}
	w := getOnboarding(t, s, "/onboarding", sponsor)
	if w.Code != http.StatusOK {
		t.Fatalf("sponsor GET = %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, own := range []string{"prop-own-1", "prop-own-2", "prop-own-3", ownInv} {
		if !strings.Contains(findIndexBlock(t, body), own) {
			t.Errorf("sponsor index lacks own row %s", own)
		}
	}
	for _, foreign := range []string{"prop-foreign-1", "prop-foreign-2", foreignInv, "foreign invitee"} {
		if strings.Contains(body, foreign) {
			t.Errorf("foreign row %q reached a sponsor's page (hidden markup included)", foreign)
		}
	}
	if strings.Contains(strings.ToLower(body), "hidden candidate") || strings.Contains(strings.ToLower(body), "candidates hidden") {
		t.Error("the page hints at rows it withheld")
	}
	// The detail pane keeps refusing a foreign id outright.
	if w := getOnboarding(t, s, "/onboarding?status=approved&id=prop-foreign-2", sponsor); w.Code != http.StatusNotFound {
		t.Errorf("foreign detail = %d, want 404", w.Code)
	}

	// An admin's index holds everyone.
	admin := map[string]string{"X-Forwarded-Email": "admin@example.org", "X-Forwarded-Groups": "ADMINS"}
	idx := findIndexBlock(t, getOnboarding(t, s, "/onboarding", admin).Body.String())
	for _, id := range []string{"prop-own-1", "prop-foreign-1", "prop-foreign-2", ownInv, foreignInv} {
		if !strings.Contains(idx, id) {
			t.Errorf("admin index lacks %s", id)
		}
	}
}

// A draft (unregistered org, incomplete intake) still holds the intake record
// rather than an assembled manifest, so its name and GitHub login sit under
// candidate_submission. The issue asks for both across every stage, drafts
// included. Seeded through the real AssembleIntakeRecord so the stored shape
// is the one the submit path writes.
func TestOnboardingFindIndex_DraftSearchableByNameAndGitHub(t *testing.T) {
	s := newOnboardTestServer(t)
	id := storeDraft(t, s, onboardstore.Prebound{Tier: "developer", Sector: "commercial"},
		map[string]any{"name": "Alice Smith", "email": "alice.smith@example.org", "github_username": "alice-gh", "organization": "Demoforge"})
	idx := findIndexBlock(t, getOnboarding(t, s, "/onboarding", nil).Body.String())
	want := `data-filter="` + id + ` alice.smith@example.org Alice Smith alice-smith alice-gh"`
	if !strings.Contains(idx, want) {
		t.Errorf("index lacks %s\n%s", want, idx)
	}
	if !strings.Contains(idx, `<div class="h">Alice Smith</div>`) {
		t.Errorf("the draft's own name is not the row title")
	}
}

// The typed term travels in the URL fragment, which never reaches the server.
// A ?q= that does arrive (an old link, a hand-edited URL) is ignored: not a
// filter, not a prefill, not echoed anywhere in the page.
func TestOnboardingFindIndex_QueryStringIgnored(t *testing.T) {
	s := newOnboardTestServer(t)
	seedProposal(t, s, "prop-1", "admin@example.org", "developer", onboardstore.ProposalProposed)

	w := getOnboarding(t, s, "/onboarding?q="+url.QueryEscape("<b>zz-not-a-candidate-zz"), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("GET = %d", w.Code)
	}
	body := w.Body.String()
	if strings.Contains(body, "zz-not-a-candidate-zz") {
		t.Errorf("a ?q= term was reflected into the page")
	}
	if !strings.Contains(body, `<input id="candidate-filter" type="search" autocomplete="off"`) {
		t.Errorf("the find box carries a server-side value attribute")
	}
	if !strings.Contains(findIndexBlock(t, body), "prop-1") {
		t.Errorf("a ?q= removed rows from the index")
	}
}

// When the invite table cannot be read, the Invited tab refuses (502) rather
// than rendering an empty funnel, and every proposal tab still renders with
// the index covering proposals only and saying so. Removing either guard
// used to leave the package green.
func TestOnboarding_InvitesUnavailable(t *testing.T) {
	s := newOnboardTestServer(t)
	seedProposal(t, s, "prop-1", "admin@example.org", "developer", onboardstore.ProposalProposed)
	if _, err := s.Onboard.DB().Exec(`DROP TABLE onboard_invites`); err != nil {
		t.Fatalf("drop invites: %v", err)
	}

	if w := getOnboarding(t, s, "/onboarding?status=invited", nil); w.Code != http.StatusBadGateway {
		t.Errorf("Invited tab without its table = %d, want 502", w.Code)
	}

	w := getOnboarding(t, s, "/onboarding?status=proposed", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("proposal tab without invites = %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "Invites could not be read") {
		t.Errorf("the page does not say the index is proposals only")
	}
	if !strings.Contains(findIndexBlock(t, body), "prop-1") {
		t.Errorf("the proposal rows dropped out of the index with the invites")
	}
}

// Template-only renders: the block reads its fields by name, so a typo in the
// template would otherwise surface only on a live page.
func TestTemplates_onboardingFindBlock(t *testing.T) {
	out := renderPage(t, "onboarding", &pageVars{
		CSRFToken:     "nonce-123",
		OnboardStatus: "proposed",
		OnboardFindRows: []onboardFindRow{
			{ID: "prop-1", Stage: "enrolled", StageLabel: "Enrolled", Title: "Jane Doe", Email: "jane@example.org", Handle: "jane-doe", Sponsor: "Sponsor One", Env: "zagros", ChangedOn: "2026-09-21", Filter: "prop-1 jane@example.org Jane Doe jane-doe"},
			{ID: "inv-1", Stage: "invited", StageLabel: "Invited", StateLabel: "expired", Title: "ben@example.org", Email: "ben@example.org", Sponsor: "Sponsor One", Env: "zagros", Filter: "inv-1 ben@example.org"},
		},
		OnboardFindTruncated: true,
		OnboardFindPartial:   true,
	})
	for _, want := range []string{
		`<input id="candidate-filter" type="search" autocomplete="off"`, `id="ob-find" hidden`, `id="ob-stage"`,
		`href="/onboarding?status=enrolled&id=prop-1"`, `href="/onboarding?status=invited&id=inv-1"`,
		`<span class="ob-badge stage">Enrolled</span>`, `<span class="ob-badge pet">expired</span>`,
		"The index holds the 2 most recently changed candidates", "Invites could not be read",
		`class="ob-empty ob-find-none" hidden`, "/static/onboard-find.js",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("onboarding page lacks %q", want)
		}
	}
	empty := renderPage(t, "onboarding", &pageVars{CSRFToken: "n", OnboardStatus: "proposed"})
	if !strings.Contains(empty, `id="ob-find" hidden`) || strings.Contains(empty, "The index holds") {
		t.Error("an empty index must still render the container and no truncation note")
	}
}
