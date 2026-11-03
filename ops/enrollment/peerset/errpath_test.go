package peerset

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// TestLoad_UnreadableFileSurfacesIOError covers the read-error branch
// distinct from os.IsNotExist. On platforms where chmod removes read
// permission, we can deterministically force a non-NotExist read error.
func TestLoad_UnreadableFileSurfacesIOError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file mode; skip")
	}
	if runtime.GOOS == "windows" {
		t.Skip("windows file ACL semantics; skip")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "unreadable.json")
	if err := os.WriteFile(p, []byte(`[]`), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(p, 0o644); err != nil {
			t.Errorf("restore mode: %v", err)
		}
	})
	_, err := Load(p)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// Don't pin to a sentinel — this isn't a schema error, just an I/O error.
}

// TestSave_FailsWhenTargetDirIsAFile covers the MkdirAll error branch
// in atomicWriteFile: a path whose parent is a regular file forces the
// directory-creation step to fail.
func TestSave_FailsWhenTargetDirIsAFile(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(blocker, "sub", "out.json")
	err := emptyV2().Save(target)
	if err == nil {
		t.Fatal("expected save to fail when parent path is a regular file")
	}
}

// TestLoad_BlankDocumentReturnsEmpty exercises the trimmed-empty branch
// in parse so the coverage report doesn't show a stale uncovered line.
func TestLoad_BlankDocumentReturnsEmpty(t *testing.T) {
	p := filepath.Join(t.TempDir(), "blank.json")
	_ = os.WriteFile(p, []byte("\n   \n\t"), 0o644)
	ps, err := Load(p)
	if err != nil {
		t.Fatalf("Load blank: %v", err)
	}
	if ps.SchemaVersion != SchemaVersion {
		t.Fatalf("want empty v2, got %+v", ps)
	}
}

// TestValidate_EmptyRelayName + DeprecatedEmptyName cover the relay
// + deprecated-peer name-emptiness branches in Validate.
func TestValidate_EmptyRelayName(t *testing.T) {
	ps := emptyV2()
	ps.Relays = []Relay{{Name: ""}}
	if err := ps.Validate(); !errors.Is(err, ErrSchemaInvalid) {
		t.Fatalf("err = %v, want ErrSchemaInvalid", err)
	}
}

func TestValidate_DeprecatedEmptyName(t *testing.T) {
	ps := emptyV2()
	ps.DeprecatedPeers = []DeprecatedPeer{{Peer: Peer{}}}
	if err := ps.Validate(); !errors.Is(err, ErrSchemaInvalid) {
		t.Fatalf("err = %v, want ErrSchemaInvalid", err)
	}
}

// TestUpsert_PreservesCreatedAtOnReplace ensures that re-running upsert
// for the same name doesn't reset CreatedAt. Hits the "previousCreatedAt"
// fallback line in Upsert.
func TestUpsert_PreservesCreatedAtOnReplace(t *testing.T) {
	t.Cleanup(fixedClock(time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)))
	ps := emptyV2()
	_ = ps.Upsert(samplePeer("a", "P1", "198.18.0.1"))
	first := ps.Peers[0].Metadata.CreatedAt
	// Re-upsert with the same name; the new Peer has Metadata zero
	// so the preserved-CreatedAt path runs.
	_ = ps.Upsert(samplePeer("a", "P2", "198.18.0.1"))
	if !ps.Peers[0].Metadata.CreatedAt.Equal(first) {
		t.Fatalf("CreatedAt regressed: %v → %v", first, ps.Peers[0].Metadata.CreatedAt)
	}
}
