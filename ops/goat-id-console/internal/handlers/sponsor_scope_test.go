package handlers

// Sponsor-scoped onboarding (ADR 1072): non-escalated sponsors — participant
// keys on the machine realm, portal-bound logins on the human realm — see and
// vouch only their own invitees at their annotated tier cap; admin-stage
// actions stay operator/ADMINS-only.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/onboardstore"
)

// sponsorPortalRoundTripper serves the two read-only portal surfaces the
// console delegates to: GET /api/whoami (participant keys) and
// GET /api/orgs/sponsorship (login-email lookup).
type sponsorPortalRoundTripper struct {
	keys   map[string]portalWhoami
	emails map[string]sponsorLookupResponse
	down   bool
}

func (rt *sponsorPortalRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if rt.down {
		return nil, errors.New("portal down")
	}
	w := httptest.NewRecorder()
	w.Header().Set("Content-Type", "application/json")
	switch {
	case req.Method == http.MethodGet && strings.HasSuffix(req.URL.Path, "/api/whoami"):
		key := strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer ")
		wh, ok := rt.keys[strings.TrimSpace(key)]
		if !ok {
			w.WriteHeader(http.StatusUnauthorized)
		} else {
			_ = json.NewEncoder(w).Encode(wh)
		}
	case req.Method == http.MethodGet && strings.HasSuffix(req.URL.Path, "/api/orgs/sponsorship"):
		lr, ok := rt.emails[req.URL.Query().Get("email")]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
		} else {
			_ = json.NewEncoder(w).Encode(lr)
		}
	default:
		w.WriteHeader(http.StatusNotFound)
	}
	return w.Result(), nil
}

// fakeSponsorPortal configures onboardHTTP with a stubbed portal client and
// returns the portal base URL plus the RoundTripper for tests that need to
// flip it to "down".
func fakeSponsorPortal(t *testing.T, keys map[string]sponsorLookupResponse, emails map[string]sponsorLookupResponse) (string, *sponsorPortalRoundTripper) {
	t.Helper()
	keysPortal := map[string]portalWhoami{}
	for k, v := range keys {
		keysPortal[k] = portalWhoami{
			Org:               v.Org,
			Alias:             v.Alias,
			SponsorAuthorized: v.SponsorAuthorized,
			SponsorMaxTier:    v.SponsorMaxTier,
		}
	}
	rt := &sponsorPortalRoundTripper{keys: keysPortal, emails: emails}
	prev := onboardHTTP
	onboardHTTP = &http.Client{Transport: rt}
	t.Cleanup(func() { onboardHTTP = prev })
	portalURL := "https://portal.example.test"
	return portalURL, rt
}

// seedProposal inserts a proposal row directly (the scoping tests need rows
// under several sponsors without walking the whole invite flow each time).
func seedProposal(t *testing.T, s *Server, id, sponsor, tier, status string) {
	t.Helper()
	p := &onboardstore.Proposal{
		ID: id, Sponsor: sponsor, Env: "zagros", Handle: "cand-" + id,
		Email: id + "@example.org", Status: status, ManifestJSON: "{}", Tier: tier,
		Complete: true,
	}
	if err := s.Onboard.CreateProposal(context.Background(), p); err != nil {
		t.Fatalf("seed proposal %s: %v", id, err)
	}
}

