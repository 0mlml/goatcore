package onboardlint

import (
	"strings"
	"testing"
)

// A valid post-cutover DEVELOPER-tier manifest (the DEMO-ORG shape).
const validDeveloper = `
identity:
  handle: demo-org-008
  email: "demo-org-008@example.org"
  name: "DEMO-ORG 008"
  status: active
  added: 2026-06-24
  added_by: person-900@example.org
vendor:
  display_name: "DEMO-ORG"
  short_tag: demo-org-008
affiliation:
  id: demo-org
  kind: partner
  nation: POL
  display_name: "DEMO-ORG"
netbird:
  enabled: true
  sites: [zagros]
  role: peer
  ttl_days: 7
  usage_limit: 5
  auto_groups: [DEVELOPER, OBSERVER]
ory_site:
  enabled: true
  sites: [zagros]
  groups: []
  credentials: {password: true, passkey: false}
  netbird_mgmt_admin: false
ory_idp:
  enabled: false
  sites: []
  groups: []
  fido2_required: false
zenoh:
  enabled: true
  trial:
    vendor: demo-org-008
`

func TestLint_ValidDeveloper(t *testing.T) {
	r := Lint([]byte(validDeveloper), "yaml")
	if !r.OK {
		t.Fatalf("expected OK, got findings: %+v", r.Findings)
	}
	if r.Shape != "vendor-primary" {
		t.Errorf("shape = %q, want vendor-primary", r.Shape)
	}
}

func TestLint_MissingAffiliationAfterCutover(t *testing.T) {
	m := strings.Replace(validDeveloper, `affiliation:
  id: demo-org
  kind: partner
  nation: POL
  display_name: "DEMO-ORG"
`, "", 1)
	r := Lint([]byte(m), "yaml")
	if r.OK {
		t.Fatal("expected NOT OK (missing affiliation post-cutover)")
	}
	found := false
	for _, f := range r.Findings {
		if f.Field == "identity-grammar" && strings.Contains(f.Message, "affiliation") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected identity-grammar affiliation finding, got %+v", r.Findings)
	}
}

func TestLint_BadEnumGroup(t *testing.T) {
	m := strings.Replace(validDeveloper, "auto_groups: [DEVELOPER, OBSERVER]", "auto_groups: [NONSENSE, OBSERVER]", 1)
	r := Lint([]byte(m), "yaml")
	if r.OK {
		t.Fatal("expected NOT OK (bad enum group)")
	}
	hit := false
	for _, f := range r.Findings {
		if strings.HasPrefix(f.Field, "netbird.auto_groups") {
			hit = true
		}
	}
	if !hit {
		t.Errorf("expected a netbird.auto_groups field finding, got %+v", r.Findings)
	}
}

func TestLint_DeprecatedVendorGroup(t *testing.T) {
	// VENDOR is schema-valid (legacy alias) but the grammar gate forbids it
	// post-cutover. Use it WITH an affiliation block so only the deprecated-
	// group rule fires.
	m := strings.Replace(validDeveloper, "auto_groups: [DEVELOPER, OBSERVER]", "auto_groups: [VENDOR, OBSERVER]", 1)
	r := Lint([]byte(m), "yaml")
	if r.OK {
		t.Fatal("expected NOT OK (deprecated VENDOR group post-cutover)")
	}
	hit := false
	for _, f := range r.Findings {
		if f.Field == "identity-grammar" && strings.Contains(f.Message, "VENDOR") {
			hit = true
		}
	}
	if !hit {
		t.Errorf("expected deprecated-group finding for VENDOR, got %+v", r.Findings)
	}
}

func TestLint_JSONInput(t *testing.T) {
	// The webform submits JSON; confirm the JSON path validates equivalently.
	r := Lint([]byte(`{"not":"a manifest"}`), "json")
	if r.OK {
		t.Fatal("expected NOT OK for a non-manifest JSON object")
	}
}

func TestGrammarRegression_quotesOnlyKnownDeprecatedNames(t *testing.T) {
	// The quoted name comes from the fixed deprecated table, so a submitted
	// group carrying a quote is never echoed into the message.
	data := map[string]any{
		"identity":    map[string]any{"added": "2999-01-01"},
		"affiliation": map[string]any{"id": "example"},
		"netbird":     map[string]any{"auto_groups": []any{"VENDOR", "x' OR 'y", "OBSERVER"}},
	}
	errs := grammarRegressionErrors(data)
	if len(errs) != 1 || !strings.Contains(errs[0], "entry 'VENDOR'") {
		t.Fatalf("want one finding naming VENDOR, got %q", errs)
	}
	if strings.Contains(errs[0], "x' OR 'y") {
		t.Fatalf("submitted group text reached the message: %q", errs[0])
	}
}
