package api

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/decoder"
	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/registry"
)

// displayNameForLanguage maps a stored language tag to a friendly tab
// label for the schema viewer. Languages with an autogen template
// already carry their display name; this helper only fires for
// vendor-uploaded rows in languages the autogen pass doesn't cover
// (e.g. C / C++ / Java / shell).
func displayNameForLanguage(lang string) string {
	switch lang {
	case "rust":
		return "Rust"
	case "go":
		return "Go"
	case "python":
		return "Python"
	case "typescript":
		return "TypeScript"
	case "javascript":
		return "JavaScript"
	case "c":
		return "C"
	case "cpp":
		return "C++"
	case "java":
		return "Java"
	case "shell":
		return "Shell"
	default:
		return lang
	}
}

// renderSchemaByKey serves the structured "Swagger for protobuf" view
// of a topic's uploaded schema body.
//
//	GET /api/schemas/render/{key...}[?version=v2]
//
// Returns a flat JSON tree of messages + enums parsed from the .fds.
// The catch-all path captures the topic's full key_pattern verbatim;
// the optional `version` query selects a specific schema version
// (default = latest).
//
// Same auth surface as the fetch endpoint — any authenticated principal
// reads it, this is a browseability endpoint.
func (s *Server) renderSchemaByKey(w http.ResponseWriter, r *http.Request) {
	if s.schemas == nil {
		writeError(w, http.StatusServiceUnavailable, "config", "schema store not configured")
		return
	}
	key := r.PathValue("key")
	if key == "" {
		writeError(w, http.StatusBadRequest, "decode", "missing key path")
		return
	}
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

	topic, err := s.reg.GetTopicByVendorKey(r.Context(), vendor, key)
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
		row, err = s.reg.GetLatestSchemaForTopic(r.Context(), topic.ID)
	} else {
		if verr := registry.ValidSchemaVersion(version); verr != nil {
			writeError(w, http.StatusBadRequest, "validate", verr.Error())
			return
		}
		row, err = s.reg.GetSchemaByTopicVersion(r.Context(), topic.ID, version)
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
	descriptorBytes, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store", err.Error())
		return
	}

	// Look up sibling versions so the UI can offer a version picker
	// without a second request.
	allRows, _ := s.reg.ListSchemasByTopic(r.Context(), topic.ID)
	versions := make([]string, 0, len(allRows))
	for _, ar := range allRows {
		versions = append(versions, ar.Version)
	}

	// Render the data dictionary + reconstruct source + build code
	// examples, dispatched by the persisted schema-body kind. Both the
	// protobuf and XSD paths emit the same RenderedSchema shape + the
	// same CodeExample list, so the viewer renders them identically;
	// source_lang tells the frontend how to label + highlight the
	// source tab.
	var (
		rendered   *decoder.RenderedSchema
		source     map[string]string
		sourceLang = "proto"
		primary    string
		examples   []decoder.CodeExample
	)
	if row.Format == "xsd" {
		sourceLang = "xsd"
		rendered, err = decoder.RenderXSD(descriptorBytes)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, "render", err.Error())
			return
		}
		source = map[string]string{"schema.xsd": string(descriptorBytes)}
		// The topic's xml:<Root> names the element the viewer lands on.
		if _, root := decoder.ParseXMLFormat(topic.Format); root != "" {
			primary = root
		}
		if primary != "" {
			p := decoder.XSDExampleParams{TopicKey: key, RootElement: primary}
			for _, m := range rendered.Messages {
				if m.Name == primary && len(m.Fields) > 0 {
					p.SampleField = m.Fields[0].Name
					break
				}
			}
			examples = decoder.RenderXSDCodeExamples(p)
		}
	} else {
		rendered, err = decoder.RenderDescriptorSet(descriptorBytes)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, "render", err.Error())
			return
		}
		// Reconstruct readable .proto source per file in the descriptor.
		source, _ = decoder.WriteProtoText(descriptorBytes)

		// Suggest the protobuf message name implied by the topic's format
		// field (`protobuf:<MsgName>`); the UI lands on that message first.
		if strings.HasPrefix(topic.Format, "protobuf:") {
			primary = strings.TrimPrefix(topic.Format, "protobuf:")
		}

		// Build per-language code examples templated against the primary
		// message + topic key. Picks the first scalar field as the sample
		// to print in the snippet body.
		if primary != "" {
			params := decoder.ExampleParams{
				TopicKey:        key,
				MessageName:     primary,
				SampleField:     "ts_ms",
				SampleFieldJSON: "tsMs",
			}
			for _, m := range rendered.Messages {
				if m.Name == primary || m.FullName == primary {
					params.Package = m.Package
					params.MessageFullName = m.FullName
					params.ProtoFile = m.File
					for _, f := range m.Fields {
						if f.Cardinality == "singular" && (f.Type == "string" || f.Type == "double" || f.Type == "uint64" || f.Type == "int64" || f.Type == "int32" || f.Type == "uint32") {
							params.SampleField = f.Name
							params.SampleFieldJSON = f.JSONName
							break
						}
					}
					break
				}
			}
			examples = decoder.RenderCodeExamples(params)
		}
	}

	// Overlay any vendor-provided rows on top of autogen — a vendor row
	// for a given language replaces the autogen snippet entirely and
	// flips the badge to "vendor". Languages without a row keep their
	// "autogen" source so the UI continues to surface the disclaimer.
	if vendorRows, vrErr := s.reg.ListCodeExamplesByTopic(r.Context(), topic.ID); vrErr == nil && len(vendorRows) > 0 {
		overrides := make(map[string]registry.TopicCodeExample, len(vendorRows))
		for _, v := range vendorRows {
			overrides[v.Language] = v
		}
		// Replace matching autogen entries in-place; append any vendor
		// rows for languages the autogen pass didn't emit (e.g. c, cpp).
		seenLangs := make(map[string]bool)
		for i := range examples {
			seenLangs[examples[i].Language] = true
			if v, ok := overrides[examples[i].Language]; ok {
				examples[i].Code = v.Code
				// ADR 1052 Phase 3: org is the primary provenance value
				// (was "vendor"; the portal's same-image schema.js reads both).
				examples[i].Source = "org"
			}
		}
		for lang, v := range overrides {
			if seenLangs[lang] {
				continue
			}
			examples = append(examples, decoder.CodeExample{
				Language:     lang,
				DisplayName:  displayNameForLanguage(lang),
				HighlightTag: lang,
				Code:         v.Code,
				Source:       "org", // ADR 1052 Phase 3 (was "vendor")
			})
		}
	}

	w.Header().Set("X-Schema-Version", row.Version)
	w.Header().Set("X-Schema-Compatibility", row.Compatibility)
	w.Header().Set("X-Schema-SHA256", row.SHA256)
	w.Header().Set("X-Schema-Size-Bytes", strconv.FormatInt(row.SizeBytes, 10))
	writeJSON(w, http.StatusOK, map[string]any{
		"key":             key,
		"vendor":          vendor,
		"topic_id":        topic.ID,
		"format":          topic.Format,
		"primary_message": primary,
		"schema": map[string]any{
			"version":        row.Version,
			"compatibility":  row.Compatibility,
			"sha256":         row.SHA256,
			"size_bytes":     row.SizeBytes,
			"uploaded_at":    row.UploadedAt,
			"uploaded_by":    row.UploadedBy,
			"all_versions":   versions,
			"format_storage": row.Format,
		},
		"rendered":     rendered,
		"proto_source": source,
		"source_lang":  sourceLang,
		"examples":     examples,
	})
}
