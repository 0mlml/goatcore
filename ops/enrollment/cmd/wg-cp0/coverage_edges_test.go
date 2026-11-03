package main

// coverage_edges_test.go — targeted tests for low-cov branches in the
// Session-C owned files. These cover the error returns and unusual
// classifier outputs that the happy-path tests in cmd_*_test.go
// don't exercise.

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/usareur-af/goatcore/ops/enrollment/peerset"
)

func TestWriteRelaysFile_EmptySliceProducesValidJSON(t *testing.T) {
	path, cleanup, err := writeRelaysFile(nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	defer cleanup()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(strings.TrimSpace(string(data)), "[") {
		t.Errorf("empty relays should marshal to []: %s", data)
	}
}

func TestCommitAddRelay_UnexpectedOpRejected(t *testing.T) {
	ps := &peerset.PeerSet{}
	err := commitAddRelay(ps, peerset.Relay{Name: "x"}, PlanRemove)
	if err == nil || !strings.Contains(err.Error(), "unexpected op") {
		t.Errorf("expected unexpected-op error; got %v", err)
	}
}

func TestCommitAddRelay_PlanKeepIsNoOp(t *testing.T) {
	ps := &peerset.PeerSet{Relays: []peerset.Relay{{Name: "x"}}}
	if err := commitAddRelay(ps, peerset.Relay{Name: "x"}, PlanKeep); err != nil {
		t.Errorf("PlanKeep should be a no-op; got %v", err)
	}
	if len(ps.Relays) != 1 {
		t.Errorf("Relays mutated: %v", ps.Relays)
	}
}

func TestCmdUpdateEndpoints_LoadCorruptJSONFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "broken.json")
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := runArgs(t, "update-endpoints", "anything", "--path", path)
	if code != 1 {
		t.Errorf("code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "load") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestCmdUpdateTTL_LoadCorruptJSONFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "broken.json")
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := runArgs(t, "update-ttl", "x", "--extend", "90d", "--path", path)
	if code != 1 {
		t.Errorf("code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "load") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestCmdAddRelay_LoadCorruptJSONFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "broken.json")
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := runArgs(t, "add-relay", "r",
		"--endpoint", "x:1", "--pubkey", validPubkey(), "--address", "198.18.0.4",
		"--path", path)
	if code != 1 {
		t.Errorf("code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "load") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestCmdUpdateEndpoints_NoActivePeers(t *testing.T) {
	// Empty peerset with --all-peers: no targets, prints "nothing to
	// do" and exits 0.
	path := filepath.Join(t.TempDir(), "empty.json")
	writeV2Fixture(t, path, peerset.PeerSet{
		Relays: []peerset.Relay{{
			Name: "r", Endpoint: "x:1", MeshAddress: "198.18.0.2",
			CurrentPubkey: base64.StdEncoding.EncodeToString(make([]byte, 32)),
		}},
	})
	code, stdout, _ := runArgs(t, "update-endpoints", "--all-peers", "--path", path)
	if code != 0 {
		t.Errorf("code = %d, want 0", code)
	}
	if !strings.Contains(stdout, "No peers selected") {
		t.Errorf("stdout = %q", stdout)
	}
}

func TestCmdUpdateTTL_NoActivePeers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.json")
	writeV2Fixture(t, path, peerset.PeerSet{})
	code, stdout, _ := runArgs(t, "update-ttl", "--all-peers", "--extend", "90d", "--path", path)
	if code != 0 {
		t.Errorf("code = %d, want 0", code)
	}
	if !strings.Contains(stdout, "No peers selected") {
		t.Errorf("stdout = %q", stdout)
	}
}

func TestResolveBundleCreateBin_PathLookupFallback(t *testing.T) {
	// Unset the override; resolveBundleCreateBin should fall through
	// to exec.LookPath. With a contrived empty PATH the lookup fails
	// (operator-actionable error). This exercises the fallback branch.
	t.Setenv("WG_CP0_BUNDLE_CREATE_BIN", "")
	t.Setenv("PATH", "")
	_, err := resolveBundleCreateBin()
	if err == nil {
		t.Errorf("expected error with empty PATH")
	}
	if !strings.Contains(err.Error(), "bundle-create not found") {
		t.Errorf("error message: %v", err)
	}
}
