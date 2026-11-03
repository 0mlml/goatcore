package peerset

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Load reads the JSON-of-record at path, parses it, auto-migrates v1
// (today's flat array of {name, pubkey, address}) to v2 in-memory on
// read, and validates schema invariants before returning.
//
// A missing file is not an error — Load returns an empty v2 PeerSet
// so callers can treat "no file yet" as "no peers yet" without a
// dedicated probe step. This matches the existing bundle-create
// allowlist semantics it replaces.
func Load(path string) (*PeerSet, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return emptyV2(), nil
		}
		return nil, fmt.Errorf("peerset: read %s: %w", path, err)
	}
	return parse(data)
}

// parse classifies the document (v1 or v2), routes it through the
// appropriate unmarshaler, and runs Validate before returning.
// Exported tests cover both shapes via Load; parse is the internal
// branching point.
func parse(data []byte) (*PeerSet, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return emptyV2(), nil
	}

	// Branch on the first non-whitespace byte. `[` is the v1 flat-array
	// shape; `{` is a v2 envelope. Anything else is malformed.
	switch trimmed[0] {
	case '[':
		ps, err := migrateV1(trimmed)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrMigrationFailed, err)
		}
		if err := ps.Validate(); err != nil {
			return nil, err
		}
		return ps, nil
	case '{':
		var ps PeerSet
		dec := json.NewDecoder(bytes.NewReader(trimmed))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&ps); err != nil {
			return nil, fmt.Errorf("%w: parse v2: %v", ErrSchemaInvalid, err)
		}
		if ps.SchemaVersion == 0 {
			// A v2-shaped envelope without an explicit schema_version is
			// either operator hand-edit or a partial-migrate artifact;
			// surface as schema-invalid rather than silently defaulting.
			return nil, fmt.Errorf("%w: missing schema_version", ErrSchemaInvalid)
		}
		if ps.SchemaVersion > SchemaVersion {
			return nil, fmt.Errorf("%w: schema_version %d > supported %d", ErrSchemaInvalid, ps.SchemaVersion, SchemaVersion)
		}
		if err := ps.Validate(); err != nil {
			return nil, err
		}
		return &ps, nil
	default:
		return nil, fmt.Errorf("%w: unexpected leading byte %q", ErrSchemaInvalid, trimmed[0])
	}
}

// Save writes the PeerSet to path as a pretty-printed v2 envelope with
// deterministic field order (the struct definition is the source of
// order) and atomic file replacement (write to .tmp, rename). Mode is
// 0644 — the file is committed-in-git and consumed by Ansible plays
// running as different uids; secrecy is not the threat model (the
// pubkeys aren't secret), so a restrictive mode would only break
// downstream readers.
func (ps *PeerSet) Save(path string) error {
	// Always stamp the schema_version we serialize. If a caller built a
	// PeerSet from scratch without setting it, Save normalizes the
	// envelope rather than emitting a schema-invalid document.
	out := *ps
	out.SchemaVersion = SchemaVersion
	if out.Metadata.SchemaMinVersion == 0 {
		out.Metadata.SchemaMinVersion = SchemaVersion
	}
	// Defensive: never emit nil slices — empty arrays are easier to
	// scan in `git diff` and unambiguous in downstream consumers.
	if out.Peers == nil {
		out.Peers = []Peer{}
	}
	if out.Relays == nil {
		out.Relays = []Relay{}
	}
	if out.DeprecatedPeers == nil {
		out.DeprecatedPeers = []DeprecatedPeer{}
	}

	data, err := json.MarshalIndent(&out, "", "  ")
	if err != nil {
		return fmt.Errorf("peerset: marshal: %w", err)
	}
	data = append(data, '\n')
	return atomicWriteFile(path, data, 0o644)
}

// atomicWriteFile writes data to a sibling .tmp file and renames it
// over the target on success. Same shape as bundle-create's prior
// allowlist writer; lifted here so peerset is the only owner.
func atomicWriteFile(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("peerset: ensure dir %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp.")
	if err != nil {
		return fmt.Errorf("peerset: create tmp: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("peerset: write tmp: %w", err)
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("peerset: chmod tmp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("peerset: close tmp: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		cleanup()
		return fmt.Errorf("peerset: rename: %w", err)
	}
	return nil
}

// emptyV2 returns a PeerSet representing "no peers, no history" — the
// load-time fallback for a missing or empty file. Callers see a usable
// envelope rather than a nil pointer.
func emptyV2() *PeerSet {
	return &PeerSet{
		SchemaVersion:   SchemaVersion,
		Peers:           []Peer{},
		Relays:          []Relay{},
		DeprecatedPeers: []DeprecatedPeer{},
		Metadata:        Metadata{SchemaMinVersion: SchemaVersion},
	}
}
