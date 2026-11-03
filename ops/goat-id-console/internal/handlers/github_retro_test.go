package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/onboardstore"
)

// githubRecorder stands in for api.github.com: it records each collaborator
// PUT and answers 201 (an invitation was created).
type githubRecorder struct {
	mu   sync.Mutex
	puts []string
}

func (g *githubRecorder) RoundTrip(r *http.Request) (*http.Response, error) {
	g.mu.Lock()
	g.puts = append(g.puts, r.Method+" "+r.URL.Path)
	g.mu.Unlock()
	return &http.Response{StatusCode: http.StatusCreated, Body: io.NopCloser(strings.NewReader("{}")), Header: http.Header{}}, nil
}

func (g *githubRecorder) calls() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.puts...)
}

// withDeveloperRepoPolicy points the server at a policy that grants developers
// one repo and nothing to browse, with a token so grants are attempted, and
// routes GitHub calls to a recorder.
func withDeveloperRepoPolicy(t *testing.T, s *Server) *githubRecorder {
	t.Helper()
	path := filepath.Join(t.TempDir(), "repo-access.yaml")
	policy := "version: 1\ndefault_owner: usareur-af\nrules:\n" +
		"  - name: dev-examples\n    match:\n      tier: [developer]\n    grant:\n      - {repo: hello-galaxy, permission: pull}\n"
	if err := os.WriteFile(path, []byte(policy), 0o600); err != nil {
		t.Fatal(err)
	}
	s.Cfg.RepoAccessConfigPath = path
	s.Cfg.GitHubOnboardToken = "test-token"
	rec := &githubRecorder{}
	prev := repoGrantHTTPClient
	repoGrantHTTPClient = &http.Client{Transport: rec}
	t.Cleanup(func() { repoGrantHTTPClient = prev })
	return rec
}

func mintTierInvite(t *testing.T, s *Server, tier string) string {
	t.Helper()
	pb, _ := json.Marshal(onboardstore.Prebound{Tier: tier, Affiliation: "partner"})
	tok, err := s.Onboard.MintInvite(context.Background(), &onboardstore.Invite{Sponsor: "dev@bypass.local", Env: "zagros", PreboundJSON: string(pb)})
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func submitFull(t *testing.T, h http.Handler, tok string, sub map[string]any) onboardSubmitResponse {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"token": tok, "submission": withPersonDefaults(sub)})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/public/onboard/submit", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("submit: want 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var r onboardSubmitResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &r); err != nil {
		t.Fatal(err)
	}
	return r
}

// The preview names the tiers that receive repositories, read from the live
// policy, so the form warns only where a username actually matters.
func TestPreviewNamesRepoGrantingTiers(t *testing.T) {
	s := newOnboardTestServer(t)
	withDeveloperRepoPolicy(t, s)
	h := s.Routes()

	pv := preview(t, h, mintTierInvite(t, s, "developer"))
	if len(pv.RepoTiers) == 0 || !strings.EqualFold(pv.RepoTiers[0], "developer") {
		t.Fatalf("developer invite: repo_tiers = %v, want developer first", pv.RepoTiers)
	}
	for _, tier := range pv.RepoTiers {
		if strings.EqualFold(tier, "browse") {
			t.Fatalf("browse gets no repos in this policy but was listed: %v", pv.RepoTiers)
		}
	}

	// No policy file: nothing to warn about.
	s.Cfg.RepoAccessConfigPath = filepath.Join(t.TempDir(), "absent.yaml")
	if pv := preview(t, h, mintTierInvite(t, s, "developer")); len(pv.RepoTiers) != 0 {
		t.Fatalf("no policy: repo_tiers = %v, want empty", pv.RepoTiers)
	}
}

// Leaving the username blank on a repo-granting tier is a warning, never a
// refusal; giving one, or being on a tier with no repos, carries no warning.
func TestSubmitWithoutGitHubWarnsButSucceeds(t *testing.T) {
	s := newOnboardTestServer(t)
	withDeveloperRepoPolicy(t, s)
	h := s.Routes()

	r := submitFull(t, h, mintTierInvite(t, s, "developer"), map[string]any{"name": "Dev A", "email": "a@example.org", "organization": "Org"})
	if len(r.Warnings) != 1 || r.Warnings[0] != githubMissingWarning {
		t.Fatalf("developer without github: warnings = %v, want [%s]", r.Warnings, githubMissingWarning)
	}
	if r.ProposalID == "" {
		t.Fatal("the submit must still create the proposal")
	}

	r = submitFull(t, h, mintTierInvite(t, s, "developer"), map[string]any{"name": "Dev B", "email": "b@example.org", "organization": "Org", "github_username": "octocat"})
	if len(r.Warnings) != 0 {
		t.Fatalf("developer with github: warnings = %v, want none", r.Warnings)
	}

	r = submitFull(t, h, mintTierInvite(t, s, "browse"), map[string]any{"name": "Browse C", "email": "c@example.org", "organization": "Org"})
	if len(r.Warnings) != 0 {
		t.Fatalf("browse tier gets no repos: warnings = %v, want none", r.Warnings)
	}
}

