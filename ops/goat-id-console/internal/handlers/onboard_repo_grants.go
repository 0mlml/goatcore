package handlers

// onboard_repo_grants.go — the provision-time GitHub repo-collaborator grant
// hook (docs/design/onboarding-github-repo-grants.md). After the runner reports
// a proposal `onboarded`, the console reads repo-access.yaml FRESH, evaluates it
// against the person's tier + affiliation attributes + captured github username,
// and adds them as a collaborator to each granted repo.
//
// FAIL-OPEN: the token being unset, the config being absent, or a GitHub API
// error on any repo is logged + recorded but NEVER fails the onboard or blocks
// the welcome. The outcomes are persisted on the proposal so the welcome package
// can surface which repos the person was added to.

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/coalition"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/onboardstore"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/repoaccess"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/welcome"
)

// repoGrantHTTPClient is the client the GitHub collaborator calls use; nil is
// http.DefaultClient. Tests replace it to observe the calls without network.
var repoGrantHTTPClient *http.Client

// repoGrantManifest is the subset of the onboard manifest the repo-grant
// evaluator reads: the github login + the affiliation attributes the policy
// matches on. The tier comes off the proposal row (denormalized).
type repoGrantManifest struct {
	Identity struct {
		GitHub string `json:"github"`
	} `json:"identity"`
	Affiliation struct {
		Kind string `json:"kind"`
		// `alliance_standing` is the field; the legacy `coalition` key is still accepted.
		Standing  string `json:"alliance_standing"`
		Coalition string `json:"coalition"`
		Sector    string `json:"sector"`
		Nation    string `json:"nation"`
	} `json:"affiliation"`
}

// The subject builder is deliberately NOT offered in a non-deriving form. It
// was, briefly, and the preview endpoint picked it up: the grant path derived
// coalition standing while the endpoint an operator READS to decide did not,
// so the two disagreed about the same person. Every caller inside the server
// takes s.repoSubjectDerived; repoSubjectWith is exported to tests only so the
// derivation itself can be exercised with an explicit registry.
//
// repoSubjectWith builds the repoaccess.Subject the policy is evaluated against from
// a proposal: the captured github login + affiliation attributes off the manifest,
// with the tier off the (denormalized) proposal row. Returns ok=false when the
// manifest can't be parsed (a caller then treats the person as ungrantable). The
// github username may be blank — the effective set is still computable for a
// review, but a grant is only attempted when a login is present.
// repoSubjectWith resolves the policy subject, DERIVING coalition standing from
// nation when the manifest does not carry it.
//
// Why the derivation is not optional: coalition is captured at intake only when
// a candidate explicitly picks it, and most candidates do not. A policy rule
// keyed on coalition would therefore look correct, evaluate cleanly, and grant
// almost nobody: the same shape of silent nothing as the missing token this
// file already documents.
//
// Standing IS derivable — it is nation-derived against the goatnet's active
// framework (ADR 1077) — and the console already loads that registry for the
// identity page's standing row. Deriving here reuses that one resolver instead
// of adding a second copy of the rule. An explicit manifest value always WINS:
// a candidate or reviewer who stated a standing is not overridden by a
// computation.
func repoSubjectWith(p *onboardstore.Proposal, reg *coalition.Registry, framework string) (repoaccess.Subject, bool) {
	var m repoGrantManifest
	if err := json.Unmarshal([]byte(p.ManifestJSON), &m); err != nil {
		return repoaccess.Subject{}, false
	}
	coal := strings.TrimSpace(m.Affiliation.Coalition)
	if coal == "" && reg != nil && strings.TrimSpace(m.Affiliation.Nation) != "" {
		coal = reg.Resolve(m.Affiliation.Nation, framework, time.Now().UTC()).Standing
	}
	return repoaccess.Subject{
		Tier:            p.Tier,
		AffiliationKind: m.Affiliation.Kind,
		Coalition:       coal,
		Sector:          m.Affiliation.Sector,
		Nation:          m.Affiliation.Nation,
		GitHubUsername:  strings.TrimSpace(m.Identity.GitHub),
	}, true
}

