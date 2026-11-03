package api

// intake_hints.go — the tier-ordering helpers RETAINED after the onboarding
// engine was stripped from the portal (docs/design/onboarding-extraction-plan.md
// slice 5). The onboarding workflow now lives in goat-id-console; the portal
// keeps only the Zenoh cert/slot mint (`POST /api/orgs`) + the ADR 1071
// sponsorship annotation (`setVendorSponsorship`) + whoami. That surviving
// sponsorship handler still needs the tier privilege-ordering (to validate a
// sponsor's `max_tier` cap), so these helpers — previously in the deleted
// onboard_assemble.go / onboard_submit.go — move here. `slug` moves too: it is
// used by trashgoat.go, not onboarding.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"strings"
)

// intakeHints is the parsed subset of onboard-intake-form.json the portal still
// needs: the tier list with its privilege ranks (ADR 1071 sponsorship cap).
type intakeHints struct {
	Defaults map[string]any `json:"defaults"`
	Tiers    []intakeTier   `json:"tiers"`
}

type intakeTier struct {
	ID    string         `json:"id"`
	Label string         `json:"label"`
	Rank  int            `json:"rank"` // privilege order for the sponsorship cap (ADR 1071); higher = more capable
	Set   map[string]any `json:"set"`
}

// intakeHints reads + parses the shared onboard-intake-form.json (the single
// source for the tier ranks) from the embedded static FS. Retained for the
// sponsorship-cap validation in setVendorSponsorship (ADR 1071).
func (s *Server) intakeHints() (*intakeHints, error) {
	if s.staticFS == nil {
		return nil, errors.New("no static FS")
	}
	raw, err := fs.ReadFile(s.staticFS, "admin/onboard-intake-form.json")
	if err != nil {
		return nil, err
	}
	return parseIntakeHints(raw)
}

func parseIntakeHints(raw []byte) (*intakeHints, error) {
	var h intakeHints
	if err := json.Unmarshal(raw, &h); err != nil {
		return nil, fmt.Errorf("parse intake hints: %w", err)
	}
	return &h, nil
}

// tierRank returns the privilege rank of a tier id (higher = more capable),
// or ok=false for an unknown tier.
func (h *intakeHints) tierRank(tier string) (int, bool) {
	t := strings.ToLower(strings.TrimSpace(tier))
	for _, e := range h.Tiers {
		if strings.ToLower(e.ID) == t {
			return e.Rank, true
		}
	}
	return 0, false
}

var handleSlugRe = regexp.MustCompile(`[^a-z0-9-]+`)

// slug lowercases + collapses to a handle-safe slug. Used by trashgoat.go.
func slug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = handleSlugRe.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}
