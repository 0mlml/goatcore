package handlers

// Console nav gap (companion to #1373's CLI-side finding): before this,
// nothing in the authenticated console linked to /operator-key at all, and
// the Guide nav link never defaulted to the viewer's own role even though the
// server already knows it (that's what gates the admin nav showing at all).
// These tests pin both fixes so a future template edit can't silently drop
// them the way the original omission went unnoticed.

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-id-console/web"
)

func TestGuideRoleForGroups(t *testing.T) {
	cases := []struct {
		name   string
		groups []string
		want   string
	}{
		{"platform-admin canonical", []string{"PLATFORM-ADMIN"}, "platform-admin"},
		{"platform-admin legacy alias", []string{"ADMINS"}, "platform-admin"},
		{"iam-admin", []string{"IAM-ADMIN"}, "iam-admin"},
		{"platform-admin takes priority over iam-admin", []string{"IAM-ADMIN", "PLATFORM-ADMIN"}, "platform-admin"},
		{"developer only", []string{"DEVELOPER", "OBSERVER"}, ""},
		{"no groups", nil, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := guideRoleForGroups(c.groups); got != c.want {
				t.Errorf("guideRoleForGroups(%v) = %q, want %q", c.groups, got, c.want)
			}
		})
	}
}

// renderLayoutFor is a minimal render-execute helper matching the pattern in
// templates_smoke_test.go: clone the base template set, parse one page's
// content into it, execute "layout" (which owns the nav) with the given vars.
func renderLayoutFor(t *testing.T, page string, vars *pageVars) string {
	t.Helper()
	base, err := loadTemplates()
	if err != nil {
		t.Fatalf("loadTemplates: %v", err)
	}
	tm, err := base.Clone()
	if err != nil {
		t.Fatalf("clone: %v", err)
	}
	data, err := fs.ReadFile(web.FS, "templates/"+page+".html")
	if err != nil {
		t.Fatalf("read %s: %v", page, err)
	}
	if _, err := tm.New(page).Parse(string(data)); err != nil {
		t.Fatalf("parse %s: %v", page, err)
	}
	var b strings.Builder
	if err := tm.ExecuteTemplate(&b, "layout", vars); err != nil {
		t.Fatalf("execute %s: %v", page, err)
	}
	return b.String()
}

// The admin nav (non-sponsor branch) links to /operator-key.
func TestTemplates_adminNavLinksToOperatorKey(t *testing.T) {
	out := renderLayoutFor(t, "user_detail", &pageVars{User: &userDetail{ID: "x"}})
	if !strings.Contains(out, `href="/operator-key"`) {
		t.Errorf("admin nav missing a link to /operator-key:\n%s", out)
	}
}

// The sponsor nav (narrowed chrome, ADR 1072) ALSO links to /operator-key —
// sponsors are exactly the population most likely to be a freshly-promoted
// platform-admin who has never seen the admin nav yet.
func TestTemplates_sponsorNavLinksToOperatorKey(t *testing.T) {
	out := renderLayoutFor(t, "onboarding", &pageVars{SponsorMode: true})
	if !strings.Contains(out, `href="/operator-key"`) {
		t.Errorf("sponsor nav missing a link to /operator-key:\n%s", out)
	}
}

// The Guide link threads &role= when the viewer resolves to one (platform-
// admin, iam-admin), and stays exactly as before (no role param at all) for
// everyone else — a hand-typed ?topic=identity-admin link keeps working
// unchanged.
func TestTemplates_guideLinkThreadsRole(t *testing.T) {
	withRole := renderLayoutFor(t, "user_detail", &pageVars{
		User: &userDetail{ID: "x"}, GuideURL: "https://guide.example.test", GuideRole: "platform-admin",
	})
	if !strings.Contains(withRole, `href="https://guide.example.test?topic=identity-admin&amp;role=platform-admin"`) {
		t.Errorf("Guide link did not thread role=platform-admin:\n%s", withRole)
	}

	noRole := renderLayoutFor(t, "user_detail", &pageVars{
		User: &userDetail{ID: "x"}, GuideURL: "https://guide.example.test",
	})
	if !strings.Contains(noRole, `href="https://guide.example.test?topic=identity-admin"`) {
		t.Errorf("Guide link without a resolved role should be unchanged from before this fix:\n%s", noRole)
	}
	if strings.Contains(noRole, "&amp;role=") {
		t.Errorf("Guide link should carry no role param when GuideRole is empty:\n%s", noRole)
	}
}

// The admin queue page (/onboarding/operator-keys) cross-links to the
// personal request page — before this, a newly-promoted admin following the
// "Operator keys" nav tab landed only in the decide queue, with no pointer to
// where they'd actually request their own first key.
func TestTemplates_operatorKeyQueueCrossLinksToSelfRequest(t *testing.T) {
	out := renderLayoutFor(t, "operator_keys", &pageVars{OperatorKeyDirPath: "/opt/goat-zenoh-portal/operator-keys.d"})
	if !strings.Contains(out, `href="/operator-key"`) {
		t.Errorf("admin queue page missing a cross-link to /operator-key:\n%s", out)
	}
}
