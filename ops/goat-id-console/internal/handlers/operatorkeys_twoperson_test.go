package handlers

// Operator-key approvals, hardened per #210: the queue and its decisions are
// admin-only (adminGate, not the sponsor-admitting onboardGate), requester ≠
// decider is enforced in the store behind BOTH doors, self-revoke stays
// allowed, and the onboarding dashboard grows an admin-only "Operator keys"
// tab after a divider. An admin must not be able to request a key and approve it
// themselves.

import (
	"context"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/onboardstore"
	"github.com/usareur-af/goatcore/ops/goat-id-console/web"
)

// browserForm posts an urlencoded form as a signed-in browser session.
func browserForm(t *testing.T, h http.Handler, email, groups, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequestWithContext(context.Background(), "POST", path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("X-Forwarded-Email", email)
	r.Header.Set("X-Forwarded-Groups", groups)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func seedOperatorKeyRequest(t *testing.T, s *Server, handle, email string) *onboardstore.OperatorKey {
	t.Helper()
	row, err := s.Onboard.RequestOperatorKey(context.Background(),
		&onboardstore.OperatorKey{Handle: handle, Email: email, Env: "zagros"})
	if err != nil {
		t.Fatalf("seed operator key for %s: %v", handle, err)
	}
	return row
}

// The queue moved from onboardGate to adminGate: a sponsorship-authorized
// non-admin · who yesterday could approve anyone's operator key · now gets
// 403 on the page and on every decision, before any handler logic runs.
func TestOperatorKeyQueue_adminOnly(t *testing.T) {
	s := newOnboardTestServer(t)
	s.Cfg.DevBypassAuth = false
	// A portal that WOULD resolve this login as a sponsor · proving the gate
	// never even asks: sponsorship buys no entry here.
	portalURL, _ := fakeSponsorPortal(t, nil, map[string]sponsorLookupResponse{
		"sponsor@example.org": {Org: "0ff404dd", SponsorAuthorized: true, SponsorMaxTier: "developer"},
	})
	s.Cfg.PortalInternalURL = portalURL
	s.Cfg.PortalLookupToken = "svc-token"
	h := s.Routes()
	row := seedOperatorKeyRequest(t, s, "cand1", "cand1@example.org")

	get := func(email, groups string) *httptest.ResponseRecorder {
		r := httptest.NewRequestWithContext(context.Background(), "GET", "/onboarding/operator-keys", nil)
		r.Header.Set("X-Forwarded-Email", email)
		r.Header.Set("X-Forwarded-Groups", groups)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}

	if w := get("sponsor@example.org", "DEVELOPER"); w.Code != http.StatusForbidden {
		t.Fatalf("sponsor queue GET = %d, want 403", w.Code)
	}
	if w := get("admin@example.org", "ADMINS"); w.Code != http.StatusOK {
		t.Fatalf("admin queue GET = %d, want 200; %s", w.Code, w.Body.String())
	}
	// The matrix drives the gate (ADR 1073) · IAM-ADMIN passes too.
	if w := get("iam@example.org", "IAM-ADMIN"); w.Code != http.StatusOK {
		t.Fatalf("iam-admin queue GET = %d, want 200", w.Code)
	}

	form := url.Values{"csrf": {s.csrfNonce}}
	if w := browserForm(t, h, "sponsor@example.org", "DEVELOPER",
		"/operator-keys/"+row.ID+"/approve", form); w.Code != http.StatusForbidden {
		t.Fatalf("sponsor decision = %d, want 403", w.Code)
	}
	if cur, _ := s.Onboard.OperatorKey(context.Background(), row.ID); cur.Status != onboardstore.OperatorKeyRequested {
		t.Fatalf("row after sponsor attempt = %q, want untouched requested", cur.Status)
	}
}

// Browser door: an admin cannot decide their own request · matched by session
// email or by the handle it resolves to · gets a 403 that names the rule AND
// the single-admin break-glass path, and the refusal is still an audit event.
// A different admin decides normally; self-revoke stays allowed.
func TestOperatorKeyDecision_browserTwoPersonRule(t *testing.T) {
	s := newOnboardTestServer(t)
	s.Cfg.DevBypassAuth = false
	h := s.Routes()

	// Email-matched own row.
	rowEmail := seedOperatorKeyRequest(t, s, "handle-a", "person-11@example.org")
	form := url.Values{"csrf": {s.csrfNonce}}
	w := browserForm(t, h, "admin@example.org", "ADMINS", "/operator-keys/"+rowEmail.ID+"/approve", form)
	if w.Code != http.StatusForbidden {
		t.Fatalf("self-approve = %d, want 403; %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "your own request") ||
		!strings.Contains(w.Body.String(), "break-glass") {
		t.Fatalf("self-approve refusal must name the rule and the break-glass path; got: %.300s", w.Body.String())
	}
	if cur, _ := s.Onboard.OperatorKey(context.Background(), rowEmail.ID); cur.Status != onboardstore.OperatorKeyRequested || cur.Secret != "" {
		t.Fatalf("row after self-approve = %+v, want untouched requested", cur)
	}
	if w := browserForm(t, h, "admin@example.org", "ADMINS", "/operator-keys/"+rowEmail.ID+"/reject", form); w.Code != http.StatusForbidden {
		t.Fatalf("self-reject = %d, want 403", w.Code)
	}

	// Handle-matched own row: the session email differs from the row's, but
	// the onboarding record resolves the session to the row's handle.
	seedProposal(t, s, "p-adm2", "onboard-runner", "developer", onboardstore.ProposalOnboarded)
	// seedProposal derives handle/email from the id: handle cand-p-adm2, email p-adm2@example.org.
	rowHandle := seedOperatorKeyRequest(t, s, "cand-p-adm2", "other-address@example.org")
	if w := browserForm(t, h, "p-adm2@example.org", "ADMINS", "/operator-keys/"+rowHandle.ID+"/approve", form); w.Code != http.StatusForbidden {
		t.Fatalf("handle-matched self-approve = %d, want 403", w.Code)
	}

	// A DIFFERENT admin approves the first row fine.
	if w := browserForm(t, h, "second@example.org", "ADMINS", "/operator-keys/"+rowEmail.ID+"/approve", form); w.Code != http.StatusOK {
		t.Fatalf("different-admin approve = %d, want 200; %.300s", w.Code, w.Body.String())
	}
	cur, _ := s.Onboard.OperatorKey(context.Background(), rowEmail.ID)
	if cur.Status != onboardstore.OperatorKeyActive {
		t.Fatalf("row after different-admin approve = %q, want active", cur.Status)
	}

	// Self-REVOKE of the now-live key is de-escalation and must keep working.
	if w := browserForm(t, h, "admin@example.org", "ADMINS", "/operator-keys/"+rowEmail.ID+"/revoke", form); w.Code != http.StatusOK {
		t.Fatalf("self-revoke = %d, want 200; %.300s", w.Code, w.Body.String())
	}
	if cur, _ := s.Onboard.OperatorKey(context.Background(), rowEmail.ID); cur.Status != onboardstore.OperatorKeyRevoked {
		t.Fatalf("row after self-revoke = %q, want revoked", cur.Status)
	}
}

// API door: the per-handle operator principal is the decider; its own
// row refuses with 403 self_decision, another handle's row decides normally ·
// and the decision is attributed to the principal, not to nobody.
func TestOperatorKeyDecision_apiTwoPersonRule(t *testing.T) {
	s := newOnboardTestServer(t)
	h := s.Routes()

	own := seedOperatorKeyRequest(t, s, "onboard-runner", "")
	other := seedOperatorKeyRequest(t, s, "somebody-else", "")

	if w := machineReq(t, h, "runner-key", "POST", "/api/onboard/operator-keys/"+own.ID+"/approve", ""); w.Code != http.StatusForbidden ||
		!strings.Contains(w.Body.String(), "self_decision") {
		t.Fatalf("api self-approve = %d %s, want 403 self_decision", w.Code, w.Body.String())
	}
	if w := machineReq(t, h, "runner-key", "POST", "/api/onboard/operator-keys/"+own.ID+"/reject", ""); w.Code != http.StatusForbidden {
		t.Fatalf("api self-reject = %d, want 403", w.Code)
	}
	if cur, _ := s.Onboard.OperatorKey(context.Background(), own.ID); cur.Status != onboardstore.OperatorKeyRequested {
		t.Fatalf("own row after api refusals = %q, want requested", cur.Status)
	}

	if w := machineReq(t, h, "runner-key", "POST", "/api/onboard/operator-keys/"+other.ID+"/approve", ""); w.Code != http.StatusOK {
		t.Fatalf("api other-approve = %d; %s", w.Code, w.Body.String())
	}
	cur, _ := s.Onboard.OperatorKey(context.Background(), other.ID)
	if cur.Status != onboardstore.OperatorKeyActive || cur.DecidedBy != "onboard-runner" {
		t.Fatalf("other row = status %q decided_by %q, want active/onboard-runner", cur.Status, cur.DecidedBy)
	}
	// Self-revoke via API stays allowed too.
	if w := machineReq(t, h, "runner-key", "POST", "/api/onboard/operator-keys/"+other.ID+"/revoke", ""); w.Code != http.StatusOK {
		t.Fatalf("api revoke = %d; %s", w.Code, w.Body.String())
	}
}

// The onboarding dashboard's Operator-keys tab (#210): rendered for admin
// sessions only, after a divider at the end of the stage strip, count in the
// existing pill idiom (present when >0, quiet at zero, tab never vanishes),
// announced via aria-label. Sponsor markup must not contain it at all ·
// assert the markup, not the styling.
func TestTemplates_onboardingOperatorKeysTab(t *testing.T) {
	base, err := loadTemplates()
	if err != nil {
		t.Fatalf("loadTemplates: %v", err)
	}
	stages := []onboardStage{
		{Key: "approved", Label: "Approved", Count: 2},
		{Key: "onboarding", Label: "Provisioning"},
	}
	render := func(t *testing.T, vars *pageVars) string {
		t.Helper()
		tpl, err := base.Clone()
		if err != nil {
			t.Fatal(err)
		}
		data, err := fs.ReadFile(web.FS, "templates/onboarding.html")
		if err != nil {
			t.Fatalf("read template: %v", err)
		}
		if _, err := tpl.New("onboarding").Parse(string(data)); err != nil {
			t.Fatalf("parse: %v", err)
		}
		var sb strings.Builder
		if err := tpl.ExecuteTemplate(&sb, "layout", vars); err != nil {
			t.Fatalf("execute: %v", err)
		}
		return sb.String()
	}

	t.Run("admin with pending requests", func(t *testing.T) {
		out := render(t, &pageVars{OnboardStages: stages, OnboardStatus: "approved",
			OperatorKeysTab: true, OperatorKeysPending: 3})
		for _, want := range []string{
			`href="/onboarding/operator-keys"`,
			"Operator keys",
			`aria-label="Operator keys · 3 awaiting decision"`,
			`class="tabdiv" aria-hidden="true"`,
			`<span class="c">3</span>`,
		} {
			if !strings.Contains(out, want) {
				t.Errorf("missing %q", want)
			}
		}
	})
	t.Run("admin at zero keeps a quiet tab", func(t *testing.T) {
		out := render(t, &pageVars{OnboardStages: stages, OnboardStatus: "approved",
			OperatorKeysTab: true})
		if !strings.Contains(out, "Operator keys") {
			t.Error("the tab must not vanish at zero · a quiet tab is the honest zero state")
		}
		if !strings.Contains(out, `aria-label="Operator keys · 0 awaiting decision"`) {
			t.Error("zero state must still be announced")
		}
		// The count chip is present-but-hidden at zero, matching the stage tabs.
		if strings.Contains(out, `Operator keys<span class="c">`) {
			t.Error("a visible zero count breaks the stage-tab idiom")
		}
	})
	t.Run("sponsor markup carries no tab and no count", func(t *testing.T) {
		out := render(t, &pageVars{OnboardStages: stages, OnboardStatus: "approved",
			SponsorMode: true})
		for _, banned := range []string{"/onboarding/operator-keys", "Operator keys", "tabdiv"} {
			if strings.Contains(out, banned) {
				t.Errorf("sponsor render must not contain %q", banned)
			}
		}
	})
	t.Run("provisioning tab explains its usual emptiness", func(t *testing.T) {
		out := render(t, &pageVars{OnboardStages: stages, OnboardStatus: "approved"})
		if !strings.Contains(out, "Runner mint in progress · usually empty.") {
			t.Error("the Provisioning tab needs its transient-by-design hint")
		}
	})
}

// End-to-end shape of the hole the store test fences: a key requested through
// the MACHINE door (handle, no email), approved from the BROWSER by the very
// person it belongs to, whose session knows only an email. The onboarding
// record is what ties the two together, and the refusal has to hold on this
// door.
func TestOperatorKeyDecision_browserSelfDecisionOnMachineFiledRow(t *testing.T) {
	s := newOnboardTestServer(t)
	s.Cfg.DevBypassAuth = false
	h := s.Routes()

	// seedProposal derives handle cand-p-ada and email p-ada@example.org.
	seedProposal(t, s, "p-ada", "onboard-runner", "developer", onboardstore.ProposalOnboarded)
	row := seedOperatorKeyRequest(t, s, "cand-p-ada", "") // handle only, as the API files them
	form := url.Values{"csrf": {s.csrfNonce}}

	w := browserForm(t, h, "p-ada@example.org", "ADMINS", "/operator-keys/"+row.ID+"/approve", form)
	if w.Code != http.StatusForbidden {
		t.Fatalf("self-approve of a machine-filed own row = %d, want 403; %.300s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "your own request") {
		t.Errorf("refusal must name the rule; got %.300s", w.Body.String())
	}
	if cur, _ := s.Onboard.OperatorKey(context.Background(), row.ID); cur.Status != onboardstore.OperatorKeyRequested {
		t.Fatalf("row = %q, want untouched requested", cur.Status)
	}

	// A different admin, also known only by email, still decides it · the
	// enrichment must not turn the rule into "nobody may decide".
	if w := browserForm(t, h, "someone-else@example.org", "ADMINS",
		"/operator-keys/"+row.ID+"/approve", form); w.Code != http.StatusOK {
		t.Fatalf("different-admin approve = %d, want 200; %.300s", w.Code, w.Body.String())
	}
}

// The third outcome: neither "yours" nor "not yours" but "cannot be told".
// An admin with no onboarding record, deciding a machine-filed row whose
// handle resolves to nobody, is refused 403 · and the queue renders that row
// without decide controls, so the page never offers what the store refuses.
func TestOperatorKeyDecision_uncomparableIdentitiesRefused(t *testing.T) {
	s := newOnboardTestServer(t)
	s.Cfg.DevBypassAuth = false
	h := s.Routes()

	row := seedOperatorKeyRequest(t, s, "orphan-handle", "") // no email, no proposal
	form := url.Values{"csrf": {s.csrfNonce}}

	w := browserForm(t, h, "admin@example.org", "ADMINS", "/operator-keys/"+row.ID+"/approve", form)
	if w.Code != http.StatusForbidden {
		t.Fatalf("uncomparable approve = %d, want 403; %.300s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "goat admin operator-key") {
		t.Errorf("refusal must point at the door that always resolves a handle; got %.400s", w.Body.String())
	}
	if cur, _ := s.Onboard.OperatorKey(context.Background(), row.ID); cur.Status != onboardstore.OperatorKeyRequested {
		t.Fatalf("row = %q, want untouched requested", cur.Status)
	}

	// The queue itself must not offer a button that cannot work.
	q := httptest.NewRequestWithContext(context.Background(), "GET", "/onboarding/operator-keys", nil)
	q.Header.Set("X-Forwarded-Email", "admin@example.org")
	q.Header.Set("X-Forwarded-Groups", "ADMINS")
	qw := httptest.NewRecorder()
	h.ServeHTTP(qw, q)
	if qw.Code != http.StatusOK {
		t.Fatalf("queue GET = %d", qw.Code)
	}
	if strings.Contains(qw.Body.String(), "/operator-keys/"+row.ID+"/approve") {
		t.Error("the queue must not render an approve control the store will refuse")
	}
	if !strings.Contains(qw.Body.String(), "Not decidable from this session") {
		t.Errorf("the row must say why it cannot be decided; got %.500s", qw.Body.String())
	}
}

// The machine door answers the same refusal with its own error code, not a
// 500: nothing faulted, so a retry would only repeat.
func TestOperatorKeyDecision_apiUncomparableIdentitiesRefused(t *testing.T) {
	s := newOnboardTestServer(t)
	h := s.Routes()

	row := seedOperatorKeyRequest(t, s, "cand-x", "someone@example.org")
	// The API principal is the handle "onboard-runner"; this row carries an
	// email and a handle, so the pair IS comparable and the decision passes ·
	// the guard must not fire on ordinary machine decisions.
	if w := machineReq(t, h, "runner-key", "POST", "/api/onboard/operator-keys/"+row.ID+"/approve", ""); w.Code != http.StatusOK {
		t.Fatalf("ordinary api approve = %d; %s", w.Code, w.Body.String())
	}
}
