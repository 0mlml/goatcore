package api

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Payload + cadence shape validators for trashgoat specs.
//
// The portal does NOT execute payloads — that's the runner's job (PR #3).
// What the portal IS responsible for: refusing specs whose
// payload_spec/cadence_spec JSON shape would obviously crash or
// confuse the runner, and bounding payload sizes at sanity-check
// thresholds. The runner is free to refuse more (e.g. higher rate
// limits than the cadence asserts).
//
// Validation is hand-rolled because the narrow domain (4 payload kinds
// + 3 cadence kinds) doesn't justify a generic JSON Schema library.
// Each violation carries a `field` path so the UI can highlight it.

// trashgoatSpecError is a single-violation report. Multiple fields
// surface as a list (`{"errors":[{...},{...}]}`).
type trashgoatSpecError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (e trashgoatSpecError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// Sanity limits. Not policy enforcement — the runner can choose to
// refuse smaller or fewer. These are the "obviously a typo" floor.
const (
	trashgoatMaxBytes              = 64 * 1024 * 1024 // 64 MiB
	trashgoatMinChunkSize          = 1024             // 1 KiB
	trashgoatMaxChunkSize          = 4 * 1024 * 1024  // 4 MiB
	trashgoatMaxBurstCount         = 10000
	trashgoatMaxEveryIntervalMS    = 24 * 60 * 60 * 1000 // 24h
	trashgoatMinEveryIntervalMS    = 1                   // runner may clamp; portal refuses 0
	trashgoatMaxBurstSpacingMS     = 24 * 60 * 60 * 1000
)

// validateTrashgoatPayloadSpec walks raw payload JSON; returns the
// first violation it finds (UI surfaces one field at a time today). On
// success returns the canonical compacted JSON to persist.
func validateTrashgoatPayloadSpec(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", trashgoatSpecError{Field: "payload_spec", Message: "must be a JSON object"}
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return "", trashgoatSpecError{Field: "payload_spec", Message: "must be a JSON object: " + err.Error()}
	}
	kindAny, ok := m["kind"]
	if !ok {
		return "", trashgoatSpecError{Field: "payload_spec.kind", Message: "required"}
	}
	kind, ok := kindAny.(string)
	if !ok {
		return "", trashgoatSpecError{Field: "payload_spec.kind", Message: "must be a string"}
	}
	switch kind {
	case "bytes":
		if err := validatePayloadBytes(m); err != nil {
			return "", err
		}
	case "chunked":
		if err := validatePayloadChunked(m); err != nil {
			return "", err
		}
	case "json_shape":
		if err := validatePayloadJSONShape(m); err != nil {
			return "", err
		}
	case "protobuf_shape":
		if err := validatePayloadProtobufShape(m); err != nil {
			return "", err
		}
	default:
		return "", trashgoatSpecError{Field: "payload_spec.kind", Message: fmt.Sprintf("unknown kind %q (expected bytes|chunked|json_shape|protobuf_shape)", kind)}
	}
	return compactJSON(raw)
}

func validatePayloadBytes(m map[string]any) error {
	size, err := intField(m, "size_bytes", true)
	if err != nil {
		return err
	}
	if size <= 0 {
		return trashgoatSpecError{Field: "payload_spec.size_bytes", Message: "must be > 0"}
	}
	if size > trashgoatMaxBytes {
		return trashgoatSpecError{Field: "payload_spec.size_bytes", Message: fmt.Sprintf("must be ≤ %d (64 MiB)", trashgoatMaxBytes)}
	}
	if err := validatePayloadContentField(m); err != nil {
		return err
	}
	return nil
}

func validatePayloadChunked(m map[string]any) error {
	total, err := intField(m, "total_size_bytes", true)
	if err != nil {
		return err
	}
	if total <= 0 {
		return trashgoatSpecError{Field: "payload_spec.total_size_bytes", Message: "must be > 0"}
	}
	if total > trashgoatMaxBytes {
		return trashgoatSpecError{Field: "payload_spec.total_size_bytes", Message: fmt.Sprintf("must be ≤ %d (64 MiB)", trashgoatMaxBytes)}
	}
	chunk, err := intField(m, "chunk_size_bytes", true)
	if err != nil {
		return err
	}
	if chunk < trashgoatMinChunkSize || chunk > trashgoatMaxChunkSize {
		return trashgoatSpecError{Field: "payload_spec.chunk_size_bytes", Message: fmt.Sprintf("must be in [%d, %d]", trashgoatMinChunkSize, trashgoatMaxChunkSize)}
	}
	if chunk > total {
		return trashgoatSpecError{Field: "payload_spec.chunk_size_bytes", Message: "must be ≤ total_size_bytes"}
	}
	if err := validatePayloadContentField(m); err != nil {
		return err
	}
	return nil
}

func validatePayloadJSONShape(m map[string]any) error {
	tpl, ok := m["template"]
	if !ok {
		return trashgoatSpecError{Field: "payload_spec.template", Message: "required"}
	}
	if _, ok := tpl.(map[string]any); !ok {
		return trashgoatSpecError{Field: "payload_spec.template", Message: "must be an object"}
	}
	return nil
}

