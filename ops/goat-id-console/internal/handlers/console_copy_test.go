package handlers

import (
	"context"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/kratos"
	"github.com/usareur-af/goatcore/ops/goat-id-console/web"
)

// `GET /` was a catch-all, so every unrouted path answered the Users page with
// a 200: on a build that predated the Holds page, /holds did exactly that
// (#1500). Only the root serves Users now; anything unrouted is a 404.
func TestRoutes_unknownPathIs404(t *testing.T) {
	s := newOnboardTestServer(t)
	// The Users page lists identities; an empty directory is enough to tell
	// a routed page from a 404.
	dir := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("[]"))
	}))
	t.Cleanup(dir.Close)
	s.Kratos = map[string]*kratos.Client{"tenant": kratos.NewClient(dir.URL)}
	h := s.Routes()
	get := func(path string) int {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	for _, p := range []string{"/nope", "/users", "/holds/extra"} {
		if code := get(p); code != http.StatusNotFound {
			t.Errorf("GET %s: want 404, got %d", p, code)
		}
	}
	for _, p := range []string{"/", "/holds"} {
		if code := get(p); code == http.StatusNotFound {
			t.Errorf("GET %s: routed page answered 404", p)
		}
	}
}

// The count read "376 identityies": the plural added "ies" to "identity".
func TestTemplates_usersListCountsIdentitiesInPlainEnglish(t *testing.T) {
	cases := []struct {
		users []userRow
		want  string
	}{
		{nil, "0 identities"},
		{[]userRow{{ID: "a"}}, "1 identity"},
		{[]userRow{{ID: "a"}, {ID: "b"}}, "2 identities"},
	}
	for _, c := range cases {
		out := renderPage(t, "users_list", &pageVars{Users: c.users})
		if !strings.Contains(out, c.want) {
			t.Errorf("%d users: want %q in the count", len(c.users), c.want)
		}
		if strings.Contains(out, "identityies") {
			t.Errorf("%d users: count still reads \"identityies\"", len(c.users))
		}
	}
}

// The voice rules (docs/design/design-system.md, "Voice + casing") allow no
// emoji in UI text. This walks every embedded template and static file for a
// pictograph, so the #1500 sweep stays swept.
func TestWebAssets_carryNoEmoji(t *testing.T) {
	err := fs.WalkDir(web.FS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(web.FS, path)
		if err != nil {
			return err
		}
		if !utf8.Valid(data) {
			return nil // binary asset
		}
		for i, line := range strings.Split(string(data), "\n") {
			for _, r := range line {
				if r >= 0x1F300 && r <= 0x1FAFF {
					t.Errorf("%s:%d carries emoji %q (U+%04X)", path, i+1, r, r)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk web.FS: %v", err)
	}
}
