package handlers

// github_retro.go — a GitHub username supplied AFTER onboarding.
//
// The repo-collaborator grant (onboard_repo_grants.go) fires once, when the
// runner reports a proposal onboarded, and grants nothing to a person with no
// GitHub username. Before this file, a developer who left the box blank at
// intake was permanently outside the example and partner repositories: the
// manifest froze at mint and nothing re-ran the grant.
//
// One write, three doors:
//   - the person themselves, signed in, at /github (selfServiceAuth);
//   - an admin, on the Onboarded or Enrolled card (POST /onboarding/{id}/github);
//   - an operator key, POST /api/onboard/proposals/{id}/github.
//
// Each stores the login where the grant reads it (identity.github on the
// proposal manifest) and re-runs the same grantRepoAccess, so adding a
// username later produces the same repository invitations an on-time one
// would have. A person still in review goes through the review-stage field
// edit instead, which the grant will read at provision.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/onboardstore"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/repoaccess"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/welcome"
)

// githubMissingWarning is the submit response's warning code when a candidate
// on a repo-granting tier sends no GitHub username.
const githubMissingWarning = "github_username_missing"

// githubMissingConsequence is the plain-language consequence, shared by every
// surface that states it so they cannot drift.
const githubMissingConsequence = "Without a GitHub username you are not added to the example and partner " +
	"repositories this level includes, so you will not see the code and examples other " +
	"participants build from. You can add it at any time after onboarding, on the GitHub " +
	"page of the identity console (/github), and the repositories are granted then."

// repoGrantingTiers returns which of the candidate tiers this goatnet's
// repo-access policy can grant repositories to. prebound, when set, is the
// invite's locked tier; selectable is the menu offered otherwise. Both are
// checked so the form can warn whichever tier ends up chosen.
func (s *Server) repoGrantingTiers(prebound string, selectable []intakeTier) []string {
	cfg, err := repoaccess.Load(s.Cfg.RepoAccessConfigPath)
	if err != nil || cfg == nil {
		return []string{}
	}
	var tiers []string
	if t := strings.TrimSpace(prebound); t != "" {
		tiers = append(tiers, t)
	}
	for _, t := range selectable {
		tiers = append(tiers, t.ID)
	}
	out := cfg.TiersWithGrants(tiers)
	if out == nil {
		out = []string{}
	}
	return out
}

// normalizeGitHubLogin applies the review-stage rule (setGitHub) to a login
// arriving through a post-mint door: a leading @ is a paste artifact, the
// shape is GitHub's own. "" is valid and means remove.
func normalizeGitHubLogin(val string) (string, *fieldEditError) {
	val = strings.TrimPrefix(strings.TrimSpace(val), "@")
	if val == "" {
		return "", nil
	}
	if !githubPattern.MatchString(val) || strings.Contains(val, "--") {
		return "", badRequest("%q is not a GitHub login (alphanumeric with single hyphens, max 39)", val)
	}
	return val, nil
}

// githubUpdate is the outcome of one GitHub username change.
type githubUpdate struct {
	Before string              `json:"before"`
	After  string              `json:"after"`
	Grants []welcome.RepoGrant `json:"grants"`
	// Stage says which path applied it: "review" (the manifest will be read at
	// provision) or "post-mint" (the grant ran now).
	Stage string `json:"stage"`
}

// setGitHubForProposal applies a GitHub username to one proposal at whatever
// stage it is in, and audits it with the request's actor. Review stages use
// the field edit; onboarded and enrolled rows are written directly and the
// repo grant is re-run. Mid-mint and closed rows are refused with a reason.
func (s *Server) setGitHubForProposal(r *http.Request, p *onboardstore.Proposal, raw string) (*githubUpdate, *fieldEditError) {
	login, ferr := normalizeGitHubLogin(raw)
	if ferr != nil {
		return nil, ferr
	}
	switch p.Status {
	case onboardstore.ProposalProposed, onboardstore.ProposalSponsorApproved:
		edits, ferr := s.applyFieldEdits(r, p.ID, map[string]string{FieldIdentityGitHub: login})
		if ferr != nil {
			return nil, ferr
		}
		u := &githubUpdate{After: login, Stage: "review", Grants: []welcome.RepoGrant{}}
		for _, e := range edits {
			if e.Field == FieldIdentityGitHub {
				u.Before = e.Before
			}
		}
		return u, nil
	case onboardstore.ProposalOnboarded, onboardstore.ProposalEnrolled:
		return s.setGitHubPostMint(r, p, login)
	case onboardstore.ProposalApproved, onboardstore.ProposalOnboarding:
		return nil, &fieldEditError{Status: http.StatusConflict,
			Message: "access is being set up right now — try again in a few minutes, once onboarding has finished"}
	default:
		return nil, &fieldEditError{Status: http.StatusConflict,
			Message: "this onboarding record is " + p.Status + ", so there is no live access for a GitHub username to extend"}
	}
}

