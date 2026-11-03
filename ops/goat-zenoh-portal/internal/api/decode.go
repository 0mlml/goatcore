package api

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/decoder"
	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/registry"
	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/zenohproxy"
)

// Block 85 PR-C — inspector-side schema decode.
//
// GET /api/topics/decoded?key=<topic>[&n=20][&version=v2]
//
// Behavior:
//
//	1. Look up the topic by (vendor, key_pattern). 404 if not registered.
//	2. Verify the topic has format protobuf:<MsgName>. 400 otherwise — the
//	   other formats (json/cbor/msgpack/text/raw) decode in the browser
//	   without needing schema fetch.
//	3. Look up the schema row for the topic. 409 if no body uploaded yet.
//	4. Read the descriptor bytes from disk via the schemastore.
//	5. Fetch n recent samples from Zenoh.
//	6. For each sample, extract the payload bytes from sample.Value and
//	   decode against the descriptor. Report per-sample error rather than
//	   failing the whole response — one bad sample shouldn't hide the rest.
//
// Response:
//
//	{
//	  "key": "caracal/tracks/aircraft/v1",
//	  "format": "protobuf:Track",
//	  "schema_version": "v1",
//	  "samples": [
//	    {
//	      "timestamp": "...",
//	      "encoding": "application/x-protobuf",
//	      "source": "...",
//	      "decoded": { /* JSON object */ },
//	      "error": null
//	    },
//	    ...
//	  ]
//	}

type decodedSample struct {
	Timestamp string          `json:"timestamp,omitempty"`
	Encoding  string          `json:"encoding,omitempty"`
	Source    string          `json:"source,omitempty"`
	Decoded   json.RawMessage `json:"decoded,omitempty"`
	Error     string          `json:"error,omitempty"`
	// Raw, base64-encoded payload bytes. Included only when the
	// caller passes `&raw=true`; off by default to keep the response
	// small for the common UI use case where only the decoded JSON
	// matters. Used by the vendor-decode-rca diagnostic tool + the
	// stethoscope decode-rate check to discriminate failure modes
	// against on-wire bytes when the descriptor itself looks healthy
	// but decode is failing. See docs/operations/vendor-schema-fixup.md.
	RawBase64 string `json:"raw_base64,omitempty"`
}

