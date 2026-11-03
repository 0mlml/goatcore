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
	"time"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/onboardstore"
)

func postGitHubRequestMachine(t *testing.T, h http.Handler, path string, body any) machineResp {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, path, rdr)
	req.Header.Set("Authorization", "Bearer runner-key")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return machineResp{code: rec.Code, body: rec.Body.String()}
}

func nextMail(t *testing.T, messages <-chan string) string {
	t.Helper()
	select {
	case m := <-messages:
		return m
	case <-time.After(5 * time.Second):
		t.Fatal("no mail delivered")
		return ""
	}
}

// An onboarded person with no GitHub username can be sent a request that
// links to /github; the mail names the goatnet and the link, the request is
// audited, and the proposal does not move.
func TestGitHubRequestEmailsAnOnboardedPersonWithout(t *testing.T) {
	for _, state := range []string{onboardstore.ProposalOnboarded, onboardstore.ProposalEnrolled} {
		t.Run(state, func(t *testing.T) {
			s := newOnboardTestServer(t)
			s.Cfg.ConsolePublicURL = "https://admin.auth.zagros.example.test/"
			withDeveloperRepoPolicy(t, s)
			h := s.Routes()
			pid := submitFull(t, h, mintTierInvite(t, s, "developer"), map[string]any{"name": "No Login", "email": "nologin@example.org", "organization": "Org"}).ProposalID
			driveTo(t, s, pid, state)
			// Capture from here: setup may send onboarding mail of its own.
			messages := captureMail(t, s)

			res := postGitHubRequestMachine(t, h, "/api/onboard/proposals/"+pid+"/github-request", nil)
			if res.code != http.StatusOK {
				t.Fatalf("request: %d %s", res.code, res.body)
			}
			var out githubRequestResult
			if err := json.Unmarshal([]byte(res.body), &out); err != nil {
				t.Fatal(err)
			}
			if !out.Sent || out.Link != "https://admin.auth.zagros.example.test/github" {
				t.Fatalf("result = %+v, want sent with the console /github link", out)
			}
			msg := nextMail(t, messages)
			for _, want := range []string{"nologin@example.org", "Hello No Login", "https://admin.auth.zagros.example.test/github", "zagros"} {
				if !strings.Contains(msg, want) {
					t.Fatalf("mail lacks %q:\n%s", want, msg)
				}
			}
			ev := noticeEvents(s, "onboard_github_requested")
			if len(ev) != 1 || ev[0].Result != "ok" {
				t.Fatalf("audit = %+v, want one ok onboard_github_requested", ev)
			}
			if st := statusOf(t, s, pid); st != state {
				t.Fatalf("status moved to %s; a request must not move the row", st)
			}
		})
	}
}

// A record that already has a login, or is not live yet, is refused and no
// mail leaves.
func TestGitHubRequestRefusals(t *testing.T) {
	s := newOnboardTestServer(t)
	withDeveloperRepoPolicy(t, s)
	h := s.Routes()

	has := submitFull(t, h, mintTierInvite(t, s, "developer"), map[string]any{"name": "Has", "email": "has@example.org", "organization": "Org", "github_username": "has-login"}).ProposalID
	driveTo(t, s, has, onboardstore.ProposalOnboarded)
	review := submitFull(t, h, mintTierInvite(t, s, "developer"), map[string]any{"name": "Review", "email": "review@example.org", "organization": "Org"}).ProposalID
	messages := captureMail(t, s)
	if res := postGitHubRequestMachine(t, h, "/api/onboard/proposals/"+has+"/github-request", nil); res.code != http.StatusConflict || !strings.Contains(res.body, "has-login") {
		t.Fatalf("has a login: %d %s, want 409 naming it", res.code, res.body)
	}

	if res := postGitHubRequestMachine(t, h, "/api/onboard/proposals/"+review+"/github-request", nil); res.code != http.StatusConflict {
		t.Fatalf("in review: %d %s, want 409", res.code, res.body)
	}
	if res := postGitHubRequestMachine(t, h, "/api/onboard/proposals/prop-nope/github-request", nil); res.code != http.StatusNotFound {
		t.Fatalf("unknown: %d, want 404", res.code)
	}
	if len(messages) != 0 {
		t.Fatal("a refused request delivered mail")
	}
}

