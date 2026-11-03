package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/decoder"
	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/protocompile"
	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/registry"
	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/schemastore"
)

// Block 85 — portal-driven schema publishing.
//
// Three endpoints land here:
//
//   POST   /api/vendors/{vendor}/topics/{id}/schemas
//   GET    /api/vendors/{vendor}/topics/{id}/schemas
//   GET    /api/schemas/{key...}   (catch-all; resolves key_pattern → topic)
//
// The store is a Store (filesystem) injected at Server construction; the
// registry table that pairs with it is topic_schemas.
//
// Design memo: docs/design/portal-schema-publishing.md.

// --- Upload ----------------------------------------------------------

// schemaEntry is the JSON shape returned for any topic_schemas row.
type schemaEntry struct {
	ID            int64  `json:"id"`
	TopicID       int64  `json:"topic_id"`
	Version       string `json:"version"`
	Compatibility string `json:"compatibility"`
	Format        string `json:"format"`
	SizeBytes     int64  `json:"size_bytes"`
	SHA256        string `json:"sha256"`
	UploadedAt    int64  `json:"uploaded_at"`
	UploadedBy    string `json:"uploaded_by"`
	FetchURL      string `json:"fetch_url"`
}

func schemaToEntry(s *registry.TopicSchema, fetchURL string) schemaEntry {
	return schemaEntry{
		ID:            s.ID,
		TopicID:       s.TopicID,
		Version:       s.Version,
		Compatibility: s.Compatibility,
		Format:        s.Format,
		SizeBytes:     s.SizeBytes,
		SHA256:        s.SHA256,
		UploadedAt:    s.UploadedAt,
		UploadedBy:    s.UploadedBy,
		FetchURL:      fetchURL,
	}
}