func postGitHubMachine(t *testing.T, h http.Handler, pid, login string) machineResp {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"github": login})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/onboard/proposals/"+pid+"/github", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer runner-key")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return machineResp{code: rec.Code, body: rec.Body.String()}
}

func manifestGitHub(t *testing.T, s *Server, pid string) string {
	t.Helper()
	p, err := s.Onboard.GetProposal(context.Background(), pid)
	if err != nil {
		t.Fatal(err)
	}
	_, gh := manifestIdentity(p.ManifestJSON)
	return gh
}

// A username added after onboarding lands where the grant reads it and the
// grant runs again: GitHub is asked to add the person to the policy's repos.
func TestGitHubAfterOnboardingRerunsTheGrant(t *testing.T) {
	for _, state := range []string{onboardstore.ProposalOnboarded, onboardstore.ProposalEnrolled} {
		t.Run(state, func(t *testing.T) {
			s := newOnboardTestServer(t)
			gh := withDeveloperRepoPolicy(t, s)
			h := s.Routes()
			pid := submitFull(t, h, mintTierInvite(t, s, "developer"), map[string]any{"name": "Late Dev", "email": "late@example.org", "organization": "Org"}).ProposalID
			driveTo(t, s, pid, state)
			before := len(gh.calls())

			res := postGitHubMachine(t, h, pid, "@late-dev")
			if res.code != http.StatusOK {
				t.Fatalf("set github: %d %s", res.code, res.body)
			}
			var u githubUpdate
			if err := json.Unmarshal([]byte(res.body), &u); err != nil {
				t.Fatal(err)
			}
			if u.After != "late-dev" || u.Stage != "post-mint" {
				t.Fatalf("update = %+v, want after=late-dev stage=post-mint", u)
			}
			if got := manifestGitHub(t, s, pid); got != "late-dev" {
				t.Fatalf("manifest identity.github = %q, want late-dev", got)
			}
			calls := gh.calls()[before:]
			if len(calls) != 1 || calls[0] != "PUT /repos/usareur-af/hello-galaxy/collaborators/late-dev" {
				t.Fatalf("github calls = %v, want one PUT for hello-galaxy", calls)
			}
			if len(u.Grants) != 1 || u.Grants[0].Outcome != "invited" {
				t.Fatalf("grants = %+v, want one invited", u.Grants)
			}
			if st := statusOf(t, s, pid); st != state {
				t.Fatalf("status moved to %s; a GitHub change must not move the row", st)
			}
		})
	}
}

// A bad login, a row mid-mint and a closed row are refused, and none of them
// calls GitHub.
func TestGitHubPostMintRefusals(t *testing.T) {
	s := newOnboardTestServer(t)
	gh := withDeveloperRepoPolicy(t, s)
	h := s.Routes()
	ctx := context.Background()

	pid := submitFull(t, h, mintTierInvite(t, s, "developer"), map[string]any{"name": "R", "email": "r@example.org", "organization": "Org"}).ProposalID
	driveTo(t, s, pid, onboardstore.ProposalOnboarded)
	if res := postGitHubMachine(t, h, pid, "bad--login"); res.code != http.StatusBadRequest {
		t.Fatalf("bad login: want 400, got %d %s", res.code, res.body)
	}

	mid := submitFull(t, h, mintTierInvite(t, s, "developer"), map[string]any{"name": "M", "email": "m@example.org", "organization": "Org"}).ProposalID
	for _, st := range []string{onboardstore.ProposalSponsorApproved, onboardstore.ProposalApproved} {
		if err := s.Onboard.DecideProposal(ctx, mid, st, "admin@x", ""); err != nil {
			t.Fatal(err)
		}
	}
	if res := postGitHubMachine(t, h, mid, "mid-dev"); res.code != http.StatusConflict {
		t.Fatalf("approved (mid-mint): want 409, got %d %s", res.code, res.body)
	}

	arch := submitFull(t, h, mintTierInvite(t, s, "developer"), map[string]any{"name": "A", "email": "arch@example.org", "organization": "Org"}).ProposalID
	driveTo(t, s, arch, onboardstore.ProposalArchived)
	if res := postGitHubMachine(t, h, arch, "arch-dev"); res.code != http.StatusConflict {
		t.Fatalf("archived: want 409, got %d %s", res.code, res.body)
	}
	if n := len(gh.calls()); n != 0 {
		t.Fatalf("refused changes called GitHub %d times", n)
	}
}

