package peerset

import "fmt"

// Validate enforces schema invariants the API boundary should already
// be preserving but that an operator-edited file (or a buggy upstream
// migration) might violate. Called by Load after parse + after every
// mutation method indirectly via the same checks they enforce on
// input, plus exposed for explicit pre-Save validation in CI gates.
//
// Invariants:
//
//   - schema_version is 2 (the version this package speaks).
//   - Every active Peer has non-empty Name, WGCP0Address, CurrentPubkey.
//   - No two active peers share a Name.
//   - No two active peers share a CurrentPubkey.
//   - No two active peers share a WGCP0Address.
//   - PubkeyHistory entries carry non-empty Pubkey + non-zero RotatedAt.
//   - Every Relay has non-empty Name.
//   - No two relays share a Name.
//   - Every DeprecatedPeer carries the inner Peer's Name + a non-zero
//     RemovedAt.
//   - ActiveForwarder, when set, has non-empty Tier and Host.
func (ps *PeerSet) Validate() error {
	if ps.SchemaVersion != SchemaVersion {
		return fmt.Errorf("%w: schema_version %d (want %d)", ErrSchemaInvalid, ps.SchemaVersion, SchemaVersion)
	}

	seenName := make(map[string]struct{}, len(ps.Peers))
	seenPubkey := make(map[string]string, len(ps.Peers))
	seenAddr := make(map[string]string, len(ps.Peers))

	for i, p := range ps.Peers {
		if p.Name == "" {
			return fmt.Errorf("%w: peers[%d] empty name", ErrSchemaInvalid, i)
		}
		if p.CurrentPubkey == "" {
			return fmt.Errorf("%w: peers[%d] (%s) empty current_pubkey", ErrSchemaInvalid, i, p.Name)
		}
		if p.WGCP0Address == "" {
			return fmt.Errorf("%w: peers[%d] (%s) empty wg_cp0_address", ErrSchemaInvalid, i, p.Name)
		}
		if _, ok := seenName[p.Name]; ok {
			return fmt.Errorf("%w: %s appears twice in active peers", ErrDuplicateName, p.Name)
		}
		seenName[p.Name] = struct{}{}
		if prev, ok := seenPubkey[p.CurrentPubkey]; ok {
			return fmt.Errorf("%w: %s shared by %q and %q", ErrDuplicatePubkey, p.CurrentPubkey, prev, p.Name)
		}
		seenPubkey[p.CurrentPubkey] = p.Name
		if prev, ok := seenAddr[p.WGCP0Address]; ok {
			return fmt.Errorf("%w: %s shared by %q and %q", ErrDuplicateAddress, p.WGCP0Address, prev, p.Name)
		}
		seenAddr[p.WGCP0Address] = p.Name
		for j, h := range p.PubkeyHistory {
			if h.Pubkey == "" {
				return fmt.Errorf("%w: peers[%d] (%s) history[%d] empty pubkey", ErrSchemaInvalid, i, p.Name, j)
			}
			if h.RotatedAt.IsZero() {
				return fmt.Errorf("%w: peers[%d] (%s) history[%d] zero rotated_at", ErrSchemaInvalid, i, p.Name, j)
			}
		}
	}

	seenRelay := make(map[string]struct{}, len(ps.Relays))
	for i, r := range ps.Relays {
		if r.Name == "" {
			return fmt.Errorf("%w: relays[%d] empty name", ErrSchemaInvalid, i)
		}
		if _, ok := seenRelay[r.Name]; ok {
			return fmt.Errorf("%w: relay %s appears twice", ErrDuplicateName, r.Name)
		}
		seenRelay[r.Name] = struct{}{}
	}

	for i, dp := range ps.DeprecatedPeers {
		if dp.Name == "" {
			return fmt.Errorf("%w: deprecated_peers[%d] empty name", ErrSchemaInvalid, i)
		}
		if dp.RemovedAt.IsZero() {
			return fmt.Errorf("%w: deprecated_peers[%d] (%s) zero removed_at", ErrSchemaInvalid, i, dp.Name)
		}
	}

	if ps.ActiveForwarder != nil {
		if ps.ActiveForwarder.Tier == "" || ps.ActiveForwarder.Host == "" {
			return fmt.Errorf("%w: active_forwarder requires tier + host", ErrSchemaInvalid)
		}
	}

	return nil
}
