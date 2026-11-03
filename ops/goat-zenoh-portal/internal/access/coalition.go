package access

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// coalition.go is the B2.2 projection (data-access program): derive release-set
// membership from captured coalition standing, so a coalition MEMBER org
// automatically lands in the framework's member release lane (REL-COALITION)
// without a hand edit of release-sets.yaml. Standing is nation-derived against
// the active coalition framework (ADR 1077) — the same rule as
// ops/identity/generate.py:resolve_coalition, reimplemented read-only here so
// the live portal derives it from the roster it already loads. Partners
// (non-member nations) are NOT added to the member lane by construction; their
// bilateral lanes stay a declared registry edit.

// dateFormat is the ISO date shape every effective date in the registry uses.
const dateFormat = "2006-01-02"

// CoalitionFrameworks is the parsed ops/identity/coalition-frameworks.yaml
// (only the fields the projection consumes).
type CoalitionFrameworks struct {
	DefaultFramework string                        `yaml:"default_framework"`
	Frameworks       map[string]coalitionFramework `yaml:"frameworks"`
}

type coalitionFramework struct {
	// MemberMarkers are affiliation.nation values that are members BY DEFINITION
	// — alliance bodies such as NATO or EU, which carry no nation row of their own.
	MemberMarkers []string          `yaml:"member_markers"`
	Members       []coalitionMember `yaml:"members"`
}

type coalitionMember struct {
	Nation      string `yaml:"nation"`
	MemberSince string `yaml:"member_since"`
	// MemberUntil closes the membership range: empty = still a member; set = the
	// date the nation left, from which point it resolves back to partner.
	MemberUntil string `yaml:"member_until"`
}

// LoadCoalitionFrameworks reads the framework registry. Fail-closed: a
// missing/malformed file is an error (an empty framework set must be a declared
// state, never a silent "nobody is a member").
func LoadCoalitionFrameworks(path string) (*CoalitionFrameworks, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read coalition-frameworks %s: %w", path, err)
	}
	var cf CoalitionFrameworks
	if err := yaml.Unmarshal(raw, &cf); err != nil {
		return nil, fmt.Errorf("parse coalition-frameworks %s: %w", path, err)
	}
	return &cf, nil
}

// isMember reports whether nation is a member of framework `fwName` as of
// `asOf`. Unknown framework or nation ⇒ false (partner) — parity with
// resolve_coalition's fail-to-partner default.
//
// Membership is a RANGE, [member_since, member_until), and a framework can also
// name markers that are members by definition. Both were missing from the first
// version of this function, which checked member_since alone: a `nation: NATO`
// org (an alliance body — the registry's member_markers) resolved partner and
// was excluded from the very lane it defines, and a nation that WITHDREW would
// have kept reading as a member forever. Neither had bitten yet (no such org is
// onboarded; no nation has left), but this function decides who lands in the
// derived member release lane, so the failure would have been a silent
// over-share or under-share rather than an error.

// alpha2To3 maps the ISO 3166-1 alpha-2 codes of the framework nations we carry
// onto their alpha-3 forms. The manifest schema documents alpha-3 as "preferred"
// but its pattern is ^[A-Z]{2,8}$, so BOTH spellings validate and both can appear
// in a roster for the same country. Compared raw, those are different nations:
// `US` matches no member row and silently resolves to non-member standing.
//
// Scope: this derivation feeds the Data-Access COP reveal
// (internal/api/access.go) and NEVER the ACL, so a wrong spelling affects
// DISPLAYED standing, not access. ADR 1077 section 5 -- standing bounds
// eligibility and never issues a grant.
var alpha2To3 = map[string]string{
	"AL": "ALB", "BE": "BEL", "BG": "BGR", "CA": "CAN", "HR": "HRV", // oss-allow: public ISO or alliance table
	"CZ": "CZE", "DK": "DNK", "EE": "EST", "FI": "FIN", "FR": "FRA", // oss-allow: public ISO or alliance table
	"DE": "DEU", "GR": "GRC", "HU": "HUN", "IS": "ISL", "IT": "ITA", // oss-allow: public ISO or alliance table
	"LV": "LVA", "LT": "LTU", "LU": "LUX", "ME": "MNE", "NL": "NLD", // oss-allow: public ISO or alliance table
	"MK": "MKD", "NO": "NOR", "PL": "POL", "PT": "PRT", "RO": "ROU", // oss-allow: public ISO or alliance table
	"SK": "SVK", "SI": "SVN", "ES": "ESP", "SE": "SWE", "TR": "TUR", // oss-allow: public ISO or alliance table
	"GB": "GBR", "UK": "GBR", "US": "USA", "UA": "UKR", // oss-allow: public ISO or alliance table
}

// normalizeNation upper-cases, trims, and folds a two-letter code to alpha-3 so
// either abbreviation resolves to the same nation. Anything that is not a known
// alpha-2 is returned as-is: unknown codes and multinational markers (NATO, EU)
// must pass through untouched.
func normalizeNation(n string) string {
	n = strings.ToUpper(strings.TrimSpace(n))
	if a3, ok := alpha2To3[n]; ok {
		return a3
	}
	return n
}

func (cf *CoalitionFrameworks) isMember(fwName, nation string, asOf time.Time) bool {
	fw, ok := cf.Frameworks[fwName]
	if !ok {
		return false
	}
	nation = normalizeNation(nation)
	if nation == "" {
		return false
	}
	// Framework bodies (NATO, EU) are members by definition, not by nation row.
	for _, marker := range fw.MemberMarkers {
		if normalizeNation(marker) == nation {
			return true
		}
	}
	for _, m := range fw.Members {
		if normalizeNation(m.Nation) != nation {
			continue
		}
		since, err := time.Parse(dateFormat, strings.TrimSpace(m.MemberSince))
		if err == nil && since.After(asOf) {
			return false // not yet a member as-of this date
		}
		// An unparseable member_since is treated as undated rather than as a
		// blocker, matching the prior behaviour; the registry's own validator
		// (generate.py check_coalition_frameworks) is what rejects bad dates.
		if until := strings.TrimSpace(m.MemberUntil); until != "" {
			end, uerr := time.Parse(dateFormat, until)
			if uerr == nil && !asOf.Before(end) {
				return false // left the framework on/before this date
			}
		}
		return true
	}
	return false
}

// rosterOrgNations parses the orgs block of roster.json for {id, nation}. Its
// own minimal read (the fuller roster load is for reconciliation); an org with
// no nation is skipped — it cannot resolve a standing.
func rosterOrgNations(rosterPath string) (map[string]string, error) {
	raw, err := os.ReadFile(rosterPath)
	if err != nil {
		return nil, fmt.Errorf("read roster %s: %w", rosterPath, err)
	}
	var rf struct {
		Orgs []struct {
			ID     string `json:"id"`
			Nation string `json:"nation"`
		} `json:"orgs"`
	}
	if err := json.Unmarshal(raw, &rf); err != nil {
		return nil, fmt.Errorf("parse roster %s: %w", rosterPath, err)
	}
	out := map[string]string{}
	for _, o := range rf.Orgs {
		if o.ID != "" && strings.TrimSpace(o.Nation) != "" {
			out[o.ID] = o.Nation
		}
	}
	return out, nil
}

// The member release-lane derivation lived here until ADR 1148 §3 removed it.
// An alliance-member-only lane grants nothing: members already hold read-all,
// so a lane whose audience is exactly the members hands them access they have.
// The construct existed only in this code -- the circulated model never asked
// for it -- and it fed the Data-Access COP reveal, never the ACL, so removing it
// changes what an operator SEES and nothing about who reads what.
