package api

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/pubsubstate"
)

// handleAdminSnapshot serializes the current portal state to a tarball
// stream (state-of-record substrate, Block 86). Operator-only; the
// caller decides where to ship the bytes (MinIO bucket, per-site
// Forgejo repo, both).
//
// Returns Content-Type: application/gzip with the rendered state under
// the canonical pubsub-state/ root. Standard tar+gzip; extract with
// `tar xzf -`.
//
// Headers carry summary metadata so the caller can log / metric without
// re-parsing the tarball:
//
//	X-Pubsub-Content-Sha256: <hex>
//	X-Pubsub-Vendor-Count:   <n>
//	X-Pubsub-Topic-Count:    <n>
//	X-Pubsub-Schema-Count:   <n>
//	X-Pubsub-Example-Count:  <n>
func (s *Server) handleAdminSnapshot(w http.ResponseWriter, r *http.Request) {
	if s.schemas == nil {
		writeError(w, http.StatusServiceUnavailable, "no_schemastore", "schemastore not configured; snapshot would be incomplete")
		return
	}
	_ = s.reg.LogAuditEvent(r.Context(), actorOf(r), "admin.snapshot.read", "", "")
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	state, err := pubsubstate.Snapshot(ctx, s.reg, s.schemas)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "snapshot", err.Error())
		return
	}

	// Stage the tree under a temp dir, hash it, then stream as
	// tar+gzip. Staging keeps the tarball layout identical to what the
	// reconciler will read back from a Forgejo clone, so the byte hash
	// is comparable across stores.
	tmp, err := os.MkdirTemp("", "pubsub-snapshot-*")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "tmpdir", err.Error())
		return
	}
	defer os.RemoveAll(tmp)

	stateRoot := filepath.Join(tmp, "pubsub-state")
	manifest, err := pubsubstate.WriteRepo(stateRoot, state, "snapshot-endpoint", time.Now().Unix())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "write", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("X-Pubsub-Content-Sha256", manifest.ContentSHA256)
	w.Header().Set("X-Pubsub-Vendor-Count", fmt.Sprintf("%d", manifest.VendorCount))
	w.Header().Set("X-Pubsub-Topic-Count", fmt.Sprintf("%d", manifest.TopicCount))
	w.Header().Set("X-Pubsub-Schema-Count", fmt.Sprintf("%d", manifest.SchemaCount))
	w.Header().Set("X-Pubsub-Example-Count", fmt.Sprintf("%d", manifest.ExampleCount))
	w.Header().Set("Content-Disposition", `attachment; filename="pubsub-state.tar.gz"`)

	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	if err := writeTreeToTar(stateRoot, "pubsub-state", tw); err != nil {
		// Headers already sent; best we can do is log + abort.
		_ = tw.Close()
		_ = gz.Close()
		return
	}
	_ = tw.Close()
	_ = gz.Close()
}

// handleAdminSnapshotMeta is a HEAD-equivalent that returns the summary
// metadata without streaming the tarball. Useful for the auto-commit
// hook to short-circuit when content hasn't changed.
func (s *Server) handleAdminSnapshotMeta(w http.ResponseWriter, r *http.Request) {
	if s.schemas == nil {
		writeError(w, http.StatusServiceUnavailable, "no_schemastore", "schemastore not configured")
		return
	}
	_ = s.reg.LogAuditEvent(r.Context(), actorOf(r), "admin.snapshot.read", "meta", "")
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	state, err := pubsubstate.Snapshot(ctx, s.reg, s.schemas)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "snapshot", err.Error())
		return
	}
	tmp, err := os.MkdirTemp("", "pubsub-snapshot-meta-*")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "tmpdir", err.Error())
		return
	}
	defer os.RemoveAll(tmp)
	stateRoot := filepath.Join(tmp, "pubsub-state")
	manifest, err := pubsubstate.WriteRepo(stateRoot, state, "snapshot-meta", time.Now().Unix())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "write", err.Error())
		return
	}
	out := map[string]any{
		"content_sha256": manifest.ContentSHA256,
		"vendor_count":   manifest.VendorCount,
		"topic_count":    manifest.TopicCount,
		"schema_count":   manifest.SchemaCount,
		"example_count":  manifest.ExampleCount,
		"generated_at":   manifest.GeneratedAt,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func writeTreeToTar(rootDir, prefix string, tw *tar.Writer) error {
	return filepath.WalkDir(rootDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(rootDir, path)
		if err != nil {
			return err
		}
		archPath := prefix
		if rel != "." {
			archPath = filepath.Join(prefix, rel)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = archPath
		if d.IsDir() {
			hdr.Name += "/"
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		_, err = io.Copy(tw, f)
		_ = f.Close()
		return err
	})
}
