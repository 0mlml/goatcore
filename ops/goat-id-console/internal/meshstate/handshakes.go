package meshstate

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// The NetBird management API marks a peer `connected` once it holds a
// control-plane session, so a device whose site passes TCP 443 and blocks the
// mesh data ports reads as connected while it can reach nothing (zagros,
// 0 of 41 peers, every relay timed out). Only a peer's own view
// separates the two. The mesh-health snapshot on the management host writes
// that view: for each peer, this host's last WireGuard handshake with it
// (roles/mesh-health, peer-handshakes.json, four fields, mounted read-only).

// HandshakeStaleAfter is how old the summary may be before it is ignored. The
// snapshot timer runs every 20s; a file older than this means the writer has
// stopped, and a stale view is never believed.
const HandshakeStaleAfter = 5 * time.Minute

// PeerHandshake is one row of the summary.
type PeerHandshake struct {
	IP            string `json:"ip"`
	FQDN          string `json:"fqdn"`
	Status        string `json:"status"`
	LastHandshake string `json:"last_handshake"`
}

// NeverHandshaked is true when this host has never completed a WireGuard
// handshake with the peer: not connected, and no handshake time (NetBird
// writes the zero time, 0001-01-01T00:00:00Z, or nothing).
func (h PeerHandshake) NeverHandshaked() bool {
	if h.Status == "Connected" {
		return false
	}
	if h.LastHandshake == "" {
		return true
	}
	t, err := time.Parse(time.RFC3339Nano, h.LastHandshake)
	if err != nil {
		return false // unreadable is unknown, never "never"
	}
	return t.IsZero() || t.Year() <= 1
}

// ReadHandshakes loads the summary keyed by mesh IP. It refuses a file older
// than HandshakeStaleAfter (relative to now) so a stopped writer cannot keep a
// flag up or down.
func ReadHandshakes(path string, now time.Time) (map[string]PeerHandshake, error) {
	if path == "" {
		return nil, fmt.Errorf("PEER_HANDSHAKES_PATH is unset")
	}
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if age := now.Sub(st.ModTime()); age > HandshakeStaleAfter {
		return nil, fmt.Errorf("peer handshake summary is stale (%s old)", age.Round(time.Second))
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc struct {
		Peers []PeerHandshake `json:"peers"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("peer handshake summary unreadable: %w", err)
	}
	out := make(map[string]PeerHandshake, len(doc.Peers))
	for _, p := range doc.Peers {
		if p.IP != "" {
			out[p.IP] = p
		}
	}
	return out, nil
}