// setGitHubPostMint stores the login on an onboarded or enrolled proposal and
// re-runs the repo-access grant. The grant is fail-open (a GitHub error never
// fails the change); its outcomes come back so the caller can show them.
func (s *Server) setGitHubPostMint(r *http.Request, p *onboardstore.Proposal, login string) (*githubUpdate, *fieldEditError) {
	before, err := s.Onboard.SetProposalGitHub(r.Context(), p.ID, login)
	switch {
	case errors.Is(err, onboardstore.ErrNotFound):
		return nil, &fieldEditError{Status: http.StatusNotFound, Message: "proposal not found"}
	case errors.Is(err, onboardstore.ErrGitHubNotEditable):
		return nil, &fieldEditError{Status: http.StatusConflict,
			Message: "this record changed stage while the edit was in flight — reload and retry"}
	case err != nil:
		return nil, &fieldEditError{Status: http.StatusInternalServerError, Message: "could not store the GitHub username"}
	}
	// The grant re-runs even when the login is unchanged: a repeat is how a
	// person onboarded before the token existed, or before a repo joined the
	// policy, collects what they are owed.
	u := &githubUpdate{Before: before, After: login, Stage: "post-mint", Grants: []welcome.RepoGrant{}}
	if login != "" {
		fresh, gerr := s.Onboard.GetProposal(r.Context(), p.ID)
		if gerr == nil {
			if g := s.grantRepoAccess(context.WithoutCancel(r.Context()), fresh); g != nil {
				u.Grants = g
			}
		}
	}
	s.emitAudit(r, auditActor(r), "onboard_github_set", nil, "",
		map[string]any{"proposal_id": p.ID, FieldIdentityGitHub: before},
		map[string]any{"proposal_id": p.ID, FieldIdentityGitHub: login, "repos_granted": len(u.Grants)},
		false, "ok", "")
	return u, nil
}

// --- operator API ------------------------------------------------------------

