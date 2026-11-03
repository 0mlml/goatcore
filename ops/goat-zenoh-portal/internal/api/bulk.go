// Bulk topic import/export per vendor.
//
// Endpoints:
//   GET  /api/vendors/{vendor}/topics/export?format=yaml|json
//   POST /api/vendors/{vendor}/topics/import?dry_run=true
//
// Wire format (YAML; JSON renders the same shape). The anchor key is `org`;
// the deprecated `vendor` key is still read on import and emitted on export
// during the ADR 1052 deprecation window (docs/design/vendor-to-org-machine-rename.md):
//
//   org: homemade-sensors
//   topics:
//     - key_pattern: homemade-sensors/garage/temperature/v1
//       format: json
//       schema_ref: attest/schema/homemade-sensors/garage/temperature/v1
//       description: "Garage thermometer — DHT22 over ESP32"
//       publish_to_storage: false
//     - …
//
// Semantics on import:
//   - Each topic is upserted by (vendor, key_pattern). Matching rows
//     update format / schema_ref / description / publish_to_storage;
//     new rows are created.
//   - Topics in the registry but NOT in the file are left alone by
//     default (additive). Pass `?mode=replace` to retire (soft-delete)
//     the unlisted ones.
//   - `?dry_run=true` returns the computed diff without applying it.
//
// Auth: same scope as topic-CRUD — operator or matching vendor.

package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/registry"
)

// bulkTopic is the wire shape of a single topic row in import/export.
// Field order intentionally matches the order operators want when
// reading a generated file: key first, format next, then metadata.
type bulkTopic struct {
	KeyPattern       string `json:"key_pattern"        yaml:"key_pattern"`
	Format           string `json:"format"             yaml:"format"`
	SchemaRef        string `json:"schema_ref"         yaml:"schema_ref,omitempty"`
	Description      string `json:"description"        yaml:"description,omitempty"`
	PublishToStorage bool   `json:"publish_to_storage" yaml:"publish_to_storage"`
}

type bulkFile struct {
	// Org is the anchor key (ADR 1052 Phase 1, vendor→org machine rename).
	// Dual-emitted on export and dual-read on import alongside the deprecated
	// Vendor key; Org wins when both are present. See
	// docs/design/vendor-to-org-machine-rename.md.
	Org    string      `json:"org,omitempty"    yaml:"org,omitempty"`
	Vendor string      `json:"vendor"           yaml:"vendor"` // deprecated alias of org
	Topics []bulkTopic `json:"topics"           yaml:"topics"`
}

// resolvedOrg returns the file's declared anchor, preferring the new `org` key
// and falling back to the deprecated `vendor` key (ADR 1052 Phase 1 dual-read).
// usedLegacyVendor reports whether the fallback fired, so the caller can record
// continued vendor-surface use for the Phase-4 removal gate.
func (b *bulkFile) resolvedOrg() (name string, usedLegacyVendor bool) {
	if b.Org != "" {
		return b.Org, false
	}
	if b.Vendor != "" {
		return b.Vendor, true
	}
	return "", false
}

type importDiffEntry struct {
	KeyPattern string `json:"key_pattern" yaml:"key_pattern"`
	Action     string `json:"action"      yaml:"action"` // create / update / unchanged / retire
}

type importDiff struct {
	// Org is dual-emitted alongside the deprecated Vendor key (ADR 1052 Phase 1).
	Org     string            `json:"org"      yaml:"org"`
	Vendor  string            `json:"vendor"   yaml:"vendor"` // deprecated alias of org
	DryRun  bool              `json:"dry_run"  yaml:"dry_run"`
	Mode    string            `json:"mode"     yaml:"mode"`
	Entries []importDiffEntry `json:"entries"  yaml:"entries"`
	Summary map[string]int    `json:"summary"  yaml:"summary"`
}

