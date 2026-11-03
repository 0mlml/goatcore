package welcome

import (
	"regexp"
	"strings"
	"testing"
)

func devPackage() Package {
	return RenderPackage(zagrosCatalogWithGuide(),
		Recipient{Name: "Jane Example", Handle: "jane-example", Tier: "developer", Groups: []string{"DEVELOPER"}, FabricAccess: true, Publisher: true},
		Artifacts{SetupKey: "ABCD-1234", PortalAPIKey: "vp_abc_secret",
			RecoveryLink: "https://auth.example/recovery?flow=q", RecoveryCode: "123456",
			BundleURL: "https://portal.example/api/org-bundle/a/b.tar.gz"})
}

// The text part is the record. Building the document must not change it:
// Render (the string the whole test file above pins) and Package.Text are
// the same bytes.
func TestPackage_textIsRender(t *testing.T) {
	pkg := devPackage()
	text := Render(zagrosCatalogWithGuide(),
		Recipient{Name: "Jane Example", Handle: "jane-example", Tier: "developer", Groups: []string{"DEVELOPER"}, FabricAccess: true, Publisher: true},
		Artifacts{SetupKey: "ABCD-1234", PortalAPIKey: "vp_abc_secret",
			RecoveryLink: "https://auth.example/recovery?flow=q", RecoveryCode: "123456",
			BundleURL: "https://portal.example/api/org-bundle/a/b.tar.gz"})
	if pkg.Text != text {
		t.Fatal("Package.Text differs from Render")
	}
}

// The HTML part carries every word of the text part, in order. A client that
// shows the HTML shows the same email; nothing is summarized away or moved.
func TestPackage_htmlCarriesEveryWordInOrder(t *testing.T) {
	pkg := devPackage()
	stripped := regexp.MustCompile(`<[^>]+>`).ReplaceAllString(pkg.HTML, " ")
	stripped = strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">", "&#34;", `"`, "&#39;", "'", "&quot;", `"`).Replace(stripped)
	// Compare words with their edge punctuation removed: a URL that the text
	// closes with `".` becomes <a>url</a>". in HTML, which is the same words.
	norm := func(ws []string) []string {
		out := ws[:0]
		for _, w := range ws {
			if w = strings.Trim(w, "\"'.,;:()`"); w != "" {
				out = append(out, w)
			}
		}
		return out
	}
	got := norm(strings.Fields(stripped))
	// Text words, minus the typewriter rules and the note letters/heading the
	// HTML renders as list markers and a heading of its own.
	want := norm(strings.Fields(strings.NewReplacer("============================================================", " ").Replace(pkg.Text)))
	gi := 0
	for _, w := range want {
		// List markers ("*", "-", "1)", "A.") become HTML list markers, and the
		// "!!" of the callout title becomes its box; the words stay.
		if regexp.MustCompile(`^([A-Z]|\d+|\*|-|!!)$`).MatchString(w) {
			continue
		}
		found := false
		for gi < len(got) {
			if got[gi] == w {
				found = true
				gi++
				break
			}
			gi++
		}
		if !found {
			t.Fatalf("HTML part lost or reordered %q\n--- html ---\n%s", w, pkg.HTML)
		}
	}
}

// Structure, not just words: headings are headings, steps are a list, commands
// and credentials are verbatim blocks, the deadline callout is a callout, the
// notes are a lettered list after everything else.
func TestPackage_htmlStructure(t *testing.T) {
	h := devPackage().HTML
	for _, must := range []string{
		"<h1", ">STEP 1 — join the ZAGROS mesh</h2>", "<ol style=", "<pre style=", "sudo netbird up",
		">Your portal API key (paste this whole string into the Sign in box):</p>", "vp_abc_secret",
		`href="https://auth.example/recovery?flow=q"`, "TIME-SENSITIVE", `<ol type="A"`, ">IF SOMETHING DOES NOT WORK — notes</h2>",
	} {
		if !strings.Contains(h, must) {
			t.Errorf("HTML part missing %q", must)
		}
	}
	if strings.Contains(h, "====") {
		t.Error("typewriter rules leaked into the HTML part")
	}
	if strings.Index(h, `<ol type="A"`) < strings.Index(h, "EVERYTHING ELSE") {
		t.Error("notes must come after the guide pointer")
	}
	// Every credential and command stays intact inside a verbatim block: a
	// <pre> command block, a credential <span>, or the link box a lone address
	// renders as (<a>). "Inside" = the nearest such opening tag before it has
	// not been closed before it.
	for _, verbatim := range []string{"--setup-key ABCD-1234", "vp_abc_secret", "/api/org-bundle/a/b.tar.gz"} {
		at := strings.Index(h, verbatim)
		if at < 0 {
			t.Errorf("%q missing from the HTML part", verbatim)
			continue
		}
		inside := false
		for _, tag := range []string{"pre", "span", "a"} {
			if o := strings.LastIndex(h[:at], "<"+tag); o >= 0 {
				if c := strings.Index(h[o:], "</"+tag+">"); c < 0 || o+c > at {
					inside = true
				}
			}
		}
		if !inside {
			t.Errorf("%q is not inside a verbatim block", verbatim)
		}
	}
}
