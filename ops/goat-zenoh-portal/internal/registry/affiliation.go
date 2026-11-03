package registry

// Affiliation spelling guard. The grant affiliation is free text stamped onto
// every machine the custodian enrolls, and fleet views and mesh-group
// derivation key on it — so "1ABCT" and "1abct " are two different units to
// the registry, and a second spelling silently forks the fleet (the same
// failure shape the identity fragility audit fixed for the
// custodian key).
//
// Nothing here validates the unit against a catalog, because the affiliation
// is not yet a reference to anything: it should eventually resolve to an org
// id in the registry the console already carries (ADR 1112 — orgs as
// first-class rows, with aliases and match-or-create at onboarding; ADR 1147
// reserves the noun for exactly that entity). Once it does, the fold below is
// retired and the alias table answers the question properly. Until then the
// guard is the honest subset: the first spelling of a unit is accepted as
// written, and any later value that folds to it but is not spelled identically
// is refused, naming the spelling already on file.
//
// The guard runs at both places the column is written: SetMachineGrant (the
// grant's affiliation) and RecordMachineEnrollment (the slot's, reached by a
// fresh enrollment and by the custody retro-stamp on an existing slot). A
// write that reaches the column any other way is a new door and gets the
// same check.
//
// An operator can repair verified legacy spellings with RepairAffiliations.
// It previews exact records and updates both tables with an atomic audit.
// One gap remains, waiting on that same org id:
//
//   - Distinct units that fold together. The fold drops separators, so a real
//     "1-2 IN" is refused as a variant of an on-file "12 IN". A refusal is
//     loud and writes nothing, but it blocks that unit until the row on file
//     is corrected by hand.

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"github.com/jmoiron/sqlx"
	"golang.org/x/text/unicode/norm"
)

// cleanAffiliation is the form every write site stores: outer whitespace
// dropped and the text in Unicode composed form (NFC). An accented letter can
// arrive as one code point or as a base letter plus a combining mark (a paste
// from a macOS file name carries the latter); they render identically, so
// storing one form keeps them one spelling on file instead of two rows the
// fleet views cannot tell apart.
func cleanAffiliation(s string) string {
	return norm.NFC.String(strings.TrimSpace(s))
}

// foldAffiliation reduces an affiliation to its comparison form: composed
// (NFC), lowercased, with everything that is not a letter or a digit dropped,
// so case, spacing, and separator style ("1st Unit" / "1st-Unit" /
// "1st_Unit" / "1stUnit" / "1st Unit.") all fold together. Used only for
// comparison — the stored spelling stays as the operator wrote it.
//
// Letter is unicode.IsLetter, not [a-z]: an ASCII-only fold would erase every
// diacritic in a name like "Forțele Aeriene Române" and collide unit names
// that share only their unaccented consonants. The NFC step is for rows on
// file, which cleanAffiliation did not see if they predate it: a combining
// mark is not a letter and would otherwise fold away.
func foldAffiliation(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(norm.NFC.String(s)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// affiliationOnFile is one distinct affiliation already in the registry, with
// mine set when it is this custodian's own grant — the only difference is the
// wording of the refusal.
type affiliationOnFile struct {
	Affiliation string `db:"affiliation"`
	Mine        bool   `db:"mine"`
}

// checkAffiliationSpelling refuses an affiliation that folds to the same unit
// as one already on file (in a grant or stamped on an enrolled machine) but is
// not spelled identically. Returns nil for an exact match or a genuinely new
// unit.
//
// Takes a queryer rather than the Registry so the caller can run it inside the
// same transaction as the write: check-then-write with nothing holding between
// them lets two concurrent first grants for "Unit-A" and "unit-a" both pass
// and both land, which is the fork this guard exists to prevent.
//
// Rows are read in byte order so that, when rows written before the guard
// already carry two spellings of one unit, the refusal names the same one
// every time rather than whichever the query plan emits first.
func checkAffiliationSpelling(ctx context.Context, q sqlx.QueryerContext, custodian, affiliation string) error {
	var existing []affiliationOnFile
	err := sqlx.SelectContext(ctx, q, &existing, `
		SELECT affiliation, MAX(mine) AS mine FROM (
		    SELECT affiliation, CASE WHEN custodian = $1 THEN 1 ELSE 0 END AS mine
		      FROM machine_grants WHERE affiliation != ''
		    UNION ALL
		    SELECT affiliation, 0 AS mine
		      FROM slots WHERE affiliation IS NOT NULL AND affiliation != ''
		) AS on_file
		GROUP BY affiliation
		ORDER BY affiliation`, custodian)
	if err != nil {
		return fmt.Errorf("affiliation lookup: %w", err)
	}
	folded := foldAffiliation(affiliation)
	var variants []string
	for _, have := range existing {
		if foldAffiliation(have.Affiliation) == folded {
			variants = append(variants, have.Affiliation)
		}
	}
	if len(variants) > 1 {
		return fmt.Errorf("legacy affiliation spellings %q block enrollment; an operator must preview goat machine affiliation repair after verifying these records represent one organization: %w", variants, ErrAffiliationVariant)
	}
	for _, have := range existing {
		if have.Affiliation == affiliation || foldAffiliation(have.Affiliation) != folded {
			continue
		}
		// Pointing the custodian at `goat machine ls` only helps when the
		// spelling on file is somebody else's; for their own grant it sends
		// them to look up what the message already told them.
		if have.Mine {
			return fmt.Errorf(
				"affiliation %q is a spelling variant of %q, the unit already on your grant — send that spelling exactly: %w",
				affiliation, have.Affiliation, ErrAffiliationVariant)
		}
		return fmt.Errorf(
			"affiliation %q is a spelling variant of existing unit %q — use the existing spelling exactly (copy it from `goat machine ls`): %w",
			affiliation, have.Affiliation, ErrAffiliationVariant)
	}
	return nil
}
