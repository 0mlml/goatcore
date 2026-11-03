package handlers

import (
	"encoding/json"
	"html/template"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestOnboardingScriptDataRoundTrips(t *testing.T) {
	proposal := map[string]string{"id": "fixture", "name": "</script><script>bad()</script> & \"quoted\" \\ path ' apostrophe \u2028"}
	raw, err := json.Marshal(proposal)
	if err != nil {
		t.Fatal(err)
	}
	for _, sponsor := range []bool{false, true} {
		out := renderLayoutFor(t, "onboarding", &pageVars{
			OnboardEnabled: true, SponsorMode: sponsor,
			// #nosec G203 -- synthetic injection fixture; assertions below verify template escaping.
			Proposal: &proposalView{ID: "fixture"}, ProposalJSON: template.JS(raw),
		})
		wantAdmin := `window.GOAT_CONSOLE_ADMIN = "` + strconv.FormatBool(!sponsor) + `" === "true";`
		if !strings.Contains(out, wantAdmin) {
			t.Fatalf("sponsor=%v: admin visibility changed", sponsor)
		}
		match := regexp.MustCompile(`JSON\.parse\(("(?:\\.|[^"\\])*")\)`).FindStringSubmatch(out)
		if len(match) != 2 {
			t.Fatal("proposal JSON string missing")
		}
		var decoded string
		if err := json.Unmarshal([]byte(match[1]), &decoded); err != nil {
			t.Fatalf("decode rendered JavaScript string %q: %v", match[1], err)
		}
		var got map[string]string
		if err := json.Unmarshal([]byte(decoded), &got); err != nil {
			t.Fatal(err)
		}
		if got["name"] != proposal["name"] || got["id"] != proposal["id"] {
			t.Fatalf("proposal data changed: %#v", got)
		}
		if strings.Contains(out, "<script>bad()</script>") {
			t.Fatal("proposal data escaped its script string")
		}
	}
}

func TestOnboardingScriptAbsentDataIsNull(t *testing.T) {
	out := renderLayoutFor(t, "onboarding", &pageVars{
		OnboardEnabled: true, Proposal: &proposalView{ID: "fixture"},
	})
	if !strings.Contains(out, `JSON.parse("null")`) {
		t.Fatal("absent proposal data must be explicit null")
	}
}
