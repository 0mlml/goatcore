// Package coalition resolves an affiliation's coalition standing (member vs
// partner) against the goatnet's active coalition framework, for DISPLAY on the
// console's identity-detail view.
//
// WHY THIS EXISTS. Standing is the eligibility predicate for a framework's
// member release-lane (ADR 1077 / ADR 1027), and it is nation-derived — it is
// deliberately NOT stamped into the cert and NOT carried in the Ory `aff` claim
// (identity-grammar.md: "rides the registry (currently-is), never the frozen
// cert"). The consequence for an operator reading a person's page: nothing on
// the identity itself says "partner", so the console has to resolve it or the
// operator cannot see it at all. That is what this package is for — a read
// surface, not a gate. Nothing here authorizes anything.
//
// ONE HOME FOR THE FACT. The membership facts live in exactly one place,
// ops/identity/coalition-frameworks.yaml, bind-mounted read-only into the
// container the same way group-permissions.yml already is. This file carries
// only the resolution RULE, and it is a deliberate re-expression of the
// canonical implementation in ops/identity/generate.py:resolve_coalition —
// member_markers, the effective-dated [member_since, member_until) range, and
// the fail-to-partner defaults, all of it. TestRuleParityWithCanonicalRegistry
// fails closed if this file and the registry drift apart.
package coalition

import (
	"fmt"
	"os"
	"strings"
	"time"

	nationpkg "github.com/usareur-af/goatcore/ops/goat-id-console/internal/nation"

	"gopkg.in/yaml.v3"
)

// DateFormat is the ISO date shape every effective date in the registry uses.
const DateFormat = "2006-01-02"

// Registry is the parsed coalition-frameworks.yaml — only the fields the
// standing resolution consumes.
type Registry struct {
	DefaultFramework string               `yaml:"default_framework"`
	Frameworks       map[string]Framework `yaml:"frameworks"`
}

// Framework is one alliance / coalition / interagency construct.
type Framework struct {
	DisplayName   string   `yaml:"display_name"`
	Kind          string   `yaml:"kind"`
	AOR           string   `yaml:"aor"`
	MemberMarkers []string `yaml:"member_markers"`
	Members       []Member `yaml:"members"`
}

// Member is one nation's effective-dated membership. Membership is a RANGE:
// MemberUntil empty = still a member; set = the date the nation left, from
// which point it reads back as a partner.
type Member struct {
	Nation      string `yaml:"nation"`
	Name        string `yaml:"name"`
	MemberSince string `yaml:"member_since"`
	MemberUntil string `yaml:"member_until"`
}

// Standing values — the closed grammar (grammar.yaml affiliation.alliance_standing).
const (
	StandingMember  = "member"
	StandingPartner = "partner"
)

// Load reads the framework registry. A missing or malformed file is an error
// rather than an empty registry: "nobody is a member" must be a declared state,
// never a silent parse failure. Callers that only display standing should keep
// the error and say so on the page instead of rendering a standing they cannot
// justify.
func Load(path string) (*Registry, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read coalition-frameworks %s: %w", path, err)
	}
	var reg Registry
	if err := yaml.Unmarshal(raw, &reg); err != nil {
		return nil, fmt.Errorf("parse coalition-frameworks %s: %w", path, err)
	}
	if len(reg.Frameworks) == 0 {
		return nil, fmt.Errorf("coalition-frameworks %s declares no frameworks", path)
	}
	return &reg, nil
}

// Resolution is a standing plus the reasoning behind it, because an operator
// reading "partner" needs to know whether that is a fact about the nation or a
// fallback from missing data — the two call for completely different actions
// (none vs. record the nation).
type Resolution struct {
	Standing      string    // member | partner (the closed grammar; never a third value)
	Framework     string    // framework id the standing is relative to
	FrameworkName string    // its display name, for the page
	Nation        string    // the nation the resolution keyed on ("" = none recorded)
	NationName    string    // the registry's name for that nation, when it is a listed member
	MemberSince   string    // when membership began, for a member
	MemberUntil   string    // when it ended, for a nation that left
	AsOf          time.Time // the date the standing was resolved for
	// Basis is one operator-readable sentence naming WHY this standing came
	// out. It exists so the page never shows a bare "partner" that the reader
	// has to guess at.
	Basis string
}

