package peerset

import (
	"fmt"
	"time"
)

// now is the wall-clock source mutate paths stamp into Metadata. Test
// hooks override this for deterministic round-trip evidence; production
// callers ignore it.
var now = time.Now

// Upsert inserts or replaces (by Name) the given peer in the active
// peer set. The caller-supplied Peer is validated for duplicates against
// every other active row (pubkey collisions and address collisions are
// fatal; a peer cannot share either with anyone else). Metadata is
// stamped with the operation tag "upsert" and the current time.
//
// Upsert is the *intended* path for the bundle-create
// --update-allowlist call site: it replaces the prior entry by name
// when the operator re-runs for the same device-id, and appends a new
// entry the first time the device is issued.
func (ps *PeerSet) Upsert(p Peer) error {
	if p.Name == "" {
		return fmt.Errorf("%w: empty name", ErrDuplicateName)
	}
	if p.CurrentPubkey == "" {
		return fmt.Errorf("%w: %s missing current_pubkey", ErrSchemaInvalid, p.Name)
	}
	if p.WGCP0Address == "" {
		return fmt.Errorf("%w: %s missing wg_cp0_address", ErrSchemaInvalid, p.Name)
	}

	// Reject collisions against any *other* active peer. Same-name
	// row is the replacement path (we overwrite below); same-pubkey or
	// same-address with a different name is a substrate-breaking
	// collision and must fail loud.
	for i := range ps.Peers {
		if ps.Peers[i].Name == p.Name {
			continue
		}
		if ps.Peers[i].CurrentPubkey == p.CurrentPubkey {
			return fmt.Errorf("%w: %s already held by peer %q", ErrDuplicatePubkey, p.CurrentPubkey, ps.Peers[i].Name)
		}
		if ps.Peers[i].WGCP0Address == p.WGCP0Address {
			return fmt.Errorf("%w: %s already held by peer %q", ErrDuplicateAddress, p.WGCP0Address, ps.Peers[i].Name)
		}
	}

	t := now()
	for i := range ps.Peers {
		if ps.Peers[i].Name == p.Name {
			// Replacement path. If the pubkey changed, the new pubkey
			// will be appended via RotatePubkey; raw Upsert preserves
			// the existing history but does NOT auto-rotate. This is
			// the durable engineering choice from ADR 0322 Property 4
			// (no gratuitous rotation): operators who want to rotate
			// call RotatePubkey explicitly.
			previousCreatedAt := ps.Peers[i].Metadata.CreatedAt
			ps.Peers[i] = p
			if ps.Peers[i].Metadata.CreatedAt.IsZero() {
				ps.Peers[i].Metadata.CreatedAt = previousCreatedAt
			}
			ps.Peers[i].Metadata.LastUpdatedAt = t
			if ps.Peers[i].Metadata.LastOperation == "" {
				ps.Peers[i].Metadata.LastOperation = "upsert"
			}
			ps.stampEnvelope(t, "upsert "+p.Name)
			return nil
		}
	}

	// New row.
	if p.Metadata.CreatedAt.IsZero() {
		p.Metadata.CreatedAt = t
	}
	p.Metadata.LastUpdatedAt = t
	if p.Metadata.LastOperation == "" {
		p.Metadata.LastOperation = "upsert"
	}
	ps.Peers = append(ps.Peers, p)
	ps.stampEnvelope(t, "upsert "+p.Name)
	return nil
}