func machineReq(t *testing.T, h http.Handler, key, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var rdr *strings.Reader
	if body == "" {
		rdr = strings.NewReader("")
	} else {
		rdr = strings.NewReader(body)
	}
	r := httptest.NewRequestWithContext(context.Background(), method, path, rdr)
	r.Header.Set("Authorization", "Bearer "+key)
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// Machine realm: a participant sponsor key is scoped to its own rows; admin
// actions are operator-only; an escalated-cap participant keeps the full view.
func TestMachineRealm_ParticipantSponsorScoped(t *testing.T) {
	s := newOnboardTestServer(t)
	portalURL, _ := fakeSponsorPortal(t, map[string]sponsorLookupResponse{
		"sponsora-key":    {Org: "0ff404dd", Alias: "pat-example", SponsorAuthorized: true, SponsorMaxTier: "developer"},
		"admin-cap-key": {Org: "aaaa1111", Alias: "big-sponsor", SponsorAuthorized: true, SponsorMaxTier: "iam-admin"},
	}, nil)
	s.Cfg.PortalInternalURL = portalURL
	h := s.Routes()

	// Rows under three sponsors: the participant (by prefix), a foreign org,
	// and the operator.
	seedProposal(t, s, "p-own", "0ff404dd", "developer", onboardstore.ProposalProposed)
	seedProposal(t, s, "p-foreign", "other-org", "developer", onboardstore.ProposalProposed)
	seedProposal(t, s, "p-op", "onboard-runner", "developer", onboardstore.ProposalSponsorApproved)

	// Scoped list: exactly the own row.
	w := machineReq(t, h, "sponsora-key", "GET", "/api/onboard/proposals", "")
	var ps []onboardstore.Proposal
	_ = json.Unmarshal(w.Body.Bytes(), &ps)
	if len(ps) != 1 || ps[0].ID != "p-own" {
		t.Fatalf("participant list = %+v, want exactly p-own", ps)
	}
	// Foreign detail reads 404.
	if w := machineReq(t, h, "sponsora-key", "GET", "/api/onboard/proposals/p-foreign", ""); w.Code != http.StatusNotFound {
		t.Fatalf("foreign get = %d, want 404", w.Code)
	}
	// Foreign vouch reads 404; own vouch lands.
	if w := machineReq(t, h, "sponsora-key", "POST", "/api/onboard/proposals/p-foreign/sponsor-approve", "{}"); w.Code != http.StatusNotFound {
		t.Fatalf("foreign vouch = %d, want 404; %s", w.Code, w.Body.String())
	}
	if w := machineReq(t, h, "sponsora-key", "POST", "/api/onboard/proposals/p-own/sponsor-approve", "{}"); w.Code != http.StatusOK {
		t.Fatalf("own vouch = %d, want 200; %s", w.Code, w.Body.String())
	}
	// Admin-stage + runner-contract actions are operator-only for participants.
	for _, act := range []string{"approve", "reject", "onboarded", "claim", "result"} {
		body := "{}"
		if act == "result" {
			body = `{"status":"onboarded"}`
		}
		if w := machineReq(t, h, "sponsora-key", "POST", "/api/onboard/proposals/p-own/"+act, body); w.Code != http.StatusForbidden {
			t.Fatalf("participant %s = %d, want 403; %s", act, w.Code, w.Body.String())
		}
	}
	// The operator key keeps the full view + admin actions.
	w = machineReq(t, h, "runner-key", "GET", "/api/onboard/proposals", "")
	ps = nil
	_ = json.Unmarshal(w.Body.Bytes(), &ps)
	if len(ps) != 3 {
		t.Fatalf("operator list = %d rows, want 3", len(ps))
	}
	// The operator passes the guard (the synthetic "{}" manifest then fails
	// the re-lint with 400 — the guard admitting is what's under test here).
	if w := machineReq(t, h, "runner-key", "POST", "/api/onboard/proposals/p-own/approve", "{}"); w.Code == http.StatusForbidden {
		t.Fatalf("operator approve was refused by the principal guard: %d %s", w.Code, w.Body.String())
	}
	// An escalated-cap participant sees everything (view only).
	w = machineReq(t, h, "admin-cap-key", "GET", "/api/onboard/proposals", "")
	ps = nil
	_ = json.Unmarshal(w.Body.Bytes(), &ps)
	if len(ps) != 3 {
		t.Fatalf("escalated-cap list = %d rows, want 3", len(ps))
	}
}

// Machine realm: participant vouch respects the tier cap on their OWN invitee.
func TestMachineRealm_VouchTierCap(t *testing.T) {
	s := newOnboardTestServer(t)
	portalURL, _ := fakeSponsorPortal(t, map[string]sponsorLookupResponse{
		"browse-key": {Org: "bbbb2222", SponsorAuthorized: true, SponsorMaxTier: "browse"},
	}, nil)
	s.Cfg.PortalInternalURL = portalURL
	h := s.Routes()

	seedProposal(t, s, "p-high", "bbbb2222", "developer", onboardstore.ProposalProposed)
	w := machineReq(t, h, "browse-key", "POST", "/api/onboard/proposals/p-high/sponsor-approve", "{}")
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "tier_exceeds_sponsorship") {
		t.Fatalf("over-cap vouch = %d, want 403 tier_exceeds_sponsorship; %s", w.Code, w.Body.String())
	}
}

