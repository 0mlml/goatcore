// Package repoaccess evaluates a declarative role/identity → GitHub repo
// collaborator policy at onboarding and grants access via the GitHub API.
//
// CONFIG-DRIVEN BY DESIGN (docs/design/onboarding-github-repo-grants.md): repos
// and the role→repo policy change over time, so the mapping is a config file the
// console reads FRESH at each provision — adding/removing a repo is an operator
// edit, never a code change or redeploy. The policy shape mirrors
// ops/goat-id-console/config/repo-access.example.yaml. The feature is fully
// optional (absent file = off) and fail-open (a grant failure never fails the
// onboard).
package repoaccess

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config is the parsed repo-access policy (repo-access.yaml).
type Config struct {
	Version      int    `yaml:"version"`
	DefaultOwner string `yaml:"default_owner"`
	Rules        []Rule `yaml:"rules"`
}

// Rule is one named policy rule: everyone matching Match receives its Grant set.
type Rule struct {
	Name        string  `yaml:"name"`
	Description string  `yaml:"description"`
	Match       Match   `yaml:"match"`
	Grant       []Grant `yaml:"grant"`
}

// Match is AND across non-empty keys, OR within a key's list (case-insensitive).
// An empty list is a wildcard for that key; an all-empty Match matches everyone
// (who has a github username).
type Match struct {
	Tier            []string `yaml:"tier"`
	AffiliationKind []string `yaml:"affiliation_kind"`
	// `alliance_standing` is the preferred matcher key; `coalition` still honoured.
	// Both are honoured -- a policy file may use either, and a rule matches
	// if EITHER list matches, so existing policy files keep working unchanged.
	Standing  []string `yaml:"alliance_standing"`
	Coalition []string `yaml:"coalition"`
	Sector    []string `yaml:"sector"`
	Nation    []string `yaml:"nation"`
}

// Grant is one repo + permission to add the subject to. Repo is "name" (owner
// defaults to Config.DefaultOwner) or "owner/name".
type Grant struct {
	Repo       string `yaml:"repo"`
	Permission string `yaml:"permission"`
}

// Load reads + parses the policy file at path. A missing file returns (nil,nil)
// — the feature is off, not an error. A malformed file is a real error.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read repo-access config: %w", err)
	}
	var c Config
	if err := yaml.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("parse repo-access config: %w", err)
	}
	return &c, nil
}

// Subject is the resolved onboarding identity the policy is evaluated against.
type Subject struct {
	Tier            string
	AffiliationKind string
	Coalition       string
	Sector          string
	Nation          string
	GitHubUsername  string
}

// ResolvedGrant is one owner/repo the subject should be added to, at the
// strongest permission the matched rules granted.
type ResolvedGrant struct {
	Owner      string
	Repo       string
	Permission string
}

// permRank orders GitHub collaborator permissions weakest→strongest so a
// dedupe keeps the strongest grant when multiple rules target the same repo.
var permRank = map[string]int{
	"pull":     1,
	"triage":   2,
	"push":     3,
	"maintain": 4,
	"admin":    5,
}

// ValidPermission reports whether perm is one of the GitHub collaborator
// permission levels the policy + audibles accept (case-insensitive).
func ValidPermission(perm string) bool {
	_, ok := permRank[strings.ToLower(strings.TrimSpace(perm))]
	return ok
}

// Grants evaluates the policy against s and returns the deduped set of repos to
// grant (strongest permission wins per owner/repo). Returns nothing when s has
// no github username — a grant is only ever attempted for a supplied login.
func (c *Config) Grants(s Subject) []ResolvedGrant {
	if c == nil || strings.TrimSpace(s.GitHubUsername) == "" {
		return nil
	}
	// Accumulate best-permission per "owner/repo" and preserve first-seen order.
	best := map[string]ResolvedGrant{}
	var order []string
	for _, rule := range c.Rules {
		if !rule.Match.matches(s) {
			continue
		}
		for _, g := range rule.Grant {
			owner, repo := c.resolveRepo(g.Repo)
			if repo == "" {
				continue
			}
			perm := strings.ToLower(strings.TrimSpace(g.Permission))
			key := strings.ToLower(owner) + "/" + strings.ToLower(repo)
			cur, seen := best[key]
			if !seen {
				order = append(order, key)
				best[key] = ResolvedGrant{Owner: owner, Repo: repo, Permission: perm}
				continue
			}
			if permRank[perm] > permRank[cur.Permission] {
				cur.Permission = perm
				best[key] = cur
			}
		}
	}
	out := make([]ResolvedGrant, 0, len(order))
	for _, key := range order {
		out = append(out, best[key])
	}
	return out
}

// --- audibles: per-onboardee overrides on top of the on-rails policy ---------
//
// The policy (Grants) is the "on-rails" default; an operator can AUDIBLE it per
// onboardee at sponsor-approve / admin-approve / provision — add a repo the
// policy wouldn't grant, or drop one it would. The effective set (Effective) is
// the policy composed with these overrides, each tagged with its Source so the
// review card + welcome package show WHY a repo is on the list
// (docs/design/onboarding-github-repo-grants.md §2).

