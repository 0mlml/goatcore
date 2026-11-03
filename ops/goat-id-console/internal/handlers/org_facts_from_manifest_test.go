package handlers

import (
	"reflect"
	"testing"
)

func TestOrgFactsFromManifest_InvalidJSON(t *testing.T) {
	f := orgFactsFromManifest(`{malformed}`)
	if !reflect.DeepEqual(f, manifestOrgFacts{}) {
		t.Errorf("expected empty facts on malformed JSON, got %+v", f)
	}
}

func TestOrgFactsFromManifest_NoAffiliation(t *testing.T) {
	f := orgFactsFromManifest(`{"other": "data"}`)
	if !reflect.DeepEqual(f, manifestOrgFacts{}) {
		t.Errorf("expected empty facts when no affiliation, got %+v", f)
	}
}

func TestOrgFactsFromManifest_WithAffiliation(t *testing.T) {
	f := orgFactsFromManifest(`{
		"affiliation": {
			"id": " test-id ",
			"kind": " partner ",
			"nation": " USA ",
			"sector": " commercial ",
			"gov_branch": " none "
		}
	}`)
	want := manifestOrgFacts{
		AffID:     "test-id",
		Kind:      "partner",
		Nation:    "USA",
		Sector:    "commercial",
		GovBranch: "none",
	}
	if !reflect.DeepEqual(f, want) {
		t.Errorf("got %+v, want %+v", f, want)
	}
}

func TestOrgFactsFromManifest_IntakeRecord(t *testing.T) {
	f := orgFactsFromManifest(`{
		"schema_version": "candidate-intake/v-draft",
		"prebound": {
			"org": " prebound-org ",
			"sector": " state "
		},
		"candidate_submission": {
			"organization": "Submitted Org"
		}
	}`)

	want := manifestOrgFacts{
		AffID:  "prebound-org",
		Sector: "state",
	}
	if f.AffID != want.AffID || f.Sector != want.Sector {
		t.Errorf("got %+v, want AffID=%s, Sector=%s", f, want.AffID, want.Sector)
	}
}

func TestOrgFactsFromManifest_IntakeRecord_FallbackToSubmission(t *testing.T) {
	f := orgFactsFromManifest(`{
		"schema_version": "candidate-intake/v-draft",
		"candidate_submission": {
			"organization": "Submitted Org"
		}
	}`)

	wantAffID := "submitted-org"
	if f.AffID != wantAffID {
		t.Errorf("got AffID=%q, want %q", f.AffID, wantAffID)
	}
}

func TestRewriteManifestAffiliation_InvalidJSON(t *testing.T) {
	_, err := rewriteManifestAffiliation(`{malformed}`, "org")
	if err == nil {
		t.Fatal("expected error on malformed JSON")
	}
}

func TestRewriteManifestAffiliation_Standard(t *testing.T) {
	manifestJSON := `{"affiliation": {"id": "old"}}`
	updated, err := rewriteManifestAffiliation(manifestJSON, "new-canonical-org")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `{"affiliation":{"id":"new-canonical-org"}}`
	if updated != want {
		t.Errorf("got %q, want %q", updated, want)
	}
}