// grantRepoAccess evaluates the repo-access policy for a freshly-onboarded
// proposal and grants each matched repo via the GitHub API. It is a no-op (and
// never an error) when the token is unset, the config is absent, or the person
// has no github username. Outcomes are persisted on the proposal (fail-open) so
// the welcome package surfaces them. Returns the grant outcomes for immediate
// use (e.g. the auto-email path) — nil when nothing was attempted.
func (s *Server) grantRepoAccess(ctx context.Context, p *onboardstore.Proposal) []welcome.RepoGrant {
	token := strings.TrimSpace(s.Cfg.GitHubOnboardToken)
	if token == "" {
		// Degrading to no-op is right — a missing token must never fail an
		// onboard. Doing it SILENTLY is not: with the policy correct and
		// GITHUB_ONBOARD_TOKEN empty, every onboardee records an empty grant list
		// that is indistinguishable from "the policy matched nothing".
		//
		// Warn only when a grant would OTHERWISE have been attempted — a
		// goatnet with no policy or an onboardee with no github username is a
		// legitimate quiet no-op and should not cry wolf.
		if subject, ok := s.repoSubjectDerived(p); ok && subject.GitHubUsername != "" {
			if cfg, err := repoaccess.Load(s.Cfg.RepoAccessConfigPath); err == nil && cfg != nil {
				slog.Warn("repo-grant: SKIPPED — GITHUB_ONBOARD_TOKEN is unset, so the "+
					"deployed repo-access policy granted nothing; the empty grant list on "+
					"this proposal is a missing credential, not a policy decision",
					"id", p.ID, "github", subject.GitHubUsername,
					"policy", s.Cfg.RepoAccessConfigPath)
			}
		}
		return nil
	}
	subject, ok := s.repoSubjectDerived(p)
	if !ok {
		slog.Warn("repo-grant: manifest parse failed", "id", p.ID)
		return nil
	}
	gh := subject.GitHubUsername
	if gh == "" {
		return nil // nothing to grant without a github login
	}
	cfg, err := repoaccess.Load(s.Cfg.RepoAccessConfigPath)
	if err != nil {
		slog.Warn("repo-grant: config load failed", "path", s.Cfg.RepoAccessConfigPath, "err", err)
		return nil
	}
	// Per-onboardee overrides (the "audible" layer): even when cfg is nil (no
	// policy file), an override with adds still proceeds — a goatnet with no
	// policy can still audible a repo onto a specific onboardee.
	ov, oerr := repoaccess.ParseOverrides(p.RepoOverridesJSON)
	if oerr != nil {
		// Fail-open: a malformed override never fails the onboard — fall back to
		// the policy alone (empty overrides).
		slog.Warn("repo-grant: override parse failed", "id", p.ID, "err", oerr)
		ov = repoaccess.Overrides{}
	}
	if cfg == nil && len(ov.Add) == 0 {
		return nil // absent file + no audible adds = nothing to do
	}
	resolved := cfg.Effective(subject, ov)
	if len(resolved) == 0 {
		return nil
	}
	out := make([]welcome.RepoGrant, 0, len(resolved))
	for _, g := range resolved {
		outcome, gerr := repoaccess.AddCollaborator(ctx, repoGrantHTTPClient, token, g.Owner, g.Repo, gh, g.Permission)
		if gerr != nil {
			// Fail-open: record the failure, keep going, never fail the onboard.
			slog.Warn("repo-grant: add collaborator failed",
				"repo", g.Owner+"/"+g.Repo, "user", gh, "source", g.Source, "err", gerr)
		} else {
			slog.Info("repo-grant: collaborator added",
				"repo", g.Owner+"/"+g.Repo, "user", gh, "permission", g.Permission, "source", g.Source, "outcome", outcome)
		}
		out = append(out, repoGrantRecord(g.Owner, g.Repo, g.Permission, g.Source, outcome, gerr))
	}
	// Persist the outcomes on the proposal so the welcome-download endpoint (which
	// reloads from disk) surfaces them too. Fail-open on a store error.
	if b, merr := json.Marshal(out); merr == nil {
		if serr := s.Onboard.SetProposalRepoGrants(ctx, p.ID, string(b)); serr != nil {
			slog.Warn("repo-grant: persist outcomes failed", "id", p.ID, "err", serr)
		} else {
			p.RepoGrantsJSON = string(b) // reflect on the in-memory proposal for the welcome
		}
	}
	return out
}

// repoSubjectDerived is repoSubject with this server's coalition registry and
// active framework applied — the form every in-server caller wants.
func (s *Server) repoSubjectDerived(p *onboardstore.Proposal) (repoaccess.Subject, bool) {
	return repoSubjectWith(p, s.Coalition, s.Cfg.CoalitionFramework)
}

// repoGrantRecord is the persisted row for one attempted grant. A failure keeps
// GitHub's status and a secret-free class (repoaccess.FailureClass), so the
// stored proposal says why a grant failed after the console log has rotated
// (#2107). No token and no response body reach the record.
func repoGrantRecord(owner, repo, permission, source string, outcome repoaccess.Outcome, err error) welcome.RepoGrant {
	rec := welcome.RepoGrant{
		Owner:      owner,
		Repo:       repo,
		Permission: permission,
		Outcome:    string(outcome),
		Source:     source,
	}
	if err != nil {
		var he *repoaccess.HTTPError
		if errors.As(err, &he) {
			rec.HTTPStatus = he.Status
		}
		rec.Failure = repoaccess.FailureClass(err)
	}
	return rec
}