// RotatePubkey appends the current pubkey to PubkeyHistory and sets a
// fresh one. The caller supplies the new pubkey + a free-form reason
// (audit trail). Returns ErrPeerNotFound if no active peer matches.
// The new pubkey is validated for collision against every other peer.
func (ps *PeerSet) RotatePubkey(name, newPubkey, reason string) error {
	if newPubkey == "" {
		return fmt.Errorf("%w: empty new pubkey", ErrSchemaInvalid)
	}
	for i := range ps.Peers {
		if ps.Peers[i].Name != name {
			continue
		}
		if ps.Peers[i].CurrentPubkey == newPubkey {
			return fmt.Errorf("%w: %s already current for %s", ErrDuplicatePubkey, newPubkey, name)
		}
		for j := range ps.Peers {
			if j == i {
				continue
			}
			if ps.Peers[j].CurrentPubkey == newPubkey {
				return fmt.Errorf("%w: %s already held by peer %q", ErrDuplicatePubkey, newPubkey, ps.Peers[j].Name)
			}
		}
		t := now()
		ps.Peers[i].PubkeyHistory = append(ps.Peers[i].PubkeyHistory, PubkeyHistoryEntry{
			Pubkey:    ps.Peers[i].CurrentPubkey,
			RotatedAt: t,
			Reason:    reason,
		})
		ps.Peers[i].CurrentPubkey = newPubkey
		ps.Peers[i].Metadata.LastUpdatedAt = t
		ps.Peers[i].Metadata.LastOperation = "rotate"
		ps.stampEnvelope(t, "rotate "+name)
		return nil
	}
	return fmt.Errorf("%w: %s", ErrPeerNotFound, name)
}

// Remove moves the named active peer into deprecated_peers[] with the
// caller-supplied reason and a removal timestamp. Returns ErrPeerNotFound
// if no active peer matches. This is the *only* path that takes a peer
// out of the active set; the operator workflow that calls it sees
// "WOULD REMOVE" in the dry-run before the change lands.
func (ps *PeerSet) Remove(name, reason string) error {
	for i := range ps.Peers {
		if ps.Peers[i].Name != name {
			continue
		}
		t := now()
		dep := DeprecatedPeer{
			Peer:          ps.Peers[i],
			RemovedAt:     t,
			RemovedReason: reason,
		}
		dep.Peer.Metadata.LastUpdatedAt = t
		dep.Peer.Metadata.LastOperation = "remove"
		ps.DeprecatedPeers = append(ps.DeprecatedPeers, dep)
		ps.Peers = append(ps.Peers[:i], ps.Peers[i+1:]...)
		ps.stampEnvelope(t, "remove "+name)
		return nil
	}
	return fmt.Errorf("%w: %s", ErrPeerNotFound, name)
}

// Resurrect is the inverse of Remove. It moves the named row out of
// deprecated_peers[] and back into the active set, preserving every
// previously-recorded field (including PubkeyHistory). Returns
// ErrPeerNotFound when no deprecated row matches. Returns
// ErrDuplicateName / ErrDuplicatePubkey / ErrDuplicateAddress if the
// active set has acquired a conflicting peer in the interim.
func (ps *PeerSet) Resurrect(name string) error {
	for i := range ps.DeprecatedPeers {
		if ps.DeprecatedPeers[i].Name != name {
			continue
		}
		dep := ps.DeprecatedPeers[i]
		for j := range ps.Peers {
			switch {
			case ps.Peers[j].Name == dep.Name:
				return fmt.Errorf("%w: active peer %q already exists", ErrDuplicateName, dep.Name)
			case ps.Peers[j].CurrentPubkey == dep.CurrentPubkey:
				return fmt.Errorf("%w: %s already held by peer %q", ErrDuplicatePubkey, dep.CurrentPubkey, ps.Peers[j].Name)
			case ps.Peers[j].WGCP0Address == dep.WGCP0Address:
				return fmt.Errorf("%w: %s already held by peer %q", ErrDuplicateAddress, dep.WGCP0Address, ps.Peers[j].Name)
			}
		}
		t := now()
		dep.Peer.Metadata.LastUpdatedAt = t
		dep.Peer.Metadata.LastOperation = "resurrect"
		ps.Peers = append(ps.Peers, dep.Peer)
		ps.DeprecatedPeers = append(ps.DeprecatedPeers[:i], ps.DeprecatedPeers[i+1:]...)
		ps.stampEnvelope(t, "resurrect "+name)
		return nil
	}
	return fmt.Errorf("%w: deprecated peer %s", ErrPeerNotFound, name)
}

// stampEnvelope updates the envelope-level Metadata.LastModified* fields
// after every mutation so a casual reader of the JSON sees when and how
// the file last changed.
func (ps *PeerSet) stampEnvelope(t time.Time, via string) {
	ps.Metadata.LastModifiedAt = t
	ps.Metadata.LastModifiedVia = via
}
