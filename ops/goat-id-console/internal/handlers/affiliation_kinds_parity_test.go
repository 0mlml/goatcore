package handlers

import (
	"encoding/json"
	"os"
	"sort"
	"testing"
)

// TestAffiliationKindsMatchSchemaEnum locks affiliationKinds (the served
// selectable-affiliations list) to the affiliation.kind enum in the
// operator-manifest schema — the SoT that also validates every onboarded
// manifest and mirrors the grammar canon
// (docs/design/identity-grammar.md `aff-kind=<vendor|organic|partner|civil>`).
//
// It exists because these drifted: `civil` was added to the schema, both intake
// forms, the welcome help text, and the grammar SoT, but the Go slice kept
// {partner, organic, vendor} — so the served selectable set silently dropped
// `civil`, a value everything downstream accepts. Fail closed on any future gap.
func TestAffiliationKindsMatchSchemaEnum(t *testing.T) {
	raw, err := os.ReadFile("../onboardlint/operator-manifest.schema.json")
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse schema: %v", err)
	}

	// properties.affiliation.properties.kind.enum
	dig := func(m map[string]any, keys ...string) any {
		var cur any = m
		for _, k := range keys {
			mm, ok := cur.(map[string]any)
			if !ok {
				return nil
			}
			cur = mm[k]
		}
		return cur
	}
	rawEnum, ok := dig(doc, "properties", "affiliation", "properties", "kind", "enum").([]any)
	if !ok {
		t.Fatal("could not locate properties.affiliation.properties.kind.enum in the schema")
	}
	schemaEnum := make([]string, 0, len(rawEnum))
	for _, v := range rawEnum {
		schemaEnum = append(schemaEnum, v.(string))
	}

	got := append([]string(nil), affiliationKinds...)
	sort.Strings(got)
	want := append([]string(nil), schemaEnum...)
	sort.Strings(want)

	if len(got) != len(want) {
		t.Fatalf("affiliationKinds %v != schema enum %v (count)", affiliationKinds, schemaEnum)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("affiliationKinds %v != schema enum %v (set mismatch)", affiliationKinds, schemaEnum)
		}
	}
}