// The sweep finds exactly the live records without a login; a dry run sends
// nothing, the real run mails each one.
func TestGitHubRequestSweep(t *testing.T) {
	s := newOnboardTestServer(t)
	withDeveloperRepoPolicy(t, s)
	h := s.Routes()

	a := submitFull(t, h, mintTierInvite(t, s, "developer"), map[string]any{"name": "A", "email": "a@example.org", "organization": "Org"}).ProposalID
	driveTo(t, s, a, onboardstore.ProposalOnboarded)
	b := submitFull(t, h, mintTierInvite(t, s, "developer"), map[string]any{"name": "B", "email": "b@example.org", "organization": "Org"}).ProposalID
	driveTo(t, s, b, onboardstore.ProposalEnrolled)
	c := submitFull(t, h, mintTierInvite(t, s, "developer"), map[string]any{"name": "C", "email": "c@example.org", "organization": "Org", "github_username": "c-login"}).ProposalID
	driveTo(t, s, c, onboardstore.ProposalOnboarded)
	_ = submitFull(t, h, mintTierInvite(t, s, "developer"), map[string]any{"name": "D", "email": "d@example.org", "organization": "Org"}).ProposalID
	messages := captureMail(t, s)

	type sweep struct {
		DryRun   bool                  `json:"dry_run"`
		Requests []githubRequestResult `json:"requests"`
	}
	read := func(res machineResp) sweep {
		t.Helper()
		if res.code != http.StatusOK {
			t.Fatalf("sweep: %d %s", res.code, res.body)
		}
		var out sweep
		if err := json.Unmarshal([]byte(res.body), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	ids := func(rs []githubRequestResult) map[string]bool {
		m := map[string]bool{}
		for _, r := range rs {
			m[r.ID] = true
		}
		return m
	}

	dry := read(postGitHubRequestMachine(t, h, "/api/onboard/github-requests", map[string]any{"env": "zagros", "dry_run": true}))
	if got := ids(dry.Requests); len(got) != 2 || !got[a] || !got[b] {
		t.Fatalf("dry run picked %v, want exactly %s and %s", got, a, b)
	}
	if len(messages) != 0 {
		t.Fatal("dry run delivered mail")
	}

	real := read(postGitHubRequestMachine(t, h, "/api/onboard/github-requests", map[string]any{"env": "zagros"}))
	for _, r := range real.Requests {
		if !r.Sent {
			t.Fatalf("row %s not sent: %s", r.ID, r.Reason)
		}
	}
	got := map[string]bool{}
	for i := 0; i < 2; i++ {
		msg := nextMail(t, messages)
		for _, addr := range []string{"a@example.org", "b@example.org"} {
			if strings.Contains(msg, "To: "+addr) {
				got[addr] = true
			}
		}
	}
	if len(got) != 2 {
		t.Fatalf("mailed %v, want a@example.org and b@example.org", got)
	}

	other := read(postGitHubRequestMachine(t, h, "/api/onboard/github-requests", map[string]any{"env": "zagros-prod", "dry_run": true}))
	if len(other.Requests) != 0 {
		t.Fatalf("env filter leaked: %+v", other.Requests)
	}
}

// The card offers the request only when no login is on file, and the button
// sends it from an admin session.
func TestGitHubRequestAdminCard(t *testing.T) {
	s := newOnboardTestServer(t)
	withDeveloperRepoPolicy(t, s)
	h := s.Routes()
	pid := submitFull(t, h, mintTierInvite(t, s, "developer"), map[string]any{"name": "Card", "email": "cardreq@example.org", "organization": "Org"}).ProposalID
	driveTo(t, s, pid, onboardstore.ProposalOnboarded)
	messages := captureMail(t, s)

	page := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/onboarding?id="+pid, nil)
	page.Header.Set("X-Forwarded-Email", "admin@example.org")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, page)
	if !strings.Contains(rec.Body.String(), "/onboarding/"+pid+"/github-request") {
		t.Fatal("onboarded card without a login: want the Email a request form")
	}

	form := url.Values{"csrf": {s.csrfNonce}}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/onboarding/"+pid+"/github-request", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Forwarded-Email", "admin@example.org")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "flash="+flashGitHubRequested) {
		t.Fatalf("card request: %d location=%q body=%s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	if msg := nextMail(t, messages); !strings.Contains(msg, "cardreq@example.org") || !strings.Contains(msg, "Requested by admin@example.org") {
		t.Fatalf("mail does not reach the person or name the admin:\n%s", msg)
	}
}
