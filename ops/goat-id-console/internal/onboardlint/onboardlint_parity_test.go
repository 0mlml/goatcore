package onboardlint

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// validatePyRel is the canonical Python linter, relative to this test file's
// package dir (ops/goat-id-console/internal/onboardlint).
const validatePyRel = "../../../operators/validate.py"

// parityCorpus is a set of manifests spanning the verdict space: valid tiers,
// schema violations, and grammar-gate violations. Keyed by handle so each can
// be written to "<handle>.yml" — validate.py cross-checks filename stem against
// identity.handle, so a mismatched temp name would diverge from the Go linter
// (which validates content, not a file). Naming the file by handle removes that
// artifact and keeps the comparison purely about validation rules.
var parityCorpus = map[string]string{
	// valid DEVELOPER tier (DEMO-ORG shape)
	"demo-org-008": validDeveloper,
	// valid browse-only tier
	"browse-only": `
identity: {handle: browse-only, email: "b@example.org", name: "B", status: active, added: 2026-06-24, added_by: a@example.org}
affiliation: {id: some-unit, kind: organic, nation: USA}
netbird: {enabled: true, sites: [zagros], role: peer, usage_limit: 5, auto_groups: [OBSERVER]}
ory_site: {enabled: true, sites: [zagros], groups: [], credentials: {password: true, passkey: false}}
ory_idp: {enabled: false, sites: [], groups: [], fido2_required: false}
zenoh: {enabled: false}
`,
	// schema violation — bad enum in auto_groups
	"bad-enum": strings.Replace(validDeveloper, "auto_groups: [DEVELOPER, OBSERVER]", "auto_groups: [NONSENSE]", 1),
	// grammar violation — missing affiliation post-cutover
	"no-affil": strings.Replace(validDeveloper, `affiliation:
  id: demo-org
  kind: partner
  nation: POL
  display_name: "DEMO-ORG"
`, "", 1),
	// grammar violation — deprecated VENDOR group (with affiliation present)
	"dep-vendor": strings.Replace(validDeveloper, "auto_groups: [DEVELOPER, OBSERVER]", "auto_groups: [VENDOR, OBSERVER]", 1),
	// schema violation — missing a required block (zenoh)
	"no-zenoh": strings.Replace(validDeveloper, `zenoh:
  enabled: true
  trial:
    vendor: demo-org-008
`, "", 1),
}

type pyResult struct {
	OK bool `json:"ok"`
}

// runValidatePy runs `validate.py --json <file>` and returns the ok verdict.
func runValidatePy(t *testing.T, py, file string) bool {
	t.Helper()
	cmd := exec.Command(py, validatePyRel, "--json", file) //nolint:gosec,noctx // test harness: pyenv-declared interpreter executes repo-pinned validate.py
	out, err := cmd.Output()
	// validate.py exits 1 when a manifest is invalid; that's expected, not a
	// harness failure. Only a missing-interpreter / usage error (no stdout) is.
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("validate.py invocation failed: %v", err)
		}
	}
	var results []pyResult
	if jerr := json.Unmarshal(out, &results); jerr != nil {
		t.Fatalf("validate.py --json output not parseable: %v\noutput: %s", jerr, out)
	}
	if len(results) != 1 {
		t.Fatalf("validate.py returned %d results, want 1", len(results))
	}
	return results[0].OK
}

// TestParity_GoMatchesValidatePy asserts the Go linter and validate.py agree on
// the pass/fail verdict for every corpus manifest. Message wording is library-
// specific and intentionally NOT compared — only the verdict, which is the
// contract that "green in the form ⇒ accepted by the onboard gate" relies on.
func TestParity_GoMatchesValidatePy(t *testing.T) {
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not available; parity test runs in CI where it is present")
	}
	if _, err := os.Stat(validatePyRel); err != nil {
		t.Skipf("validate.py not found at %s: %v", validatePyRel, err)
	}
	// Sanity: ensure jsonschema/pyyaml import (validate.py exits 2 otherwise).
	if out, err := exec.Command(py, "-c", "import yaml, jsonschema").CombinedOutput(); err != nil { //nolint:gosec,noctx // test harness: pyenv-declared interpreter + inline import only
		t.Skipf("python deps (pyyaml/jsonschema) unavailable: %v\n%s", err, out)
	}

	dir := t.TempDir()
	for handle, content := range parityCorpus {
		t.Run(handle, func(t *testing.T) {
			file := filepath.Join(dir, handle+".yml")
			if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			pyOK := runValidatePy(t, py, file)
			goOK := Lint([]byte(content), "yaml").OK
			if pyOK != goOK {
				t.Errorf("verdict divergence for %q: validate.py ok=%v, go ok=%v\nfindings: %+v",
					handle, pyOK, goOK, Lint([]byte(content), "yaml").Findings)
			}
		})
	}
}