// RepoRef names one repo (an override "remove" target, or the resolved owner of
// an "add"). Owner defaults to Config.DefaultOwner when unqualified.
type RepoRef struct {
	Owner string `json:"owner,omitempty"`
	Repo  string `json:"repo"`
}

// Overrides is the per-onboardee audible: repos to Add on top of the policy, and
// repos to Remove from it. Add reuses Grant (Repo carries "owner/name" or "name";
// the owner resolves via resolveRepo / DefaultOwner). Remove matches owner/repo
// case-insensitively and wins over both the policy and any Add.
type Overrides struct {
	Add    []Grant   `json:"add"`
	Remove []RepoRef `json:"remove"`
}

// EffectiveGrant is one owner/repo the subject should be added to in the final,
// override-composed set, tagged with the Source that put it there.
type EffectiveGrant struct {
	Owner      string `json:"owner"`
	Repo       string `json:"repo"`
	Permission string `json:"permission"`
	Source     string `json:"source"` // "policy" | "audible"
}

// Source values for EffectiveGrant.
const (
	SourcePolicy  = "policy"
	SourceAudible = "audible"
)

// ParseOverrides decodes an Overrides from JSON. "" or "{}" (the stored default)
// decode to an empty Overrides, not an error.
func ParseOverrides(s string) (Overrides, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "{}" {
		return Overrides{}, nil
	}
	var ov Overrides
	if err := json.Unmarshal([]byte(s), &ov); err != nil {
		return Overrides{}, fmt.Errorf("parse repo overrides: %w", err)
	}
	return ov, nil
}

// Effective computes the final grant set: the on-rails policy (Source=policy)
// composed with the per-onboardee overrides. Adds append a new repo (Source=
// audible) or, when the repo is already present, keep the STRONGER permission —
// tagging the row audible only when the add actually raised it. Removes drop a
// matching owner/repo case-insensitively and win over both policy and add. The
// result is deduped by owner/repo (case-insensitive), first-seen order preserved.
//
// A nil config is not an error: a goatnet with no policy file can still audible
// (only the adds appear). A subject with no github username still yields nothing
// from the policy, but an explicit audible add is honored — the caller decides
// whether to attempt it (a grant needs a username to target).
func (c *Config) Effective(s Subject, ov Overrides) []EffectiveGrant {
	best := map[string]*EffectiveGrant{}
	var order []string
	upsert := func(g EffectiveGrant) {
		key := strings.ToLower(g.Owner) + "/" + strings.ToLower(g.Repo)
		if cur, seen := best[key]; seen {
			// Repo already present: keep the stronger permission. Tag audible only
			// when the add actually raised the level over what was there.
			if permRank[g.Permission] > permRank[cur.Permission] {
				cur.Permission = g.Permission
				if g.Source == SourceAudible {
					cur.Source = SourceAudible
				}
			}
			return
		}
		gg := g
		best[key] = &gg
		order = append(order, key)
	}

	// 1) Policy grants (Source=policy). Grants already dedupes strongest-wins.
	for _, g := range c.Grants(s) {
		upsert(EffectiveGrant{Owner: g.Owner, Repo: g.Repo, Permission: g.Permission, Source: SourcePolicy})
	}
	// 2) Audible adds. resolveRepo defaults the owner; a nil config still resolves
	//    an unqualified repo to a blank owner (rare — an add should carry owner/name),
	//    which is honored as-is.
	for _, a := range ov.Add {
		owner, repo := c.resolveRepo(a.Repo)
		if repo == "" {
			continue
		}
		perm := strings.ToLower(strings.TrimSpace(a.Permission))
		if perm == "" {
			perm = "pull"
		}
		upsert(EffectiveGrant{Owner: owner, Repo: repo, Permission: perm, Source: SourceAudible})
	}
	// 3) Audible removes — win over policy AND add.
	for _, rm := range ov.Remove {
		owner, repo := c.resolveRepo(rm.Repo)
		if repo == "" {
			repo = strings.TrimSpace(rm.Repo)
		}
		// A remove may carry an explicit owner via RepoRef.Owner or "owner/name".
		if o := strings.TrimSpace(rm.Owner); o != "" && !strings.Contains(rm.Repo, "/") {
			owner = o
		}
		key := strings.ToLower(owner) + "/" + strings.ToLower(repo)
		delete(best, key)
	}

	out := make([]EffectiveGrant, 0, len(order))
	for _, key := range order {
		if g, ok := best[key]; ok {
			out = append(out, *g)
		}
	}
	return out
}

// resolveRepo splits an "owner/name" repo value, defaulting the owner to
// DefaultOwner when unqualified.
func (c *Config) resolveRepo(repo string) (owner, name string) {
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return "", ""
	}
	if o, n, ok := strings.Cut(repo, "/"); ok {
		return strings.TrimSpace(o), strings.TrimSpace(n)
	}
	// nil config (no policy file) still resolves an "owner/name" add; an
	// unqualified name has no default owner to fall back on.
	if c == nil {
		return "", repo
	}
	return strings.TrimSpace(c.DefaultOwner), repo
}