// Resolve derives standing for a nation as-of a date, relative to framework
// `framework` (empty = the registry's default_framework). It mirrors
// generate.py:resolve_coalition exactly, including its fail-to-partner
// defaults: an unknown nation, an unknown framework, or a date before
// membership began all resolve to partner.
func (r *Registry) Resolve(nation, framework string, asOf time.Time) Resolution {
	// Fold alpha-2 onto alpha-3 first. Comparing raw strings told a reviewer who
	// typed `US` that the organisation was not a USA match and not a NATO member.
	n := nationpkg.Normalize(nation)
	fwID := strings.TrimSpace(framework)
	if fwID == "" {
		fwID = strings.TrimSpace(r.DefaultFramework)
	}
	res := Resolution{Standing: StandingPartner, Framework: fwID, Nation: n, AsOf: asOf}

	fw, ok := r.Frameworks[fwID]
	if !ok {
		res.Basis = fmt.Sprintf("framework %q is not in the registry", fwID)
		return res
	}
	res.FrameworkName = fw.DisplayName

	if n == "" {
		res.Basis = "no nation recorded"
		return res
	}

	for _, marker := range fw.MemberMarkers {
		if nationpkg.Normalize(marker) == n {
			res.Standing = StandingMember
			// A framework body is not a country in the framework; it IS the
			// framework, or an organ of it. "NATO is part of NATO" is what the
			// generic sentence produced.
			label := frameworkLabel(fwID, fw.DisplayName)
			if n == label {
				res.Basis = fmt.Sprintf("%s is the alliance itself, not a nation in it", n)
			} else {
				res.Basis = fmt.Sprintf("%s is a %s body", n, label)
			}
			return res
		}
	}

	for _, m := range fw.Members {
		if nationpkg.Normalize(m.Nation) != n {
			continue
		}
		res.NationName = m.Name
		res.MemberSince = strings.TrimSpace(m.MemberSince)
		res.MemberUntil = strings.TrimSpace(m.MemberUntil)

		since, err := time.Parse(DateFormat, res.MemberSince)
		if err != nil {
			res.Basis = fmt.Sprintf("%s carries an unreadable member_since (%q) — the rule defaults to partner",
				n, res.MemberSince)
			return res
		}
		if asOf.Before(since) {
			res.Basis = fmt.Sprintf("%s did not join %s until %s",
				nationLabel(n, res.NationName), frameworkLabel(fwID, fw.DisplayName), res.MemberSince)
			return res
		}
		if res.MemberUntil != "" {
			until, uerr := time.Parse(DateFormat, res.MemberUntil)
			if uerr != nil {
				res.Basis = fmt.Sprintf("%s carries an unreadable member_until (%q) — the rule defaults to partner",
					n, res.MemberUntil)
				return res
			}
			if !asOf.Before(until) {
				res.Basis = fmt.Sprintf("%s left %s in %s",
					nationLabel(n, res.NationName), frameworkLabel(fwID, fw.DisplayName), yearOf(res.MemberUntil))
				return res
			}
		}
		res.Standing = StandingMember
		res.Basis = fmt.Sprintf("%s joined %s in %s",
			nationLabel(n, res.NationName), frameworkLabel(fwID, fw.DisplayName), yearOf(res.MemberSince))
		return res
	}

	res.Basis = fmt.Sprintf("%s is not a member of %s", n, frameworkLabel(fwID, fw.DisplayName))
	return res
}

// frameworkLabel renders a framework id as its acronym, upper-cased. The
// registry stores ids lower-case (`nato`), and echoing that raw put "nato" on
// an operator's screen. No expansion: NATO needs no gloss, and the id IS what
// the goatnet configured, so there is nothing to prove about the match — unlike
// a nation, where the operator may have typed a different spelling.
func frameworkLabel(id, _ string) string {
	return strings.ToUpper(strings.TrimSpace(id))
}

// nationLabel renders a nation as "United States (USA)" — the full name plus the
// normalised code, so an operator who typed `US` can see WHICH country was
// matched rather than trusting a three-letter echo of their own input. Falls
// back to the bare code when the registry carries no name for it.
func nationLabel(code, name string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	name = strings.TrimSpace(name)
	if name == "" {
		return code
	}
	return name + " (" + code + ")"
}

// yearOf trims an ISO date to its year for display. The exact day a nation
// acceded is precision nobody reading a registration form needs; the full date
// stays in MemberSince for anything that does.
func yearOf(isoDate string) string {
	d := strings.TrimSpace(isoDate)
	if len(d) >= 4 {
		return d[:4]
	}
	return d
}
