package access

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTemp(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "release-sets.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write temp: %v", err)
	}
	return p
}

func TestLoadReleaseSets_ParsesAndNormalizes(t *testing.T) {
	p := writeTemp(t, `
version: 1
release_sets:
  - set: GALAXY
    prefix: release/GALAXY/
    members: [abc123, def456, abc123, "  ", ""]
  - set: NATO
    # prefix omitted → defaults to release/NATO/
    members: [ghi789]
  - set: "   "        # blank name → skipped (cannot name a lane)
    members: [zzz]
`)
	got, err := LoadReleaseSets(p)
	if err != nil {
		t.Fatalf("LoadReleaseSets: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 sets (blank-named dropped), got %d: %+v", len(got), got)
	}
	galaxy := got[0]
	if galaxy.Set != "GALAXY" || galaxy.Prefix != "release/GALAXY/" {
		t.Errorf("GALAXY set/prefix wrong: %+v", galaxy)
	}
	if len(galaxy.Partners) != 2 || galaxy.Partners[0] != "abc123" || galaxy.Partners[1] != "def456" {
		t.Errorf("GALAXY members not deduped/trimmed: %+v", galaxy.Partners)
	}
	if got[1].Set != "NATO" || got[1].Prefix != "release/NATO/" {
		t.Errorf("NATO default prefix wrong: %+v", got[1])
	}
}

func TestLoadReleaseSets_EmptyDeclaredIsNotAnError(t *testing.T) {
	// A declared-empty registry is a valid governance state (no partners in
	// any set yet), distinct from a missing file.
	p := writeTemp(t, "version: 1\nrelease_sets:\n  - set: GALAXY\n    members: []\n")
	got, err := LoadReleaseSets(p)
	if err != nil {
		t.Fatalf("declared-empty must not error: %v", err)
	}
	if len(got) != 1 || len(got[0].Partners) != 0 {
		t.Fatalf("want one empty set, got %+v", got)
	}
}

func TestLoadReleaseSets_MissingFileFailsClosed(t *testing.T) {
	// Fail-closed: a missing file is an error, never a silent empty picture.
	if _, err := LoadReleaseSets(filepath.Join(t.TempDir(), "nope.yaml")); err == nil {
		t.Fatal("missing release-sets file must fail closed, got nil error")
	}
}

func TestLoadReleaseSets_MalformedFailsClosed(t *testing.T) {
	p := writeTemp(t, "release_sets: [ this is: not valid yaml")
	if _, err := LoadReleaseSets(p); err == nil {
		t.Fatal("malformed release-sets file must fail closed, got nil error")
	}
}

// The reveal wiring: a declared set flows into the COP graph as partner→release
// edges (Assemble already models this; here we lock that LoadReleaseSets output
// composes with it).
func TestReleaseSets_FeedGraphEdges(t *testing.T) {
	rel := []ReleaseIn{{Set: "GALAXY", Prefix: "release/GALAXY/", Partners: []string{"abc123"}}}
	g := Assemble(Sources{Releases: rel})
	var found bool
	for _, e := range g.Edges {
		if e.Kind == EdgeMayReceiveRel {
			found = true
		}
	}
	if !found {
		t.Fatal("declared release-set did not produce a may-receive edge in the COP graph")
	}
}
