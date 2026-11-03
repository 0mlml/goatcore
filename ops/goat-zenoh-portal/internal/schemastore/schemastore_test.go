package schemastore

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoreWriteOpenRoundTrip(t *testing.T) {
	root := t.TempDir()
	s, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	payload := []byte("hello descriptor set")
	rel, sha, n, err := s.Write(42, "v1", "protobuf-fds", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != int64(len(payload)) {
		t.Errorf("size %d, want %d", n, len(payload))
	}
	wantSum := sha256.Sum256(payload)
	if sha != hex.EncodeToString(wantSum[:]) {
		t.Errorf("sha = %q, want %q", sha, hex.EncodeToString(wantSum[:]))
	}
	wantRel := filepath.Join("42", "v1.fds")
	if rel != wantRel {
		t.Errorf("rel = %q, want %q", rel, wantRel)
	}

	// Disk content matches.
	rc, err := s.Open(rel)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	got, err := io.ReadAll(rc)
	rc.Close()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("disk bytes != payload")
	}

	// File mode is 0640 (not world-readable).
	info, err := os.Stat(filepath.Join(root, rel))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o640 {
		t.Errorf("mode = %o, want 0640", mode)
	}
}

func TestStoreRejectsOversize(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// MaxFileSize + 1 bytes.
	big := bytes.Repeat([]byte{0xAB}, int(MaxFileSize+1))
	_, _, _, err = s.Write(1, "v1", "protobuf-fds", bytes.NewReader(big))
	if err == nil {
		t.Fatalf("oversize write: expected error, got nil")
	}
}

func TestStoreRejectsUnknownFormat(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, _, _, err = s.Write(1, "v1", "json-schema", bytes.NewReader([]byte("x")))
	if err == nil || !strings.Contains(err.Error(), "unknown format") {
		t.Errorf("unknown format: got %v", err)
	}
}

func TestStoreOpenRefusesUnsafePaths(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	cases := []string{
		"../etc/passwd",
		"42/../../escape",
		"/absolute/path",
	}
	for _, p := range cases {
		if _, err := s.Open(p); err == nil {
			t.Errorf("Open(%q): expected error, got nil", p)
		}
	}
}
