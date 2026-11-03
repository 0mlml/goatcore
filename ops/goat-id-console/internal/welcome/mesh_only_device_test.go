package welcome

import (
	"strings"
	"testing"
)

// #1586: when a person has enrolled only a headless server and reads the welcome
// email on a workstation that is not on the mesh, the login link cannot resolve
// there. The email must say so next to the link and next to the
// /fabric-credential instruction, not only in the notes at the end.

func TestRender_loginLinkSaysMeshDeviceAndServerDoesNotCount(t *testing.T) {
	for name, a := range map[string]Artifacts{
		"code":      {SetupKey: "K", RecoveryLink: "https://auth.example/recovery?flow=q", RecoveryCode: "123456"},
		"link only": {SetupKey: "K", RecoveryLink: "https://auth.example/recovery?flow=q"},
	} {
		out := Render(zagrosCatalog(), Recipient{Name: "New", Handle: "new", Tier: "browse"}, a)
		step2 := out[strings.Index(out, "STEP 2"):]
		if i := strings.Index(step2, "STEP 3"); i > 0 {
			step2 = step2[:i]
		}
		for _, m := range []string{"connected to the mesh", "only a server", "enroll the computer", "same setup key"} {
			if !strings.Contains(step2, m) {
				t.Errorf("%s: step 2 missing %q beside the login link\n---\n%s", name, m, step2)
			}
		}
	}
}

func TestRender_fabricCredentialSaysServerDoesNotCount(t *testing.T) {
	out := Render(zagrosCatalog(),
		Recipient{Name: "Dev", Handle: "dev", Tier: "developer", Groups: []string{"DEVELOPER"}, FabricAccess: true, Publisher: true},
		Artifacts{SetupKey: "K", CredentialDeferred: true})
	i := strings.Index(out, "/fabric-credential")
	if i < 0 {
		t.Fatalf("deferred email names no /fabric-credential page\n---\n%s", out)
	}
	near := out[i:min(len(out), i+600)]
	for _, m := range []string{"on a device that is on the mesh", "A server you enrolled does not count"} {
		if !strings.Contains(near, m) {
			t.Errorf("fabric-credential instruction missing %q\n---\n%s", m, near)
		}
	}
}

func TestGuide_troubleshootingHasLoginLinkNotOnMesh(t *testing.T) {
	tp, ok := Topics["troubleshooting"]
	if !ok {
		t.Fatal("no troubleshooting topic")
	}
	for _, sec := range tp.Sections {
		if strings.Contains(sec.Heading, "login link") {
			body := strings.Join(sec.Paras, " ")
			for _, m := range []string{"only on the mesh", "server", "same setup key"} {
				if !strings.Contains(body, m) {
					t.Errorf("login-link entry missing %q: %s", m, body)
				}
			}
			return
		}
	}
	t.Error("troubleshooting has no entry for a login link that does not open (#1586)")
}
