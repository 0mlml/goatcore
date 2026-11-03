package coalition

import (
	"fmt"
	"strings"

	nationpkg "github.com/usareur-af/goatcore/ops/goat-id-console/internal/nation"
)

// derive.go is ADR 1151 migration step 2, and it extends this package rather
// than starting a new one because Resolve already lives here: standing is
// derived in exactly one place and nothing else in the console recomputes it.
//
// Step 2 asks for two things Resolve alone does not give:
//
//  1. RECONCILE a seeded affiliation.alliance_standing against the derived one
//     and warn on conflict. A manifest may still carry a hand-typed standing.
//     ADR 1077 §5 makes the derived value authoritative, so the seeded one is
//     never trusted — but silently discarding it hides that somebody wrote down
//     something that is no longer true, which is worth an operator's attention.
//
//  2. DERIVE `organic`, the "is this ours?" axis that ADR 1151 lifts out of
//     affiliation.kind. There is no organic field and there must not be one:
//     the answer changes with WHO IS OPERATING, not with the org.
//
// Neither is an authorization input. Standing is not in the ACL, not in the
// cert, and not a NetBird group (ADR 1077 §5), and `organic` is a comparison of
// two nation codes computed for display and reconciliation. Nothing here gates
// a read or a write.

// StandingConflict is a seeded alliance_standing that disagrees with the value
// the registry derives. It is a reveal, not an error: the derived value is used
// regardless.
type StandingConflict struct {
	Seeded  string     // what the manifest said
	Derived Resolution // what the registry says, with its Basis
}

// Error renders the conflict as one operator-readable sentence. It carries the
// resolution's own Basis so the reader is not left to work out why the derived
// value came out the way it did.
func (c StandingConflict) Error() string {
	return fmt.Sprintf(
		"affiliation.alliance_standing is seeded %q but derives to %q — %s. The derived value is authoritative (ADR 1077 §5); the seeded one is stale, or was never right.",
		c.Seeded, c.Derived.Standing, c.Derived.Basis)
}

// Reconcile compares a seeded standing against a resolution.
//
// It returns nil when there is nothing to say: no seeded value (the normal,
// healthy state — standing is meant to be derived), or a seeded value that
// agrees. It returns a conflict ONLY when a seeded value is present and
// disagrees. Comparison is case- and space-insensitive; an unrecognized seeded
// string counts as a disagreement rather than being ignored, because a manifest
// carrying `alliance_standing: ally` is also something an operator should see.
func Reconcile(seeded string, derived Resolution) *StandingConflict {
	s := strings.ToLower(strings.TrimSpace(seeded))
	if s == "" || s == strings.ToLower(derived.Standing) {
		return nil
	}
	return &StandingConflict{Seeded: seeded, Derived: derived}
}

// Organic answers "is this org ours?" — ADR 1151's replacement for the
// ours/foreign axis currently smuggled inside affiliation.kind.
//
// RELATIVE TO THE OPERATOR, never to one fixed nation. The same Estonian unit is
// organic on an Estonian-operated goatnet and foreign on a US-operated one, and
// neither goatnet is wrong. That is why this takes the operating sovereign as an
// argument instead of comparing against a constant — the bug ADR 1151 names in
// the old `organic` definition, which baked in one sovereign.
//
// The second return says whether the question could be answered at all, and note
// which way it fails: with no declared operating sovereign, or no nation on the
// affiliation, the answer is (false, false) — "not known to be ours", NEVER a
// default of foreign and never a default of organic. A caller must render
// unknown as unknown; a missing config line must not silently reclassify a
// nation's own units as somebody else's.
//
// Alliance bodies (NATO, EU) are nobody's own sovereign, and fall out as
// not-organic by simple comparison — no special case needed.
func Organic(nation, operatingSovereign string) (organic bool, known bool) {
	// Fold alpha-2 to alpha-3 on BOTH sides, or `US` reads as foreign on a USA
	// network — the defect this comparison existed to answer.
	n := nationpkg.Normalize(nation)
	s := nationpkg.Normalize(operatingSovereign)
	if n == "" || s == "" {
		return false, false
	}
	return n == s, true
}

// DeriveKind computes the deprecated affiliation.kind from the target block, so
// migration step 3's dual-read has one implementation to share rather than a
// copy per surface. It is ADR 1151's table read backwards:
//
//	commercial            -> vendor
//	civil-society         -> civil
//	state + organic       -> organic
//	state + not organic   -> partner
//
// The legacy ADR 1077 sector values collapse first — military, law-enforcement,
// intelligence and civil-government are all `state`; ngo, media and academic are
// all `civil-society` — so a manifest written before the migration derives the
// same kind it already carries. That equivalence is what makes the window safe.
//
// ok is false when the inputs cannot decide: an unknown sector, or a state
// sector whose organic-ness is unknown because no operating sovereign is
// declared. A caller that gets ok=false MUST keep whatever kind the manifest
// carries rather than invent one — during the migration window the stored value
// is still the authority for everything that reads it.
func DeriveKind(sector string, organic, organicKnown bool) (kind string, ok bool) {
	switch strings.ToLower(strings.TrimSpace(sector)) {
	case "commercial":
		return "vendor", true
	case "civil-society", "ngo", "media", "academic":
		return "civil", true
	case "state", "military", "law-enforcement", "intelligence", "civil-government":
		if !organicKnown {
			return "", false
		}
		if organic {
			return "organic", true
		}
		return "partner", true
	default:
		return "", false
	}
}

// GovBranch collapses a legacy ADR 1077 sector value to the ADR 1151
// {sector, gov_branch} pair, for the step-4 backfill and for any surface that
// wants to show the target block for an org that still carries the legacy value.
//
// ok is false for a value that is not a legacy governmental sector — including
// the target values themselves, which need no collapsing.
func GovBranch(legacySector string) (sector, branch string, ok bool) {
	switch strings.ToLower(strings.TrimSpace(legacySector)) {
	case "military":
		return "state", "military", true
	case "law-enforcement":
		return "state", "law-enforcement", true
	case "intelligence":
		return "state", "intelligence", true
	case "civil-government":
		return "state", "civil", true
	default:
		return "", "", false
	}
}

// NormalizeNation exposes the alpha-2 -> alpha-3 fold to callers outside this
// package, so an API echoing a nation back reports the code that was actually
// resolved rather than the one that was typed.
func NormalizeNation(n string) string { return nationpkg.Normalize(n) }

// NationDisplay renders a nation as "United States (USA)" using the framework
// registry's own name for it, falling back to the bare normalised code when the
// registry has no row — an unknown code must still render as itself, never as
// an empty string.
func NationDisplay(r *Registry, code string) string {
	n := nationpkg.Normalize(code)
	if n == "" {
		return ""
	}
	if r != nil {
		for _, fw := range r.Frameworks {
			for _, m := range fw.Members {
				if nationpkg.Normalize(m.Nation) == n && strings.TrimSpace(m.Name) != "" {
					return strings.TrimSpace(m.Name) + " (" + n + ")"
				}
			}
		}
	}
	return n
}
