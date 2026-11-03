package web

// The header nav's dropdowns are native <details> elements, which toggle
// INDEPENDENTLY — open three and you get three menus stacked over each other and
// over the page. nav.js is the only thing enforcing single-open, closing on an
// outside click, and closing on Escape.
//
// progress.html shipped the dropdown markup without the script and reached ZAGROS
// that way. Nothing failed: no console error, no broken layout, no test. The
// markup was right and the script was simply absent, so the page looks correct
// until someone opens a second menu — which is why it survived review and a
// deploy.
//
// The invariant is on the DROPDOWNS, not the container: `nav-grouped` is a
// styling class that flat navs use too (data-access.html has it with plain
// links and no dropdowns, and correctly does not load nav.js). What obliges a
// page to load the script is owning a `details class="nav-group"`.

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"
)

// Matches the dropdown element nav.js manages: `nav.nav-grouped details.nav-group`.
var navGroupDetails = regexp.MustCompile(`<details[^>]*class="[^"]*\bnav-group\b`)

func TestNav_PagesWithDropdownsLoadNavJS(t *testing.T) {
	pages, err := navPages()
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(pages) == 0 {
		t.Fatal("no embedded HTML pages found — the embed or this glob is wrong")
	}

	var withDropdowns int
	for _, p := range pages {
		b, err := fs.ReadFile(Static, p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		body := string(b)
		if !navGroupDetails.MatchString(body) {
			continue // no dropdowns on this page; nav.js has nothing to manage
		}
		withDropdowns++
		if !strings.Contains(body, "/nav.js") {
			t.Errorf("%s has dropdown menus (details.nav-group) but never loads /nav.js — "+
				"opening one menu will not close the others, so they stack on top of "+
				"each other and over the page", p)
		}
	}

	if withDropdowns == 0 {
		t.Fatal("no page matched details.nav-group — the markup was restructured and this " +
			"test is now vacuous. Point it at whatever the dropdowns became; do not delete it.")
	}
	t.Logf("%d page(s) carry dropdown menus and load nav.js", withDropdowns)
}

// The converse is worth holding too: nav.js selects strictly
// `nav.nav-grouped details.nav-group`, so a dropdown that grows outside a
// nav.nav-grouped container is unmanaged in exactly the same invisible way.
func TestNav_DropdownsLiveInsideAGroupedNav(t *testing.T) {
	pages, err := navPages()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range pages {
		b, err := fs.ReadFile(Static, p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		body := string(b)
		if !navGroupDetails.MatchString(body) {
			continue
		}
		if !strings.Contains(body, `<nav class="nav-grouped"`) {
			t.Errorf("%s has details.nav-group but no <nav class=\"nav-grouped\"> wrapper — "+
				"nav.js selects `nav.nav-grouped details.nav-group` and would not find them", p)
		}
	}
}

func navPages() ([]string, error) {
	var pages []string
	err := fs.WalkDir(Static, "static", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.HasSuffix(path, ".html") {
			pages = append(pages, path)
		}
		return nil
	})
	return pages, err
}

func TestNav_StartsClosedWithOperatorControlsHidden(t *testing.T) {
	pages, err := navPages()
	if err != nil {
		t.Fatal(err)
	}
	grouped := regexp.MustCompile(`(?s)<nav class="nav-grouped">.*?</nav>`)
	opened := regexp.MustCompile(`<details[^>]*\sopen(?:\s|>|=)`)
	for _, path := range pages {
		data, err := fs.ReadFile(Static, path)
		if err != nil {
			t.Fatal(err)
		}
		nav := grouped.FindString(string(data))
		if nav == "" {
			continue
		}
		if opened.MatchString(nav) {
			t.Errorf("%s starts with an open menu", path)
		}
		if !strings.Contains(nav, `class="nav-group operator-only hidden"`) {
			t.Errorf("%s exposes the Admin menu before authentication", path)
		}
		for _, href := range []string{"/progress.html", "/data-access.html", "/admin/trashgoat.html", "/peering/echo.html"} {
			if !strings.Contains(nav, `href="`+href+`"`) {
				t.Errorf("%s is missing %s", path, href)
			}
		}
	}
}