// Machine realm: invites scope the same way (list/get/revoke).
func TestMachineRealm_InvitesScoped(t *testing.T) {
	s := newOnboardTestServer(t)
	portalURL, _ := fakeSponsorPortal(t, map[string]sponsorLookupResponse{
		"sponsora-key": {Org: "0ff404dd", Alias: "pat-example", SponsorAuthorized: true, SponsorMaxTier: "developer"},
	}, nil)
	s.Cfg.PortalInternalURL = portalURL
	h := s.Routes()

	// One invite minted by the participant, one by the operator.
	w := machineReq(t, h, "sponsora-key", "POST", "/api/onboard/invite", `{"env":"zagros"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("participant mint = %d; %s", w.Code, w.Body.String())
	}
	var mine struct {
		ID      string `json:"id"`
		Sponsor string `json:"sponsor"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &mine)
	if mine.Sponsor != "0ff404dd" {
		t.Fatalf("participant mint sponsor = %q, want the prefix", mine.Sponsor)
	}
	w = machineReq(t, h, "runner-key", "POST", "/api/onboard/invite", `{"env":"zagros"}`)
	var theirs struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &theirs)

	// Scoped list + foreign 404s + own revoke.
	w = machineReq(t, h, "sponsora-key", "GET", "/api/onboard/invites", "")
	var invs []onboardstore.Invite
	_ = json.Unmarshal(w.Body.Bytes(), &invs)
	if len(invs) != 1 || invs[0].ID != mine.ID {
		t.Fatalf("participant invite list = %+v, want exactly own", invs)
	}
	if w := machineReq(t, h, "sponsora-key", "GET", "/api/onboard/invites/"+theirs.ID, ""); w.Code != http.StatusNotFound {
		t.Fatalf("foreign invite get = %d, want 404", w.Code)
	}
	if w := machineReq(t, h, "sponsora-key", "POST", "/api/onboard/invites/"+theirs.ID+"/revoke", "{}"); w.Code != http.StatusNotFound {
		t.Fatalf("foreign invite revoke = %d, want 404", w.Code)
	}
	if w := machineReq(t, h, "sponsora-key", "POST", "/api/onboard/invites/"+mine.ID+"/revoke", "{}"); w.Code != http.StatusOK {
		t.Fatalf("own invite revoke = %d; %s", w.Code, w.Body.String())
	}
	// Participant mint above the cap is refused.
	if w := machineReq(t, h, "sponsora-key", "POST", "/api/onboard/invite", `{"env":"zagros","tier":"iam-admin"}`); w.Code != http.StatusForbidden {
		t.Fatalf("over-cap mint = %d, want 403; %s", w.Code, w.Body.String())
	}
}