// apiSetProposalGitHub is POST /api/onboard/proposals/{id}/github, body
// {"github": "<login>"} ("" removes it). Operator principals only: it can
// trigger repository grants for someone else.
func (s *Server) apiSetProposalGitHub(w http.ResponseWriter, r *http.Request) {
	if !requireOperatorPrincipal(w, r) {
		return
	}
	var body struct {
		GitHub string `json:"github"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		onboardError(w, http.StatusBadRequest, "decode", `body must be {"github": "<login>"}`)
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
	u, ferr := s.setGitHubForProposal(r, p, body.GitHub)
	if ferr != nil {
		onboardError(w, ferr.Status, "github_refused", ferr.Message)
		return
	}
	onboardJSON(w, http.StatusOK, u)
}

// --- admin browser door --------------------------------------------------------

// handleOnboardingGitHub is the GitHub username form on the Onboarded and
// Enrolled cards. Admin sessions only: like restore, it causes something to be
// granted to a person, which is not a sponsor's decision.
func (s *Server) handleOnboardingGitHub(w http.ResponseWriter, r *http.Request) {
	if s.Onboard == nil {
		s.renderError(w, r, 503, "onboarding is not configured")
		return
	}
	if err := s.validateCSRF(r); err != nil {
		s.renderError(w, r, 400, err.Error())
		return
	}
	if sponsorFromCtx(r.Context()) != nil {
		s.renderError(w, r, 403, "setting a person's GitHub username is an admin act; the person can also do it themselves at /github")
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
	u, ferr := s.setGitHubForProposal(r, p, r.FormValue("github"))
	if ferr != nil {
		s.renderError(w, r, ferr.Status, ferr.Message)
		return
	}
	q := url.Values{}
	q.Set("id", id)
	q.Set("status", p.Status)
	q.Set("flash", flashGitHubSet)
	if u.After == "" {
		q.Set("flash", flashGitHubCleared)
	}
	http.Redirect(w, r, "/onboarding?"+q.Encode(), http.StatusSeeOther)
}

// handleOnboardingRepoAccessView is the browser-session read of the effective
// repo-access set the review card draws. The card used to fetch the machine
// route (/api/onboard/proposals/{id}/repo-access), which takes only a bearer
// key, so in a browser it always failed and the card fell back to a generic
// "repos granted per policy at provision" line, never naming a repository.
// Sponsor sessions see only their own invitees, as on the page itself.
func (s *Server) handleOnboardingRepoAccessView(w http.ResponseWriter, r *http.Request) {
	if s.Onboard == nil {
		onboardError(w, http.StatusServiceUnavailable, "not_configured", "onboarding is not configured")
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
	if sp := sponsorFromCtx(r.Context()); sp != nil && !sponsorAllows(sp.keys(), p.Sponsor) {
		onboardError(w, http.StatusNotFound, "not_found", "proposal not found")
		return
	}
	ov, oerr := repoaccess.ParseOverrides(p.RepoOverridesJSON)
	if oerr != nil {
		ov = repoaccess.Overrides{}
	}
	onboardJSON(w, http.StatusOK, s.loadRepoAccessView(p, ov))
}

// --- self-service door -----------------------------------------------------------

// handleGitHubSelf renders /github: the signed-in person's own GitHub username
// and what it grants.
func (s *Server) handleGitHubSelf(w http.ResponseWriter, r *http.Request) {
	s.renderGitHubSelf(w, r, nil, nil)
}

// handleGitHubSelfSubmit stores the signed-in person's GitHub username on
// their own onboarding record. The record is found from the SESSION email; the
// form never names whose record to change.
func (s *Server) handleGitHubSelfSubmit(w http.ResponseWriter, r *http.Request) {
	if err := s.validateCSRF(r); err != nil {
		s.renderError(w, r, http.StatusForbidden, err.Error())
		return
	}
	p := s.ownProposal(r)
	if p == nil {
		s.renderGitHubSelf(w, r, &flash{Level: "error",
			Message: "There is no onboarding record for your address on this goatnet, so there is nothing to attach a GitHub username to. Ask the person who invited you."}, nil)
		return
	}
	u, ferr := s.setGitHubForProposal(r, p, r.FormValue("github"))
	if ferr != nil {
		s.renderGitHubSelf(w, r, &flash{Level: "error", Message: ferr.Message}, nil)
		return
	}
	var msg string
	switch {
	case u.After == "":
		msg = "Removed. Repositories you were already added to keep you until an admin removes you."
	case u.Stage == "review":
		msg = "Saved. Your request is still being reviewed; the repositories are granted when your access is set up."
	case len(u.Grants) == 0:
		msg = "Saved. No repository invitation was sent just now: either your level includes none, or this goatnet cannot send invitations yet. An admin can tell you which."
	default:
		msg = "Saved. GitHub has been asked to invite you to the repositories below; accept each invitation from your GitHub notifications or email."
	}
	s.renderGitHubSelf(w, r, &flash{Level: "success", Message: msg}, u.Grants)
}

// ownProposal is the signed-in person's own live onboarding record, or nil.
func (s *Server) ownProposal(r *http.Request) *onboardstore.Proposal {
	if s.Onboard == nil {
		return nil
	}
	email := strings.TrimSpace(actorFromCtx(r.Context()).Email)
	p, ok, err := s.Onboard.ActiveProposalForEmail(r.Context(), email)
	if err != nil || !ok {
		return nil
	}
	return p
}

func (s *Server) renderGitHubSelf(w http.ResponseWriter, r *http.Request, f *flash, granted []welcome.RepoGrant) {
	vars := &pageVars{Title: "Your GitHub username"}
	if f != nil {
		vars.Flashes = flashes(f)
	}
	gv := &githubSelfView{Consequence: githubMissingConsequence, Granted: granted}
	if p := s.ownProposal(r); p != nil {
		gv.HasRecord = true
		gv.Status = p.Status
		_, gv.Current = manifestIdentity(p.ManifestJSON)
		if subject, ok := s.repoSubjectDerived(p); ok {
			if cfg, err := repoaccess.Load(s.Cfg.RepoAccessConfigPath); err == nil && cfg != nil {
				// A malformed legacy preview degrades to base grants; this view never authorizes access.
				ov, _ := repoaccess.ParseOverrides(p.RepoOverridesJSON) //nolint:errcheck // display-only preview
				for _, g := range cfg.WouldGrant(subject, ov) {
					gv.Repos = append(gv.Repos, g.Owner+"/"+g.Repo)
				}
			}
		}
	}
	vars.GitHubSelf = gv
	s.render(w, r, "github", vars)
}

// githubSelfView is what /github shows.
type githubSelfView struct {
	HasRecord   bool
	Status      string
	Current     string
	Repos       []string // what this level grants with a username on file
	Granted     []welcome.RepoGrant
	Consequence string
}
