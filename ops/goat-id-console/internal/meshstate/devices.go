package meshstate

import "strings"

// DevicePeer is one mesh peer attributed to a person, as the onboarding card
// shows it.
type DevicePeer struct {
	Name      string
	OS        string
	Connected bool
	// ControlOnly: management reports the peer connected, but the management
	// host has never completed a WireGuard handshake with it — the site lets
	// the control connection out and blocks the data ports (handshakes.go).
	ControlOnly bool
}

// DeviceSummary is what the onboarding card says about a person's enrolled
// devices.
//
// ServerOnly is true when the person owns at least one peer and none of them is
// a device a person browses from (Windows, macOS, iOS, Android). Every login
// surface is mesh-only, so such a person is `enrolled` yet cannot open the
// login link from the workstation they read their email on, for example when
// the only peer is a headless Linux server (#1586).
// It is a display hint, never a state: the enrolled state machine is unchanged.
type DeviceSummary struct {
	Peers      []DevicePeer
	ServerOnly bool
	// ControlOnly is true when every connected peer the person owns is
	// control-only: nothing on the mesh, sign-in pages included, is reachable
	// from any of their devices.
	ControlOnly bool
}

// DevicesFor returns the peers owned by handle (same name grammar as every
// other attribution here, OwnsName) and whether all of them are server-class.
//
// hs is the management host's per-peer handshake view keyed by mesh IP (nil
// when unavailable). A peer is flagged control-only only when hs has a row for
// it saying no handshake ever happened; a peer missing from hs is unknown and
// never flagged.
func DevicesFor(peers []Peer, handle string, known map[string]struct{}, hs map[string]PeerHandshake) DeviceSummary {
	var out DeviceSummary
	browser := false
	for _, p := range peers {
		if !OwnsName(p.Name, handle, known) {
			continue
		}
		dp := DevicePeer{Name: p.Name, OS: p.OS, Connected: p.Connected}
		if h, ok := hs[p.IP]; ok && p.Connected && h.NeverHandshaked() {
			dp.ControlOnly = true
		}
		out.Peers = append(out.Peers, dp)
		if BrowserCapableOS(p.OS) {
			browser = true
		}
	}
	out.ServerOnly = len(out.Peers) > 0 && !browser
	connected, controlOnly := 0, 0
	for _, d := range out.Peers {
		if d.Connected {
			connected++
			if d.ControlOnly {
				controlOnly++
			}
		}
	}
	out.ControlOnly = connected > 0 && controlOnly == connected
	return out
}

// BrowserCapableOS reports whether NetBird's OS string names a platform people
// browse from. A heuristic, deliberately narrow: Linux is NOT counted, because
// NetBird reports a desktop and a headless server Linux the same way, and the
// failure this exists for is a Linux server mistaken for "enrolled". A Linux
// desktop user therefore sees the hint too; it is advisory text, not a gate.
func BrowserCapableOS(os string) bool {
	o := strings.ToLower(os)
	for _, k := range []string{"windows", "darwin", "macos", "mac os", "ios", "ipados", "android"} {
		if strings.Contains(o, k) {
			return true
		}
	}
	return false
}