func (s *Server) exportTopics(w http.ResponseWriter, r *http.Request) {
	vendor := r.PathValue("vendor")
	format := strings.ToLower(r.URL.Query().Get("format"))
	if format == "" {
		format = "yaml"
	}

	topics, err := s.reg.ListVendorTopics(r.Context(), vendor)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}

	// Dual-emit: both the new `org` key and the deprecated `vendor` key carry
	// the same anchor during the deprecation window (ADR 1052 Phase 1).
	out := bulkFile{Org: vendor, Vendor: vendor, Topics: make([]bulkTopic, 0, len(topics))}
	for _, t := range topics {
		out.Topics = append(out.Topics, bulkTopic{
			KeyPattern:       t.KeyPattern,
			Format:           t.Format,
			SchemaRef:        t.SchemaRef,
			Description:      t.Description,
			PublishToStorage: t.PublishToStorage,
		})
	}

	switch format {
	case "json":
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-topics.json"`, vendor))
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(out)
	case "yaml", "yml":
		w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-topics.yaml"`, vendor))
		enc := yaml.NewEncoder(w)
		enc.SetIndent(2)
		_ = enc.Encode(out)
		_ = enc.Close()
	default:
		writeError(w, http.StatusBadRequest, "format", "format must be 'yaml' or 'json'")
	}
}

