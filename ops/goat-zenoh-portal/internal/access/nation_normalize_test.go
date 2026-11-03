package access

import "testing"

// Either national abbreviation must resolve to the same nation. Before this,
// `nation: US` matched no member row in coalition-frameworks.yaml (which lists
// USA) and silently resolved to non-member standing on a NATO goatnet -- live
// in the roster for demo-org-odt (9 members), demoorg and do-odt.
// Unknown codes and multinational markers must pass through untouched.
func TestNationAbbrevEitherResolves(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"US", "USA"}, {"USA", "USA"}, {"us", "USA"}, {" US ", "USA"},
		{"GB", "GBR"}, {"UK", "GBR"}, {"LT", "LTU"}, {"LTU", "LTU"}, // oss-allow: public ISO table
		{"NATO", "NATO"}, {"ZZZ", "ZZZ"},
	} {
		if got := normalizeNation(c.in); got != c.want {
			t.Errorf("normalizeNation(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
