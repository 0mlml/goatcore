package handlers

// onboard_repo_access.go — the "audible" repo-access API: GET the effective set
// (policy ⊕ overrides) for a proposal, and POST a new override set (the caller
// sends the full desired override, which REPLACES the stored one). This is the
// per-onboardee lever an operator/sponsor pulls at sponsor-approve / admin-approve
// / provision to add a repo the on-rails policy wouldn't grant, or drop one it
// would (docs/design/onboarding-github-repo-grants.md §2/§4).
//
// Reads are scoped like apiGetProposal (a non-escalated participant sponsor sees
// only their own invitees; foreign ids 404, no existence leak). Writes are
// audited and refused once the
// proposal is past `onboarded` (the grants have already fired).

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/onboardstore"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/repoaccess"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/welcome"
)

// repoAccessView is the GET/POST response: the computed effective set, the stored
// overrides that produced it, the captured github username, and whether a token
// is configured (so the UI can warn that grants will be skipped without one).
type repoAccessView struct {
	Effective []repoaccess.EffectiveGrant `json:"effective"`
	Overrides repoaccess.Overrides        `json:"overrides"`
	GitHub    string                      `json:"github"`
	HasToken  bool                        `json:"has_token"`
	// Missing is what the effective set WOULD be with a GitHub username on
	// file, present only while there is none: the repositories this person is
	// currently left out of. Effective is empty in that case (no login, no
	// grant), which on its own reads as "the policy grants nothing".
	Missing []repoaccess.EffectiveGrant `json:"missing_without_github,omitempty"`
}

// repoAccessSetRequest is the POST body: the FULL desired override set (it
// replaces the stored one). Grant reuses repoaccess.Grant (repo + permission);
// remove targets a repo by "owner/name" or "name".
type repoAccessSetRequest struct {
	Add    []repoaccess.Grant   `json:"add"`
	Remove []repoaccess.RepoRef `json:"remove"`
}

// loadRepoAccessView computes the effective set for a proposal from the live
// policy + the proposal's manifest Subject + the given overrides.
func (s *Server) loadRepoAccessView(p *onboardstore.Proposal, ov repoaccess.Overrides) repoAccessView {
	subject, ok := s.repoSubjectDerived(p)
	view := repoAccessView{
		Overrides: ov,
		HasToken:  strings.TrimSpace(s.Cfg.GitHubOnboardToken) != "",
	}
	if !ok {
		// Unparseable manifest → no subject; still report the overrides + token
		// state so the UI degrades honestly rather than erroring.
		view.Effective = []repoaccess.EffectiveGrant{}
		return view
	}
	view.GitHub = subject.GitHubUsername
	// The policy is loaded FRESH (repo/policy changes are an operator edit, never
	// a redeploy). A load error → nil config, which Effective handles (adds still
	// apply); a missing file is (nil,nil) — feature off, adds still apply.
	cfg, err := repoaccess.Load(s.Cfg.RepoAccessConfigPath)
	if err != nil {
		// Best-effort: treat an unreadable policy as "no policy" so
		// explicit per-onboardee overrides still apply.
		cfg = nil
	}
	eff := cfg.Effective(subject, ov)
	if eff == nil {
		eff = []repoaccess.EffectiveGrant{}
	}
	view.Effective = eff
	if strings.TrimSpace(subject.GitHubUsername) == "" {
		view.Missing = cfg.WouldGrant(subject, ov)
	}
	return view
}

// apiGetProposalRepoAccess returns the effective repo-access set for a proposal.
// Scoped like apiGetProposal (foreign ids 404).
func (s *Server) apiGetProposalRepoAccess(w http.ResponseWriter, r *http.Request) {
	pr := onboardPrincipalFromCtx(r.Context())
	p, err := s.Onboard.GetProposal(r.Context(), r.PathValue("id"))
	if errors.Is(err, onboardstore.ErrNotFound) {
		onboardError(w, http.StatusNotFound, "not_found", "proposal not found")
		return
	}
	if err != nil {
		onboardError(w, http.StatusInternalServerError, "get_error", "could not load proposal")
		return
	}
	if !pr.allowsSponsor(p.Sponsor) {
		onboardError(w, http.StatusNotFound, "not_found", "proposal not found")
		return
	}
	ov, oerr := repoaccess.ParseOverrides(p.RepoOverridesJSON)
	if oerr != nil {
		// A malformed stored override reads as empty — never a 500 on a GET.
		ov = repoaccess.Overrides{}
	}
	onboardJSON(w, http.StatusOK, s.loadRepoAccessView(p, ov))
}

