package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
)

// relayEntry is the JSON shape of the operator-curated relays file.
//
// The address field (control-plane mesh address, e.g. 198.18.0.2) is
// informational from bundle-create's perspective — the bundle's
// known_endpoints[*] carries (endpoint, pubkey, kind="cp-relay") and
// the device-side wg-cp0-bundle-apply derives AllowedIPs from the
// address field at render time.
type relayEntry struct {
	Name     string `json:"name"`
	Endpoint string `json:"endpoint"`
	Address  string `json:"address"`
	Pubkey   string `json:"pubkey"`

	// PubkeyBytes is the decoded 32-byte Curve25519 public key, populated
	// after JSON parse + validation.
	PubkeyBytes []byte `json:"-"`
}

func loadRelaysFile(path string) ([]relayEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var entries []relayEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("parse json: %w", err)
	}

	seen := make(map[string]bool, len(entries))
	for i := range entries {
		e := &entries[i]
		if err := validateRelayEntry(e); err != nil {
			return nil, fmt.Errorf("relay %q: %w", e.Name, err)
		}
		if seen[e.Name] {
			return nil, fmt.Errorf("duplicate relay name: %q", e.Name)
		}
		seen[e.Name] = true

		raw, err := base64.StdEncoding.DecodeString(e.Pubkey)
		if err != nil {
			return nil, fmt.Errorf("relay %q: pubkey base64 decode: %w", e.Name, err)
		}
		if len(raw) != wgPubkeySize {
			return nil, fmt.Errorf("relay %q: pubkey wrong size: got %d want %d", e.Name, len(raw), wgPubkeySize)
		}
		e.PubkeyBytes = raw
	}
	return entries, nil
}

func validateRelayEntry(e *relayEntry) error {
	if e.Name == "" {
		return fmt.Errorf("name is required")
	}
	if e.Endpoint == "" {
		return fmt.Errorf("endpoint is required")
	}
	if !strings.Contains(e.Endpoint, ":") {
		return fmt.Errorf("endpoint %q must include port (e.g. host:51821)", e.Endpoint)
	}
	host, port, err := net.SplitHostPort(e.Endpoint)
	if err != nil {
		return fmt.Errorf("endpoint %q: %w", e.Endpoint, err)
	}
	if host == "" {
		return fmt.Errorf("endpoint %q: host empty", e.Endpoint)
	}
	if port == "" {
		return fmt.Errorf("endpoint %q: port empty", e.Endpoint)
	}
	if e.Address == "" {
		return fmt.Errorf("address is required")
	}
	if ip := net.ParseIP(e.Address); ip == nil {
		return fmt.Errorf("address %q is not a valid IP literal", e.Address)
	}
	if e.Pubkey == "" {
		return fmt.Errorf("pubkey is required")
	}
	return nil
}
