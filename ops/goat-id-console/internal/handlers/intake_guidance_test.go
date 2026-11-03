package handlers

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-id-console/web"
)

// The intake form must say what a field DECIDES, not just its format. Three of
// the four consequential fields are marked "optional", which reads as
// "unimportant" — and the live cohort produced a candidate's own name in the
// Organization field and two blank GitHub usernames.
//
// Pinned as a test because this is copy: it is the first thing edited away by
// someone tidying the form, and nothing else would notice.
func TestIntakeForm_explainsWhatFieldsDecide(t *testing.T) {
	raw, err := fs.ReadFile(web.FS, "static/onboard.html")
	if err != nil {
		t.Fatalf("read intake form: %v", err)
	}
	html := string(raw)

	for _, want := range []string{
		// Organization: the failure that prompted this. The copy now explains that it is
		// the organization or unit the candidate's access is grouped under, not their
		// own name, and that teammates should all type the same label.
		"organization or unit your access is grouped under",
		"same</strong> name here",
		// GitHub: the consequence of leaving it blank.
		"it is how you get repo access",
		"you get no repositories",
		// Nationality: whose it is, and what follows from it. The field used to
		// be "Nation" and to mean the ORGANISATION's, which is how a person
		// filling in a form about themselves answered it with their own
		// citizenship (ADR 1171). Both halves are asserted: that the form says
		// whose nationality it wants, and that it still says what turns on it.
		"Your own citizenship — <strong>not</strong> your organization's nation",
		"Your citizenship can limit which shared data you may read",
		// Machines: what the number does.
		"device allowance on your enrollment key",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("intake form no longer explains: %q", want)
		}
	}
}
