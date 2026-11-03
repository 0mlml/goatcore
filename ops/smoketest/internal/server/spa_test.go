package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// spaHandler stats the requested path to decide between the file and the SPA
// index. A ".." in the path must not let that stat look outside dir: a file
// that exists only outside dir has to fall back to index ("/") like any other
// missing file.
func TestSPAHandlerStatStaysInsideDir(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "dist")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "outside.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app.js"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	for reqPath, want := range map[string]string{
		"/../outside.txt": "/",
		"/app.js":         "/app.js",
		"/missing":        "/",
	} {
		var got string
		h := spaHandler(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			got = r.URL.Path
		}), dir)
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.URL.Path = reqPath // bypass the request parser's cleaning, as a raw handler would see it
		h.ServeHTTP(httptest.NewRecorder(), req)
		if got != want {
			t.Errorf("%s: file server saw %q, want %q", reqPath, got, want)
		}
	}
}
