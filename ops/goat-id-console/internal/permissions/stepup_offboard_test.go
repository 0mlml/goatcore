package permissions

import (
	"os"
	"path/filepath"
	"testing"
)

// The console's real-cut offboard (goat#372) sits behind the AAL2 step-up
// gate the same way delete_identity does. This pins the canonical YAML so
// the gate cannot be dropped without a CI signal — the handler consults
// step_up_required at runtime, so a missing entry silently un-gates the
// button.
func TestStepUpList_IncludesOffboardIdentity(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	yamlPath := filepath.Join(wd, "..", "..", "..", "permissions", "group-permissions.yml")
	if _, err := os.Stat(yamlPath); err != nil {
		t.Skipf("canonical YAML not at %s: %v", yamlPath, err)
	}
	m, err := Load(yamlPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !m.IsStepUpAction("offboard_identity") {
		t.Error(`step_up_required must list "offboard_identity" — the console's real-cut offboard button is AAL2-gated (goat#372)`)
	}
}

func TestStepUpList_IncludesReleaseHold(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	yamlPath := filepath.Join(wd, "..", "..", "..", "permissions", "group-permissions.yml")
	m, err := Load(yamlPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !m.IsStepUpAction("release_hold") {
		t.Error(`step_up_required must list "release_hold"`)
	}
}
