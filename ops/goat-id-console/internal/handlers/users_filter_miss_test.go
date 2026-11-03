package handlers

// The users list filters Kratos logins; a candidate has none until the mint.
// A miss there hands the typed term to the onboarding page's cross-stage find
// (#1306), in the URL fragment so it reaches no server log, and only once the
// term is long enough for the find to act on. This pins the hand-off markup
// and the way it is built.

import (
	"strings"
	"testing"
)

func TestTemplates_usersListFilterMissHint(t *testing.T) {
	out := renderPage(t, "users_list", &pageVars{})
	for _, want := range []string{`id="user-filter-miss"`, `id="user-filter-miss-link"`, `id="user-filter-miss-term"`, "'/onboarding#q=' + encodeURIComponent", "needle.length >= 2 && shown === 0", "textContent"} {
		if !strings.Contains(out, want) {
			t.Errorf("users list lacks %q", want)
		}
	}
	if strings.Contains(out, "innerHTML") {
		t.Error("the miss hint must never be built with innerHTML")
	}
	if strings.Contains(out, "/onboarding?q=") {
		t.Error("the hand-off must carry the term in the fragment, never the query string")
	}
}