// The person sets their own username from /github; the record is found from
// the session, never from the form.
func TestGitHubSelfService(t *testing.T) {
	s := newOnboardTestServer(t)
	gh := withDeveloperRepoPolicy(t, s)
	h := s.Routes()
	pid := submitFull(t, h, mintTierInvite(t, s, "developer"), map[string]any{"name": "Self", "email": "self@example.org", "organization": "Org"}).ProposalID
	driveTo(t, s, pid, onboardstore.ProposalEnrolled)

	get := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/github", nil)
	get.Header.Set("X-Forwarded-Email", "self@example.org")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, get)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "usareur-af/hello-galaxy") {
		t.Fatalf("GET /github: %d, want 200 naming the missing repo; body:\n%s", rec.Code, rec.Body.String())
	}

	form := url.Values{"csrf": {s.csrfNonce}, "github": {"self-dev"}}
	post := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/github", strings.NewReader(form.Encode()))
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	post.Header.Set("X-Forwarded-Email", "self@example.org")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, post)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /github: %d %s", rec.Code, rec.Body.String())
	}
	if got := manifestGitHub(t, s, pid); got != "self-dev" {
		t.Fatalf("identity.github = %q, want self-dev", got)
	}
	if n := len(gh.calls()); n != 1 {
		t.Fatalf("github calls = %d, want 1", n)
	}

	// Someone with no onboarding record changes nothing.
	post = httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/github", strings.NewReader(form.Encode()))
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	post.Header.Set("X-Forwarded-Email", "stranger@example.org")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, post)
	if !strings.Contains(rec.Body.String(), "no onboarding record for your address") {
		t.Fatalf("stranger: want the no-record message; body:\n%s", rec.Body.String())
	}
}

// The admin form on the card stores the username and redirects back to it.
func TestGitHubAdminCardForm(t *testing.T) {
	s := newOnboardTestServer(t)
	withDeveloperRepoPolicy(t, s)
	h := s.Routes()
	pid := submitFull(t, h, mintTierInvite(t, s, "developer"), map[string]any{"name": "Card", "email": "card@example.org", "organization": "Org"}).ProposalID
	driveTo(t, s, pid, onboardstore.ProposalOnboarded)

	page := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/onboarding?id="+pid, nil)
	page.Header.Set("X-Forwarded-Email", "admin@example.org")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, page)
	if !strings.Contains(rec.Body.String(), "No GitHub username on file") || !strings.Contains(rec.Body.String(), "/onboarding/"+pid+"/github") {
		t.Fatalf("onboarded card: want the GitHub warning and form")
	}

	form := url.Values{"csrf": {s.csrfNonce}, "github": {"card-dev"}}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/onboarding/"+pid+"/github", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Forwarded-Email", "admin@example.org")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "flash="+flashGitHubSet) {
		t.Fatalf("admin set: %d location=%q body=%s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	if got := manifestGitHub(t, s, pid); got != "card-dev" {
		t.Fatalf("identity.github = %q, want card-dev", got)
	}
}

// The card's browser read names the repositories a person without a username
// is missing, instead of reading as "the policy grants nothing".
func TestRepoAccessBrowserViewNamesMissingRepos(t *testing.T) {
	s := newOnboardTestServer(t)
	withDeveloperRepoPolicy(t, s)
	h := s.Routes()
	pid := submitFull(t, h, mintTierInvite(t, s, "developer"), map[string]any{"name": "Miss", "email": "miss@example.org", "organization": "Org"}).ProposalID

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/onboarding/"+pid+"/repo-access", nil)
	req.Header.Set("X-Forwarded-Email", "admin@example.org")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("browser repo-access: %d %s", rec.Code, rec.Body.String())
	}
	var v repoAccessView
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if len(v.Effective) != 0 || len(v.Missing) != 1 || v.Missing[0].Repo != "hello-galaxy" {
		t.Fatalf("view = %+v, want nothing effective and hello-galaxy missing", v)
	}
}