func (s *Server) handleDecoded(w http.ResponseWriter, r *http.Request) {
	if s.schemas == nil {
		writeError(w, http.StatusServiceUnavailable, "config", "schema store not configured")
		return
	}
	key := r.URL.Query().Get("key")
	if key == "" {
		writeError(w, http.StatusBadRequest, "decode", "missing key= query param")
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
			writeError(w, http.StatusNotFound, "notfound", "topic not registered")
			return
		}
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}

	// Dispatch by format: protobuf:<Msg> decodes wire bytes against the
	// .fds; xml / xml:<Root> project the XML document to JSON (root
	// checked against the declared element when present). `symbol` is
	// the message name or root element; `isXML` selects the decoder.
	var symbol string
	isXML := false
	switch {
	case strings.HasPrefix(topic.Format, "protobuf:"):
		symbol = strings.TrimPrefix(topic.Format, "protobuf:")
		if symbol == "" {
			writeError(w, http.StatusBadRequest, "format", "topic format has empty protobuf message name")
			return
		}
	case strings.HasPrefix(topic.Format, "xml:"):
		isXML = true
		symbol = strings.TrimPrefix(topic.Format, "xml:")
		if symbol == "" {
			writeError(w, http.StatusBadRequest, "format", "topic format has empty xml root element")
			return
		}
	case topic.Format == "xml":
		isXML = true // opaque XML: structural decode, no root check
	default:
		writeError(w, http.StatusBadRequest, "format",
			"decoded endpoint handles protobuf:<MsgName> or xml[:<Root>] topics; topic format is "+topic.Format)
		return
	}

	// Look up the schema — either the requested version or the latest.
	var schemaRow *registry.TopicSchema
	if v := r.URL.Query().Get("version"); v != "" {
		if verr := registry.ValidSchemaVersion(v); verr != nil {
			writeError(w, http.StatusBadRequest, "validate", verr.Error())
			return
		}
		schemaRow, err = s.reg.GetSchemaByTopicVersion(r.Context(), topic.ID, v)
	} else {
		schemaRow, err = s.reg.GetLatestSchemaForTopic(r.Context(), topic.ID)
	}
	// XML is self-describing, so xml / xml:<Root> topics decode without an
	// uploaded schema body; protobuf requires the .fds.
	var descriptorBytes []byte
	schemaVersion := ""
	if err != nil {
		if errors.Is(err, registry.ErrNotFound) {
			if !isXML {
				writeError(w, http.StatusConflict, "no_schema",
					"topic has no schema uploaded; POST one to /api/vendors/"+vendor+"/topics/"+strconv.FormatInt(topic.ID, 10)+"/schemas first")
				return
			}
		} else {
			writeError(w, http.StatusInternalServerError, "db", err.Error())
			return
		}
	} else {
		schemaVersion = schemaRow.Version
		rc, oerr := s.schemas.Open(schemaRow.FilePath)
		if oerr != nil {
			writeError(w, http.StatusInternalServerError, "store", oerr.Error())
			return
		}
		descriptorBytes, err = io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "store", err.Error())
			return
		}
	}

	// Fetch samples. Default n=1 (latest); operator can request more.
	n := 1
	if v := r.URL.Query().Get("n"); v != "" {
		if parsed, perr := strconv.Atoi(v); perr == nil && parsed > 0 && parsed <= 100 {
			n = parsed
		}
	}
	var samples []zenohproxy.Sample
	if n == 1 {
		samples, err = s.zenoh.Latest(r.Context(), key)
	} else {
		samples, err = s.zenoh.Recent(r.Context(), key, n)
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, "zenoh", err.Error())
		return
	}

	includeRaw := r.URL.Query().Get("raw") == "true"
	out := make([]decodedSample, 0, len(samples))
	for _, smp := range samples {
		entry := decodedSample{
			Timestamp: smp.Timestamp,
			Encoding:  smp.Encoding,
			Source:    smp.Source,
		}
		payload, perr := extractPayload(smp.Value)
		if perr != nil {
			entry.Error = "extract payload: " + perr.Error()
			out = append(out, entry)
			continue
		}
		if includeRaw {
			entry.RawBase64 = base64.StdEncoding.EncodeToString(payload)
		}
		var decoded []byte
		var derr error
		if isXML {
			decoded, derr = decoder.DecodeXML(descriptorBytes, symbol, payload)
		} else {
			decoded, derr = decoder.DecodeProtobuf(descriptorBytes, symbol, payload)
		}
		if derr != nil {
			entry.Error = derr.Error()
			out = append(out, entry)
			continue
		}
		entry.Decoded = decoded
		out = append(out, entry)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"key":            key,
		"format":         topic.Format,
		"schema_version": schemaVersion,
		"samples":        out,
	})
}

// extractPayload pulls binary payload bytes out of a Zenoh REST sample's
// Value field. Zenoh REST returns the value as one of:
//
//   - a base64 string (typical for binary encodings) — JSON: "AAAA..."
//   - a numeric byte array — JSON: [12, 34, 56]
//   - an inline JSON object/scalar (for application/json encoding)
//
// The protobuf decode only makes sense for binary payloads; the first
// two shapes are useful, the third returns the raw JSON bytes (which
// will fail the protobuf decode and surface as a per-sample error).
func extractPayload(value json.RawMessage) ([]byte, error) {
	if len(value) == 0 {
		return nil, errors.New("empty value")
	}
	// Attempt base64 string.
	var s string
	if err := json.Unmarshal(value, &s); err == nil {
		return base64.StdEncoding.DecodeString(s)
	}
	// Attempt byte array.
	var bs []byte
	if err := json.Unmarshal(value, &bs); err == nil {
		return bs, nil
	}
	// Fall through: return the raw JSON bytes verbatim. Caller's decode
	// will fail with a descriptive error.
	return []byte(value), nil
}
