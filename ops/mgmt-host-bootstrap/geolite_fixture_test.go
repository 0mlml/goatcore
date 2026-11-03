// geolite_fixture_test.go — seeding the MaxMind geolocation databases the
// integration stack would otherwise download on every startup.
//
// NO build tag on purpose: the seeding logic is plain filesystem work, so it
// is unit-testable without docker and gets covered by the fast `unit` job as
// well as the integration one that uses it.
//
// Background: testdata/geolite/README.md.

package main

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

// geolocationFixtureDir is the seed directory. CI populates it from an
// actions/cache; locally it is filled by the warm command in its README.
const geolocationFixtureDir = "testdata/geolite"

// seedGeolocationDatadir copies the cached MaxMind databases into a
// per-test directory and returns its path, or "" when the cache is absent.
//
// Absent is a supported state, not a failure: the container then does what it
// always did and downloads them. That path is slow and depends on a third-party
// CDN's throughput on the day, which is exactly what this seeding exists to
// keep out of the gate — so say so in the log rather than let a later deadline
// be read as a netbird regression.
func seedGeolocationDatadir(t *testing.T) string {
	t.Helper()
	return seedGeolocationDatadirFrom(t, geolocationFixtureDir)
}

// seedGeolocationDatadirFrom is seedGeolocationDatadir against an explicit
// source directory, so the behavior is testable without the real fixture.
func seedGeolocationDatadirFrom(t *testing.T, srcDir string) string {
	t.Helper()

	mmdb, _ := filepath.Glob(filepath.Join(srcDir, "GeoLite2-City_*.mmdb"))
	geonames, _ := filepath.Glob(filepath.Join(srcDir, "geonames_*.db"))
	if len(mmdb) == 0 || len(geonames) == 0 {
		t.Logf("geolocation fixture absent under %s (mmdb=%d geonames=%d) — "+
			"netbird-mgmt will download ~82 MB from pkgs.netbird.io during startup. "+
			"See %s/README.md to seed it; a readiness timeout from here is the CDN, not netbird.",
			srcDir, len(mmdb), len(geonames), srcDir)
		return ""
	}

	// Newest match wins, matching netbird's own choice (getDatabaseFilename
	// takes files[len(files)-1] from a sorted glob).
	dir := t.TempDir()
	for _, src := range []string{mmdb[len(mmdb)-1], geonames[len(geonames)-1]} {
		dst := filepath.Join(dir, filepath.Base(src))
		if err := copyFileTo(src, dst); err != nil {
			// Seeding is an optimization; a broken cache must not be a test
			// failure. Fall back to the download path.
			t.Logf("geolocation fixture unusable (%v) — falling back to download", err)
			return ""
		}
	}
	// The container runs as its own user and prunes stale databases in here.
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Logf("chmod geolocation datadir: %v — falling back to download", err)
		return ""
	}
	t.Logf("seeded geolocation datadir from %s (%s, %s)", srcDir,
		filepath.Base(mmdb[len(mmdb)-1]), filepath.Base(geonames[len(geonames)-1]))
	return dir
}

func copyFileTo(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func TestSeedGeolocationDatadir(t *testing.T) {
	write := func(t *testing.T, dir, name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	t.Run("absent cache falls back to the download path", func(t *testing.T) {
		if got := seedGeolocationDatadirFrom(t, t.TempDir()); got != "" {
			t.Fatalf("empty fixture dir: want \"\", got %q", got)
		}
	})

	t.Run("half a cache is not a cache", func(t *testing.T) {
		// A truncated cache must not seed a Datadir with one database in it:
		// netbird would then download only the missing half, which is the
		// slow path wearing the fast path's clothes.
		src := t.TempDir()
		write(t, src, "GeoLite2-City_20260602.mmdb", "mmdb")
		if got := seedGeolocationDatadirFrom(t, src); got != "" {
			t.Fatalf("mmdb-only fixture: want \"\", got %q", got)
		}
	})

	t.Run("both databases are copied, newest wins", func(t *testing.T) {
		src := t.TempDir()
		write(t, src, "GeoLite2-City_20260101.mmdb", "old")
		write(t, src, "GeoLite2-City_20260602.mmdb", "new")
		write(t, src, "geonames_20260602.db", "geonames")

		dir := seedGeolocationDatadirFrom(t, src)
		if dir == "" {
			t.Fatal("complete fixture: want a seeded dir, got \"\"")
		}

		// Newest wins, matching netbird's own files[len(files)-1] choice, and
		// the superseded database must NOT be seeded — netbird prunes
		// non-matching databases and we would be handing it work to do.
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read seeded dir: %v", err)
		}
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		if len(names) != 2 {
			t.Fatalf("want exactly 2 seeded files, got %v", names)
		}

		body, err := os.ReadFile(filepath.Join(dir, "GeoLite2-City_20260602.mmdb"))
		if err != nil {
			t.Fatalf("read seeded mmdb: %v", err)
		}
		if string(body) != "new" {
			t.Fatalf("seeded the wrong mmdb: content %q", body)
		}
		if _, err := os.Stat(filepath.Join(dir, "geonames_20260602.db")); err != nil {
			t.Fatalf("geonames not seeded: %v", err)
		}

		// The container writes here as its own user and prunes stale
		// databases; a read-only Datadir would fail that at runtime.
		fi, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("stat seeded dir: %v", err)
		}
		if fi.Mode().Perm() != 0o777 {
			t.Fatalf("seeded dir perm = %o, want 777", fi.Mode().Perm())
		}
	})

	t.Run("the real fixture dir honors the same contract", func(t *testing.T) {
		// Calls the entry point the integration suite uses, against whatever
		// testdata/geolite/ actually holds. Asserted as an invariant rather
		// than an expected value, because both states are legitimate: CI seeds
		// the directory from a cache, a fresh clone leaves it empty.
		dir := seedGeolocationDatadir(t)
		if dir == "" {
			return // unseeded checkout — the documented fallback
		}
		for _, pattern := range []string{"GeoLite2-City_*.mmdb", "geonames_*.db"} {
			hits, err := filepath.Glob(filepath.Join(dir, pattern))
			if err != nil {
				t.Fatalf("glob %s: %v", pattern, err)
			}
			if len(hits) != 1 {
				t.Fatalf("seeded dir holds %d files matching %s, want exactly 1", len(hits), pattern)
			}
		}
	})

	t.Run("seeding never mutates the cache it read", func(t *testing.T) {
		src := t.TempDir()
		write(t, src, "GeoLite2-City_20260602.mmdb", "mmdb")
		write(t, src, "geonames_20260602.db", "geonames")

		if dir := seedGeolocationDatadirFrom(t, src); dir == "" {
			t.Fatal("want a seeded dir")
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			t.Fatalf("read source dir: %v", err)
		}
		if len(entries) != 2 {
			t.Fatalf("source dir changed: %d entries", len(entries))
		}
	})
}