// uploadSchema handles POST /api/vendors/{vendor}/topics/{id}/schemas.
// Multipart form fields:
//
//	file          — the schema body (.fds or .proto)
//	version       — vendor-supplied version label ^v\d+$
//	compatibility — backward | forward | none
//	format        — protobuf-fds | protobuf-source
//
// When format=protobuf-source, the .proto bytes are compiled in-process
// via internal/protocompile and persisted as a FileDescriptorSet under
// the existing protobuf-fds on-disk layout. Single-file source with
// google/protobuf/* well-known-types imports only; multi-file source
// continues to use the protobuf-fds path. Closes F-158.
//
// Auth: operator-or-matching-vendor (wired via scopedHandler in server.go).
// Body cap: schemastore.MaxFileSize (1 MiB).
func (s *Server) uploadSchema(w http.ResponseWriter, r *http.Request) {
	if s.schemas == nil {
		writeError(w, http.StatusServiceUnavailable, "config", "schema store not configured")
		return
	}
	vendor := r.PathValue("vendor")
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "decode", "id must be integer")
		return
	}

	// Cap the request body well above MaxFileSize to allow for the
	// multipart envelope overhead. The schemastore enforces the real cap
	// per-file.
	r.Body = http.MaxBytesReader(w, r.Body, schemastore.MaxFileSize+(1<<15))
	if err := r.ParseMultipartForm(schemastore.MaxFileSize); err != nil {
		writeError(w, http.StatusBadRequest, "decode", "multipart parse: "+err.Error())
		return
	}

	version := strings.TrimSpace(r.FormValue("version"))
	compatibility := strings.TrimSpace(r.FormValue("compatibility"))
	format := strings.TrimSpace(r.FormValue("format"))
	if format == "" {
		format = "protobuf-fds"
	}
	if compatibility == "" {
		compatibility = "none"
	}
	if err := registry.ValidSchemaVersion(version); err != nil {
		writeError(w, http.StatusBadRequest, "validate", err.Error())
		return
	}
	if err := registry.ValidSchemaFormat(format); err != nil {
		writeError(w, http.StatusBadRequest, "validate", err.Error())
		return
	}
	if err := registry.ValidCompatibility(compatibility); err != nil {
		writeError(w, http.StatusBadRequest, "validate", err.Error())
		return
	}

	file, fileHdr, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "decode", "missing 'file' form field")
		return
	}
	defer file.Close()
	if fileHdr.Size > schemastore.MaxFileSize {
		writeError(w, http.StatusRequestEntityTooLarge, "too_large", schemastore.ErrTooLarge.Error())
		return
	}

	// Read the body into memory (already capped at MaxFileSize above).
	// Keeping it in a buffer lets us compile protobuf-source uploads
	// before handing the resulting bytes to schemastore.Write via a
	// bytes.Reader. Read cap = MaxFileSize+1 so a body that grew past
	// fileHdr.Size between header and stream surfaces as too_large.
	bodyBytes, err := io.ReadAll(io.LimitReader(file, schemastore.MaxFileSize+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "decode", "read body: "+err.Error())
		return
	}
	if int64(len(bodyBytes)) > schemastore.MaxFileSize {
		writeError(w, http.StatusRequestEntityTooLarge, "too_large", schemastore.ErrTooLarge.Error())
		return
	}

	// Compile protobuf-source on ingress; the persisted bytes are
	// always a serialized FileDescriptorSet so the decoder + render
	// endpoints downstream remain identical between the two paths.
	sourceFormat := format
	if format == "protobuf-source" {
		compiled, cerr := protocompile.CompileSingleProto(r.Context(), fileHdr.Filename, bodyBytes)
		if cerr != nil {
			writeError(w, http.StatusBadRequest, "compile", cerr.Error())
			return
		}
		if int64(len(compiled)) > schemastore.MaxFileSize {
			writeError(w, http.StatusRequestEntityTooLarge, "too_large",
				fmt.Sprintf("compiled descriptor set (%d bytes) exceeds %d cap; source was %d bytes",
					len(compiled), schemastore.MaxFileSize, len(bodyBytes)))
			return
		}
		bodyBytes = compiled
		format = "protobuf-fds"
	}

	// Look up the topic + verify it belongs to this vendor.
	t, err := s.reg.GetTopic(r.Context(), id)
	if err != nil {
		if errors.Is(err, registry.ErrNotFound) {
			writeError(w, http.StatusNotFound, "notfound", "topic not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	if t.VendorPrefix != vendor {
		writeError(w, http.StatusNotFound, "notfound", "topic not found under this vendor")
		return
	}

	// Write-time format ↔ schema-body consistency check, per schema-doc
	// kind. Mirrors the read-side schema-body check in
	// crates/stethoscope-checks: a topic that declares a schema-bound
	// format must upload a body that actually declares that symbol.
	// Without this gate a vendor can land a permanently-FAIL'ing schema
	// row that only stethoscope surfaces after the fact (the DRS-RCA
	// shape). protobuf-source has already been compiled to protobuf-fds
	// above, so only two persisted kinds reach here.
	switch format {
	case "protobuf-fds":
		// "protobuf:<Name>" topics must carry that top-level message.
		if verr := decoder.ValidateFormatAgainstFDS(t.Format, bodyBytes); verr != nil {
			var fme *decoder.FormatFDSMismatchError
			if errors.As(verr, &fme) {
				detail := verr.Error()
				if sugg := fme.Suggestion(); sugg != "" {
					detail += fmt.Sprintf(" — most likely correct: PATCH topic format to %q, or rebuild the .fds with %q at top-level.", sugg, fme.DeclaredName)
				}
				writeError(w, http.StatusUnprocessableEntity, "format_mismatch", detail)
				return
			}
			writeError(w, http.StatusBadRequest, "fds_decode", verr.Error())
			return
		}
	case "xsd":
		// The body must be well-formed XSD regardless of topic format
		// (parity with proto.Unmarshal implicitly gating .fds bodies),
		// and an "xml:<Root>" topic must declare that top-level element.
		if verr := decoder.ValidateXSDWellFormed(bodyBytes); verr != nil {
			writeError(w, http.StatusBadRequest, "xsd_decode", verr.Error())
			return
		}
		if verr := decoder.ValidateFormatAgainstXSD(t.Format, bodyBytes); verr != nil {
			var xme *decoder.FormatXSDMismatchError
			if errors.As(verr, &xme) {
				detail := verr.Error()
				if sugg := xme.Suggestion(); sugg != "" {
					detail += fmt.Sprintf(" — most likely correct: PATCH topic format to %q, or add <xs:element name=%q ...> at top-level in the .xsd.", sugg, xme.DeclaredName)
				}
				writeError(w, http.StatusUnprocessableEntity, "format_mismatch", detail)
				return
			}
			writeError(w, http.StatusBadRequest, "xsd_decode", verr.Error())
			return
		}
	}

	// Pre-check for collision so we don't write the file then fail. The
	// db UNIQUE constraint is still the authoritative dedup gate.
	if existing, err := s.reg.GetSchemaByTopicVersion(r.Context(), id, version); err == nil && existing != nil {
		writeError(w, http.StatusConflict, "exists", "schema with this version already exists for this topic")
		return
	} else if err != nil && !errors.Is(err, registry.ErrNotFound) {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}

	// Persist bytes to disk.
	relPath, sha, n, err := s.schemas.Write(id, version, format, bytes.NewReader(bodyBytes))
	if err != nil {
		if errors.Is(err, schemastore.ErrTooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "too_large", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "store", err.Error())
		return
	}

	// Identify uploader for audit trail.
	uploadedBy := vendor
	if p := PrincipalFromContext(r.Context()); p != nil && p.Operator {
		uploadedBy = "operator"
	}

	row := &registry.TopicSchema{
		TopicID:       id,
		Version:       version,
		Compatibility: compatibility,
		Format:        format,
		FilePath:      relPath,
		SizeBytes:     n,
		SHA256:        sha,
		UploadedBy:    uploadedBy,
	}
	if _, err := s.reg.InsertSchema(r.Context(), row); err != nil {
		// Best-effort cleanup of the orphaned file. Subsequent re-upload
		// with the same version will collide on disk too; that's caught
		// by the schemastore Write rename, which surfaces an error the
		// caller resolves by removing the row.
		_ = removeStoreFile(s.schemas, relPath)
		if errors.Is(err, registry.ErrSchemaExists) {
			writeError(w, http.StatusConflict, "exists", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}

	// Bump topic's schema_ref pointer to the canonical attest/schema/... URI
	// when it's blank — surfaces the published schema in the existing
	// pointer field so the UI's "Schema" column lights up without a UI change.
	if t.SchemaRef == "" {
		t.SchemaRef = "attest/schema/" + t.KeyPattern + "/" + version
		_ = s.reg.UpdateTopic(r.Context(), t)
	}

	detailsJSON, _ := json.Marshal(map[string]any{
		"topic_id":      id,
		"version":       version,
		"format":        format,
		"source_format": sourceFormat,
		"compatibility": compatibility,
		"size_bytes":    n,
		"sha256":        sha,
	})
	_ = s.reg.LogAuditEvent(r.Context(), uploadedBy, "schema-upload", t.KeyPattern, string(detailsJSON))

	writeJSON(w, http.StatusCreated, schemaToEntry(row, schemaFetchURL(t.KeyPattern, version)))
}

// listSchemas returns every schema row for a topic.
// GET /api/vendors/{vendor}/topics/{id}/schemas.
func (s *Server) listSchemas(w http.ResponseWriter, r *http.Request) {
	vendor := r.PathValue("vendor")
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "decode", "id must be integer")
		return
	}
	t, err := s.reg.GetTopic(r.Context(), id)
	if err != nil || t.VendorPrefix != vendor {
		writeError(w, http.StatusNotFound, "notfound", "topic not found under this vendor")
		return
	}
	rows, err := s.reg.ListSchemasByTopic(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	out := make([]schemaEntry, 0, len(rows))
	for i := range rows {
		out = append(out, schemaToEntry(&rows[i], schemaFetchURL(t.KeyPattern, rows[i].Version)))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"topic_id":    id,
		"key_pattern": t.KeyPattern,
		"schemas":     out,
	})
}

// fetchSchemaByKey is the catch-all consumer-facing fetch endpoint:
//
//	GET /api/schemas/<vendor>/<system>/.../<topic-version>?version=v2
//
// Without ?version=, returns the latest uploaded schema. The captured
// path IS the topic's key_pattern; the optional `version` query param
// selects a specific schema version. The first path segment is the
// vendor prefix (per the existing key_pattern convention enforced by
// validateTopic).
//
// Auth: same Bearer-token gate as the rest of /api/. Any authenticated
// principal can fetch any schema — this is a read-only browseability
// endpoint, not a per-vendor confidentiality boundary.
func (s *Server) fetchSchemaByKey(w http.ResponseWriter, r *http.Request) {
	if s.schemas == nil {
		writeError(w, http.StatusServiceUnavailable, "config", "schema store not configured")
		return
	}
	key := r.PathValue("key")
	if key == "" {
		writeError(w, http.StatusBadRequest, "decode", "missing key path")
		return
	}
	// The captured wildcard contains the full key_pattern verbatim. The
	// vendor prefix is the first segment.
	parts := strings.SplitN(key, "/", 2)
	if len(parts) < 2 {
		writeError(w, http.StatusBadRequest, "decode", "key must be <vendor>/<rest...>")
		return
	}
	vendor := parts[0]
	if err := registry.ValidVendorPrefix(vendor); err != nil {
		writeError(w, http.StatusBadRequest, "validate", err.Error())
		return
	}

	t, err := s.reg.GetTopicByVendorKey(r.Context(), vendor, key)
	if err != nil {
		if errors.Is(err, registry.ErrNotFound) {
			writeError(w, http.StatusNotFound, "notfound", "topic not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}

	var row *registry.TopicSchema
	version := r.URL.Query().Get("version")
	if version == "" {
		row, err = s.reg.GetLatestSchemaForTopic(r.Context(), t.ID)
	} else {
		if verr := registry.ValidSchemaVersion(version); verr != nil {
			writeError(w, http.StatusBadRequest, "validate", verr.Error())
			return
		}
		row, err = s.reg.GetSchemaByTopicVersion(r.Context(), t.ID, version)
	}
	if err != nil {
		if errors.Is(err, registry.ErrNotFound) {
			writeError(w, http.StatusNotFound, "notfound", "no schema uploaded for this topic"+optVersionMsg(version))
			return
		}
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}

	rc, err := s.schemas.Open(row.FilePath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store", err.Error())
		return
	}
	defer rc.Close()

	w.Header().Set("Content-Type", contentTypeForFormat(row.Format))
	// A partner-uploaded body is a download, never a document of this origin.
	w.Header().Set("Content-Disposition", attachmentFor(t.KeyPattern, row.Version, row.Format))
	w.Header().Set("X-Schema-Version", row.Version)
	w.Header().Set("X-Schema-Compatibility", row.Compatibility)
	w.Header().Set("X-Schema-SHA256", row.SHA256)
	w.Header().Set("X-Schema-Format", row.Format)
	w.Header().Set("Cache-Control", cacheRevalidate)
	// The stored digest is the validator, so a client that already holds this
	// version gets 304 on If-None-Match (and on If-Modified-Since, from the
	// upload time) instead of the bytes again.
	w.Header().Set("ETag", `"`+row.SHA256+`"`)
	uploaded := time.Unix(row.UploadedAt, 0).UTC()
	if seeker, ok := rc.(io.ReadSeeker); ok {
		http.ServeContent(w, r, "", uploaded, seeker)
		return
	}
	w.Header().Set("Content-Length", strconv.FormatInt(row.SizeBytes, 10))
	w.Header().Set("Last-Modified", uploaded.Format(http.TimeFormat))
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, rc)
}

// --- helpers ---------------------------------------------------------

func schemaFetchURL(keyPattern, version string) string {
	return "/api/schemas/" + keyPattern + "?version=" + version
}

func optVersionMsg(version string) string {
	if version == "" {
		return ""
	}
	return " (version " + version + ")"
}

// attachmentFor names the download after the topic key and schema version,
// with every character outside [A-Za-z0-9._-] replaced so a key pattern's
// slashes and wildcards cannot shape the filename.
func attachmentFor(keyPattern, version, format string) string {
	clean := func(s string) string {
		return strings.Map(func(r rune) rune {
			switch {
			case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
				return r
			}
			return '_'
		}, s)
	}
	ext := "bin"
	switch format {
	case "protobuf-fds":
		ext = "fds"
	case "xsd":
		ext = "xsd"
	}
	name := clean(keyPattern) + "-" + clean(version) + "." + ext
	return mime.FormatMediaType("attachment", map[string]string{"filename": name})
}

func contentTypeForFormat(format string) string {
	switch format {
	case "protobuf-fds":
		return "application/x-protobuf-descriptor-set"
	default:
		return "application/octet-stream"
	}
}

// removeStoreFile is a best-effort cleanup helper. Soaks the dependency
// on the concrete store type so callers stay simple.
func removeStoreFile(st *schemastore.Store, rel string) error {
	rc, err := st.Open(rel)
	if err != nil {
		return err
	}
	_ = rc.Close()
	// schemastore doesn't expose Remove today — rare orphan case (db
	// insert fails after disk write succeeded) is acceptable as a leak
	// for v1; ops can sweep the dir periodically.
	return nil
}