// apiSetProposalRepoAccess REPLACES the stored override set with the posted one,
// validates it, persists it, audits the actor + action, and returns the
// new effective set. Refused once the proposal is past `onboarded` (the grants
// have already fired — there is nothing left to influence).
func (s *Server) apiSetProposalRepoAccess(w http.ResponseWriter, r *http.Request) {
	// H1: repo-access audibles are operator-only. The POST body carries a
	// free-text repo string and accepts `admin` as a permission, so a
	// sponsorship-authorized participant reaching this on their own invitee's
	// proposal could grant themselves GitHub admin on an arbitrary repo. The
	// GET stays sponsor-scoped (a sponsor may READ the effective set for their
	// own invitee); only the mutation is gated, matching every other mutating
	// onboard verb (requireOperatorPrincipal). Fail closed.
	if !requireOperatorPrincipal(w, r) {
		return
	}
	pr := onboardPrincipalFromCtx(r.Context())
	id := r.PathValue("id")
	p, err := s.Onboard.GetProposal(r.Context(), id)
	if errors.Is(err, onboardstore.ErrNotFound) {
		onboardError(w, http.StatusNotFound, "not_found", "proposal not found")
		return
	}
	if err != nil {
		onboardError(w, http.StatusInternalServerError, "get_error", "could not load proposal")
		return
	}
	if !pr.allowsSponsor(p.Sponsor) {
		onboardError(w, http.StatusNotFound, "not_found", "proposal not found")
		return
	}
	s.applyRepoOverrides(w, r, p, onboardAPIActor(r), true)
}

