// Package nation folds ISO 3166-1 alpha-2 country codes onto their alpha-3
// forms, so `US` and `USA` name the same country everywhere in this console.
//
// WHY IT IS ITS OWN PACKAGE. The map used to live unexported inside orgstore,
// whose own comment said "if a third consumer appears, that is the moment to
// extract it". One did: the coalition package resolves alliance standing and
// derives ours-vs-foreign from a nation code, and it was comparing raw strings.
// A reviewer who typed `US` on a US-operated network was told the organisation
// was FOREIGN and NOT a NATO member — both false, both from the same missing
// fold. Two copies were a duplication; three would have been a defect generator.
//
// The operator-manifest schema calls alpha-3 "preferred" but its pattern is
// ^[A-Z]{2,8}$, so both spellings validate and both are live in the roster
// today — `usareur-af` carries USA while `demo-org-odt` carries US for the
// same country.
//
// goat-zenoh-portal keeps its own copy in internal/access: it is a separate Go
// module, and a shared dependency for thirty constants is not worth the
// coupling. That copy is the one remaining duplicate, deliberately.
package nation

import "strings"

var alpha2To3 = map[string]string{
	"AL": "ALB", "BE": "BEL", "BG": "BGR", "CA": "CAN", "HR": "HRV", // oss-allow: public ISO or alliance table
	"CZ": "CZE", "DK": "DNK", "EE": "EST", "FI": "FIN", "FR": "FRA", // oss-allow: public ISO or alliance table
	"DE": "DEU", "GR": "GRC", "HU": "HUN", "IS": "ISL", "IT": "ITA", // oss-allow: public ISO or alliance table
	"LV": "LVA", "LT": "LTU", "LU": "LUX", "ME": "MNE", "NL": "NLD", // oss-allow: public ISO or alliance table
	"MK": "MKD", "NO": "NOR", "PL": "POL", "PT": "PRT", "RO": "ROU", // oss-allow: public ISO or alliance table
	"SK": "SVK", "SI": "SVN", "ES": "ESP", "SE": "SWE", "TR": "TUR", // oss-allow: public ISO or alliance table
	"GB": "GBR", "UK": "GBR", "US": "USA", "UA": "UKR", // oss-allow: public ISO or alliance table
}

// Normalize upper-cases, trims, and folds a known alpha-2 code to alpha-3.
//
// Anything that is not a known alpha-2 is returned unchanged, which is what
// keeps multinational markers working: NATO and EU are not countries and must
// pass through untouched so the framework's member_markers can still match them.
func Normalize(n string) string {
	n = strings.ToUpper(strings.TrimSpace(n))
	if a3, ok := alpha2To3[n]; ok {
		return a3
	}
	return n
}