func validatePayloadProtobufShape(m map[string]any) error {
	mt, ok := m["message_type"].(string)
	if !ok || mt == "" {
		return trashgoatSpecError{Field: "payload_spec.message_type", Message: "required (e.g. notional.tracks_demo.v1.Entity)"}
	}
	if !strings.Contains(mt, ".") {
		return trashgoatSpecError{Field: "payload_spec.message_type", Message: "must be a fully-qualified protobuf message name (with package)"}
	}
	// fields is optional — when absent, runner emits a zero-valued
	// message. Shape-validate when present.
	if fields, ok := m["fields"]; ok {
		if _, ok := fields.(map[string]any); !ok {
			return trashgoatSpecError{Field: "payload_spec.fields", Message: "must be an object when present"}
		}
	}
	return nil
}

// validatePayloadContentField checks the optional `content` directive
// shared by bytes + chunked. Defaults to "random" when absent.
func validatePayloadContentField(m map[string]any) error {
	cv, ok := m["content"]
	if !ok {
		return nil
	}
	cs, ok := cv.(string)
	if !ok {
		return trashgoatSpecError{Field: "payload_spec.content", Message: "must be a string"}
	}
	switch {
	case cs == "random" || cs == "zeros":
		return nil
	case strings.HasPrefix(cs, "repeating:"):
		if len(cs) == len("repeating:") {
			return trashgoatSpecError{Field: "payload_spec.content", Message: "repeating: needs a pattern after the colon"}
		}
		return nil
	default:
		return trashgoatSpecError{Field: "payload_spec.content", Message: fmt.Sprintf("unknown content %q (expected random|zeros|repeating:<pattern>)", cs)}
	}
}

// validateTrashgoatCadenceSpec walks raw cadence JSON; same return
// shape as the payload validator.
func validateTrashgoatCadenceSpec(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", trashgoatSpecError{Field: "cadence_spec", Message: "must be a JSON object"}
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return "", trashgoatSpecError{Field: "cadence_spec", Message: "must be a JSON object: " + err.Error()}
	}
	kindAny, ok := m["kind"]
	if !ok {
		return "", trashgoatSpecError{Field: "cadence_spec.kind", Message: "required"}
	}
	kind, ok := kindAny.(string)
	if !ok {
		return "", trashgoatSpecError{Field: "cadence_spec.kind", Message: "must be a string"}
	}
	switch kind {
	case "oneshot":
		// nothing else required
	case "every":
		ms, err := intField(m, "interval_ms", true)
		if err != nil {
			return "", err
		}
		if ms < trashgoatMinEveryIntervalMS {
			return "", trashgoatSpecError{Field: "cadence_spec.interval_ms", Message: "must be > 0"}
		}
		if ms > trashgoatMaxEveryIntervalMS {
			return "", trashgoatSpecError{Field: "cadence_spec.interval_ms", Message: fmt.Sprintf("must be ≤ %d (24h)", trashgoatMaxEveryIntervalMS)}
		}
	case "burst":
		count, err := intField(m, "count", true)
		if err != nil {
			return "", err
		}
		if count <= 0 {
			return "", trashgoatSpecError{Field: "cadence_spec.count", Message: "must be > 0"}
		}
		if count > trashgoatMaxBurstCount {
			return "", trashgoatSpecError{Field: "cadence_spec.count", Message: fmt.Sprintf("must be ≤ %d", trashgoatMaxBurstCount)}
		}
		spacing, err := intField(m, "spacing_ms", true)
		if err != nil {
			return "", err
		}
		if spacing < 0 {
			return "", trashgoatSpecError{Field: "cadence_spec.spacing_ms", Message: "must be ≥ 0"}
		}
		if spacing > trashgoatMaxBurstSpacingMS {
			return "", trashgoatSpecError{Field: "cadence_spec.spacing_ms", Message: fmt.Sprintf("must be ≤ %d (24h)", trashgoatMaxBurstSpacingMS)}
		}
		// then_idle is optional bool; if present, just validate type.
		if ti, ok := m["then_idle"]; ok {
			if _, ok := ti.(bool); !ok {
				return "", trashgoatSpecError{Field: "cadence_spec.then_idle", Message: "must be a boolean when present"}
			}
		}
	default:
		return "", trashgoatSpecError{Field: "cadence_spec.kind", Message: fmt.Sprintf("unknown kind %q (expected oneshot|every|burst)", kind)}
	}
	return compactJSON(raw)
}

// intField extracts an integer field from a JSON-unmarshalled map.
// JSON numbers come back as float64; we accept that and convert,
// refusing non-integral values. When required is true and missing,
// returns a violation.
func intField(m map[string]any, name string, required bool) (int64, error) {
	v, ok := m[name]
	if !ok {
		if required {
			return 0, trashgoatSpecError{Field: "payload_spec." + name, Message: "required"}
		}
		return 0, nil
	}
	switch n := v.(type) {
	case float64:
		if n != float64(int64(n)) {
			return 0, trashgoatSpecError{Field: "payload_spec." + name, Message: "must be an integer"}
		}
		return int64(n), nil
	case int:
		return int64(n), nil
	case int64:
		return n, nil
	default:
		return 0, trashgoatSpecError{Field: "payload_spec." + name, Message: "must be an integer"}
	}
}

// compactJSON re-marshals to drop whitespace; deterministic storage form.
func compactJSON(raw json.RawMessage) (string, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", err
	}
	out, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(out), nil
}
