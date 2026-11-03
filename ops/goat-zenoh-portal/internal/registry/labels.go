package registry

// labels.go implements the producer data-label lanes (ADR 1075): three closed
// enums stamped on a producer namespace at registration, plus the
// justification-on-restriction gate. The lane values are the SoT law
// (ops/permissions/data-access-roles.yaml `data_labels.lanes`) — this file is
// the code that speaks them; ops/ci/check-data-access-roles.sh guards that the
// SoT enums stay these exact closed sets, and the closedEnum maps below must
// track them (a divergence is a CI-visible policy change, by ADR only — G1).

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// The three closed label lanes (ADR 1075). fidelity/context are advisory +
// display; access is the authz-relevant lane. "" (unlabeled) is a valid state
// for every lane, rendered honestly and never fabricated; an unlabeled access
// lane resolves to the per-goatnet default at read time, not here.
var (
	labelFidelityValues = map[string]bool{"notional": true, "simulated": true, "real": true}
	labelContextValues  = map[string]bool{"experiment": true, "exercise": true, "operational": true}
	labelAccessValues   = map[string]bool{"community": true, "org": true, "controlled": true}
)

// AccessCommunity, AccessOrg, AccessControlled name the access-lane values so
// callers (the resolver, the drift counter) never spell the strings inline.
const (
	AccessCommunity  = "community"
	AccessOrg        = "org"
	AccessControlled = "controlled"
)

// Labels is a producer's data-label set (ADR 1075). Empty strings are the
// unlabeled state per lane. Justification is REQUIRED when Access is org or
// controlled and MUST be empty-or-ignored otherwise (restriction is the
// deliberate recorded act; openness needs no ceremony).
type Labels struct {
	Fidelity      string `json:"fidelity,omitempty"`
	Context       string `json:"context,omitempty"`
	Access        string `json:"access,omitempty"`
	Justification string `json:"justification,omitempty"`
}

// sortedEnum returns a lane's legal values for an error message, deterministically.
func sortedEnum(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Validate checks the label set against the closed enums and the
// justification-on-restriction gate. Empty lanes are legal (unlabeled). A value
// outside a lane's enum, or a restrictive access lane (org|controlled) with no
// justification, is refused — the friction is on restriction, never on openness.
func (l Labels) Validate() error {
	l = l.normalized()
	if l.Fidelity != "" && !labelFidelityValues[l.Fidelity] {
		return fmt.Errorf("fidelity %q: must be one of %v or unset", l.Fidelity, sortedEnum(labelFidelityValues))
	}
	if l.Context != "" && !labelContextValues[l.Context] {
		return fmt.Errorf("context %q: must be one of %v or unset", l.Context, sortedEnum(labelContextValues))
	}
	if l.Access != "" && !labelAccessValues[l.Access] {
		return fmt.Errorf("access %q: must be one of %v or unset", l.Access, sortedEnum(labelAccessValues))
	}
	if (l.Access == AccessOrg || l.Access == AccessControlled) && l.Justification == "" {
		return fmt.Errorf("access %q requires a justification — restriction is a deliberate recorded act (ADR 1075)", l.Access)
	}
	return nil
}

// normalized lowercases/trims the lane values and drops a justification that
// rides a non-restrictive access lane (justification is only meaningful on
// restriction; keeping it on a community lane would be misleading state).
func (l Labels) normalized() Labels {
	out := Labels{
		Fidelity:      strings.ToLower(strings.TrimSpace(l.Fidelity)),
		Context:       strings.ToLower(strings.TrimSpace(l.Context)),
		Access:        strings.ToLower(strings.TrimSpace(l.Access)),
		Justification: strings.TrimSpace(l.Justification),
	}
	if out.Access != AccessOrg && out.Access != AccessControlled {
		out.Justification = ""
	}
	return out
}

// EffectiveFidelity resolves the fidelity a producer displays: its explicit
// lane, else "notional" when the legacy synthetic flag is set (the additive
// migration bridge — a producer that never set the lane but self-declared
// synthetic still reads as notional), else "" (honestly unlabeled).
func EffectiveFidelity(labelFidelity string, synthetic bool) string {
	if labelFidelity != "" {
		return labelFidelity
	}
	if synthetic {
		return "notional"
	}
	return ""
}

// EffectiveAccess resolves the access lane an unlabeled producer inherits: its
// explicit lane, else the per-goatnet default (goatnetDefault; ZAGROS/sandbox =
// community). A caller passing an empty default gets community — the
// fail-safe-open sandbox posture, never a silent restriction.
func EffectiveAccess(labelAccess, goatnetDefault string) string {
	if labelAccess != "" {
		return labelAccess
	}
	if goatnetDefault != "" {
		return goatnetDefault
	}
	return AccessCommunity
}

// SetVendorLabels validates and stores a producer's label set (ADR 1075).
// Idempotent; ErrNotFound on an unknown prefix. Validation (closed enums +
// justification gate) runs before the write, so an invalid label set never
// lands. The justification is cleared when access is not restrictive (see
// normalized) so the stored row can't carry a misleading why.
func (r *Registry) SetVendorLabels(ctx context.Context, prefix string, l Labels) error {
	l = l.normalized()
	if err := l.Validate(); err != nil {
		return err
	}
	res, err := r.db.ExecContext(ctx,
		`UPDATE vendors SET label_fidelity = $1, label_context = $2, label_access = $3, label_justification = $4 WHERE prefix = $5`,
		l.Fidelity, l.Context, l.Access, l.Justification, prefix)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// LabelDrift is the ADR 1075 restrictive-share telemetry: the non-community
// share of producers per goatnet — the ratchet-up-bias counter. Producers is
// the total; Restricted is those whose EFFECTIVE access lane is org|controlled;
// Community is the rest (explicit community + unlabeled-resolving-to-default).
// The share is intentionally a plain honest ratio, not a fabricated grade.
type LabelDrift struct {
	Producers       int            `json:"producers"`
	Community       int            `json:"community"`
	Restricted      int            `json:"restricted"`
	RestrictedShare float64        `json:"restricted_share"`
	AccessDefault   string         `json:"access_default"`
	ByAccess        map[string]int `json:"by_access"`
}

// ComputeLabelDrift rolls up the restrictive-share counter over every producer,
// resolving each producer's effective access lane against the goatnet default.
// Pure over the vendor list so it is trivially testable; the handler supplies
// the live list + the configured default.
func ComputeLabelDrift(vendors []Vendor, goatnetDefault string) LabelDrift {
	d := LabelDrift{AccessDefault: EffectiveAccess("", goatnetDefault), ByAccess: map[string]int{}}
	for _, v := range vendors {
		eff := EffectiveAccess(v.LabelAccess, goatnetDefault)
		d.Producers++
		d.ByAccess[eff]++
		if eff == AccessOrg || eff == AccessControlled {
			d.Restricted++
		} else {
			d.Community++
		}
	}
	if d.Producers > 0 {
		d.RestrictedShare = float64(d.Restricted) / float64(d.Producers)
	}
	return d
}

// nowUnix is a tiny seam so grant/label time logic is deterministic in tests.
func nowUnix() int64 { return time.Now().Unix() }
