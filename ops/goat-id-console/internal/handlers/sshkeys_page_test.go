package handlers

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/onboardstore"
	"github.com/usareur-af/goatcore/ops/goat-id-console/web"
)

// The page is where the "a key grants nothing yet" promise is either kept or
// broken, so these assert the WORDS as well as the controls. A page that
// renders an approve button next to the phrase "you now have access" would
// pass a structural test and still mislead the person reading it.
func TestTemplates_sshKeysPage(t *testing.T) {
	base, err := loadTemplates()
	if err != nil {
		t.Fatalf("loadTemplates: %v", err)
	}
	rows := []onboardstore.SSHKey{
		{ID: "sshk-1", Handle: "rtexample5768", Email: "j@example.test",
			KeyType: "ssh-ed25519", Fingerprint: "SHA256:abc", Status: onboardstore.SSHKeySubmitted},
		{ID: "sshk-2", Handle: "user296x", KeyType: "ssh-ed25519",
			Fingerprint: "SHA256:def", Status: onboardstore.SSHKeyInstalled},
		{ID: "sshk-3", Handle: "anninstaller", Email: "a@example.test",
			KeyType: "ssh-ed25519", Fingerprint: "SHA256:ghi",
			Status: onboardstore.SSHKeyApproved, SkipReason: "no_manifest",
			SkipReportedBy: "installer", SkipReportedAt: 1786789008}, // 2026-08-15 UTC
		// A reason from a newer installer than this console binary. Per-goatnet
		// image pins make this skew expected, not anomalous.
		{ID: "sshk-4", Handle: "skewedrow", KeyType: "ssh-ed25519",
			Fingerprint: "SHA256:jkl", Status: onboardstore.SSHKeyApproved,
			SkipReason: "host_unreachable", SkipReportedBy: "installer",
			SkipReportedAt: 1786789008},
		// An annotation that outlived its state — what a failed clear leaves behind.
		{ID: "sshk-5", Handle: "staleannotation", KeyType: "ssh-ed25519",
			Fingerprint: "SHA256:mno", Status: onboardstore.SSHKeyInstalled,
			SkipReason: "no_manifest", SkipReportedBy: "installer",
			SkipReportedAt: 1786789008},
	}
	cases := []struct {
		name string
		vars *pageVars
		must []string
		not  []string
	}{
		{
			name: "self view states plainly that nothing is granted yet",
			vars: &pageVars{SSHKeySelf: true, SSHKeyHandle: "rtexample5768", SSHKeys: rows[:1]},
			must: []string{"gives you no access", "Waiting on an admin", "PRIVATE KEY", "ssh-keygen -t ed25519"},
			// The self view must never render decision controls: the person
			// whose key it is cannot be the one who approves it.
			not: []string{"/approve", "/reject", "/revoke"},
		},
		{
			name: "queue separates approved from installed and offers the decisions",
			vars: &pageVars{SSHKeys: rows[:2], SSHKeyStatus: onboardstore.SSHKeySubmitted},
			must: []string{
				"does <strong>not</strong> install it",
				"/ssh-keys/sshk-1/approve", "/ssh-keys/sshk-1/reject",
				// installed rows stay revocable — revocation must reach every live state
				"/ssh-keys/sshk-2/revoke",
			},
			// A submitted key has not been decided, so it must not offer revoke.
			not: []string{"/ssh-keys/sshk-1/revoke"},
		},
		{
			name: "queue renders canonical skip annotation wording",
			vars: &pageVars{SSHKeys: rows[2:], SSHKeyStatus: onboardstore.SSHKeyApproved},
			must: []string{
				"skipped 2026-08-15 UTC by installer",
				"no operator manifest",
			},
		},
		{
			// An unknown reason must not render as nothing: an empty annotation is
			// byte-identical to "never skipped", which is the false signal this
			// feature exists to remove. The raw value is never echoed.
			name: "unrecognized reason says so instead of rendering nothing",
			vars: &pageVars{SSHKeys: rows[3:4], SSHKeyStatus: onboardstore.SSHKeyApproved},
			must: []string{
				"skipped 2026-08-15 UTC by installer",
				"reason not recognized by this console",
			},
			not: []string{"host_unreachable"},
		},
		{
			// MarkSSHKeyInstalled clears the annotation in a second statement, so a
			// failed clear can leave one behind. The queue must not then show an
			// installed key as though the installer had refused it.
			name: "annotation is never rendered beside a non-approved status",
			vars: &pageVars{SSHKeys: rows[4:5], SSHKeyStatus: onboardstore.SSHKeyInstalled},
			not:  []string{"skipped 2026-08-15", "no operator manifest"},
		},
		{
			name: "empty queue says so rather than rendering a bare table",
			vars: &pageVars{SSHKeys: nil, SSHKeyStatus: "approved"},
			must: []string{"Nothing in this state"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tm, err := base.Clone()
			if err != nil {
				t.Fatalf("clone: %v", err)
			}
			data, err := fs.ReadFile(web.FS, "templates/ssh_keys.html")
			if err != nil {
				t.Fatalf("read template: %v", err)
			}
			if _, err := tm.New("ssh_keys").Parse(string(data)); err != nil {
				t.Fatalf("parse: %v", err)
			}
			var b strings.Builder
			if err := tm.ExecuteTemplate(&b, "layout", c.vars); err != nil {
				t.Fatalf("execute: %v", err)
			}
			for _, m := range c.must {
				if !strings.Contains(b.String(), m) {
					t.Errorf("render missing %q", m)
				}
			}
			for _, n := range c.not {
				if strings.Contains(b.String(), n) {
					t.Errorf("render must NOT contain %q", n)
				}
			}
		})
	}
}

