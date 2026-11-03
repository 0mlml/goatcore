package handlers

import (
	"encoding/json"
	"io/fs"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-id-console/web"
)

// netbird.handset_limit (ADR 1175) defaults to 1 from the intake form's
// defaults block: a person who is never asked still gets one handset key.
func TestAssembleManifestHandsetLimitDefaultsToOne(t *testing.T) {
	hints, err := loadIntakeHints()
	if err != nil {
		t.Fatalf("loadIntakeHints: %v", err)
	}
	m, err := assembleManifest(hints, effectiveInputs{
		Tier: "browse", Affiliation: "partner", AffiliationID: "handset-org", Handle: "test-handle", Email: "test@example.test",
	})
	if err != nil {
		t.Fatalf("assembleManifest: %v", err)
	}
	got, ok := toInt(getPath(m, "netbird.handset_limit"))
	if !ok || got != 1 {
		t.Fatalf("netbird.handset_limit: want 1 from the form defaults, got %v", getPath(m, "netbird.handset_limit"))
	}
}

// The admin form asks the handset question beside the setup-key reuse
// question, in the same section, with the 0–5 range stated.
func TestIntakeFormAsksForHandsets(t *testing.T) {
	raw, err := fs.ReadFile(web.FS, "static/admin/onboard-intake-form.json")
	if err != nil {
		t.Fatalf("read form: %v", err)
	}
	var form struct {
		Sections []struct {
			Title  string `json:"title"`
			Fields []struct {
				Path    string `json:"path"`
				Label   string `json:"label"`
				Type    string `json:"type"`
				Default any    `json:"default"`
				Help    string `json:"help"`
			} `json:"fields"`
		} `json:"sections"`
		Defaults map[string]any `json:"defaults"`
	}
	if err := json.Unmarshal(raw, &form); err != nil {
		t.Fatalf("parse form: %v", err)
	}
	found := false
	for _, s := range form.Sections {
		var sawUsage bool
		for _, f := range s.Fields {
			if f.Path == "netbird.usage_limit" {
				sawUsage = true
			}
			if f.Path == "netbird.handset_limit" {
				found = true
				if !sawUsage {
					t.Errorf("netbird.handset_limit must follow netbird.usage_limit in section %q", s.Title)
				}
				if f.Type != "number" || f.Label != "Phones or tablets" {
					t.Errorf("handset question shape: type=%q label=%q", f.Type, f.Label)
				}
				if d, ok := toInt(f.Default); !ok || d != 1 {
					t.Errorf("handset question default: want 1, got %v", f.Default)
				}
			}
		}
	}
	if !found {
		t.Fatalf("netbird.handset_limit question not in the admin form")
	}
	if d, ok := toInt(form.Defaults["netbird.handset_limit"]); !ok || d != 1 {
		t.Fatalf("defaults block: netbird.handset_limit want 1, got %v", form.Defaults["netbird.handset_limit"])
	}
}