// handleOnboardingRepoAccessSet is the browser-session write behind the review
// card's Repo access panel. The /api/onboard/... route takes only a bearer key,
// which a browser session does not hold, so the panel's saves could not succeed
// there. Admin sessions only, as with the API's operator rule: the body names a
// free-text repo, so a sponsor must not reach it. GitHub `admin` permission is
// reserved to platform admins. The body is JSON, so CSRF travels in a header.
func (s *Server) handleOnboardingRepoAccessSet(w http.ResponseWriter, r *http.Request) {
	if s.Onboard == nil {
		onboardError(w, http.StatusServiceUnavailable, "not_configured", "onboarding is not configured")
		return
	}
	if tok := r.Header.Get("X-CSRF-Token"); tok == "" || tok != s.csrfNonce {
		onboardError(w, http.StatusBadRequest, "bad_csrf", "CSRF token mismatch — refresh the page and retry")
		return
	}
	if sponsorFromCtx(r.Context()) != nil {
		onboardError(w, http.StatusForbidden, "admin_only", "changing a person's repo access is an admin act")
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
	actor := actorFromCtx(r.Context())
	s.applyRepoOverrides(w, r, p, actor.Email, slices.Contains(actor.Groups, "PLATFORM-ADMIN"))
}

// repoAccessSetResponse is the effective set after a change, plus the outcome of
// each grant when the change fired them (a person already onboarded).
type repoAccessSetResponse struct {
	repoAccessView
	Grants []welcome.RepoGrant `json:"grants,omitempty"`
}

// applyRepoOverrides validates and stores a person's audible and answers with the
// effective set. Before onboarding, the audible is applied at provision. For a
// person already onboarded or enrolled, the grant fires now, and only adds are
// accepted: dropping a repo from the set would not take it away on GitHub, so the
// panel must not suggest it did.
func (s *Server) applyRepoOverrides(w http.ResponseWriter, r *http.Request, p *onboardstore.Proposal, actor string, allowAdminPerm bool) {
	var req repoAccessSetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		onboardError(w, http.StatusBadRequest, "bad_json", "body must be JSON: {add:[{repo,permission}], remove:[{repo}]}")
		return
	}
	// Validate: adds need a non-empty repo + a permission in the allowed set;
	// removes need a non-empty repo. Normalize a blank permission to pull.
	add := make([]repoaccess.Grant, 0, len(req.Add))
	for _, a := range req.Add {
		repo := strings.TrimSpace(a.Repo)
		if repo == "" {
			onboardError(w, http.StatusBadRequest, "bad_add", "each add needs a non-empty repo")
			return
		}
		perm := strings.ToLower(strings.TrimSpace(a.Permission))
		if perm == "" {
			perm = "pull"
		}
		if !repoaccess.ValidPermission(perm) {
			onboardError(w, http.StatusBadRequest, "bad_permission",
				"permission must be one of pull|triage|push|maintain|admin")
			return
		}
		if perm == "admin" && !allowAdminPerm {
			onboardError(w, http.StatusForbidden, "admin_permission_refused",
				"only a platform admin can grant GitHub admin on a repo")
			return
		}
		add = append(add, repoaccess.Grant{Repo: repo, Permission: perm})
	}
	remove := make([]repoaccess.RepoRef, 0, len(req.Remove))
	for _, rm := range req.Remove {
		repo := strings.TrimSpace(rm.Repo)
		if repo == "" {
			onboardError(w, http.StatusBadRequest, "bad_remove", "each remove needs a non-empty repo")
			return
		}
		remove = append(remove, repoaccess.RepoRef{Owner: strings.TrimSpace(rm.Owner), Repo: repo})
	}
	ov := repoaccess.Overrides{Add: add, Remove: remove}

	postMint := p.Status == onboardstore.ProposalOnboarded || p.Status == onboardstore.ProposalEnrolled
	if postMint {
		prev, perr := repoaccess.ParseOverrides(p.RepoOverridesJSON)
		if perr != nil {
			prev = repoaccess.Overrides{}
		}
		if repoOverridesDropAccess(prev, ov) {
			onboardError(w, http.StatusConflict, "remove_on_github",
				"this person is already onboarded, so removing a repo here would not take it away; remove them as a collaborator on GitHub")
			return
		}
	}

	b, merr := json.Marshal(ov)
	if merr != nil {
		onboardError(w, http.StatusInternalServerError, "encode_error", "could not encode overrides")
		return
	}
	if err := s.Onboard.SetProposalRepoOverrides(r.Context(), p.ID, string(b)); err != nil {
		if errors.Is(err, onboardstore.ErrNotFound) {
			onboardError(w, http.StatusNotFound, "not_found", "proposal not found")
			return
		}
		onboardError(w, http.StatusInternalServerError, "store_error", "could not persist overrides")
		return
	}
	p.RepoOverridesJSON = string(b)
	// F-207: attribute the audible to the resolved per-handle machine actor.
	s.emitAuditWithInviteToken(r, Actor{Email: actor}, "onboard_repo_access_override",
		p.Handle, "", nil,
		map[string]interface{}{"env": p.Env, "adds": len(add), "removes": len(remove), "post_mint": postMint},
		false, "ok", "")

	resp := repoAccessSetResponse{repoAccessView: s.loadRepoAccessView(p, ov)}
	if postMint {
		// The grant is fail-open and its outcomes come back to the caller, so a
		// repo the token cannot see is named rather than silently missing.
		resp.Grants = s.grantRepoAccess(context.WithoutCancel(r.Context()), p)
	}
	onboardJSON(w, http.StatusOK, resp)
}

// repoOverridesDropAccess reports whether next takes away something prev had or
// removes anything new. Either would change the set without changing GitHub.
func repoOverridesDropAccess(prev, next repoaccess.Overrides) bool {
	for _, was := range prev.Add {
		if !slices.ContainsFunc(next.Add, func(g repoaccess.Grant) bool { return strings.EqualFold(g.Repo, was.Repo) }) {
			return true
		}
	}
	for _, rm := range next.Remove {
		if !slices.ContainsFunc(prev.Remove, func(o repoaccess.RepoRef) bool { return strings.EqualFold(o.Repo, rm.Repo) }) {
			return true
		}
	}
	return false
}

// apiRepoAccessPolicy returns the loaded on-rails repo-access policy (default
// owner + rules) — the `goat access repo policy` view. A missing policy file is
// not an error: it returns configured=false so the caller renders "no policy
// (feature off — audibles still apply)". A malformed file is a 500.
func (s *Server) apiRepoAccessPolicy(w http.ResponseWriter, r *http.Request) {
	cfg, err := repoaccess.Load(s.Cfg.RepoAccessConfigPath)
	if err != nil {
		onboardError(w, http.StatusInternalServerError, "policy_error", "could not load repo-access policy")
		return
	}
	if cfg == nil {
		onboardJSON(w, http.StatusOK, map[string]any{
			"default_owner": "", "rules": []any{}, "configured": false,
		})
		return
	}
	onboardJSON(w, http.StatusOK, map[string]any{
		"default_owner": cfg.DefaultOwner,
		"rules":         cfg.Rules,
		"configured":    true,
	})
}
