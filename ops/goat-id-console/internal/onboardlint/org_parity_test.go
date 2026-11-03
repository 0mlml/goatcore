package onboardlint

import "testing"

// TestClassifyShape_OrgBlockParity is the ADR 1052 landing-zone guard: an
// `org:`-authored manifest must classify identically to the legacy `vendor:`
// shape (the lint prefers org, falls back to vendor). Keeps the org: parallel
// path honest without cutting over the vendor: shape.
func TestClassifyShape_OrgBlockParity(t *testing.T) {
	base := func(blockKey, zenohKey string) map[string]any {
		return map[string]any{
			"netbird": map[string]any{"auto_groups": []any{"DEVELOPER", "OBSERVER"}},
			blockKey:  map[string]any{"display_name": "Acme Corporation", "short_tag": "acme"},
			"zenoh": map[string]any{
				"enabled": true,
				"trial":   map[string]any{zenohKey: "acme-jane"},
			},
		}
	}

	vShape, vDetail := classifyShape(base("vendor", "vendor"))
	oShape, oDetail := classifyShape(base("org", "org"))

	if vShape != oShape || vDetail != oDetail {
		t.Fatalf("org: block classifies differently than vendor:\n vendor => (%q, %q)\n org    => (%q, %q)",
			vShape, vDetail, oShape, oDetail)
	}
	if vShape != "vendor-primary" {
		t.Fatalf("expected vendor-primary, got %q", vShape)
	}

	// Mixed: org key for the block, vendor key for the zenoh prefix (a partially
	// migrated manifest) still resolves the display + prefix.
	mShape, mDetail := classifyShape(base("org", "vendor"))
	if mShape != "vendor-primary" || mDetail != vDetail {
		t.Fatalf("mixed org-block/vendor-prefix classified (%q,%q), want (%q,%q)", mShape, mDetail, vShape, vDetail)
	}
}
