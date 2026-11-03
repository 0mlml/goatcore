package handlers

import (
	"errors"
	"strings"
	"testing"
)

// affiliation.kind resolution in the assembler (ADR 1151 step 3): an explicit
// kind wins; otherwise it derives from the stamped sector plus the organic
// test; when neither decides, the key is omitted and the manifest is still
// complete. A blank kind is no longer an incomplete draft.
func TestAssembleManifest_KindResolution(t *testing.T) {
	hints, err := loadIntakeHints()
	if err != nil {
		t.Fatalf("loadIntakeHints: %v", err)
	}
	base := effectiveInputs{Tier: "browse", AffiliationID: "kind-org", Handle: "kind-test", Email: "kind@example.test"}

	cases := []struct {
		name       string
		mutate     func(in *effectiveInputs)
		wantKind   string
		wantSet    bool
		wantScheme string // unit_code.scheme when a unit code is given
	}{
		{"explicit kind wins over sector", func(in *effectiveInputs) { in.Affiliation = "Partner"; in.Sector = "commercial" }, "partner", true, "nato-logfas"},
		{"commercial derives vendor", func(in *effectiveInputs) { in.Sector = "commercial" }, "vendor", true, "sovereign"},
		{"civil-society derives civil", func(in *effectiveInputs) { in.Sector = "civil-society" }, "civil", true, "sovereign"},
		{"legacy ngo derives civil", func(in *effectiveInputs) { in.Sector = "ngo"; in.NongovSectors = true }, "civil", true, "sovereign"},
		{"state + own nation derives organic", func(in *effectiveInputs) {
			in.Sector = "state"
			in.Nation = "USA"
			in.OperatingSovereign = "USA"
		}, "organic", true, "sovereign"},
		{"legacy military + allied nation derives partner", func(in *effectiveInputs) {
			in.Sector = "military"
			in.Nation = "LVA"
			in.OperatingSovereign = "USA"
		}, "partner", true, "nato-logfas"},
		{"state with unknown nation stays blank", func(in *effectiveInputs) { in.Sector = "state"; in.OperatingSovereign = "USA" }, "", false, "sovereign"},
		{"state with no operating sovereign stays blank", func(in *effectiveInputs) { in.Sector = "state"; in.Nation = "USA" }, "", false, "sovereign"},
		{"no sector stays blank", func(in *effectiveInputs) {}, "", false, "sovereign"},
		{"dropped non-gov sector cannot derive", func(in *effectiveInputs) { in.Sector = "ngo"; in.NongovSectors = false }, "", false, "sovereign"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := base
			in.UnitCode = "UIC-1"
			c.mutate(&in)
			m, err := assembleManifest(hints, in)
			if err != nil {
				t.Fatalf("assembleManifest: %v", err)
			}
			v := getPath(m, "affiliation.kind")
			got, set := v.(string)
			if set != c.wantSet || got != c.wantKind {
				t.Fatalf("affiliation.kind = (%q, set=%v), want (%q, set=%v)", got, set, c.wantKind, c.wantSet)
			}
			if scheme, _ := getPath(m, "affiliation.unit_code.scheme").(string); scheme != c.wantScheme {
				t.Fatalf("unit_code.scheme = %q, want %q", scheme, c.wantScheme)
			}
		})
	}
}

// Tier and handle remain the required set; a blank tier is still incomplete.
func TestAssembleManifest_BlankTierStillIncomplete(t *testing.T) {
	hints, err := loadIntakeHints()
	if err != nil {
		t.Fatalf("loadIntakeHints: %v", err)
	}
	_, err = assembleManifest(hints, effectiveInputs{Handle: "h", Email: "h@example.test", Sector: "commercial", AffiliationID: "kind-org"})
	var inc *incompleteManifestError
	if !errors.As(err, &inc) {
		t.Fatalf("want incompleteManifestError, got %v", err)
	}
	if got := inc.Error(); !strings.Contains(got, "tier") || strings.Contains(got, "affiliation") {
		t.Fatalf("missing set must name tier and not affiliation, got %q", got)
	}
}
