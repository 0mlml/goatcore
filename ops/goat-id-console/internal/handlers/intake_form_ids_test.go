package handlers

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-id-console/web"
)

// Every element id the intake form's script looks up by literal must exist in
// the form's markup. #569 renamed the coalition <select> to alliance_standing
// and left the submit handler reading getElementById("coalition").value: the
// handler threw on the null before its fetch, so "Submit for review" did
// nothing, showed nothing, and the invite stayed open (ZAGROS).
//
// Rows assembled at runtime (tierRow-sel, mqRow-inp, ...) are built from string
// concatenation, not literals, so they sit outside this check by construction.
func TestIntakeForm_scriptIdsExistInMarkup(t *testing.T) {
	html := readIntakeForm(t)
	re := regexp.MustCompile(`getElementById\("([^"]+)"\)`)
	seen := map[string]bool{}
	for _, m := range re.FindAllStringSubmatch(html, -1) {
		id := m[1]
		// Rows assembled at runtime (tierRow-sel, mqRow-inp, ...) are built from
		// string concatenation inside the script rather than markup literals, so
		// they deliberately sit outside this check by construction.
		if id == "tierRow-sel" || id == "mqRow-inp" {
			continue
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		if !strings.Contains(html, `id="`+id+`"`) {
			t.Errorf("script looks up #%s but the form has no element with that id", id)
		}
	}
	if len(seen) == 0 {
		t.Fatal("no getElementById literals found; the regexp no longer matches the form")
	}
}

// The submission carries the alliance-standing pick under the key the engine
// prefers (candidateSubmission.Standing, json alliance_standing). The legacy
// `coalition` key is still accepted server-side, but the form no longer has a
// control by that name, so it must not read one.
func TestIntakeForm_submitsAllianceStanding(t *testing.T) {
	html := readIntakeForm(t)
	if !strings.Contains(html, `alliance_standing: document.getElementById("alliance_standing").value`) {
		t.Error("submit handler does not send alliance_standing from the #alliance_standing control")
	}
	if strings.Contains(html, `getElementById("coalition")`) {
		t.Error("submit handler still reads the retired #coalition control")
	}
}

func readIntakeForm(t *testing.T) string {
	t.Helper()
	raw, err := fs.ReadFile(web.FS, "static/onboard.html")
	if err != nil {
		t.Fatalf("read intake form: %v", err)
	}
	return string(raw)
}