// matches reports whether the subject satisfies this Match: AND across the
// non-empty keys, OR (case-insensitive) within each key's list; an empty list
// is a wildcard.
func (m Match) matches(s Subject) bool {
	return listMatch(m.Tier, s.Tier) &&
		listMatch(m.AffiliationKind, s.AffiliationKind) &&
		standingMatch(m, s) &&
		listMatch(m.Sector, s.Sector) &&
		listMatch(m.Nation, s.Nation)
}

// standingMatch honours both spellings of the alliance-standing matcher key
// (ADR 1148 renamed `coalition` to `standing`). A rule matches when EITHER
// declared list matches, so a policy file written against either name keeps
// working and a file may be migrated one rule at a time. Both empty is a
// wildcard, which is listMatch's existing behaviour.
func standingMatch(m Match, s Subject) bool {
	hasStanding, hasCoalition := len(m.Standing) > 0, len(m.Coalition) > 0
	switch {
	case !hasStanding && !hasCoalition:
		return true // neither key declared — wildcard, as listMatch would be
	case hasStanding && hasCoalition:
		// Both declared: either may satisfy the rule, so a file can be migrated
		// key-by-key without a rule briefly matching nobody.
		return listMatch(m.Standing, s.Coalition) || listMatch(m.Coalition, s.Coalition)
	case hasStanding:
		return listMatch(m.Standing, s.Coalition)
	default:
		return listMatch(m.Coalition, s.Coalition)
	}
}

// listMatch reports whether want is in list (case-insensitive). An empty list
// is a wildcard (always matches).
func listMatch(list []string, want string) bool {
	if len(list) == 0 {
		return true
	}
	for _, v := range list {
		if strings.EqualFold(strings.TrimSpace(v), strings.TrimSpace(want)) {
			return true
		}
	}
	return false
}

// Outcome is the interpreted result of a collaborator add.
type Outcome string

const (
	OutcomeAdded   Outcome = "added"   // 204 — already had access / added directly
	OutcomeInvited Outcome = "invited" // 201 — an invitation was created
	OutcomeFailed  Outcome = "failed"
)

// AddCollaborator adds username to owner/repo at the given permission via the
// GitHub API (PUT /repos/{owner}/{repo}/collaborators/{username}). 201 → an
// invitation was created (OutcomeInvited); 204 → the user already had access or
// was added directly (OutcomeAdded); any other status is an error carrying the
// status + a body snippet.
func AddCollaborator(ctx context.Context, httpClient *http.Client, token, owner, repo, username, permission string) (Outcome, error) {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	body, err := json.Marshal(map[string]string{"permission": permission})
	if err != nil {
		return OutcomeFailed, fmt.Errorf("encode body: %w", err)
	}
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/collaborators/%s", owner, repo, username)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return OutcomeFailed, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return OutcomeFailed, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusCreated: // 201
		return OutcomeInvited, nil
	case http.StatusNoContent: // 204
		return OutcomeAdded, nil
	default:
		snippet, readErr := io.ReadAll(io.LimitReader(resp.Body, 512))
		msg := strings.TrimSpace(string(snippet))
		if readErr != nil {
			if msg != "" {
				msg = fmt.Sprintf("%s (body read error: %v)", msg, readErr)
			} else {
				msg = fmt.Sprintf("body read error: %v", readErr)
			}
		}
		return OutcomeFailed, &HTTPError{Owner: owner, Repo: repo, Status: resp.StatusCode, Body: msg}
	}
}

// HTTPError is a collaborator add GitHub answered with a non-success status.
// Error() keeps the log line it always printed (status plus a body snippet);
// Status is what a caller persists, through FailureClass, so the durable
// grant record says why it failed without carrying the body (#2107).
type HTTPError struct {
	Owner, Repo string
	Status      int
	Body        string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("github add collaborator %s/%s: HTTP %d: %s", e.Owner, e.Repo, e.Status, e.Body)
}

// FailureClass maps a failed collaborator add to a short, secret-free class an
// operator can triage from the stored record. "" for a nil error. The classes
// name the likely cause GitHub's status implies for a fine-grained token:
//
//	token_rejected             401: token expired, revoked or malformed
//	token_forbidden            403: token lacks Administration write on the repo
//	repo_not_visible_to_token  404: repo not selected on the token, or renamed
//	request_rejected           422: GitHub refused the request itself
//	github_error               any other status
//	no_response                no HTTP answer at all (network, timeout)
func FailureClass(err error) string {
	if err == nil {
		return ""
	}
	var he *HTTPError
	if !errors.As(err, &he) {
		return "no_response"
	}
	switch he.Status {
	case http.StatusUnauthorized:
		return "token_rejected"
	case http.StatusForbidden:
		return "token_forbidden"
	case http.StatusNotFound:
		return "repo_not_visible_to_token"
	case http.StatusUnprocessableEntity:
		return "request_rejected"
	default:
		return "github_error"
	}
}