func (s *Server) importTopics(w http.ResponseWriter, r *http.Request) {
	vendor := r.PathValue("vendor")
	dryRun := r.URL.Query().Get("dry_run") == "true"
	mode := strings.ToLower(r.URL.Query().Get("mode"))
	if mode == "" {
		mode = "additive"
	}
	if mode != "additive" && mode != "replace" {
		writeError(w, http.StatusBadRequest, "mode", "mode must be 'additive' or 'replace'")
		return
	}

	// A body past the cap is refused, never cut: a truncated file can still
	// parse, and in replace mode every topic missing from it is retired.
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxJSONBody))
	if err != nil {
		writeDecodeError(w, err)
		return
	}
	parsed, err := parseBulk(r.Header.Get("Content-Type"), body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "parse", err.Error())
		return
	}
	if len(parsed.Topics) > maxImportTopics {
		writeError(w, http.StatusBadRequest, "validate",
			fmt.Sprintf("topics must hold at most %d entries", maxImportTopics))
		return
	}
	// The same caps the single-topic write routes enforce.
	for i, t := range parsed.Topics {
		if err := capped(
			fieldCap{"key_pattern", t.KeyPattern, maxKeyPattern},
			fieldCap{"format", t.Format, maxFormat},
			fieldCap{"schema_ref", t.SchemaRef, maxSchemaRef},
			fieldCap{"description", t.Description, maxDescription},
		); err != nil {
			writeError(w, http.StatusBadRequest, "validate", fmt.Sprintf("topics[%d]: %v", i, err))
			return
		}
	}
	// Dual-read the file's declared anchor: prefer `org`, fall back to the
	// deprecated `vendor` key (ADR 1052 Phase 1). A legacy fallback counts as
	// continued vendor-surface use for the Phase-4 removal gate.
	fileOrg, usedLegacy := parsed.resolvedOrg()
	if usedLegacy {
		s.legacyVendorHits.Add(1)
	}
	if fileOrg != "" && fileOrg != vendor {
		writeError(w, http.StatusBadRequest, "org_mismatch",
			fmt.Sprintf("file org %q does not match URL org %q", fileOrg, vendor))
		return
	}

	// Load current registry state for this vendor.
	current, err := s.reg.ListVendorTopics(r.Context(), vendor)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	currentByKey := map[string]registry.Topic{}
	for _, t := range current {
		currentByKey[t.KeyPattern] = t
	}

	// Compute diff. Dual-emit org + the deprecated vendor key (ADR 1052 Phase 1).
	diff := importDiff{
		Org:     vendor,
		Vendor:  vendor,
		DryRun:  dryRun,
		Mode:    mode,
		Entries: []importDiffEntry{},
		Summary: map[string]int{"create": 0, "update": 0, "unchanged": 0, "retire": 0},
	}
	seenInFile := map[string]bool{}
	for _, ft := range parsed.Topics {
		seenInFile[ft.KeyPattern] = true
		existing, has := currentByKey[ft.KeyPattern]
		switch {
		case !has:
			diff.Entries = append(diff.Entries, importDiffEntry{ft.KeyPattern, "create"})
			diff.Summary["create"]++
		case rowsDiffer(existing, ft):
			diff.Entries = append(diff.Entries, importDiffEntry{ft.KeyPattern, "update"})
			diff.Summary["update"]++
		default:
			diff.Entries = append(diff.Entries, importDiffEntry{ft.KeyPattern, "unchanged"})
			diff.Summary["unchanged"]++
		}
	}
	if mode == "replace" {
		for _, t := range current {
			if !seenInFile[t.KeyPattern] {
				diff.Entries = append(diff.Entries, importDiffEntry{t.KeyPattern, "retire"})
				diff.Summary["retire"]++
			}
		}
	}

	if dryRun {
		writeJSON(w, http.StatusOK, diff)
		return
	}

	// Apply.
	applied := 0
	for _, ft := range parsed.Topics {
		existing, has := currentByKey[ft.KeyPattern]
		if !has {
			t := &registry.Topic{
				VendorPrefix:     vendor,
				KeyPattern:       ft.KeyPattern,
				Format:           ft.Format,
				SchemaRef:        ft.SchemaRef,
				Description:      ft.Description,
				PublishToStorage: ft.PublishToStorage,
			}
			if _, err := s.reg.CreateTopic(r.Context(), t); err != nil {
				if !errors.Is(err, registry.ErrTopicExists) {
					writeRegistryError(w, fmt.Errorf("topic %q: %w", ft.KeyPattern, err))
					return
				}
			}
			applied++
			continue
		}
		if rowsDiffer(existing, ft) {
			existing.Format = ft.Format
			existing.SchemaRef = ft.SchemaRef
			existing.Description = ft.Description
			existing.PublishToStorage = ft.PublishToStorage
			if err := s.reg.UpdateTopic(r.Context(), &existing); err != nil {
				writeRegistryError(w, fmt.Errorf("topic %q: %w", ft.KeyPattern, err))
				return
			}
			applied++
		}
	}
	if mode == "replace" {
		for _, t := range current {
			if !seenInFile[t.KeyPattern] {
				if err := s.reg.RetireTopic(r.Context(), t.ID); err != nil {
					writeError(w, http.StatusInternalServerError, "retire",
						fmt.Sprintf("topic %q: %s", t.KeyPattern, err.Error()))
					return
				}
				applied++
			}
		}
	}
	_ = s.reg.LogAuditEvent(r.Context(), vendor, "topic-bulk-import", vendor,
		fmt.Sprintf(`{"mode":%q,"applied":%d}`, mode, applied))

	writeJSON(w, http.StatusOK, diff)
}

func parseBulk(contentType string, body []byte) (*bulkFile, error) {
	out := &bulkFile{}
	ct := strings.ToLower(contentType)
	switch {
	case strings.Contains(ct, "yaml"):
		if err := yaml.Unmarshal(body, out); err != nil {
			return nil, fmt.Errorf("yaml: %w", err)
		}
	case strings.Contains(ct, "json"):
		if err := json.Unmarshal(body, out); err != nil {
			return nil, fmt.Errorf("json: %w", err)
		}
	default:
		// Content-Type missing or unknown: try YAML first (a superset of
		// JSON for valid documents), fall back to JSON on parse failure.
		if err := yaml.Unmarshal(body, out); err != nil {
			if jerr := json.Unmarshal(body, out); jerr != nil {
				return nil, fmt.Errorf("could not parse body as yaml or json")
			}
		}
	}
	if len(out.Topics) == 0 {
		return nil, fmt.Errorf("topics list is empty")
	}
	return out, nil
}

func rowsDiffer(a registry.Topic, b bulkTopic) bool {
	return a.Format != b.Format ||
		a.SchemaRef != b.SchemaRef ||
		a.Description != b.Description ||
		a.PublishToStorage != b.PublishToStorage
}
