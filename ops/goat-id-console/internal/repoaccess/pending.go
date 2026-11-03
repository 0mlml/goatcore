package repoaccess

import "strings"

// pendingLogin stands in for a GitHub username when asking what a person
// WOULD be granted once they supply one. It is never sent to GitHub: the
// policy only tests whether a login is present, not what it is.
const pendingLogin = "pending-github-login"

// WouldGrant is Effective as if the subject had a GitHub username: the repos a
// person without one is missing. A subject who already has a username gets
// their actual effective set.
func (c *Config) WouldGrant(s Subject, ov Overrides) []EffectiveGrant {
	if strings.TrimSpace(s.GitHubUsername) == "" {
		s.GitHubUsername = pendingLogin
	}
	return c.Effective(s, ov)
}

// TiersWithGrants returns the subset of tiers some rule can grant a repo to:
// a rule with at least one grant whose tier list names the tier, or is empty
// (which matches every tier). The other match keys are ignored on purpose:
// this answers "could this tier receive repos", which is what decides whether
// to warn a candidate who leaves the username blank, before their affiliation
// is settled.
func (c *Config) TiersWithGrants(tiers []string) []string {
	if c == nil {
		return nil
	}
	var out []string
	for _, t := range tiers {
		for _, rule := range c.Rules {
			if len(rule.Grant) == 0 {
				continue
			}
			if len(rule.Match.Tier) == 0 || containsFold(rule.Match.Tier, t) {
				out = append(out, t)
				break
			}
		}
	}
	return out
}

func containsFold(list []string, v string) bool {
	v = strings.TrimSpace(v)
	for _, x := range list {
		if strings.EqualFold(strings.TrimSpace(x), v) {
			return true
		}
	}
	return false
}
