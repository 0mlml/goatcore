package handlers

import (
	"strings"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/config"
)

// TestOnboardedEmailShowsBothEnrollShapes: the onboarded (enrollment
// instructions) email prints the mesh-join line for BOTH operating systems,
// each under its own label, when a setup key and a management URL are both
// configured. GH #944: a Windows enrollee cannot use the sudo-only line, and
// this branch had no test entering it.
func TestOnboardedEmailShowsBothEnrollShapes(t *testing.T) {
	srv := &Server{Cfg: &config.Config{
		NotifyGoatnet:   "zagros",
		NotifyMgmtURL:   "https://netbird.zagros.example",
		NotifyRosenpass: false,
	}}
	_, body, ok := srv.candidateEmail(notifyRequest{
		Event: "onboarded", Env: "zagros", Email: "a@b.test", Name: "Sam", SetupKey: " KEY-123 ",
	})
	if !ok {
		t.Fatal("onboarded event not handled")
	}
	// req.Env is the goatnet this notification is for, and so the --profile the
	// command carries — see welcome.EnrollCommandNamed's doc comment.
	unix := "sudo netbird up --profile zagros --management-url https://netbird.zagros.example --setup-key KEY-123 --enable-rosenpass=false --rosenpass-permissive=false"
	win := strings.TrimPrefix(unix, "sudo ")
	for _, must := range []string{
		"Enroll your machine onto the mesh",
		"macOS / Linux:\r\n    " + unix + "\r\n",
		"Windows (administrator Command Prompt):\r\n    " + win + "\r\n",
	} {
		if !strings.Contains(body, must) {
			t.Errorf("onboarded email missing %q\n---\n%s", must, body)
		}
	}
	// The Windows line is the ONLY unprefixed `netbird up`; the key is trimmed
	// in both shapes and never printed bare when the full command is available.
	if n := strings.Count(body, "\n    netbird up "); n != 1 {
		t.Errorf("want exactly one unprefixed netbird up line, got %d\n---\n%s", n, body)
	}
	if strings.Contains(body, "Your mesh enrollment key:") {
		t.Errorf("bare-key fallback must not appear when the URL is configured\n---\n%s", body)
	}
}

// TestOnboardedEmailProfileFallsBackToTheServedGoatnet: a payload without env
// must still print a runnable --profile. The <goatnet> placeholder beside a
// real setup key is a shell input redirect, so the pasted command would fail.
func TestOnboardedEmailProfileFallsBackToTheServedGoatnet(t *testing.T) {
	srv := &Server{Cfg: &config.Config{
		NotifyGoatnet: "zagros-prod",
		NotifyMgmtURL: "https://netbird.zagros.example",
	}}
	for _, tc := range []struct{ env, want string }{
		{"", "zagros-prod"},
		{"zagros", "zagros"},
	} {
		_, body, ok := srv.candidateEmail(notifyRequest{
			Event: "onboarded", Env: tc.env, Email: "a@b.test", SetupKey: "KEY-123",
		})
		if !ok {
			t.Fatal("onboarded event not handled")
		}
		if n := strings.Count(body, "netbird up --profile "+tc.want+" --management-url"); n != 2 {
			t.Errorf("env %q: want both enroll lines on --profile %s, got %d\n---\n%s", tc.env, tc.want, n, body)
		}
		if strings.Contains(body, "<goatnet>") {
			t.Errorf("env %q: placeholder printed beside a real key\n---\n%s", tc.env, body)
		}
	}
}
