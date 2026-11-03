package handlers

// Asking a person for their GitHub username after onboarding.
//
// The repo-collaborator grant needs a GitHub login, and a person who left it
// blank at intake is added to no repository until one is on file. Admins can
// type it on the card; the person can add it at /github. Neither reaches a
// person who does not know to. This file is the nudge: an admin, or one
// sweep, emails the person a request that links to /github, where the record
// is found from the signed-in session, so the username lands on the right
// onboarding record without the email naming it. Nothing is stored on the
// proposal; the request is an audit event (onboard_github_requested), so a
// repeat is visible and nothing drifts.

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/onboardstore"
)

// githubRequestResult is what one request produced.
type githubRequestResult struct {
	ID     string `json:"id"`
	Email  string `json:"email"`
	Env    string `json:"env"`
	Status string `json:"status"`
	// Sent is true when the mail left; false with Reason when it did not.
	Sent   bool   `json:"sent"`
	Reason string `json:"reason,omitempty"`
	// Link is the page the mail points at.
	Link string `json:"link,omitempty"`
}

// githubRequestable says whether a proposal may be asked, or why not.
// A record that already carries a login is not asked: the right verb there is
// the grant re-run (POST …/github), not a request.
func githubRequestable(p *onboardstore.Proposal) *fieldEditError {
	switch p.Status {
	case onboardstore.ProposalOnboarded, onboardstore.ProposalEnrolled:
	default:
		return &fieldEditError{Status: http.StatusConflict,
			Message: "this onboarding record is " + p.Status + ", so there is no live access a GitHub username would extend; a request is for onboarded or enrolled people"}
	}
	if _, login := manifestIdentity(p.ManifestJSON); login != "" {
		return &fieldEditError{Status: http.StatusConflict,
			Message: "this record already has GitHub username " + login + "; to re-run the grant, submit the username again instead of requesting one"}
	}
	if strings.TrimSpace(p.Email) == "" {
		return &fieldEditError{Status: http.StatusConflict, Message: "this record has no email address to send a request to"}
	}
	return nil
}

// githubRequestMail is the one message. Plain text, like the stage
// notifications: the record is the text.
func (s *Server) githubRequestMail(p *onboardstore.Proposal, askedBy string) (subject, body, link string) {
	goatnet := strings.TrimSpace(p.Env)
	if goatnet == "" {
		goatnet = "the data fabric"
	}
	link = consoleBase(s.Cfg) + "/github"
	name, _ := manifestIdentity(p.ManifestJSON)
	greeting := "Hello,"
	if name != "" {
		greeting = "Hello " + name + ","
	}
	subject = "Your GitHub username for " + goatnet
	var b strings.Builder
	b.WriteString(greeting + "\n\n")
	b.WriteString("Your access to " + goatnet + " is set up, but there is no GitHub username on your onboarding record, ")
	b.WriteString("so you have not been added to the example and partner repositories your access level includes.\n\n")
	b.WriteString("Add it here, signed in with your " + goatnet + " account:\n\n    " + link + "\n\n")
	b.WriteString("Saving it sends you a GitHub invitation for each repository; accept each one from your GitHub notifications or email.\n\n")
	if askedBy != "" {
		b.WriteString("Requested by " + askedBy + ".\n")
	}
	if tag := strings.TrimSpace(s.Cfg.NotifyGoatnetTagline); tag != "" {
		b.WriteString("\n" + tag + "\n")
	}
	return subject, b.String(), link
}

// requestGitHubForProposal sends one request and records it. Refusals come
// back as fieldEditError with the HTTP status the caller should answer with.
func (s *Server) requestGitHubForProposal(r *http.Request, p *onboardstore.Proposal) (*githubRequestResult, *fieldEditError) {
	if ferr := githubRequestable(p); ferr != nil {
		return nil, ferr
	}
	actor := auditActor(r)
	subject, body, link := s.githubRequestMail(p, actor.Email)
	res := &githubRequestResult{ID: p.ID, Email: p.Email, Env: p.Env, Status: p.Status, Link: link}
	if s.Mailer == nil || !s.Mailer.Enabled() {
		res.Reason = "mailer disabled: this goatnet cannot send email (SES_SMTP_* not configured)"
		s.emitAudit(r, actor, "onboard_github_requested", nil, "",
			map[string]any{"proposal_id": p.ID}, map[string]any{"proposal_id": p.ID, "email": p.Email, "link": link},
			false, "error", res.Reason)
		return res, &fieldEditError{Status: http.StatusServiceUnavailable, Message: res.Reason}
	}
	if err := s.Mailer.Send(p.Email, subject, body); err != nil {
		res.Reason = "send failed: " + err.Error()
		s.emitAudit(r, actor, "onboard_github_requested", nil, "",
			map[string]any{"proposal_id": p.ID}, map[string]any{"proposal_id": p.ID, "email": p.Email, "link": link},
			false, "error", res.Reason)
		return res, &fieldEditError{Status: http.StatusBadGateway, Message: res.Reason}
	}
	res.Sent = true
	s.emitAudit(r, actor, "onboard_github_requested", nil, "",
		map[string]any{"proposal_id": p.ID}, map[string]any{"proposal_id": p.ID, "email": p.Email, "link": link},
		false, "ok", "")
	return res, nil
}

