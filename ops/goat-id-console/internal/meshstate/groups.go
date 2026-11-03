// Group rules shared with the inactivity sweep. The console and
// ops/scripts/lifecycle-sweep.py must grade one live peer the same
// way, or the console offers a release the engine then refuses, so
// each rule here names the sweep function it mirrors.

package meshstate

import (
	"sort"
	"strings"
)

// sameSite reports whether recordSite and site refer to the same
// site id (lowercase on the record).
func sameSite(site, recordSite string) bool {
	site = strings.TrimSpace(strings.ToLower(site))
	recordSite = strings.TrimSpace(strings.ToLower(recordSite))
	return site != "" && site == recordSite
}

// peerGroupNames returns the peer's group names, excluding the
// implicit All group everywhere.
func peerGroupNames(groups []Group) []string {
	out := make([]string, 0, len(groups))
	for _, g := range groups {
		name := strings.TrimSpace(g.Name)
		if name == "" || strings.EqualFold(name, "All") {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func containsGroup(groups []string, want string) bool {
	want = strings.TrimSpace(want)
	for _, g := range groups {
		if strings.EqualFold(g, want) {
			return true
		}
	}
	return false
}

// otherSiteGroups returns the peer's live groups that are not hold
// groups (IsHoldGroup) — the groups a held peer must NOT still hold.
// The rule is the sweep's (choose_hold_group in lifecycle-sweep.py): a
// hold removes EVERY prior group, so ANY other group beside the hold
// group — a role, SENSOR, INFRA, PARTNER, a hand-made group, another
// site's group on a shared management host — is a contradiction. Only
// the implicit All (already dropped by peerGroupNames) is exempt. The
// two readers must grade one live peer the same way, or the console
// offers a release the engine then refuses.
func otherSiteGroups(siteUpper string, groups []string) []string {
	siteUpper = strings.TrimSpace(strings.ToUpper(siteUpper))
	if siteUpper == "" {
		return nil
	}
	var out []string
	for _, g := range groups {
		if IsHoldGroup(g) {
			continue
		}
		out = append(out, g)
	}
	return out
}

// IsHoldGroup reports whether g is a hold group under the sweep's rule
// (hold_groups_of in lifecycle-sweep.py): any group with a -PROBATION-
// segment and any group ending in -QUARANTINED, compared
// case-insensitively. The site prefix is deliberately not required: a
// hold group under an earlier site id (a peer held before a site rename
// keeps the old prefix) is still a hold to the sweep, and the page must
// not call that peer "not on hold".
func IsHoldGroup(g string) bool {
	u := strings.ToUpper(strings.TrimSpace(g))
	return strings.Contains(u, "-PROBATION-") || strings.HasSuffix(u, "-QUARANTINED")
}

// holdGroupsOf returns EVERY hold group the peer is in, in the
// caller's (sorted) order. More than one is a contradiction the caller
// must name; it is never collapsed to the first match. No site id ⇒
// nothing is graded.
func holdGroupsOf(siteUpper string, groups []string) []string {
	siteUpper = strings.TrimSpace(strings.ToUpper(siteUpper))
	if siteUpper == "" {
		return nil
	}
	var out []string
	for _, g := range groups {
		if IsHoldGroup(g) {
			out = append(out, g)
		}
	}
	return out
}

// unlistedGroups returns the peer's live groups that are neither the
// record's hold group nor one of its prior groups. For a release in
// progress these are the groups the sweep refuses to resume past.
func unlistedGroups(groups []string, r HoldRecord) []string {
	var out []string
	for _, g := range groups {
		if strings.EqualFold(g, strings.TrimSpace(r.HoldGroup)) || containsGroup(r.PriorGroups, g) {
			continue
		}
		out = append(out, g)
	}
	return out
}

func hasRecordForPeer(records []HoldRecord, peerID string) bool {
	for _, r := range records {
		if r.PeerID == peerID {
			return true
		}
	}
	return false
}
