package welcome

import (
	"strings"
	"testing"
)

func TestPasswordHandoffPrecedesStepThree(t *testing.T) {
	r := Recipient{Name: "Example", Handle: "example", Tier: "browse"}
	for _, reissue := range []bool{false, true} {
		for _, code := range []string{"", "123456"} {
			a := Artifacts{RecoveryLink: "https://auth.example/recovery", RecoveryCode: code}
			pkg := RenderPackage(zagrosCatalog(), r, a)
			if reissue {
				pkg = RenderReissuePackage(zagrosCatalog(), r, a)
			}
			for format, out := range map[string]string{"text": pkg.Text, "html": pkg.HTML} {
				start, end := strings.Index(out, "STEP 2 —"), strings.Index(out, "STEP 3 —")
				if start < 0 || end <= start {
					t.Fatalf("%s: missing ordered steps", format)
				}
				step := out[start:end]
				previous := -1
				for _, action := range []string{"recovery", "does not set your password", "Find the Password section", "Enter a new password", "Select the save button", "Wait for confirmation"} {
					pos := strings.Index(step, action)
					if pos <= previous {
						t.Errorf("%s reissue=%v code=%v: %q missing or out of order", format, reissue, code != "", action)
					}
					previous = pos
				}
				for _, recovery := range []string{"validation message", "save again", "expired", "fresh one"} {
					if !strings.Contains(step, recovery) {
						t.Errorf("%s: missing recovery guidance %q", format, recovery)
					}
				}
			}
		}
	}
}

func TestPasswordHandoffDoesNotResetUpgradePassword(t *testing.T) {
	pkg := RenderUpgradePackage(zagrosCatalog(), Recipient{Name: "Example", Handle: "example", Tier: "browse"}, Upgrade{})
	for format, out := range map[string]string{"text": pkg.Text, "html": pkg.HTML} {
		if !strings.Contains(out, "Your existing password keeps working") {
			t.Errorf("%s: missing existing-password guidance", format)
		}
		if strings.Contains(out, "Enter a new password") {
			t.Errorf("%s: upgrade must not instruct a password reset", format)
		}
	}
}

func TestGuideExplainsPasswordSave(t *testing.T) {
	for _, section := range Topics["credentials"].Sections {
		if section.Heading != "Login (password or one-time link)" {
			continue
		}
		body := strings.Join(section.Paras, " ")
		for _, action := range []string{"recovery", "does not set your password", "Password section", "save button", "confirmation", "STEP 3", "validation", "expired", "existing password"} {
			if !strings.Contains(body, action) {
				t.Errorf("guide missing %q", action)
			}
		}
		return
	}
	t.Fatal("login section missing")
}
