package handlers

import "testing"

// collab.enabled is the per-individual cloud-collaboration-environment decision.
// It is ALWAYS stamped (true|false) so the onboarding record captures the explicit
// choice rather than an ambiguous absence, and it reflects the sponsor's prebound
// Collab input verbatim. The provisioning/activation fields are NOT set by
// assembleManifest — they are stamped later by the (possibly external) provisioner.
func TestAssembleManifestCollabDecision(t *testing.T) {
	hints, err := loadIntakeHints()
	if err != nil {
		t.Fatalf("loadIntakeHints: %v", err)
	}

	base := func(collab bool) effectiveInputs {
		return effectiveInputs{
			Tier:          "browse",
			Affiliation:   "partner",
			AffiliationID: "collab-org",
			Handle:        "test-handle",
			Email:         "test@example.test",
			Collab:        collab,
		}
	}

	for _, c := range []struct {
		name string
		in   bool
	}{
		{"included", true},
		{"excluded", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			m, err := assembleManifest(hints, base(c.in))
			if err != nil {
				t.Fatalf("assembleManifest: %v", err)
			}
			v := getPath(m, "collab.enabled")
			got, ok := v.(bool)
			if !ok {
				t.Fatalf("collab.enabled: want a bool, got %T (%v) — the decision must always be stamped", v, v)
			}
			if got != c.in {
				t.Fatalf("collab.enabled = %v, want %v", got, c.in)
			}
			// The provisioner owns activation — assembleManifest must not presume it.
			if a := getPath(m, "collab.activated"); a != nil {
				t.Fatalf("collab.activated should be unset at assembly time, got %v", a)
			}
		})
	}
}