// The submit banner must describe what actually happened to the record, not
// the happy path (F-381, goat#214: a resubmitted-after-reject key got the
// "recorded, awaiting approval" banner while waiting in no queue at all).
func TestSSHKeySubmitFlashIsStateAware(t *testing.T) {
	cases := []struct {
		status, level string
		must          []string
		not           []string
	}{
		{onboardstore.SSHKeySubmitted, "success",
			[]string{"grants no access yet", "an admin has to approve it"}, nil},
		{onboardstore.SSHKeyApproved, "success",
			[]string{"already approved", "waiting on the installer"},
			[]string{"an admin has to approve it"}},
		{onboardstore.SSHKeyInstalled, "success",
			[]string{"already installed"},
			[]string{"an admin has to approve it"}},
		{onboardstore.SSHKeyRevoked, "error",
			[]string{"revoked", "generate a new keypair"},
			[]string{"Key recorded"}},
	}
	for _, c := range cases {
		t.Run(c.status, func(t *testing.T) {
			f := sshKeySubmitFlash(&onboardstore.SSHKey{Status: c.status, Fingerprint: "SHA256:abc"})
			if f.Level != c.level {
				t.Errorf("level = %q, want %q", f.Level, c.level)
			}
			if !strings.Contains(f.Message, "SHA256:abc") {
				t.Errorf("banner must name the fingerprint: %q", f.Message)
			}
			for _, m := range c.must {
				if !strings.Contains(f.Message, m) {
					t.Errorf("banner for %s missing %q: %q", c.status, m, f.Message)
				}
			}
			for _, n := range c.not {
				if strings.Contains(f.Message, n) {
					t.Errorf("banner for %s must NOT claim %q: %q", c.status, n, f.Message)
				}
			}
		})
	}
}

// flashes() is tiny, but it is what carries the "not installed yet" wording to
// the operator, so a nil-vs-empty slip would silently drop the correction.
func TestFlashesWrapsOnlyWhenPresent(t *testing.T) {
	if flashes(nil) != nil {
		t.Error("nil flash must stay nil so the layout renders no banner")
	}
	got := flashes(&flash{Level: "success", Message: "Approved x. Not installed yet"})
	if len(got) != 1 || !strings.Contains(got[0].Message, "Not installed yet") {
		t.Errorf("flash not carried through: %+v", got)
	}
}
