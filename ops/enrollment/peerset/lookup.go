package peerset

// Lookup returns the active peer with the given name plus true, or the
// zero Peer plus false. The returned *Peer is a pointer into the
// PeerSet's underlying slice; callers reading metadata can avoid a
// copy, but mutations must go through Upsert / Remove / Resurrect so
// invariants stay enforced at the API boundary.
func (ps *PeerSet) Lookup(name string) (*Peer, bool) {
	for i := range ps.Peers {
		if ps.Peers[i].Name == name {
			return &ps.Peers[i], true
		}
	}
	return nil, false
}

// LookupByPubkey finds the active peer whose CurrentPubkey matches.
// Used by drift-reconciliation paths in the future 22R.2 orchestrator
// (and by audit tooling) to map "this pubkey is on the relay" back to
// "this peer in the declaration."
func (ps *PeerSet) LookupByPubkey(pubkey string) (*Peer, bool) {
	if pubkey == "" {
		return nil, false
	}
	for i := range ps.Peers {
		if ps.Peers[i].CurrentPubkey == pubkey {
			return &ps.Peers[i], true
		}
	}
	return nil, false
}

// LookupDeprecated returns a deprecated-peer row by name plus true,
// or nil + false. Used by Resurrect; exported for orchestrator and
// audit tooling.
func (ps *PeerSet) LookupDeprecated(name string) (*DeprecatedPeer, bool) {
	for i := range ps.DeprecatedPeers {
		if ps.DeprecatedPeers[i].Name == name {
			return &ps.DeprecatedPeers[i], true
		}
	}
	return nil, false
}

// Relay returns the relay inventory row for a given name. Unused by
// 22R.1's bundle-create call site but exposed because the future
// orchestrator surface (22R.2) will iterate relays for the fan-out
// step and benefits from a uniform lookup primitive.
func (ps *PeerSet) Relay(name string) (*Relay, bool) {
	for i := range ps.Relays {
		if ps.Relays[i].Name == name {
			return &ps.Relays[i], true
		}
	}
	return nil, false
}