// Human realm: onboardGate admits ADMINS untouched, admits portal-bound
// sponsors with the scoped ctx, refuses unbound logins, and fails closed when
// the portal is down.
func TestOnboardGate_HumanRealm(t *testing.T) {
	s := newOnboardTestServer(t)
	s.Cfg.DevBypassAuth = false
	portalURL, portalRT := fakeSponsorPortal(t, nil, map[string]sponsorLookupResponse{
		"sponsora@example.org": {Org: "0ff404dd", Alias: "pat-example", SponsorAuthorized: true, SponsorMaxTier: "developer"},
	})
	s.Cfg.PortalInternalURL = portalURL
	s.Cfg.PortalLookupToken = "svc-token"

	gated := s.onboardGate(func(w http.ResponseWriter, r *http.Request) {
		sp := sponsorFromCtx(r.Context())
		_ = json.NewEncoder(w).Encode(map[string]any{"sponsor": sp != nil})
	})
	do := func(email, groups string) *httptest.ResponseRecorder {
		r := httptest.NewRequestWithContext(context.Background(), "GET", "/onboarding", nil)
		r.Header.Set("X-Forwarded-Email", email)
		r.Header.Set("X-Forwarded-Groups", groups)
		w := httptest.NewRecorder()
		gated(w, r)
		return w
	}

	if w := do("admin@example.org", "ADMINS"); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"sponsor":false`) {
		t.Fatalf("admin gate = %d %s", w.Code, w.Body.String())
	}
	// IAM-ADMIN holds matrix admin on id-admin-console — the gate consults
	// the matrix, never an ADMINS literal (ADR 1073).
	if w := do("iam@example.org", "IAM-ADMIN"); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"sponsor":false`) {
		t.Fatalf("iam-admin gate = %d %s", w.Code, w.Body.String())
	}
	if w := do("Sponsora@EXAMPLE.org", "DEVELOPER,OBSERVER"); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"sponsor":true`) {
		t.Fatalf("sponsor gate = %d %s", w.Code, w.Body.String())
	}
	if w := do("nobody@example.org", "DEVELOPER"); w.Code != http.StatusForbidden {
		t.Fatalf("non-sponsor gate = %d, want 403", w.Code)
	}
	portalRT.down = true
	if w := do("someone-else@example.org", "DEVELOPER"); w.Code != http.StatusBadGateway {
		t.Fatalf("portal-down gate = %d, want 502", w.Code)
	}
}

// Human realm: a sponsor session's mint carries their cap, an over-cap tier is
// refused, and admin decide actions are refused.
func TestHumanRealm_SponsorMintAndDecide(t *testing.T) {
	s := newOnboardTestServer(t)
	s.Cfg.DevBypassAuth = false
	portalURL, _ := fakeSponsorPortal(t, nil, map[string]sponsorLookupResponse{
		"sponsora@example.org": {Org: "0ff404dd", Alias: "pat-example", SponsorAuthorized: true, SponsorMaxTier: "developer"},
	})
	s.Cfg.PortalInternalURL = portalURL
	s.Cfg.PortalLookupToken = "svc-token"
	h := s.Routes()

	post := func(path string, form url.Values) *httptest.ResponseRecorder {
		form.Set("csrf", s.csrfNonce)
		r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, path, strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("X-Forwarded-Email", "sponsora@example.org")
		r.Header.Set("X-Forwarded-Groups", "DEVELOPER,OBSERVER")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}

	// Over-cap mint refused.
	form := url.Values{}
	form.Set("env", "zagros")
	form.Set("tier", "iam-admin")
	if w := post("/onboarding/invite", form); w.Code != http.StatusForbidden {
		t.Fatalf("over-cap human mint = %d, want 403; %s", w.Code, w.Body.String())
	}
	// In-cap mint: sponsor = login email, cap stamped on the invite.
	form = url.Values{}
	form.Set("env", "zagros")
	form.Set("tier", "developer")
	if w := post("/onboarding/invite", form); w.Code != http.StatusOK {
		t.Fatalf("human mint = %d; %s", w.Code, w.Body.String())
	}
	invs, err := s.Onboard.ListInvites(context.Background(), "")
	if err != nil || len(invs) != 1 {
		t.Fatalf("invites = %v (%v)", invs, err)
	}
	if invs[0].Sponsor != "sponsora@example.org" {
		t.Fatalf("human mint sponsor = %q, want the login email", invs[0].Sponsor)
	}
	pb, err := invs[0].Prebound()
	if err != nil {
		t.Fatalf("decode stamped prebinding: %v", err)
	}
	if pb.SponsorMaxTier != "developer" {
		t.Fatalf("stamped cap = %q, want developer", pb.SponsorMaxTier)
	}

	// Admin decide from a sponsor session is refused; own sponsor-approve lands.
	seedProposal(t, s, "p-own", "sponsora@example.org", "developer", onboardstore.ProposalProposed)
	seedProposal(t, s, "p-foreign", "someone-else", "developer", onboardstore.ProposalProposed)
	if w := post("/onboarding/p-own/approve", url.Values{}); w.Code != http.StatusForbidden {
		t.Fatalf("sponsor admin-approve = %d, want 403; %s", w.Code, w.Body.String())
	}
	if w := post("/onboarding/p-foreign/sponsor-approve", url.Values{}); w.Code != http.StatusNotFound {
		t.Fatalf("sponsor foreign vouch = %d, want 404; %s", w.Code, w.Body.String())
	}
	if w := post("/onboarding/p-own/sponsor-approve", url.Values{}); w.Code != http.StatusSeeOther {
		t.Fatalf("sponsor own vouch = %d, want 303 redirect; %s", w.Code, w.Body.String())
	}
}
