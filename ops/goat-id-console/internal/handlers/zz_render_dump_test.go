package handlers

import (
	"os"
	"testing"
)

// TestDumpOrgNewPanel writes the rendered create-new-org panel to a file so the
// LAYOUT can be looked at, not just asserted on. Skipped unless GOAT_DUMP_HTML
// names an output path — it is a developer affordance, never a gate.
func TestDumpOrgNewPanel(t *testing.T) {
	out := os.Getenv("GOAT_DUMP_HTML")
	if out == "" {
		t.Skip("set GOAT_DUMP_HTML=<path> to dump the rendered panel")
	}
	html := renderPage(t, "onboarding", &pageVars{
		CSRFToken:     "nonce-123",
		OnboardStatus: "proposed",
		Proposal: &proposalView{
			ID: "prop-8daccb028c699894", Handle: "cutepotatodev",
			Status: "proposed", Editable: true,
			// The REGISTERED-org case: the create-new panel suppresses itself,
			// which is exactly when the operator used to lose the derived fields.
			OrgTyped: "", OrgID: "cis", OrgNation: "SVK",
			OrgDisplayName: "CIS", GitHub: "CutePotatoDev",
		},
	})
	if err := os.WriteFile(out, []byte(html), 0o600); err != nil { //nolint:gosec // developer affordance: the path is the developer's own GOAT_DUMP_HTML, never request input
		t.Fatalf("write %s: %v", out, err)
	}
	t.Logf("wrote %d bytes to %s", len(html), out)
}
