package handlers

import (
	"flag"
	"os"
	"testing"
)

var browserFixturesDir = flag.String("browser-fixtures-dir", "", "directory for rendered browser test fixtures")

// Render the production templates for browser states that require an AAL2
// session or external offboarding probes. No fixture route is exposed by the server.
func TestAccessibilityBrowserFixtures(t *testing.T) {
	dir := *browserFixturesDir
	if dir == "" {
		dir = t.TempDir()
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	})
	write := func(name, page string, vars *pageVars) {
		t.Helper()
		if err := root.WriteFile(name+".html", []byte(renderPage(t, page, vars)), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, factor := range []bool{false, true} {
		name := "enroll"
		if factor {
			name = "verify"
		}
		write(name, "step_up", &pageVars{StepUp: &stepUpVars{Enforced: true}, StepUpChallenge: &stepUpChallengeVars{
			ActionLabel: "Delete identity", Why: "This action is irreversible", HasSecondFactor: factor,
			VerifyURL: "https://auth.example.test/verify", EnrollURL: "https://auth.example.test/enroll",
			ReturnTo: "/users", WindowMinutes: 5,
		}})
	}
	write("resume", "step_up_resume", &pageVars{StepUpResume: &stepUpResumeVars{
		ActionLabel: "Delete identity", PostTo: "/users/fixture/delete",
	}})
	for _, state := range []string{"clear", "lingering", "unverified"} {
		write(state, "offboard_verify", &pageVars{OffboardVerify: &offboardVerifyVars{
			Person: "fixture@example.test", Verdict: state, Receipt: "none",
		}})
	}
}
