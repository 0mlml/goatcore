package opkeys

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeActive struct{ active, known []string }

func (f fakeActive) ActiveHandles(context.Context, string) ([]string, error) {
	return f.active, nil
}

func (f fakeActive) KnownHandles(context.Context, string) ([]string, error) {
	return f.known, nil
}

// The file IS the grant. Its contents must be exactly what the portal parses,
// because a file the portal cannot read is a key that silently does not work —
// which presents to the person as "it was approved and the Portal still refuses
// me", the least debuggable failure in the whole flow.
func TestWrite_producesALineThePortalParses(t *testing.T) {
	d := Dir{Path: t.TempDir()}
	if err := d.Write("ada", "op_deadbeef"); err != nil {
		t.Fatalf("write: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(d.Path, "ada.key"))
	if err != nil {
		t.Fatal(err)
	}
	var pair string
	for _, line := range strings.Split(string(body), "\n") {
		if line != "" && !strings.HasPrefix(line, "#") {
			pair = line
		}
	}
	if pair != "ada:op_deadbeef" {
		t.Errorf("payload line = %q, want %q", pair, "ada:op_deadbeef")
	}
	if !strings.Contains(string(body), "managed by goat-id-console") {
		t.Error("the file must say what manages it, or somebody will hand-edit it")
	}
}

// Revoking is a delete, and the desired end state of deleting something that is
// already gone has been reached. Treating that as an error would make a retried
// revoke look like a failure.
func TestRemove_isIdempotent(t *testing.T) {
	d := Dir{Path: t.TempDir()}
	if err := d.Write("ada", "op_x"); err != nil {
		t.Fatal(err)
	}
	if err := d.Remove("ada"); err != nil {
		t.Fatalf("first remove: %v", err)
	}
	if err := d.Remove("ada"); err != nil {
		t.Errorf("removing an absent key must succeed, got %v", err)
	}
}

// A handle reaches this code from a session. Building a path from any external
// string without neutralizing separators is one traversal away from writing
// outside the directory.
func TestWrite_handleCannotEscapeTheDirectory(t *testing.T) {
	root := t.TempDir()
	d := Dir{Path: filepath.Join(root, "keys")}
	if err := d.Write("../../etc/evil", "op_x"); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "etc")); !os.IsNotExist(err) {
		t.Fatal("a handle escaped the key directory")
	}
	entries, err := os.ReadDir(d.Path)
	if err != nil || len(entries) != 1 {
		t.Fatalf("expected exactly one contained file, got %v (err %v)", entries, err)
	}
}

// Reconcile splits three ways because the three cases call for different
// actions, and the version that did not caused a real near-miss: run against a
// live goatnet it classified the onboard-runner's key and two pre-registry
// human keys as "unexplained grants" and offered to delete them.
//
//	ada    — active row + file      → agrees
//	ghost  — row we revoked + file  → STALE, the record says access was removed
//	runner — file, no row at all    → not ours, report only
//	bob    — active row, no file    → holds a key the portal refuses
func TestReconcile_deletesOnlyWhatThisConsoleIssued(t *testing.T) {
	d := Dir{Path: t.TempDir()}
	for _, h := range []string{"ada", "ghost", "runner"} {
		if err := d.Write(h, "op_"+h); err != nil {
			t.Fatal(err)
		}
	}
	store := fakeActive{
		active: []string{"ada", "bob"},
		known:  []string{"ada", "bob", "ghost"}, // runner was never requested here
	}

	res, err := d.Reconcile(context.Background(), store, "zagros", false)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if strings.Join(res.Stale, ",") != "ghost" {
		t.Errorf("stale = %v, want [ghost]", res.Stale)
	}
	if strings.Join(res.Unmanaged, ",") != "runner" {
		t.Errorf("unmanaged = %v, want [runner]", res.Unmanaged)
	}
	if strings.Join(res.Missing, ",") != "bob" {
		t.Errorf("missing = %v, want [bob]", res.Missing)
	}
	// Dry run must not have touched anything.
	if _, err := os.Stat(filepath.Join(d.Path, "ghost.key")); err != nil {
		t.Error("a dry-run reconcile removed a file")
	}

	if _, err := d.Reconcile(context.Background(), store, "zagros", true); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if _, err := os.Stat(filepath.Join(d.Path, "ghost.key")); !os.IsNotExist(err) {
		t.Error("apply must remove a grant this console revoked")
	}
	// THE POINT: a key this console never issued survives --apply. Deleting it
	// would break a service whose own deploy wrote it.
	if _, err := os.Stat(filepath.Join(d.Path, "runner.key")); err != nil {
		t.Error("apply deleted a key this console does not own")
	}
	if _, err := os.Stat(filepath.Join(d.Path, "ada.key")); err != nil {
		t.Error("apply must leave an active grant alone")
	}
}

// A goatnet that has not configured the directory must degrade to the old
// behavior rather than failing every approval.
func TestUnmanagedDirIsANoOp(t *testing.T) {
	var d Dir
	if d.Managed() {
		t.Fatal("empty path must not report as managed")
	}
	if err := d.Write("ada", "op_x"); err != nil {
		t.Errorf("write on unmanaged dir: %v", err)
	}
	if err := d.Remove("ada"); err != nil {
		t.Errorf("remove on unmanaged dir: %v", err)
	}
	if h, err := d.Handles(); err != nil || h != nil {
		t.Errorf("handles on unmanaged dir = %v, %v", h, err)
	}
}