// githubMissing lists the onboarded and enrolled proposals with no GitHub
// username, optionally on one goatnet.
func (s *Server) githubMissing(r *http.Request, env string) ([]onboardstore.Proposal, error) {
	var out []onboardstore.Proposal
	for _, st := range []string{onboardstore.ProposalOnboarded, onboardstore.ProposalEnrolled} {
		rows, err := s.Onboard.ListProposals(r.Context(), st)
		if err != nil {
			return nil, fmt.Errorf("list %s: %w", st, err)
		}
		for _, p := range rows {
			if env != "" && !strings.EqualFold(strings.TrimSpace(p.Env), env) {
				continue
			}
			if _, login := manifestIdentity(p.ManifestJSON); login != "" {
				continue
			}
			if strings.TrimSpace(p.Email) == "" {
				continue
			}
			out = append(out, p)
		}
	}
	return out, nil
}

// --- operator API ------------------------------------------------------------

// apiRequestProposalGitHub is POST /api/onboard/proposals/{id}/github-request.
// Operator principals only. 200 with the result when the mail left; the
// refusal status otherwise (409 already has one / not live, 503 no mailer,
// 502 send failed).
func (s *Server) apiRequestProposalGitHub(w http.ResponseWriter, r *http.Request) {
	if !requireOperatorPrincipal(w, r) {
		return
	}
	p, err := s.Onboard.GetProposal(r.Context(), r.PathValue("id"))
	if errors.Is(err, onboardstore.ErrNotFound) {
		onboardError(w, http.StatusNotFound, "not_found", "proposal not found")
		return
	}
	if err != nil {
		onboardError(w, http.StatusInternalServerError, "get_error", "could not load proposal")
		return
	}
	res, ferr := s.requestGitHubForProposal(r, p)
	if ferr != nil {
		onboardError(w, ferr.Status, "github_request_refused", ferr.Message)
		return
	}
	onboardJSON(w, http.StatusOK, res)
}

// apiRequestGitHubMissing is POST /api/onboard/github-requests, body
// {"env": "<goatnet>", "dry_run": bool}. It finds every onboarded or enrolled
// person with no GitHub username and asks each one; dry_run lists them
// without sending. Each row says whether its mail left. The sweep never
// fails as a whole on one bad address: 200 with per-row reasons.
func (s *Server) apiRequestGitHubMissing(w http.ResponseWriter, r *http.Request) {
	if !requireOperatorPrincipal(w, r) {
		return
	}
	var body struct {
		Env    string `json:"env"`
		DryRun bool   `json:"dry_run"`
	}
	if r.Body != nil && r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			onboardError(w, http.StatusBadRequest, "decode", `body must be {"env": "<goatnet>", "dry_run": false}`)
			return
		}
	}
	rows, err := s.githubMissing(r, strings.TrimSpace(body.Env))
	if err != nil {
		onboardError(w, http.StatusInternalServerError, "list_error", err.Error())
		return
	}
	out := make([]githubRequestResult, 0, len(rows))
	for i := range rows {
		p := rows[i]
		if body.DryRun {
			_, _, link := s.githubRequestMail(&p, "")
			out = append(out, githubRequestResult{ID: p.ID, Email: p.Email, Env: p.Env, Status: p.Status, Link: link, Reason: "dry run"})
			continue
		}
		res, ferr := s.requestGitHubForProposal(r, &p)
		if ferr != nil && res == nil {
			res = &githubRequestResult{ID: p.ID, Email: p.Email, Env: p.Env, Status: p.Status, Reason: ferr.Message}
		}
		out = append(out, *res)
	}
	onboardJSON(w, http.StatusOK, map[string]any{"dry_run": body.DryRun, "requests": out})
}

// --- admin browser door --------------------------------------------------------

// handleOnboardingGitHubRequest is the "Email a request" control beside the
// GitHub box on the Onboarded and Enrolled cards. Admin sessions only, like
// the username form itself.
func (s *Server) handleOnboardingGitHubRequest(w http.ResponseWriter, r *http.Request) {
	if s.Onboard == nil {
		s.renderError(w, r, 503, "onboarding is not configured")
		return
	}
	if err := s.validateCSRF(r); err != nil {
		s.renderError(w, r, 400, err.Error())
		return
	}
	if sponsorFromCtx(r.Context()) != nil {
		s.renderError(w, r, 403, "asking a person for their GitHub username is an admin act")
		return
	}
	id := r.PathValue("id")
	p, err := s.Onboard.GetProposal(r.Context(), id)
	if errors.Is(err, onboardstore.ErrNotFound) {
		s.renderError(w, r, 404, "proposal not found")
		return
	}
	if err != nil {
		s.renderError(w, r, 502, "load proposal: "+err.Error())
		return
	}
	if _, ferr := s.requestGitHubForProposal(r, p); ferr != nil {
		s.renderError(w, r, ferr.Status, ferr.Message)
		return
	}
	q := url.Values{}
	q.Set("id", id)
	q.Set("status", p.Status)
	q.Set("flash", flashGitHubRequested)
	http.Redirect(w, r, "/onboarding?"+q.Encode(), http.StatusSeeOther)
}
