package handlers

import "testing"

// affiliation.sector gate (ADR 1077): the governmental cluster is always
// stamped; the non-governmental cluster is stamped only when the goatnet's
// NongovSectors gate is on, and a non-gov value submitted with the gate off is
// dropped (not stamped), never an error.
func TestAssembleManifestSectorGate(t *testing.T) {
	hints, err := loadIntakeHints()
	if err != nil {
		t.Fatalf("loadIntakeHints: %v", err)
	}

	base := func(sector string, nongov bool) effectiveInputs {
		return effectiveInputs{
			Tier:          "browse",
			Affiliation:   "partner",
			AffiliationID: "sector-org",
			Handle:        "test-handle",
			Email:         "test@example.test",
			Sector:        sector,
			NongovSectors: nongov,
		}
	}

	sectorOf := func(t *testing.T, in effectiveInputs) (string, bool) {
		t.Helper()
		m, err := assembleManifest(hints, in)
		if err != nil {
			t.Fatalf("assembleManifest: %v", err)
		}
		v := getPath(m, "affiliation.sector")
		if v == nil {
			return "", false
		}
		s, _ := v.(string)
		return s, true
	}

	cases := []struct {
		name    string
		sector  string
		nongov  bool
		wantVal string
		wantSet bool
	}{
		{"gov military, gate off", "military", false, "military", true},
		{"gov intelligence, gate off", "intelligence", false, "intelligence", true},
		{"gov intelligence, gate on", "intelligence", true, "intelligence", true},
		{"nongov ngo, gate off -> dropped", "ngo", false, "", false},
		{"nongov ngo, gate on -> stamped", "ngo", true, "ngo", true},
		{"nongov media, gate on -> stamped", "media", true, "media", true},
		{"unknown value, gate on -> dropped", "bogus", true, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, set := sectorOf(t, base(c.sector, c.nongov))
			if set != c.wantSet || got != c.wantVal {
				t.Fatalf("sector=%q nongov=%v: got (%q,%v), want (%q,%v)",
					c.sector, c.nongov, got, set, c.wantVal, c.wantSet)
			}
		})
	}
}
