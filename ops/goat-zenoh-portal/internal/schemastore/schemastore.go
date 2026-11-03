// Package schemastore is the v1 portal-local filesystem store for
// uploaded schema bodies (Block 85; design memo at
// docs/design/portal-schema-publishing.md).
//
// Layout under the configured root directory:
//
//	<root>/<topic_id>/<version>.<ext>
//
// Per-topic subdirectory keyed by server-assigned numeric topic_id —
// sidesteps path-safety issues with vendor-supplied strings. The
// associated db row in topic_schemas carries the matching topic_id
// + version + format + sha256 + size_bytes.
//
// Long-term (per the design memo's "Long-term evolution" §) the bytes
// migrate to Zenoh fabric storage at attest/schema/<vendor>/... and this
// package becomes a one-time replay source. v1 keeps it simple.
package schemastore

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
)

// MaxFileSize is the per-upload byte cap. 1 MiB is comfortably above
// typical protobuf FileDescriptorSet sizes (tens of KiB to ~200 KiB even
// for large schemas) and well below anything we'd want to keep in the
// portal's local store. Bigger schemas suggest a misuse.
const MaxFileSize int64 = 1 << 20 // 1 MiB

// ErrTooLarge is returned when an upload exceeds MaxFileSize.
var ErrTooLarge = fmt.Errorf("schema body exceeds %d bytes", MaxFileSize)

// Store wraps the on-disk schema directory.
type Store struct {
	root string
}

// New constructs a Store rooted at the given directory. The directory is
// created (with parents) if missing; mode 0750 so the portal user owns it
// and group can read.
func New(root string) (*Store, error) {
	if root == "" {
		return nil, errors.New("schemastore root path is empty")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("schemastore: resolve abs path: %w", err)
	}
	if err := os.MkdirAll(abs, 0o750); err != nil {
		return nil, fmt.Errorf("schemastore: mkdir %q: %w", abs, err)
	}
	return &Store{root: abs}, nil
}

// Root returns the absolute directory the store is rooted at.
func (s *Store) Root() string { return s.root }

// relPath returns the on-disk relative path for a (topic_id, version, ext)
// triple. Keeps stringly-typed callers from constructing the path by hand.
func relPath(topicID int64, version, ext string) string {
	return filepath.Join(strconv.FormatInt(topicID, 10), version+"."+ext)
}

// extForFormat returns the file extension matching the schema format.
// Mirrors the ValidSchemaFormat enum in registry/schemas.go.
func extForFormat(format string) (string, error) {
	switch format {
	case "protobuf-fds":
		return "fds", nil
	case "xsd":
		return "xsd", nil
	default:
		return "", fmt.Errorf("schemastore: unknown format %q", format)
	}
}

// Write reads body up to MaxFileSize+1 bytes, persists it under the
// per-topic subdirectory, and returns the relative path + sha256 + size.
// Caller passes the relative path back to InsertSchema. The file is
// written via an atomic create+rename so partial writes are never visible.
//
// On collision (file already exists), Write fails — the caller should
// first check the registry for a (topic_id, version) row; the database
// UNIQUE constraint is the authoritative dedup gate.
func (s *Store) Write(topicID int64, version, format string, body io.Reader) (relPathStr, sha256Hex string, size int64, err error) {
	ext, err := extForFormat(format)
	if err != nil {
		return "", "", 0, err
	}
	rel := relPath(topicID, version, ext)
	abs := filepath.Join(s.root, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
		return "", "", 0, fmt.Errorf("schemastore: mkdir parent: %w", err)
	}

	// Tee the body through a sha256 hasher while we count + cap bytes.
	limited := &io.LimitedReader{R: body, N: MaxFileSize + 1}
	hasher := sha256.New()
	mr := io.TeeReader(limited, hasher)

	tmp, err := os.CreateTemp(filepath.Dir(abs), filepath.Base(abs)+".tmp-*")
	if err != nil {
		return "", "", 0, fmt.Errorf("schemastore: tempfile: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(tmpName)
		}
	}()

	n, err := io.Copy(tmp, mr)
	if err != nil {
		_ = tmp.Close()
		return "", "", 0, fmt.Errorf("schemastore: copy: %w", err)
	}
	if n > MaxFileSize {
		_ = tmp.Close()
		return "", "", 0, ErrTooLarge
	}
	if err := tmp.Chmod(0o640); err != nil {
		_ = tmp.Close()
		return "", "", 0, fmt.Errorf("schemastore: chmod: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", "", 0, fmt.Errorf("schemastore: close tempfile: %w", err)
	}
	if err := os.Rename(tmpName, abs); err != nil {
		return "", "", 0, fmt.Errorf("schemastore: rename: %w", err)
	}
	return rel, hex.EncodeToString(hasher.Sum(nil)), n, nil
}

// RemoveTopicDir best-effort removes the per-topic subdirectory and every
// schema file under it. Used by the topic-purge path on hard-delete, after
// the registry row + FK-CASCADEd topic_schemas rows are gone. Idempotent:
// returns nil if the directory is already absent.
func (s *Store) RemoveTopicDir(topicID int64) error {
	dir := filepath.Join(s.root, strconv.FormatInt(topicID, 10))
	return os.RemoveAll(dir)
}

// Open returns a reader for the given relative path. Caller must Close.
// The relative path is taken verbatim from the topic_schemas row's
// file_path column; the safety boundary is that field is only written by
// Write above (no user-supplied path strings).
func (s *Store) Open(rel string) (io.ReadCloser, error) {
	// Defense in depth: refuse path-traversal even though file_path is
	// never user-supplied at upload time.
	cleaned := filepath.Clean(rel)
	if filepath.IsAbs(cleaned) || cleaned != rel || cleaned == ".." || hasParentSegment(cleaned) {
		return nil, fmt.Errorf("schemastore: unsafe relative path %q", rel)
	}
	return os.Open(filepath.Join(s.root, cleaned))
}

func hasParentSegment(p string) bool {
	for _, seg := range filepath.SplitList(p) {
		if seg == ".." {
			return true
		}
	}
	// Also catch ".." anywhere in the path.
	for {
		dir, last := filepath.Split(p)
		if last == ".." {
			return true
		}
		if dir == "" || dir == p {
			return false
		}
		p = filepath.Clean(dir)
		if p == "/" || p == "." {
			return false
		}
	}
}
