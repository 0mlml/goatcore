package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// manifestJSONToYAML converts a stored manifest_json string to the YAML shape
// `goat admin onboard` reads from ops/operators/<env>/<handle>.yml. It normalizes
// JSON numbers so a whole number (ttl_days, usage_limit) renders as an int, not a
// float — the same SQLSTATE-safe / int-preserving normalization the portal's
// approve renderer does, so the on-disk manifest is byte-identical to what an
// operator would have run by hand.
func manifestJSONToYAML(manifestJSON string) ([]byte, error) {
	dec := json.NewDecoder(strings.NewReader(manifestJSON))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("decode manifest_json: %w", err)
	}
	v = normalizeNumbers(v)
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		return nil, fmt.Errorf("encode yaml: %w", err)
	}
	_ = enc.Close()
	return buf.Bytes(), nil
}

// normalizeNumbers recursively turns json.Number into int64 (when integral) or
// float64, so yaml.Marshal emits `usage_limit: 5`, never `5` quoted or `5.0`.
func normalizeNumbers(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			t[k] = normalizeNumbers(val)
		}
		return t
	case []any:
		for i, val := range t {
			t[i] = normalizeNumbers(val)
		}
		return t
	case json.Number:
		if i, err := t.Int64(); err == nil {
			return i
		}
		if f, err := t.Float64(); err == nil {
			return f
		}
		return t.String()
	default:
		return v
	}
}
